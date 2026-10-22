import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  type ColumnDef,
  getCoreRowModel,
  type PaginationState,
  useReactTable,
} from '@tanstack/react-table'
import { type Messages, t } from '@/i18n'
import { formatNumber } from '@/lib/format'
import { useMessages } from '@/context/locale-provider'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { DataTableView } from '@/components/data-table'
import { QueryState } from '@/components/query-state'
import { failureText, importItemsQueryOptions, type ImportItem } from './data'

const PAGE_SIZE = 20

type FailuresTableProps = {
  id: string
  live: boolean // 任务进行中:轮询以实时看到新增的失败项
}

/** 失败条目表:原因按错误码本地化,根因原文附在下方;分页在组件内。 */
export function FailuresTable({ id, live }: FailuresTableProps) {
  const m = useMessages().imports
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: PAGE_SIZE,
  })
  const query = useQuery(
    importItemsQueryOptions(
      id,
      {
        state: 'failed',
        page: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
      },
      live
    )
  )
  const total = query.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / pagination.pageSize))
  // 重试或删除后失败项变少,当前页可能越界。
  useEffect(() => {
    if (query.data && pagination.pageIndex >= pageCount) {
      setPagination((p) => ({ ...p, pageIndex: pageCount - 1 }))
    }
  }, [query.data, pagination.pageIndex, pageCount])

  const columns = useMemo(() => failureColumns(m), [m])
  const table = useReactTable({
    data: query.data?.items ?? [],
    columns,
    getRowId: (row) => String(row.id),
    state: { pagination },
    manualPagination: true,
    pageCount,
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          {query.data
            ? t(m.failures.titleCount, { count: formatNumber(total) })
            : m.failures.title}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <QueryState query={query}>
          <DataTableView
            table={table}
            pagination={total > pagination.pageSize}
            minWidthClassName='min-w-3xl'
            cellClassName='align-top'
            empty={
              <p className='text-sm text-muted-foreground'>
                {m.failures.empty}
              </p>
            }
          />
        </QueryState>
      </CardContent>
    </Card>
  )
}

function failureColumns(m: Messages['imports']): ColumnDef<ImportItem>[] {
  return [
    {
      id: 'url',
      header: m.failures.url,
      cell: ({ row }) => (
        <p className='font-mono text-xs break-all'>{row.original.url}</p>
      ),
      meta: { className: 'w-1/2' },
    },
    {
      id: 'reason',
      header: m.failures.reason,
      cell: ({ row }) => {
        const { error_code: code, error_detail: detail } = row.original
        return (
          <div className='space-y-0.5 whitespace-normal'>
            <p className='text-sm text-destructive'>
              {failureText(code, detail)}
            </p>
            {detail !== '' && (
              <p className='text-xs break-all text-muted-foreground'>
                {detail}
              </p>
            )}
          </div>
        )
      },
    },
    {
      id: 'sources',
      header: m.failures.sources,
      cell: ({ row }) => {
        const { sources, source_count: count } = row.original
        if (sources.length === 0) return null
        return (
          <div className='space-y-0.5 whitespace-normal'>
            <p className='text-xs break-all text-muted-foreground'>
              {sources[0]}
            </p>
            {count > 1 && (
              <p className='text-xs text-muted-foreground'>
                {t(m.failures.moreSources, { count: formatNumber(count - 1) })}
              </p>
            )}
          </div>
        )
      },
    },
  ]
}
