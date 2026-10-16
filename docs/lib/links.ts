import { PathUtils, type InferPageType } from 'fumadocs-core/source'

import { source } from '@/lib/source'

type DocPage = InferPageType<typeof source>

/**
 * 把文档正文与卡片里的链接解析为站内地址;指向的页面或锚点不存在时抛错,构建随之失败。
 *
 * - `#标题`:本页锚点。
 * - `./x.mdx`、`../x.mdx#标题`:按文件路径指向同语言的页面(该页缺英文版时为中文原文)。
 * - 其余(外部地址等)原样返回。
 *
 * 锚点对照目标页的标题目录,目录收录全部级别的标题;接口参考页没有标题目录,不能带锚点。
 */
export function resolveDocLink(href: string, page: DocPage): string {
  if (href.startsWith('#')) {
    checkAnchor(page, href, href, page)
    return href
  }
  if (!href.startsWith('./') && !href.startsWith('../')) return href
  const target = source.getPageByHref(href, {
    dir: PathUtils.dirname(page.path),
    language: page.locale,
  })
  if (!target) throw new Error(`${page.path}: 链接 ${decodeURI(href)} 指向的页面不存在`)
  if (!target.hash) return target.page.url
  checkAnchor(target.page, `#${target.hash}`, href, page)
  return `${target.page.url}#${target.hash}`
}

/**
 * 确认锚点是目标页的某个标题。
 * @param target 锚点所在的页面
 * @param anchor 带 `#` 的锚点,非 ASCII 字符为百分号编码(MDX 编码过链接地址),标题目录里是原文
 * @param href 链接地址,用于报错
 * @param from 链接所在的页面,用于报错
 */
function checkAnchor(target: DocPage, anchor: string, href: string, from: DocPage) {
  const toc = target.type === 'openapi' ? [] : target.data.toc
  const decoded = decodeURIComponent(anchor)
  if (!toc.some((item) => item.url === decoded)) {
    throw new Error(`${from.path}: 链接 ${decodeURI(href)} 的锚点在 ${target.path} 里不存在`)
  }
}
