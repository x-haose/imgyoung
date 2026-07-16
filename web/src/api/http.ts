/**
 * 后端 API 的最小 fetch 封装:同源 cookie 会话 + CSRF 头自动回传。
 * CSRF 令牌由登录 / GET /api/admin/me 响应下发,保存在模块内存,
 * 刷新页面后经 authProvider.check() 重新取得。
 */

/** API 错误:携带 HTTP 状态码与后端 {error} 信封中的消息。 */
export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

let csrfToken = "";

/** 记录当前会话的 CSRF 令牌(登录 / me 响应中获得)。 */
export function setCsrfToken(token: string): void {
  csrfToken = token;
}

/** 发起 API 请求;非 2xx 一律抛 ApiError(消息取后端 error 字段)。 */
export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  const method = (init?.method ?? "GET").toUpperCase();
  if (method !== "GET" && method !== "HEAD") {
    headers.set("X-CSRF-Token", csrfToken);
    if (init?.body != null && !headers.has("Content-Type")) {
      headers.set("Content-Type", "application/json");
    }
  }
  const resp = await fetch(path, { ...init, headers, credentials: "same-origin" });
  if (!resp.ok) {
    let message = `请求失败(${resp.status})`;
    try {
      const body = (await resp.json()) as { error?: string };
      if (body.error) {
        message = body.error;
      }
    } catch {
      // 非 JSON 错误体(如网关页),保留默认消息。
    }
    throw new ApiError(resp.status, message);
  }
  if (resp.status === 204) {
    return undefined as T;
  }
  return (await resp.json()) as T;
}
