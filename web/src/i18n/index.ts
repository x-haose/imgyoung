import { getCookie, setCookie } from '@/lib/cookies'
import { en } from './en'
import { type Messages, zhCN } from './zh-CN'

export type { Messages }

/** 支持的界面语言;简体中文为词典基准。 */
export const locales = ['zh-CN', 'en'] as const
export type Locale = (typeof locales)[number]

/** 各语言在切换菜单里的自称。 */
export const localeNames: Record<Locale, string> = {
  'zh-CN': '简体中文',
  en: 'English',
}

const dictionaries: Record<Locale, Messages> = { 'zh-CN': zhCN, en }

const LOCALE_COOKIE = 'imgyoung-locale'
const LOCALE_COOKIE_MAX_AGE = 60 * 60 * 24 * 365

function isLocale(v: string | undefined): v is Locale {
  return v !== undefined && (locales as readonly string[]).includes(v)
}

/** 初始语言:已保存的偏好优先,否则按浏览器语言,中文环境用简体中文,其余用英文。 */
function detectLocale(): Locale {
  const saved = getCookie(LOCALE_COOKIE)
  if (isLocale(saved)) return saved
  return navigator.language.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en'
}

let current: Locale = detectLocale()

/** 当前语言;React 树外的代码(请求层、格式化)据此取文案。 */
export function getLocale(): Locale {
  return current
}

/** 切换当前语言并持久化;界面随 LocaleProvider 的状态更新重绘。 */
export function setCurrentLocale(locale: Locale): void {
  current = locale
  setCookie(LOCALE_COOKIE, locale, LOCALE_COOKIE_MAX_AGE)
}

/** 当前语言的词典。 */
export function messages(): Messages {
  return dictionaries[current]
}

/** 把模板中的 {name} 占位符替换为参数值;没给值的占位符原样保留。 */
export function t(
  template: string,
  params: Record<string, string | number>
): string {
  return template.replace(/\{(\w+)\}/g, (match, key: string) =>
    key in params ? String(params[key]) : match
  )
}
