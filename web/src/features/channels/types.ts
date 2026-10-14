/** 渠道模块的接口类型,与后端 channel.Info / channel.Template / channel.EditConfig 的 JSON 形态对齐。 */

/** 渠道的副本补齐水位(按副本状态聚合)。 */
export interface ReplicaStats {
  done: number // 已就绪副本数
  pending: number // 待补齐副本数(含补齐进行中)
  failed: number // 补齐失败副本数
}

/** 渠道列表项(凭证与配置不出库)。 */
export interface ChannelItem {
  id: number
  name: string
  driver: string // rclone 后端名
  priority: number // 越大越优先
  healthy: boolean
  encrypted: boolean
  allow_direct_link: boolean
  last_checked_at: string // 最近探测时刻,UTC 文本;空 = 尚未探测
  last_error: string // 最近探测失败原因;空 = 最近一次探测成功
  deleting: boolean // 清理中:排空任务正在删除渠道上的对象,清完即消失
  replicas: ReplicaStats
}

export type FieldType = 'text' | 'password' | 'select' | 'oauth'

export interface FieldOption {
  label: string
  value: string
}

/** 模板的一个用户输入字段;标签与提示由后端下发。 */
export interface TemplateField {
  key: string
  label: string
  type: FieldType
  required: boolean
  placeholder?: string
  help?: string
  options?: FieldOption[] // select 字段的选项
}

/** 渠道模板(后端下发,不含 provider 等预设参数)。 */
export interface ChannelTemplate {
  id: string
  name: string
  description: string
  driver: string // rclone 后端名
  category: string // object_storage / drive
  requires_oauth: boolean // 须先完成授权才能保存
  fields: TemplateField[]
}

/** 单渠道配置的编辑视图;敏感值已替换为哨兵。 */
export interface EditConfig {
  template: string // 配置所用模板 id;空 = 自定义
  driver: string
  config: Record<string, string> // 掩码后的后端参数
  inputs?: Record<string, string> // 模板表单字段值;仅模板渠道
}

/** 测试连接的结果。 */
export interface TestResult {
  ok: boolean
  error?: string // 失败原因,不含凭证
}

/** 敏感配置值的回读哨兵:原样回传表示保留原凭证,与后端 channel.SecretPlaceholder 一致。 */
export const SECRET_PLACEHOLDER = '__imgyoung_secret_keep__'

/** 自定义模式的模板 id;后端按此值走 driver + config 直填路径。 */
export const CUSTOM = 'custom'

/** 网盘类模板的分类值;网盘无桶也无稳定公开直链,相关开关对其不显示。 */
export const CATEGORY_DRIVE = 'drive'

/** 授权回调路径,须与各开放平台登记的回调地址逐字符一致。 */
export const OAUTH_CALLBACK_PATH = '/api/admin/channel-oauth/callback'
