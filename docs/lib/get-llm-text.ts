import { REPO_URL } from '@/lib/site'
import type { source } from '@/lib/source'

/** 接口参考页在 LLM 文本里的替代内容:页面由 OpenAPI 文档渲染,没有 Markdown 正文,直接指向文档本身。 */
const OPENAPI_POINTER = `OpenAPI 3.1 document / OpenAPI 文档: ${REPO_URL}/blob/main/api/openapi.yaml`

/**
 * 把一篇文档转成 LLM 可直接消费的 Markdown:标题、页面 URL、描述与处理后的正文。
 * 供 `/llms-full.txt` 汇总与单页 `.md` 端点复用。
 */
export async function getLLMText(page: (typeof source)['$inferPage']): Promise<string> {
  const header = `# ${page.data.title} (${page.url})\n\n${page.data.description ?? ''}`
  if (!('getText' in page.data)) return `${header}\n\n${OPENAPI_POINTER}`
  const processed = await page.data.getText('processed')
  return `${header}\n\n${processed}`
}
