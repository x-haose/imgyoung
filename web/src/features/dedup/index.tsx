import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { t } from '@/i18n'
import { CopyCheck } from 'lucide-react'
import { toast } from 'sonner'
import { useMessages } from '@/context/locale-provider'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { Page } from '@/components/layout/page'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTablePagination } from '@/components/data-table'
import { QueryState } from '@/components/query-state'
import { CandidateCard } from './candidate-card'
import {
  affectedByDecision,
  type CandidateSide,
  candidatesQueryOptions,
  DEDUP_PAGE_SIZE,
  dismissCandidate,
  mergeCandidate,
} from './data'

const route = getRouteApi('/_authenticated/dedup/')

export function Dedup() {
  const m = useMessages()
  const queryClient = useQueryClient()
  const [selected, setSelected] = useState<ReadonlySet<number>>(new Set())
  const [busyId, setBusyId] = useState<number | null>(null)
  const [confirmBatch, setConfirmBatch] = useState(false)

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search: route.useSearch(),
      navigate: route.useNavigate(),
      pagination: {
        pageKey: 'page',
        pageSizeKey: 'page_size',
        defaultPage: 1,
        defaultPageSize: DEDUP_PAGE_SIZE,
      },
      globalFilter: { enabled: false },
    })
  const query = useQuery(
    candidatesQueryOptions(pagination.pageIndex + 1, pagination.pageSize)
  )
  const total = query.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / pagination.pageSize))
  const items = query.data?.items ?? []

  useEffect(() => {
    if (query.data) ensurePageInRange(pageCount)
  }, [query.data, pageCount, ensurePageInRange])

  // 只借 TanStack Table 管分页状态,卡片网格自己渲染。
  const table = useReactTable({
    data: items,
    columns: [],
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    pageCount,
    state: { pagination },
    onPaginationChange: (updater) => {
      setSelected(new Set())
      onPaginationChange(updater)
    },
  })

  // 裁决失败(候选已被他人处理、图片已删)时列表同样过期,成败都重取。
  const invalidateAll = () => {
    for (const key of affectedByDecision) {
      void queryClient.invalidateQueries({ queryKey: key })
    }
  }
  const settle = () => {
    setSelected(new Set())
    invalidateAll()
  }

  const merge = useMutation({
    mutationFn: ({ id, keep }: { id: number; keep: CandidateSide }) =>
      mergeCandidate(id, keep.blob_id),
    onMutate: ({ id }) => setBusyId(id),
    onSettled: () => setBusyId(null),
    onSuccess: () => {
      toast.success(m.dedup.merged)
      settle()
    },
  })
  const dismiss = useMutation({
    mutationFn: (id: number) => dismissCandidate(id),
    onMutate: (id) => setBusyId(id),
    onSettled: () => setBusyId(null),
    onSuccess: () => {
      toast.success(m.dedup.dismissed)
      settle()
    },
  })
  // 忽略是单条端点:顺序执行,失败计数后继续,结果一次汇报。
  const batchDismiss = useMutation({
    mutationFn: async (ids: number[]) => {
      let done = 0
      let failed = 0
      for (const id of ids) {
        try {
          await dismissCandidate(id)
          done++
        } catch {
          failed++
        }
      }
      return { done, failed }
    },
    onSuccess: ({ done, failed }) => {
      if (failed > 0) {
        toast.warning(t(m.dedup.batchDismissPartial, { done, failed }))
      } else {
        toast.success(t(m.dedup.batchDismissDone, { done }))
      }
      setConfirmBatch(false)
      settle()
    },
  })

  /** 把 mutate 包成只在结束时兑现、从不拒绝的 Promise;失败已由全局提示展示,卡片只需知道何时收尾。 */
  const whenSettled = (run: (done: () => void) => void) =>
    new Promise<void>((resolve) => run(resolve))

  const toggle = (id: number, on: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (on) next.add(id)
      else next.delete(id)
      return next
    })

  return (
    <Page
      title={m.dedup.title}
      description={t(m.dedup.subtitle, { total })}
      actions={
        selected.size > 0 && (
          <>
            <Button variant='ghost' onClick={() => setSelected(new Set())}>
              {m.dedup.clearSelection}
            </Button>
            <Button variant='outline' onClick={() => setConfirmBatch(true)}>
              {t(m.dedup.dismissSelected, { count: selected.size })}
            </Button>
          </>
        )
      }
    >
      <QueryState query={query} skeleton={<GridSkeleton />}>
        {items.length === 0 ? (
          <EmptyState />
        ) : (
          <div className='grid grid-cols-1 gap-4 xl:grid-cols-2'>
            {items.map((candidate) => (
              <CandidateCard
                key={candidate.id}
                candidate={candidate}
                selected={selected.has(candidate.id)}
                onSelectedChange={(on) => toggle(candidate.id, on)}
                busy={busyId === candidate.id}
                onMerge={(keep) =>
                  whenSettled((done) =>
                    merge.mutate(
                      { id: candidate.id, keep },
                      { onSettled: done, onError: invalidateAll }
                    )
                  )
                }
                onDismiss={() =>
                  whenSettled((done) =>
                    dismiss.mutate(candidate.id, {
                      onSettled: done,
                      onError: invalidateAll,
                    })
                  )
                }
              />
            ))}
          </div>
        )}
        {total > pagination.pageSize && <DataTablePagination table={table} />}
      </QueryState>
      <ConfirmDialog
        open={confirmBatch}
        onOpenChange={setConfirmBatch}
        title={t(m.dedup.dismissSelectedTitle, { count: selected.size })}
        desc={m.dedup.dismissSelectedDesc}
        confirmText={m.dedup.dismiss}
        isLoading={batchDismiss.isPending}
        handleConfirm={() => batchDismiss.mutate([...selected])}
      />
    </Page>
  )
}

function EmptyState() {
  const m = useMessages()
  return (
    <Card>
      <CardContent className='flex flex-col items-center gap-2 py-16 text-center'>
        <CopyCheck className='size-8 text-muted-foreground/60' />
        <p className='font-medium'>{m.dedup.empty}</p>
        <p className='text-sm text-muted-foreground'>{m.dedup.emptyHint}</p>
      </CardContent>
    </Card>
  )
}

function GridSkeleton() {
  return (
    <div className='grid grid-cols-1 gap-4 xl:grid-cols-2'>
      {Array.from({ length: 4 }, (_, i) => (
        <Skeleton key={i} className='h-72 w-full rounded-xl' />
      ))}
    </div>
  )
}
