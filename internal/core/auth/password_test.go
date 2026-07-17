package auth

import (
	"strings"
	"testing"
)

// TestPasswordRoundtrip 正确密码校验通过,错误密码拒绝,两次哈希互不相同。
func TestPasswordRoundtrip(t *testing.T) {
	phc, err := HashPassword("s3cret-密码")
	if err != nil {
		t.Fatalf("哈希失败: %v", err)
	}
	if !strings.HasPrefix(phc, "$argon2id$") {
		t.Fatalf("PHC 前缀异常: %s", phc)
	}
	if ok, err := VerifyPassword(phc, "s3cret-密码"); err != nil || !ok {
		t.Fatalf("正确密码应通过: ok=%v err=%v", ok, err)
	}
	if ok, _ := VerifyPassword(phc, "wrong"); ok {
		t.Fatal("错误密码不应通过")
	}
	phc2, err := HashPassword("s3cret-密码")
	if err != nil {
		t.Fatalf("二次哈希失败: %v", err)
	}
	if phc == phc2 {
		t.Fatal("两次哈希应因随机盐而不同")
	}
}

// TestNewRandomPassword 生成的初始密码非空、够长、两次互不相同,且可被正常哈希校验。
func TestNewRandomPassword(t *testing.T) {
	p1, err := NewRandomPassword()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(p1) < 16 {
		t.Fatalf("初始密码过短: %q(%d 字符)", p1, len(p1))
	}
	p2, err := NewRandomPassword()
	if err != nil {
		t.Fatalf("二次生成失败: %v", err)
	}
	if p1 == p2 {
		t.Fatal("两次生成的初始密码不应相同")
	}
	phc, err := HashPassword(p1)
	if err != nil {
		t.Fatalf("哈希初始密码: %v", err)
	}
	if ok, err := VerifyPassword(phc, p1); err != nil || !ok {
		t.Fatalf("初始密码应可校验通过: ok=%v err=%v", ok, err)
	}
}

// TestVerifyPasswordMalformed 非法 PHC 串一律报错而非误判通过。
func TestVerifyPasswordMalformed(t *testing.T) {
	cases := map[string]string{
		"空串":     "",
		"非argon": "$bcrypt$v=19$m=1,t=1,p=1$AAAA$BBBB",
		"缺段":     "$argon2id$v=19$m=65536,t=3,p=1$AAAA",
		"坏盐":     "$argon2id$v=19$m=65536,t=3,p=1$!!!!$AAAA",
		"坏参数":    "$argon2id$v=19$m=x,t=3,p=1$AAAA$BBBB",
	}
	for name, phc := range cases {
		if ok, err := VerifyPassword(phc, "pw"); err == nil || ok {
			t.Errorf("%s: 应报格式错误, got ok=%v err=%v", name, ok, err)
		}
	}
}
