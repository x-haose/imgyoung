import { type Messages, t } from '@/i18n'
import { formatNumber } from '@/lib/format'
import { type ImportJob, processableTotal } from './data'
import { ProgressBar } from './progress-bar'

/** 列表里的进度:一行说明加一条细进度条;待确认的任务只有发现数,没有进度条。 */
export function JobProgress({
  job,
  m,
}: {
  job: ImportJob
  m: Messages['imports']
}) {
  const { status, counts, scan } = job
  if (status === 'ready') {
    return (
      <span className='text-sm text-muted-foreground'>
        {t(m.progress.candidates, { count: formatNumber(counts.candidate) })}
      </span>
    )
  }
  if (status === 'scanning') {
    return (
      <div className='space-y-1.5'>
        <span className='text-sm text-muted-foreground'>
          {scan.pages_found === 0
            ? m.progress.waiting
            : t(m.progress.pages, {
                done: formatNumber(scan.pages_done),
                found: formatNumber(scan.pages_found),
              })}
        </span>
        <ProgressBar
          total={scan.pages_found}
          indeterminate={scan.pages_found === 0}
          segments={[{ value: scan.pages_done, className: 'bg-primary' }]}
          label={m.status.scanning}
        />
      </div>
    )
  }
  const total = processableTotal(counts)
  const text = {
    done: formatNumber(counts.done),
    total: formatNumber(total),
    failed: formatNumber(counts.failed),
  }
  return (
    <div className='space-y-1.5'>
      <span className='text-sm text-muted-foreground'>
        {counts.failed > 0
          ? t(m.progress.imagesFailed, text)
          : t(m.progress.images, text)}
      </span>
      <ProgressBar
        total={total}
        segments={[
          { value: counts.done, className: 'bg-success' },
          { value: counts.failed, className: 'bg-destructive' },
        ]}
        label={m.status[status]}
      />
    </div>
  )
}
