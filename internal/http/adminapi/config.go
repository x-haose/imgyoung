package adminapi

import (
	"net/http"
	"time"

	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/core/replication"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/config"
)

// RuntimeConfigView 面向后台 UI 的只读运行配置切面,启动时由装配根从进程配置摘取。
// 只含 UI 呈现所需的键;凭证/密钥类一律不进此结构。
type RuntimeConfigView struct {
	ColdCleanupDays    int    `json:"cold_cleanup_days"`    // 冷图待清理阈值(天);0 表示未启用
	DeleteGraceMinutes int    `json:"delete_grace_minutes"` // 引用归零到物理删的宽限(分钟);0 表示立即
	ProcessWidths      []int  `json:"process_widths"`       // 按需处理允许的宽度档白名单(像素);空表示不允许缩放
	DefaultTTLHours    int    `json:"default_ttl_hours"`    // 全局默认 TTL(小时);0 表示永不过期
	TTLRenewOnAccess   bool   `json:"ttl_renew_on_access"`  // 公开访问是否顺延带 TTL 图片的到期时刻
	MaxUploadMB        int64  `json:"max_upload_mb"`        // 单图上限(MB)
	BaseURL            string `json:"base_url"`             // 对外站点地址;空则按请求域名推导绝对地址
	AccessMode         string `json:"access_mode"`          // 访问模式:proxy / redirect
	EXIFPolicy         string `json:"exif_policy"`          // 全局 EXIF 档位:passthrough / strip
}

// HandleConfig 返回生效运行配置的只读切面,供前端呈现阈值文案与档位。
// @Description: 挂在会话门内对全部登录用户开放——上传上限、删除宽限、缩略档位
// 对普通用户的操作反馈同样必要,且不含任何凭证/密钥类配置。
func (h *Handlers) HandleConfig(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, h.cfg.Runtime)
}

// megapixel 一百万像素,像素预算换算回配置单位(MP)用。
const megapixel = 1_000_000

// FullConfigView 全量非敏感生效配置的分组切面,设置页「运行配置」卡片按组呈现。
// 凭证/密钥/连接串类配置一律不进此结构;配置写路径仍是配置文件/环境变量。
type FullConfigView struct {
	Server      ServerConfigGroup      `json:"server"`      // 服务
	Upload      UploadConfigGroup      `json:"upload"`      // 上传限制
	Lifecycle   LifecycleConfigGroup   `json:"lifecycle"`   // 生命周期
	Processing  ProcessingConfigGroup  `json:"processing"`  // 按需处理
	Security    SecurityConfigGroup    `json:"security"`    // 安全
	Reliability ReliabilityConfigGroup `json:"reliability"` // 可靠性
}

// ServerConfigGroup 服务组:监听与对外形态。
type ServerConfigGroup struct {
	Listen              string `json:"listen"`                 // HTTP 监听地址
	BaseURL             string `json:"base_url"`               // 对外站点地址;空则按请求域名推导绝对地址
	AccessMode          string `json:"access_mode"`            // 访问模式:proxy / redirect
	CDNCloudflareZoneID string `json:"cdn_cloudflare_zone_id"` // Cloudflare Zone ID;空表示未配置删除级联的 CDN purge
	CDNPurgeEnabled     bool   `json:"cdn_purge_enabled"`      // 删除级联的 CDN purge 是否生效(zone、token、base_url 三者齐备)
}

// UploadConfigGroup 上传限制组:体量、像素与流量闸。
type UploadConfigGroup struct {
	MaxUploadMB          int64 `json:"max_upload_mb"`          // 单图上限(MB)
	MaxPixelsMP          int64 `json:"max_pixels_mp"`          // 像素预算(百万像素)
	MaxConcurrentUploads int   `json:"max_concurrent_uploads"` // 上传并发上限
	UploadRatePerMin     int   `json:"upload_rate_per_min"`    // 每用户每分钟上传次数上限;0 表示不启用
}

// LifecycleConfigGroup 生命周期组:TTL、冷图与删除节奏。
type LifecycleConfigGroup struct {
	DefaultTTLHours     int  `json:"default_ttl_hours"`     // 全局默认 TTL(小时);0 表示永不过期
	TTLRenewOnAccess    bool `json:"ttl_renew_on_access"`   // 公开访问是否顺延带 TTL 图片的到期时刻
	ColdCleanupDays     int  `json:"cold_cleanup_days"`     // 冷图待清理阈值(天);0 表示未启用
	DeleteGraceMinutes  int  `json:"delete_grace_minutes"`  // 引用归零到物理删的宽限(分钟);0 表示立即
	CleanupSweepSeconds int  `json:"cleanup_sweep_seconds"` // 生命周期清理扫描间隔(秒)
}

// ProcessingConfigGroup 按需处理组:档位白名单与质量参数。
type ProcessingConfigGroup struct {
	ProcessWidths      []int    `json:"process_widths"`       // 允许的宽度档(像素);空表示不允许缩放
	ProcessFormats     []string `json:"process_formats"`      // 允许的目标格式
	ProcessQualityWebP int      `json:"process_quality_webp"` // webp 质量档 [0,100]
	ProcessQualityJPEG int      `json:"process_quality_jpeg"` // jpeg 质量档 [1,100]
	EXIFPolicy         string   `json:"exif_policy"`          // 全局 EXIF 档位:passthrough / strip
}

// SecurityConfigGroup 安全组:防盗链空 Referer 策略与登录限速参数。
type SecurityConfigGroup struct {
	AllowEmptyReferer  bool `json:"allow_empty_referer"`  // 空 Referer 是否放行(防盗链白名单启用时生效)
	LoginBurst         int  `json:"login_burst"`          // 每个 IP / 用户名允许的突发登录尝试次数
	LoginRefillSeconds int  `json:"login_refill_seconds"` // 突发额度用尽后每恢复一次尝试的间隔(秒)
}

// ReliabilityConfigGroup 可靠性组:备份、健康检查与副本对账节奏。
type ReliabilityConfigGroup struct {
	BackupKeep               int `json:"backup_keep"`                // 元数据库备份保留份数
	BackupIntervalHours      int `json:"backup_interval_hours"`      // 备份间隔(小时)
	HealthCheckSeconds       int `json:"health_check_seconds"`       // 渠道健康检查间隔(秒)
	ReconcileIntervalSeconds int `json:"reconcile_interval_seconds"` // 副本对账间隔(秒)
}

// FullConfigViewFrom 从进程配置摘取全量非敏感切面(纯映射,装配时调用一次)。
// @param cfg 进程运行配置
// @return 分组只读切面
func FullConfigViewFrom(cfg config.Config) FullConfigView {
	return FullConfigView{
		Server: ServerConfigGroup{
			Listen:              cfg.Listen,
			BaseURL:             cfg.BaseURL,
			AccessMode:          cfg.AccessMode,
			CDNCloudflareZoneID: cfg.CDNCloudflareZoneID,
			CDNPurgeEnabled:     cfg.CDNPurgeEnabled(),
		},
		Upload: UploadConfigGroup{
			MaxUploadMB:          cfg.MaxUploadBytes >> 20,
			MaxPixelsMP:          cfg.MaxPixels / megapixel,
			MaxConcurrentUploads: cfg.MaxConcurrentUploads,
			UploadRatePerMin:     cfg.UploadRatePerMin,
		},
		Lifecycle: LifecycleConfigGroup{
			DefaultTTLHours:     cfg.DefaultTTLHours,
			TTLRenewOnAccess:    cfg.TTLRenewOnAccess,
			ColdCleanupDays:     cfg.ColdCleanupDays,
			DeleteGraceMinutes:  cfg.DeleteGraceMinutes,
			CleanupSweepSeconds: cfg.CleanupSweepSeconds,
		},
		Processing: ProcessingConfigGroup{
			ProcessWidths:      cfg.ProcessWidths,
			ProcessFormats:     cfg.ProcessFormats,
			ProcessQualityWebP: cfg.ProcessQualityWebP,
			ProcessQualityJPEG: cfg.ProcessQualityJPEG,
			EXIFPolicy:         string(cfg.EXIFPolicy),
		},
		Security: SecurityConfigGroup{
			AllowEmptyReferer:  cfg.AllowEmptyReferer,
			LoginBurst:         auth.LoginBurst,
			LoginRefillSeconds: int(auth.LoginRefillEvery / time.Second),
		},
		Reliability: ReliabilityConfigGroup{
			BackupKeep:               cfg.BackupKeep,
			BackupIntervalHours:      cfg.BackupIntervalHours,
			HealthCheckSeconds:       cfg.HealthCheckSeconds,
			ReconcileIntervalSeconds: int(replication.ReconcileInterval / time.Second),
		},
	}
}

// HandleConfigFull 返回全量非敏感生效配置(仅管理员路由),设置页自助排障用。
func (h *Handlers) HandleConfigFull(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, h.cfg.Full)
}
