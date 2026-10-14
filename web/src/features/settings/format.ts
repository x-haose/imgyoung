import { getLocale } from '@/i18n'
import { PLACEHOLDER } from '@/lib/format'

/** RFC 3339 时刻 → 本地时间,24 小时制;不可解析原样返回。 */
export function formatInstant(iso: string): string {
  if (!iso) return PLACEHOLDER
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return new Intl.DateTimeFormat(getLocale(), {
    dateStyle: 'medium',
    timeStyle: 'medium',
    hour12: false,
  }).format(d)
}
