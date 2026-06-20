import React from 'react'
import { cn, formatCurrency } from '@/lib/utils'
import { Badge } from '@/components/ui/Badge'

export type TradeRowVariant = 'default' | 'success' | 'warning' | 'error' | 'info'

export interface TradeRowItem {
  id: string
  badge?: { label: string; variant: TradeRowVariant }
  symbol?: string
  side?: string
  price?: number | string
  pnl?: number
  time?: string
  extra?: React.ReactNode
}

interface TradeRowProps {
  item: TradeRowItem
  className?: string
}

export const TradeRow: React.FC<TradeRowProps> = ({ item, className }) => {
  return (
    <div
      className={cn(
        'flex items-center justify-between rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2',
        className
      )}
    >
      <div className="flex items-center gap-2 min-w-0">
        {item.badge && (
          <Badge variant={item.badge.variant}>{item.badge.label}</Badge>
        )}
        {item.symbol && (
          <span className="text-xs text-[#ccc] truncate">{item.symbol}</span>
        )}
        {item.side && (
          <span className="text-xs text-[#888] whitespace-nowrap">{item.side}</span>
        )}
        {item.extra && <div className="flex items-center gap-2">{item.extra}</div>}
      </div>
      <div className="flex items-center gap-3 flex-shrink-0">
        {item.price !== undefined && (
          <span className="text-xs text-[#888]">@{item.price}</span>
        )}
        {item.pnl !== undefined && (
          <span
            className={cn(
              'text-xs font-medium',
              item.pnl >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]'
            )}
          >
            {item.pnl >= 0 ? '+' : ''}
            {formatCurrency(item.pnl)}
          </span>
        )}
        {item.time && (
          <span className="text-[10px] text-[#555] whitespace-nowrap">
            {item.time}
          </span>
        )}
      </div>
    </div>
  )
}

interface TradeRowListProps {
  items: TradeRowItem[]
  className?: string
  rowClassName?: string
  maxHeight?: string
}

export const TradeRowList: React.FC<TradeRowListProps> = ({
  items,
  className,
  rowClassName,
  maxHeight,
}) => {
  return (
    <div className={cn('space-y-1.5', className)} style={maxHeight ? { maxHeight, overflowY: 'auto' } : undefined}>
      {items.map((item) => (
        <TradeRow key={item.id} item={item} className={rowClassName} />
      ))}
    </div>
  )
}

export default TradeRow
