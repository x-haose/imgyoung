/** 展示层格式化:字节、时刻、相对时间与图片地址,按当前界面语言输出。 */
import { formatDistanceToNowStrict } from 'date-fns'
import { getLocale, type Locale } from '@/i18n'
import { enUS, zhCN } from 'date-fns/locale'

const dateFnsLocales = { 'zh-CN': zhCN, en: enUS } satisfies Record<
  Locale,
  unknown
>

/** 空值与不可用值的占位符。 */
export const PLACEHOLDER = '—'

/** 解析后端 UTC 文本时间(YYYY-MM-DD HH:MM:SS);空值或不可解析返回 null。 */
export function parseUTC(utc: string | null | undefined): Date | null {
  if (!utc) return null
  const d = new Date(utc.replace(' ', 'T') + 'Z')
  return Number.isNaN(d.getTime()) ? null : d
}

/** 把本地时刻(毫秒时间戳)转为后端可比较的 UTC 文本时间(YYYY-MM-DD HH:MM:SS)。 */
export function toUTCText(ms: number): string {
  return new Date(ms).toISOString().slice(0, 19).replace('T', ' ')
}

/** 后端 UTC 文本时间 → 本地时刻,24 小时制;空值显示占位符,不可解析原样返回。 */
export function formatTime(utc: string | null | undefined): string {
  if (!utc) return PLACEHOLDER
  const d = parseUTC(utc)
  if (!d) return utc
  return new Intl.DateTimeFormat(getLocale(), {
    dateStyle: 'medium',
    timeStyle: 'medium',
    hour12: false,
  }).format(d)
}

/** 后端 UTC 文本时间 → 「N 分钟前」式相对描述;空值或不可解析返回空串。 */
export function formatRelativeTime(utc: string | null | undefined): string {
  const d = parseUTC(utc)
  if (!d) return ''
  return formatDistanceToNowStrict(d, {
    addSuffix: true,
    locale: dateFnsLocales[getLocale()],
  })
}

/** 整数按当前语言加千分位。 */
export function formatNumber(n: number | undefined | null): string {
  if (n === undefined || n === null) return PLACEHOLDER
  return new Intl.NumberFormat(getLocale()).format(n)
}

/** 字节 → 人类可读单位(B / KB / MB / GB / TB);空值显示占位符。 */
export function formatBytes(n: number | undefined | null): string {
  if (n === undefined || n === null) return PLACEHOLDER
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v >= 100 ? 0 : 1)} ${units[i]}`
}

/**
 * 按服务端宽度档白名单为图片地址拼缩放参数,选最接近 target 的档位。
 * 配置未加载(undefined)时直接用 target(与内置默认档位一致);
 * 已加载但白名单为空表示服务端不允许缩放,返回原图地址。
 */
export function thumbSrc(
  url: string,
  widths: number[] | null | undefined,
  target: number
): string {
  if (widths === undefined) return `${url}?w=${target}`
  if (widths === null || widths.length === 0) return url
  const best = widths.reduce((a, w) =>
    Math.abs(w - target) < Math.abs(a - target) ? w : a
  )
  return `${url}?w=${best}`
}

/**
 * 后台缩略图地址:走会话鉴权的管理端通道,浏览不计入公开访问统计,
 * 不会把冷图摘出待清理列表或抬高热门榜;对外分享链接仍用 /i/ 路径。
 */
export function adminThumbSrc(
  id: string,
  widths: number[] | null | undefined,
  target: number
): string {
  return thumbSrc(`/api/admin/images/${id}/thumb`, widths, target)
}

/** 把服务端返回的访问路径补全为可分享的绝对链接;已是绝对地址(配置了 base_url)时原样保留。 */
export function absoluteURL(path: string): string {
  return new URL(path, window.location.origin).href
}
