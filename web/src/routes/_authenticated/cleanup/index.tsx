import { createFileRoute, stripSearchParams } from '@tanstack/react-router'
import { int, searchSchema } from '@/lib/search'
import { requireAdmin } from '@/lib/session'
import { Cleanup } from '@/features/cleanup'
import { CLEANUP_PAGE_SIZE } from '@/features/cleanup/data'

const defaults = { page: 1, page_size: CLEANUP_PAGE_SIZE }

export const Route = createFileRoute('/_authenticated/cleanup/')({
  validateSearch: searchSchema({
    page: int(defaults.page, 1),
    page_size: int(defaults.page_size, 1, 100),
  }),
  // 缺省的页码与页长不写进地址栏。
  search: { middlewares: [stripSearchParams(defaults)] },
  beforeLoad: ({ context, location }) =>
    requireAdmin(context.queryClient, location),
  component: Cleanup,
})
