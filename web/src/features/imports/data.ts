/** 批量导入数据层:契约类型、查询定义与写操作,全部经会话态管理 API。 */
import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { messages } from '@/i18n'
import {
  api,
  type ErrorCode,
  type ListResponse,
  type QueryValue,
  type Schema,
} from '@/lib/api'
import { errorText } from '@/lib/server-text'

/** 导入任务(对应 /api/admin/imports 响应)。 */
export type ImportJob = Schema<'ImportJob'>

export type ImportMode = Schema<'ImportMode'>
export type ImportStatus = Schema<'ImportStatus'>
export type ImportCounts = Schema<'ImportCounts'>
export type ImportScanReport = Schema<'ImportScanReport'>

/** 任务的一个条目,即一张待导入的图片地址。 */
export type ImportItem = Schema<'ImportItem'>

/** 待确认图片地址的域名分组。 */
export type ImportHost = Schema<'ImportHost'>

/** 创建任务的请求体;各模式只带自己的字段。 */
export type ImportCreateRequest = Schema<'ImportCreateRequest'>

/** 对照表的下载格式。 */
export type MappingFormat = 'json' | 'csv'

/** 任务列表每页条数的缺省值。 */
export const IMPORTS_PAGE_SIZE = 20

/** 进行中的任务向服务端取进度的间隔(毫秒)。 */
const POLL_INTERVAL_MS = 2000

export const importsKey = ['imports'] as const

/** 任务是否仍由服务端推进;只有这两个状态的进度会自己变化。 */
export function isRunning(status: ImportStatus): boolean {
  return status === 'scanning' || status === 'importing'
}

/** 任务可以处理的条目总数:已导入、失败与待处理三者之和,不含已跳过与待确认。 */
export function processableTotal(counts: ImportCounts): number {
  return counts.done + counts.failed + counts.pending
}

/** 任务任一条目还在等待转存时,「重试」才有可做的事。 */
export function canRetry(job: ImportJob): boolean {
  return (
    (job.status === 'done' ||
      job.status === 'aborted' ||
      job.status === 'cancelled') &&
    job.counts.failed + job.counts.pending > 0
  )
}

export interface ImportListParams {
  page: number
  page_size: number
  user_id?: string // 仅管理员生效:只看该用户创建的任务
}

/** 任务分页列表;有进行中的任务时轮询,翻页时保留上一页数据。 */
export const importsQueryOptions = (params: ImportListParams) =>
  queryOptions({
    queryKey: [...importsKey, 'list', params] as const,
    queryFn: () =>
      api.get<ListResponse<ImportJob>>(
        '/api/admin/imports',
        params as unknown as Record<string, QueryValue>
      ),
    placeholderData: keepPreviousData,
    refetchInterval: (query) =>
      query.state.data?.items.some((j) => isRunning(j.status))
        ? POLL_INTERVAL_MS
        : false,
  })

/** 任务详情;任务进行中时轮询取进度。 */
export const importQueryOptions = (id: string) =>
  queryOptions({
    queryKey: [...importsKey, 'detail', id] as const,
    queryFn: () => api.get<ImportJob>(`/api/admin/imports/${id}`),
    refetchInterval: (query) =>
      query.state.data && isRunning(query.state.data.status)
        ? POLL_INTERVAL_MS
        : false,
  })

/** 待确认条目的域名分组,数量多的在前。 */
export const importHostsQueryOptions = (id: string) =>
  queryOptions({
    queryKey: [...importsKey, 'hosts', id] as const,
    queryFn: () => api.get<ImportHost[]>(`/api/admin/imports/${id}/hosts`),
  })

export interface ImportItemParams {
  state: Schema<'ImportItemState'>
  page: number
  page_size: number
}

/** 任务的条目分页;live 为真时轮询,供导入进行中实时看到新增的失败项。 */
export const importItemsQueryOptions = (
  id: string,
  params: ImportItemParams,
  live: boolean
) =>
  queryOptions({
    queryKey: [...importsKey, 'items', id, params] as const,
    queryFn: () =>
      api.get<ListResponse<ImportItem>>(
        `/api/admin/imports/${id}/items`,
        params as unknown as Record<string, QueryValue>
      ),
    placeholderData: keepPreviousData,
    refetchInterval: live ? POLL_INTERVAL_MS : false,
  })

export const createImport = (body: ImportCreateRequest) =>
  api.post<ImportJob>('/api/admin/imports', body)

export const startImport = (id: string, hosts: string[]) =>
  api.post<ImportJob>(`/api/admin/imports/${id}/start`, { hosts })

export const cancelImport = (id: string) =>
  api.post<ImportJob>(`/api/admin/imports/${id}/cancel`)

export const retryImport = (id: string) =>
  api.post<ImportJob>(`/api/admin/imports/${id}/retry`)

export const deleteImport = (id: string) =>
  api.delete(`/api/admin/imports/${id}`)

/** 下载对照表;经会话鉴权的 GET,取回完整内容后才交给浏览器保存。 */
export const downloadMapping = (id: string, format: MappingFormat) =>
  api.download(`/api/admin/imports/${id}/mapping`, { format })

/**
 * 条目或页面失败原因的当前语言文案。条目只有错误码没有参数,
 * 词典里带参数占位符的错误码改用 imports.failureReasons 里不带参数的说法;
 * 错误码为空时退回根因原文。
 */
export function failureText(code: ErrorCode | null, detail: string): string {
  if (code === null) return detail
  const own: Record<string, string> = messages().imports.failureReasons
  return Object.hasOwn(own, code)
    ? own[code]
    : errorText({ code, error: detail })
}
