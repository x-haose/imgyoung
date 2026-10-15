import type * as PageTree from 'fumadocs-core/page-tree'
import { llms } from 'fumadocs-core/source'

import { getLLMText } from '@/lib/get-llm-text'
import { i18n, type Language } from '@/lib/i18n'
import { source } from '@/lib/source'

/** 各语言的端点地址与条目文案;文案带英文括注,抓到中文那份的模型未必读中文。 */
const ENDPOINTS: Record<
  Language,
  { index: string; indexLabel: string; full: string; fullLabel: string }
> = {
  zh: {
    index: '/zh/llms.txt',
    indexLabel: '中文索引 (Chinese index)',
    full: '/zh/llms-full.txt',
    fullLabel: '中文全文 (Chinese full text)',
  },
  en: {
    index: '/en/llms.txt',
    indexLabel: 'English index',
    full: '/en/llms-full.txt',
    fullLabel: 'English full text',
  },
}

/** 索引末尾的交叉指路:本语言的全文,以及其余语言的索引与全文。 */
function crossReferences(lang: Language): string {
  const here = ENDPOINTS[lang]
  const others = i18n.languages.filter((l) => l !== lang).map((l) => ENDPOINTS[l])
  return [
    '',
    '## Full text and other languages',
    '',
    `- [${here.fullLabel}](${here.full})`,
    ...others.flatMap((o) => [`- [${o.indexLabel}](${o.index})`, `- [${o.fullLabel}](${o.full})`]),
    '',
  ].join('\n')
}

/** 某个语言的站点结构索引:页面标题、描述与链接,末尾附交叉指路。 */
export function llmsIndex(lang: Language): string {
  return llms(source).index(lang) + '\n' + crossReferences(lang)
}

/** 按页面树顺序展开出页面节点:文件夹先出 index 页再出子页,分隔符跳过,即侧栏自上而下的阅读顺序。 */
function itemsInTreeOrder(nodes: PageTree.Node[]): PageTree.Item[] {
  return nodes.flatMap((node) => {
    if (node.type === 'page') return [node]
    if (node.type === 'folder') {
      return [...(node.index ? [node.index] : []), ...itemsInTreeOrder(node.children)]
    }
    return []
  })
}

/**
 * 某个语言的全部页面,按导航顺序排列。
 *
 * 全文是给模型一次读完的,章节次序必须与人读文档的次序一致,故取自页面树而非文件扫描顺序;
 * 未被任何 meta.json 列入的页面不在树上,补在末尾,不漏正文。
 */
function pagesInReadingOrder(lang: Language) {
  const tree = source.getPageTree(lang)
  const ordered = itemsInTreeOrder(tree.children)
    .map((item) => source.getNodePage(item, lang))
    .filter((page) => page !== undefined)
  const seen = new Set(ordered.map((page) => page.url))
  const rest = source.getPages(lang).filter((page) => !seen.has(page.url))
  return [...ordered, ...rest]
}

/** 某个语言的全站文档 Markdown 全文,一次抓取即可作为完整上下文。 */
export async function llmsFullText(lang: Language): Promise<string> {
  const texts = await Promise.all(pagesInReadingOrder(lang).map(getLLMText))
  return texts.join('\n\n')
}
