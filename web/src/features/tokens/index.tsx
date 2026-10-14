import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { Plus } from 'lucide-react'
import { toast } from 'sonner'
import { useSession } from '@/lib/session'
import { useMessages } from '@/context/locale-provider'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { Page } from '@/components/layout/page'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableView } from '@/components/data-table'
import { QueryState } from '@/components/query-state'
import { type TokenAction, tokenColumns } from './columns'
import {
  deleteToken,
  revokeToken,
  rotateToken,
  type TokenItem,
  TOKENS_PAGE_SIZE,
  tokensQueryKey,
  tokensQueryOptions,
} from './data'
import { TokensEmptyState } from './empty-state'
import { IssueTokenDialog } from './issue-token-dialog'
import { IssuedTokenDialog, type IssuedResult } from './issued-token-dialog'
import { S3CredentialDialog } from './s3-credential-dialog'
import { UserFilter } from './user-filter'

const route = getRouteApi('/_authenticated/tokens/')

type Pending = { kind: Exclude<TokenAction, 's3'>; token: TokenItem }

export function Tokens() {
  const m = useMessages()
  const { user: me } = useSession()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const queryClient = useQueryClient()
  const isAdmin = me.role === 'admin'
  // 未选或选中本人 = 本人视角;看他人时隐藏签发、轮转与 S3 凭证(明文只下发给持有者)。
  const viewingUser = isAdmin && search.user !== me.id ? search.user : undefined
  const viewingSelf = viewingUser === undefined

  const [issuing, setIssuing] = useState(false)
  const [issued, setIssued] = useState<IssuedResult | null>(null)
  const [s3For, setS3For] = useState<TokenItem | null>(null)
  const [pending, setPending] = useState<Pending | null>(null)

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search,
      navigate,
      pagination: {
        pageSizeKey: 'page_size',
        defaultPageSize: TOKENS_PAGE_SIZE,
      },
      globalFilter: { enabled: false },
    })
  const query = useQuery(
    tokensQueryOptions({
      page: pagination.pageIndex + 1,
      page_size: pagination.pageSize,
      user_id: viewingUser,
    })
  )
  const total = query.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / pagination.pageSize))
  useEffect(() => {
    if (query.data) ensurePageInRange(pageCount)
  }, [query.data, pageCount, ensurePageInRange])

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: tokensQueryKey })
  const rotate = useMutation({
    mutationFn: (token: TokenItem) => rotateToken(token.id),
    onSuccess: (result) => {
      refresh()
      setPending(null)
      setIssued({ token: result, rotated: true })
    },
  })
  const revoke = useMutation({
    mutationFn: (token: TokenItem) => revokeToken(token.id),
    onSuccess: () => {
      refresh()
      setPending(null)
      toast.success(m.tokens.revoke.done)
    },
  })
  const remove = useMutation({
    mutationFn: (token: TokenItem) => deleteToken(token.id),
    onSuccess: () => {
      refresh()
      setPending(null)
      toast.success(m.tokens.delete.done)
    },
  })

  const columns = useMemo(
    () =>
      tokenColumns({
        m,
        viewingSelf,
        onAction: (kind, token) =>
          kind === 's3' ? setS3For(token) : setPending({ kind, token }),
      }),
    [m, viewingSelf]
  )
  const table = useReactTable({
    data: query.data?.items ?? [],
    columns,
    state: { pagination },
    manualPagination: true,
    pageCount,
    enableSorting: false,
    onPaginationChange,
    getCoreRowModel: getCoreRowModel(),
  })

  const confirmTexts = pending && m.tokens[pending.kind]
  const confirmPending =
    rotate.isPending || revoke.isPending || remove.isPending

  return (
    <Page
      title={m.tokens.title}
      description={m.tokens.description}
      actions={
        <>
          {isAdmin && (
            <UserFilter
              selfId={me.id}
              value={viewingUser ?? me.id}
              onChange={(userId) =>
                navigate({
                  search: (prev) => ({
                    ...prev,
                    user: userId === me.id ? undefined : userId,
                    page: undefined,
                  }),
                })
              }
            />
          )}
          {viewingSelf && (
            <Button onClick={() => setIssuing(true)}>
              <Plus />
              {m.tokens.issue}
            </Button>
          )}
        </>
      }
    >
      <QueryState query={query}>
        <DataTableView
          table={table}
          pagination={total > pagination.pageSize}
          empty={
            <TokensEmptyState
              viewingSelf={viewingSelf}
              onIssue={() => setIssuing(true)}
            />
          }
        />
      </QueryState>

      <IssueTokenDialog
        open={issuing}
        onOpenChange={setIssuing}
        onIssued={(token) => setIssued({ token, rotated: false })}
      />
      <IssuedTokenDialog issued={issued} onClose={() => setIssued(null)} />
      <S3CredentialDialog token={s3For} onClose={() => setS3For(null)} />
      {pending && confirmTexts && (
        <ConfirmDialog
          open
          onOpenChange={(open) => !open && !confirmPending && setPending(null)}
          title={confirmTexts.title}
          desc={
            <p>
              <span className='font-mono font-medium text-foreground'>
                {pending.token.name}
              </span>
              {' · '}
              {confirmTexts.desc}
            </p>
          }
          confirmText={confirmTexts.confirm}
          destructive={pending.kind !== 'rotate'}
          isLoading={confirmPending}
          handleConfirm={() => {
            if (pending.kind === 'rotate') rotate.mutate(pending.token)
            else if (pending.kind === 'revoke') revoke.mutate(pending.token)
            else remove.mutate(pending.token)
          }}
        />
      )}
    </Page>
  )
}
