import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { t } from '@/i18n'
import {
  DatabaseBackup,
  Download,
  History,
  Loader2,
  TriangleAlert,
} from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage, hasStatus } from '@/lib/errors'
import { formatBytes } from '@/lib/format'
import { useMessages } from '@/context/locale-provider'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { QueryState } from '@/components/query-state'
import {
  type Snapshot,
  backupsQueryOptions,
  createBackup,
  downloadBackup,
  downloadExport,
  restoreBackup,
} from './data'
import { formatInstant } from './format'

/** 反向代理等待上游超时的状态码:Nginx 504,Cloudflare 524。 */
const PROXY_TIMEOUT_STATUSES = [504, 524]

/** 备份与导出分区:快照表需要整行宽度,导出卡片排在其下。 */
export function BackupTab() {
  return (
    <div className='grid max-w-5xl gap-6'>
      <BackupsCard />
      <ExportCard />
    </div>
  )
}

function ExportCard() {
  const x = useMessages().settings.export
  const exp = useMutation({ mutationFn: downloadExport })
  return (
    <Card>
      <CardHeader>
        <CardTitle>{x.title}</CardTitle>
        <CardDescription>{x.desc}</CardDescription>
      </CardHeader>
      <CardContent className='grid gap-4'>
        <Alert className='border-warning/40 bg-warning/10'>
          <TriangleAlert className='text-warning' />
          <AlertTitle>{x.keyWarningTitle}</AlertTitle>
          <AlertDescription>{x.keyWarningDesc}</AlertDescription>
        </Alert>
        <Button
          variant='outline'
          className='w-fit'
          disabled={exp.isPending}
          onClick={() => exp.mutate()}
        >
          {exp.isPending ? <Loader2 className='animate-spin' /> : <Download />}
          {x.download}
        </Button>
      </CardContent>
    </Card>
  )
}

function BackupsCard() {
  const m = useMessages()
  const b = m.settings.backup
  const queryClient = useQueryClient()
  const query = useQuery(backupsQueryOptions())
  const [restoring, setRestoring] = useState<Snapshot | null>(null)
  const [restarting, setRestarting] = useState(false)

  // 部分渠道没写成时请求失败,但已写成的渠道上多了这份快照,成败都要刷新列表。
  const create = useMutation({
    mutationFn: createBackup,
    onSuccess: (snap) => {
      toast.success(t(b.created, { name: snap.name }))
    },
    onSettled: () =>
      queryClient.invalidateQueries({
        queryKey: backupsQueryOptions().queryKey,
      }),
  })
  const restore = useMutation({
    mutationFn: (snap: Snapshot) => restoreBackup(snap.name),
    onSuccess: () => {
      setRestoring(null)
      setRestarting(true)
    },
    // 代理到点断开时服务端已中止并撤回恢复,通用的「请求失败(504)」看不出这一点;401 由会话失效处理。
    onError: (err) => {
      if (hasStatus(err, 401)) return
      toast.error(
        PROXY_TIMEOUT_STATUSES.some((s) => hasStatus(err, s))
          ? b.restoreProxyTimeout
          : errorMessage(err)
      )
    },
  })

  const items = query.data?.items ?? []
  return (
    <Card>
      <CardHeader>
        <CardTitle>{b.title}</CardTitle>
        <CardDescription>{b.desc}</CardDescription>
        <CardAction>
          <Button
            size='sm'
            disabled={create.isPending || restarting}
            onClick={() => create.mutate()}
          >
            {create.isPending ? (
              <Loader2 className='animate-spin' />
            ) : (
              <DatabaseBackup />
            )}
            {b.createNow}
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        {restarting ? (
          <RestartWatch />
        ) : (
          <QueryState
            query={query}
            skeleton={<Skeleton className='h-32 w-full rounded-lg' />}
          >
            {items.length === 0 ? (
              <p className='py-8 text-center text-sm text-muted-foreground'>
                {b.empty}
              </p>
            ) : (
              <SnapshotTable items={items} onRestore={setRestoring} />
            )}
          </QueryState>
        )}
      </CardContent>

      <ConfirmDialog
        open={restoring !== null}
        onOpenChange={(open) => !open && setRestoring(null)}
        title={t(b.restoreTitle, { name: restoring?.name ?? '' })}
        desc={b.restoreDesc}
        confirmText={b.restoreConfirm}
        destructive
        isLoading={restore.isPending}
        handleConfirm={() => restoring && restore.mutate(restoring)}
        className='sm:max-w-lg'
      />
    </Card>
  )
}

type SnapshotTableProps = {
  items: Snapshot[] // 快照,最新在前
  onRestore: (snap: Snapshot) => void // 发起恢复确认
}

/** 快照表:每行一份快照,列出持有它的渠道,行尾下载与恢复。 */
function SnapshotTable({ items, onRestore }: SnapshotTableProps) {
  const m = useMessages()
  const b = m.settings.backup
  const dl = useMutation({ mutationFn: downloadBackup })
  return (
    <div className='overflow-hidden rounded-lg border'>
      <Table>
        <TableHeader>
          <TableRow className='bg-muted/60 hover:bg-muted/60'>
            <TableHead className='ps-4'>{b.name}</TableHead>
            <TableHead>{b.size}</TableHead>
            <TableHead>{b.takenAt}</TableHead>
            <TableHead>{b.channels}</TableHead>
            <TableHead className='pe-3 text-end'>{m.common.actions}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((snap) => (
            <TableRow key={snap.name}>
              <TableCell className='ps-4 font-mono text-xs'>
                {snap.name}
              </TableCell>
              <TableCell className='text-muted-foreground'>
                {formatBytes(snap.size)}
              </TableCell>
              <TableCell className='text-muted-foreground'>
                {formatInstant(snap.taken_at)}
              </TableCell>
              <TableCell>
                <div className='flex flex-wrap gap-1'>
                  {snap.channels.map((name) => (
                    <Badge
                      key={name}
                      variant='outline'
                      className='font-normal text-muted-foreground'
                    >
                      {name}
                    </Badge>
                  ))}
                </div>
              </TableCell>
              <TableCell className='pe-2 text-end'>
                <div className='flex justify-end gap-1'>
                  <Button
                    variant='ghost'
                    size='sm'
                    disabled={dl.isPending}
                    onClick={() => dl.mutate(snap.name)}
                  >
                    {dl.isPending && dl.variables === snap.name ? (
                      <Loader2 className='animate-spin' />
                    ) : (
                      <Download />
                    )}
                    {b.download}
                  </Button>
                  <Button
                    variant='ghost'
                    size='sm'
                    className='text-destructive hover:text-destructive'
                    onClick={() => onRestore(snap)}
                  >
                    <History />
                    {b.restore}
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

/** 进程重启后仍提示过慢的阈值(毫秒)。 */
const RESTART_SLOW_AFTER = 60_000

/**
 * 恢复已受理后的等待:先等旧进程退出(健康检查失联),再等新进程起来,起来即整页刷新。
 * 直接等健康检查成功会被尚未退出的旧进程骗到。
 */
function RestartWatch() {
  const b = useMessages().settings.backup
  const [slow, setSlow] = useState(false)

  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let down = false
    const started = Date.now()
    const probe = async (): Promise<boolean> => {
      try {
        const resp = await fetch('/healthz', { cache: 'no-store' })
        return resp.ok
      } catch {
        // 进程退出期间连接被拒绝,这正是「已停机」的信号。
        return false
      }
    }
    const tick = async () => {
      const up = await probe()
      if (cancelled) return
      if (!down && !up) down = true
      if (down && up) {
        window.location.reload()
        return
      }
      if (Date.now() - started > RESTART_SLOW_AFTER) setSlow(true)
      timer = setTimeout(tick, down ? 2000 : 1000)
    }
    void tick()
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [])

  return (
    <Alert>
      <Loader2 className='animate-spin text-primary' />
      <AlertTitle>{b.restarting}</AlertTitle>
      <AlertDescription>
        <p>{b.restartingDesc}</p>
        {slow && <p className='text-warning'>{b.restartSlow}</p>}
      </AlertDescription>
    </Alert>
  )
}
