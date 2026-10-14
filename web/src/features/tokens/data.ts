import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { api, type ListResponse } from '@/lib/api'
import { parseUTC } from '@/lib/format'

/** Token 列表项(对应 GET /api/admin/tokens);只有识别前缀,无哈希无明文。 */
export interface TokenItem {
  id: number
  name: string
  prefix: string
  scopes: string
  expires_at: string | null // 到期时刻;null 表示永久
  last_used_at: string | null // 最近使用时刻;null 表示从未使用
  revoked_at: string | null // 吊销时刻;null 表示未吊销
  created_at: string
}

/** 签发 / 轮转的响应:token 为明文,仅此一次下发。 */
export interface IssuedToken {
  id: number
  name: string
  prefix: string
  token: string
}

/** 一枚 Token 的 S3 协议凭证与端点配置(对应 POST /api/admin/tokens/{id}/s3-credential)。 */
export interface S3Credential {
  access_key_id: string
  secret_access_key: string
  endpoint: string
  region: string
  bucket: string
  url_prefix: string
}

export type TokenStatus = 'active' | 'expired' | 'revoked'

/** Token 当前状态;吊销优先于过期,过期按 UTC 到期时刻与现在比较。 */
export function tokenStatus(row: TokenItem, now = Date.now()): TokenStatus {
  if (row.revoked_at) return 'revoked'
  const expires = parseUTC(row.expires_at)
  if (expires && expires.getTime() <= now) return 'expired'
  return 'active'
}

/** 用途名长度上限,与后端 tokenNameMaxLen 同值。 */
export const TOKEN_NAME_MAX_LEN = 64

/** 签发时可选的有效期档位(天);另有「永久」= 0 小时。 */
export const TOKEN_EXPIRY_DAYS = [7, 30, 90, 365] as const

export interface TokenListParams {
  page: number
  page_size: number
  user_id?: string // 管理员查看指定用户时传;缺省为本人
}

/** 每页 Token 条数的缺省值。 */
export const TOKENS_PAGE_SIZE = 20

export const tokensQueryKey = ['tokens'] as const

/** Token 分页列表;翻页时保留上一页数据,避免表格闪空。 */
export const tokensQueryOptions = (params: TokenListParams) =>
  queryOptions({
    queryKey: [...tokensQueryKey, params],
    queryFn: () =>
      api.get<ListResponse<TokenItem>>('/api/admin/tokens', {
        page: params.page,
        page_size: params.page_size,
        user_id: params.user_id,
      }),
    placeholderData: keepPreviousData,
  })

export const issueToken = (body: { name: string; expires_in_hours: number }) =>
  api.post<IssuedToken>('/api/admin/tokens', body)

export const rotateToken = (id: number) =>
  api.post<IssuedToken>(`/api/admin/tokens/${id}/rotate`)

export const revokeToken = (id: number) =>
  api.post(`/api/admin/tokens/${id}/revoke`)

export const deleteToken = (id: number) => api.delete(`/api/admin/tokens/${id}`)

/** 凭证由主密钥按需派生、不落库,故用 POST 取回而不缓存。 */
export const s3CredentialQueryOptions = (id: number) =>
  queryOptions({
    queryKey: [...tokensQueryKey, id, 's3-credential'],
    queryFn: () =>
      api.post<S3Credential>(`/api/admin/tokens/${id}/s3-credential`),
    staleTime: 0,
    gcTime: 0,
  })
