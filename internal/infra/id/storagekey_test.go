package id

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestIsStorageKey StorageKey 的产物总能被认出;长度、分隔符或字符集不符的键一律不认。
func TestIsStorageKey(t *testing.T) {
	sum := sha256.Sum256([]byte("image"))
	key := StorageKey(New(), hex.EncodeToString(sum[:]))
	if !IsStorageKey(key) {
		t.Fatalf("StorageKey 的产物 %q 未被认出", key)
	}
	for _, k := range []string{
		"",
		"photos/a.jpg",
		"_imgyoung/backup/20260101-000000.000.db",
		"AbCdEfGhIj_0123456",   // 哈希段少一位
		"AbCdEfGhIj_012345678", // 哈希段多一位
		"AbCdEfGhIj-01234567",  // 分隔符不是下划线
		"AbCdEfGh-j_01234567",  // 短 ID 段含字符集之外的字符
		"AbCdEfGhIj_0123456G",  // 哈希段不是十六进制
		"AbCdEfGhIj_ABCDEF01",  // 哈希段是大写十六进制
	} {
		if IsStorageKey(k) {
			t.Errorf("IsStorageKey(%q) = true, 期望 false", k)
		}
	}
}
