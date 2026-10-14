import { useEffect, useMemo, useState } from 'react'
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { getRouteApi, Link } from '@tanstack/react-router'
import { t } from '@/i18n'
import {
  BookOpen,
  CheckSquare,
  ImageOff,
  Images as ImagesIcon,
  Search,
  Trash2,
  Upload,
  X,
} from 'lucide-react'
import { toast } from 'sonner'
import { useSession } from '@/lib/session'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { useServerConfig } from '@/hooks/use-server-config'
import { Page } from '@/components/layout/page'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { QueryState } from '@/components/query-state'
import {
  BATCH_DELETE_MAX,
  batchDeleteImages,
  deleteWarning,
  imagesQueryKey,
  imagesQueryOptions,
  toListParams,
} from './data'
import { ImageDetailSheet } from './detail-sheet'
import { ActiveFilterChips, type FilterPatch, ImageFilters } from './filters'
import { GridPagination } from './grid-pagination'
import { ImageCard } from './image-card'
import { hasFilters, PAGE_SIZE } from './search'
import { UploadDialog } from './upload-dialog'

const route = getRouteApi('/_authenticated/images/')

const gridClass =
  'grid grid-cols-2 gap-4 @xl/content:grid-cols-3 @3xl/content:grid-cols-4 @6xl/content:grid-cols-6'

/** 图片页:缩略卡片网格 + 搜索 / 多维筛选 + 多选批量删除 + 详情抽屉 + 上传对话框。 */
export function Images() {
  const m = useMessages()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const { user } = useSession()
  const isAdmin = user.role === 'admin'
  const config = useServerConfig()
  const queryClient = useQueryClient()

  const params = useMemo(() => toListParams(search), [search])
  const list = useQuery({
    ...imagesQueryOptions(params),
    placeholderData: keepPreviousData,
  })
  const items = list.data?.items ?? []
  const total = list.data?.total
  const pageCount =
    total === undefined ? undefined : Math.max(1, Math.ceil(total / PAGE_SIZE))

  // 删空末页或手改地址越界时回到最后一页。
  useEffect(() => {
    if (pageCount !== undefined && search.page > pageCount) {
      navigate({
        replace: true,
        search: (prev) => ({
          ...prev,
          page: pageCount === 1 ? undefined : pageCount,
        }),
      })
    }
  }, [pageCount, search.page, navigate])

  // 输入框是地址栏 q 的草稿:地址栏变了(后退、清除筛选)草稿跟着重置。
  const [keyword, setKeyword] = useState(search.q ?? '')
  const [syncedQ, setSyncedQ] = useState(search.q)
  if (search.q !== syncedQ) {
    setSyncedQ(search.q)
    setKeyword(search.q ?? '')
  }
  const applyFilter = (patch: FilterPatch & { q?: string }) =>
    navigate({ search: (prev) => ({ ...prev, ...patch, page: undefined }) })
  const setPage = (page: number) =>
    navigate({
      search: (prev) => ({ ...prev, page: page === 1 ? undefined : page }),
    })
  const openDetail = (id: string) =>
    navigate({ search: (prev) => ({ ...prev, view: id }) })
  const closeDetail = () =>
    navigate({ search: (prev) => ({ ...prev, view: undefined }) })

  const [selecting, setSelecting] = useState(false)
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const [confirmBatch, setConfirmBatch] = useState(false)
  const [uploadOpen, setUploadOpen] = useState(false)
  // 翻页或改筛选后,勾选的卡片已不在眼前,清空避免误删看不见的图。
  const listKey = JSON.stringify(params)
  const [syncedListKey, setSyncedListKey] = useState(listKey)
  if (listKey !== syncedListKey) {
    setSyncedListKey(listKey)
    setSelected(new Set())
  }

  const exitSelecting = () => {
    setSelecting(false)
    setSelected(new Set())
  }
  const toggleSelect = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else if (next.size >= BATCH_DELETE_MAX) {
        toast.warning(t(m.images.maxSelect, { max: BATCH_DELETE_MAX }))
        return prev
      } else {
        next.add(id)
      }
      return next
    })
  const selectPage = () =>
    setSelected(new Set(items.slice(0, BATCH_DELETE_MAX).map((i) => i.id)))

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: imagesQueryKey })
  const batch = useMutation({
    mutationFn: () => batchDeleteImages([...selected]),
    onSuccess: (res) => {
      if (res.failed.length > 0) {
        toast.warning(
          t(m.images.batchPartial, {
            deleted: res.deleted,
            failed: res.failed.length,
            error: res.failed[0].error,
          })
        )
      } else {
        toast.success(t(m.images.batchDeleted, { count: res.deleted }))
      }
      setConfirmBatch(false)
      exitSelecting()
      refresh()
    },
  })

  const filtered = hasFilters(search)

  return (
    <Page
      title={m.images.title}
      description={
        <>
          {isAdmin ? m.images.scopeAll : m.images.scopeMine}
          {total !== undefined && ` · ${t(m.images.total, { total })}`}
        </>
      }
      actions={
        <div className='flex flex-wrap items-center gap-2'>
          <form
            className='relative min-w-48 flex-1'
            onSubmit={(e) => {
              e.preventDefault()
              applyFilter({ q: keyword.trim() || undefined })
            }}
          >
            <Search className='pointer-events-none absolute start-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground' />
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              placeholder={m.images.searchPlaceholder}
              className='w-full ps-8 sm:w-56'
              aria-label={m.common.search}
            />
          </form>
          <ImageFilters
            isAdmin={isAdmin}
            search={search}
            onChange={applyFilter}
          />
          <Button
            variant={selecting ? 'secondary' : 'outline'}
            onClick={() => (selecting ? exitSelecting() : setSelecting(true))}
          >
            {selecting ? <X /> : <CheckSquare />}
            {selecting ? m.images.cancelSelect : m.images.select}
          </Button>
          <Button onClick={() => setUploadOpen(true)}>
            <Upload />
            {m.images.upload}
          </Button>
        </div>
      }
    >
      <ActiveFilterChips
        isAdmin={isAdmin}
        search={search}
        onChange={applyFilter}
      />

      <QueryState query={list} skeleton={<GridSkeleton />}>
        {items.length === 0 ? (
          <EmptyState
            filtered={filtered}
            onUpload={() => setUploadOpen(true)}
            onClear={() =>
              applyFilter({
                q: undefined,
                user: undefined,
                token: undefined,
                since: undefined,
                until: undefined,
                exempt: undefined,
              })
            }
          />
        ) : (
          <div
            className={cn(
              gridClass,
              list.isPlaceholderData && 'opacity-60 transition-opacity'
            )}
          >
            {items.map((img) => (
              <ImageCard
                key={img.id}
                image={img}
                selecting={selecting}
                selected={selected.has(img.id)}
                onOpen={openDetail}
                onToggle={toggleSelect}
              />
            ))}
          </div>
        )}
        {pageCount !== undefined && (
          <GridPagination
            page={search.page}
            pageCount={pageCount}
            onChange={setPage}
          />
        )}
      </QueryState>

      {selecting && (
        <div className='fixed bottom-6 left-1/2 z-20 flex max-w-[calc(100vw-2rem)] -translate-x-1/2 flex-wrap items-center justify-center gap-2 rounded-xl border bg-popover px-4 py-2.5 text-popover-foreground shadow-lg'>
          <span className='text-sm font-medium tabular-nums'>
            {t(m.images.selectedCount, { count: selected.size })}
          </span>
          <Button variant='ghost' size='sm' onClick={selectPage}>
            {m.images.selectPage}
          </Button>
          <Button
            variant='destructive'
            size='sm'
            disabled={selected.size === 0}
            onClick={() => setConfirmBatch(true)}
          >
            <Trash2 />
            {m.images.batchDelete}
          </Button>
          <Button variant='outline' size='sm' onClick={exitSelecting}>
            {m.common.cancel}
          </Button>
        </div>
      )}

      <ConfirmDialog
        open={confirmBatch}
        onOpenChange={setConfirmBatch}
        title={t(m.images.batchDeleteTitle, { count: selected.size })}
        desc={deleteWarning(m.images, config?.delete_grace_minutes)}
        confirmText={m.images.batchDelete}
        destructive
        isLoading={batch.isPending}
        handleConfirm={() => batch.mutate()}
      />

      <ImageDetailSheet
        id={search.view}
        isAdmin={isAdmin}
        onClose={closeDetail}
      />
      <UploadDialog
        open={uploadOpen}
        onOpenChange={setUploadOpen}
        onUploaded={refresh}
      />
    </Page>
  )
}

function GridSkeleton() {
  return (
    <div className={gridClass}>
      {Array.from({ length: 12 }, (_, i) => (
        <div key={i} className='overflow-hidden rounded-xl border bg-card'>
          <Skeleton className='aspect-[4/3] rounded-none' />
          <div className='space-y-2 px-3 py-2.5'>
            <Skeleton className='h-4 w-3/4' />
            <Skeleton className='h-3 w-1/3' />
          </div>
        </div>
      ))}
    </div>
  )
}

/** 空态:没有任何图时引导上传或看指南;筛选无匹配时引导清除筛选。 */
function EmptyState({
  filtered,
  onUpload,
  onClear,
}: {
  filtered: boolean
  onUpload: () => void
  onClear: () => void
}) {
  const m = useMessages()
  return (
    <Card>
      <CardContent className='flex flex-col items-center gap-4 py-14 text-center'>
        <span className='flex size-14 items-center justify-center rounded-full bg-muted text-muted-foreground'>
          {filtered ? (
            <ImageOff className='size-7' />
          ) : (
            <ImagesIcon className='size-7' />
          )}
        </span>
        <div className='space-y-1'>
          <p className='font-medium'>
            {filtered ? m.images.noMatchTitle : m.images.emptyTitle}
          </p>
          <p className='text-sm text-muted-foreground'>
            {filtered ? m.images.noMatchDesc : m.images.emptyDesc}
          </p>
        </div>
        {filtered ? (
          <Button variant='outline' onClick={onClear}>
            {m.images.clearFilters}
          </Button>
        ) : (
          <div className='flex flex-wrap justify-center gap-2'>
            <Button onClick={onUpload}>
              <Upload />
              {m.images.uploadFirst}
            </Button>
            <Button variant='outline' asChild>
              <Link to='/guide'>
                <BookOpen />
                {m.images.viewGuide}
              </Link>
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
