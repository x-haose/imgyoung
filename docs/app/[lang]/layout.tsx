import '@/app/global.css'

import { i18nProvider } from 'fumadocs-ui/i18n'
import { RootProvider } from 'fumadocs-ui/provider/next'
import type { Metadata } from 'next'

import { translations } from '@/lib/layout.shared'
import { SITE_URL } from '@/lib/site'

/** 相对路径元数据(og:image 等)的解析基址,与 canonical / hreflang 同一来源。 */
export const metadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  title: { template: '%s · ImgYoung', default: 'ImgYoung' },
  icons: { icon: '/favicon.svg' },
}

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
