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

// envPrefix 环境变量统一前缀,剥掉后小写即为配置键(IMGYOUNG_DATA_DIR → data_dir);
// koanf 的 env provider 据此自动映射,无需为每个键单列常量。
const envPrefix = "IMGYOUNG_"

// envConfig 配置文件路径变量;留空则自动发现工作目录下的 imgyoung.yml。
// 它只定位文件、不作为配置键参与合并,故单列。
const envConfig = envPrefix + "CONFIG"

// defaultConfigFile 自动发现的默认配置文件名(相对工作目录)。
const defaultConfigFile = "imgyoung.yml"

// megapixel 一百万像素。
const megapixel = 1_000_000

// tokenHexLen 随机 Token 的十六进制字符长度(24 字节熵)。
const tokenHexLen = 48

// Config 进程级运行配置。字段经 koanf 标签直接从合并后的配置反序列化;
// 末两个 koanf:"-" 字段为派生量,由 max_upload_mb / max_pixels_mp 换算而来。
type Config struct {
	// Listen HTTP 监听地址,默认 ":8080"。
	Listen string `koanf:"listen"`
	// DataDir 数据目录,默认 "data"。
	DataDir string `koanf:"data_dir"`
	// Token 上传鉴权 Token;为空表示需要 main 生成随机值。
	Token string `koanf:"token"`
	// MasterKey 主密钥(64 位 hex);为空表示使用数据目录文件(首启生成)。
	MasterKey string `koanf:"master_key"`
	// MaxConcurrentUploads 上传并发上限,默认 8(设计定稿·内存防护)。
	MaxConcurrentUploads int `koanf:"max_concurrent_uploads"`
	// BaseURL 对外站点地址(去尾部斜杠);为空时上传响应返回相对路径。
	BaseURL string `koanf:"base_url"`
	// RefererWhitelist 防盗链白名单;为空表示不启用防盗链。
	RefererWhitelist []string `koanf:"referer_whitelist"`
	// AllowEmptyReferer 空 Referer 是否放行,默认 true(设计定稿·防盗链)。
	AllowEmptyReferer bool `koanf:"allow_empty_referer"`
	// EXIFPolicy 全局默认 EXIF 档位,默认 passthrough(不动用户原图);
	// 单次上传可覆盖。枚举见 imaging 包。
	EXIFPolicy imaging.EXIFPolicy `koanf:"exif_policy"`
	// MinSuccess 上传返回成功所需的最少同步副本数,默认 1。
	MinSuccess int `koanf:"min_success"`
	// AccessMode 访问模式:proxy(默认,代理)/ redirect(302 省流量)。
	AccessMode string `koanf:"access_mode"`
	// EncryptNewChannels 新建渠道未显式指定 encrypted 时的默认值,默认 false。
	EncryptNewChannels bool `koanf:"encrypt_new_channels"`
	// HealthCheckSeconds 渠道健康检查间隔(秒),默认 60。
	HealthCheckSeconds int `koanf:"health_check_seconds"`
	// BackupIntervalHours 元数据库自动备份间隔(小时),默认 24。
	BackupIntervalHours int `koanf:"backup_interval_hours"`
	// BackupKeep 备份保留份数,默认 7;丢失映射即全部不可反查,宁多勿少。
	BackupKeep int `koanf:"backup_keep"`
	// MaxUploadBytes 单图字节上限,由 max_upload_mb 换算,默认 50MB。
	MaxUploadBytes int64 `koanf:"-"`
	// MaxPixels 像素预算(width × height 上限),由 max_pixels_mp 换算,默认 50MP。
	MaxPixels int64 `koanf:"-"`
}

// defaults 各配置键的默认值;键与 Config 的 koanf 标签一致,
// max_upload_mb / max_pixels_mp 不直接绑定字段,仅供派生量换算。
var defaults = map[string]any{
	"listen":                 ":8080",
	"data_dir":               "data",
	"token":                  "",
	"master_key":             "",
	"max_upload_mb":          int64(50),
	"max_concurrent_uploads": 8,
	"base_url":               "",
	"max_pixels_mp":          int64(50),
	"referer_whitelist":      []string{},
	"allow_empty_referer":    true,
	"exif_policy":            "passthrough",
	"min_success":            1,
	"access_mode":            "proxy",
	"encrypt_new_channels":   false,
	"health_check_seconds":   60,
	"backup_interval_hours":  24,
	"backup_keep":            7,
}

// Load 按"默认值 → 配置文件 → 环境变量"三层合并出最终配置,非法值快速失败。
// @Description: 空字符串环境变量视为未设置;显式指定的配置文件不存在时报错,
// 自动发现的 imgyoung.yml 不存在则静默跳过。
// @return 完整配置
// @return 配置文件读取失败或数值非法时的错误
func Load() (Config, error) {
	k := koanf.New(".")
	if err := k.Load(confmap.Provider(defaults, "."), nil); err != nil {
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
	provider := env.ProviderWithValue(envPrefix, ".", func(key, value string) (string, any) {
		name := strings.ToLower(strings.TrimPrefix(key, envPrefix))
		if value == "" || name == "config" {
			return "", nil
		}
		if name == "referer_whitelist" {
			return name, splitCSV(value)
		}
		return name, value
	})
	if err := k.Load(provider, nil); err != nil {
		return Config{}, fmt.Errorf("读取环境变量: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return Config{}, fmt.Errorf("解析配置: %w", err)
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	cfg.MaxUploadBytes = k.Int64("max_upload_mb") << 20
	cfg.MaxPixels = k.Int64("max_pixels_mp") * megapixel
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate 校验数值边界与枚举,首个非法项即返回(设计定稿·输入验证)。
// @Description: 单位化字段的错误按原始单位(MB / 百万像素)提示,便于用户定位。
// @return 非法配置的错误;全部合法时为 nil
func (c Config) validate() error {
	switch {
	case c.MaxUploadBytes <= 0:
		return fmt.Errorf("max_upload_mb=%d 非法: 需要正整数(单位 MB)", c.MaxUploadBytes>>20)
	case c.MaxConcurrentUploads <= 0:
		return fmt.Errorf("max_concurrent_uploads=%d 非法: 需要正整数", c.MaxConcurrentUploads)
	case c.MaxPixels <= 0:
		return fmt.Errorf("max_pixels_mp=%d 非法: 需要正整数(单位百万像素)", c.MaxPixels/megapixel)
	case c.MinSuccess < 1:
		return fmt.Errorf("min_success=%d 非法: 需要正整数", c.MinSuccess)
	case c.AccessMode != "proxy" && c.AccessMode != "redirect":
		return fmt.Errorf("access_mode=%q 非法: 可选 proxy / redirect", c.AccessMode)
	case c.HealthCheckSeconds < 1:
		return fmt.Errorf("health_check_seconds=%d 非法: 需要正整数", c.HealthCheckSeconds)
	case c.BackupIntervalHours < 1:
		return fmt.Errorf("backup_interval_hours=%d 非法: 需要正整数", c.BackupIntervalHours)
	case c.BackupKeep < 1:
		return fmt.Errorf("backup_keep=%d 非法: 需要正整数", c.BackupKeep)
	}
	// 档位枚举的单一真相在 imaging 包;spoof 为实验特性,校验即拒绝、不静默降级。
	if err := imaging.ValidatePolicy(c.EXIFPolicy); err != nil {
		return fmt.Errorf("exif_policy 配置非法: %w", err)
	}
	return nil
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
