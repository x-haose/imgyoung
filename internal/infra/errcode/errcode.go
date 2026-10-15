// Package errcode 面向调用方的错误码:接口错误信封里的 code 与管理后台词典共用这一套标识,
// 参数填进文案里的占位符;服务端只认中文文案,其它语言由界面按错误码与参数自行渲染。
package errcode

import (
	"errors"
	"strings"
)

// Code 错误码;取值即接口契约里 ErrorCode 枚举的成员。只能由本包登记产生,不存在没有文案的错误码。
type Code struct {
	id string // 错误码标识,如 channel_not_found
}

// String 返回错误码标识。
// @return 标识
func (c Code) String() string { return c.id }

// MarshalText 按标识序列化,错误码在 JSON 里是字符串。
// @return 标识的字节
// @return 恒为 nil
func (c Code) MarshalText() ([]byte, error) { return []byte(c.id), nil }

// catalog 错误码 → 中文文案模板,{name} 为参数占位符;由 define 在包初始化时填入。
var catalog = map[Code]string{}

// define 登记一个错误码及其中文文案模板。
// @param code 错误码
// @param message 中文文案模板,半角标点,{name} 占位符取自 Error.Params
// @return 错误码
func define(code, message string) Code {
	c := Code{id: code}
	if _, dup := catalog[c]; dup {
		panic("errcode: 错误码重复登记 " + code)
	}
	catalog[c] = message
	return c
}

// Codes 返回全部已登记的错误码,顺序不定。
// @return 错误码切片
func Codes() []Code {
	out := make([]Code, 0, len(catalog))
	for c := range catalog {
		out = append(out, c)
	}
	return out
}

// Error 带错误码的错误。
type Error struct {
	Code   Code              // 错误码
	Params map[string]string // 文案参数,键即文案模板里的占位符名;无参数时为 nil
	cause  error             // 被包裹的错误,供 errors.Is / errors.As 沿链判定;可为 nil
}

// New 创建带错误码的错误。
// @param code 错误码
// @param kv 文案参数,键值交替;个数须为偶数
// @return 错误
func New(code Code, kv ...string) *Error {
	return &Error{Code: code, Params: params(kv)}
}

// Wrap 创建包裹 cause 的带错误码错误;cause 通常是调用方据以分类的哨兵错误。
// @param cause 被包裹的错误
// @param code 错误码
// @param kv 文案参数,键值交替;个数须为偶数
// @return 错误
func Wrap(cause error, code Code, kv ...string) *Error {
	return &Error{Code: code, Params: params(kv), cause: cause}
}

// params 把键值交替的参数列表转为映射。
// @param kv 键值交替的参数
// @return 参数映射;无参数时为 nil
func params(kv []string) map[string]string {
	if len(kv)%2 != 0 {
		panic("errcode: 参数须键值成对")
	}
	if len(kv) == 0 {
		return nil
	}
	out := make(map[string]string, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		out[kv[i]] = kv[i+1]
	}
	return out
}

// Error 返回中文文案。
// @return 按错误码模板填好参数的文案
func (e *Error) Error() string {
	return Message(e.Code, e.Params)
}

// Unwrap 返回被包裹的错误。
// @return 被包裹的错误;无则为 nil
func (e *Error) Unwrap() error {
	return e.cause
}

// From 取出错误链上第一个带错误码的错误。
// @param err 任意错误
// @return 带错误码的错误与是否找到
func From(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// Message 渲染错误码的中文文案。
// @param code 错误码;零值是代码缺陷,直接 panic
// @param params 文案参数
// @return 填好参数的文案;模板里没有给值的占位符原样保留
func Message(code Code, params map[string]string) string {
	msg, ok := catalog[code]
	if !ok {
		panic("errcode: 未登记的错误码 " + code.id)
	}
	for k, v := range params {
		msg = strings.ReplaceAll(msg, "{"+k+"}", v)
	}
	return msg
}
