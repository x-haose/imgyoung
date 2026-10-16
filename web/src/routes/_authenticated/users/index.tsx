import { createFileRoute, stripSearchParams } from '@tanstack/react-router'
import { int, pageParam, searchSchema, str } from '@/lib/search'
import { requireAdmin } from '@/lib/session'
import { Users } from '@/features/users'
import { USERS_PAGE_SIZE } from '@/features/users/data'

const defaults = { page: 1, page_size: USERS_PAGE_SIZE }

export const Route = createFileRoute('/_authenticated/users/')({
  validateSearch: searchSchema({
    page: pageParam(defaults.page),
    page_size: int(defaults.page_size, 1, 100),
    filter: str(), // 按登录名或 ID 的子串筛选
  }),
  // 缺省的页码与页长不写进地址栏。
  search: { middlewares: [stripSearchParams(defaults)] },
  beforeLoad: ({ context, location }) =>
    requireAdmin(context.queryClient, location),
  component: Users,
})
