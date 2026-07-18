/** 图片页共享类型:列表项契约与筛选状态。 */

import type { Dayjs } from "dayjs";

/** 图片列表/详情项(对应 /api/admin/images 响应)。 */
export interface ImageItem {
  id: string;
  url: string;
  user_id: string;
  token_id: number | null;
  token_name: string | null;
  file_name: string;
  mime: string;
  size: number;
  width: number;
  height: number;
  sha256: string;
  expire_at: string | null;
  ttl_seconds: number | null;
  last_access_at: string | null;
  pending_cleanup: boolean;
  cleanup_exempt: boolean;
  created_at: string;
  ref_count: number;
}

/** 图片列表的筛选状态;null 表示该维度不过滤。 */
export interface ImageFilterState {
  tokenId: number | null; // 按上传来源 Token 过滤
  userId: string | null; // 按归属用户过滤(仅管理员)
  range: [Dayjs, Dayjs] | null; // 按上传时间范围过滤(本地日期,含首尾两天)
}

/** 全空的筛选状态(初始值与"清除全部"共用)。 */
export const emptyFilters: ImageFilterState = { tokenId: null, userId: null, range: null };
