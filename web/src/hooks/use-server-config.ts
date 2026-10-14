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
  build: BuildInfo
}

/** 服务端二进制的构建信息。 */
export interface BuildInfo {
  version: string // 版本号:发布版为 git 标签;源码构建为 Go 推导的模块版本;无从得知时为 devel
  commit: string // 构建所在提交的哈希,有未提交改动时带 -dirty 后缀;未知为空串
  built_at: string // 构建时刻,RFC 3339;只有发布流水线注入,源码构建为空串
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
