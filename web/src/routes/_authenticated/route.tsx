import { createFileRoute, redirect } from '@tanstack/react-router'
import { FIRST_RUN_PATH, requireSession } from '@/lib/session'
import { AuthenticatedLayout } from '@/components/layout/authenticated-layout'

export const Route = createFileRoute('/_authenticated')({
  // 仍用初始密码时,改掉前不放行任何业务页。
  beforeLoad: async ({ context, location }) => {
    const session = await requireSession(context.queryClient, location)
    if (session.user.must_change_password) {
      throw redirect({ to: FIRST_RUN_PATH })
    }
  },
  component: AuthenticatedLayout,
})
