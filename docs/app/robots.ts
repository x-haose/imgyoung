import type { MetadataRoute } from 'next'

import { SITE_URL } from '@/lib/site'

/** 爬虫规则:全站允许抓取(含给模型准备的纯文本端点),并指向 sitemap。 */
export default function robots(): MetadataRoute.Robots {
  return {
    rules: { userAgent: '*', allow: '/' },
    sitemap: `${SITE_URL}/sitemap.xml`,
  }
}
