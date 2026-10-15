import { defineI18n } from 'fumadocs-core/i18n'

/**
 * 文档站语言配置。
 *
 * 中文为默认语言,内容文件不带语言后缀(`index.mdx`);英文以 `.en.mdx` 后缀并存于同一目录。
 * URL 始终带语言前缀(`/zh/docs/...`、`/en/docs/...`),两种语言的链接可以直接分享,不依赖浏览器协商。
 * 某页缺英文版时,英文站显示中文原文。
 */
export const i18n = defineI18n({
  defaultLanguage: 'zh',
  languages: ['zh', 'en'],
})

/** 受支持的语言代码。 */
export type Language = (typeof i18n.languages)[number]

/** 把路由段上的任意字符串收窄为受支持的语言,用于按语言分发的端点。 */
export function isLanguage(value: string): value is Language {
  return (i18n.languages as readonly string[]).includes(value)
}
