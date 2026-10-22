import { createFileRoute, stripSearchParams } from '@tanstack/react-router'
import { bool, int, pageParam, searchSchema } from '@/lib/search'
import { Imports } from '@/features/imports'
import { IMPORTS_PAGE_SIZE } from '@/features/imports/data'

const defaults = { page: 1, page_size: IMPORTS_PAGE_SIZE }

export const Route = createFileRoute('/_authenticated/imports/')({
  validateSearch: searchSchema({
    page: pageParam(defaults.page),
    page_size: int(defaults.page_size, 1, 100),
    new: bool(), // 打开新建导入对话框
  }),
  // 缺省的页码与页长不写进地址栏。
  search: { middlewares: [stripSearchParams(defaults)] },
  component: Imports,
})
