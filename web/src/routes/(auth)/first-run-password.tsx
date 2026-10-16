import { createFileRoute, redirect } from '@tanstack/react-router'
import { requireSession } from '@/lib/session'
import { FirstRunPassword } from '@/features/auth/first-run-password'

export const Route = createFileRoute('/(auth)/first-run-password')({
  // 已改过初始密码者不应停留在本页(如手动输入 URL),回落总览。
  beforeLoad: async ({ context, location }) => {
    const session = await requireSession(context.queryClient, location)
    if (!session.user.must_change_password) throw redirect({ to: '/' })
  },
  component: FirstRunPassword,
})
