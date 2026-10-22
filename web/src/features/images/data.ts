/** 图片页数据层:契约类型、查询定义与写操作,全部经会话态管理 API。 */
import { queryOptions } from '@tanstack/react-query'
import { type Messages, t } from '@/i18n'
import { api, type ListResponse, type QueryValue, type Schema } from '@/lib/api'
import { parseUTC, toUTCText } from '@/lib/format'
import { type ImagesSearch, PAGE_SIZE } from './search'

/** 图片列表 / 详情项(对应 /api/admin/images 响应)。 */
export type ImageItem = Schema<'Image'>

/** 单图在一个渠道上的副本状态。 */
export type ReplicaItem = Schema<'Replica'>

/** 图片详情:列表项字段之外附带副本分布,按渠道优先级降序。 */
export type ImageDetail = Schema<'ImageDetail'>

/** 批量删除响应。 */
export type BatchDeleteResult = Schema<'BatchDeleteResult'>

/** 上传与转存的成功响应;url 已是绝对地址。 */
export type UploadResult = Schema<'UploadResult'>

/** 筛选下拉所需的 Token 切面。 */
export type TokenOption = Pick<Schema<'Token'>, 'id' | 'name'>

/** 筛选下拉所需的用户切面(仅管理员)。 */
export type UserOption = Pick<Schema<'User'>, 'id' | 'name'>

/** 列表查询参数,键名与后端查询串一致。 */
export interface ImageListParams {
  page: number
  page_size: number
  search?: string
  user_id?: string
  token_id?: number
  since?: string // UTC 文本,闭区间下界
  until?: string // UTC 文本,开区间上界
  exempt?: 1
}

/** 单次批量删除的张数上限,与后端一致,超限整请求 400。 */
export const BATCH_DELETE_MAX = 100

/** 筛选下拉一次拉取的上限;筛选场景不分页。 */
const OPTION_PAGE_SIZE = 100

/** 后端允许的 EXIF 档位;空表示跟随站点默认。 */
export const EXIF_POLICIES = ['strip', 'passthrough', 'spoof'] as const
export type ExifPolicy = (typeof EXIF_POLICIES)[number]

/** 本地日期(YYYY-MM-DD)当天零点的毫秒时间戳;offsetDays 为向后平移的天数。 */
function localDayStart(day: string, offsetDays = 0): number {
  const [y, m, d] = day.split('-').map(Number)
  return new Date(y, m - 1, d + offsetDays).getTime()
}

/** 把地址栏状态翻译成后端列表参数;时间范围按本地日界取闭开区间 [首日 00:00, 末日次日 00:00)。 */
export function toListParams(s: ImagesSearch): ImageListParams {
  return {
    page: s.page,
    page_size: PAGE_SIZE,
    search: s.q,
    user_id: s.user,
    token_id: s.token || undefined,
    since: s.since ? toUTCText(localDayStart(s.since)) : undefined,
    until: s.until ? toUTCText(localDayStart(s.until, 1)) : undefined,
    exempt: s.exempt ? 1 : undefined,
  }
}

/** 到期时刻是否已过;永久图恒为 false。 */
export function isExpired(expireAt: string | null): boolean {
  const d = parseUTC(expireAt)
  return d !== null && d.getTime() < Date.now()
}

/** 对外分享地址:配置了 base_url 用它,否则按当前站点推导。 */
export function shareURL(path: string, baseURL: string | undefined): string {
  return new URL(path, baseURL || window.location.origin).href
}

/** 分享链接的复制格式;名称是格式本身的专名,不翻译。 */
export const copyFormats = [
  { key: 'url', label: 'URL', build: (u: string) => u },
  { key: 'markdown', label: 'Markdown', build: (u: string) => `![](${u})` },
  {
    key: 'html',
    label: 'HTML',
    build: (u: string) => `<img src="${u}" alt="" />`,
  },
  { key: 'bbcode', label: 'BBCode', build: (u: string) => `[img]${u}[/img]` },
] as const

export const imagesQueryKey = ['images'] as const

export const imagesQueryOptions = (params: ImageListParams) =>
  queryOptions({
    queryKey: [...imagesQueryKey, 'list', params] as const,
    queryFn: () =>
      api.get<ListResponse<ImageItem>>(
        '/api/admin/images',
        params as unknown as Record<string, QueryValue>
      ),
  })

export const imageQueryOptions = (id: string) =>
  queryOptions({
    queryKey: [...imagesQueryKey, 'detail', id] as const,
    queryFn: () => api.get<ImageDetail>(`/api/admin/images/${id}`),
  })

/** Token 选项:管理员指定了用户则取该用户的,否则取本人的(后端同一语义)。 */
export const tokenOptionsQueryOptions = (userId?: string) =>
  queryOptions({
    queryKey: ['tokens', 'options', userId ?? ''] as const,
    queryFn: () =>
      api.get<ListResponse<TokenOption>>('/api/admin/tokens', {
        page_size: OPTION_PAGE_SIZE,
        user_id: userId,
      }),
    staleTime: 60_000,
  })

export const userOptionsQueryOptions = () =>
  queryOptions({
    queryKey: ['users', 'options'] as const,
    queryFn: () =>
      api.get<ListResponse<UserOption>>('/api/admin/users', {
        page_size: OPTION_PAGE_SIZE,
      }),
    staleTime: 60_000,
  })

/** PATCH 修改项;ttl_hours 为 0 表示改永久,cleanup_exempt 仅管理员可带。 */
export type ImagePatch = Schema<'ImagePatch'>

export const patchImage = (id: string, patch: ImagePatch) =>
  api.patch<ImageDetail>(`/api/admin/images/${id}`, patch)

export const deleteImage = (id: string) => api.delete(`/api/admin/images/${id}`)

export const batchDeleteImages = (ids: string[]) =>
  api.post<BatchDeleteResult>('/api/admin/images/batch-delete', { ids })

/** 上传可选项;ttl 为秒数字符串或 "never",缺省跟随用户 / 站点默认;exif 缺省跟随站点默认。 */
export interface UploadOptions {
  ttl?: string
  exif?: ExifPolicy
}

export const uploadFile = (file: File, name: string, opts: UploadOptions) => {
  const fd = new FormData()
  fd.append('file', file, name)
  if (opts.ttl !== undefined) fd.append('ttl', opts.ttl)
  if (opts.exif !== undefined) fd.append('exif_policy', opts.exif)
  return api.form<UploadResult>('/api/admin/upload', fd)
}

export const uploadFromURL = (url: string, opts: UploadOptions) =>
  api.post<UploadResult>('/api/admin/upload/url', {
    url,
    ...(opts.ttl !== undefined ? { ttl: opts.ttl } : {}),
    ...(opts.exif !== undefined ? { exif_policy: opts.exif } : {}),
  })

/** 删除确认文案:宽限为 0 或配置未加载时不承诺不存在的宽限期。 */
export function deleteWarning(
  m: Messages['images'],
  graceMinutes: number | undefined
): string {
  return graceMinutes
    ? t(m.deleteWarning, { minutes: graceMinutes })
    : m.deleteWarningImmediate
}
