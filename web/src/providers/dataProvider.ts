import type { DataProvider } from "@refinedev/core";

import { apiFetch } from "../api/http";

/** 后端列表信封:{items, total}。 */
interface ListEnvelope<T> {
  items: T[];
  total: number;
}

/** 资源名 → 管理 API 路径。 */
function resourceUrl(resource: string): string {
  return `/api/admin/${resource}`;
}

/**
 * 对接自有 REST 信封的薄 dataProvider:
 * 列表 = GET ?page&page_size&<过滤参数>,成功体 {items,total};
 * 其余按 REST 惯例映射,错误统一由 apiFetch 抛 ApiError 交 Refine 通知层。
 */
export const dataProvider: DataProvider = {
  getApiUrl: () => "/api/admin",

  getList: async ({ resource, pagination, filters }) => {
    const params = new URLSearchParams();
    const page = pagination?.currentPage ?? 1;
    const pageSize = pagination?.pageSize ?? 20;
    params.set("page", String(page));
    params.set("page_size", String(pageSize));
    for (const f of filters ?? []) {
      // 后端过滤全部是字段等值/包含语义,直接把 field=value 平铺为查询参数。
      if ("field" in f && f.value !== undefined && f.value !== null && f.value !== "") {
        params.set(f.field, String(f.value));
      }
    }
    const res = await apiFetch<ListEnvelope<Record<string, unknown>>>(
      `${resourceUrl(resource)}?${params.toString()}`,
    );
    return { data: res.items as never, total: res.total };
  },

  getOne: async ({ resource, id }) => {
    const data = await apiFetch<Record<string, unknown>>(`${resourceUrl(resource)}/${id}`);
    return { data: data as never };
  },

  create: async ({ resource, variables }) => {
    const data = await apiFetch<Record<string, unknown>>(resourceUrl(resource), {
      method: "POST",
      body: JSON.stringify(variables ?? {}),
    });
    return { data: (data ?? {}) as never };
  },

  update: async ({ resource, id, variables }) => {
    const data = await apiFetch<Record<string, unknown> | undefined>(
      `${resourceUrl(resource)}/${id}`,
      { method: "PUT", body: JSON.stringify(variables ?? {}) },
    );
    return { data: (data ?? {}) as never };
  },

  deleteOne: async ({ resource, id }) => {
    await apiFetch(`${resourceUrl(resource)}/${id}`, { method: "DELETE" });
    return { data: {} as never };
  },

  custom: async ({ url, method, payload }) => {
    const data = await apiFetch<unknown>(url, {
      method: (method ?? "get").toUpperCase(),
      body: payload === undefined ? undefined : JSON.stringify(payload),
    });
    return { data: data as never };
  },
};
