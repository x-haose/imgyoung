package rclone

import (
	"fmt"
	"slices"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/obscure"
)

// SensitiveParams 返回后端声明为密码或敏感信息的参数名,按名排序、无重复。
// @Description: 判定依据是后端选项元数据的 IsPassword 与 Sensitive,与 rclone config redacted 的口径一致:
// 除密码、密钥与 OAuth 令牌外,访问密钥 ID、账号名这类身份信息也在其中。不是后端选项的键不在返回值里。
// 调用方据此决定回显时掩码哪些值、从错误文案里抹除哪些值。
// @param backend rclone 后端名
// @return 参数名
// @return 后端不存在时的错误
func SensitiveParams(backend string) ([]string, error) {
	ri, err := fs.Find(backend)
	if err != nil {
		return nil, fmt.Errorf("未知的 rclone 后端 %q", backend)
	}
	var names []string
	for _, opt := range ri.Options {
		if opt.IsPassword || opt.Sensitive {
			names = append(names, opt.Name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names), nil
}

// ObscureSecrets 把后端声明为密码类的参数值按 rclone 要求编码(原地修改 params)。
// @Description: 判定依据取自后端自身的选项元数据,新增模板无需逐个标注;漏编码的密码会被
// 后端当作已编码值解出乱码,表现为凭证正确却认证失败。适用于值确定是明文的场景(表单字段)。
// @param backend rclone 后端名
// @param params 后端参数
// @return 后端不存在或编码失败时的错误
func ObscureSecrets(backend string, params map[string]string) error {
	return obscureMatching(backend, params, func(string) bool { return true })
}

// ObscurePlainSecrets 同 ObscureSecrets,但已是编码产物的值原样保留。
// @Description: "已编码"按 rclone 自己的规则判定(能 Reveal 即是),与 rclone config create
// 不带 --obscure/--no-obscure 时一致,直接粘贴 rclone.conf 里的值因此也能用。代价是极少数
// 恰好形似编码产物的明文密码会被放过,表现为认证失败;这类值先行 rclone obscure 即可。
// 适用于来源不明的场景(直填 rclone 参数)。
// @param backend rclone 后端名
// @param params 后端参数
// @return 后端不存在或编码失败时的错误
func ObscurePlainSecrets(backend string, params map[string]string) error {
	return obscureMatching(backend, params, func(v string) bool {
		_, err := obscure.Reveal(v)
		return err != nil
	})
}

// obscureMatching 对密码类参数中 isPlain 判定为明文的值编码。
// @param backend rclone 后端名
// @param params 后端参数
// @param isPlain 判定值是否为待编码明文
// @return 后端不存在或编码失败时的错误
func obscureMatching(backend string, params map[string]string, isPlain func(string) bool) error {
	ri, err := fs.Find(backend)
	if err != nil {
		return fmt.Errorf("未知的 rclone 后端 %q", backend)
	}
	for k, v := range params {
		opt := ri.Options.Get(k)
		if opt == nil || !opt.IsPassword || v == "" || !isPlain(v) {
			continue
		}
		enc, err := obscure.Obscure(v)
		if err != nil {
			return fmt.Errorf("编码 %q: %w", k, err)
		}
		params[k] = enc
	}
	return nil
}
