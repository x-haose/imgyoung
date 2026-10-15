package channel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
	"github.com/x-haose/imgyoung/store/rclone"
)

// Language 服务端生成的说明文字(导出文件的注释等)所用的语言。
type Language string

// 支持的语言,取值与管理后台的界面语言代码一致。
const (
	LanguageChinese Language = "zh-CN" // 简体中文
	LanguageEnglish Language = "en"    // 英文
)

// RcloneConfigFile 渠道的 rclone 配置文件(rclone.conf)。
type RcloneConfigFile struct {
	Name    string // 文件名,形如 imgyoung-channel-3.rclone.conf;文件头的用法示例按此名引用
	Content string // 文件全文:说明注释 + 远端段
}

// RcloneConfig 生成渠道的 rclone 配置文件:标准 rclone 凭它即可脱离本服务读写渠道数据。
// @Description: 后端远端的参数与本服务驱动所用的完全一致,凭证与授权令牌在内;加密渠道另有一个 crypt 远端
// 指向渠道存储路径,密码与盐即本服务为该渠道派生的那一对。远端与文件以渠道 ID 命名,文件头注释说明各远端与用法。
// 文件里的凭证与解密密码与主密钥同等敏感。
// @param channelID 渠道主键
// @param lang 文件头注释的语言
// @return 配置文件
// @return ErrChannelNotFound;配置里有 rclone 配置文件无法表达的值时包裹 ErrInvalidChannel 的带错误码错误;
// 读取、解密、密钥派生失败或后端不存在时的错误
func (m *Manager) RcloneConfig(ctx context.Context, channelID int64, lang Language) (RcloneConfigFile, error) {
	row, err := m.cfg.Queries.GetChannel(ctx, channelID)
	if errors.Is(err, sql.ErrNoRows) {
		return RcloneConfigFile{}, ErrChannelNotFound
	}
	if err != nil {
		return RcloneConfigFile{}, fmt.Errorf("读取渠道 %d: %w", channelID, err)
	}
	params, err := m.unsealConfigMap(row.ConfigSealed)
	if err != nil {
		return RcloneConfigFile{}, err
	}
	opts, err := m.channelOptions(row, params)
	if err != nil {
		return RcloneConfigFile{}, err
	}
	names := rcloneNamesOf(row.ID)
	body, err := rclone.ConfigFile(opts, names.backend, names.crypt)
	if ue, ok := errors.AsType[*rclone.UnrepresentableError](err); ok {
		return RcloneConfigFile{}, errcode.Wrap(ErrInvalidChannel, errcode.ChannelUnrepresentable,
			"remote", ue.Remote, "key", ue.Key)
	}
	if err != nil {
		return RcloneConfigFile{}, fmt.Errorf("生成渠道 %d 的 rclone 配置: %w", channelID, err)
	}
	header := rcloneConfigHeader(headerTexts[lang], row, opts, names)
	return RcloneConfigFile{Name: names.file, Content: header + body}, nil
}

// rcloneNames 渠道导出的 rclone 配置里用到的名字。
type rcloneNames struct {
	backend string // 后端远端,形如 imgyoung-3
	crypt   string // crypt 远端,形如 imgyoung-3-crypt;仅加密渠道写出
	file    string // 配置文件名,形如 imgyoung-channel-3.rclone.conf
}

// rcloneNamesOf 按渠道 ID 给出名字:只含字母数字、连字符与点,远端名合 rclone 命名规则,同一渠道每次导出都相同。
// @param channelID 渠道主键
// @return 名字
func rcloneNamesOf(channelID int64) rcloneNames {
	backend := fmt.Sprintf("imgyoung-%d", channelID)
	return rcloneNames{
		backend: backend,
		crypt:   backend + "-crypt",
		file:    fmt.Sprintf("imgyoung-channel-%d.rclone.conf", channelID),
	}
}

// headerText 配置文件头注释的一种语言版本;%q、%d、%s 依次由 rcloneConfigHeader 填入。
type headerText struct {
	title      string   // 标题行:渠道名、ID、创建时刻
	holds      string   // 文件所含敏感内容与保管要求,%s 为 plainHolds 或 cryptHolds
	plainHolds string   // 未加密渠道的敏感内容
	cryptHolds string   // 加密渠道的敏感内容
	backend    string   // 后端远端说明:远端名、后端名
	crypt      string   // crypt 远端说明:远端名、数据根
	dataRoot   string   // 未加密渠道的数据位置
	usage      string   // 用法小标题
	tokenNote  []string // 持有授权令牌时附加的令牌刷新提醒,每项一行
}

// headerTexts 各语言的文件头注释。
var headerTexts = map[Language]headerText{
	LanguageChinese: {
		title:      "ImgYoung 渠道 %q(ID %d,创建于 %s UTC)的 rclone 配置。",
		holds:      "文件含%s,与主密钥一样离线妥善保存。",
		plainHolds: "渠道凭证",
		cryptHolds: "渠道凭证与解密密码",
		backend:    "[%s] 渠道后端(%s),参数与 ImgYoung 所用一致",
		crypt:      "[%s] 渠道的加密层,指向渠道存储路径 %s,经它读写的是明文",
		dataRoot:   "渠道数据位于 %s",
		usage:      "用法:",
		tokenNote: []string{
			"授权令牌过期时 rclone 会刷新并写回本文件。Box 等刷新令牌只能用一次的服务商,",
			"本文件与仍在使用该渠道的 ImgYoung 会互相作废对方的令牌,不要同时使用。",
		},
	},
	LanguageEnglish: {
		title:      "rclone configuration of ImgYoung channel %q (ID %d, created %s UTC).",
		holds:      "This file contains %s. Keep it offline and safe, like the master key.",
		plainHolds: "the channel credentials",
		cryptHolds: "the channel credentials and decryption passwords",
		backend:    "[%s] channel backend (%s), parameters identical to those ImgYoung uses",
		crypt: "[%s] encryption layer of the channel, pointing at the channel storage path %s; " +
			"data read or written through it is plaintext",
		dataRoot: "Channel data is at %s",
		usage:    "Usage:",
		tokenNote: []string{
			"rclone refreshes an expired access token and writes it back to this file. With providers whose refresh",
			"tokens work only once (Box and others), this file and an ImgYoung still using the channel invalidate",
			"each other's tokens; do not use both at the same time.",
		},
	},
}

// rcloneConfigHeader 生成配置文件头的注释:渠道是谁、各远端是什么、数据在哪、怎么用。
// @param text 注释所用语言的文本
// @param row 渠道记录
// @param opts 渠道的驱动构造参数
// @param names 远端名与文件名
// @return 以 # 开头的注释行,末尾空一行
func rcloneConfigHeader(text headerText, row sqlc.Channel, opts rclone.Options, names rcloneNames) string {
	dataRoot := names.backend + ":" + opts.Path
	holds := text.plainHolds
	usageRoot := dataRoot
	if opts.Crypt != nil {
		holds = text.cryptHolds
		usageRoot = names.crypt + ":"
	}
	lines := []string{
		fmt.Sprintf(text.title, row.Name, row.ID, row.CreatedAt),
		fmt.Sprintf(text.holds, holds),
		"",
		fmt.Sprintf(text.backend, names.backend, row.Driver),
	}
	if opts.Crypt != nil {
		lines = append(lines, fmt.Sprintf(text.crypt, names.crypt, dataRoot))
	} else {
		lines = append(lines, fmt.Sprintf(text.dataRoot, dataRoot))
	}
	lines = append(lines, "", text.usage,
		"  rclone --config "+names.file+" ls "+shellArg(usageRoot),
		"  rclone --config "+names.file+" copy "+shellArg(usageRoot)+" ./imgyoung-restore")
	if opts.Params[ConfigKeyToken] != "" {
		lines = append(append(lines, ""), text.tokenNote...)
	}
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(strings.TrimRight("# "+stripControl(line), " ") + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

// plainShellArg 无需引号即可原样作 shell 参数的字符集。
var plainShellArg = regexp.MustCompile(`^[\w./:@+=,-]+$`)

// shellArg 把命令行参数写成可直接粘贴进 POSIX shell 的形式:含其他字符时以单引号包裹。
// @param s 参数
// @return shell 形式的参数
func shellArg(s string) string {
	if plainShellArg.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// stripControl 去掉控制字符(含换行):注释行里出现换行会让其后的内容被 rclone 当作配置解析。
// @param s 原文
// @return 不含控制字符的文本
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
