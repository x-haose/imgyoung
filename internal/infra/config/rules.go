package config

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// integer 运营配置里出现的整数类型。
type integer interface {
	~int | ~int64
}

// settingError 构造一条配置取值非法的错误,文案参数带上配置键与取值。
// @param code 错误码
// @param key 配置键
// @param value 取值
// @param kv 其余文案参数,键值交替
// @return 错误
func settingError[T integer](code errcode.Code, key string, value T, kv ...string) error {
	return errcode.New(code, append([]string{"key", key, "value", strconv.FormatInt(int64(value), 10)}, kv...)...)
}

// positive 校验取值为正整数。
// @param key 配置键
// @param v 取值
// @return 非法时的错误
func positive[T integer](key string, v T) error {
	if v < 1 {
		return settingError(errcode.SettingPositive, key, v)
	}
	return nil
}

// nonNegative 校验取值不为负。
// @param key 配置键
// @param v 取值
// @return 非法时的错误
func nonNegative[T integer](key string, v T) error {
	if v < 0 {
		return settingError(errcode.SettingNonNegative, key, v)
	}
	return nil
}

// atMost 校验取值不超过上限。
// @param key 配置键
// @param v 取值
// @param limit 上限(含)
// @return 非法时的错误
func atMost[T integer](key string, v, limit T) error {
	if v > limit {
		return settingError(errcode.SettingTooLarge, key, v, "max", strconv.FormatInt(int64(limit), 10))
	}
	return nil
}

// between 校验取值落在闭区间内。
// @param key 配置键
// @param v 取值
// @param lo 下限(含)
// @param hi 上限(含)
// @return 非法时的错误
func between[T integer](key string, v, lo, hi T) error {
	if v < lo || v > hi {
		return settingError(errcode.SettingRange, key, v,
			"min", strconv.FormatInt(int64(lo), 10), "max", strconv.FormatInt(int64(hi), 10))
	}
	return nil
}

// oneOf 校验取值属于可选值之一。
// @param key 配置键
// @param v 取值
// @param options 可选值
// @return 非法时的错误
func oneOf[T ~string](key string, v T, options []T) error {
	if slices.Contains(options, v) {
		return nil
	}
	names := make([]string, len(options))
	for i, o := range options {
		names[i] = string(o)
	}
	return errcode.New(errcode.SettingOption, "key", key, "value", fmt.Sprint(v), "options", strings.Join(names, " / "))
}

// firstError 返回第一个非 nil 的错误。
// @param errs 依次求值的校验结果
// @return 第一个错误;全部为 nil 时为 nil
func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
