import { loader } from 'fumadocs-core/source'
import * as icons from 'lucide-react'
import { createElement } from 'react'

import { docs } from '@/.source/server'
import { i18n } from '@/lib/i18n'
import { openapi } from '@/lib/openapi'

/** 各标签的英文标题与英文说明,按 x-slug 索引;生成中文页时顺带收集。 */
const englishTags = new Map<string, { title: string; description: string }>()

/**
 * 接口参考的虚拟页面:`api/openapi.yaml` 每个标签一页,挂在 `/docs/api/<x-slug>`。
 *
 * 标题取标签的 `x-displayName`(中文显示名),说明取 `description`;路径段取 `x-slug`。
 * 新增标签漏写 `x-slug` 或 `x-description-en` 时构建直接失败。
 */
const zhApiPages = await openapi.staticSource({
  baseDir: 'api',
  per: 'tag',
  name(output) {
    const tag = output.tag as (Record<string, unknown> & { name: string }) | undefined
    const slug = tag?.['x-slug']
    const descriptionEn = tag?.['x-description-en']
    if (
      !tag ||
      typeof slug !== 'string' ||
      slug === '' ||
      typeof descriptionEn !== 'string' ||
      descriptionEn === ''
    ) {
      throw new Error(
        `api/openapi.yaml 的标签「${output.info.title}」缺少 x-slug 或 x-description-en,接口参考页无法生成`
      )
    }
    englishTags.set(slug, { title: tag.name, description: descriptionEn })
    return slug
  },
})

/**
 * 英文站的接口参考页:与中文页是同一份接口内容,标题与说明换成标签名(英文)与 `x-description-en`,
 * 以 `.en.mdx` 路径并入,语言划分与手写文档一致。
 */
const apiPages = {
  files: zhApiPages.files.flatMap((file): (typeof zhApiPages.files)[number][] => {
    if (file.type !== 'page') return [file]
    const slug = file.path.replace(/^.*\//, '').replace(/\.mdx$/, '')
    const english = englishTags.get(slug)
    if (!english) throw new Error(`接口参考页「${file.path}」没有对应的标签`)
    return [
      file,
      { ...file, path: file.path.replace(/\.mdx$/, '.en.mdx'), data: { ...file.data, ...english } },
    ]
  }),
}

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
