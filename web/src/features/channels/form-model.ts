/** 渠道配置表单的数据模型:表单值、校验规则、初值与请求体映射。 */
import { z } from 'zod'
import { type Messages, t } from '@/i18n'
import { type ChannelSource } from './data'
import {
  type ChannelItem,
  type ChannelTemplate,
  CUSTOM,
  type EditConfig,
} from './types'

/** 创建时服务端加密的三态;继承态不提交该字段,由后端按全局默认固化。 */
export type EncryptChoice = 'inherit' | 'on' | 'off'

/** 渠道最终是否加密:plain 不加密;inherited 因继承全局默认而加密;explicit 显式或固有加密。 */
export type EncryptionState = 'plain' | 'inherited' | 'explicit'

export type ConfigEntry = { key: string; value: string }

export type FormValues = {
  name: string
  priority: number
  encrypted: EncryptChoice
  allow_direct_link: boolean
  auto_create_bucket: boolean
  inputs: Record<string, string> // 模板字段值,键为字段 key
  driver: string // 自定义模式
  config: ConfigEntry[] // 自定义模式
}

export const PRIORITY_MAX = 1000
const NAME_MAX_BYTES = 64

/** 校验规则随模板变化:模板必填字段逐个检查,自定义模式检查驱动名与参数键。 */
export function buildSchema(
  m: Messages['channels'],
  template: ChannelTemplate | null,
  encryption: (choice: EncryptChoice) => EncryptionState
) {
  const priorityMessage = m.priorityRange
  return z
    .object({
      name: z
        .string()
        .trim()
        .min(1, m.nameRequired)
        .refine(
          (v) => new TextEncoder().encode(v).length <= NAME_MAX_BYTES,
          m.nameTooLong
        ),
      priority: z
        .number({ error: priorityMessage })
        .int(priorityMessage)
        .min(0, priorityMessage)
        .max(PRIORITY_MAX, priorityMessage),
      encrypted: z.enum(['inherit', 'on', 'off']),
      allow_direct_link: z.boolean(),
      auto_create_bucket: z.boolean(),
      inputs: z.record(z.string(), z.string()),
      driver: z.string(),
      config: z.array(z.object({ key: z.string(), value: z.string() })),
    })
    .superRefine((v, ctx) => {
      if (template) {
        for (const f of template.fields) {
          if (f.required && (v.inputs[f.key] ?? '').trim() === '') {
            ctx.addIssue({
              code: 'custom',
              path: ['inputs', f.key],
              message: t(m.fieldRequired, { label: f.label }),
            })
          }
        }
      } else {
        if (v.driver.trim() === '') {
          ctx.addIssue({
            code: 'custom',
            path: ['driver'],
            message: m.driverRequired,
          })
        }
        const seen = new Set<string>()
        v.config.forEach((kv, i) => {
          const key = kv.key.trim()
          if (key === '') {
            ctx.addIssue({
              code: 'custom',
              path: ['config', i, 'key'],
              message: m.configKeyRequired,
            })
          } else if (seen.has(key)) {
            ctx.addIssue({
              code: 'custom',
              path: ['config', i, 'key'],
              message: m.configKeyDuplicate,
            })
          }
          seen.add(key)
        })
      }
      const state = encryption(v.encrypted)
      if (state !== 'plain' && v.allow_direct_link) {
        ctx.addIssue({
          code: 'custom',
          path: ['allow_direct_link'],
          message:
            state === 'inherited'
              ? m.directLinkInheritEncrypted
              : m.directLinkEncrypted,
        })
      }
    })
}

/** 模板字段全部以受控空串起步,未碰过的字段不会以 undefined 进入校验。 */
export function defaultValues(
  template: ChannelTemplate | null,
  channel: ChannelItem | null,
  prefill: EditConfig | undefined
): FormValues {
  const inputs: Record<string, string> = {}
  for (const f of template?.fields ?? []) {
    inputs[f.key] = prefill?.inputs?.[f.key] ?? ''
  }
  return {
    name: channel?.name ?? '',
    priority: channel?.priority ?? 100,
    encrypted: 'inherit',
    allow_direct_link: channel?.allow_direct_link ?? false,
    auto_create_bucket: false,
    inputs,
    driver: prefill?.driver ?? '',
    config: Object.entries(prefill?.config ?? {}).map(([key, value]) => ({
      key,
      value,
    })),
  }
}

/** 配置部分的请求体:模板模式提交字段值,自定义模式提交驱动与参数(去掉空键)。 */
export function toSource(
  values: FormValues,
  template: ChannelTemplate | null
): ChannelSource {
  if (template) {
    const inputs: Record<string, string> = {}
    for (const f of template.fields) {
      const v = (values.inputs[f.key] ?? '').trim()
      if (v !== '') inputs[f.key] = v
    }
    return { template: template.id, inputs }
  }
  const config: Record<string, string> = {}
  for (const kv of values.config) {
    const key = kv.key.trim()
    if (key !== '') config[key] = kv.value
  }
  return { template: CUSTOM, driver: values.driver.trim(), config }
}
