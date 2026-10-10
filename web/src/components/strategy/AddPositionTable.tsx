import type { AddPositionItem } from '@/types'
import { DataTable } from '@/components/DataTable'

interface AddPositionTableProps {
  value: AddPositionItem[]
  onChange: (next: AddPositionItem[]) => void
  disabled?: boolean
  /** 档级 EMA 开关列（仅合约：引擎只给合约评估补仓指标，现货显示即死开关）。
   *  与全局"补仓 EMA 确认"开关双闸——两者同开时该档补仓才要求 EMA 确认。 */
  showEma?: boolean
}

const inputCls =
  'w-full bg-quant-bg border border-quant-border rounded px-2 py-1 text-xs focus:outline-none focus:border-quant-gold'

export function AddPositionTable({
  value,
  onChange,
  disabled = false,
  showEma = false,
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
          width: '76px',
          render: (item) => <span className="text-xs text-muted-foreground">第 {item.order} 次补仓</span>,
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
        ...(showEma
          ? [
              {
                key: 'emaEnabled',
                title: 'EMA 确认',
                width: '70px',
                render: (item: AddPositionItem, index: number) => (
                  <input
                    type="checkbox"
                    aria-label={`第 ${item.order} 次补仓 EMA 确认`}
                    disabled={disabled}
                    checked={item.emaEnabled ?? false}
                    onChange={(e) => updateRow(index, { emaEnabled: e.target.checked })}
                    className="rounded"
                  />
                ),
              },
            ]
          : []),
      ]}
    />
  )
}
