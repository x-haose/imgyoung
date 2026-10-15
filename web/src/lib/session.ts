import {
  type QueryClient,
  queryOptions,
  useSuspenseQuery,
} from '@tanstack/react-query'
import { type ParsedLocation, redirect } from '@tanstack/react-router'
import { api, type Schema, setCsrfToken } from './api'
import { hasStatus } from './errors'

/** 会话主体信息,由登录与 /me 响应返回。 */
export type SessionUser = Schema<'SessionUser'>

/** 登录与 /me 的响应:主体与状态变更请求须回传的 CSRF 令牌。 */
export type Session = Schema<'Session'>

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

/**
 * 路由守卫:取会话,未登录跳登录页并记住回跳地址。
 * 缓存里有会话就直接用,不让导航等网络;同时按会话查询的保鲜期在后台重新校验,仍新鲜时不发请求。
 */
export async function requireSession(
  queryClient: QueryClient,
  location: ParsedLocation
): Promise<Session> {
  let session: Session
  try {
    session = await queryClient.query({
      ...sessionQueryOptions(),
      staleTime: 'static',
    })
  } catch (err) {
    if (hasStatus(err, 401)) {
      throw redirect({ to: '/login', search: { redirect: location.href } })
    }
    throw err
  }
  revalidateSession(queryClient)
  return session
}

/**
 * 后台重新校验会话,本次导航不等它。
 * 校验失败已写进会话查询的缓存,由读会话的页面显示;会话失效(401)另由请求层的 onUnauthorized 处理器跳登录页。
 */
function revalidateSession(queryClient: QueryClient): void {
  queryClient.query(sessionQueryOptions()).catch(() => undefined)
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
