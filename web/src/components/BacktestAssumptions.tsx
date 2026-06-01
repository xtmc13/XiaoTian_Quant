import { Info } from 'lucide-react'

interface Props {
  commission?: number
  slippage?: number
  leverage?: number
  initialBalance?: number
  interval?: string
}

export function BacktestAssumptions({
  commission = 0.001,
  slippage = 0.0005,
  leverage = 1,
  initialBalance = 10000,
  interval = '1h',
}: Props) {
  const items = [
    { label: '手续费', value: `${(commission * 100).toFixed(2)}%` },
    { label: '滑点', value: `${(slippage * 100).toFixed(2)}%` },
    { label: '杠杆', value: `${leverage}x` },
    { label: '初始资金', value: `$${initialBalance.toLocaleString()}` },
    { label: 'K线周期', value: interval },
  ]

  return (
    <div className="rounded-xl border border-quant-border/50 bg-quant-bg-secondary/50 p-3">
      <div className="flex items-center gap-1.5 mb-2">
        <Info className="h-3.5 w-3.5 text-muted-foreground" />
        <span className="text-[11px] font-medium text-muted-foreground">回测假设</span>
      </div>
      <div className="flex flex-wrap gap-x-4 gap-y-1">
        {items.map((item) => (
          <div key={item.label} className="flex items-center gap-1 text-[11px]">
            <span className="text-muted-foreground">{item.label}:</span>
            <span className="font-mono text-foreground">{item.value}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
