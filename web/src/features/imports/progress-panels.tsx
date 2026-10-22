import { t } from '@/i18n'
import { formatNumber } from '@/lib/format'
import { useMessages } from '@/context/locale-provider'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { CountsStrip } from './counts-strip'
import { type ImportJob, processableTotal } from './data'
import { ProgressBar } from './progress-bar'

/** 扫描中:已处理页数与已发现页数;发现数为 0 时显示排队或读取中的说明。 */
export function ScanningPanel({ job }: { job: ImportJob }) {
  const m = useMessages().imports.detail
  const { pages_done: done, pages_found: found } = job.scan
  return (
    <Card>
      <CardHeader>
        <CardTitle>{m.scanTitle}</CardTitle>
        <CardDescription>
          {found === 0 ? m.scanWaiting : m.scanFound}
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-3'>
        <ProgressBar
          total={found}
          indeterminate={found === 0}
          segments={[{ value: done, className: 'bg-primary' }]}
          label={m.scanTitle}
        />
        {found > 0 && (
          <p className='text-sm tabular-nums'>
            {t(m.scanProgress, {
              done: formatNumber(done),
              found: formatNumber(found),
            })}
          </p>
        )}
      </CardContent>
    </Card>
  )
}

/** 导入中:已导入与失败占总量的比例,各状态计数;还没有任何进展时提示可能在排队。 */
export function ImportingPanel({ job }: { job: ImportJob }) {
  const m = useMessages().imports.detail
  const { counts } = job
  const total = processableTotal(counts)
  const processed = counts.done + counts.failed
  return (
    <Card>
      <CardHeader>
        <CardTitle>{m.importTitle}</CardTitle>
        {processed === 0 && (
          <CardDescription>{m.importWaiting}</CardDescription>
        )}
      </CardHeader>
      <CardContent className='space-y-4'>
        <ProgressBar
          total={total}
          segments={[
            { value: counts.done, className: 'bg-success' },
            { value: counts.failed, className: 'bg-destructive' },
          ]}
          label={m.importTitle}
        />
        <p className='text-sm tabular-nums'>
          {t(m.importProgress, {
            done: formatNumber(processed),
            total: formatNumber(total),
          })}
        </p>
        <CountsStrip counts={counts} />
      </CardContent>
    </Card>
  )
}
