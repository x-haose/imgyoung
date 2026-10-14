import { queryOptions } from '@tanstack/react-query'
import { api, type ListResponse, type Schema } from '@/lib/api'

/** 总览里的渠道概览项。 */
export type ChannelStat = Schema<'ChannelStat'>

/** 热门图项,按累计访问次数降序。 */
export type TopImage = Schema<'TopImage'>

/** 管理员视图:全站计数、待办、访问与流量估算、热门图与渠道水位。 */
export type AdminDashboard = Schema<'AdminDashboard'>

/** 普通用户视图:本人用量。 */
export type UserDashboard = Schema<'UserDashboard'>

export type Dashboard = Schema<'Dashboard'>

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
export type AlertKind = Schema<'Alert'>['kind']

export type AlertItem = Schema<'Alert'>

/** 未恢复的告警,仅管理员可读;按评估周期的量级轮询。 */
export const openAlertsQueryOptions = () =>
  queryOptions({
    queryKey: ['alerts', 'open'],
    queryFn: () => api.get<ListResponse<AlertItem>>('/api/admin/alerts'),
    refetchInterval: 60_000,
  })
