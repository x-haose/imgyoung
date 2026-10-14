import { createFileRoute } from '@tanstack/react-router'
import { requireAdmin } from '@/lib/session'
import { Cleanup } from '@/features/cleanup'

export const Route = createFileRoute('/_authenticated/cleanup/')({
  beforeLoad: ({ context, location }) =>
    requireAdmin(context.queryClient, location),
  component: Cleanup,
})
