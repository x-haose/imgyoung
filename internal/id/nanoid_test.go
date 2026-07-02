package id

import (
	"strings"
	"testing"
)

// TestNewLength 验证生成的短 ID 长度符合约定(10 位)。
func TestNewLength(t *testing.T) {
	got, err := New()
	if err != nil {
		t.Fatalf("New() 返回错误: %v", err)
	}
	if len(got) != Length {
		t.Fatalf("短 ID 长度 = %d, 期望 %d (id=%q)", len(got), Length, got)
	}
}

// TestNewAlphabet 验证短 ID 只包含 URL 安全字符集 [0-9A-Za-z]。
func TestNewAlphabet(t *testing.T) {
	got, err := New()
	if err != nil {
		t.Fatalf("New() 返回错误: %v", err)
	}
	for _, c := range got {
		if !strings.ContainsRune(alphabet, c) {
			t.Fatalf("短 ID %q 含字符集之外的字符 %q", got, c)
		}
	}
}

// TestNewUniqueness 验证连续生成不出现重复(碰撞概率应远低于测试规模)。
func TestNewUniqueness(t *testing.T) {
	const n = 10000
	seen := make(map[string]struct{}, n)
	for range n {
		got, err := New()
		if err != nil {
			t.Fatalf("New() 返回错误: %v", err)
		}
		if _, dup := seen[got]; dup {
			t.Fatalf("生成 %d 个短 ID 出现重复: %q", n, got)
		}
		seen[got] = struct{}{}
	}
}
