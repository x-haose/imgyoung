import { createFileRoute } from '@tanstack/react-router'
import { requireAdmin } from '@/lib/session'
import { Settings } from '@/features/settings'

export const Route = createFileRoute('/_authenticated/settings/')({
  beforeLoad: ({ context, location }) =>
    requireAdmin(context.queryClient, location),
  component: Settings,
})
