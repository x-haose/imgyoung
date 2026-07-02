// Package channel 实现渠道管理:配置构造、优先级选择、健康状态与副本编排
// (设计定稿·渠道管理)。
package channel

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rclone/rclone/fs/config/obscure"
)

// BuildRemote 由后端类型、参数与路径构造 rclone 连接字符串。
// @Description: 形式为 ":backend,k=v,k2='v2':path"(设计定稿·配置动态注入,
// 不依赖 rclone 配置文件);值含特殊字符时按 fspath 语法加引号并双写转义。
// 产物含凭证,严禁写入日志。
// @param driver rclone 后端名(local/s3/webdav 等)
// @param params 后端参数,可为 nil
// @param path 后端内路径
// @return 连接字符串
func BuildRemote(driver string, params map[string]string, path string) string {
	var b strings.Builder
	b.WriteString(":")
	b.WriteString(driver)
	// 参数按键排序,保证同一配置产出确定的字符串。
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(",")
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(quoteValue(params[k]))
	}
	b.WriteString(":")
	b.WriteString(path)
	return b.String()
}

// quoteValue 按 fspath 语法为参数值加引号:含分隔符或引号时用单引号包裹,
// 内部单引号双写转义(见 rclone fs/fspath 的状态机解析规则)。
// @param v 原始参数值
// @return 可安全嵌入连接字符串的值
func quoteValue(v string) string {
	if !strings.ContainsAny(v, `:,'"`) {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

// WrapCrypt 将内层连接字符串包进 rclone crypt 后端(渠道存储加密)。
// @Description: 文件名加密取 standard;password/password2 按 rclone 要求
// 经 obscure 编码(设计定稿·渠道存储加密:基于 rclone 原生 crypt)。
// @param inner 内层连接字符串
// @param password 内容加密密码(由主密钥派生)
// @param salt 文件名加密盐(password2,由主密钥派生)
// @return crypt 包裹后的连接字符串
// @return obscure 编码失败时的错误
func WrapCrypt(inner, password, salt string) (string, error) {
	pw, err := obscure.Obscure(password)
	if err != nil {
		return "", fmt.Errorf("编码 crypt 密码: %w", err)
	}
	pw2, err := obscure.Obscure(salt)
	if err != nil {
		return "", fmt.Errorf("编码 crypt 盐: %w", err)
	}
	return BuildRemote("crypt", map[string]string{
		"remote":              inner,
		"password":            pw,
		"password2":           pw2,
		"filename_encryption": "standard",
	}, ""), nil
}
