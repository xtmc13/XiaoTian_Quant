import type { MovingTPTier } from '@/types'
import { DataTable } from '@/components/DataTable'

interface MovingTPTiersTableProps {
  value: MovingTPTier[]
  onChange: (next: MovingTPTier[]) => void
  disabled?: boolean
}

const inputCls =
  'w-full bg-quant-bg border border-quant-border rounded px-2 py-1 text-xs focus:outline-none focus:border-quant-gold'

export function MovingTPTiersTable({ value, onChange, disabled = false }: MovingTPTiersTableProps) {
  const updateRow = (index: number, patch: Partial<MovingTPTier>) => {
    const next = value.map((row, i) => (i === index ? { ...row, ...patch } : row))
    onChange(next)
  }

  return (
    <DataTable
      data={value}
      keyExtractor={(_, index) => String(index)}
      emptyText="暂无移动止盈档位"
      columns={[
        {
          key: 'tier',
          title: '档位',
          width: '60px',
          render: (_, index) => <span className="text-xs text-muted-foreground">第 {index + 1} 档</span>,
        },
        {
          key: 'ratio',
          title: '止盈比例 (%)',
          width: '120px',
          render: (item, index) => (
            <input
              type="number"
              min={0.1}
              max={50}
              step={0.1}
              disabled={disabled}
              value={item.ratio}
              onChange={(e) => updateRow(index, { ratio: Number(e.target.value) })}
              className={inputCls}
            />
          ),
        },
        {
          key: 'drawback',
          title: '止盈回撤 (%)',
          width: '120px',
          render: (item, index) => (
            <input
              type="number"
              min={0.1}
              max={100}
              step={0.1}
              disabled={disabled}
              value={item.drawback}
              onChange={(e) => updateRow(index, { drawback: Number(e.target.value) })}
              className={inputCls}
            />
          ),
        },
      ]}
    />
  )
}
