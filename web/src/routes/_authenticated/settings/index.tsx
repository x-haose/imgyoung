import { createFileRoute, stripSearchParams } from '@tanstack/react-router'
import { int, oneOf, pageParam, searchSchema } from '@/lib/search'
import { requireAdmin } from '@/lib/session'
import { Settings } from '@/features/settings'
import { SETTINGS_TABS } from '@/features/settings/tabs'

/** 搜索参数缺省值;等于缺省的键不写进地址栏。 */
const SEARCH_DEFAULTS = { tab: 'config', page: 1, page_size: 20 } as const

export const Route = createFileRoute('/_authenticated/settings/')({
  validateSearch: searchSchema({
    tab: oneOf(SETTINGS_TABS, SEARCH_DEFAULTS.tab),
    page: pageParam(SEARCH_DEFAULTS.page),
    page_size: int(SEARCH_DEFAULTS.page_size, 1, 100),
  }),
  search: { middlewares: [stripSearchParams(SEARCH_DEFAULTS)] },
  beforeLoad: ({ context, location }) =>
    requireAdmin(context.queryClient, location),
  component: Settings,
})
