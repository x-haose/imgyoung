import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { Badge } from '@/components/ui/badge'
import { type ImportStatus, isRunning } from './data'

/** 状态 → 语义色:进行中用信息蓝并脉动,待确认用警示黄,完成为绿,中止为红,取消退为灰。 */
const statusClass: Record<ImportStatus, string> = {
  scanning: 'border-info/30 bg-info/10 text-info',
  ready: 'border-warning/30 bg-warning/10 text-warning',
  importing: 'border-info/30 bg-info/10 text-info',
  done: 'border-success/30 bg-success/10 text-success',
  cancelled: 'border-border bg-muted text-muted-foreground',
  aborted: 'border-destructive/30 bg-destructive/10 text-destructive',
}

/** 导入任务状态徽标。 */
export function ImportStatusBadge({ status }: { status: ImportStatus }) {
  const m = useMessages()
  return (
    <Badge variant='outline' className={cn('gap-1.5', statusClass[status])}>
      <span
        className={cn(
          'size-1.5 rounded-full bg-current',
          isRunning(status) && 'animate-pulse'
        )}
        aria-hidden='true'
      />
      {m.imports.status[status]}
    </Badge>
  )
}
