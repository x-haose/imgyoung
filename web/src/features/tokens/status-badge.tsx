import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { Badge } from '@/components/ui/badge'
import { type TokenStatus } from './data'

/** 状态 → 语义色:有效用品牌绿,过期用警示黄,吊销退为灰。 */
const statusClass: Record<TokenStatus, string> = {
  active: 'border-success/30 bg-success/10 text-success',
  expired: 'border-warning/30 bg-warning/10 text-warning',
  revoked: 'border-border bg-muted text-muted-foreground',
}

export function TokenStatusBadge({ status }: { status: TokenStatus }) {
  const m = useMessages()
  return (
    <Badge variant='outline' className={cn('gap-1.5', statusClass[status])}>
      <span className='size-1.5 rounded-full bg-current' aria-hidden='true' />
      {m.tokens.status[status]}
    </Badge>
  )
}
