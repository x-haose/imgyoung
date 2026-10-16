import {
  DocsBody,
  DocsDescription,
  DocsPage,
  DocsTitle,
  MarkdownCopyButton,
  ViewOptionsPopover,
} from 'fumadocs-ui/layouts/notebook/page'
import defaultMdxComponents from 'fumadocs-ui/mdx'
import { Card } from 'fumadocs-ui/components/card'
import type { Metadata } from 'next'
import type { ComponentProps } from 'react'
import { notFound } from 'next/navigation'

import { OpenAPIPage } from '@/components/api-page'
import { getMDXComponents } from '@/components/mdx'
import { resolveDocLink } from '@/lib/links'
import { languageAlternates, REPO_URL, SITE_URL } from '@/lib/site'
import { source } from '@/lib/source'

export default async function Page(props: PageProps<'/[lang]/docs/[[...slug]]'>) {
  const params = await props.params
  const page = source.getPage(params.slug, params.lang)
  if (!page) notFound()

  // 接口参考页由 OpenAPI 文档渲染:整宽展示,没有 Markdown 原文可复制
  if (page.type === 'openapi') {
    return (
      <DocsPage full>
        <DocsTitle>{page.data.title}</DocsTitle>
        <DocsDescription>{page.data.description}</DocsDescription>
        <DocsBody>
          <OpenAPIPage {...page.data.getOpenAPIPageProps()} />
        </DocsBody>
      </DocsPage>
    )
  }

  const MDX = page.data.body
  return (
    <DocsPage toc={page.data.toc}>
      <DocsTitle>{page.data.title}</DocsTitle>
      <DocsDescription>{page.data.description}</DocsDescription>
      {/* 复制本页 Markdown,或选择用哪个 AI 打开、去 GitHub 看源文件;都指向同一个 .md 端点 */}
      <div className='mb-6 flex flex-row items-center gap-2 border-b pb-6'>
        <MarkdownCopyButton markdownUrl={`${page.url}.md`} />
        <ViewOptionsPopover
          markdownUrl={`${page.url}.md`}
          githubUrl={`${REPO_URL}/blob/main/docs/content/docs/${page.path}`}
        />
      </div>
      <DocsBody>
        {/* 正文与卡片里的链接经 resolveDocLink 解析,指向的页面或锚点不存在时构建失败 */}
        <MDX
          components={getMDXComponents({
            a: ({ href, ...props }: ComponentProps<'a'>) => (
              <defaultMdxComponents.a {...props} href={href && resolveDocLink(href, page)} />
            ),
            Card: (props: ComponentProps<typeof Card>) => (
              <Card {...props} href={props.href && resolveDocLink(props.href, page)} />
            ),
          })}
        />
      </DocsBody>
    </DocsPage>
  )
}

export function generateStaticParams() {
  return source.generateParams()
}

export async function generateMetadata(
  props: PageProps<'/[lang]/docs/[[...slug]]'>
): Promise<Metadata> {
  const params = await props.params
  const page = source.getPage(params.slug, params.lang)
  if (!page) notFound()

  // 页面 URL 形如 /{lang}/docs/...,换掉开头的语言段即得同一页的其他语言地址
  const rest = page.url.slice(params.lang.length + 1)
  return {
    title: page.data.title,
    description: page.data.description,
    alternates: {
      canonical: `${SITE_URL}${page.url}`,
      languages: languageAlternates((l) => `${SITE_URL}/${l}${rest}`),
      // 声明本页的 Markdown 形态,AI 代理据此发现纯文本版本
      ...(page.type === 'openapi'
        ? {}
        : { types: { 'text/markdown': `${SITE_URL}${page.url}.md` } }),
    },
  }
}
