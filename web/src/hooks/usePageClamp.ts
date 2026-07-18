import { useEffect } from "react";

/**
 * 受控分页的越界自愈:当前页超出最大页时回退到最后一个有效页。
 * 裁决/删除掉末页最后一条后,列表重取同一页会得到空 items,
 * antd 受控 current 不会自行回退,页面会停在空页且可能没有翻页入口;
 * 本 hook 监听服务端 total,检测到越界即拉回。
 * total 传服务端原始返回值:undefined 表示尚无成功响应(加载中/失败),
 * 此时不动作,避免把正常的翻页加载误判为越界而弹回第 1 页。
 */
export function usePageClamp(
  page: number,
  setPage: (page: number) => void,
  total: number | undefined,
  pageSize: number,
): void {
  useEffect(() => {
    if (total === undefined) {
      return;
    }
    const maxPage = Math.max(1, Math.ceil(total / pageSize));
    if (page > maxPage) {
      setPage(maxPage);
    }
  }, [page, setPage, total, pageSize]);
}
