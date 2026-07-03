package imaging

import (
	"errors"
	"fmt"
)

// EXIFPolicy EXIF 处理档位。
// @Description: 枚举的单一真相在本包;配置校验、API 层解析、上传管道均引用
// 这里的常量与 ValidatePolicy,不得散落字符串字面量。
type EXIFPolicy string

// EXIF 档位取值;spoof 为实验特性,尚未实现,取值时校验失败。
const (
	// PolicyStrip 剥离全部元数据。
	PolicyStrip EXIFPolicy = "strip"
	// PolicyPassthrough 保留原样,不动任何元数据(全局默认档)。
	PolicyPassthrough EXIFPolicy = "passthrough"
)

// ErrUnknownPolicy EXIF 档位取值非法或尚未实现(如实验特性 spoof)。
var ErrUnknownPolicy = errors.New("imaging: 未知的 EXIF 策略")

// ValidatePolicy 校验档位为已实现的明确取值。
// @Description: 空值、实验特性 spoof 与乱填值一律返回 ErrUnknownPolicy——
// 不做隐式回退,档位决策必须由调用方显式完成(不静默降级)。
// @param p 待校验的档位
// @return 取值非法时的错误
func ValidatePolicy(p EXIFPolicy) error {
	switch p {
	case PolicyStrip, PolicyPassthrough:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnknownPolicy, p)
	}
}
