package imaging

import (
	"errors"
	"fmt"
	"slices"
)

// EXIFPolicy EXIF 处理档位。
// @Description: 枚举的单一真相在本包;配置校验、API 层解析、上传管道均引用
// 这里的常量与 ValidatePolicy,不得散落字符串字面量。
type EXIFPolicy string

// EXIF 档位取值。
const (
	// PolicyStrip 剥离全部元数据。
	PolicyStrip EXIFPolicy = "strip"
	// PolicyPassthrough 保留原样,不动任何元数据(全局默认档)。
	PolicyPassthrough EXIFPolicy = "passthrough"
	// PolicySpoof 剥离原有元数据,换成一份自洽的假机型、位置与拍摄时间,见 Spoof。
	PolicySpoof EXIFPolicy = "spoof"
)

// ErrUnknownPolicy EXIF 档位取值非法。
var ErrUnknownPolicy = errors.New("imaging: 未知的 EXIF 策略")

// Policies 返回已实现的档位。
// @return 档位切片(新分配,可自由修改)
func Policies() []EXIFPolicy {
	return []EXIFPolicy{PolicyStrip, PolicyPassthrough, PolicySpoof}
}

// ValidatePolicy 校验档位为已实现的明确取值。
// @Description: 空值与乱填值一律返回 ErrUnknownPolicy——
// 不做隐式回退,档位决策必须由调用方显式完成(不静默降级)。
// @param p 待校验的档位
// @return 取值非法时的错误
func ValidatePolicy(p EXIFPolicy) error {
	if slices.Contains(Policies(), p) {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrUnknownPolicy, p)
}
