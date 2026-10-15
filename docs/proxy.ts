import { createI18nMiddleware } from 'fumadocs-core/i18n/middleware'
import { isMarkdownPreferred, rewritePath } from 'fumadocs-core/negotiation'
import { NextResponse, type NextFetchEvent, type NextRequest } from 'next/server'

import { i18n } from '@/lib/i18n'

const i18nMiddleware = createI18nMiddleware(i18n)

/** 文档页路径 → 该页 Markdown 原文端点;通配段不匹配空串,栏目首页另由第二条兜住。 */
const toMarkdown = [
  rewritePath('/:lang/docs/*path', '/llms.mdx/:lang/docs/*path'),
  rewritePath('/:lang/docs', '/llms.mdx/:lang/docs'),
]

/**
 * 请求入口:`Accept` 声明更想要 Markdown 的请求(AI 代理)导向 Markdown 原文端点,
 * 其余交给语言协商中间件。中间件跑在 next.config 的 rewrite 之前,`.md` / `.mdx` 后缀
 * 此时还在路径上,先剥掉再改写,否则后缀会被带进 slug 而查不到页面。
 */
export default function proxy(request: NextRequest, event: NextFetchEvent) {
  if (isMarkdownPreferred(request)) {
    const path = request.nextUrl.pathname.replace(/\.mdx?$/, '')
    for (const rule of toMarkdown) {
      const rewritten = rule.rewrite(path)
      if (rewritten) return NextResponse.rewrite(new URL(rewritten, request.url))
    }
  }
  return i18nMiddleware(request, event)
}

export const config = {
  /**
   * 跳过框架内部路由(`_next`、`api`)、单页 Markdown 的内部重写目标(`llms.mdx`),
   * 以及末段带扩展名的固定路径(robots.txt、sitemap.xml、llms.txt、public/ 静态文件)——
   * 加语言前缀就取不到了;`.md` / `.mdx` 例外,文档页追加后缀时仍要走协商与语言前缀补全。
   */
  matcher: ['/((?!api|_next|llms\\.mdx|.*\\.(?!mdx?$)[^/.]+$).*)'],
}
