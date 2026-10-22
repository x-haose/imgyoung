package signedurl

import (
	"net/url"
	"testing"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/secret"
)

func newSigner(t *testing.T) *Signer {
	t.Helper()
	master, err := secret.NewMasterKey()
	if err != nil {
		t.Fatalf("生成主密钥: %v", err)
	}
	s, err := New(master)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// signed 签发后拆出 exp 与 sig 参数。
func signed(t *testing.T, s *Signer, id string, epoch int64, expires time.Time) (exp, sig string) {
	t.Helper()
	v, err := url.ParseQuery(s.Query(id, epoch, expires))
	if err != nil {
		t.Fatalf("解析查询串: %v", err)
	}
	return v.Get(ParamExpires), v.Get(ParamSignature)
}

// TestVerify 覆盖验签的边界:到期时刻当秒有效、过一秒失效;图片、到期时刻、签名任一被改都失效;
// 到期时刻的非规范写法即便数值相同也失效;换一把主密钥签发的链接失效。
func TestVerify(t *testing.T) {
	s := newSigner(t)
	expires := time.Unix(1_800_000_000, 0)
	exp, sig := signed(t, s, "abc123", 3, expires)

	cases := map[string]struct {
		id, exp, sig string
		epoch        int64
		now          time.Time
		ok           bool
	}{
		"到期前":       {"abc123", exp, sig, 3, expires.Add(-time.Hour), true},
		"到期当秒":      {"abc123", exp, sig, 3, expires, true},
		"过期一秒":      {"abc123", exp, sig, 3, expires.Add(time.Second), false},
		"换了图片":      {"abc124", exp, sig, 3, expires.Add(-time.Hour), false},
		"改了到期时刻":    {"abc123", "1800000001", sig, 3, expires.Add(-time.Hour), false},
		"改了签名":      {"abc123", exp, sig[:len(sig)-1] + "A", 3, expires.Add(-time.Hour), false},
		"缺签名":       {"abc123", exp, "", 3, expires.Add(-time.Hour), false},
		"缺到期时刻":     {"abc123", "", sig, 3, expires.Add(-time.Hour), false},
		"到期时刻带前导加号": {"abc123", "+" + exp, sig, 3, expires.Add(-time.Hour), false},
		"到期时刻带前导零":  {"abc123", "0" + exp, sig, 3, expires.Add(-time.Hour), false},
		"代次变了":      {"abc123", exp, sig, 4, expires.Add(-time.Hour), false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := s.Verify(tc.id, tc.epoch, tc.exp, tc.sig, tc.now)
			if tc.ok != (err == nil) {
				t.Fatalf("Verify = %v, 期望通过=%v", err, tc.ok)
			}
		})
	}
	if err := newSigner(t).Verify("abc123", 3, exp, sig, expires.Add(-time.Hour)); err == nil {
		t.Fatal("另一把主密钥派生的签名器不应认这份签名")
	}
}
