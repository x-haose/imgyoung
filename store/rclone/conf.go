package rclone

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/fspath"
	"github.com/unknwon/goconfig"
)

// ErrUnrepresentable 参数值无法写进 rclone 配置文件:按 rclone 的读法读不回原值(含换行、首尾空白等)。
var ErrUnrepresentable = errors.New("参数值无法写进 rclone 配置文件")

// sectionTypeKey rclone 配置文件段内记录后端名的保留键。
const sectionTypeKey = "type"

// ConfigFile 生成与 Open(opts) 所构造驱动等价的 rclone 配置文件(rclone.conf)文本,标准 rclone 可直接使用。
// @Description: 后端段名为 remote,参数为驱动实际交给后端的参数(含本包补的默认值),空值省略——驱动与
// rclone 都把空值当作未设置;键 type 是段内记录后端名的保留键,没有任何后端以它为选项,驱动同样不会用到,
// 不写出。密码类参数按 Options 的约定本就是 rclone 编码产物,原样写出。opts.Crypt 非 nil 时再生成名为
// cryptRemote 的 crypt 段,指向 remote:opts.Path,参数与驱动的 crypt 层一致(密码与盐由本函数编码)。
// 文本由 rclone 读写配置文件所用的同一 INI 库序列化,写完按 rclone 的读法回读核对,读不回原值即报错,
// 不产出与驱动不等价的文件。
// @param opts 驱动构造参数;OnChange 不参与
// @param remote 后端段名
// @param cryptRemote crypt 段名;opts.Crypt 为 nil 时不用
// @return 配置文件文本
// @return 后端不存在、段名不合 rclone 命名规则、编码失败或参数值无法表达(ErrUnrepresentable)时的错误
func ConfigFile(opts Options, remote, cryptRemote string) (string, error) {
	if _, err := fs.Find(opts.Backend); err != nil {
		return "", fmt.Errorf("未知的 rclone 后端 %q", opts.Backend)
	}
	sections := []configSection{{name: remote, backend: opts.Backend, params: withDefaults(opts.Backend, opts.Params)}}
	if opts.Crypt != nil {
		params, err := cryptParams(remote+":"+opts.Path, opts.Crypt)
		if err != nil {
			return "", err
		}
		sections = append(sections, configSection{name: cryptRemote, backend: "crypt", params: params})
	}
	cf, err := goconfig.LoadFromReader(strings.NewReader(""))
	if err != nil {
		return "", fmt.Errorf("初始化配置文件: %w", err)
	}
	for i := range sections {
		if err := sections[i].writeTo(cf); err != nil {
			return "", err
		}
	}
	var buf bytes.Buffer
	if err := goconfig.SaveConfigData(cf, &buf); err != nil {
		return "", fmt.Errorf("序列化配置文件: %w", err)
	}
	text := buf.String()
	if err := verifyReadBack(text, sections); err != nil {
		return "", err
	}
	return text, nil
}

// configSection rclone 配置文件里的一段:一个远端。
type configSection struct {
	name    string            // 远端名,即段名
	backend string            // rclone 后端名,写作段内 type
	params  map[string]string // 后端参数;空值与保留键 type 不写出
}

// written 返回段内实际写出的参数:后端名在 type 键下,其余为非空且非保留键的参数。
// @return 键 → 值
func (s *configSection) written() map[string]string {
	out := map[string]string{sectionTypeKey: s.backend}
	for k, v := range s.params {
		if v != "" && k != sectionTypeKey {
			out[k] = v
		}
	}
	return out
}

// writeTo 把本段写进配置文件:type 在前,其余参数按键名排序,输出稳定。
// @param cf 配置文件
// @return 段名不合 rclone 命名规则时的错误
func (s *configSection) writeTo(cf *goconfig.ConfigFile) error {
	if err := fspath.CheckConfigName(s.name); err != nil {
		return fmt.Errorf("远端名 %q: %w", s.name, err)
	}
	params := s.written()
	cf.SetValue(s.name, sectionTypeKey, s.backend)
	for _, k := range slices.Sorted(maps.Keys(params)) {
		if k != sectionTypeKey {
			cf.SetValue(s.name, k, params[k])
		}
	}
	return nil
}

// verifyReadBack 按 rclone 读配置文件的方式回读文本,核对每段写出的参数都读回原值。
// @Description: rclone 的 INI 库读值时去首尾空白、按行切分、展开 %(name)s 引用,这些值写得进却读不回原样。
// 含换行的值会让整个文件解析失败,而解析错误带着出错那一行的原文(可能是凭证),故先逐值查出换行;
// 解析失败时也不带解析错误的原文。
// @param text 配置文件文本
// @param sections 写入的段
// @return 解析失败或某参数读不回原值(ErrUnrepresentable,只带段名与键名,不带值)时的错误
func verifyReadBack(text string, sections []configSection) error {
	for i := range sections {
		for k, v := range sections[i].written() {
			if strings.ContainsAny(v, "\r\n") {
				return fmt.Errorf("%w: 远端 %q 的参数 %q 含换行", ErrUnrepresentable, sections[i].name, k)
			}
		}
	}
	cf, err := goconfig.LoadFromReader(strings.NewReader(text))
	if err != nil {
		return fmt.Errorf("%w: 生成的文本按 rclone 的读法无法解析", ErrUnrepresentable)
	}
	for i := range sections {
		for k, v := range sections[i].written() {
			if got, err := cf.GetValue(sections[i].name, k); err != nil || got != v {
				return fmt.Errorf("%w: 远端 %q 的参数 %q", ErrUnrepresentable, sections[i].name, k)
			}
		}
	}
	return nil
}
