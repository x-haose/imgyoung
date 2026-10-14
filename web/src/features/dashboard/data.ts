import { queryOptions } from '@tanstack/react-query'
import { api, type ListResponse } from '@/lib/api'

/** 总览里的渠道概览项。 */
export interface ChannelStat {
  id: number
  name: string
  driver: string // rclone 后端名
  healthy: boolean
  bytes: number // 已存字节,副本口径
}

/** 热门图项,按累计访问次数降序。 */
export interface TopImage {
  id: string
  url: string // 公开访问路径
  file_name: string
  access_count: number // 累计访问次数,活跃度落库周期粒度
  size: number // 存储字节
}

/** 管理员视图:全站计数、待办、访问与流量估算、热门图与渠道水位。 */
export interface AdminDashboard {
  backup_colocated: boolean // 元数据库快照与图片数据同盘
  images: number
  storage_bytes: number
  users: number
  pending_cleanup: number
  pending_dedup: number
  total_accesses: number
  traffic_estimate_bytes: number // 访问数 × 原图字节的粗略估算
  top_images: TopImage[]
  channels: ChannelStat[]
}

/** 普通用户视图:本人用量。 */
export interface UserDashboard {
  images: number
  used_bytes: number
  quota_bytes: number | null // null 表示不限
}

export type Dashboard = AdminDashboard | UserDashboard

/** 同一端点按会话角色返回两种形状,管理员视图以渠道列表为标志。 */
export function isAdminDashboard(d: Dashboard): d is AdminDashboard {
  return 'channels' in d
}

export const dashboardQueryOptions = () =>
  queryOptions({
    queryKey: ['dashboard'],
    queryFn: () => api.get<Dashboard>('/api/admin/dashboard'),
  })

/** 告警类型,与后端 alert 包常量一致。 */
export type AlertKind =
  | 'channel_unhealthy'
  | 'replication_failed'
  | 'channel_quota_exceeded'
  | 'drain_failed'
  | 'backup_failed'

export interface AlertItem {
  id: number
  kind: AlertKind
  target: string // 告警对象,渠道名等;无对象为空串
  message: string // 服务端生成的人话描述
  opened_at: string // UTC 文本
  resolved_at: string | null // null 表示仍未恢复
}

/** 未恢复的告警,仅管理员可读;按评估周期的量级轮询。 */
export const openAlertsQueryOptions = () =>
  queryOptions({
    queryKey: ['alerts', 'open'],
    queryFn: () => api.get<ListResponse<AlertItem>>('/api/admin/alerts'),
    refetchInterval: 60_000,
  })
