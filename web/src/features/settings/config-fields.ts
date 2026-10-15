/**
 * 运行配置表单的字段注册表:分组、控件类型、取值范围与单位都在这里,表单按它渲染与校验。
 * 取值范围与后端 config 包的校验一致,后端是最终裁决,这里只为就地提示。
 */
import { type Messages, t } from '@/i18n'
import { type RuntimeSettings } from './data'

type ConfigMessages = Messages['settings']['config']

export type FieldKey = Exclude<keyof RuntimeSettings, 'allow_empty_referer'>

export type GroupKey = keyof ConfigMessages['groups']

type UnitKey = keyof ConfigMessages['units']

/** 控件类型:int 数值;text 单行文本;url 带主机的 http/https 地址;bool 开关;select 枚举;widths 宽度档;formats 格式多选;secret 只写不回显的凭证。 */
export type FieldDef =
  | { key: FieldKey; kind: 'int'; min: number; max?: number; unit?: UnitKey }
  | { key: FieldKey; kind: 'text' | 'url' | 'secret' }
  | { key: FieldKey; kind: 'bool' }
  | { key: FieldKey; kind: 'select'; options: readonly string[] }
  | { key: FieldKey; kind: 'widths' }
  | { key: FieldKey; kind: 'formats'; options: readonly string[] }

export interface FieldGroup {
  key: GroupKey
  fields: FieldDef[]
}

/** 按需处理支持的目标格式,与后端 imaging 包一致。 */
const PROCESS_FORMATS = ['webp', 'jpeg', 'png'] as const

export const CONFIG_GROUPS: FieldGroup[] = [
  {
    key: 'access',
    fields: [
      { key: 'access_mode', kind: 'select', options: ['proxy', 'redirect'] },
      { key: 'base_url', kind: 'url' },
      { key: 'min_success', kind: 'int', min: 1, unit: 'replicas' },
      { key: 'encrypt_new_channels', kind: 'bool' },
    ],
  },
  {
    key: 'upload',
    fields: [
      { key: 'max_upload_mb', kind: 'int', min: 1, unit: 'mb' },
      { key: 'max_pixels_mp', kind: 'int', min: 1, unit: 'megapixels' },
      { key: 'max_concurrent_uploads', kind: 'int', min: 1, unit: 'count' },
      { key: 'upload_rate_per_min', kind: 'int', min: 0, unit: 'perMinute' },
    ],
  },
  {
    key: 'lifecycle',
    fields: [
      { key: 'default_ttl_hours', kind: 'int', min: 0, unit: 'hours' },
      { key: 'ttl_renew_on_access', kind: 'bool' },
      { key: 'cold_cleanup_days', kind: 'int', min: 0, unit: 'days' },
      { key: 'delete_grace_minutes', kind: 'int', min: 0, unit: 'minutes' },
      { key: 'cleanup_sweep_seconds', kind: 'int', min: 1, unit: 'seconds' },
      { key: 'activity_flush_seconds', kind: 'int', min: 1, unit: 'seconds' },
      { key: 'idempotency_key_ttl_hours', kind: 'int', min: 1, unit: 'hours' },
    ],
  },
  {
    key: 'process',
    fields: [
      { key: 'process_widths', kind: 'widths' },
      { key: 'process_formats', kind: 'formats', options: PROCESS_FORMATS },
      { key: 'exif_policy', kind: 'select', options: ['passthrough', 'strip'] },
      {
        key: 'process_quality_webp',
        kind: 'int',
        min: 0,
        max: 100,
      },
      {
        key: 'process_quality_jpeg',
        kind: 'int',
        min: 1,
        max: 100,
      },
      {
        key: 'process_webp_method',
        kind: 'int',
        min: 0,
        max: 6,
      },
      { key: 'process_workers', kind: 'int', min: 1, unit: 'count' },
      { key: 'process_timeout_ms', kind: 'int', min: 1, unit: 'ms' },
      { key: 'variants_cache_mb', kind: 'int', min: 1, unit: 'mb' },
    ],
  },
  {
    key: 'dedup',
    fields: [
      { key: 'dedup_scan_seconds', kind: 'int', min: 1, unit: 'seconds' },
      { key: 'dedup_phash_max_distance', kind: 'int', min: 0, unit: 'hamming' },
    ],
  },
  {
    key: 'reliability',
    fields: [
      { key: 'health_check_seconds', kind: 'int', min: 1, unit: 'seconds' },
      { key: 'backup_interval_hours', kind: 'int', min: 1, unit: 'hours' },
      { key: 'backup_keep', kind: 'int', min: 1, unit: 'count' },
      { key: 'usage_report_seconds', kind: 'int', min: 1, unit: 'seconds' },
      { key: 'channel_quota_gb', kind: 'int', min: 0, unit: 'gb' },
    ],
  },
  {
    key: 'cdn',
    fields: [
      { key: 'cdn_cloudflare_zone_id', kind: 'text' },
      { key: 'cdn_cloudflare_api_token', kind: 'secret' },
    ],
  },
  {
    key: 'alerts',
    fields: [
      { key: 'alert_webhook_url', kind: 'url' },
      { key: 'audit_retention_days', kind: 'int', min: 0, unit: 'days' },
    ],
  },
]

/** 表单里一个字段的值:数值字段清空时为空串;宽度档是逗号分隔文本;凭证字段空串表示不改。 */
export type FieldValue = number | string | boolean | string[]

export type FormValues = Record<FieldKey, FieldValue>

/** 服务端配置 → 表单基线:宽度档转成文本,凭证输入框恒置空。 */
export function baselineValues(s: RuntimeSettings): FormValues {
  const values = {} as FormValues
  for (const group of CONFIG_GROUPS) {
    for (const def of group.fields) {
      values[def.key] = baselineOf(def, s)
    }
  }
  return values
}

function baselineOf(def: FieldDef, s: RuntimeSettings): FieldValue {
  switch (def.kind) {
    case 'widths':
      return (s.process_widths ?? []).join(', ')
    case 'formats':
      return [...(s.process_formats ?? [])]
    case 'secret':
      return ''
    default:
      return s[def.key] as FieldValue
  }
}

/** 解析宽度档文本为正整数数组(去重、升序);含非法项返回 null。 */
export function parseWidths(text: string): number[] | null {
  const parts = text
    .split(/[,，\s]+/)
    .map((p) => p.trim())
    .filter((p) => p !== '')
  const widths = new Set<number>()
  for (const part of parts) {
    if (!/^\d+$/.test(part)) return null
    const n = Number(part)
    if (n < 1) return null
    widths.add(n)
  }
  return [...widths].sort((a, b) => a - b)
}

function isHTTPURL(raw: string): boolean {
  try {
    const u = new URL(raw)
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.host !== ''
  } catch {
    return false
  }
}

/** 单个字段的就地校验;合法返回 undefined。 */
export function validateField(
  def: FieldDef,
  value: FieldValue,
  m: ConfigMessages
): string | undefined {
  switch (def.kind) {
    case 'int': {
      if (value === '') return m.required
      if (typeof value !== 'number' || !Number.isInteger(value)) {
        return m.integer
      }
      if (value < def.min) return t(m.min, { min: def.min })
      if (def.max !== undefined && value > def.max) {
        return t(m.max, { max: def.max })
      }
      return undefined
    }
    case 'widths':
      return parseWidths(String(value)) === null ? m.widthsInvalid : undefined
    case 'formats':
      return Array.isArray(value) && value.length > 0
        ? undefined
        : m.formatsRequired
    case 'url': {
      const raw = String(value).trim()
      return raw === '' || isHTTPURL(raw) ? undefined : m.urlInvalid
    }
    default:
      return undefined
  }
}

/** 表单值 → 提交给后端的取值:宽度档解析为数组,文本去首尾空白。 */
export function toPatchValue(def: FieldDef, value: FieldValue): unknown {
  switch (def.kind) {
    case 'widths':
      return parseWidths(String(value)) ?? []
    case 'formats':
      return [...(value as string[])].sort()
    case 'text':
    case 'url':
    case 'secret':
      return String(value).trim()
    default:
      return value
  }
}

/** 一个字段相对基线是否有实质改动;凭证字段只要填了就算改动。 */
export function isChanged(
  def: FieldDef,
  value: FieldValue,
  baseline: FieldValue
): boolean {
  if (def.kind === 'secret') return String(value).trim() !== ''
  return (
    JSON.stringify(toPatchValue(def, value)) !==
    JSON.stringify(toPatchValue(def, baseline))
  )
}

/** 全部字段定义的平铺视图。 */
export const CONFIG_FIELDS: FieldDef[] = CONFIG_GROUPS.flatMap((g) => g.fields)
