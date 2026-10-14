import {
  flexRender,
  type Row,
  type Table as TableInstance,
} from '@tanstack/react-table'
import { cn } from '@/lib/utils'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { DataTablePagination } from './pagination'

type DataTableViewProps<T> = {
  table: TableInstance<T>
  empty: React.ReactNode // 没有行时整表显示的内容
  minWidthClassName?: string // 表格最小宽度;窄屏时容器横向滚动,不撑破布局
  cellClassName?: string // 追加到每个单元格,如 align-top
  rowClassName?: (row: Row<T>) => string | undefined // 按行追加样式,如高亮未处理的行
  pagination?: boolean
}

/** 表格渲染:卡片容器、列 meta 的样式钩子、选中行标记、空态行与分页条;列定义与数据由调用方的 table 实例决定。 */
export function DataTableView<T>({
  table,
  empty,
  minWidthClassName = 'min-w-2xl',
  cellClassName,
  rowClassName,
  pagination = true,
}: DataTableViewProps<T>) {
  const rows = table.getRowModel().rows
  return (
    <div className='flex flex-col gap-4'>
      <div className='overflow-hidden rounded-xl border bg-card shadow-sm'>
        <Table className={minWidthClassName}>
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow
                key={headerGroup.id}
                className='bg-muted/50 hover:bg-muted/50'
              >
                {headerGroup.headers.map((header) => (
                  <TableHead
                    key={header.id}
                    colSpan={header.colSpan}
                    className={cn(
                      'h-11 text-xs tracking-wide text-muted-foreground uppercase first:ps-4 last:pe-4',
                      header.column.columnDef.meta?.className,
                      header.column.columnDef.meta?.thClassName
                    )}
                  >
                    {header.isPlaceholder
                      ? null
                      : flexRender(
                          header.column.columnDef.header,
                          header.getContext()
                        )}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {rows.length === 0 ? (
              <TableRow className='hover:bg-transparent'>
                <TableCell
                  colSpan={table.getVisibleLeafColumns().length}
                  className='h-48 text-center whitespace-normal'
                >
                  {empty}
                </TableCell>
              </TableRow>
            ) : (
              rows.map((row) => (
                <TableRow
                  key={row.id}
                  data-state={row.getIsSelected() ? 'selected' : undefined}
                  className={rowClassName?.(row)}
                >
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      className={cn(
                        'py-3 first:ps-4 last:pe-4',
                        cellClassName,
                        cell.column.columnDef.meta?.className,
                        cell.column.columnDef.meta?.tdClassName
                      )}
                    >
                      {flexRender(
                        cell.column.columnDef.cell,
                        cell.getContext()
                      )}
                    </TableCell>
                  ))}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
      {pagination && <DataTablePagination table={table} />}
    </div>
  )
}
