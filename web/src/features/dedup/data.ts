import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { api, type ListResponse, type Schema } from '@/lib/api'

/** 候选对中一方的展示信息(对应 /api/admin/dedup 响应)。 */
export type CandidateSide = Schema<'CandidateSide'>

/** 相似候选项。 */
export type CandidateItem = Schema<'DedupCandidate'>

/** 每页候选组数的缺省值;两列卡片,一屏约十组。 */
export const DEDUP_PAGE_SIZE = 20

/** 查询键前缀;裁决后整体失效。 */
export const dedupKey = ['dedup'] as const

export const candidatesQueryOptions = (page: number, pageSize: number) =>
  queryOptions({
    queryKey: [...dedupKey, { page, pageSize }],
    queryFn: () =>
      api.get<ListResponse<CandidateItem>>('/api/admin/dedup', {
        page,
        page_size: pageSize,
      }),
    // 翻页时沿用上一页数据,避免整页退回骨架屏。
    placeholderData: keepPreviousData,
  })

export const mergeCandidate = (id: number, keepBlobId: number) =>
  api.post(`/api/admin/dedup/${id}/merge`, { keep_blob_id: keepBlobId })

export const dismissCandidate = (id: number) =>
  api.post(`/api/admin/dedup/${id}/dismiss`)

/** 裁决会改变总览的待办计数与图片列表,一并失效的查询键前缀。 */
export const affectedByDecision = [dedupKey, ['dashboard'], ['images']] as const
