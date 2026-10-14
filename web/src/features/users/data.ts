import { queryOptions } from '@tanstack/react-query'
import { api, type ListResponse, type Schema } from '@/lib/api'

export type UserRole = Schema<'Role'>

/** 用户列表项(对应 GET /api/admin/users),不含任何凭证材料。 */
export type UserItem = Schema<'User'>

/** 建号 / 编辑的请求体;quota_bytes 为 0 表示不限,default_ttl_hours 为 null 表示随全局。 */
export type UserWriteBody = Schema<'UserWriteRequest'>

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
