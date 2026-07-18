/**
 * 后端 API 的最小 fetch 封装:同源 cookie 会话 + CSRF 头自动回传。
 * CSRF 令牌由登录 / GET /api/admin/me 响应下发,保存在模块内存,
 * 刷新页面后经 authProvider.check() 重新取得。
 * 横切自愈:401 清会话状态并跳登录页;写请求收到「CSRF 校验失败」的 403 时,
 * 确认会话主体未变后换新令牌重放一次;其它 403(权限不足等)直接抛给调用方,不重放。
 */

/** API 错误:携带 HTTP 状态码与后端 {error} 信封中的消息;status 为 0 表示网络层失败。 */
export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string, options?: ErrorOptions) {
    super(message, options);
    this.status = status;
  }
}

let csrfToken = "";
let sessionUserId = "";

/** 记录当前会话的 CSRF 令牌与会话主体 id(登录 / me 响应中获得);登出时传空串清除。 */
export function setCsrfToken(token: string, userId = ""): void {
  csrfToken = token;
  sessionUserId = userId;
}

/**
 * 401 时不触发整页跳转的端点:登录接口的 401 是密码错误,
 * me 接口的 401 由 authProvider.check() 走 Refine 内路由跳转,硬跳会造成刷新循环。
 */
const authExemptPaths = new Set(["/api/admin/auth/login", "/api/admin/me"]);

/** 后端 CSRF 中间件拒绝请求时的 error 消息;写请求 403 仅此一种触发令牌自愈重放。 */
const csrfRejectedMessage = "CSRF 校验失败";

/** rawFetch 发送请求并把网络层异常统一转成中文 ApiError(status=0,保留 cause)。 */
async function rawFetch(path: string, init: RequestInit | undefined, withCsrf: boolean): Promise<Response> {
  const headers = new Headers(init?.headers);
  if (withCsrf) {
    headers.set("X-CSRF-Token", csrfToken);
    // FormData 必须由浏览器自设带 boundary 的 multipart Content-Type,不能手写。
    if (init?.body != null && !(init.body instanceof FormData) && !headers.has("Content-Type")) {
      headers.set("Content-Type", "application/json");
    }
  }
  try {
    return await fetch(path, { ...init, headers, credentials: "same-origin" });
  } catch (err) {
    throw new ApiError(0, "网络连接失败,请确认服务在线后重试", { cause: err });
  }
}

/**
 * refreshCsrfToken 静默调 me 换取新 CSRF 令牌;拿不到(会话已失效等)返回 false。
 * me 返回的主体与本地记录不一致(如另一标签页换账号重新登录)也返回 false,
 * 避免以他人身份重放请求并把操作记入错误的审计主体。
 */
async function refreshCsrfToken(): Promise<boolean> {
  try {
    const resp = await rawFetch("/api/admin/me", undefined, false);
    if (!resp.ok) {
      return false;
    }
    const body = (await resp.json()) as { csrf_token?: string; user?: { id?: string } };
    if (!body.csrf_token || body.user?.id !== sessionUserId) {
      return false;
    }
    csrfToken = body.csrf_token;
    return true;
  } catch {
    return false;
  }
}

/** readErrorMessage 提取错误响应 {error} 信封中的消息;非 JSON 错误体(如网关页)退回默认文案。 */
async function readErrorMessage(resp: Response): Promise<string> {
  try {
    const body = (await resp.json()) as { error?: string };
    if (body.error) {
      return body.error;
    }
  } catch {
    // 体不可解析时用默认消息。
  }
  return `请求失败(${resp.status})`;
}

/** 发起 API 请求;非 2xx 一律抛 ApiError(消息取后端 error 字段)。 */
export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const method = (init?.method ?? "GET").toUpperCase();
  const withCsrf = method !== "GET" && method !== "HEAD";
  let resp = await rawFetch(path, init, withCsrf);
  let errorMessage = resp.ok ? "" : await readErrorMessage(resp);
  if (
    resp.status === 403 &&
    withCsrf &&
    errorMessage === csrfRejectedMessage &&
    (await refreshCsrfToken())
  ) {
    // 会话仍有效但令牌陈旧(如另一标签页重新登录过):换新令牌后重放本次请求。
    resp = await rawFetch(path, init, withCsrf);
    errorMessage = resp.ok ? "" : await readErrorMessage(resp);
  }
  if (resp.status === 401 && !authExemptPaths.has(path)) {
    // 会话已过期:清除本地令牌并整页回登录页(已在登录页则不跳,避免刷新循环)。
    setCsrfToken("");
    if (window.location.pathname !== "/login") {
      window.location.assign("/login");
    }
  }
  if (!resp.ok) {
    throw new ApiError(resp.status, errorMessage);
  }
  if (resp.status === 204) {
    return undefined as T;
  }
  return (await resp.json()) as T;
}
