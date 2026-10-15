import { loader } from 'fumadocs-core/source'
import * as icons from 'lucide-react'
import { createElement } from 'react'

import { docs } from '@/.source/server'
import { i18n } from '@/lib/i18n'
import { openapi } from '@/lib/openapi'

/**
 * 接口参考的虚拟页面:`api/openapi.yaml` 每个标签一页,挂在 `/docs/api/<x-slug>`。
 *
 * 路径段取标签的 `x-slug` 扩展——标签名是中文展示名,不适合做 URL;
 * 新增标签漏写 `x-slug` 时构建直接失败,不生成含中文或乱码的路径。
 */
const apiPages = await openapi.staticSource({
  baseDir: 'api',
  per: 'tag',
  name(output) {
    const slug = (output.tag as Record<string, unknown> | undefined)?.['x-slug']
    if (typeof slug !== 'string' || slug === '') {
      throw new Error(
        `api/openapi.yaml 的标签「${output.info.title}」缺少 x-slug,接口参考页无法确定路径`
      )
    }
    return slug
  },
})

/** 文档页面树与页面查询入口,按语言切分;手写文档与接口参考共用一棵树。 */
export const source = loader(
  { docs: docs.toFumadocsSource(), openapi: apiPages },
  {
    baseUrl: '/docs',
    i18n,
    plugins: [openapi.loaderPlugin()],
    /** meta.json 的 `icon` 字段按名字解析为 lucide 图标组件;名字拼错时构建失败。 */
    icon(name) {
      if (!name) return
      if (!(name in icons)) throw new Error(`未知的 lucide 图标名「${name}」`)
      return createElement(icons[name as keyof typeof icons] as React.FC)
    },
  }
)
