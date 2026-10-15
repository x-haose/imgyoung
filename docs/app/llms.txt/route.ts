import { i18n } from '@/lib/i18n'
import { llmsIndex } from '@/lib/llms'

/** 内容随构建产物固定,无需重新验证。 */
export const revalidate = false

/** `/llms.txt`:默认语言的站点结构索引;末尾交叉指向其余语言与全文端点。 */
export function GET() {
  return new Response(llmsIndex(i18n.defaultLanguage), {
    headers: { 'Content-Type': 'text/plain; charset=utf-8' },
  })
}
