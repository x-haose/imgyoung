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
)

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
		"listen":        ":8080",
		"data_dir":      "data",
		"token":         "",
		"max_upload_mb": int64(50),
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
		return strings.ToLower(strings.TrimPrefix(key, envPrefix)), value
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
	}
	if err := k.Unmarshal("", &raw); err != nil {
		return Config{}, fmt.Errorf("解析配置: %w", err)
	}
	if raw.MaxUploadMB <= 0 {
		return Config{}, fmt.Errorf("max_upload_mb=%d 非法: 需要正整数(单位 MB)", raw.MaxUploadMB)
	}
	return Config{
		Listen:         raw.Listen,
		DataDir:        raw.DataDir,
		Token:          raw.Token,
		MaxUploadBytes: raw.MaxUploadMB << 20,
	}, nil
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
