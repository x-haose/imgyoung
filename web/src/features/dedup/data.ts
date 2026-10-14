import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { api, type ListResponse } from '@/lib/api'

/** 候选对中一方的展示信息(对应 /api/admin/dedup 响应)。 */
export interface CandidateSide {
  blob_id: number // 物理图 id,裁决保留方的标识
  image_id: string // 该物理图任一引用的短 ID,预览用;空串表示暂无引用
  mime: string
  size: number // 存储字节
  width: number
  height: number
  refs: number // 引用数,合并后并入保留方
}

/** 相似候选项。 */
export interface CandidateItem {
  id: number
  distance: number // pHash 汉明距离
  created_at: string // 发现时刻,UTC 文本
  a: CandidateSide
  b: CandidateSide
}

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
