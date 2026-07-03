// Package config 装配运行配置,优先级由低到高:内嵌默认 config.yml < 外部
// configs/config.yml < 环境变量(IMGYOUNG_*)。基于 koanf(设计定稿·依赖清单);
// 渠道等业务配置存 DB,本包只管进程级配置。
package config

import (
	"crypto/rand"
	_ "embed"
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

// defaultConfigYAML 内嵌的默认配置(config.yml),作为合并的最底层;
// 用文件而非 Go 字面量承载默认值,让默认既可读又随二进制自带、不依赖运行时文件。
//
//go:embed config.yml
var defaultConfigYAML []byte

// envPrefix 环境变量统一前缀,剥掉后小写即为配置键(IMGYOUNG_DATA_DIR → data_dir);
// koanf 的 env provider 据此自动映射,无需为每个键单列常量。
const envPrefix = "IMGYOUNG_"

// envConfig 配置文件路径变量;留空则自动发现工作目录下的 imgyoung.yml。
// 它只定位文件、不作为配置键参与合并,故单列。
const envConfig = envPrefix + "CONFIG"

// defaultConfigFile 自动发现的外部配置文件路径(相对工作目录),可选,覆盖内嵌默认。
const defaultConfigFile = "configs/config.yml"

// megapixel 一百万像素。
const megapixel = 1_000_000

// tokenHexLen 随机 Token 的十六进制字符长度(24 字节熵)。
const tokenHexLen = 48

// Config 进程级运行配置。字段经 koanf 标签直接从合并后的配置反序列化;
// 末两个 koanf:"-" 字段为派生量,由 max_upload_mb / max_pixels_mp 换算而来。
// 默认值见内嵌的 config.yml,不在此重复。
type Config struct {
	Listen               string             `koanf:"listen"`                 // HTTP 监听地址
	DataDir              string             `koanf:"data_dir"`               // 数据目录(SQLite 与本地渠道)
	Token                string             `koanf:"token"`                  // 上传鉴权 Token;为空则由 main 生成随机值
	MasterKey            string             `koanf:"master_key"`             // 主密钥(64 位 hex);为空则用数据目录文件
	MaxConcurrentUploads int                `koanf:"max_concurrent_uploads"` // 上传并发上限(设计定稿·内存防护)
	BaseURL              string             `koanf:"base_url"`               // 对外站点地址(去尾斜杠);空则返回相对路径
	RefererWhitelist     []string           `koanf:"referer_whitelist"`      // 防盗链白名单;空表示不启用
	AllowEmptyReferer    bool               `koanf:"allow_empty_referer"`    // 空 Referer 是否放行(设计定稿·防盗链)
	EXIFPolicy           imaging.EXIFPolicy `koanf:"exif_policy"`            // 全局默认 EXIF 档位,单次上传可覆盖;枚举见 imaging
	MinSuccess           int                `koanf:"min_success"`            // 上传返回成功所需的最少同步副本数
	AccessMode           string             `koanf:"access_mode"`            // 访问模式:proxy(代理)/ redirect(302 省流量)
	EncryptNewChannels   bool               `koanf:"encrypt_new_channels"`   // 新建渠道未显式指定 encrypted 时的默认值
	HealthCheckSeconds   int                `koanf:"health_check_seconds"`   // 渠道健康检查间隔(秒)
	BackupIntervalHours  int                `koanf:"backup_interval_hours"`  // 元数据库自动备份间隔(小时)
	BackupKeep           int                `koanf:"backup_keep"`            // 备份保留份数;丢失映射即全部不可反查,宁多勿少
	MaxUploadBytes       int64              `koanf:"-"`                      // 单图字节上限,由 max_upload_mb 换算
	MaxPixels            int64              `koanf:"-"`                      // 像素预算(w×h 上限),由 max_pixels_mp 换算
}

// Load 按"内嵌默认 → 外部配置文件 → 环境变量"三层合并出最终配置,非法值快速失败。
// @Description: 空字符串环境变量视为未设置;显式指定的配置文件不存在时报错,
// 自动发现的 configs/config.yml 不存在则静默跳过(内嵌默认已兜底)。
// @return 完整配置
// @return 配置文件读取失败或数值非法时的错误
func Load() (Config, error) {
	k := koanf.New(".")
	// 第一层:内嵌默认配置(config.yml)。
	base, err := yaml.Parser().Unmarshal(defaultConfigYAML)
	if err != nil {
		return Config{}, fmt.Errorf("解析内嵌默认配置: %w", err)
	}
	if err := k.Load(confmap.Provider(base, "."), nil); err != nil {
		return Config{}, fmt.Errorf("加载默认配置: %w", err)
	}

	// 第二层:可选外部 yaml 配置文件,覆盖内嵌默认。
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
