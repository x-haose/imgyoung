import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { api, type ListResponse, type Schema } from '@/lib/api'

/** 运营配置(settings 表键值,后台修改即时热生效);键名与后端配置键一一对应。 */
export type RuntimeSettings = Schema<'Settings'>

/** 全量生效配置切面(GET /api/admin/config/full,仅管理员)。 */
export type FullConfig = Schema<'FullConfig'>

export const fullConfigQueryOptions = () =>
  queryOptions({
    queryKey: ['config-full'],
    queryFn: () => api.get<FullConfig>('/api/admin/config/full'),
    staleTime: 60_000,
  })

/** 提交一组运营配置修改(只传改动的键),成功即热生效并返回更新后的全量切面;非法取值整体 400。 */
export function updateConfig(patch: Partial<RuntimeSettings>) {
  return api.put<FullConfig>('/api/admin/config', patch)
}

/** 防盗链白名单规则行。 */
export type RefererRule = Schema<'RefererRule'>

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
export type Snapshot = Schema<'Snapshot'>

export const backupsQueryOptions = () =>
  queryOptions({
    queryKey: ['backups'],
    queryFn: () => api.get<ListResponse<Snapshot>>('/api/admin/backups'),
  })

export function createBackup() {
  return api.post<Snapshot>('/api/admin/backups')
}

/** 恢复响应:快照已暂存,服务随即停机等进程管理器拉起时换入。 */
export type RestoreAccepted = Schema<'RestoreAccepted'>

export function restoreBackup(name: string) {
  return api.post<RestoreAccepted>(
    `/api/admin/backups/${encodeURIComponent(name)}/restore`
  )
}

/** 快照下载地址;浏览器直接下载,不走 fetch。 */
export function backupDownloadURL(name: string): string {
  return `/api/admin/backups/${encodeURIComponent(name)}`
}

/** 告警类型,与后端 alert 包的 Kind 常量一致;顺序即筛选器的展示顺序。 */
export const ALERT_KINDS = [
  'channel_unhealthy',
  'replication_failed',
  'channel_quota_exceeded',
  'drain_failed',
  'backup_failed',
] as const satisfies readonly AlertKind[]

export type AlertKind = Schema<'Alert'>['kind']

/** 一条告警记录。 */
export type AlertItem = Schema<'Alert'>

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
