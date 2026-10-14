import { type ColumnDef } from '@tanstack/react-table'
import { type Messages, t } from '@/i18n'
import { formatNumber, formatTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { DataTableColumnHeader } from '@/components/data-table'
import { type UserItem } from './data'
import { RowActions, type UserAction } from './row-actions'
import { UsageBar } from './usage-bar'

export type { UserAction }

type ColumnOptions = {
  m: Messages
  selfId: string // 当前登录用户:标「当前」且不可删自己
  onAction: (action: UserAction, user: UserItem) => void
}

export function userColumns({
  m,
  selfId,
  onAction,
}: ColumnOptions): ColumnDef<UserItem>[] {
  return [
    {
      accessorKey: 'name',
      meta: { title: m.users.columns.user },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={m.users.columns.user} />
      ),
      cell: ({ row }) => {
        const u = row.original
        return (
          <div className='flex items-center gap-2'>
            <span
              className={cn(
                'font-medium',
                u.disabled && 'text-muted-foreground line-through'
              )}
            >
              {u.name}
            </span>
            {u.role === 'admin' && (
              <Badge
                variant='outline'
                className='border-primary/30 bg-primary/10 text-primary'
              >
                {m.users.badges.admin}
              </Badge>
            )}
            {u.disabled && (
              <Badge variant='outline' className='text-muted-foreground'>
                {m.users.badges.disabled}
              </Badge>
            )}
            {u.id === selfId && (
              <span className='text-xs text-muted-foreground'>
                {m.users.badges.self}
              </span>
            )}
          </div>
        )
      },
    },
    {
      accessorKey: 'id',
      meta: { title: m.users.columns.id },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={m.users.columns.id} />
      ),
      cell: ({ row }) => (
        <code className='font-mono text-xs text-muted-foreground select-all'>
          {row.original.id}
        </code>
      ),
    },
    {
      accessorKey: 'image_count',
      meta: { title: m.users.columns.images, className: 'text-end' },
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={m.users.columns.images}
          className='justify-end'
        />
      ),
      cell: ({ row }) => (
        <span className='tabular-nums'>
          {formatNumber(row.original.image_count)}
        </span>
      ),
    },
    {
      id: 'usage',
      accessorFn: (row) => row.used_bytes,
      meta: { title: m.users.columns.usage },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={m.users.columns.usage} />
      ),
      cell: ({ row }) => (
        <UsageBar
          used={row.original.used_bytes}
          quota={row.original.quota_bytes}
        />
      ),
    },
    {
      accessorKey: 'default_ttl_hours',
      meta: { title: m.users.columns.ttl },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={m.users.columns.ttl} />
      ),
      cell: ({ row }) => {
        const hours = row.original.default_ttl_hours
        if (hours === null) {
          return (
            <span className='text-muted-foreground'>{m.users.ttlGlobal}</span>
          )
        }
        return hours === 0
          ? m.users.ttlForever
          : t(m.users.ttlHours, { hours: formatNumber(hours) })
      },
    },
    {
      accessorKey: 'created_at',
      meta: { title: m.users.columns.created },
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={m.users.columns.created}
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
          user={row.original}
          isSelf={row.original.id === selfId}
          onAction={onAction}
        />
      ),
    },
  ]
}
