import { i18n, type Language } from '@/lib/i18n'

/** 源码仓库地址,导航栏、页脚与文档页「在 GitHub 上查看」共用。 */
export const REPO_URL = 'https://github.com/x-haose/imgyoung'

/**
 * 站点绝对地址。
 *
 * `robots.txt`、`sitemap.xml` 与页面 `<link rel="alternate">` 都要绝对 URL,而它们在构建期生成,
 * 拿不到运行期的请求主机名。正式站与预览部署经 `NEXT_PUBLIC_SITE_URL` 指定;本地开发回落到
 * dev server 地址;生产构建缺了它直接失败,不生成指向错误域名的链接。
 */
function resolveSiteURL(): string {
  const configured = process.env.NEXT_PUBLIC_SITE_URL
  if (configured) return configured.replace(/\/$/, '')
  if (process.env.NODE_ENV === 'development') return 'http://localhost:3000'
  throw new Error(
    '构建文档站须设置 NEXT_PUBLIC_SITE_URL(站点对外的绝对地址,如 https://docs.example.com)'
  )
}

export const SITE_URL = resolveSiteURL()

/**
 * 一处内容在各语言下的绝对地址,键为 hreflang 值。
 *
 * 除各语言自身外附一条 `x-default` 指向默认语言,供搜索引擎在读者语言不在支持列表时选用;
 * sitemap 与页面 `<head>` 两处的 hreflang 由此保持同一份取值。
 * @param toUrl 把语言代码映射成该语言的地址
 */
export function languageAlternates(toUrl: (lang: Language) => string): Record<string, string> {
  return Object.fromEntries([
    ...i18n.languages.map((lang) => [lang, toUrl(lang)]),
    ['x-default', toUrl(i18n.defaultLanguage)],
  ])
}
