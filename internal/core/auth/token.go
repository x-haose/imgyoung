package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	sqlite "modernc.org/sqlite"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// sqliteConstraintUnique modernc 驱动的唯一约束冲突扩展错误码(SQLITE_CONSTRAINT_UNIQUE)。
const sqliteConstraintUnique = 2067

// IssueToken 为用户签发一个具名上传 Token。
// @Description: 明文仅此一次返回,落库只存 SHA256 与识别前缀;同一用户的有效
// Token 名唯一,与已吊销 Token 重名不冲突。
// @param ctx 控制超时与取消
// @param userID 所属用户 id
// @param name Token 用途名
// @return 签发结果(含明文)
// @return ErrTokenNameTaken 或基础设施错误
func (s *Service) IssueToken(ctx context.Context, userID, name string) (IssuedToken, error) {
	plaintext, err := newTokenPlaintext()
	if err != nil {
		return IssuedToken{}, err
	}
	prefix := plaintext[:tokenPrefixChars]
	id, err := s.cfg.Queries.CreateToken(ctx, sqlc.CreateTokenParams{
		UserID:    userID,
		Name:      name,
		TokenHash: hashSecret(plaintext),
		Prefix:    prefix,
		Scopes:    "upload",
	})
	if err != nil {
		var se *sqlite.Error
		if errors.As(err, &se) && se.Code() == sqliteConstraintUnique &&
			strings.Contains(se.Error(), "tokens.name") {
			return IssuedToken{}, ErrTokenNameTaken
		}
		return IssuedToken{}, fmt.Errorf("写入 Token: %w", err)
	}
	return IssuedToken{ID: id, Plaintext: plaintext, Prefix: prefix}, nil
}

// AuthenticateToken 校验上传 Token 明文并返回其主体。
// @Description: 命中后仅在内存记录使用时刻,由 FlushTokenUsage 批量回写,
// 不逐请求写库。已吊销、不存在或所属用户被禁用均返回 ErrTokenInvalid。
// @param ctx 控制超时与取消
// @param plaintext Token 明文(Authorization 头去除 Bearer 前缀后)
// @return Token 主体
// @return ErrTokenInvalid 或基础设施错误
func (s *Service) AuthenticateToken(ctx context.Context, plaintext string) (TokenPrincipal, error) {
	row, err := s.cfg.Queries.GetActiveTokenByHash(ctx, hashSecret(plaintext))
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

// RevokeToken 吊销用户名下的一个 Token,幂等以外的未命中视为不存在。
// @Description: user_id 同时作为归属校验,普通用户无法吊销他人 Token。
// @param ctx 控制超时与取消
// @param userID Token 所属用户 id
// @param tokenID Token 记录 id
// @return ErrTokenNotFound 或基础设施错误
func (s *Service) RevokeToken(ctx context.Context, userID string, tokenID int64) error {
	n, err := s.cfg.Queries.RevokeToken(ctx, sqlc.RevokeTokenParams{
		RevokedAt: sql.NullString{String: sqlc.TimeText(time.Now()), Valid: true},
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

// EnsureBootstrapToken 确保环境变量指定的引导 Token 在库,幂等。
// @Description: 明文哈希已在库(任意名下)即不动;否则吊销该用户旧的 bootstrap
// Token 并以当前明文重建——环境变量的当前值即 bootstrap 名下的唯一真相,
// headless 部署不进 UI 即可上传。
// @param ctx 控制超时与取消
// @param userID 归属用户 id
// @param plaintext 引导 Token 明文(来自环境变量,长度形态不限)
// @return 基础设施错误
func (s *Service) EnsureBootstrapToken(ctx context.Context, userID, plaintext string) error {
	_, err := s.cfg.Queries.GetActiveTokenByHash(ctx, hashSecret(plaintext))
	switch {
	case err == nil:
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("查询引导 Token: %w", err)
	}
	if _, err := s.cfg.Queries.RevokeTokenByName(ctx, sqlc.RevokeTokenByNameParams{
		RevokedAt: sql.NullString{String: sqlc.TimeText(time.Now()), Valid: true},
		UserID:    userID,
		Name:      "bootstrap",
	}); err != nil {
		return fmt.Errorf("吊销旧引导 Token: %w", err)
	}
	prefix := plaintext
	if len(prefix) > tokenPrefixChars {
		prefix = prefix[:tokenPrefixChars]
	}
	if _, err := s.cfg.Queries.CreateToken(ctx, sqlc.CreateTokenParams{
		UserID:    userID,
		Name:      "bootstrap",
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
			LastUsedAt: sql.NullString{String: sqlc.TimeText(at), Valid: true},
			ID:         id,
		}); err != nil {
			s.cfg.Logger.Warn("回写 Token 使用时刻失败", "token", id, "err", err)
		}
	}
}
