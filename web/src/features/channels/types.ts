/** 渠道模块的接口类型与常量;结构来自接口文档的生成类型。 */
import { type Schema } from '@/lib/api'

/** 渠道的副本补齐水位(按副本状态聚合)。 */
export type ReplicaStats = Schema<'ReplicaStats'>

/** 渠道列表项(凭证与配置不出库)。 */
export type ChannelItem = Schema<'Channel'>

/** 模板字段的输入类型。 */
export type FieldType = Schema<'TemplateField'>['type']

export type FieldOption = Schema<'FieldOption'>

/** 模板的一个用户输入字段;标签与提示由后端下发。 */
export type TemplateField = Schema<'TemplateField'>

/** 渠道模板(后端下发,不含 provider 等预设参数)。 */
export type ChannelTemplate = Schema<'ChannelTemplate'>

/** 单渠道配置的编辑视图;敏感值已替换为哨兵。 */
export type EditConfig = Schema<'ChannelEditConfig'>

/** 测试连接的结果。 */
export type TestResult = Schema<'ChannelTestResult'>

/** 敏感配置值的回读哨兵:原样回传表示保留原凭证,与后端 channel.SecretPlaceholder 一致。 */
export const SECRET_PLACEHOLDER = '__imgyoung_secret_keep__'

/** 自定义模式的模板 id;后端按此值走 driver + config 直填路径。 */
export const CUSTOM = 'custom'

/** 网盘类模板的分类值;网盘无桶也无稳定公开直链,相关开关对其不显示。 */
export const CATEGORY_DRIVE = 'drive'

/** 授权令牌参数的键,与后端 channel.ConfigKeyToken 一致;自定义模式里令牌就在这个参数下。 */
export const TOKEN_KEY = 'token'

/** 需授权模板里自建应用标识字段的 key,与后端 channel.ConfigKeyClientID 一致;一键授权须先填它。 */
export const CLIENT_ID_KEY = 'client_id'

/** 授权回调路径,须与各开放平台登记的回调地址逐字符一致。 */
export const OAUTH_CALLBACK_PATH = '/api/admin/channel-oauth/callback'
