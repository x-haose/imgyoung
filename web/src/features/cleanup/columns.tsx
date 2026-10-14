import { type ColumnDef } from '@tanstack/react-table'
import { type Messages, t } from '@/i18n'
import {
  adminThumbSrc,
  formatBytes,
  formatRelativeTime,
  formatTime,
} from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { type CleanupItem } from './data'

type Handlers = {
  onKeep: (item: CleanupItem) => void
  onDelete: (item: CleanupItem) => void
}

/** 待清理表的列;文案与缩略图档位随语言和服务端配置变化,由调用方用 useMemo 缓存。 */
export function cleanupColumns(
  m: Messages['cleanup'],
  widths: number[] | null | undefined,
  { onKeep, onDelete }: Handlers
): ColumnDef<CleanupItem>[] {
  return [
    {
      id: 'select',
      header: ({ table }) => (
        <Checkbox
          checked={
            table.getIsAllPageRowsSelected() ||
            (table.getIsSomePageRowsSelected() && 'indeterminate')
          }
          onCheckedChange={(v) => table.toggleAllPageRowsSelected(v === true)}
          aria-label={m.selectAllPage}
        />
      ),
      cell: ({ row }) => (
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(v) => row.toggleSelected(v === true)}
          aria-label={t(m.selectRow, {
            name: row.original.file_name || row.original.id,
          })}
        />
      ),
      meta: { className: 'w-10 ps-4' },
    },
    {
      id: 'preview',
      header: m.preview,
      cell: ({ row }) => (
        // 大图也走管理端不计数通道:冷图正因「无访问」才在此,走 /i/ 会把它当公开访问摘出队列。
        <a
          href={`/api/admin/images/${row.original.id}/thumb`}
          target='_blank'
          rel='noreferrer'
          className='block size-14 overflow-hidden rounded-md bg-muted'
        >
          <img
            src={adminThumbSrc(row.original.id, widths, 100)}
            alt={row.original.file_name || row.original.id}
            loading='lazy'
            className='size-full object-cover'
          />
        </a>
      ),
      meta: { className: 'w-20' },
    },
    {
      id: 'image',
      header: m.image,
      cell: ({ row }) => (
        <div className='min-w-0'>
          <div className='truncate font-medium'>
            {row.original.file_name || row.original.id}
          </div>
          <div className='truncate text-xs text-muted-foreground'>
            {t(m.meta, {
              mime: row.original.mime,
              width: row.original.width,
              height: row.original.height,
              size: formatBytes(row.original.size),
            })}
          </div>
        </div>
      ),
      meta: { className: 'min-w-56' },
    },
    {
      accessorKey: 'user_id',
      header: m.owner,
      cell: ({ getValue }) => (
        <span className='font-mono text-xs'>{getValue<string>()}</span>
      ),
      meta: { className: 'text-nowrap' },
    },
    {
      accessorKey: 'last_access_at',
      header: m.lastAccess,
      cell: ({ getValue }) => {
        const v = getValue<string | null>()
        if (!v)
          return (
            <span className='text-muted-foreground'>{m.neverAccessed}</span>
          )
        return (
          <div className='text-nowrap'>
            <div>{formatTime(v)}</div>
            <div className='text-xs text-muted-foreground'>
              {formatRelativeTime(v)}
            </div>
          </div>
        )
      },
    },
    {
      accessorKey: 'created_at',
      header: m.uploadedAt,
      cell: ({ getValue }) => (
        <span className='text-nowrap text-muted-foreground'>
          {formatTime(getValue<string>())}
        </span>
      ),
    },
    {
      id: 'decision',
      header: m.decision,
      cell: ({ row }) => (
        <div className='flex justify-end gap-1'>
          <Button
            variant='outline'
            size='sm'
            onClick={() => onKeep(row.original)}
          >
            {m.keep}
          </Button>
          <Button
            variant='ghost'
            size='sm'
            className='text-destructive hover:bg-destructive/10 hover:text-destructive'
            onClick={() => onDelete(row.original)}
          >
            {m.delete}
          </Button>
        </div>
      ),
      meta: { className: 'w-40 pe-4 text-end' },
    },
  ]
}
