import { createFileRoute, stripSearchParams } from '@tanstack/react-router'
import { int, searchSchema, str } from '@/lib/search'
import { Tokens } from '@/features/tokens'
import { TOKENS_PAGE_SIZE } from '@/features/tokens/data'

const defaults = { page: 1, page_size: TOKENS_PAGE_SIZE }

export const Route = createFileRoute('/_authenticated/tokens/')({
  validateSearch: searchSchema({
    page: int(defaults.page, 1),
    page_size: int(defaults.page_size, 1, 100),
    user: str(), // 管理员按用户查看;缺省为本人
  }),
  // 缺省的页码与页长不写进地址栏。
  search: { middlewares: [stripSearchParams(defaults)] },
  component: Tokens,
})
