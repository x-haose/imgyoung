import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { api, type ListResponse, type Schema } from '@/lib/api'
import { toUTCText } from '@/lib/format'
import { type AuditAction } from './actions'

/** 每页审计条数的缺省值。 */
export const AUDIT_PAGE_SIZE = 50

/** 审计记录项(对应 /api/admin/audit 响应)。 */
export type AuditItem = Schema<'AuditEntry'>

/** 列表筛选;日期为本地日界的 YYYY-MM-DD,区间为闭开 [from 00:00, to 次日 00:00)。 */
export interface AuditFilters {
  page: number
  page_size: number
  action?: AuditAction
  actor?: string
  target?: string
  from?: string
  to?: string
}

/** 本地日期的零点毫秒;day 已经过格式校验。 */
function localMidnight(day: string, plusDays = 0): number {
  const [y, mo, d] = day.split('-').map(Number)
  return new Date(y, mo - 1, d + plusDays).getTime()
}

export const auditQueryOptions = (f: AuditFilters) =>
  queryOptions({
    queryKey: ['audit', f],
    queryFn: () =>
      api.get<ListResponse<AuditItem>>('/api/admin/audit', {
        page: f.page,
        page_size: f.page_size,
        action: f.action,
        actor: f.actor,
        target: f.target,
        since: f.from ? toUTCText(localMidnight(f.from)) : undefined,
        until: f.to ? toUTCText(localMidnight(f.to, 1)) : undefined,
      }),
    placeholderData: keepPreviousData,
  })

/** 动作所属领域 → 徽标配色,便于扫读;同一领域同一色。 */
export function actionTone(action: string): string {
  const domain = action.split('.')[0]
  switch (domain) {
    case 'channel':
      return 'border-info/40 bg-info/10 text-info'
    case 'token':
      return 'border-primary/40 bg-primary/10 text-primary'
    case 'user':
      return 'border-warning/40 bg-warning/10 text-warning'
    case 'image':
    case 'dedup':
      return 'border-destructive/40 bg-destructive/10 text-destructive'
    default:
      return 'border-border bg-secondary text-secondary-foreground'
  }
}
