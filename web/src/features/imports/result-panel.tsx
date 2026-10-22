import { useMutation } from '@tanstack/react-query'
import { t } from '@/i18n'
import { FileJson, FileSpreadsheet, Loader2, TriangleAlert } from 'lucide-react'
import { useMessages } from '@/context/locale-provider'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { CountsStrip } from './counts-strip'
import {
  canRetry,
  downloadMapping,
  failureText,
  type ImportJob,
  type MappingFormat,
  processableTotal,
} from './data'
import { ProgressBar } from './progress-bar'

/** 结束状态(完成、取消、中止)的结果摘要:中止原因、各状态计数与对照表下载。 */
export function ResultPanel({ job }: { job: ImportJob }) {
  const m = useMessages().imports.detail
  const { counts, status } = job
  const headline = {
    done: m.resultDone,
    cancelled: m.resultCancelled,
    aborted: m.resultAborted,
  }[status as 'done' | 'cancelled' | 'aborted']
  return (
    <Card>
      <CardHeader>
        <CardTitle>{m.resultTitle}</CardTitle>
        <CardDescription>{headline}</CardDescription>
      </CardHeader>
      <CardContent className='space-y-4'>
        {status === 'aborted' && job.error_code && (
          <Alert className='border-destructive/40 bg-destructive/10'>
            <TriangleAlert className='text-destructive' />
            <AlertTitle>
              {t(m.abortedReason, { reason: failureText(job.error_code, '') })}
            </AlertTitle>
            <AlertDescription>{m.abortedHint}</AlertDescription>
          </Alert>
        )}
        <ProgressBar
          total={processableTotal(counts)}
          segments={[
            { value: counts.done, className: 'bg-success' },
            { value: counts.failed, className: 'bg-destructive' },
          ]}
          label={m.resultTitle}
        />
        <CountsStrip counts={counts} />
        {canRetry(job) && (
          <p className='text-xs text-muted-foreground'>{m.retryHint}</p>
        )}
        {counts.done > 0 && <MappingDownload id={job.id} />}
      </CardContent>
    </Card>
  )
}

/** 对照表下载:JSON 与 CSV 两个按钮,各自显示下载中状态。 */
function MappingDownload({ id }: { id: string }) {
  const m = useMessages().imports.detail
  const download = useMutation({
    mutationFn: (format: MappingFormat) => downloadMapping(id, format),
  })
  const busy = (format: MappingFormat) =>
    download.isPending && download.variables === format
  return (
    <div className='space-y-2 rounded-lg border bg-muted/30 p-3'>
      <div className='flex flex-wrap items-center gap-2'>
        <span className='me-1 text-sm font-medium'>{m.mapping}</span>
        {(['json', 'csv'] as const).map((format) => (
          <Button
            key={format}
            variant='outline'
            size='sm'
            disabled={download.isPending}
            onClick={() => download.mutate(format)}
          >
            {busy(format) ? (
              <Loader2 className='animate-spin' />
            ) : format === 'json' ? (
              <FileJson />
            ) : (
              <FileSpreadsheet />
            )}
            {format === 'json' ? m.mappingJson : m.mappingCsv}
          </Button>
        ))}
      </div>
      <p className='text-xs text-muted-foreground'>{m.mappingHint}</p>
    </div>
  )
}
