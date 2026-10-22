import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { type ImportCounts } from './data'

/** 状态计数 → 圆点颜色。 */
const dotClass: Record<keyof ImportCounts, string> = {
  done: 'bg-success',
  failed: 'bg-destructive',
  pending: 'bg-primary',
  skipped: 'bg-muted-foreground/50',
  candidate: 'bg-warning',
}

/** 各状态条目数的一排小计;为 0 的状态不显示。 */
export function CountsStrip({ counts }: { counts: ImportCounts }) {
  const m = useMessages()
  const keys = (Object.keys(dotClass) as (keyof ImportCounts)[]).filter(
    (k) => counts[k] > 0
  )
  return (
    <dl className='flex flex-wrap gap-x-6 gap-y-2'>
      {keys.map((k) => (
        <div key={k} className='flex items-center gap-2'>
          <span
            className={cn('size-2 rounded-full', dotClass[k])}
            aria-hidden='true'
          />
          <dt className='text-sm text-muted-foreground'>
            {m.imports.detail.counts[k]}
          </dt>
          <dd className='text-sm font-semibold tabular-nums'>
            {formatNumber(counts[k])}
          </dd>
        </div>
      ))}
    </dl>
  )
}
