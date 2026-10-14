import { createFileRoute, stripSearchParams } from '@tanstack/react-router'
import { Images } from '@/features/images'
import { imagesSearchSchema } from '@/features/images/search'

export const Route = createFileRoute('/_authenticated/images/')({
  validateSearch: imagesSearchSchema,
  // 缺省值不写进地址栏,分享出去的链接只带真正改过的条件。
  search: { middlewares: [stripSearchParams({ page: 1, token: 0 })] },
  component: Images,
})
