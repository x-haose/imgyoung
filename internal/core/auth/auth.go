// Package auth 提供认证域:后台登录会话(密码 + session + CSRF)与数据 API
// 的具名 Token(签发/校验/吊销),以及登录限速与 Token 使用时刻的异步回写。
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// sessionTTL 会话绝对有效期,到期须重新登录,不做滑动续期。
const sessionTTL = 30 * 24 * time.Hour

// 认证域哨兵错误,调用方据此映射 HTTP 状态码。
var (
	ErrInvalidCredentials = errors.New("auth: 用户名或密码错误")
	ErrUserDisabled       = errors.New("auth: 用户已被禁用")
	ErrTooManyAttempts    = errors.New("auth: 登录尝试过于频繁")
	ErrSessionInvalid     = errors.New("auth: 会话无效或已过期")
	ErrTokenInvalid       = errors.New("auth: Token 无效或已吊销")
	ErrTokenNotFound      = errors.New("auth: Token 不存在")
	ErrTokenNameTaken     = errors.New("auth: 同名 Token 已存在")
	ErrTokenActive        = errors.New("auth: Token 仍有效,不可删除")
)

// dummyPHC 用户不存在时仍参与校验的占位哈希,抹平存在与否的时间差,防用户名枚举。
var dummyPHC = func() string {
	phc, err := HashPassword("imgyoung-dummy-password")
	if err != nil {
		panic(fmt.Sprintf("初始化占位密码哈希: %v", err))
	}
	return phc
}()

// Config Service 的全部依赖。
type Config struct {
	Conn    *sql.DB       // 数据库连接(事务用)
	Queries *sqlc.Queries // sqlc 查询集
	Logger  *slog.Logger  // 结构化日志
}

// Service 认证服务;除 Token 使用追踪外无可变状态,并发安全。
type Service struct {
	cfg       Config
	limiter   *LoginLimiter       // 登录尝试限速,按 IP 与用户名双键
	mu        sync.Mutex          // 保护 tokenSeen
	tokenSeen map[int64]time.Time // Token id → 最近使用时刻,待批量回写
}

// Principal 认证通过后的请求主体,由中间件解析并显式传参进业务层。
type Principal struct {
	UserID             string // 用户 id
	Name               string // 登录名
	Role               string // 角色:admin / user
	MustChangePassword bool   // 当前仍为出厂默认密码,前端据此强制跳改密页
}

// IsAdmin 判定主体是否具备管理员角色。
// @return 管理员为 true
func (p Principal) IsAdmin() bool { return p.Role == "admin" }

// LoginResult 一次成功登录的产物。
type LoginResult struct {
	SessionID string    // 会话 id 明文,仅此一次下发,进 HttpOnly cookie
	CSRFToken string    // 绑定该会话的 CSRF 令牌
	Principal Principal // 登录主体
}

// IssuedToken 一次 Token 签发的产物。
type IssuedToken struct {
	ID        int64  // Token 记录 id
	Name      string // 用途名(轮转时继承原 Token)
	Plaintext string // Token 明文,仅签发时返回一次
	Prefix    string // 明文前缀,列表识别用
}

// TokenPrincipal Token 认证通过后的主体与凭证信息。
type TokenPrincipal struct {
	UserID  string // Token 所属用户 id
	TokenID int64  // Token 记录 id,用于使用追踪与上传来源记录
	Scopes  string // 授权范围
}

// New 创建认证服务。
// @param cfg 依赖,字段均必填
// @return 就绪的认证服务
func New(cfg Config) *Service {
	return &Service{cfg: cfg, limiter: NewLoginLimiter(), tokenSeen: map[int64]time.Time{}}
}

// Login 校验用户名密码并创建会话。
// @Description: 尝试先过 IP 与用户名双键限速;用户不存在时仍执行一次占位哈希校验,
// 抹平时间差防枚举。成功产出的 session id 只存哈希,明文仅此一次返回。
// @param ctx 控制超时与取消
// @param name 登录名
// @param password 明文密码
// @param clientIP 客户端 IP,限速键
// @return 会话明文、CSRF 令牌与主体
// @return ErrTooManyAttempts / ErrInvalidCredentials / ErrUserDisabled 或基础设施错误
func (s *Service) Login(ctx context.Context, name, password, clientIP string) (LoginResult, error) {
	if !s.limiter.Allow("ip:"+clientIP) || !s.limiter.Allow("user:"+name) {
		return LoginResult{}, ErrTooManyAttempts
	}
	user, err := s.cfg.Queries.GetUserByName(ctx, name)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, _ = VerifyPassword(dummyPHC, password)
		return LoginResult{}, ErrInvalidCredentials
	case err != nil:
		return LoginResult{}, fmt.Errorf("查询用户: %w", err)
	}
	// 空哈希表示密码从未初始化,视为不可登录而非空密码可登录。
	if user.PasswordHash == "" {
		_, _ = VerifyPassword(dummyPHC, password)
		return LoginResult{}, ErrInvalidCredentials
	}
	ok, err := VerifyPassword(user.PasswordHash, password)
	if err != nil {
		return LoginResult{}, fmt.Errorf("校验密码: %w", err)
	}
	if !ok {
		return LoginResult{}, ErrInvalidCredentials
	}
	if user.Disabled != 0 {
		return LoginResult{}, ErrUserDisabled
	}
	// 凭证正确即清空限速计数:限速只针对失败尝试,不误伤合法用户的正常登录。
	s.limiter.Reset("ip:" + clientIP)
	s.limiter.Reset("user:" + name)
	sessionID, err := randomString()
	if err != nil {
		return LoginResult{}, fmt.Errorf("生成会话 id: %w", err)
	}
	csrf, err := randomString()
	if err != nil {
		return LoginResult{}, fmt.Errorf("生成 CSRF 令牌: %w", err)
	}
	if err := s.cfg.Queries.CreateSession(ctx, sqlc.CreateSessionParams{
		TokenHash: hashSecret(sessionID),
		UserID:    user.ID,
		CsrfToken: csrf,
		ExpiresAt: sqlc.TimeText(time.Now().Add(sessionTTL)),
	}); err != nil {
		return LoginResult{}, fmt.Errorf("写入会话: %w", err)
	}
	return LoginResult{
		SessionID: sessionID,
		CSRFToken: csrf,
		Principal: Principal{
			UserID: user.ID, Name: user.Name, Role: user.Role,
			MustChangePassword: user.MustChangePassword != 0,
		},
	}, nil
}

// SessionInfo 会话校验通过后的主体与 CSRF 令牌。
type SessionInfo struct {
	Principal Principal // 会话主体
	CSRFToken string    // 绑定该会话的 CSRF 令牌
}

// ValidateSession 校验会话明文并返回主体。
// @Description: 过期与用户被禁用均视为会话无效;过期行留待清理任务回收,此处不删。
// @param ctx 控制超时与取消
// @param sessionID 会话 id 明文(来自 cookie)
// @return 主体与 CSRF 令牌
// @return ErrSessionInvalid 或基础设施错误
func (s *Service) ValidateSession(ctx context.Context, sessionID string) (SessionInfo, error) {
	row, err := s.cfg.Queries.GetSession(ctx, hashSecret(sessionID))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return SessionInfo{}, ErrSessionInvalid
	case err != nil:
		return SessionInfo{}, fmt.Errorf("查询会话: %w", err)
	}
	if row.ExpiresAt <= sqlc.TimeText(time.Now()) || row.UserDisabled != 0 {
		return SessionInfo{}, ErrSessionInvalid
	}
	return SessionInfo{
		Principal: Principal{
			UserID: row.UserID, Name: row.UserName, Role: row.UserRole,
			MustChangePassword: row.UserMustChangePassword != 0,
		},
		CSRFToken: row.CsrfToken,
	}, nil
}

// Logout 删除会话,幂等。
// @param ctx 控制超时与取消
// @param sessionID 会话 id 明文
// @return 基础设施错误
func (s *Service) Logout(ctx context.Context, sessionID string) error {
	if err := s.cfg.Queries.DeleteSession(ctx, hashSecret(sessionID)); err != nil {
		return fmt.Errorf("删除会话: %w", err)
	}
	return nil
}

// SetPassword 重置用户密码并吊销其全部会话。
// @Description: 改密后既有会话一律失效,持旧 cookie 的浏览器须重新登录。
// @param ctx 控制超时与取消
// @param userID 目标用户 id
// @param password 新明文密码
// @return 基础设施错误
func (s *Service) SetPassword(ctx context.Context, userID, password string) error {
	phc, err := HashPassword(password)
	if err != nil {
		return err
	}
	if err := s.cfg.Queries.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{
		PasswordHash: phc, ID: userID,
	}); err != nil {
		return fmt.Errorf("更新密码: %w", err)
	}
	if err := s.cfg.Queries.DeleteSessionsByUser(ctx, userID); err != nil {
		return fmt.Errorf("吊销用户会话: %w", err)
	}
	return nil
}

// DeleteExpiredSessions 回收已过期会话,供周期清理任务调用。
// @param ctx 控制超时与取消
// @return 回收行数
// @return 基础设施错误
func (s *Service) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	n, err := s.cfg.Queries.DeleteExpiredSessions(ctx, sqlc.TimeText(time.Now()))
	if err != nil {
		return 0, fmt.Errorf("回收过期会话: %w", err)
	}
	return n, nil
}
