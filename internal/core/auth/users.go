package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	sqlite "modernc.org/sqlite"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/id"
)

// 用户角色取值。
const (
	RoleAdmin = "admin" // 管理全站:用户/渠道/裁决/审计
	RoleUser  = "user"  // 只见自己的图片与 Token
)

// 用户管理哨兵错误,调用方据此映射 HTTP 状态码。
var (
	ErrUserNameTaken = errors.New("auth: 登录名已存在")
	ErrUserHasImages = errors.New("auth: 用户名下仍有图片")
	ErrUserProtected = errors.New("auth: 内置管理员不可禁用、降级或删除")
	ErrUserNotFound  = errors.New("auth: 用户不存在")
	ErrInvalidRole   = errors.New("auth: 角色取值非法")
	ErrEmptyPassword = errors.New("auth: 密码不能为空")
)

// CreateUser 创建用户(管理员建号,不开放注册)。
// @Description: 密码必填并即时可登录;quotaBytes <= 0 表示不限配额。
// @param ctx 控制超时与取消
// @param name 登录名,全局唯一
// @param password 初始明文密码
// @param role 角色,见 Role 常量
// @param quotaBytes 存储配额上限,按引用计全额;<= 0 表示不限
// @return 新建用户行
// @return ErrUserNameTaken / ErrInvalidRole / ErrEmptyPassword 或基础设施错误
func (s *Service) CreateUser(ctx context.Context, name, password, role string, quotaBytes int64) (sqlc.User, error) {
	if role != RoleAdmin && role != RoleUser {
		return sqlc.User{}, ErrInvalidRole
	}
	if password == "" {
		return sqlc.User{}, ErrEmptyPassword
	}
	phc, err := HashPassword(password)
	if err != nil {
		return sqlc.User{}, err
	}
	userID, err := id.New()
	if err != nil {
		return sqlc.User{}, fmt.Errorf("生成用户 id: %w", err)
	}
	if err := s.cfg.Queries.CreateUser(ctx, sqlc.CreateUserParams{
		ID:           userID,
		Name:         name,
		PasswordHash: phc,
		Role:         role,
		QuotaBytes:   quotaNull(quotaBytes),
	}); err != nil {
		if isUniqueViolation(err, "users.name") {
			return sqlc.User{}, ErrUserNameTaken
		}
		return sqlc.User{}, fmt.Errorf("写入用户: %w", err)
	}
	user, err := s.cfg.Queries.GetUser(ctx, userID)
	if err != nil {
		return sqlc.User{}, fmt.Errorf("读取新建用户: %w", err)
	}
	return user, nil
}

// UpdateUserParams 用户资料的完整目标状态(PUT 语义)。
type UpdateUserParams struct {
	Name       string // 登录名
	Role       string // 角色,见 Role 常量
	QuotaBytes int64  // 存储配额;<= 0 表示不限
	Disabled   bool   // 是否禁用
}

// UpdateUser 更新用户资料;禁用即时生效(吊销其全部会话)。
// @Description: 内置管理员不可禁用与降级,防止把系统锁死在无管理员状态。
// @param ctx 控制超时与取消
// @param userID 目标用户 id
// @param p 目标状态
// @return ErrUserNotFound / ErrUserProtected / ErrUserNameTaken / ErrInvalidRole 或基础设施错误
func (s *Service) UpdateUser(ctx context.Context, userID string, p UpdateUserParams) error {
	if p.Role != RoleAdmin && p.Role != RoleUser {
		return ErrInvalidRole
	}
	if _, err := s.getUser(ctx, userID); err != nil {
		return err
	}
	if userID == sqlc.DefaultUserID && (p.Role != RoleAdmin || p.Disabled) {
		return ErrUserProtected
	}
	if err := s.cfg.Queries.UpdateUser(ctx, sqlc.UpdateUserParams{
		Name:       p.Name,
		Role:       p.Role,
		QuotaBytes: quotaNull(p.QuotaBytes),
		Disabled:   boolInt(p.Disabled),
		ID:         userID,
	}); err != nil {
		if isUniqueViolation(err, "users.name") {
			return ErrUserNameTaken
		}
		return fmt.Errorf("更新用户: %w", err)
	}
	if p.Disabled {
		if err := s.cfg.Queries.DeleteSessionsByUser(ctx, userID); err != nil {
			return fmt.Errorf("吊销被禁用用户会话: %w", err)
		}
	}
	return nil
}

// DeleteUser 删除用户及其凭证(Token 与会话)。
// @Description: 名下仍有图片时拒绝——图片须先删除或另行处置,不做隐式级联删图;
// 凭证行随用户在单事务内删除,审计留痕保留(actor 记的是 id 文本)。
// @param ctx 控制超时与取消
// @param userID 目标用户 id
// @return ErrUserProtected / ErrUserNotFound / ErrUserHasImages 或基础设施错误
func (s *Service) DeleteUser(ctx context.Context, userID string) error {
	if userID == sqlc.DefaultUserID {
		return ErrUserProtected
	}
	if _, err := s.getUser(ctx, userID); err != nil {
		return err
	}
	// 快速路径:名下有图直接拒绝。并发下(查后、删前有新上传)本判断可能漏网,
	// 最终由下方 DeleteUser 撞外键约束兜住——两处都收敛到同一个 ErrUserHasImages。
	n, err := s.cfg.Queries.CountImagesByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("统计用户图片: %w", err)
	}
	if n > 0 {
		return ErrUserHasImages
	}
	tx, err := s.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := s.cfg.Queries.WithTx(tx)
	if err := q.DeleteTokensByUser(ctx, userID); err != nil {
		return fmt.Errorf("删除用户 Token: %w", err)
	}
	if err := q.DeleteSessionsByUser(ctx, userID); err != nil {
		return fmt.Errorf("删除用户会话: %w", err)
	}
	if err := q.DeleteUser(ctx, userID); err != nil {
		if isForeignKeyViolation(err) {
			return ErrUserHasImages
		}
		return fmt.Errorf("删除用户: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交用户删除: %w", err)
	}
	return nil
}

// ChangePassword 校验旧密码后改密,改密后全部会话失效。
// @param ctx 控制超时与取消
// @param userID 目标用户 id
// @param oldPassword 旧明文密码
// @param newPassword 新明文密码
// @return ErrInvalidCredentials / ErrEmptyPassword / ErrUserNotFound 或基础设施错误
func (s *Service) ChangePassword(ctx context.Context, userID, oldPassword, newPassword string) error {
	if newPassword == "" {
		return ErrEmptyPassword
	}
	user, err := s.getUser(ctx, userID)
	if err != nil {
		return err
	}
	ok, err := VerifyPassword(user.PasswordHash, oldPassword)
	if err != nil || !ok {
		return ErrInvalidCredentials
	}
	return s.SetPassword(ctx, userID, newPassword)
}

// getUser 读取用户并把 sql.ErrNoRows 归一为 ErrUserNotFound。
// @param ctx 控制超时与取消
// @param userID 目标用户 id
// @return 用户行
// @return ErrUserNotFound 或基础设施错误
func (s *Service) getUser(ctx context.Context, userID string) (sqlc.User, error) {
	user, err := s.cfg.Queries.GetUser(ctx, userID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return sqlc.User{}, ErrUserNotFound
	case err != nil:
		return sqlc.User{}, fmt.Errorf("查询用户: %w", err)
	}
	return user, nil
}

// quotaNull 把 <= 0 的配额归一为 NULL(不限)。
// @param quotaBytes 配额字节数
// @return 可空配额列值
func quotaNull(quotaBytes int64) sql.NullInt64 {
	return sql.NullInt64{Int64: quotaBytes, Valid: quotaBytes > 0}
}

// boolInt 布尔转 SQLite 的 0/1 表达。
// @param b 布尔值
// @return 0 或 1
func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// isUniqueViolation 判定错误是否为指定列的唯一约束冲突。
// @param err 待判定错误
// @param column 形如 "users.name" 的约束列
// @return 命中为 true
func isUniqueViolation(err error, column string) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqliteConstraintUnique && strings.Contains(se.Error(), column)
}

// sqliteConstraintForeignKey modernc 驱动的外键约束冲突扩展错误码(SQLITE_CONSTRAINT_FOREIGNKEY)。
const sqliteConstraintForeignKey = 787

// isForeignKeyViolation 判定错误是否为外键约束冲突。
// @param err 待判定错误
// @return 命中为 true
func isForeignKeyViolation(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqliteConstraintForeignKey
}
