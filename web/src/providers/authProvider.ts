import type { AuthProvider } from "@refinedev/core";

import { ApiError, apiFetch, setCsrfToken } from "../api/http";

/** 会话主体信息,由登录与 /me 响应返回。 */
export interface SessionUser {
  id: string;
  name: string;
  role: "admin" | "user";
  /** 仍为出厂默认密码;为真时前端强制跳首次改密页,改前不放行其余页面。 */
  must_change_password: boolean;
}

/** 首次强制改密页路由;仍用默认密码时所有已认证页都重定向到此。 */
export const FIRST_RUN_PATH = "/first-run-password";

interface SessionResponse {
  user: SessionUser;
  csrf_token: string;
}

let currentUser: SessionUser | null = null;

/** 当前登录用户;未登录为 null(check() 成功后可用)。 */
export function sessionUser(): SessionUser | null {
  return currentUser;
}

/** 对接后端 session 认证:login/logout/check 分别映射到对应端点。 */
export const authProvider: AuthProvider = {
  login: async ({ name, password }: { name: string; password: string }) => {
    try {
      const res = await apiFetch<SessionResponse>("/api/admin/auth/login", {
        method: "POST",
        body: JSON.stringify({ name, password }),
      });
      setCsrfToken(res.csrf_token, res.user.id);
      currentUser = res.user;
      return { success: true, redirectTo: res.user.must_change_password ? FIRST_RUN_PATH : "/" };
    } catch (err) {
      const message = err instanceof ApiError ? err.message : "网络错误,请稍后重试";
      return {
        success: false,
        error: { name: "登录失败", message },
      };
    }
  },
  logout: async () => {
    try {
      await apiFetch("/api/admin/auth/logout", { method: "POST" });
    } catch {
      // 会话可能已过期,登出视为成功。
    }
    currentUser = null;
    setCsrfToken("");
    return { success: true, redirectTo: "/login" };
  },
  check: async () => {
    try {
      const res = await apiFetch<SessionResponse>("/api/admin/me");
      setCsrfToken(res.csrf_token, res.user.id);
      currentUser = res.user;
      return { authenticated: true };
    } catch {
      return { authenticated: false, logout: true, redirectTo: "/login" };
    }
  },
  onError: async (error) => {
    if (error instanceof ApiError && error.status === 401) {
      return { logout: true, redirectTo: "/login", error };
    }
    return { error };
  },
  getIdentity: async () => currentUser,
  getPermissions: async () => currentUser?.role ?? null,
};
