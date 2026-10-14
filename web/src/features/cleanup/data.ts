import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { api, type ListResponse, type Schema } from '@/lib/api'

/** 待清理项(对应 /api/admin/cleanup 响应)。 */
export type CleanupItem = Schema<'CleanupItem'>

/** POST /images/batch-delete 的响应。 */
export type BatchDeleteResult = Schema<'BatchDeleteResult'>

/** 单次批量删除的 id 数上限,与后端 batchDeleteMax 一致;每页至多 100 条,全选本页不会越界。 */
export const BATCH_DELETE_MAX = 100

/** 每页冷图条数的缺省值。 */
export const CLEANUP_PAGE_SIZE = 50

export const cleanupKey = ['cleanup'] as const

export const cleanupQueryOptions = (page: number, pageSize: number) =>
  queryOptions({
    queryKey: [...cleanupKey, { page, pageSize }],
    queryFn: () =>
      api.get<ListResponse<CleanupItem>>('/api/admin/cleanup', {
        page,
        page_size: pageSize,
      }),
    placeholderData: keepPreviousData,
  })

/** 保留 = 置清理豁免,此后不再参与冷图打标。 */
export const keepImage = (id: string) =>
  api.patch(`/api/admin/images/${id}`, { cleanup_exempt: true })

export const deleteImage = (id: string) => api.delete(`/api/admin/images/${id}`)

export const batchDeleteImages = (ids: string[]) =>
  api.post<BatchDeleteResult>('/api/admin/images/batch-delete', { ids })

/** 裁决同时改变图片列表与总览待办计数,一并失效的查询键前缀。 */
export const affectedByDecision = [
  cleanupKey,
  ['images'],
  ['dashboard'],
] as const
