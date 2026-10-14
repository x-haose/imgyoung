import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { api, type ListResponse } from '@/lib/api'

/** 运营配置(settings 表键值,后台修改即时热生效);键名与后端配置键一一对应。 */
export interface RuntimeSettings {
  max_upload_mb: number // 单图上限(MB)
  max_pixels_mp: number // 像素预算(百万像素)
  max_concurrent_uploads: number // 上传并发上限
  upload_rate_per_min: number // 每用户每分钟上传次数上限;0 表示不启用
  base_url: string // 对外站点地址;空则按请求域名推导绝对地址
  cdn_cloudflare_zone_id: string // Cloudflare Zone ID
  cdn_cloudflare_api_token: string // 出口恒为空串(脱敏);是否已配置看 derived.cdn_api_token_set
  allow_empty_referer: boolean // 空 Referer 是否放行
  exif_policy: string // 全局 EXIF 档位:passthrough / strip
  min_success: number // 上传成功所需的最少同步副本数
  access_mode: string // 访问模式:proxy / redirect
  encrypt_new_channels: boolean // 新建渠道未显式指定时是否加密
  health_check_seconds: number // 渠道健康检查间隔(秒)
  backup_interval_hours: number // 备份间隔(小时)
  backup_keep: number // 备份保留份数
  cleanup_sweep_seconds: number // 生命周期清理扫描间隔(秒)
  delete_grace_minutes: number // 引用归零到物理删的宽限(分钟);0 表示立即
  default_ttl_hours: number // 全局默认 TTL(小时);0 表示永不过期
  ttl_renew_on_access: boolean // 公开访问是否顺延带 TTL 图片的到期时刻
  activity_flush_seconds: number // 访问活跃度批量落库间隔(秒)
  cold_cleanup_days: number // 冷图待清理阈值(天);0 表示不启用
  usage_report_seconds: number // 渠道用量上报间隔(秒)
  channel_quota_gb: number // 单渠道用量告警阈值(GB);0 表示不告警
  dedup_scan_seconds: number // 后台查重扫描间隔(秒)
  dedup_phash_max_distance: number // pHash 相似判定汉明距离上限;0 表示不做相似扫描
  process_widths: number[] | null // 允许的宽度档(像素);空表示不允许缩放
  process_formats: string[] | null // 允许的目标格式
  process_quality_webp: number // webp 质量档 [0,100]
  process_quality_jpeg: number // jpeg 质量档 [1,100]
  process_webp_method: number // webp 质量 / 速度权衡 [0,6]
  process_workers: number // 处理并发上限
  process_timeout_ms: number // 单请求处理超时(毫秒)
  variants_cache_mb: number // 衍生图磁盘缓存上限(MB)
  idempotency_key_ttl_hours: number // 上传幂等键保留期(小时)
  alert_webhook_url: string // 告警 Webhook 地址;空表示不外发
  audit_retention_days: number // 审计日志保留天数;0 表示永久保留
}

/** 全量生效配置切面(GET /api/admin/config/full,仅管理员)。 */
export interface FullConfig {
  bootstrap: {
    listen: string // HTTP 监听地址(环境变量,改需重启)
    data_dir: string // 数据目录(环境变量,改需重启)
    trusted_proxies: string // 信任其转发头的反向代理网段,逗号分隔
    metrics_allow: string // 允许抓取 /metrics 的网段,逗号分隔
  }
  settings: RuntimeSettings
  derived: {
    cdn_purge_enabled: boolean // 删除级联的 CDN purge 是否生效(zone、token、base_url 齐备)
    cdn_api_token_set: boolean // Cloudflare API Token 是否已配置(值本身脱敏)
    login_burst: number // 每个 IP / 用户名允许的突发登录尝试次数
    login_refill_seconds: number // 突发额度用尽后每恢复一次尝试的间隔(秒)
    reconcile_interval_seconds: number // 副本对账间隔(秒)
  }
}

export const fullConfigQueryOptions = () =>
  queryOptions({
    queryKey: ['config-full'],
    queryFn: () => api.get<FullConfig>('/api/admin/config/full'),
    staleTime: 60_000,
  })

/** 提交一组运营配置修改(只传改动的键),成功即热生效并返回更新后的全量切面;非法取值整体 400。 */
export function updateConfig(patch: Record<string, unknown>) {
  return api.put<FullConfig>('/api/admin/config', patch)
}

/** 防盗链白名单规则行。 */
export interface RefererRule {
  id: number // 规则行 id,删除用
  pattern: string // 规则:裸域或子域通配
  created_at: string // 添加时刻,UTC 文本
}

export const refererQueryOptions = () =>
  queryOptions({
    queryKey: ['referer-whitelist'],
    queryFn: () =>
      api.get<ListResponse<RefererRule>>('/api/admin/referer-whitelist'),
  })

export function addRefererRule(pattern: string) {
  return api.post<RefererRule>('/api/admin/referer-whitelist', { pattern })
}

export function removeRefererRule(id: number) {
  return api.delete(`/api/admin/referer-whitelist/${id}`)
}

/** 元数据全量导出地址;浏览器直接下载,不走 fetch。 */
export const EXPORT_URL = '/api/admin/export'

/** 备份渠道上的一份元数据库快照。 */
export interface Snapshot {
  name: string // 对象文件名,形如 imgyoung-20060102-150405.000.db
  size: number // 字节数
  taken_at: string // 快照时刻,RFC 3339
}

export const backupsQueryOptions = () =>
  queryOptions({
    queryKey: ['backups'],
    queryFn: () => api.get<ListResponse<Snapshot>>('/api/admin/backups'),
  })

export function createBackup() {
  return api.post<Snapshot>('/api/admin/backups')
}

/** 恢复响应:快照已暂存,服务随即停机等进程管理器拉起时换入。 */
export interface RestoreAccepted {
  snapshot: string // 快照名
  staged: string // 暂存文件路径
  message: string // 服务端说明
}

export function restoreBackup(name: string) {
  return api.post<RestoreAccepted>(
    `/api/admin/backups/${encodeURIComponent(name)}/restore`
  )
}

/** 快照下载地址;浏览器直接下载,不走 fetch。 */
export function backupDownloadURL(name: string): string {
  return `/api/admin/backups/${encodeURIComponent(name)}`
}

/** 告警类型,与后端 alert 包的 Kind 常量一致。 */
export const ALERT_KINDS = [
  'channel_unhealthy',
  'replication_failed',
  'channel_quota_exceeded',
  'drain_failed',
  'backup_failed',
] as const

export type AlertKind = (typeof ALERT_KINDS)[number]

/** 一条告警记录。 */
export interface AlertItem {
  id: number
  kind: string // 告警类型,见 ALERT_KINDS;未知值按原样展示
  target: string // 告警对象,渠道名等;无对象为空串
  message: string // 人话描述
  opened_at: string // 开启时刻,UTC 文本
  resolved_at: string | null // 恢复时刻;null 表示仍未恢复
}

export const alertHistoryQueryOptions = (page: number, pageSize: number) =>
  queryOptions({
    queryKey: ['alerts', 'history', page, pageSize],
    queryFn: () =>
      api.get<ListResponse<AlertItem>>('/api/admin/alerts', {
        status: 'all',
        page,
        page_size: pageSize,
      }),
    placeholderData: keepPreviousData,
  })
