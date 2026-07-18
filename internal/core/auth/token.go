package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// sqliteConstraintUnique modernc 驱动的唯一约束冲突扩展错误码(SQLITE_CONSTRAINT_UNIQUE)。
const sqliteConstraintUnique = 2067

// bootstrapTokenName 环境变量托管的引导 Token 在库中的固定用途名。
const bootstrapTokenName = "bootstrap"

// IssueToken 为用户签发一个具名上传 Token。
// @Description: 明文仅此一次返回,落库只存 SHA256 与识别前缀;同一用户的有效
// Token 名唯一,与已吊销 Token 重名不冲突。到期与吊销同等失效:同名旧 Token
// 已过期时先吊销过期行再签发,不算重名冲突。expiresIn <= 0 表示永久。
// @param ctx 控制超时与取消
// @param userID 所属用户 id
// @param name Token 用途名
// @param expiresIn 有效时长;<= 0 表示永久
// @return 签发结果(含明文)
// @return ErrTokenNameTaken 或基础设施错误
func (s *Service) IssueToken(ctx context.Context, userID, name string, expiresIn time.Duration) (IssuedToken, error) {
	now := time.Now()
	var expiresAt sql.NullString
	if expiresIn > 0 {
		expiresAt = sqlc.TimeNull(now.Add(expiresIn))
	}
	issued, err := issueToken(ctx, s.cfg.Queries, userID, name, expiresAt)
	if !errors.Is(err, ErrTokenNameTaken) {
		return issued, err
	}
	// 同名唯一索引只约束未吊销行,占名的可能是一条已过期未吊销的行;
	// 吊销条件带过期谓词,活跃同名行不受影响,吊销到行才重试。
	n, revokeErr := s.cfg.Queries.RevokeExpiredTokenByName(ctx, sqlc.RevokeExpiredTokenByNameParams{
		RevokedAt: sqlc.TimeNull(now),
		UserID:    userID,
		Name:      name,
		Now:       sqlc.TimeNull(now),
	})
	if revokeErr != nil {
		return IssuedToken{}, fmt.Errorf("吊销过期同名 Token: %w", revokeErr)
	}
	if n == 0 {
		return IssuedToken{}, ErrTokenNameTaken
	}
	return issueToken(ctx, s.cfg.Queries, userID, name, expiresAt)
}

// issueToken 生成明文并写入一条 Token 记录(供签发与轮转事务共用)。
// @param ctx 控制超时与取消
// @param q 查询集,轮转时为事务绑定形态
// @param userID 所属用户 id
// @param name Token 用途名
// @param expiresAt 到期时刻;零值表示永久
// @return 签发结果(含明文)
// @return ErrTokenNameTaken 或基础设施错误
func issueToken(ctx context.Context, q *sqlc.Queries, userID, name string, expiresAt sql.NullString) (IssuedToken, error) {
	plaintext, err := newTokenPlaintext()
	if err != nil {
		return IssuedToken{}, err
	}
	prefix := plaintext[:tokenPrefixChars]
	id, err := q.CreateToken(ctx, sqlc.CreateTokenParams{
		UserID:    userID,
		Name:      name,
		TokenHash: hashSecret(plaintext),
		Prefix:    prefix,
		Scopes:    "upload",
		ExpiresAt: expiresAt,
	})
	if err != nil {
		if isUniqueViolation(err, "tokens.name") {
			return IssuedToken{}, ErrTokenNameTaken
		}
		return IssuedToken{}, fmt.Errorf("写入 Token: %w", err)
	}
	return IssuedToken{ID: id, Name: name, Plaintext: plaintext, Prefix: prefix}, nil
}

// AuthenticateToken 校验上传 Token 明文并返回其主体。
// @Description: 命中后仅在内存记录使用时刻,由 FlushTokenUsage 批量回写,
// 不逐请求写库。已吊销、已过期、不存在或所属用户被禁用均返回 ErrTokenInvalid。
// @param ctx 控制超时与取消
// @param plaintext Token 明文(Authorization 头去除 Bearer 前缀后)
// @return Token 主体
// @return ErrTokenInvalid 或基础设施错误
func (s *Service) AuthenticateToken(ctx context.Context, plaintext string) (TokenPrincipal, error) {
	row, err := s.cfg.Queries.GetActiveTokenByHash(ctx, sqlc.GetActiveTokenByHashParams{
		TokenHash: hashSecret(plaintext),
		Now:       sqlc.TimeNull(time.Now()),
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return TokenPrincipal{}, ErrTokenInvalid
	case err != nil:
		return TokenPrincipal{}, fmt.Errorf("查询 Token: %w", err)
	}
	if row.UserDisabled != 0 {
		return TokenPrincipal{}, ErrTokenInvalid
	}
	s.mu.Lock()
	s.tokenSeen[row.ID] = time.Now()
	s.mu.Unlock()
	return TokenPrincipal{UserID: row.UserID, TokenID: row.ID, Scopes: row.Scopes}, nil
}

// IsBootstrapToken 判定 Token 行是否为环境变量托管中的引导 Token。
// @Description: 仅当环境变量当前配置、且该行未吊销未过期时才视为托管;
// 未配置时同名 Token 按普通 Token 处理,环境变量换值遗留的已吊销历史行
// 按普通失效记录处理(可正常删除)。
// @param row Token 行
// @return 托管中的引导 Token 为 true
func (s *Service) IsBootstrapToken(row sqlc.Token) bool {
	return s.cfg.BootstrapManaged && row.UserID == sqlc.DefaultUserID &&
		row.Name == bootstrapTokenName &&
		!row.RevokedAt.Valid && !isExpired(row.ExpiresAt, time.Now())
}

// RevokeToken 吊销用户名下的一个 Token。
// @Description: user_id 同时作为归属校验,普通用户无法吊销他人 Token;
// 已吊销或不存在均归一为 ErrTokenNotFound。托管中的引导 Token 拒绝吊销——
// 吊销会在下次重启被环境变量复活,假反馈比无操作更糟。
// @param ctx 控制超时与取消
// @param userID Token 所属用户 id
// @param tokenID Token 记录 id
// @return ErrTokenNotFound / ErrTokenProtected 或基础设施错误
func (s *Service) RevokeToken(ctx context.Context, userID string, tokenID int64) error {
	row, err := s.getOwnedToken(ctx, userID, tokenID)
	if err != nil {
		return err
	}
	if s.IsBootstrapToken(row) {
		return ErrTokenProtected
	}
	n, err := s.cfg.Queries.RevokeToken(ctx, sqlc.RevokeTokenParams{
		RevokedAt: sqlc.TimeNull(time.Now()),
		ID:        tokenID,
		UserID:    userID,
	})
	if err != nil {
		return fmt.Errorf("吊销 Token: %w", err)
	}
	if n == 0 {
		return ErrTokenNotFound
	}
	return nil
}

// RotateToken 轮转 Token:同名新 Token 签发与旧 Token 吊销在单事务内原子完成。
// @Description: 仅限本人的有效 Token;新 Token 继承原过期语义——原为永久则新的
// 永久,原有过期则从现在起算同样时长。同名唯一索引只约束有效行,旧行在同一
// 事务内先吊销,新行写入不冲突;任一步失败整体回滚,旧 Token 保持可用。
// @param ctx 控制超时与取消
// @param userID Token 所属用户 id(归属校验)
// @param tokenID 待轮转的 Token 记录 id
// @return 新 Token 签发结果(含明文)
// @return ErrTokenNotFound / ErrTokenProtected 或基础设施错误
func (s *Service) RotateToken(ctx context.Context, userID string, tokenID int64) (IssuedToken, error) {
	row, err := s.getOwnedToken(ctx, userID, tokenID)
	if err != nil {
		return IssuedToken{}, err
	}
	if s.IsBootstrapToken(row) {
		return IssuedToken{}, ErrTokenProtected
	}
	now := time.Now()
	if row.RevokedAt.Valid || isExpired(row.ExpiresAt, now) {
		return IssuedToken{}, ErrTokenNotFound
	}
	expiresAt, err := inheritedExpiry(row, now)
	if err != nil {
		return IssuedToken{}, err
	}
	tx, err := s.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return IssuedToken{}, fmt.Errorf("开启轮转事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := s.cfg.Queries.WithTx(tx)
	n, err := q.RevokeToken(ctx, sqlc.RevokeTokenParams{
		RevokedAt: sqlc.TimeNull(now),
		ID:        tokenID,
		UserID:    userID,
	})
	if err != nil {
		return IssuedToken{}, fmt.Errorf("吊销旧 Token: %w", err)
	}
	if n == 0 {
		// 查后写前被并发吊销,按已失效处理。
		return IssuedToken{}, ErrTokenNotFound
	}
	issued, err := issueToken(ctx, q, row.UserID, row.Name, expiresAt)
	if err != nil {
		return IssuedToken{}, err
	}
	if err := tx.Commit(); err != nil {
		return IssuedToken{}, fmt.Errorf("提交轮转事务: %w", err)
	}
	return issued, nil
}

// DeleteTokenRecord 删除一条已失效(已吊销或已过期)的 Token 记录。
// @Description: 有效 Token 拒绝删除——止损入口是吊销,删除只负责清理历史行;
// 审计留痕不受影响。托管守卫只护当前有效的引导 Token,已失效的引导历史行
// 与普通失效记录同等可删。user_id 同时作为归属校验。
// @param ctx 控制超时与取消
// @param userID Token 所属用户 id
// @param tokenID Token 记录 id
// @return ErrTokenNotFound / ErrTokenActive / ErrTokenProtected 或基础设施错误
func (s *Service) DeleteTokenRecord(ctx context.Context, userID string, tokenID int64) error {
	row, err := s.getOwnedToken(ctx, userID, tokenID)
	if err != nil {
		return err
	}
	now := time.Now()
	if !row.RevokedAt.Valid && !isExpired(row.ExpiresAt, now) {
		if s.IsBootstrapToken(row) {
			return ErrTokenProtected
		}
		return ErrTokenActive
	}
	// DELETE 条件与上面的判断一致,防查后写前的状态漂移。
	n, err := s.cfg.Queries.DeleteTokenRecord(ctx, sqlc.DeleteTokenRecordParams{
		ID:     tokenID,
		UserID: userID,
		Now:    sqlc.TimeNull(now),
	})
	if err != nil {
		return fmt.Errorf("删除 Token 记录: %w", err)
	}
	if n == 0 {
		return ErrTokenNotFound
	}
	return nil
}

// EnsureBootstrapToken 确保环境变量指定的引导 Token 在库,幂等。
// @Description: 明文哈希已在库(任意名下)即不动;否则吊销该用户旧的 bootstrap
// Token 并以当前明文重建——环境变量的当前值即 bootstrap 名下的唯一真相,
// headless 部署不进 UI 即可上传。引导 Token 恒为永久,有效期由环境变量的存续决定。
// @param ctx 控制超时与取消
// @param userID 归属用户 id
// @param plaintext 引导 Token 明文(来自环境变量,长度形态不限)
// @return 基础设施错误
func (s *Service) EnsureBootstrapToken(ctx context.Context, userID, plaintext string) error {
	_, err := s.cfg.Queries.GetActiveTokenByHash(ctx, sqlc.GetActiveTokenByHashParams{
		TokenHash: hashSecret(plaintext),
		Now:       sqlc.TimeNull(time.Now()),
	})
	switch {
	case err == nil:
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("查询引导 Token: %w", err)
	}
	if _, err := s.cfg.Queries.RevokeTokenByName(ctx, sqlc.RevokeTokenByNameParams{
		RevokedAt: sqlc.TimeNull(time.Now()),
		UserID:    userID,
		Name:      bootstrapTokenName,
	}); err != nil {
		return fmt.Errorf("吊销旧引导 Token: %w", err)
	}
	prefix := plaintext
	if len(prefix) > tokenPrefixChars {
		prefix = prefix[:tokenPrefixChars]
	}
	if _, err := s.cfg.Queries.CreateToken(ctx, sqlc.CreateTokenParams{
		UserID:    userID,
		Name:      bootstrapTokenName,
		TokenHash: hashSecret(plaintext),
		Prefix:    prefix,
		Scopes:    "upload",
	}); err != nil {
		return fmt.Errorf("写入引导 Token: %w", err)
	}
	return nil
}

// FlushTokenUsage 将内存中的 Token 最近使用时刻批量回写数据库。
// @Description: 供周期任务调用;单条回写失败仅告警,时刻信息属尽力而为的元数据。
// @param ctx 控制超时与取消
func (s *Service) FlushTokenUsage(ctx context.Context) {
	s.mu.Lock()
	pending := s.tokenSeen
	s.tokenSeen = map[int64]time.Time{}
	s.mu.Unlock()
	for id, at := range pending {
		if err := s.cfg.Queries.TouchTokenUsed(ctx, sqlc.TouchTokenUsedParams{
			LastUsedAt: sqlc.TimeNull(at),
			ID:         id,
		}); err != nil {
			s.cfg.Logger.Warn("回写 Token 使用时刻失败", "token", id, "err", err)
		}
	}
}

// getOwnedToken 读取 Token 行并做归属校验;跨用户访问与不存在同样归一为不存在,
// 不向调用方泄露他人 Token 的存在性。
// @param ctx 控制超时与取消
// @param userID 期望的归属用户 id
// @param tokenID Token 记录 id
// @return Token 行
// @return ErrTokenNotFound 或基础设施错误
func (s *Service) getOwnedToken(ctx context.Context, userID string, tokenID int64) (sqlc.Token, error) {
	row, err := s.cfg.Queries.GetToken(ctx, tokenID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return sqlc.Token{}, ErrTokenNotFound
	case err != nil:
		return sqlc.Token{}, fmt.Errorf("查询 Token: %w", err)
	}
	if row.UserID != userID {
		return sqlc.Token{}, ErrTokenNotFound
	}
	return row, nil
}

// isExpired 判定可空到期时刻是否已过;NULL 表示永久。
// @param expiresAt 可空到期时刻
// @param now 判定基准时刻
// @return 已过期为 true
func isExpired(expiresAt sql.NullString, now time.Time) bool {
	return expiresAt.Valid && expiresAt.String <= sqlc.TimeText(now)
}

// inheritedExpiry 计算轮转产生的新 Token 的到期时刻:永久保持永久,
// 有过期则以原 Token 的完整有效时长从 now 重新起算。
// @param row 被轮转的原 Token 行
// @param now 起算时刻
// @return 新到期时刻;零值表示永久
// @return 原行时间列不合形态时的错误
func inheritedExpiry(row sqlc.Token, now time.Time) (sql.NullString, error) {
	if !row.ExpiresAt.Valid {
		return sql.NullString{}, nil
	}
	createdAt, err := sqlc.ParseTimeText(row.CreatedAt)
	if err != nil {
		return sql.NullString{}, err
	}
	expiresAt, err := sqlc.ParseTimeText(row.ExpiresAt.String)
	if err != nil {
		return sql.NullString{}, err
	}
	return sqlc.TimeNull(now.Add(expiresAt.Sub(createdAt))), nil
}
