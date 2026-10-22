import { useMutation } from '@tanstack/react-query'
import { t } from '@/i18n'
import {
  FileDown,
  Loader2,
  MoreHorizontal,
  Pencil,
  Plus,
  RefreshCw,
  Trash2,
} from 'lucide-react'
import { toast } from 'sonner'
import { formatNumber, formatRelativeTime, formatTime } from '@/lib/format'
import { channelFailureText } from '@/lib/server-text'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { checkChannel, type ReplicaFilter, useInvalidateChannels } from './data'
import { TypeBadge } from './type-badge'
import { type ChannelItem, type ReplicaStats } from './types'

type ChannelTableProps = {
  items: ChannelItem[]
  onCreate: () => void
  onEdit: (channel: ChannelItem) => void
  onExport: (channel: ChannelItem) => void // 导出渠道的 rclone 配置
  onDelete: (channel: ChannelItem) => void
  onRemoveNow: (channel: ChannelItem) => void // 清理中的渠道:放弃清理立即移除
  onShowReplicas: (channel: ChannelItem, filter: ReplicaFilter) => void // 打开副本清单并带上过滤
}

/** 渠道列表:优先级降序,最高优先级的健康渠道标为主写。 */
export function ChannelTable({
  items,
  onCreate,
  onEdit,
  onExport,
  onDelete,
  onRemoveNow,
  onShowReplicas,
}: ChannelTableProps) {
  const m = useMessages()
  const invalidate = useInvalidateChannels()
  const check = useMutation({
    mutationFn: checkChannel,
    onSuccess: (info) => {
      if (info.failure) {
        toast.warning(
          t(m.channels.checkFailed, {
            name: info.name,
            error: channelFailureText(info.failure),
          })
        )
      } else {
        toast.success(t(m.channels.checkPassed, { name: info.name }))
      }
      invalidate()
    },
  })

  if (items.length === 0) {
    return (
      <Card>
        <CardContent className='flex flex-col items-center gap-3 py-14 text-center'>
          <div className='space-y-1'>
            <p className='font-medium'>{m.channels.empty}</p>
            <p className='text-sm text-muted-foreground'>
              {m.channels.emptyHint}
            </p>
          </div>
          <Button onClick={onCreate}>
            <Plus />
            {m.channels.create}
          </Button>
        </CardContent>
      </Card>
    )
  }

  const sorted = [...items].sort((a, b) => b.priority - a.priority)
  const primaryId = sorted.find((c) => c.healthy && !c.deleting)?.id

  return (
    <Card className='gap-0 overflow-hidden py-0'>
      <div className='overflow-x-auto'>
        <Table className='min-w-[720px]'>
          <TableHeader>
            <TableRow className='hover:bg-transparent'>
              <TableHead>{m.channels.colChannel}</TableHead>
              <TableHead className='w-24 text-end'>
                {m.channels.colPriority}
              </TableHead>
              <TableHead className='w-40'>{m.channels.colStatus}</TableHead>
              <TableHead className='w-56'>{m.channels.colReplicas}</TableHead>
              <TableHead className='w-14'>
                <span className='sr-only'>{m.common.actions}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {sorted.map((row) => (
              <TableRow
                key={row.id}
                className={cn(row.deleting && 'opacity-70')}
              >
                <TableCell>
                  <div className='flex items-center gap-3'>
                    <TypeBadge id={row.driver} className='size-8' />
                    <div className='min-w-0'>
                      <div className='flex flex-wrap items-center gap-1.5'>
                        <span className='truncate font-medium'>{row.name}</span>
                        {row.id === primaryId && (
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Badge className='h-5 px-1.5 text-[0.65rem]'>
                                {m.channels.primary}
                              </Badge>
                            </TooltipTrigger>
                            <TooltipContent>
                              {m.channels.primaryHint}
                            </TooltipContent>
                          </Tooltip>
                        )}
                      </div>
                      <div className='mt-0.5 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground'>
                        <span className='font-mono'>{row.driver}</span>
                        {row.encrypted && (
                          <Badge
                            variant='outline'
                            className='h-5 px-1.5 text-[0.65rem]'
                          >
                            {m.channels.encrypted}
                          </Badge>
                        )}
                        {row.allow_direct_link && (
                          <Badge
                            variant='outline'
                            className='h-5 px-1.5 text-[0.65rem]'
                          >
                            {m.channels.directLink}
                          </Badge>
                        )}
                      </div>
                    </div>
                  </div>
                </TableCell>
                <TableCell className='text-end font-mono text-sm tabular-nums'>
                  {row.priority}
                </TableCell>
                <TableCell>
                  <HealthCell row={row} />
                </TableCell>
                <TableCell>
                  <ReplicaCell
                    stats={row.replicas}
                    onOpen={(filter) => onShowReplicas(row, filter)}
                  />
                </TableCell>
                <TableCell className='text-end'>
                  <RowActions
                    row={row}
                    checking={check.isPending && check.variables === row.id}
                    onCheck={() => check.mutate(row.id)}
                    onEdit={() => onEdit(row)}
                    onExport={() => onExport(row)}
                    onDelete={() => onDelete(row)}
                    onRemoveNow={() => onRemoveNow(row)}
                  />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </Card>
  )
}

/** 状态列:健康 / 异常 / 清理中徽标(异常与清理中带原因),下方小字为最近探测的相对时间。 */
function HealthCell({ row }: { row: ChannelItem }) {
  const m = useMessages()
  const badge = row.deleting ? (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge
          variant='outline'
          className='cursor-help border-warning/40 bg-warning/10 text-warning'
        >
          {m.channels.deleting}
        </Badge>
      </TooltipTrigger>
      <TooltipContent className='max-w-xs'>
        {row.failure
          ? channelFailureText(row.failure)
          : m.channels.deletingHint}
      </TooltipContent>
    </Tooltip>
  ) : row.healthy ? (
    <Badge
      variant='outline'
      className='border-success/40 bg-success/10 text-success'
    >
      {m.channels.healthy}
    </Badge>
  ) : (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge
          variant='outline'
          className='cursor-help border-destructive/40 bg-destructive/10 text-destructive'
        >
          {m.channels.unhealthy}
        </Badge>
      </TooltipTrigger>
      <TooltipContent className='max-w-xs'>
        {row.failure
          ? channelFailureText(row.failure)
          : m.channels.probeFailedHint}
      </TooltipContent>
    </Tooltip>
  )
  return (
    <div className='flex flex-col items-start gap-1'>
      {badge}
      {row.last_checked_at === '' ? (
        <span className='text-xs text-muted-foreground'>
          {m.channels.neverChecked}
        </span>
      ) : (
        <Tooltip>
          <TooltipTrigger asChild>
            <span className='text-xs text-muted-foreground'>
              {t(m.channels.checkedAgo, {
                ago: formatRelativeTime(row.last_checked_at),
              })}
            </span>
          </TooltipTrigger>
          <TooltipContent>
            {t(m.channels.checkedAt, { time: formatTime(row.last_checked_at) })}
          </TooltipContent>
        </Tooltip>
      )}
    </div>
  )
}

/** 副本水位:完成百分比与 就绪 · 待补 · 失败;非零的待补 / 失败着色,可点开该渠道的副本清单。 */
function ReplicaCell({
  stats,
  onOpen,
}: {
  stats: ReplicaStats
  onOpen: (filter: ReplicaFilter) => void
}) {
  const m = useMessages()
  const total = stats.done + stats.pending + stats.failed
  const parts = [
    {
      n: stats.done,
      text: m.channels.replicaDone,
      hint: m.channels.replicaDoneHint,
      active: 'text-foreground',
    },
    {
      n: stats.pending,
      text: m.channels.replicaPending,
      hint: m.channels.replicaPendingHint,
      active: 'text-info',
      filter: 'pending' as const,
    },
    {
      n: stats.failed,
      text: m.channels.replicaFailed,
      hint: m.channels.replicaFailedHint,
      active: 'text-destructive',
      filter: 'failed' as const,
    },
  ]
  return (
    <div className='space-y-1'>
      {total > 0 && (
        <div className='text-xs font-medium tabular-nums'>
          {t(m.channels.replicaPercent, {
            percent: Math.floor((stats.done / total) * 100),
          })}
        </div>
      )}
      <div className='flex items-center gap-1.5 text-xs whitespace-nowrap tabular-nums'>
        {parts.map((part, i) => (
          <span key={part.text} className='flex items-center gap-1.5'>
            {i > 0 && <span className='text-muted-foreground/50'>·</span>}
            <Tooltip>
              <TooltipTrigger asChild>
                {part.filter && part.n > 0 ? (
                  <button
                    type='button'
                    className={cn(
                      'cursor-pointer rounded-sm underline-offset-4 outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50',
                      part.active
                    )}
                    onClick={() => onOpen(part.filter)}
                  >
                    {t(part.text, { n: formatNumber(part.n) })}
                  </button>
                ) : (
                  <span
                    className={cn(
                      'cursor-default',
                      part.n > 0 ? part.active : 'text-muted-foreground/60'
                    )}
                  >
                    {t(part.text, { n: formatNumber(part.n) })}
                  </span>
                )}
              </TooltipTrigger>
              <TooltipContent className='max-w-xs'>
                {t(part.hint, { n: formatNumber(part.n) })}
              </TooltipContent>
            </Tooltip>
          </span>
        ))}
      </div>
    </div>
  )
}

type RowActionsProps = {
  row: ChannelItem
  checking: boolean
  onCheck: () => void
  onEdit: () => void
  onExport: () => void
  onDelete: () => void
  onRemoveNow: () => void
}

/** 行操作菜单;清理中的渠道只剩「直接移除」一条出路。 */
function RowActions({
  row,
  checking,
  onCheck,
  onEdit,
  onExport,
  onDelete,
  onRemoveNow,
}: RowActionsProps) {
  const m = useMessages()
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button
          variant='ghost'
          size='icon'
          className='size-8 text-muted-foreground'
          disabled={checking}
        >
          {checking ? <Loader2 className='animate-spin' /> : <MoreHorizontal />}
          <span className='sr-only'>{m.channels.actionsMenu}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end'>
        {row.deleting ? (
          <DropdownMenuItem variant='destructive' onClick={onRemoveNow}>
            <Trash2 />
            {m.channels.actionRemoveNow}
          </DropdownMenuItem>
        ) : (
          <>
            <DropdownMenuItem onClick={onCheck}>
              <RefreshCw />
              {m.channels.actionCheck}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={onEdit}>
              <Pencil />
              {m.common.edit}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={onExport}>
              <FileDown />
              {m.channels.actionExportConfig}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant='destructive' onClick={onDelete}>
              <Trash2 />
              {m.common.delete}
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
