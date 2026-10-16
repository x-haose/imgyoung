import { i18n, type Language } from '@/lib/i18n'

/** 源码仓库地址,导航栏、页脚与文档页「在 GitHub 上查看」共用。 */
export const REPO_URL = 'https://github.com/x-haose/imgyoung'

/** 品牌名:中文写中文名「图样」并列英文名,英文只写英文名。 */
const SITE_NAME: Record<Language, string> = { zh: '图样 ImgYoung', en: 'ImgYoung' }

/**
 * 某语言下的品牌名,用于顶栏、页脚、页面标题与分享卡片。
 * @param lang 路由段上的语言;不在支持列表时按英文
 */
export function siteName(lang: string): string {
  return SITE_NAME[lang as Language] ?? SITE_NAME.en
}

/** 分享卡片图(1280×640,中英文并列)的替代文字。 */
const OG_IMAGE_ALT: Record<Language, string> = {
  zh: '图样 ImgYoung：多渠道主备容灾的自部署图床',
  en: 'ImgYoung: self-hosted image hosting with multi-channel failover',
}

/**
 * 分享卡片图,即 `public/og.png`;设置了 `openGraph` 的页面要显式带上它,否则会盖掉外层的设置。
 * @param lang 路由段上的语言;不在支持列表时按英文
 */
export function ogImage(lang: string) {
  const alt = OG_IMAGE_ALT[lang as Language] ?? OG_IMAGE_ALT.en
  return { url: '/og.png', width: 1280, height: 640, alt }
}

/** 正式站域名。 */
const PRODUCTION_URL = 'https://imgyoung.x-haose.com'

/**
 * 站点绝对地址。
 *
 * `robots.txt`、`sitemap.xml` 与页面 `<link rel="alternate">` 都要绝对 URL,而它们在构建期生成,
 * 拿不到运行期的请求主机名。生产构建取正式域名,本地开发回落到 dev server 地址;
 * 预览部署等场合可用 `NEXT_PUBLIC_SITE_URL` 覆盖。
 */
export const SITE_URL = (
  process.env.NEXT_PUBLIC_SITE_URL ??
  (process.env.NODE_ENV === 'development' ? 'http://localhost:3000' : PRODUCTION_URL)
).replace(/\/$/, '')

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
