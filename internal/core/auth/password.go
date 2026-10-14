package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id 参数:面向自部署小机型的平衡取值,单次哈希约几十毫秒;
// 参数随 PHC 串一起存储,后续调参不影响既有哈希的校验。
const (
	argonMemoryKiB = 64 * 1024 // 内存开销,KiB
	argonTime      = 3         // 迭代轮数
	argonThreads   = 1         // 并行 lane 数,单 lane 保证低配机可用
	argonSaltLen   = 16        // 盐长度,字节
	argonKeyLen    = 32        // 派生密钥长度,字节
)

// HashPassword 将明文密码哈希为自描述的 argon2id PHC 串。
// @Description: 每次调用生成随机盐,同一密码两次哈希结果不同;
// 产物形如 $argon2id$v=19$m=...,t=...,p=...$salt$key,参数内嵌、可独立校验。
// @param password 明文密码
// @return PHC 格式哈希串
// @return 随机盐生成失败时的错误
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成密码盐: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword 校验明文密码是否与 PHC 哈希串匹配。
// @Description: 参数从哈希串自身解析,常数时间比较;串格式非法视为不匹配的错误而非 panic。
// @param phc HashPassword 产出的哈希串
// @param password 待校验明文密码
// @return 是否匹配
// @return 哈希串格式非法时的错误
func VerifyPassword(phc, password string) (bool, error) {
	p, err := parsePHC(phc)
	if err != nil {
		return false, err
	}
	//nolint:gosec // len(key) 来自解码结果,远小于 uint32 上限
	got := argon2.IDKey([]byte(password), p.salt, p.iterations, p.memoryKiB, p.lanes, uint32(len(p.key)))
	return subtle.ConstantTimeCompare(got, p.key) == 1, nil
}

// phcParams argon2id PHC 串自描述的参数、盐与密钥。
type phcParams struct {
	memoryKiB  uint32 // 内存开销,KiB
	iterations uint32 // 迭代轮数
	lanes      uint8  // 并行 lane 数
	salt       []byte // 盐
	key        []byte // 派生密钥,非空
}

// parsePHC 解析 argon2id PHC 串。
// @param phc 待解析哈希串
// @return 参数、盐与密钥
// @return 格式非法时的错误
func parsePHC(phc string) (phcParams, error) {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return phcParams{}, errors.New("密码哈希格式非法")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return phcParams{}, errors.New("密码哈希版本不支持")
	}
	var p phcParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memoryKiB, &p.iterations, &p.lanes); err != nil {
		return phcParams{}, errors.New("密码哈希参数非法")
	}
	var err error
	if p.salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return phcParams{}, errors.New("密码盐解码失败")
	}
	if p.key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil || len(p.key) == 0 {
		return phcParams{}, errors.New("密码密钥解码失败")
	}
	return p, nil
}
