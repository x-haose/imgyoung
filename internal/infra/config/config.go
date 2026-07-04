// Package config 装配运行配置,优先级由低到高:内嵌默认 config.yml < 外部
// configs/config.yml < 环境变量(IMGYOUNG_*)。基于 koanf;
// 渠道等业务配置存 DB,本包只管进程级配置。
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"github.com/x-haose/imgyoung/internal/infra/imaging"
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

// Config 进程级运行配置。字段经 koanf 标签直接从合并后的配置反序列化;
// 末两个 koanf:"-" 字段为派生量,由 max_upload_mb / max_pixels_mp 换算而来。
// 默认值见内嵌的 config.yml,不在此重复。
type Config struct {
	Listen               string             `koanf:"listen"`                 // HTTP 监听地址
	DataDir              string             `koanf:"data_dir"`               // 数据目录(SQLite 与本地渠道)
	Token                string             `koanf:"token"`                  // 上传鉴权 Token,必填;未配置则启动失败
	MasterKey            string             `koanf:"master_key"`             // 主密钥(64 位 hex);为空则用数据目录文件
	MaxConcurrentUploads int                `koanf:"max_concurrent_uploads"` // 上传并发上限
	BaseURL              string             `koanf:"base_url"`               // 对外站点地址(去尾斜杠);空则返回相对路径
	RefererWhitelist     []string           `koanf:"referer_whitelist"`      // 防盗链白名单;空表示不启用
	AllowEmptyReferer    bool               `koanf:"allow_empty_referer"`    // 空 Referer 是否放行
	EXIFPolicy           imaging.EXIFPolicy `koanf:"exif_policy"`            // 全局默认 EXIF 档位,单次上传可覆盖;枚举见 imaging
	MinSuccess           int                `koanf:"min_success"`            // 上传返回成功所需的最少同步副本数
	AccessMode           string             `koanf:"access_mode"`            // 访问模式:proxy(代理)/ redirect(302 省流量)
	EncryptNewChannels   bool               `koanf:"encrypt_new_channels"`   // 新建渠道未显式指定 encrypted 时的默认值
	HealthCheckSeconds   int                `koanf:"health_check_seconds"`   // 渠道健康检查间隔(秒)
	BackupIntervalHours  int                `koanf:"backup_interval_hours"`  // 元数据库自动备份间隔(小时)
	BackupKeep           int                `koanf:"backup_keep"`            // 备份保留份数;丢失映射即全部不可反查,宁多勿少
	CleanupSweepSeconds  int                `koanf:"cleanup_sweep_seconds"`  // 生命周期清理扫描间隔(秒)
	DeleteGraceMinutes   int                `koanf:"delete_grace_minutes"`   // 引用归零到物理删的宽限(分钟),可为 0
	DefaultTTLHours      int                `koanf:"default_ttl_hours"`      // 全局默认 TTL(小时),0 = 永不过期
	ActivityFlushSeconds int                `koanf:"activity_flush_seconds"` // 访问活跃度批量落库间隔(秒)
	ColdCleanupDays      int                `koanf:"cold_cleanup_days"`      // 冷图待清理阈值(天),0 = 不启用
	UsageReportSeconds   int                `koanf:"usage_report_seconds"`   // 渠道用量上报间隔(秒)
	ChannelQuotaGB       int                `koanf:"channel_quota_gb"`       // 单渠道用量告警阈值(GB),0 = 不告警
	DedupScanSeconds     int                `koanf:"dedup_scan_seconds"`     // 后台精确查重扫描间隔(秒)
	ProcessQualityWebP   int                `koanf:"process_quality_webp"`   // 按需处理 webp 质量档[0,100]
	ProcessQualityJPEG   int                `koanf:"process_quality_jpeg"`   // 按需处理 jpeg 质量档[1,100]
	ProcessWebPMethod    int                `koanf:"process_webp_method"`    // webp 质量/速度权衡[0,6]
	ProcessWorkers       int                `koanf:"process_workers"`        // 处理并发上限(信号量)
	ProcessTimeoutMS     int                `koanf:"process_timeout_ms"`     // 单请求处理超时(毫秒)
	VariantsCacheMB      int64              `koanf:"variants_cache_mb"`      // variant 磁盘缓存上限(MB),超则 LRU 淘汰
	MaxUploadBytes       int64              `koanf:"-"`                      // 单图字节上限,由 max_upload_mb 换算
	MaxPixels            int64              `koanf:"-"`                      // 像素预算(w×h 上限),由 max_pixels_mp 换算
	ProcessWidths        []int              `koanf:"-"`                      // 允许的宽度档白名单,由 process_widths 读入
	ProcessFormats       []string           `koanf:"-"`                      // 允许的目标格式白名单,由 process_formats 读入
}

// Load 按"内嵌默认 → 外部配置文件 → 环境变量"三层合并出最终配置,非法值快速失败。
// @Description: 空字符串环境变量视为未设置;显式指定的配置文件不存在时报错,
// 自动发现的 configs/config.yml 不存在则静默跳过(内嵌默认已兜底)。
// @return 完整配置
// @return 配置文件读取失败或数值非法时的错误
func Load() (Config, error) {
	k := koanf.New(".")
	if err := loadDefaults(k); err != nil {
		return Config{}, err
	}
	if err := loadFile(k); err != nil {
		return Config{}, err
	}
	if err := k.Load(envProvider(), nil); err != nil {
		return Config{}, fmt.Errorf("读取环境变量: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return Config{}, fmt.Errorf("解析配置: %w", err)
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	cfg.MaxUploadBytes = k.Int64("max_upload_mb") << 20
	cfg.MaxPixels = k.Int64("max_pixels_mp") * megapixel
	// 列表型档位单列读取:yaml 原生列表由 koanf 保型,逗号环境变量已在 envProvider 预切成 []int/[]string;
	// 这里 Ints/Strings 只是从已定型的切片取值,不做跨类型强转。
	cfg.ProcessWidths = k.Ints("process_widths")
	cfg.ProcessFormats = k.Strings("process_formats")
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// loadDefaults 把内嵌的 config.yml 作为最底层默认加载进 k。
// @return 内嵌配置解析或加载失败时的错误(属打包问题,正常不会发生)
func loadDefaults(k *koanf.Koanf) error {
	base, err := yaml.Parser().Unmarshal(defaultConfigYAML)
	if err != nil {
		return fmt.Errorf("解析内嵌默认配置: %w", err)
	}
	if err := k.Load(confmap.Provider(base, "."), nil); err != nil {
		return fmt.Errorf("加载默认配置: %w", err)
	}
	return nil
}

// loadFile 把可选的外部 yaml 配置叠加到 k(覆盖内嵌默认)。
// @Description: IMGYOUNG_CONFIG 显式指定则文件必须存在;否则自动发现
// configs/config.yml,缺失属正常、静默跳过(内嵌默认已兜底)。
// @return 显式文件缺失或任一文件解析失败时的错误
func loadFile(k *koanf.Koanf) error {
	path := os.Getenv(envConfig)
	explicit := path != ""
	if !explicit {
		path = defaultConfigFile
	}
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		if explicit || !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("读取配置文件 %q: %w", path, err)
		}
	}
	return nil
}

// envProvider 构造 IMGYOUNG_ 前缀的环境变量覆盖层。
// @Description: 键名剥前缀小写即配置键;空值视为未设置(不覆盖下层),
// IMGYOUNG_CONFIG 只定位文件不参与合并,列表型键按逗号切分。
// @return koanf 环境变量 provider
func envProvider() koanf.Provider {
	return env.ProviderWithValue(envPrefix, ".", func(key, value string) (string, any) {
		name := strings.ToLower(strings.TrimPrefix(key, envPrefix))
		if value == "" || name == "config" {
			return "", nil
		}
		if name == "process_widths" {
			return name, splitCommaInts(value)
		}
		if name == "referer_whitelist" || name == "process_formats" {
			return name, splitCommaList(value)
		}
		return name, value
	})
}

// validate 校验数值边界与枚举,首个非法项即返回。
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
	case c.CleanupSweepSeconds < 1:
		return fmt.Errorf("cleanup_sweep_seconds=%d 非法: 需要正整数", c.CleanupSweepSeconds)
	case c.DeleteGraceMinutes < 0:
		return fmt.Errorf("delete_grace_minutes=%d 非法: 不可为负", c.DeleteGraceMinutes)
	case c.DefaultTTLHours < 0:
		return fmt.Errorf("default_ttl_hours=%d 非法: 不可为负", c.DefaultTTLHours)
	case c.ActivityFlushSeconds < 1:
		return fmt.Errorf("activity_flush_seconds=%d 非法: 需要正整数", c.ActivityFlushSeconds)
	case c.ColdCleanupDays < 0:
		return fmt.Errorf("cold_cleanup_days=%d 非法: 不可为负", c.ColdCleanupDays)
	case c.UsageReportSeconds < 1:
		return fmt.Errorf("usage_report_seconds=%d 非法: 需要正整数", c.UsageReportSeconds)
	case c.ChannelQuotaGB < 0:
		return fmt.Errorf("channel_quota_gb=%d 非法: 不可为负", c.ChannelQuotaGB)
	case c.DedupScanSeconds < 1:
		return fmt.Errorf("dedup_scan_seconds=%d 非法: 需要正整数", c.DedupScanSeconds)
	case c.ProcessQualityWebP < 0 || c.ProcessQualityWebP > 100:
		return fmt.Errorf("process_quality_webp=%d 非法: 需要 [0,100]", c.ProcessQualityWebP)
	case c.ProcessQualityJPEG < 1 || c.ProcessQualityJPEG > 100:
		return fmt.Errorf("process_quality_jpeg=%d 非法: 需要 [1,100]", c.ProcessQualityJPEG)
	case c.ProcessWebPMethod < 0 || c.ProcessWebPMethod > 6:
		return fmt.Errorf("process_webp_method=%d 非法: 需要 [0,6]", c.ProcessWebPMethod)
	case c.ProcessWorkers < 1:
		return fmt.Errorf("process_workers=%d 非法: 需要正整数", c.ProcessWorkers)
	case c.ProcessTimeoutMS < 1:
		return fmt.Errorf("process_timeout_ms=%d 非法: 需要正整数", c.ProcessTimeoutMS)
	case c.VariantsCacheMB < 1:
		return fmt.Errorf("variants_cache_mb=%d 非法: 需要正整数", c.VariantsCacheMB)
	}
	for _, w := range c.ProcessWidths {
		if w < 1 {
			return fmt.Errorf("process_widths 含非法档位 %d: 需要正整数", w)
		}
	}
	if len(c.ProcessFormats) == 0 {
		return errors.New("process_formats 不可为空: 至少允许一种目标格式")
	}
	for _, f := range c.ProcessFormats {
		if err := imaging.ValidateFormat(f); err != nil {
			return fmt.Errorf("process_formats 配置非法: %w", err)
		}
	}
	// 档位枚举的单一真相在 imaging 包;spoof 为实验特性,校验即拒绝、不静默降级。
	if err := imaging.ValidatePolicy(c.EXIFPolicy); err != nil {
		return fmt.Errorf("exif_policy 配置非法: %w", err)
	}
	return nil
}

// splitCommaInts 把环境变量里逗号分隔的整数切成 []int,非整数项跳过。
// @Description: koanf 的 Ints 对环境变量来的 []string 强转不稳,故 process_widths 直接解析为 []int;
// 非法项在此静默跳过,数值边界由 validate 兜底(负数/零档位报错)。
// @param s 逗号分隔的原始值
// @return 解析出的整数切片
func splitCommaInts(s string) []int {
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// splitCommaList 把环境变量里逗号分隔的多值切成切片,去首尾空白、丢空项。
// @Description: 环境变量只能是扁平字符串,列表型配置(如 referer_whitelist)约定
// 用逗号分隔;yaml 中它是原生列表,不经过这里。
// @param s 逗号分隔的原始值
// @return 切分后的非空项切片
func splitCommaList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
