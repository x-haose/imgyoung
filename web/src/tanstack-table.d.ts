import '@tanstack/react-table'

declare module '@tanstack/react-table' {
  interface ColumnMeta<TData, TValue> {
    title?: string // 列显隐菜单里的列名
    className?: string // 同时作用于 th 与 td
    tdClassName?: string
    thClassName?: string
  }
}
