import '@/app/global.css'

import { i18nProvider } from 'fumadocs-ui/i18n'
import { RootProvider } from 'fumadocs-ui/provider/next'
import type { Metadata } from 'next'

import { i18n } from '@/lib/i18n'
import { translations } from '@/lib/layout.shared'
import { ogImage, SITE_URL, siteName } from '@/lib/site'

/**
 * 全站元数据:页面标题后缀为当前语言的品牌名,分享卡片用同一张图。
 * `metadataBase` 是相对路径元数据(og:image 等)的解析基址,与 canonical / hreflang 同一来源。
 */
export async function generateMetadata({ params }: LayoutProps<'/[lang]'>): Promise<Metadata> {
  const { lang } = await params
  const name = siteName(lang)
  return {
    metadataBase: new URL(SITE_URL),
    title: { template: `%s · ${name}`, default: name },
    icons: { icon: '/favicon.svg' },
    openGraph: { siteName: name, images: [ogImage(lang)] },
    twitter: { card: 'summary_large_image' },
  }
}

/** 各语言的首页与外框在构建期预渲染。 */
export function generateStaticParams() {
  return i18n.languages.map((lang) => ({ lang }))
}

/** 语言段只接受 generateStaticParams 列出的语言,其余值(如 `/install.sh` 这类带扩展名、不经语言协商的路径)一律 404。 */
export const dynamicParams = false

export default async function Layout({ params, children }: LayoutProps<'/[lang]'>) {
  const { lang } = await params
  return (
    <html lang={lang} suppressHydrationWarning>
      <body className='flex min-h-screen flex-col'>
        <RootProvider i18n={i18nProvider(translations, lang)}>{children}</RootProvider>
      </body>
    </html>
  )
}
