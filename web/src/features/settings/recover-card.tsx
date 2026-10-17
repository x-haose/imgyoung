import { useMutation } from '@tanstack/react-query'
import { t } from '@/i18n'
import { Loader2, ScanSearch } from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage, hasStatus } from '@/lib/errors'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { type RecoveryResult, recoverImages } from './data'

/** 找回图片记录:按渠道里的对象重新登记库里缺少记录的图片,执行后列出统计与没有找回的对象。 */
export function RecoverCard() {
  const r = useMessages().settings.recover
  const run = useMutation({
    mutationFn: recoverImages,
    onSuccess: (res) =>
      toast.success(t(r.done, { recovered: String(res.recovered) })),
    onError: (err) => {
      if (hasStatus(err, 401)) return
      toast.error(errorMessage(err))
    },
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle>{r.title}</CardTitle>
        <CardDescription>{r.desc}</CardDescription>
      </CardHeader>
      <CardContent className='grid gap-4'>
        <p className='text-sm text-muted-foreground'>{r.note}</p>
        <Button
          variant='outline'
          className='w-fit'
          disabled={run.isPending}
          onClick={() => run.mutate()}
        >
          {run.isPending ? (
            <Loader2 className='animate-spin' />
          ) : (
            <ScanSearch />
          )}
          {r.run}
        </Button>
        {run.data && <RecoveryOutcome result={run.data} />}
      </CardContent>
    </Card>
  )
}

type StatProps = { label: string; value: number }

function Stat({ label, value }: StatProps) {
  return (
    <div className='rounded-lg border px-4 py-3'>
      <div className='text-xs text-muted-foreground'>{label}</div>
      <div className='text-xl font-semibold tabular-nums'>{value}</div>
    </div>
  )
}

/** 一轮找回的统计,有失败时附逐条原因。 */
function RecoveryOutcome({ result }: { result: RecoveryResult }) {
  const r = useMessages().settings.recover
  const reasons: Record<string, string> = r.reasons
  return (
    <div className='grid gap-4'>
      <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
        <Stat label={r.scanned} value={result.scanned} />
        <Stat label={r.recorded} value={result.recorded} />
        <Stat label={r.recovered} value={result.recovered} />
        <Stat label={r.failed} value={result.failed} />
      </div>
      {result.failures.length > 0 && (
        <div className='grid gap-2'>
          <h4 className='text-sm font-medium'>{r.failuresTitle}</h4>
          <div className='overflow-hidden rounded-lg border'>
            <Table>
              <TableHeader>
                <TableRow className='bg-muted/60 hover:bg-muted/60'>
                  <TableHead className='ps-4'>{r.key}</TableHead>
                  <TableHead>{r.reason}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {result.failures.map((f) => (
                  <TableRow key={f.key}>
                    <TableCell className='ps-4 font-mono text-xs'>
                      {f.key}
                    </TableCell>
                    <TableCell
                      className='text-muted-foreground'
                      title={f.detail}
                    >
                      {reasons[f.reason] ?? f.reason}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          {result.failed > result.failures.length && (
            <p className='text-xs text-muted-foreground'>
              {t(r.truncated, {
                shown: String(result.failures.length),
                total: String(result.failed),
              })}
            </p>
          )}
        </div>
      )}
    </div>
  )
}
