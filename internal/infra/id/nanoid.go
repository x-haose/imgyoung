// Package id 生成对外短 ID(NanoID:crypto/rand + 拒绝采样,均匀无偏)。
package id

import (
	gonanoid "github.com/jaevor/go-nanoid"
)

// Length 短 ID 的固定长度。
// @Description: 62^10 ≈ 8.4e17 个组合,对自部署规模碰撞概率可忽略;
// 对外 URL 的私密性不依赖 ID 熵。
const Length = 10

// alphabet 短 ID 字符集,仅含 URL 安全的字母数字,避免 -_ 以便双击全选。
const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// generate 预构建的定长生成器,进程内复用。
// @Description: alphabet 与 Length 均为合法常量,故用 Must 变体——非法配置在包初始化即
// panic(等价编译期保证)。底层 crypto/rand 失败由运行时兜底(直接终止而非降级弱随机),
// 故生成本身无错误路径。
var generate = gonanoid.MustCustomASCII(alphabet, Length)

// New 生成一个新的对外短 ID。
// @return 生成的短 ID
func New() string {
	return generate()
}
