import { createFileRoute } from '@tanstack/react-router'
import { requireAdmin } from '@/lib/session'
import { Channels } from '@/features/channels'

export const Route = createFileRoute('/_authenticated/channels/')({
  beforeLoad: ({ context, location }) =>
    requireAdmin(context.queryClient, location),
  component: Channels,
})
