import { memo, ReactNode } from 'react'

interface TableRowProps {
  children: ReactNode
  className?: string
  onClick?: () => void
}

/**
 * MemoizedTableRow — 纯展示表格行，避免父组件重渲染时整表刷新。
 */
export const MemoizedTableRow = memo(function TableRow({
  children,
  className = '',
  onClick,
}: TableRowProps) {
  return (
    <tr
      className={`border-b border-quant-border/50 hover:bg-quant-card/50 transition-colors ${className}`}
      onClick={onClick}
    >
      {children}
    </tr>
  )
})

interface DataTableProps<T> {
  data: T[]
  columns: {
    key: string
    title: string
    width?: string
    render: (item: T, index: number) => ReactNode
  }[]
  keyExtractor: (item: T, index: number) => string
  emptyText?: string
  className?: string
}

/**
 * DataTable — 带 memo 优化的数据表格。
 */
export function DataTable<T>({
  data,
  columns,
  keyExtractor,
  emptyText = '暂无数据',
  className = '',
}: DataTableProps<T>) {
  if (data.length === 0) {
    return (
      <div className={`text-center py-8 text-quant-text-secondary text-sm ${className}`}>
        {emptyText}
      </div>
    )
  }

  return (
    <div className={`overflow-x-auto ${className}`}>
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-quant-border">
            {columns.map((col) => (
              <th
                key={col.key}
                scope="col"
                className="text-left py-2 px-3 text-quant-text-secondary font-medium whitespace-nowrap"
                style={{ width: col.width }}
              >
                {col.title}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.map((item, idx) => (
            <MemoizedTableRow key={keyExtractor(item, idx)}>
              {columns.map((col) => (
                <td key={col.key} className="py-2 px-3 whitespace-nowrap">
                  {col.render(item, idx)}
                </td>
              ))}
            </MemoizedTableRow>
          ))}
        </tbody>
      </table>
    </div>
  )
}
