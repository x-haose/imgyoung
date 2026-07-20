import { apiFetch } from "../../api/http";

/** 运营配置(settings 表键值,后台修改即时热生效);键名与后端配置键一一对应。 */
export interface RuntimeSettings {
  max_upload_mb: number; // 单图上限(MB)
  max_pixels_mp: number; // 像素预算(百万像素)
  max_concurrent_uploads: number; // 上传并发上限
  upload_rate_per_min: number; // 每用户每分钟上传次数上限;0 表示不启用
  base_url: string; // 对外站点地址;空则按请求域名推导绝对地址
  cdn_cloudflare_zone_id: string; // Cloudflare Zone ID
  cdn_cloudflare_api_token: string; // 出口恒为空串(脱敏);是否已配置看 derived.cdn_api_token_set
  allow_empty_referer: boolean; // 空 Referer 是否放行
  exif_policy: string; // 全局 EXIF 档位:passthrough / strip
  min_success: number; // 上传成功所需的最少同步副本数
  access_mode: string; // 访问模式:proxy / redirect
  encrypt_new_channels: boolean; // 新建渠道未显式指定时是否加密
  health_check_seconds: number; // 渠道健康检查间隔(秒)
  backup_interval_hours: number; // 备份间隔(小时)
  backup_keep: number; // 备份保留份数
  cleanup_sweep_seconds: number; // 生命周期清理扫描间隔(秒)
  delete_grace_minutes: number; // 引用归零到物理删的宽限(分钟);0 表示立即
  default_ttl_hours: number; // 全局默认 TTL(小时);0 表示永不过期
  ttl_renew_on_access: boolean; // 公开访问是否顺延带 TTL 图片的到期时刻
  activity_flush_seconds: number; // 访问活跃度批量落库间隔(秒)
  cold_cleanup_days: number; // 冷图待清理阈值(天);0 表示不启用
  usage_report_seconds: number; // 渠道用量上报间隔(秒)
  channel_quota_gb: number; // 单渠道用量告警阈值(GB);0 表示不告警
  dedup_scan_seconds: number; // 后台查重扫描间隔(秒)
  dedup_phash_max_distance: number; // pHash 相似判定汉明距离上限;0 表示不做相似扫描
  process_widths: number[] | null; // 允许的宽度档(像素);空表示不允许缩放
  process_formats: string[] | null; // 允许的目标格式
  process_quality_webp: number; // webp 质量档 [0,100]
  process_quality_jpeg: number; // jpeg 质量档 [1,100]
  process_webp_method: number; // webp 质量/速度权衡 [0,6]
  process_workers: number; // 处理并发上限
  process_timeout_ms: number; // 单请求处理超时(毫秒)
  variants_cache_mb: number; // variant 磁盘缓存上限(MB)
}

/** 全量生效配置切面(GET /api/admin/config/full,仅管理员)。 */
export interface FullConfig {
  bootstrap: {
    listen: string; // HTTP 监听地址(环境变量,改需重启)
    data_dir: string; // 数据目录(环境变量,改需重启)
  };
  settings: RuntimeSettings;
  derived: {
    cdn_purge_enabled: boolean; // 删除级联的 CDN purge 是否生效(zone、token、base_url 齐备)
    cdn_api_token_set: boolean; // Cloudflare API Token 是否已配置(值本身脱敏)
    login_burst: number; // 每个 IP / 用户名允许的突发登录尝试次数
    login_refill_seconds: number; // 突发额度用尽后每恢复一次尝试的间隔(秒)
    reconcile_interval_seconds: number; // 副本对账间隔(秒)
  };
}

/** 拉取全量生效配置(仅管理员)。 */
export function fetchFullConfig(): Promise<FullConfig> {
  return apiFetch<FullConfig>("/api/admin/config/full");
}

/**
 * 提交一组运营配置修改(仅提交变化的键),成功即热生效并返回更新后的全量切面;
 * 非法取值由后端整体拒绝(400),错误消息可直接展示。
 */
export function updateRuntimeConfig(patch: Record<string, unknown>): Promise<FullConfig> {
  return apiFetch<FullConfig>("/api/admin/config", {
    method: "PUT",
    body: JSON.stringify(patch),
  });
}
