import { createFileRoute, redirect } from '@tanstack/react-router'
import { hasStatus } from '@/lib/errors'
import { searchSchema, str } from '@/lib/search'
import {
  FIRST_RUN_PATH,
  safeRedirect,
  type Session,
  sessionQueryOptions,
} from '@/lib/session'
import { Login } from '@/features/auth/login'

export const Route = createFileRoute('/(auth)/login')({
  validateSearch: searchSchema({ redirect: str() }),
  // 已登录者不停留在登录页。
  beforeLoad: async ({ context, search }) => {
    let session: Session
    try {
      session = await context.queryClient.query(sessionQueryOptions())
    } catch (err) {
      if (hasStatus(err, 401)) return
      throw err
    }
    if (session.user.must_change_password) {
      throw redirect({ to: FIRST_RUN_PATH })
    }
    throw redirect({ href: safeRedirect(search.redirect) })
  },
  component: Login,
})
