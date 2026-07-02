// Package config 装配运行配置,三层优先级:环境变量 > 配置文件(yaml)> 默认值。
// 基于 koanf(设计定稿·依赖清单);渠道等业务配置存 DB,本包只管进程级配置。
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"github.com/x-haose/imgyoung/internal/imaging"
)

// 环境变量名。
const (
	// envConfig 配置文件路径;留空则自动发现工作目录下的 imgyoung.yml。
	envConfig = "IMGYOUNG_CONFIG"
	// envListen HTTP 监听地址。
	envListen = "IMGYOUNG_LISTEN"
	// envDataDir 数据目录(SQLite 与本地渠道都在其下)。
	envDataDir = "IMGYOUNG_DATA_DIR"
	// envToken 上传 Token;留空则启动时生成随机值并打印。
	envToken = "IMGYOUNG_TOKEN"
	// envMaxUploadMB 单图上限(MB)。
	envMaxUploadMB = "IMGYOUNG_MAX_UPLOAD_MB"
	// envBaseURL 对外站点地址(生成绝对 URL 用,PicGo 等客户端需要)。
	envBaseURL = "IMGYOUNG_BASE_URL"
	// envMaxPixelsMP 像素预算(百万像素)。
	envMaxPixelsMP = "IMGYOUNG_MAX_PIXELS_MP"
	// envRefererWhitelist 防盗链白名单,逗号分隔,支持 *.example.com 通配。
	envRefererWhitelist = "IMGYOUNG_REFERER_WHITELIST"
	// envAllowEmptyReferer 空 Referer 是否放行。
	envAllowEmptyReferer = "IMGYOUNG_ALLOW_EMPTY_REFERER"
	// envEXIFPolicy 全局默认 EXIF 档位(strip / passthrough)。
	envEXIFPolicy = "IMGYOUNG_EXIF_POLICY"
)

// megapixel 一百万像素。
const megapixel = 1_000_000

// defaultConfigFile 自动发现的默认配置文件名(相对工作目录)。
const defaultConfigFile = "imgyoung.yml"

// tokenHexLen 随机 Token 的十六进制字符长度(24 字节熵)。
const tokenHexLen = 48

// Config 进程级运行配置。
type Config struct {
	// Listen HTTP 监听地址,默认 ":8080"。
	Listen string
	// DataDir 数据目录,默认 "data"。
	DataDir string
	// Token 上传鉴权 Token;为空表示需要生成。
	Token string
	// MaxUploadBytes 单图字节上限,默认 50MB。
	MaxUploadBytes int64
	// BaseURL 对外站点地址(无尾部斜杠);为空时上传响应返回相对路径。
	BaseURL string
	// MaxPixels 像素预算(width × height 上限),默认 50MP。
	MaxPixels int64
	// RefererWhitelist 防盗链白名单;为空表示不启用防盗链。
	RefererWhitelist []string
	// AllowEmptyReferer 空 Referer 是否放行,默认 true(设计定稿·防盗链)。
	AllowEmptyReferer bool
	// EXIFPolicy 全局默认 EXIF 档位,默认 passthrough(不动用户原图);
	// 单次上传可覆盖。枚举见 imaging 包。
	EXIFPolicy imaging.EXIFPolicy
}

// envPrefix 环境变量统一前缀,剥掉后小写即为配置键(IMGYOUNG_DATA_DIR → data_dir)。
const envPrefix = "IMGYOUNG_"

// Load 按"默认值 → 配置文件 → 环境变量"三层合并出最终配置,非法值快速失败。
// @Description: 空字符串环境变量视为未设置;显式指定的配置文件不存在时报错,
// 自动发现的 imgyoung.yml 不存在则静默跳过。
// @return 完整配置
// @return 配置文件读取失败或数值非法时的错误
func Load() (Config, error) {
	k := koanf.New(".")

	// 第一层:默认值。
	defaults := confmap.Provider(map[string]any{
		"listen":              ":8080",
		"data_dir":            "data",
		"token":               "",
		"max_upload_mb":       int64(50),
		"base_url":            "",
		"max_pixels_mp":       int64(50),
		"referer_whitelist":   []string{},
		"allow_empty_referer": true,
		"exif_policy":         "passthrough",
	}, ".")
	if err := k.Load(defaults, nil); err != nil {
		return Config{}, fmt.Errorf("加载默认配置: %w", err)
	}

	// 第二层:可选 yaml 配置文件。
	path := os.Getenv(envConfig)
	explicit := path != ""
	if !explicit {
		path = defaultConfigFile
	}
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		// 自动发现模式下文件缺失属正常;显式指定或解析失败必须报错。
		if explicit || !errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("读取配置文件 %q: %w", path, err)
		}
	}

	// 第三层:环境变量覆盖(空值视为未设置,IMGYOUNG_CONFIG 只定位文件不参与合并)。
	envProvider := env.ProviderWithValue(envPrefix, ".", func(key, value string) (string, any) {
		if value == "" || key == envConfig {
			return "", nil
		}
		name := strings.ToLower(strings.TrimPrefix(key, envPrefix))
		// 列表型变量:逗号分隔并去空白。
		if key == envRefererWhitelist {
			return name, splitCSV(value)
		}
		return name, value
	})
	if err := k.Load(envProvider, nil); err != nil {
		return Config{}, fmt.Errorf("读取环境变量: %w", err)
	}

	var raw struct {
		// Listen HTTP 监听地址。
		Listen string `koanf:"listen"`
		// DataDir 数据目录。
		DataDir string `koanf:"data_dir"`
		// Token 上传鉴权 Token。
		Token string `koanf:"token"`
		// MaxUploadMB 单图上限(MB)。
		MaxUploadMB int64 `koanf:"max_upload_mb"`
		// BaseURL 对外站点地址。
		BaseURL string `koanf:"base_url"`
		// MaxPixelsMP 像素预算(百万像素)。
		MaxPixelsMP int64 `koanf:"max_pixels_mp"`
		// RefererWhitelist 防盗链白名单。
		RefererWhitelist []string `koanf:"referer_whitelist"`
		// AllowEmptyReferer 空 Referer 是否放行。
		AllowEmptyReferer bool `koanf:"allow_empty_referer"`
		// EXIFPolicy 全局默认 EXIF 档位。
		EXIFPolicy string `koanf:"exif_policy"`
	}
	if err := k.Unmarshal("", &raw); err != nil {
		return Config{}, fmt.Errorf("解析配置: %w", err)
	}
	if raw.MaxUploadMB <= 0 {
		return Config{}, fmt.Errorf("max_upload_mb=%d 非法: 需要正整数(单位 MB)", raw.MaxUploadMB)
	}
	if raw.MaxPixelsMP <= 0 {
		return Config{}, fmt.Errorf("max_pixels_mp=%d 非法: 需要正整数(单位百万像素)", raw.MaxPixelsMP)
	}
	// 档位枚举的单一真相在 imaging 包;spoof 为实验特性,校验即拒绝、不静默降级。
	if err := imaging.ValidatePolicy(imaging.EXIFPolicy(raw.EXIFPolicy)); err != nil {
		return Config{}, fmt.Errorf("exif_policy 配置非法: %w", err)
	}
	return Config{
		Listen:            raw.Listen,
		DataDir:           raw.DataDir,
		Token:             raw.Token,
		MaxUploadBytes:    raw.MaxUploadMB << 20,
		BaseURL:           strings.TrimRight(raw.BaseURL, "/"),
		MaxPixels:         raw.MaxPixelsMP * megapixel,
		RefererWhitelist:  raw.RefererWhitelist,
		AllowEmptyReferer: raw.AllowEmptyReferer,
		EXIFPolicy:        imaging.EXIFPolicy(raw.EXIFPolicy),
	}, nil
}

// splitCSV 按逗号切分并去除各项首尾空白,丢弃空项。
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// GenerateToken 生成加密安全的随机上传 Token。
// @return 十六进制 Token 字符串(tokenHexLen 位)
// @return 随机源不可用时的错误
func GenerateToken() (string, error) {
	buf := make([]byte, tokenHexLen/2)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("读取随机源: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
