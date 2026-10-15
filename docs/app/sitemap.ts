import type { MetadataRoute } from 'next'

import { i18n } from '@/lib/i18n'
import { languageAlternates, SITE_URL } from '@/lib/site'
import { source } from '@/lib/source'

/** 站点地图:各语言首页与全部文档页,每条附各语言互指的 hreflang。 */
export default function sitemap(): MetadataRoute.Sitemap {
  const entries: MetadataRoute.Sitemap = i18n.languages.map((lang) => ({
    url: `${SITE_URL}/${lang}`,
    changeFrequency: 'weekly',
    priority: 1,
    alternates: { languages: languageAlternates((l) => `${SITE_URL}/${l}`) },
  }))
  for (const lang of i18n.languages) {
    for (const page of source.getPages(lang)) {
      const rest = page.url.slice(lang.length + 1)
      entries.push({
        url: `${SITE_URL}${page.url}`,
        changeFrequency: 'weekly',
        priority: 0.8,
        alternates: { languages: languageAlternates((l) => `${SITE_URL}/${l}${rest}`) },
      })
    }
  }
  return entries
}
