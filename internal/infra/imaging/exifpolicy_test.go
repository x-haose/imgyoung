package imaging

import (
	"errors"
	"testing"
)

// TestValidatePolicy 表驱动验证档位校验:合法值通过,空值/乱填拒绝。
func TestValidatePolicy(t *testing.T) {
	cases := map[string]struct {
		policy EXIFPolicy
		wantOK bool
	}{
		"strip":       {PolicyStrip, true},
		"passthrough": {PolicyPassthrough, true},
		"空值":          {"", false},
		"spoof":       {PolicySpoof, true},
		"乱填":          {"bogus", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := ValidatePolicy(tc.policy)
			if tc.wantOK && err != nil {
				t.Fatalf("ValidatePolicy(%q) = %v, 期望通过", tc.policy, err)
			}
			if !tc.wantOK && !errors.Is(err, ErrUnknownPolicy) {
				t.Fatalf("ValidatePolicy(%q) = %v, 期望 ErrUnknownPolicy", tc.policy, err)
			}
		})
	}
}
