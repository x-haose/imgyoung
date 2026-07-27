package auth

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// s3SecretPurpose S3 密钥的派生用途前缀,与其他派生用途隔离。
const s3SecretPurpose = "s3-secret:"

// s3SecretBytes S3 密钥的随机长度,字节;十六进制展示为 64 字符。
const s3SecretBytes = 32

// S3Credential 一枚 Token 对应的 S3 协议凭证。
type S3Credential struct {
	AccessKeyID string         // 凭证标识,即 Token 明文前缀;落库可查,不足以还原 Token
	SecretKey   string         // 凭证密钥,由主密钥按 Token 哈希派生,不落库
	Principal   TokenPrincipal // 该凭证对应的认证主体;后台展示场景为零值
}

// S3SecretFor 派生某 Token 对应的 S3 密钥。
// @Description: 密钥不落库、按需重算——Token 只存哈希,再存一份可逆的 S3 密钥等于
// 给凭证开第二个可被拖库还原的入口。派生源是主密钥,故主密钥一换,全部 S3 凭证同步失效。
// @param tokenHash Token 明文的哈希(其唯一性即凭证隔离的依据)
// @return 十六进制密钥
// @return 派生失败时的错误
func (s *Service) S3SecretFor(tokenHash string) (string, error) {
	key, err := s.cfg.Master.Derive(s3SecretPurpose+tokenHash, s3SecretBytes)
	if err != nil {
		return "", fmt.Errorf("派生 S3 密钥: %w", err)
	}
	return hex.EncodeToString(key), nil
}

// LookupS3Credential 按 access key 取出对应的有效 Token 凭证。
// @Description: access key 即 Token 明文前缀,唯一定位一枚 Token;吊销、过期与用户禁用
// 一律归一为 ErrTokenInvalid,与 Bearer 路径同口径。返回的密钥供调用方重算 SigV4 签名。
// 本方法只查不记使用时刻——access key 在后台列表可见,若查到就记,任何人拿它配一个乱签名
// 就能持续刷新他人 Token 的"最近使用",让闲置判断失真;由调用方在验签通过后显式标记。
// @param ctx 控制超时与取消
// @param accessKeyID S3 客户端提交的 access key
// @return 凭证与其主体
// @return ErrTokenInvalid 或基础设施错误
func (s *Service) LookupS3Credential(ctx context.Context, accessKeyID string) (S3Credential, error) {
	row, err := s.cfg.Queries.GetActiveTokenByPrefix(ctx, sqlc.GetActiveTokenByPrefixParams{
		Prefix: accessKeyID,
		Now:    sqlc.TimeNull(time.Now()),
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return S3Credential{}, ErrTokenInvalid
	case err != nil:
		return S3Credential{}, fmt.Errorf("查询 Token: %w", err)
	}
	if row.UserDisabled != 0 {
		return S3Credential{}, ErrTokenInvalid
	}
	secretKey, err := s.S3SecretFor(row.TokenHash)
	if err != nil {
		return S3Credential{}, err
	}
	return S3Credential{
		AccessKeyID: accessKeyID,
		SecretKey:   secretKey,
		Principal:   TokenPrincipal{UserID: row.UserID, TokenID: row.ID, Scopes: row.Scopes},
	}, nil
}

// MarkTokenUsed 记一次 Token 使用,供签名校验通过后调用。
// @Description: 仅内存累计,由 FlushTokenUsage 批量回写,不逐请求写库。
// @param tokenID Token 记录 id
func (s *Service) MarkTokenUsed(tokenID int64) {
	s.mu.Lock()
	s.tokenSeen[tokenID] = time.Now()
	s.mu.Unlock()
}

// S3CredentialForToken 取某用户名下一枚 Token 的 S3 凭证,供后台展示。
// @Description: 归属校验与吊销/删除同口径——非本人 Token 归一为 ErrTokenNotFound,
// 不泄露他人 Token 的存在性。已吊销的 Token 仍可查出凭证形态,但签名校验会拒绝它。
// @param ctx 控制超时与取消
// @param userID 请求者用户 id
// @param tokenID Token 记录 id
// @return 凭证(不含主体)
// @return ErrTokenNotFound 或基础设施错误
func (s *Service) S3CredentialForToken(ctx context.Context, userID string, tokenID int64) (S3Credential, error) {
	token, err := s.getOwnedToken(ctx, userID, tokenID)
	if err != nil {
		return S3Credential{}, err
	}
	secretKey, err := s.S3SecretFor(token.TokenHash)
	if err != nil {
		return S3Credential{}, err
	}
	return S3Credential{AccessKeyID: token.Prefix, SecretKey: secretKey}, nil
}
