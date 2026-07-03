// Package id 提供对外短 ID 的生成(自实现 nanoid)。
package id

import (
	"crypto/rand"
	"fmt"
)

// Length 短 ID 的固定长度。
// @Description: 62^10 ≈ 8.4e17 个组合,对自部署规模碰撞概率可忽略;
// 对外 URL 的私密性不依赖 ID 熵。
const Length = 10

// alphabet 短 ID 字符集,仅含 URL 安全的字母数字,避免 -_ 以便双击全选。
const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// maxUnbiasedByte 拒绝采样上界:248 = 62*4,是不超过 256 的最大 62 倍数。
// 丢弃 ≥248 的字节可消除 b%62 的模偏差,保证字符均匀分布。
const maxUnbiasedByte = byte(len(alphabet) * (256 / len(alphabet)))

// New 生成一个新的对外短 ID。
// @Description: 基于 crypto/rand + 拒绝采样的均匀取字符,失败时返回错误(不降级到弱随机)。
// @return 生成的短 ID
// @return 随机源不可用时的错误
func New() (string, error) {
	buf := make([]byte, Length)
	out := make([]byte, 0, Length)
	for len(out) < Length {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("读取随机源: %w", err)
		}
		for _, b := range buf {
			if b >= maxUnbiasedByte {
				continue
			}
			out = append(out, alphabet[int(b)%len(alphabet)])
			if len(out) == Length {
				break
			}
		}
	}
	return string(out), nil
}
