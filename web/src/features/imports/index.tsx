import { useEffect, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { t } from '@/i18n'
import { PackageOpen, Plus } from 'lucide-react'
import { useSession } from '@/lib/session'
import { useMessages } from '@/context/locale-provider'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { Page } from '@/components/layout/page'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { DataTableView } from '@/components/data-table'
import { QueryState } from '@/components/query-state'
import { userOptionsQueryOptions } from '@/features/images/data'
import { importColumns } from './columns'
import { CreateImportDialog } from './create-dialog'
import { IMPORTS_PAGE_SIZE, importsQueryOptions } from './data'

const route = getRouteApi('/_authenticated/imports/')

/** Radix Select 不接受空串作为选项值,「全部用户」用哨兵值。 */
const ALL = '*'

/** 导入页:当前用户的导入任务列表与新建入口;有进行中的任务时自动刷新进度。 */
export function Imports() {
  const m = useMessages()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const isAdmin = useSession().user.role === 'admin'
  const users = useQuery({ ...userOptionsQueryOptions(), enabled: isAdmin })

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search,
      navigate,
      pagination: {
        pageKey: 'page',
        pageSizeKey: 'page_size',
        defaultPage: 1,
        defaultPageSize: IMPORTS_PAGE_SIZE,
      },
      globalFilter: { enabled: false },
    })
  const query = useQuery(
    importsQueryOptions({
      page: pagination.pageIndex + 1,
      page_size: pagination.pageSize,
      user_id: isAdmin ? search.user : undefined,
    })
  )
  const total = query.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / pagination.pageSize))
  useEffect(() => {
    if (query.data) ensurePageInRange(pageCount)
  }, [query.data, pageCount, ensurePageInRange])

  const columns = useMemo(
    () => importColumns(m.imports, isAdmin),
    [m.imports, isAdmin]
  )
  const table = useReactTable({
    data: query.data?.items ?? [],
    columns,
    getRowId: (row) => row.id,
    state: { pagination },
    manualPagination: true,
    pageCount,
    onPaginationChange,
    getCoreRowModel: getCoreRowModel(),
  })

  const setCreating = (creating: boolean) =>
    navigate({
      replace: true,
      search: (prev) => ({ ...prev, new: creating ? true : undefined }),
    })

  return (
    <Page
      title={m.imports.title}
      description={
        <>
          {m.imports.subtitle}
          {query.data && ` · ${t(m.imports.total, { total })}`}
        </>
      }
      actions={
        <>
          {isAdmin && (
            <Select
              value={search.user ?? ALL}
              onValueChange={(v) =>
                navigate({
                  search: (prev) => ({
                    ...prev,
                    user: v === ALL ? undefined : v,
                    page: undefined,
                  }),
                })
              }
            >
              <SelectTrigger className='w-44' aria-label={m.imports.filterUser}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>{m.imports.allUsers}</SelectItem>
                {(users.data?.items ?? []).map((u) => (
                  <SelectItem key={u.id} value={u.id}>
                    {u.name || u.id}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          <Button onClick={() => setCreating(true)}>
            <Plus />
            {m.imports.create}
          </Button>
        </>
      }
    >
      <QueryState query={query}>
        <DataTableView
          table={table}
          pagination={total > pagination.pageSize}
          minWidthClassName='min-w-3xl'
          empty={<EmptyState onCreate={() => setCreating(true)} />}
        />
      </QueryState>
      <CreateImportDialog
        open={search.new === true}
        onOpenChange={setCreating}
      />
    </Page>
  )
}

/** 空态:引导新建第一个导入。 */
function EmptyState({ onCreate }: { onCreate: () => void }) {
  const m = useMessages()
  return (
    <div className='flex flex-col items-center gap-4 py-6'>
      <div className='flex size-12 items-center justify-center rounded-full bg-primary/10 text-primary'>
        <PackageOpen className='size-6' />
      </div>
      <div className='space-y-1'>
        <p className='font-medium text-foreground'>{m.imports.emptyTitle}</p>
        <p className='text-sm text-muted-foreground'>{m.imports.emptyDesc}</p>
      </div>
      <Button onClick={onCreate}>
        <Plus />
        {m.imports.emptyAction}
      </Button>
    </div>
  )
}
