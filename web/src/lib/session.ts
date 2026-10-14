import {
  type QueryClient,
  queryOptions,
  useSuspenseQuery,
} from '@tanstack/react-query'
import { type ParsedLocation, redirect } from '@tanstack/react-router'
import { api, setCsrfToken } from './api'
import { hasStatus } from './errors'

/** 会话主体信息,由登录与 /me 响应返回。 */
export interface SessionUser {
  id: string
  name: string
  role: 'admin' | 'user'
  must_change_password: boolean // 仍为出厂默认密码;为真时改前不放行其余页面
}

/** 登录与 /me 的响应:主体与状态变更请求须回传的 CSRF 令牌。 */
export interface Session {
  user: SessionUser
  csrf_token: string
}

/** 首次强制改密页路由;仍用默认密码时所有已认证页都重定向到此。 */
export const FIRST_RUN_PATH = '/first-run-password'

/** 出厂默认密码;与后端 auth.DefaultAdminPassword 一致,公开值,仅首次改密页作旧密码提交。 */
export const DEFAULT_ADMIN_PASSWORD = 'imgyoung123'

export const sessionQueryKey = ['session'] as const

async function fetchSession(): Promise<Session> {
  const session = await api.get<Session>('/api/admin/me')
  setCsrfToken(session.csrf_token, session.user.id)
  return session
}

/** 会话查询:已认证路由在 beforeLoad 里取过,页面内再读命中缓存。 */
export const sessionQueryOptions = () =>
  queryOptions({
    queryKey: sessionQueryKey,
    queryFn: fetchSession,
    staleTime: 60_000,
    retry: false,
  })

export async function login(name: string, password: string): Promise<Session> {
  const session = await api.post<Session>('/api/admin/auth/login', {
    name,
    password,
  })
  setCsrfToken(session.csrf_token, session.user.id)
  return session
}

export async function logout(): Promise<void> {
  await api.post('/api/admin/auth/logout')
  setCsrfToken('')
}

/** 登录后的回跳地址:只接受站内相对路径,防开放跳转。 */
export function safeRedirect(target: string | undefined): string {
  return target && target.startsWith('/') && !target.startsWith('//')
    ? target
    : '/'
}

/** 路由守卫:取会话,未登录跳登录页并记住回跳地址。 */
export async function requireSession(
  queryClient: QueryClient,
  location: ParsedLocation
): Promise<Session> {
  try {
    return await queryClient.ensureQueryData({
      ...sessionQueryOptions(),
      revalidateIfStale: true,
    })
  } catch (err) {
    if (hasStatus(err, 401)) {
      throw redirect({ to: '/login', search: { redirect: location.href } })
    }
    throw err
  }
}

/** 路由守卫:仅管理员可进的页面,普通用户回总览。 */
export async function requireAdmin(
  queryClient: QueryClient,
  location: ParsedLocation
): Promise<Session> {
  const session = await requireSession(queryClient, location)
  if (session.user.role !== 'admin') throw redirect({ to: '/' })
  return session
}

/** 当前会话;只在已认证路由下使用,beforeLoad 已把会话放进缓存,这里不会真的挂起。 */
export function useSession(): Session {
  return useSuspenseQuery(sessionQueryOptions()).data
}
