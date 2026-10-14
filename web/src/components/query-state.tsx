import { RefreshCw, TriangleAlert } from 'lucide-react'
import { errorMessage } from '@/lib/errors'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

/** 所需的最小查询状态切面,useQuery 的返回值直接满足。 */
type QueryLike = {
  isPending: boolean // 尚无数据
  isError: boolean // 请求失败
  error: unknown // 失败原因
  refetch: () => unknown // 重试入口
}

type QueryStateProps = {
  query: QueryLike
  skeleton?: React.ReactNode // 首次加载的占位;不传用通用骨架
  children: React.ReactNode
}

/**
 * 统一列表 / 统计页的三态渲染:失败 → 带具体原因与重试按钮的错误占位
 * (避免失败被渲染成「空态 / 全 0」);首次加载 → 骨架屏;其余 → children。
 */
export function QueryState({ query, skeleton, children }: QueryStateProps) {
  const m = useMessages()
  if (query.isError) {
    return (
      <Card className='border-destructive/40'>
        <CardContent className='flex flex-col items-center gap-3 py-10 text-center'>
          <TriangleAlert className='size-8 text-destructive' />
          <div className='space-y-1'>
            <p className='font-medium'>{m.common.loadFailed}</p>
            <p className='text-sm text-muted-foreground'>
              {errorMessage(query.error)}
            </p>
          </div>
          <Button variant='outline' size='sm' onClick={() => query.refetch()}>
            <RefreshCw />
            {m.common.retry}
          </Button>
        </CardContent>
      </Card>
    )
  }
  if (query.isPending) {
    return (
      skeleton ?? (
        <div className='space-y-3'>
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className='h-10 w-full' />
          ))}
        </div>
      )
    )
  }
  return children
}
