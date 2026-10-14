import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import {
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { t } from '@/i18n'
import { Plus } from 'lucide-react'
import { toast } from 'sonner'
import { useSession } from '@/lib/session'
import { useMessages } from '@/context/locale-provider'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { Page } from '@/components/layout/page'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableToolbar, DataTableView } from '@/components/data-table'
import { QueryState } from '@/components/query-state'
import { userColumns } from './columns'
import {
  deleteUser,
  type UserItem,
  USERS_PAGE_SIZE,
  usersQueryKey,
  usersQueryOptions,
} from './data'
import { ResetPasswordDialog } from './reset-password-dialog'
import { UserFormDialog } from './user-form-dialog'

const route = getRouteApi('/_authenticated/users/')

export function Users() {
  const m = useMessages()
  const { user: me } = useSession()
  const queryClient = useQueryClient()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const [editing, setEditing] = useState<UserItem | 'new' | null>(null)
  const [resetting, setResetting] = useState<UserItem | null>(null)
  const [deleting, setDeleting] = useState<UserItem | null>(null)

  const query = useQuery(usersQueryOptions())
  const remove = useMutation({
    mutationFn: (user: UserItem) => deleteUser(user.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: usersQueryKey })
      setDeleting(null)
      toast.success(m.users.delete.done)
    },
  })

  const {
    globalFilter,
    onGlobalFilterChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search,
    navigate,
    pagination: {
      pageSizeKey: 'page_size',
      defaultPageSize: USERS_PAGE_SIZE,
    },
    globalFilter: { key: 'filter' },
  })

  const columns = useMemo(
    () =>
      userColumns({
        m,
        selfId: me.id,
        onAction: (action, user) => {
          if (action === 'edit') setEditing(user)
          else if (action === 'reset') setResetting(user)
          else setDeleting(user)
        },
      }),
    [m, me.id]
  )
  const table = useReactTable({
    data: query.data?.items ?? [],
    columns,
    state: { globalFilter, pagination },
    enableSorting: false,
    // 页码归地址栏管:筛选变化由 onGlobalFilterChange 回到第一页,数据重取不翻页。
    autoResetPageIndex: false,
    globalFilterFn: (row, _columnId, value) => {
      const needle = String(value).toLowerCase()
      return (
        row.original.name.toLowerCase().includes(needle) ||
        row.original.id.toLowerCase().includes(needle)
      )
    },
    onGlobalFilterChange,
    onPaginationChange,
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
  })
  const pageCount = table.getPageCount()
  useEffect(() => {
    if (query.data) ensurePageInRange(pageCount)
  }, [query.data, pageCount, ensurePageInRange])

  return (
    <Page
      title={m.users.title}
      description={m.users.description}
      actions={
        <Button onClick={() => setEditing('new')}>
          <Plus />
          {m.users.create}
        </Button>
      }
    >
      <QueryState query={query}>
        <div className='flex flex-col gap-4'>
          <DataTableToolbar
            table={table}
            searchPlaceholder={m.users.searchPlaceholder}
          />
          <DataTableView
            table={table}
            pagination={
              table.getFilteredRowModel().rows.length > pagination.pageSize
            }
            empty={
              <span className='text-muted-foreground'>
                {m.users.emptyFiltered}
              </span>
            }
          />
        </div>
      </QueryState>

      <UserFormDialog target={editing} onClose={() => setEditing(null)} />
      <ResetPasswordDialog
        user={resetting}
        onClose={() => setResetting(null)}
      />
      {deleting && (
        <ConfirmDialog
          open
          onOpenChange={(open) =>
            !open && !remove.isPending && setDeleting(null)
          }
          title={t(m.users.delete.title, { name: deleting.name })}
          desc={m.users.delete.desc}
          confirmText={m.users.delete.confirm}
          destructive
          isLoading={remove.isPending}
          handleConfirm={() => remove.mutate(deleting)}
        />
      )}
    </Page>
  )
}
