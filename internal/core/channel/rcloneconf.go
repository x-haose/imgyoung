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
	"github.com/x-haose/imgyoung/store/rclone"
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
// @return 配置文件
// @return ErrChannelNotFound;配置里有 rclone 配置文件无法表达的值时的 ErrInvalidChannel;
// 读取、解密、密钥派生失败或后端不存在时的错误
func (m *Manager) RcloneConfig(ctx context.Context, channelID int64) (RcloneConfigFile, error) {
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
	if errors.Is(err, rclone.ErrUnrepresentable) {
		return RcloneConfigFile{}, fmt.Errorf("%w: %w", ErrInvalidChannel, err)
	}
	if err != nil {
		return RcloneConfigFile{}, fmt.Errorf("生成渠道 %d 的 rclone 配置: %w", channelID, err)
	}
	return RcloneConfigFile{Name: names.file, Content: rcloneConfigHeader(row, opts, names) + body}, nil
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

// rcloneConfigHeader 生成配置文件头的注释:渠道是谁、各远端是什么、数据在哪、怎么用。
// @param row 渠道记录
// @param opts 渠道的驱动构造参数
// @param names 远端名与文件名
// @return 以 # 开头的注释行,末尾空一行
func rcloneConfigHeader(row sqlc.Channel, opts rclone.Options, names rcloneNames) string {
	dataRoot := names.backend + ":" + opts.Path
	holds := "渠道凭证"
	usageRoot := dataRoot
	if opts.Crypt != nil {
		holds = "渠道凭证与解密密码"
		usageRoot = names.crypt + ":"
	}
	lines := []string{
		fmt.Sprintf("ImgYoung 渠道 %q(ID %d,创建于 %s UTC)的 rclone 配置。", row.Name, row.ID, row.CreatedAt),
		"文件含" + holds + ",与主密钥一样离线妥善保存。",
		"",
		fmt.Sprintf("[%s] 渠道后端(%s),参数与 ImgYoung 所用一致", names.backend, row.Driver),
	}
	if opts.Crypt != nil {
		lines = append(lines, fmt.Sprintf("[%s] 渠道的加密层,指向渠道存储路径 %s,经它读写的是明文", names.crypt, dataRoot))
	} else {
		lines = append(lines, "渠道数据位于 "+dataRoot)
	}
	lines = append(lines, "", "用法:",
		"  rclone --config "+names.file+" ls "+shellArg(usageRoot),
		"  rclone --config "+names.file+" copy "+shellArg(usageRoot)+" ./imgyoung-restore")
	if opts.Params[ConfigKeyToken] != "" {
		lines = append(lines, "",
			"授权令牌过期时 rclone 会刷新并写回本文件。Box 等刷新令牌只能用一次的服务商,",
			"本文件与仍在使用该渠道的 ImgYoung 会互相作废对方的令牌,不要同时使用。")
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
