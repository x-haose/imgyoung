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

// DefaultAdminPassword 内置管理员的出厂默认密码,首启播种。
// @Description: 公开的固定初始凭证——仅为开箱可登录,任何暴露实例改密前都可被此密码登入。
// 播种时置 must_change_password=1,改掉它之前会话只能退出、查看自己与改密(服务端拦截)。
const DefaultAdminPassword = "imgyoung123"

const (
	secretRandomBytes = 32 // 凭证随机部分长度,字节;256 位熵,无需慢哈希
	tokenPrefixChars  = 12 // 落库供列表识别的明文前缀长度,字符
)

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
