import { useEffect, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import {
  type ColumnDef,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { type Messages, t } from '@/i18n'
import { ScrollText } from 'lucide-react'
import { formatTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { Page } from '@/components/layout/page'
import { Badge } from '@/components/ui/badge'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { DataTableView } from '@/components/data-table'
import { LongText } from '@/components/long-text'
import { QueryState } from '@/components/query-state'
import { type AuditAction } from './actions'
import {
  actionTone,
  AUDIT_PAGE_SIZE,
  type AuditFilters,
  type AuditItem,
  auditQueryOptions,
} from './data'
import { AuditFilterBar } from './filters'

const route = getRouteApi('/_authenticated/audit/')

function auditColumns(m: Messages['audit']): ColumnDef<AuditItem>[] {
  const labelOf = (action: string) =>
    (m.actions as Record<string, string>)[action] ?? action
  return [
    {
      accessorKey: 'created_at',
      header: m.time,
      cell: ({ getValue }) => (
        <span className='text-nowrap text-muted-foreground'>
          {formatTime(getValue<string>())}
        </span>
      ),
      meta: { className: 'ps-4' },
    },
    {
      accessorKey: 'action',
      header: m.action,
      cell: ({ getValue }) => {
        const action = getValue<string>()
        return (
          <Badge
            variant='outline'
            className={cn('font-normal', actionTone(action))}
          >
            {labelOf(action)}
          </Badge>
        )
      },
    },
    {
      accessorKey: 'actor_name',
      header: m.actor,
      cell: ({ row }) => (
        <Tooltip>
          <TooltipTrigger asChild>
            <span className='cursor-default font-medium'>
              {row.original.actor_name}
            </span>
          </TooltipTrigger>
          <TooltipContent>
            {t(m.actorIdHint, { id: row.original.actor })}
          </TooltipContent>
        </Tooltip>
      ),
      meta: { className: 'text-nowrap' },
    },
    {
      accessorKey: 'target',
      header: m.target,
      cell: ({ getValue }) => (
        <span className='font-mono text-xs break-all'>
          {getValue<string>()}
        </span>
      ),
    },
    {
      accessorKey: 'detail',
      header: m.detail,
      cell: ({ getValue }) => (
        <LongText
          className='max-w-md text-xs text-muted-foreground'
          contentClassName='max-w-sm break-all'
        >
          {getValue<string>()}
        </LongText>
      ),
      meta: { className: 'pe-4' },
    },
  ]
}

export function Audit() {
  const m = useMessages()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search,
      navigate,
      pagination: {
        pageKey: 'page',
        pageSizeKey: 'page_size',
        defaultPage: 1,
        defaultPageSize: AUDIT_PAGE_SIZE,
      },
      globalFilter: { enabled: false },
    })
  const filters: AuditFilters = {
    page: pagination.pageIndex + 1,
    page_size: pagination.pageSize,
    action: search.action as AuditAction | undefined,
    actor: search.actor,
    target: search.target,
    from: search.from,
    to: search.to,
  }
  const query = useQuery(auditQueryOptions(filters))
  const total = query.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / pagination.pageSize))
  const items = useMemo(() => query.data?.items ?? [], [query.data])

  useEffect(() => {
    if (query.data) ensurePageInRange(pageCount)
  }, [query.data, pageCount, ensurePageInRange])

  const columns = useMemo(() => auditColumns(m.audit), [m.audit])
  const table = useReactTable({
    data: items,
    columns,
    getRowId: (row) => String(row.id),
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    pageCount,
    state: { pagination },
    onPaginationChange,
  })

  // 改筛选条件回到第一页;空值从地址栏移除。
  const applyFilters = (
    patch: Partial<Omit<AuditFilters, 'page' | 'page_size'>>
  ) =>
    navigate({
      search: (prev) => ({ ...prev, ...patch, page: undefined }),
      replace: true,
    })

  return (
    <Page title={m.audit.title} description={t(m.audit.subtitle, { total })}>
      <AuditFilterBar filters={filters} onChange={applyFilters} />
      <QueryState query={query}>
        <DataTableView
          table={table}
          cellClassName='align-top'
          pagination={total > pagination.pageSize}
          empty={
            <div className='flex flex-col items-center gap-2'>
              <ScrollText className='size-8 text-muted-foreground/60' />
              <p className='font-medium text-foreground'>{m.audit.empty}</p>
            </div>
          }
        />
      </QueryState>
    </Page>
  )
}
