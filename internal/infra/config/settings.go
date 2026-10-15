// Package config 管理运行配置,一刀切两类:引导配置(Bootstrap)只认环境变量、
// 改需重启;运营配置(Settings)以 settings 单行宽表为唯一真相、后台修改热生效(Store)。
// 运营配置的出厂默认值在表的列 DEFAULT 上,Go 侧 Defaults 仅供测试基线(有漂移守卫测试)。
package config

import (
	"net/url"
	"strings"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/errcode"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
)

// megapixel 一百万像素。
const megapixel = 1_000_000

// Settings 运营配置。字段与 settings 表列一一对应;json 标签即配置键(后台 patch 与 API 用)。
type Settings struct {
	MaxUploadMB            int64              `json:"max_upload_mb"`             // 单图上限(MB)
	MaxPixelsMP            int64              `json:"max_pixels_mp"`             // 像素预算(百万像素)
	MaxConcurrentUploads   int                `json:"max_concurrent_uploads"`    // 上传并发上限
	UploadRatePerMin       int                `json:"upload_rate_per_min"`       // 每用户每分钟上传次数上限;0=不启用
	BaseURL                string             `json:"base_url"`                  // 对外站点地址(去尾斜杠);空则按请求域名推导
	CDNCloudflareZoneID    string             `json:"cdn_cloudflare_zone_id"`    // Cloudflare Zone ID
	CDNCloudflareAPIToken  string             `json:"cdn_cloudflare_api_token"`  // Cloudflare API Token;内存明文、入库密文、出口脱敏
	AllowEmptyReferer      bool               `json:"allow_empty_referer"`       // 空 Referer 是否放行
	EXIFPolicy             imaging.EXIFPolicy `json:"exif_policy"`               // 全局默认 EXIF 档位,单次上传可覆盖
	MinSuccess             int                `json:"min_success"`               // 上传返回成功所需的最少同步副本数
	AccessMode             string             `json:"access_mode"`               // 访问模式:proxy(代理)/ redirect(302 省流量)
	EncryptNewChannels     bool               `json:"encrypt_new_channels"`      // 新建渠道未显式指定 encrypted 时的默认值
	HealthCheckSeconds     int                `json:"health_check_seconds"`      // 渠道健康检查间隔(秒)
	BackupIntervalHours    int                `json:"backup_interval_hours"`     // 元数据库自动备份间隔(小时)
	BackupKeep             int                `json:"backup_keep"`               // 备份保留份数;丢失映射即全部不可反查,宁多勿少
	CleanupSweepSeconds    int                `json:"cleanup_sweep_seconds"`     // 生命周期清理扫描间隔(秒)
	DeleteGraceMinutes     int                `json:"delete_grace_minutes"`      // 引用归零到物理删的宽限(分钟),可为 0
	DefaultTTLHours        int                `json:"default_ttl_hours"`         // 全局默认 TTL(小时),0=永不过期
	TTLRenewOnAccess       bool               `json:"ttl_renew_on_access"`       // 公开访问是否把带 TTL 图片的到期时刻按其时长顺延
	ActivityFlushSeconds   int                `json:"activity_flush_seconds"`    // 访问活跃度批量落库间隔(秒)
	ColdCleanupDays        int                `json:"cold_cleanup_days"`         // 冷图待清理阈值(天),0=不启用
	UsageReportSeconds     int                `json:"usage_report_seconds"`      // 渠道用量上报间隔(秒)
	ChannelQuotaGB         int                `json:"channel_quota_gb"`          // 单渠道用量告警阈值(GB),0=不告警
	DedupScanSeconds       int                `json:"dedup_scan_seconds"`        // 后台查重扫描间隔(秒)
	DedupPhashMaxDistance  int                `json:"dedup_phash_max_distance"`  // pHash 相似判定的汉明距离上限;0=不做相似扫描
	ProcessWidths          []int              `json:"process_widths"`            // 允许的宽度档白名单(像素);空表示不允许缩放
	ProcessFormats         []string           `json:"process_formats"`           // 允许的目标格式白名单
	ProcessQualityWebP     int                `json:"process_quality_webp"`      // 按需处理 webp 质量档[0,100]
	ProcessQualityJPEG     int                `json:"process_quality_jpeg"`      // 按需处理 jpeg 质量档[1,100]
	ProcessWebPMethod      int                `json:"process_webp_method"`       // webp 质量/速度权衡[0,6]
	ProcessWorkers         int                `json:"process_workers"`           // 处理并发上限(信号量)
	ProcessTimeoutMS       int                `json:"process_timeout_ms"`        // 单请求处理超时(毫秒)
	VariantsCacheMB        int64              `json:"variants_cache_mb"`         // variant 磁盘缓存上限(MB),超则 LRU 淘汰
	IdempotencyKeyTTLHours int                `json:"idempotency_key_ttl_hours"` // 上传幂等键保留期(小时),超期回收;客户端重试窗口以此为界
	AlertWebhookURL        string             `json:"alert_webhook_url"`         // 告警 Webhook 地址(http/https);空=不外发
	AuditRetentionDays     int                `json:"audit_retention_days"`      // 审计日志保留天数,超期由清理循环回收;0=永久保留
}

// MaxDurationHours 以整数表达的时长(配置项与请求参数)的统一上限:100 年,单位小时。
// @Description: 时长最终要乘成 time.Duration(int64 纳秒,约 292 年溢出);溢出会得到负数,
// 让"很长"静默变成"立即到期"或"永久"之类相反的语义,故一律在入口以它封顶。
const MaxDurationHours = 876000

// Defaults 返回与 settings 表列 DEFAULT 一致的出厂配置,供测试与工具构造基线。
// @Description: 运行期不用它(真相在表);此处是测试便利副本,由 TestDefaultsMatchSchema
// 守卫与建表默认值一致,防两处漂移。
// @return 出厂运营配置
func Defaults() Settings {
	return Settings{
		MaxUploadMB:            50,
		MaxPixelsMP:            50,
		MaxConcurrentUploads:   8,
		UploadRatePerMin:       0,
		BaseURL:                "",
		CDNCloudflareZoneID:    "",
		CDNCloudflareAPIToken:  "",
		AllowEmptyReferer:      true,
		EXIFPolicy:             imaging.PolicyPassthrough,
		MinSuccess:             1,
		AccessMode:             "proxy",
		EncryptNewChannels:     false,
		HealthCheckSeconds:     60,
		BackupIntervalHours:    24,
		BackupKeep:             7,
		CleanupSweepSeconds:    60,
		DeleteGraceMinutes:     0,
		DefaultTTLHours:        0,
		TTLRenewOnAccess:       true,
		ActivityFlushSeconds:   60,
		ColdCleanupDays:        90,
		UsageReportSeconds:     300,
		ChannelQuotaGB:         0,
		DedupScanSeconds:       300,
		DedupPhashMaxDistance:  10,
		ProcessWidths:          []int{100, 200, 400, 800, 1600},
		ProcessFormats:         []string{"webp", "jpeg", "png"},
		ProcessQualityWebP:     80,
		ProcessQualityJPEG:     85,
		ProcessWebPMethod:      4,
		ProcessWorkers:         4,
		ProcessTimeoutMS:       5000,
		VariantsCacheMB:        10240,
		IdempotencyKeyTTLHours: 24,
		AlertWebhookURL:        "",
		AuditRetentionDays:     0,
	}
}

// AuditRetention 审计日志保留期;零值表示永久保留。
// @return 保留时长
func (s Settings) AuditRetention() time.Duration {
	return time.Duration(s.AuditRetentionDays) * 24 * time.Hour
}

// IdempotencyKeyTTL 上传幂等键保留期。
// @return 保留时长
func (s Settings) IdempotencyKeyTTL() time.Duration {
	return time.Duration(s.IdempotencyKeyTTLHours) * time.Hour
}

// MaxUploadBytes 单图字节上限(由 MB 换算)。
// @return 字节数
func (s Settings) MaxUploadBytes() int64 { return s.MaxUploadMB << 20 }

// MaxPixels 像素预算(宽 × 高上限,由百万像素换算)。
// @return 像素数
func (s Settings) MaxPixels() int64 { return s.MaxPixelsMP * megapixel }

// DefaultTTL 全局默认 TTL;零值表示永不过期。
// @return TTL 时长
func (s Settings) DefaultTTL() time.Duration { return time.Duration(s.DefaultTTLHours) * time.Hour }

// CDNPurgeEnabled 判定删除级联的 CDN purge 链路是否启用。
// @Description: zone、token 与 base_url 三者均非空才启用——purge 需以配置的
// 站点地址还原公开 URL(异步任务无请求上下文可推导)。
// @return 启用为 true
func (s Settings) CDNPurgeEnabled() bool {
	return s.CDNCloudflareZoneID != "" && s.CDNCloudflareAPIToken != "" && s.BaseURL != ""
}

// Masked 返回抹掉凭证字段的副本,供任何对外输出(API/日志)使用。
// @return 凭证字段置空的配置副本
func (s Settings) Masked() Settings {
	s.CDNCloudflareAPIToken = ""
	return s
}

// normalized 返回归一后的副本(目前仅 base_url 去尾斜杠);值语义,不改原值。
// @return 归一后的配置
func (s Settings) normalized() Settings {
	s.BaseURL = strings.TrimRight(s.BaseURL, "/")
	return s
}

// validate 校验数值边界与枚举,首个非法项即返回。
// @return 非法配置的错误;全部合法时为 nil
func (s Settings) validate() error {
	if err := s.validateUpload(); err != nil {
		return err
	}
	if err := s.validateSchedules(); err != nil {
		return err
	}
	if err := s.validateLifecycle(); err != nil {
		return err
	}
	if err := s.validateProcess(); err != nil {
		return err
	}
	if err := s.validateDurationCaps(); err != nil {
		return err
	}
	if err := validateBaseURL(s.BaseURL); err != nil {
		return err
	}
	return validateWebhookURL(s.AlertWebhookURL)
}

// validateBaseURL 校验对外站点地址:空表示按请求推导,否则须为 http/https 的站点根地址。
// @Description: 上传返回的链接、CDN 清除地址与网盘授权回调都由它拼出路径,带路径、查询参数或账号信息的值
// 拼出的地址不可用;末尾的 / 已由 normalized 去掉。
// @param raw 归一后的配置值
// @return 非法地址的错误;合法或为空时为 nil
func validateBaseURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return errcode.New(errcode.SettingSiteRoot, "key", "base_url", "value", raw)
	}
	return nil
}

// validateDurationCaps 校验换算成时长的配置不超过 MaxDurationHours(100 年)折算后的上限。
// @return 超限配置的错误;全部合法时为 nil
func (s Settings) validateDurationCaps() error {
	const (
		maxDays    = MaxDurationHours / 24
		maxMinutes = MaxDurationHours * 60
		maxSeconds = MaxDurationHours * 3600
		maxMillis  = maxSeconds * 1000
	)
	return firstError(
		atMost("health_check_seconds", s.HealthCheckSeconds, maxSeconds),
		atMost("backup_interval_hours", s.BackupIntervalHours, MaxDurationHours),
		atMost("cleanup_sweep_seconds", s.CleanupSweepSeconds, maxSeconds),
		atMost("delete_grace_minutes", s.DeleteGraceMinutes, maxMinutes),
		atMost("default_ttl_hours", s.DefaultTTLHours, MaxDurationHours),
		atMost("activity_flush_seconds", s.ActivityFlushSeconds, maxSeconds),
		atMost("cold_cleanup_days", s.ColdCleanupDays, maxDays),
		atMost("usage_report_seconds", s.UsageReportSeconds, maxSeconds),
		atMost("dedup_scan_seconds", s.DedupScanSeconds, maxSeconds),
		atMost("process_timeout_ms", s.ProcessTimeoutMS, maxMillis),
		atMost("idempotency_key_ttl_hours", s.IdempotencyKeyTTLHours, MaxDurationHours),
		atMost("audit_retention_days", s.AuditRetentionDays, maxDays),
	)
}

// validateUpload 校验上传入口与公开访问的配置:体积与像素预算、并发与限速、副本门槛、访问模式、EXIF 档位。
// @return 非法配置的错误;全部合法时为 nil
func (s Settings) validateUpload() error {
	// 档位枚举的单一真相在 imaging 包;spoof 为实验特性,校验即拒绝、不静默降级。
	policy := oneOf("exif_policy", s.EXIFPolicy, imaging.Policies())
	return firstError(
		positive("max_upload_mb", s.MaxUploadMB),
		positive("max_pixels_mp", s.MaxPixelsMP),
		positive("max_concurrent_uploads", s.MaxConcurrentUploads),
		nonNegative("upload_rate_per_min", s.UploadRatePerMin),
		positive("min_success", s.MinSuccess),
		oneOf("access_mode", s.AccessMode, []string{"proxy", "redirect"}),
		policy,
	)
}

// validateSchedules 校验后台循环的间隔与备份保留份数:全部须为正整数,0 会让循环空转或备份无保留。
// @return 非法配置的错误;全部合法时为 nil
func (s Settings) validateSchedules() error {
	return firstError(
		positive("health_check_seconds", s.HealthCheckSeconds),
		positive("backup_interval_hours", s.BackupIntervalHours),
		positive("backup_keep", s.BackupKeep),
		positive("cleanup_sweep_seconds", s.CleanupSweepSeconds),
		positive("activity_flush_seconds", s.ActivityFlushSeconds),
		positive("usage_report_seconds", s.UsageReportSeconds),
		positive("dedup_scan_seconds", s.DedupScanSeconds),
	)
}

// validateLifecycle 校验生命周期与查重阈值:宽限、TTL、冷图天数、配额、汉明距离、审计保留期以 0 表示不启用或永久、
// 不可为负;幂等键保留期须为正。
// @return 非法配置的错误;全部合法时为 nil
func (s Settings) validateLifecycle() error {
	return firstError(
		nonNegative("delete_grace_minutes", s.DeleteGraceMinutes),
		nonNegative("default_ttl_hours", s.DefaultTTLHours),
		nonNegative("cold_cleanup_days", s.ColdCleanupDays),
		nonNegative("channel_quota_gb", s.ChannelQuotaGB),
		nonNegative("dedup_phash_max_distance", s.DedupPhashMaxDistance),
		positive("idempotency_key_ttl_hours", s.IdempotencyKeyTTLHours),
		nonNegative("audit_retention_days", s.AuditRetentionDays),
	)
}

// validateWebhookURL 校验告警 Webhook 地址:空表示不外发,否则须为带主机的 http/https 地址。
// @param raw 配置值
// @return 非法地址的错误;合法或为空时为 nil
func validateWebhookURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errcode.New(errcode.SettingURL, "key", "alert_webhook_url", "value", raw)
	}
	return nil
}

// validateProcess 校验按需处理的编码档位、并发与超时、缓存上限,以及宽度与格式白名单。
// @return 非法配置的错误;全部合法时为 nil
func (s Settings) validateProcess() error {
	if err := firstError(
		between("process_quality_webp", s.ProcessQualityWebP, 0, 100),
		between("process_quality_jpeg", s.ProcessQualityJPEG, 1, 100),
		between("process_webp_method", s.ProcessWebPMethod, 0, 6),
		positive("process_workers", s.ProcessWorkers),
		positive("process_timeout_ms", s.ProcessTimeoutMS),
		positive("variants_cache_mb", s.VariantsCacheMB),
	); err != nil {
		return err
	}
	for _, w := range s.ProcessWidths {
		if err := positive("process_widths", w); err != nil {
			return err
		}
	}
	if len(s.ProcessFormats) == 0 {
		return errcode.New(errcode.SettingEmpty, "key", "process_formats")
	}
	for _, f := range s.ProcessFormats {
		if err := oneOf("process_formats", f, imaging.Formats()); err != nil {
			return err
		}
	}
	return nil
}
