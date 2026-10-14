import { queryOptions } from '@tanstack/react-query'
import { api, type ListResponse } from '@/lib/api'

export type UserRole = 'admin' | 'user'

/** 用户列表项(对应 GET /api/admin/users),不含任何凭证材料。 */
export interface UserItem {
  id: string
  name: string
  role: UserRole
  quota_bytes: number | null // 配额上限;null 表示不限
  default_ttl_hours: number | null // 上传默认有效期(小时);null 随全局,0 永久
  disabled: boolean
  created_at: string
  used_bytes: number // 已用字节,按引用计全额
  image_count: number
}

/** 建号 / 编辑的请求体;quota_bytes 为 0 表示不限,default_ttl_hours 为 null 表示随全局。 */
export interface UserWriteBody {
  name: string
  password?: string // 仅建号时
  role: UserRole
  quota_bytes: number
  default_ttl_hours: number | null
  disabled?: boolean // 仅编辑时
}

/** 用户默认有效期上限(小时,100 年),与服务端校验同口径。 */
export const USER_TTL_MAX_HOURS = 876000

/** 每页用户条数的缺省值(客户端分页)。 */
export const USERS_PAGE_SIZE = 20

export const usersQueryKey = ['users'] as const

/** 全部用户及其用量;后端不分页,一次取全。 */
export const usersQueryOptions = () =>
  queryOptions({
    queryKey: usersQueryKey,
    queryFn: () => api.get<ListResponse<UserItem>>('/api/admin/users'),
  })

export const createUser = (body: UserWriteBody) =>
  api.post<UserItem>('/api/admin/users', body)

export const updateUser = (id: string, body: UserWriteBody) =>
  api.put(`/api/admin/users/${id}`, body)

export const resetUserPassword = (id: string, password: string) =>
  api.post(`/api/admin/users/${id}/password`, { password })

export const deleteUser = (id: string) => api.delete(`/api/admin/users/${id}`)
