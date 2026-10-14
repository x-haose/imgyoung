import { createFileRoute } from '@tanstack/react-router'
import { requireAdmin } from '@/lib/session'
import { Users } from '@/features/users'

export const Route = createFileRoute('/_authenticated/users/')({
  beforeLoad: ({ context, location }) =>
    requireAdmin(context.queryClient, location),
  component: Users,
})
