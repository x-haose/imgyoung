import { formatBytes } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'

type UsageBarProps = {
  used: number
  quota: number | null // null 或 0 表示不限,只显示已用
}

/** 用量条:接近配额时变黄,逼近上限时变红;不限配额时只给数字。 */
export function UsageBar({ used, quota }: UsageBarProps) {
  const m = useMessages()
  if (!quota) {
    return (
      <span className='text-sm text-muted-foreground tabular-nums'>
        {formatBytes(used)} / {m.users.unlimited}
      </span>
    )
  }
  const ratio = Math.min(1, used / quota)
  return (
    <div className='flex min-w-40 flex-col gap-1.5'>
      <div className='h-1.5 w-full overflow-hidden rounded-full bg-muted'>
        <div
          className={cn(
            'h-full rounded-full transition-[width]',
            ratio >= 0.95
              ? 'bg-destructive'
              : ratio >= 0.8
                ? 'bg-warning'
                : 'bg-primary'
          )}
          style={{ width: `${Math.max(2, ratio * 100)}%` }}
        />
      </div>
      <span className='text-xs text-muted-foreground tabular-nums'>
        {formatBytes(used)} / {formatBytes(quota)}
      </span>
    </div>
  )
}
