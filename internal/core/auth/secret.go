package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// TokenPrefix 上传 Token 明文的固定前缀,便于人工识别与秘密扫描工具匹配。
const TokenPrefix = "imgy_"

const (
	secretRandomBytes    = 32 // 凭证随机部分长度,字节;256 位熵,无需慢哈希
	tokenPrefixChars     = 12 // 落库供列表识别的明文前缀长度,字符
	initialPasswordBytes = 15 // 首启自动生成初始密码的随机字节数;120 位熵,兼顾强度与可手输长度
)

// NewRandomPassword 生成一个高熵初始密码(base64url,约 20 字符)。
// @Description: 首启未配置管理员密码时由引导流程调用;明文经启动日志一次性告知运维,
// 入库只存 argon2id 哈希。定位是"开箱可登录"的临时密码,登录后应尽快在后台改。
// @return 明文密码
// @return 随机源失败时的错误
func NewRandomPassword() (string, error) {
	b := make([]byte, initialPasswordBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成初始密码: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// newTokenPlaintext 生成一个上传 Token 明文,形如 imgy_<base64url 随机串>。
// @return Token 明文
// @return 随机源失败时的错误
func newTokenPlaintext() (string, error) {
	raw, err := randomString()
	if err != nil {
		return "", fmt.Errorf("生成 Token: %w", err)
	}
	return TokenPrefix + raw, nil
}

// randomString 生成 256 位熵的 base64url 随机串。
// @return 43 字符随机串
// @return 随机源失败时的错误
func randomString() (string, error) {
	b := make([]byte, secretRandomBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashSecret 计算凭证明文的 SHA256 十六进制,作为落库形态。
// @Description: Token 与 session id 均为高熵随机串,单次 SHA256 即不可逆,无需加盐慢哈希。
// @param s 凭证明文
// @return 64 字符十六进制哈希
func hashSecret(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
