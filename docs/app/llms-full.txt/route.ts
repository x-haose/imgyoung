import { i18n } from '@/lib/i18n'
import { llmsFullText } from '@/lib/llms'

/** 内容随构建产物固定,无需重新验证。 */
export const revalidate = false

/** `/llms-full.txt`:默认语言的全站 Markdown 全文;全文按语言分开,混入用不上的语言只会挤占上下文。 */
export async function GET() {
  return new Response(await llmsFullText(i18n.defaultLanguage), {
    headers: { 'Content-Type': 'text/plain; charset=utf-8' },
  })
}
