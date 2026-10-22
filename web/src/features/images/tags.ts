import { queryOptions } from '@tanstack/react-query'
import { api, type Schema } from '@/lib/api'

/** 一张图标签的数量上限与标签名的字符数上限,与后端一致;后端是最终裁决。 */
export const MAX_TAGS_PER_IMAGE = 20
export const MAX_TAG_LENGTH = 32

/** 当前用户的标签及各自的图片数。 */
export type TagItem = Schema<'TagList'>['items'][number]

export const tagsQueryKey = ['tags'] as const

export const tagsQueryOptions = () =>
  queryOptions({
    queryKey: tagsQueryKey,
    queryFn: () => api.get<Schema<'TagList'>>('/api/admin/tags'),
    staleTime: 30_000,
  })

/** 把输入框里的文本切成标签名:逗号(中英文)与换行分隔,去首尾空白,丢弃空项与重复项。 */
export function parseTags(text: string): string[] {
  const seen = new Set<string>()
  for (const part of text.split(/[,，\n]/)) {
    const name = part.trim()
    if (name !== '') seen.add(name)
  }
  return [...seen]
}

export type BatchTagsResult = Schema<'BatchTagsResult'>

export const batchTagImages = (
  ids: string[],
  add: string[],
  remove: string[]
) =>
  api.post<BatchTagsResult>('/api/admin/images/batch-tags', {
    ids,
    add,
    remove,
  })
