import { createFromSource } from 'fumadocs-core/search/server'

import { source } from '@/lib/source'

/** 站内搜索:按语言分别建索引,中文走内置分词。 */
export const { GET } = createFromSource(source)
