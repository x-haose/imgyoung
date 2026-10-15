package imaging

import (
	"fmt"
	"image"
	"math/bits"

	"github.com/corona10/goimagehash"
)

// PHash 计算图像的 64 位感知哈希,以元数据库存放的 int64 位模式返回。
// @Description: SQLite 的 INTEGER 是有符号 64 位,哈希按位原样存入,数值本身无意义,只用于汉明距离比较;
// 位模式与无符号哈希之间的转换全部收在本文件。
// @param img 已解码的图像
// @return 感知哈希的位模式
// @return 计算失败时的错误
func PHash(img image.Image) (int64, error) {
	ph, err := goimagehash.PerceptionHash(img)
	if err != nil {
		return 0, fmt.Errorf("计算感知哈希: %w", err)
	}
	return int64(ph.GetHash()), nil
}

// PHashDistance 两个感知哈希的汉明距离。
// @param a PHash 的产物
// @param b PHash 的产物
// @return 取值不同的位数,0 到 64
func PHashDistance(a, b int64) int {
	return bits.OnesCount64(uint64(a ^ b))
}

// PHashHex 感知哈希的十六进制文本。
// @param h PHash 的产物
// @return 16 位零填充的小写十六进制
func PHashHex(h int64) string {
	return fmt.Sprintf("%016x", uint64(h))
}
