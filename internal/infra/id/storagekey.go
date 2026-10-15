package id

import "strings"

// storageKeyHashLen 对象键里内容哈希的长度:sha256 小写十六进制的前 8 位。
const storageKeyHashLen = 8

// lowerHex 小写十六进制字符集。
const lowerHex = "0123456789abcdef"

// StorageKey 组成渠道内对象键 `{短 ID}_{sha256 前 8 位}`。
// @Description: 嵌入短 ID,元数据库全损时可从存储端反查对象属于哪张图;对账 GC 凭 IsStorageKey
// 认出本服务写出的对象。
// @param shortID 短 ID,New 的产物
// @param sha256Hex 对象内容 sha256 的小写十六进制,64 位
// @return 对象键
func StorageKey(shortID, sha256Hex string) string {
	return shortID + "_" + sha256Hex[:storageKeyHashLen]
}

// IsStorageKey 判断对象键是否符合 StorageKey 的格式;不符合的对象不是本服务写出的。
// @param key 渠道内对象键
// @return 符合为 true
func IsStorageKey(key string) bool {
	if len(key) != Length+1+storageKeyHashLen || key[Length] != '_' {
		return false
	}
	return onlyChars(key[:Length], alphabet) && onlyChars(key[Length+1:], lowerHex)
}

// onlyChars 判断 s 的每个字符都在 set 里。
// @param s 待查字符串
// @param set 允许的字符集
// @return 全部在内为 true
func onlyChars(s, set string) bool {
	for _, c := range s {
		if !strings.ContainsRune(set, c) {
			return false
		}
	}
	return true
}
