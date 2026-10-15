import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, Link } from '@tanstack/react-router'
import {
  getCoreRowModel,
  type RowSelectionState,
  useReactTable,
} from '@tanstack/react-table'
import { type Messages, t } from '@/i18n'
import { Info, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { errorText } from '@/lib/server-text'
import { useMessages } from '@/context/locale-provider'
import { useServerConfig } from '@/hooks/use-server-config'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { Page } from '@/components/layout/page'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableView } from '@/components/data-table'
import { QueryState } from '@/components/query-state'
import { cleanupColumns } from './columns'
import {
  affectedByDecision,
  batchDeleteImages,
  CLEANUP_PAGE_SIZE,
  cleanupQueryOptions,
  deleteImage,
  keepImage,
} from './data'

const route = getRouteApi('/_authenticated/cleanup/')

/** 待确认的裁决:对一张或多张图保留 / 删除。 */
type Decision = { kind: 'keep' | 'delete'; ids: string[] }

export function Cleanup() {
  const m = useMessages()
  const queryClient = useQueryClient()
  const config = useServerConfig()
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({})
  const [decision, setDecision] = useState<Decision | null>(null)

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search: route.useSearch(),
      navigate: route.useNavigate(),
      pagination: {
        pageKey: 'page',
        pageSizeKey: 'page_size',
        defaultPage: 1,
        defaultPageSize: CLEANUP_PAGE_SIZE,
      },
      globalFilter: { enabled: false },
    })
  const query = useQuery(
    cleanupQueryOptions(pagination.pageIndex + 1, pagination.pageSize)
  )
  const total = query.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / pagination.pageSize))
  const items = useMemo(() => query.data?.items ?? [], [query.data])

  useEffect(() => {
    if (query.data) ensurePageInRange(pageCount)
  }, [query.data, pageCount, ensurePageInRange])

  const columns = useMemo(
    () =>
      cleanupColumns(m.cleanup, config?.process_widths, {
        onKeep: (item) => setDecision({ kind: 'keep', ids: [item.id] }),
        onDelete: (item) => setDecision({ kind: 'delete', ids: [item.id] }),
      }),
    [m.cleanup, config?.process_widths]
  )
  const table = useReactTable({
    data: items,
    columns,
    getRowId: (row) => row.id,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    pageCount,
    enableRowSelection: true,
    state: { pagination, rowSelection },
    onRowSelectionChange: setRowSelection,
    // 勾选只在当前页内有效,翻页即清空,批量请求因此不会超过单页上限。
    onPaginationChange: (updater) => {
      setRowSelection({})
      onPaginationChange(updater)
    },
  })
  const selectedIds = Object.keys(rowSelection)

  const settle = () => {
    setRowSelection({})
    setDecision(null)
    for (const key of affectedByDecision) {
      void queryClient.invalidateQueries({ queryKey: key })
    }
  }
  // 保留是单图 PATCH:顺序执行,失败计数后继续,结果一次汇报。
  const keep = useMutation({
    mutationFn: async (ids: string[]) => {
      let done = 0
      let failed = 0
      for (const id of ids) {
        try {
          await keepImage(id)
          done++
        } catch {
          failed++
        }
      }
      return { done, failed, single: ids.length === 1 }
    },
    onSuccess: ({ done, failed, single }) => {
      if (failed > 0) {
        toast.warning(t(m.cleanup.batchKeepPartial, { done, failed }))
      } else {
        toast.success(
          single ? m.cleanup.kept : t(m.cleanup.batchKeepDone, { done })
        )
      }
      settle()
    },
  })
  const remove = useMutation({
    mutationFn: async (ids: string[]) => {
      if (ids.length === 1) {
        await deleteImage(ids[0])
        return null
      }
      return batchDeleteImages(ids)
    },
    onSuccess: (result) => {
      if (result === null) {
        toast.success(m.cleanup.deleted)
      } else if (result.failed.length > 0) {
        toast.warning(
          t(m.cleanup.batchDeletePartial, {
            deleted: result.deleted,
            failed: result.failed.length,
            error: errorText(result.failed[0]),
          })
        )
      } else {
        toast.success(t(m.cleanup.batchDeleteDone, { count: result.deleted }))
      }
      settle()
    },
  })

  const graceMinutes = config?.delete_grace_minutes
  const deleteDesc = graceMinutes
    ? t(m.cleanup.deleteDesc, { minutes: graceMinutes })
    : m.cleanup.deleteDescNoGrace
  const coldDays = config?.cold_cleanup_days
  const subtitle = coldDays
    ? t(m.cleanup.subtitleEnabled, { days: coldDays, total })
    : t(m.cleanup.subtitleDisabled, { total })

  return (
    <Page
      title={m.cleanup.title}
      description={subtitle}
      actions={
        selectedIds.length > 0 && (
          <>
            <span className='text-sm text-muted-foreground'>
              {t(m.cleanup.selected, { count: selectedIds.length })}
            </span>
            <Button variant='ghost' onClick={() => setRowSelection({})}>
              {m.cleanup.clearSelection}
            </Button>
            <Button
              variant='outline'
              onClick={() => setDecision({ kind: 'keep', ids: selectedIds })}
            >
              {t(m.cleanup.keepSelected, { count: selectedIds.length })}
            </Button>
            <Button
              variant='destructive'
              onClick={() => setDecision({ kind: 'delete', ids: selectedIds })}
            >
              {t(m.cleanup.deleteSelected, { count: selectedIds.length })}
            </Button>
          </>
        )
      }
    >
      {coldDays === 0 && (
        <Alert className='border-info/40 bg-info/10'>
          <Info className='text-info' />
          <AlertDescription className='flex flex-wrap items-center gap-x-3 gap-y-1'>
            <span>{m.cleanup.disabledNotice}</span>
            <Link
              to='/settings'
              className='font-medium text-info underline-offset-4 hover:underline'
            >
              {m.cleanup.goSettings}
            </Link>
          </AlertDescription>
        </Alert>
      )}
      <QueryState query={query}>
        <DataTableView
          table={table}
          pagination={total > pagination.pageSize}
          empty={
            <div className='flex flex-col items-center gap-2'>
              <Trash2 className='size-8 text-muted-foreground/60' />
              <p className='font-medium text-foreground'>{m.cleanup.empty}</p>
              <p className='text-sm text-muted-foreground'>
                {m.cleanup.emptyHint}
              </p>
            </div>
          }
        />
      </QueryState>
      <ConfirmDialog
        open={decision !== null}
        onOpenChange={(open) => !open && setDecision(null)}
        title={decisionTitle(m.cleanup, decision)}
        desc={decision?.kind === 'delete' ? deleteDesc : m.cleanup.keepDesc}
        confirmText={
          decision?.kind === 'delete' ? m.cleanup.delete : m.cleanup.keep
        }
        destructive={decision?.kind === 'delete'}
        isLoading={keep.isPending || remove.isPending}
        handleConfirm={() => {
          if (!decision) return
          if (decision.kind === 'keep') keep.mutate(decision.ids)
          else remove.mutate(decision.ids)
        }}
      />
    </Page>
  )
}

function decisionTitle(
  m: Messages['cleanup'],
  decision: Decision | null
): string {
  if (!decision) return ''
  const count = decision.ids.length
  if (decision.kind === 'keep') {
    return count === 1 ? m.keepTitle : t(m.keepSelectedTitle, { count })
  }
  return count === 1 ? m.deleteTitle : t(m.deleteSelectedTitle, { count })
}
