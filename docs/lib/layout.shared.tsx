import { zhCN } from '@fumadocs/language/zh-cn'
import { uiTranslations } from 'fumadocs-ui/i18n'
import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared'

import { Logo } from '@/components/logo'
import { i18n, type Language } from '@/lib/i18n'
import { REPO_URL } from '@/lib/site'

/** 界面文案:中文界面用 fumadocs 官方简体词表,英文取内置默认。 */
export const translations = i18n.translations().extend(uiTranslations()).preset('zh', zhCN())

/** 顶栏链接文案。 */
const NAV: Record<Language, { docs: string; api: string }> = {
  zh: { docs: '文档', api: '接口参考' },
  en: { docs: 'Docs', api: 'API' },
}

/**
 * 导航栏与页脚的共享配置。
 * @param lang 当前语言;不在支持列表时按英文渲染
 */
export function baseOptions(lang: string): BaseLayoutProps {
  const t = NAV[lang as Language] ?? NAV.en
  return {
    nav: { title: <Logo />, url: `/${lang}` },
    githubUrl: REPO_URL,
    i18n: true,
    links: [
      { type: 'main', text: t.docs, url: `/${lang}/docs`, active: 'nested-url' },
      { type: 'main', text: t.api, url: `/${lang}/docs/api`, active: 'nested-url' },
    ],
  }
}
