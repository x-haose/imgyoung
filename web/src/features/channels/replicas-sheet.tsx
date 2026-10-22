import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  type ColumnDef,
  getCoreRowModel,
  type PaginationState,
  useReactTable,
} from '@tanstack/react-table'
import { type Messages, t } from '@/i18n'
import { Loader2, RotateCw } from 'lucide-react'
import { toast } from 'sonner'
import { formatBytes, formatNumber, formatRelativeTime } from '@/lib/format'
import { failureText } from '@/lib/server-text'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { DataTableView } from '@/components/data-table'
import { QueryState } from '@/components/query-state'
import {
  type ChannelReplica,
  replicasQueryOptions,
  type ReplicaFilter,
  retryReplicas,
  useInvalidateChannels,
} from './data'
import { type ChannelItem } from './types'

const PAGE_SIZE = 20

/** 全部过滤在 Tabs 里的取值。 */
type Tab = ReplicaFilter | 'all'

type ReplicasSheetProps = {
  target: { channel: ChannelItem; filter: ReplicaFilter } | null // 要查看的渠道与初始过滤;null 为关闭
  channel: ChannelItem | null // 渠道列表里的最新状态,用于判断是否在清理中
  onClose: () => void
}

/** 渠道副本清单侧栏:按状态过滤的分页表,带「重试失败的副本」。 */
export function ReplicasSheet({
  target,
  channel,
  onClose,
}: ReplicasSheetProps) {
  const m = useMessages().channels
  return (
    <Sheet open={target !== null} onOpenChange={(open) => !open && onClose()}>
      <SheetContent className='flex w-full flex-col gap-0 p-0 sm:max-w-4xl'>
        <SheetHeader className='border-b'>
          <SheetTitle>
            {t(m.replicas.title, { name: target?.channel.name ?? '' })}
          </SheetTitle>
          <SheetDescription>{m.replicas.desc}</SheetDescription>
        </SheetHeader>
        {target && (
          <ReplicaList
            channel={target.channel}
            deleting={channel?.deleting ?? target.channel.deleting}
            initial={target.filter}
          />
        )}
      </SheetContent>
    </Sheet>
  )
}

function ReplicaList({
  channel,
  deleting,
  initial,
}: {
  channel: ChannelItem
  deleting: boolean
  initial: ReplicaFilter
}) {
  const m = useMessages().channels
  const invalidate = useInvalidateChannels()
  const [tab, setTab] = useState<Tab>(initial)
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: PAGE_SIZE,
  })
  const query = useQuery(
    replicasQueryOptions(channel.id, {
      state: tab === 'all' ? undefined : tab,
      page: pagination.pageIndex + 1,
      page_size: pagination.pageSize,
    })
  )
  const total = query.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / pagination.pageSize))
  // 重试后失败项变少,当前页可能越界。
  useEffect(() => {
    if (query.data && pagination.pageIndex >= pageCount) {
      setPagination((p) => ({ ...p, pageIndex: pageCount - 1 }))
    }
  }, [query.data, pagination.pageIndex, pageCount])

  const retry = useMutation({
    mutationFn: () => retryReplicas(channel.id),
    onSuccess: (res) => {
      toast.success(t(m.replicas.retried, { count: formatNumber(res.retried) }))
      invalidate()
    },
  })
  const columns = useMemo(() => replicaColumns(m), [m])
  const table = useReactTable({
    data: query.data?.items ?? [],
    columns,
    state: { pagination },
    manualPagination: true,
    pageCount,
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <div className='flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <Tabs
          value={tab}
          onValueChange={(v) => {
            setTab(v as Tab)
            setPagination((p) => ({ ...p, pageIndex: 0 }))
          }}
        >
          <TabsList>
            <TabsTrigger value='failed'>{m.replicas.filterFailed}</TabsTrigger>
            <TabsTrigger value='pending'>
              {m.replicas.filterPending}
            </TabsTrigger>
            <TabsTrigger value='done'>{m.replicas.filterDone}</TabsTrigger>
            <TabsTrigger value='all'>{m.replicas.filterAll}</TabsTrigger>
          </TabsList>
        </Tabs>
        <Tooltip>
          <TooltipTrigger asChild>
            <span>
              <Button
                variant='outline'
                disabled={deleting || retry.isPending}
                onClick={() => retry.mutate()}
              >
                {retry.isPending ? (
                  <Loader2 className='animate-spin' />
                ) : (
                  <RotateCw />
                )}
                {m.replicas.retry}
              </Button>
            </span>
          </TooltipTrigger>
          <TooltipContent className='max-w-xs'>
            {deleting ? m.replicas.retryDeleting : m.replicas.retryHint}
          </TooltipContent>
        </Tooltip>
      </div>
      <QueryState query={query}>
        <DataTableView
          table={table}
          pagination={total > pagination.pageSize}
          minWidthClassName='min-w-3xl'
          cellClassName='align-top'
          empty={
            <p className='text-sm text-muted-foreground'>{m.replicas.empty}</p>
          }
        />
      </QueryState>
    </div>
  )
}

/** 副本状态 → 徽标配色。 */
const stateClass: Record<ChannelReplica['state'], string> = {
  pending: 'border-info/30 bg-info/10 text-info',
  uploading: 'border-info/30 bg-info/10 text-info',
  done: 'border-success/30 bg-success/10 text-success',
  failed: 'border-destructive/30 bg-destructive/10 text-destructive',
}

function replicaColumns(m: Messages['channels']): ColumnDef<ChannelReplica>[] {
  return [
    {
      id: 'image',
      header: m.replicas.colImage,
      cell: ({ row }) => {
        const id = row.original.image_id
        if (id === null) {
          return (
            <Tooltip>
              <TooltipTrigger asChild>
                <span className='cursor-help text-muted-foreground'>
                  {m.replicas.noImage}
                </span>
              </TooltipTrigger>
              <TooltipContent>{m.replicas.noImageHint}</TooltipContent>
            </Tooltip>
          )
        }
        return (
          <Link
            to='/images'
            search={{ view: id }}
            className='font-mono text-xs underline-offset-4 hover:underline'
          >
            {id}
          </Link>
        )
      },
    },
    {
      id: 'refs',
      header: m.replicas.colRefs,
      cell: ({ row }) => (
        <span className='tabular-nums'>
          {formatNumber(row.original.image_count)}
        </span>
      ),
    },
    {
      id: 'state',
      header: m.replicas.colState,
      cell: ({ row }) => (
        <Badge variant='outline' className={cn(stateClass[row.original.state])}>
          {m.replicas.state[row.original.state]}
        </Badge>
      ),
    },
    {
      id: 'attempts',
      header: m.replicas.colAttempts,
      cell: ({ row }) => (
        <span className='tabular-nums'>
          {formatNumber(row.original.attempts)}
        </span>
      ),
    },
    {
      id: 'reason',
      header: m.replicas.colReason,
      cell: ({ row }) => {
        const f = row.original.failure
        if (f === null) return null
        return (
          <p className='text-sm whitespace-normal text-destructive'>
            {failureText(f)}
          </p>
        )
      },
      meta: { className: 'w-1/3' },
    },
    {
      id: 'size',
      header: m.replicas.colSize,
      cell: ({ row }) => (
        <span className='whitespace-nowrap tabular-nums'>
          {formatBytes(row.original.size)}
        </span>
      ),
    },
    {
      id: 'updated',
      header: m.replicas.colUpdated,
      cell: ({ row }) => (
        <span className='whitespace-nowrap text-muted-foreground'>
          {formatRelativeTime(row.original.updated_at)}
        </span>
      ),
    },
  ]
}
