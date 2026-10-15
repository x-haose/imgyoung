/** 渠道模板的显示文案查询:服务端只下发结构,文案按模板 id、字段 key 与选项值查词典。 */
import { type FieldText, type Messages } from '@/i18n'
import { type FieldKey, type TemplateId } from './types'

type TemplateMessages = Messages['channelTemplates']

/** 模板专属文案表的统一视图:按模板 id、字段 key 取可缺省的覆盖文案。 */
type FieldOverrides = Partial<
  Record<TemplateId, Partial<Record<FieldKey, Partial<FieldText>>>>
>

/** 字段的显示文案:模板专属文案逐项覆盖通用文案。 */
export function fieldText(
  d: TemplateMessages,
  templateId: TemplateId,
  key: FieldKey
): FieldText {
  const base: FieldText = d.fields[key]
  const overrides: FieldOverrides = d.overrides
  return { ...base, ...overrides[templateId]?.[key] }
}
