import { createFileRoute, stripSearchParams } from '@tanstack/react-router'
import { day, int, oneOf, pageParam, searchSchema, str } from '@/lib/search'
import { requireAdmin } from '@/lib/session'
import { Audit } from '@/features/audit'
import { AUDIT_ACTIONS } from '@/features/audit/actions'
import { AUDIT_PAGE_SIZE } from '@/features/audit/data'

const defaults = { page: 1, page_size: AUDIT_PAGE_SIZE }

export const Route = createFileRoute('/_authenticated/audit/')({
  validateSearch: searchSchema({
    page: pageParam(defaults.page),
    page_size: int(defaults.page_size, 1, 100),
    action: oneOf(AUDIT_ACTIONS, undefined),
    actor: str(),
    target: str(),
    from: day(),
    to: day(),
  }),
  // 缺省的页码与页长不写进地址栏。
  search: { middlewares: [stripSearchParams(defaults)] },
  beforeLoad: ({ context, location }) =>
    requireAdmin(context.queryClient, location),
  component: Audit,
})
