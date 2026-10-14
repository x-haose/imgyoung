import { type ColumnDef } from '@tanstack/react-table'
import { type Messages } from '@/i18n'
import { formatRelativeTime, formatTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { DataTableColumnHeader } from '@/components/data-table'
import { type TokenItem, tokenStatus } from './data'
import { RowActions, type TokenAction } from './row-actions'
import { TokenStatusBadge } from './status-badge'

export type { TokenAction }

type ColumnOptions = {
  m: Messages
  viewingSelf: boolean // 当前列表是否本人的 Token;看别人时不下发明文类动作
  onAction: (action: TokenAction, token: TokenItem) => void
}

export function tokenColumns({
  m,
  viewingSelf,
  onAction,
}: ColumnOptions): ColumnDef<TokenItem>[] {
  return [
    {
      accessorKey: 'name',
      meta: { title: m.tokens.columns.name },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={m.tokens.columns.name} />
      ),
      cell: ({ row }) => (
        <span
          className={cn(
            'font-medium',
            tokenStatus(row.original) !== 'active' && 'text-muted-foreground'
          )}
        >
          {row.original.name}
        </span>
      ),
    },
    {
      accessorKey: 'prefix',
      meta: { title: m.tokens.columns.prefix },
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={m.tokens.columns.prefix}
        />
      ),
      cell: ({ row }) => (
        <code className='rounded-md bg-muted px-1.5 py-0.5 font-mono text-xs'>
          {row.original.prefix}…
        </code>
      ),
    },
    {
      id: 'status',
      accessorFn: (row) => tokenStatus(row),
      meta: { title: m.tokens.columns.status },
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={m.tokens.columns.status}
        />
      ),
      cell: ({ row }) => (
        <TokenStatusBadge status={tokenStatus(row.original)} />
      ),
    },
    {
      accessorKey: 'expires_at',
      meta: { title: m.tokens.columns.expires },
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={m.tokens.columns.expires}
        />
      ),
      cell: ({ row }) => (
        <span className='text-muted-foreground'>
          {row.original.expires_at
            ? formatTime(row.original.expires_at)
            : m.tokens.never}
        </span>
      ),
    },
    {
      accessorKey: 'last_used_at',
      meta: { title: m.tokens.columns.lastUsed },
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={m.tokens.columns.lastUsed}
        />
      ),
      cell: ({ row }) =>
        row.original.last_used_at ? (
          <span title={formatTime(row.original.last_used_at)}>
            {formatRelativeTime(row.original.last_used_at)}
          </span>
        ) : (
          <span className='text-muted-foreground'>{m.tokens.neverUsed}</span>
        ),
    },
    {
      accessorKey: 'created_at',
      meta: { title: m.tokens.columns.created },
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={m.tokens.columns.created}
        />
      ),
      cell: ({ row }) => (
        <span className='text-muted-foreground'>
          {formatTime(row.original.created_at)}
        </span>
      ),
    },
    {
      id: 'actions',
      enableHiding: false,
      meta: { className: 'w-12 text-end' },
      cell: ({ row }) => (
        <RowActions
          m={m}
          token={row.original}
          viewingSelf={viewingSelf}
          onAction={onAction}
        />
      ),
    },
  ]
}
