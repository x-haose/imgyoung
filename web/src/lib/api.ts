/**
 * 后端 API 的 fetch 封装:同源 cookie 会话 + CSRF 头回传。
 * CSRF 令牌由登录与 GET /api/admin/me 响应下发,保存在模块内存,刷新页面后由会话查询重新取得。
 * 写请求收到「CSRF 校验失败」的 403 时,确认会话主体未变后换新令牌重放一次;其它 403 直接抛出。
 * 401(登录与 me 端点除外)交给 onUnauthorized 注册的处理器,由应用层决定跳转。
 */
import { messages, t } from '@/i18n'

/** API 错误:携带 HTTP 状态码与后端 {error} 信封中的消息;status 为 0 表示网络层失败。 */
export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string, options?: ErrorOptions) {
    super(message, options)
    this.name = 'ApiError'
    this.status = status
  }
}

let csrfToken = ''
let sessionUserId = ''
let unauthorizedHandler: () => void = () => {}

/** 记录当前会话的 CSRF 令牌与会话主体 id(登录 / me 响应中获得);登出时传空串清除。 */
export function setCsrfToken(token: string, userId = ''): void {
  csrfToken = token
  sessionUserId = userId
}

/** 注册会话失效(401)时的处理;登录接口的 401 是密码错误,me 接口的 401 由路由守卫处理,二者不触发。 */
export function onUnauthorized(handler: () => void): void {
  unauthorizedHandler = handler
}

const authExemptPaths = new Set(['/api/admin/auth/login', '/api/admin/me'])

/** 后端 CSRF 中间件拒绝请求时的 error 消息;写请求 403 仅此一种触发令牌自愈重放。 */
const csrfRejectedMessage = 'CSRF 校验失败'

async function rawFetch(
  path: string,
  init: RequestInit | undefined,
  withCsrf: boolean
): Promise<Response> {
  const headers = new Headers(init?.headers)
  if (withCsrf) {
    headers.set('X-CSRF-Token', csrfToken)
    // FormData 必须由浏览器自设带 boundary 的 multipart Content-Type,不能手写。
    if (
      init?.body != null &&
      !(init.body instanceof FormData) &&
      !headers.has('Content-Type')
    ) {
      headers.set('Content-Type', 'application/json')
    }
  }
  try {
    return await fetch(path, { ...init, headers, credentials: 'same-origin' })
  } catch (err) {
    throw new ApiError(0, messages().common.networkError, { cause: err })
  }
}

/**
 * 静默调 me 换取新 CSRF 令牌;拿不到(会话已失效等)返回 false。
 * me 返回的主体与本地记录不一致(如另一标签页换账号重新登录)也返回 false,
 * 避免以他人身份重放请求并把操作记入错误的审计主体。
 */
async function refreshCsrfToken(): Promise<boolean> {
  const resp = await rawFetch('/api/admin/me', undefined, false)
  if (!resp.ok) return false
  const body = (await resp.json()) as {
    csrf_token?: string
    user?: { id?: string }
  }
  if (!body.csrf_token || body.user?.id !== sessionUserId) return false
  csrfToken = body.csrf_token
  return true
}

/** 提取错误响应 {error} 信封中的消息;非 JSON 错误体(如网关页)退回带状态码的默认文案。 */
async function readErrorMessage(resp: Response): Promise<string> {
  const fallback = t(messages().common.requestFailed, { status: resp.status })
  const raw = await resp.text()
  try {
    const body = JSON.parse(raw) as { error?: string }
    return body.error || fallback
  } catch {
    return fallback
  }
}

/** 发起 API 请求;非 2xx 一律抛 ApiError(消息取后端 error 字段),204 返回 undefined。 */
export async function apiFetch<T>(
  path: string,
  init?: RequestInit
): Promise<T> {
  const method = (init?.method ?? 'GET').toUpperCase()
  const withCsrf = method !== 'GET' && method !== 'HEAD'
  let resp = await rawFetch(path, init, withCsrf)
  let message = resp.ok ? '' : await readErrorMessage(resp)
  if (
    resp.status === 403 &&
    withCsrf &&
    message === csrfRejectedMessage &&
    (await refreshCsrfToken())
  ) {
    // 会话仍有效但令牌陈旧(如另一标签页重新登录过):换新令牌后重放本次请求。
    resp = await rawFetch(path, init, withCsrf)
    message = resp.ok ? '' : await readErrorMessage(resp)
  }
  if (resp.status === 401 && !authExemptPaths.has(path.split('?')[0])) {
    setCsrfToken('')
    unauthorizedHandler()
  }
  if (!resp.ok) throw new ApiError(resp.status, message)
  if (resp.status === 204) return undefined as T
  return (await resp.json()) as T
}

/** 查询参数值;undefined、null 与空串不进入查询串。 */
export type QueryValue = string | number | boolean | undefined | null

/** 把参数拼成查询串;没有有效参数时原样返回路径。 */
export function withQuery(
  path: string,
  params?: Record<string, QueryValue>
): string {
  if (!params) return path
  const q = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== null && value !== '') {
      q.set(key, String(value))
    }
  }
  const s = q.toString()
  return s ? `${path}?${s}` : path
}

function jsonInit(method: string, body?: unknown): RequestInit {
  return {
    method,
    body: body === undefined ? undefined : JSON.stringify(body),
  }
}

/** 按 HTTP 方法封装的请求入口;JSON 请求体自动序列化。 */
export const api = {
  get: <T>(path: string, params?: Record<string, QueryValue>) =>
    apiFetch<T>(withQuery(path, params)),
  post: <T = void>(path: string, body?: unknown) =>
    apiFetch<T>(path, jsonInit('POST', body)),
  put: <T = void>(path: string, body?: unknown) =>
    apiFetch<T>(path, jsonInit('PUT', body)),
  patch: <T = void>(path: string, body?: unknown) =>
    apiFetch<T>(path, jsonInit('PATCH', body)),
  delete: <T = void>(path: string) => apiFetch<T>(path, { method: 'DELETE' }),
  /** 表单上传:Content-Type 连同 boundary 由浏览器自设。 */
  form: <T>(path: string, body: FormData) =>
    apiFetch<T>(path, { method: 'POST', body }),
}

/** 后端列表统一信封;total 为过滤后的总条数,供分页。 */
export interface ListResponse<T> {
  items: T[]
  total: number
}

/** 后端列表分页参数:page 从 1 起;page_size 默认 20、上限 100。 */
export const PAGE_SIZES = [10, 20, 50, 100] as const
