import { DocsLayout } from 'fumadocs-ui/layouts/notebook'

import { Footer } from '@/components/footer'
import { baseOptions } from '@/lib/layout.shared'
import { source } from '@/lib/source'

/** 文档版式:notebook 版式,顶栏放站点标识与「文档 / 接口参考」,侧栏承载当前语言的页面树。 */
export default async function Layout({ params, children }: LayoutProps<'/[lang]/docs'>) {
  const { lang } = await params
  const options = baseOptions(lang)
  return (
    <DocsLayout {...options} tree={source.getPageTree(lang)} nav={{ ...options.nav, mode: 'top' }}>
      {children}
      {/* notebook 版式的容器是 CSS 网格,col-span-full 让页脚落在正文与侧栏下方的整行 */}
      <Footer lang={lang} className='col-span-full' />
    </DocsLayout>
  )
}
