import { queryOptions, useQuery } from '@tanstack/react-query'
import { api, type Schema } from '@/lib/api'

/** 服务端生效运行配置的只读切面(对应 GET /api/admin/config,全部登录用户可读)。 */
export type ServerConfig = Schema<'RuntimeConfig'>

/** 服务端二进制的构建信息。 */
export type BuildInfo = Schema<'BuildInfo'>

export const serverConfigQueryOptions = () =>
  queryOptions({
    queryKey: ['server-config'],
    queryFn: () => api.get<ServerConfig>('/api/admin/config'),
    staleTime: Infinity,
  })

/** 服务端生效配置,会话期内只取一次;加载中或失败返回 undefined,调用方以内置默认值兜底。 */
export function useServerConfig(): ServerConfig | undefined {
  return useQuery(serverConfigQueryOptions()).data
}
