import { useEffect, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import {
  type ColumnDef,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { PLACEHOLDER, formatTime } from '@/lib/format'
import { useMessages } from '@/context/locale-provider'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { DataTableView } from '@/components/data-table'
import { QueryState } from '@/components/query-state'
import { type AlertItem, alertHistoryQueryOptions } from './data'

const route = getRouteApi('/_authenticated/settings/')

/** 路由搜索参数里分页的缺省值;等于缺省时不写进地址栏。 */
const DEFAULT_PAGE = 1
const DEFAULT_PAGE_SIZE = 20

/** 告警历史分区:服务端分页的告警表,未恢复的行高亮。 */
export function AlertsTab() {
  const m = useMessages()
  const a = m.settings.alerts
  const { page, page_size: pageSize } = route.useSearch()
  const navigate = route.useNavigate()
  const query = useQuery(alertHistoryQueryOptions(page, pageSize))
  const items = query.data?.items ?? []
  const total = query.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / pageSize))

  // 末页记录被回收后当前页可能越界,拉回最后一个有效页。
  useEffect(() => {
    if (query.data && page > pageCount) {
      void navigate({
        search: (prev) => ({
          ...prev,
          page: pageCount === DEFAULT_PAGE ? undefined : pageCount,
        }),
        replace: true,
      })
    }
  }, [query.data, page, pageCount, navigate])

  const kinds = a.kinds as Record<string, string>
  const columns = useMemo<ColumnDef<AlertItem>[]>(
    () => [
      {
        id: 'status',
        header: a.status,
        cell: ({ row }) =>
          row.original.resolved_at ? (
            <Badge variant='outline' className='text-muted-foreground'>
              {a.resolved}
            </Badge>
          ) : (
            <Badge className='border-transparent bg-warning/15 text-warning'>
              {a.open}
            </Badge>
          ),
      },
      {
        accessorKey: 'kind',
        header: a.kind,
        cell: ({ getValue }) => {
          const kind = getValue<string>()
          return kinds[kind] ?? kind
        },
      },
      {
        accessorKey: 'target',
        header: a.target,
        cell: ({ getValue }) => (
          <span className='font-mono text-xs'>
            {getValue<string>() || PLACEHOLDER}
          </span>
        ),
      },
      {
        accessorKey: 'message',
        header: a.message,
        meta: { className: 'min-w-64 whitespace-normal' },
      },
      {
        accessorKey: 'opened_at',
        header: a.openedAt,
        cell: ({ getValue }) => (
          <span className='text-muted-foreground'>
            {formatTime(getValue<string>())}
          </span>
        ),
      },
      {
        accessorKey: 'resolved_at',
        header: a.resolvedAt,
        cell: ({ getValue }) => (
          <span className='text-muted-foreground'>
            {formatTime(getValue<string | null>())}
          </span>
        ),
      },
    ],
    [a, kinds]
  )

  const table = useReactTable({
    data: items,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    pageCount,
    state: { pagination: { pageIndex: page - 1, pageSize } },
    onPaginationChange: (updater) => {
      const current = { pageIndex: page - 1, pageSize }
      const next = typeof updater === 'function' ? updater(current) : updater
      const nextPage = next.pageIndex + 1
      void navigate({
        search: (prev) => ({
          ...prev,
          page: nextPage === DEFAULT_PAGE ? undefined : nextPage,
          page_size:
            next.pageSize === DEFAULT_PAGE_SIZE ? undefined : next.pageSize,
        }),
      })
    },
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>{a.title}</CardTitle>
        <CardDescription>{a.desc}</CardDescription>
      </CardHeader>
      <CardContent>
        <QueryState
          query={query}
          skeleton={<Skeleton className='h-48 w-full rounded-lg' />}
        >
          <DataTableView
            table={table}
            minWidthClassName='min-w-3xl'
            pagination={total > pageSize}
            rowClassName={(row) =>
              row.original.resolved_at
                ? undefined
                : 'bg-warning/5 hover:bg-warning/10'
            }
            empty={<span className='text-muted-foreground'>{a.empty}</span>}
          />
        </QueryState>
      </CardContent>
    </Card>
  )
}
