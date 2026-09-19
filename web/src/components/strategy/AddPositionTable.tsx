import type { AddPositionItem } from '@/types'
import { DataTable } from '@/components/DataTable'

interface AddPositionTableProps {
  value: AddPositionItem[]
  onChange: (next: AddPositionItem[]) => void
  disabled?: boolean
}

const inputCls =
  'w-full bg-quant-bg border border-quant-border rounded px-2 py-1 text-xs focus:outline-none focus:border-quant-gold'

export function AddPositionTable({
  value,
  onChange,
  disabled = false,
}: AddPositionTableProps) {
  const updateRow = (index: number, patch: Partial<AddPositionItem>) => {
    const next = value.map((row, i) => (i === index ? { ...row, ...patch } : row))
    onChange(next)
  }

  return (
    <DataTable
      data={value}
      keyExtractor={(item) => String(item.order)}
      emptyText="暂无补仓计划"
      columns={[
        {
          key: 'order',
          title: '订单',
          width: '60px',
          render: (item) => <span className="text-xs text-muted-foreground">第 {item.order} 单</span>,
        },
        {
          key: 'multiplier',
          title: '倍数',
          width: '90px',
          render: (item, index) => (
            <input
              type="number"
              min={0.01}
              step={0.01}
              disabled={disabled}
              value={item.multiplier}
              onChange={(e) => updateRow(index, { multiplier: Number(e.target.value) })}
              className={inputCls}
            />
          ),
        },
        {
          key: 'spread',
          title: '差价 (%)',
          width: '100px',
          render: (item, index) => (
            <input
              type="number"
              min={0.1}
              max={50}
              step={0.1}
              disabled={disabled}
              value={item.spread}
              onChange={(e) => updateRow(index, { spread: Number(e.target.value) })}
              className={inputCls}
            />
          ),
        },
        {
          key: 'callback',
          title: '回调 (%)',
          width: '100px',
          render: (item, index) => (
            <input
              type="number"
              min={0.01}
              max={5}
              step={0.01}
              disabled={disabled}
              value={item.callback}
              onChange={(e) => updateRow(index, { callback: Number(e.target.value) })}
              className={inputCls}
            />
          ),
        },
      ]}
    />
  )
}
