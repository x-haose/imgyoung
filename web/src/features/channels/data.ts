import { queryOptions, useQueryClient } from '@tanstack/react-query'
import { getLocale } from '@/i18n'
import { api, type ListResponse, type Schema } from '@/lib/api'
import type {
  ChannelItem,
  ChannelTemplate,
  CUSTOM,
  EditConfig,
  TestResult,
} from './types'

export const channelKeys = {
  all: ['channels'] as const,
  list: () => [...channelKeys.all, 'list'] as const,
  config: (id: number) => [...channelKeys.all, 'config', id] as const,
  templates: ['channel-templates'] as const,
}

export const channelsQueryOptions = () =>
  queryOptions({
    queryKey: channelKeys.list(),
    queryFn: async () =>
      (await api.get<ListResponse<ChannelItem>>('/api/admin/channels')).items,
  })

/** 模板清单是编译期常量,会话期内只取一次。 */
export const templatesQueryOptions = () =>
  queryOptions({
    queryKey: channelKeys.templates,
    queryFn: async () =>
      (
        await api.get<ListResponse<ChannelTemplate>>(
          '/api/admin/channel-templates'
        )
      ).items,
    staleTime: Infinity,
  })

/** 编辑回显;掩码后的配置不留缓存,每次打开表单都重新回读。 */
export const channelConfigQueryOptions = (id: number) =>
  queryOptions({
    queryKey: channelKeys.config(id),
    queryFn: () => api.get<EditConfig>(`/api/admin/channels/${id}/config`),
    staleTime: 0,
    gcTime: 0,
  })

/** 配置来源,创建 / 编辑 / 测试同构:模板模式给模板 id 与字段值,自定义模式给后端名与参数。 */
export type ChannelSource =
  | { template: string; inputs: Record<string, string> }
  | {
      template: typeof CUSTOM
      driver: string
      config: Record<string, string>
    }

export type ChannelCreateBody = ChannelSource & {
  name: string
  priority: number
  encrypted?: boolean // 缺省用全局默认
  allow_direct_link: boolean
  auto_create_bucket: boolean
}

export type ChannelUpdateBody = ChannelSource & {
  name: string
  priority: number
  allow_direct_link: boolean
}

/** 测试连接:编辑场景带渠道主键,配置中的哨兵值由后端还原为原凭证后再探测。 */
export type ChannelTestBody = ChannelCreateBody & { channel_id?: number }

export const createChannel = (body: ChannelCreateBody) =>
  api.post<ChannelItem>('/api/admin/channels', body)

export const updateChannel = (id: number, body: ChannelUpdateBody) =>
  api.put<ChannelItem>(`/api/admin/channels/${id}`, body)

export const testChannel = (body: ChannelTestBody) =>
  api.post<TestResult>('/api/admin/channels/test', body)

export const checkChannel = (id: number) =>
  api.post<ChannelItem>(`/api/admin/channels/${id}/check`)

/** 下载渠道的 rclone 配置文件;文件头注释的语言随当前界面语言。 */
export function downloadRcloneConfig(id: number) {
  return api.download(`/api/admin/channels/${id}/rclone-config`, {
    lang: getLocale(),
  })
}

/** purge 为真时渠道先转为清理中,对象删完才移除;为假时立即移除、远端对象保留。 */
export const deleteChannel = (id: number, purge: boolean) =>
  api.delete(`/api/admin/channels/${id}?purge=${purge}`)

export type OAuthStartBody = Schema<'OAuthStartRequest'>

export type OAuthStartResponse = Schema<'OAuthStartResponse'>

export type OAuthResultResponse = Schema<'OAuthResult'>

export const startOAuth = (body: OAuthStartBody) =>
  api.post<OAuthStartResponse>('/api/admin/channel-oauth/start', body)

export const fetchOAuthResult = (state: string) =>
  api.get<OAuthResultResponse>('/api/admin/channel-oauth/result', { state })

/** 渠道变更后列表与总览(渠道水位)一起失效。 */
export function useInvalidateChannels() {
  const queryClient = useQueryClient()
  return () => {
    void queryClient.invalidateQueries({ queryKey: channelKeys.all })
    void queryClient.invalidateQueries({ queryKey: ['dashboard'] })
  }
}
