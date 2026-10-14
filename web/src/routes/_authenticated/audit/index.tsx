import { createFileRoute } from '@tanstack/react-router'
import { requireAdmin } from '@/lib/session'
import { Audit } from '@/features/audit'

export const Route = createFileRoute('/_authenticated/audit/')({
  beforeLoad: ({ context, location }) =>
    requireAdmin(context.queryClient, location),
  component: Audit,
})
