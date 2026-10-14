import { createFileRoute } from '@tanstack/react-router'
import { Dashboard } from '@/features/dashboard'
import { dashboardQueryOptions } from '@/features/dashboard/data'

export const Route = createFileRoute('/_authenticated/')({
  // 进页前预取,导航期间由顶部进度条反馈,进页即有数据;失败由页内三态展示。
  loader: ({ context }) =>
    context.queryClient.prefetchQuery(dashboardQueryOptions()),
  component: Dashboard,
})
