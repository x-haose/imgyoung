import { queryOptions, useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'

/** 服务端生效运行配置的只读切面(对应 GET /api/admin/config,全部登录用户可读)。 */
export interface ServerConfig {
  cold_cleanup_days: number // 冷图待清理阈值(天);0 表示未启用
  delete_grace_minutes: number // 引用归零到物理删的宽限(分钟);0 表示立即
  process_widths: number[] | null // 按需处理允许的宽度档白名单;空 / null 表示不允许缩放
  default_ttl_hours: number // 全局默认 TTL(小时);0 表示永不过期
  ttl_renew_on_access: boolean // 公开访问是否顺延带 TTL 图片的到期时刻
  max_upload_mb: number // 单图上限(MB)
  base_url: string // 对外站点地址;空则按请求域名推导绝对地址
  access_mode: string // 访问模式:proxy / redirect
  exif_policy: string // 全局 EXIF 档位:passthrough / strip
}

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
