/**
 * 图片页的地址栏状态:分页、筛选与当前打开的详情,全部可分享、刷新不丢。
 * 本文件随路由定义进入首屏入口块,只依赖轻量的解析器。
 */
import { bool, day, int, pageParam, searchSchema, str } from '@/lib/search'

/** 每页卡片数(6 列 × 4 行)。 */
export const PAGE_SIZE = 24

export const imagesSearchSchema = searchSchema({
  page: pageParam(1),
  q: str(),
  user: str(),
  token: int(0, 0), // 0 表示不按 Token 过滤;Token id 从 1 起
  since: day(),
  until: day(),
  exempt: bool(),
  view: str(), // 当前打开详情的图片短 ID
})

export type ImagesSearch = ReturnType<
  (typeof imagesSearchSchema)['~standard']['validate']
>['value']

/** 是否带了任一筛选条件(决定空态文案:无图 vs 无匹配)。 */
export function hasFilters(s: ImagesSearch): boolean {
  return Boolean(s.q || s.user || s.token || s.since || s.until || s.exempt)
}
