import { createFileRoute, stripSearchParams } from '@tanstack/react-router'
import { int, pageParam, searchSchema } from '@/lib/search'
import { requireAdmin } from '@/lib/session'
import { Dedup } from '@/features/dedup'
import { DEDUP_PAGE_SIZE } from '@/features/dedup/data'

const defaults = { page: 1, page_size: DEDUP_PAGE_SIZE }

export const Route = createFileRoute('/_authenticated/dedup/')({
  validateSearch: searchSchema({
    page: pageParam(defaults.page),
    page_size: int(defaults.page_size, 1, 100),
  }),
  // 缺省的页码与页长不写进地址栏。
  search: { middlewares: [stripSearchParams(defaults)] },
  beforeLoad: ({ context, location }) =>
    requireAdmin(context.queryClient, location),
  component: Dedup,
})
