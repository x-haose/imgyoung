import { Link } from '@tanstack/react-router'
import { type ColumnDef } from '@tanstack/react-table'
import { type Messages } from '@/i18n'
import { formatRelativeTime, formatTime } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { type ImportJob } from './data'
import { JobProgress } from './job-progress'
import { ImportStatusBadge } from './status-badge'

/** 任务列表的列;showOwner 为真时多一列创建者(管理员看得到全部用户的任务);文案随语言变化,由调用方用 useMemo 缓存。 */
export function importColumns(
  m: Messages['imports'],
  showOwner: boolean
): ColumnDef<ImportJob>[] {
  const owner: ColumnDef<ImportJob>[] = showOwner
    ? [
        {
          id: 'owner',
          header: m.columns.owner,
          cell: ({ row }) => row.original.user_name,
        },
      ]
    : []
  return [
    {
      id: 'created',
      header: m.columns.created,
      cell: ({ row }) => (
        <div className='min-w-0'>
          <Link
            to='/imports/$id'
            params={{ id: row.original.id }}
            className='font-medium underline-offset-4 hover:underline'
          >
            {formatTime(row.original.created_at)}
          </Link>
          <div className='text-xs text-muted-foreground'>
            {formatRelativeTime(row.original.created_at)}
          </div>
        </div>
      ),
    },
    ...owner,
    {
      id: 'mode',
      header: m.columns.mode,
      cell: ({ row }) => m.mode[row.original.mode],
    },
    {
      id: 'status',
      header: m.columns.status,
      cell: ({ row }) => <ImportStatusBadge status={row.original.status} />,
    },
    {
      id: 'progress',
      header: m.columns.progress,
      cell: ({ row }) => <JobProgress job={row.original} m={m} />,
      meta: { className: 'w-64' },
    },
    {
      id: 'actions',
      header: () => <span className='sr-only'>{m.view}</span>,
      cell: ({ row }) => (
        <div className='flex justify-end'>
          <Button variant='ghost' size='sm' asChild>
            <Link to='/imports/$id' params={{ id: row.original.id }}>
              {m.view}
            </Link>
          </Button>
        </div>
      ),
      meta: { className: 'w-24' },
    },
  ]
}
