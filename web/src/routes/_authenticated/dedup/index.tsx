import { createFileRoute } from '@tanstack/react-router'
import { requireAdmin } from '@/lib/session'
import { Dedup } from '@/features/dedup'

export const Route = createFileRoute('/_authenticated/dedup/')({
  beforeLoad: ({ context, location }) =>
    requireAdmin(context.queryClient, location),
  component: Dedup,
})
