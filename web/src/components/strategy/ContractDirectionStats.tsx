import type { StrategyItem } from '@/types'
import { cn } from '@/lib/utils'

interface ContractDirectionStatsProps {
  strategies: StrategyItem[]
}

export function ContractDirectionStats({ strategies }: ContractDirectionStatsProps) {
  const running = strategies.filter((s) => s.status === 'running')
  const long = running.filter((s) => s.trade_direction === 'long' || s.direction === 'long').length
  const short = running.filter((s) => s.trade_direction === 'short' || s.direction === 'short').length
  const dual = running.filter(
    (s) => s.trade_direction === 'both' || s.trade_direction === 'dual' || s.direction === 'dual'
  ).length

  return (
    <div className="flex items-center gap-3 text-[11px]">
      <Stat label="开多" value={long} color="text-quant-green" />
      <Stat label="开空" value={short} color="text-quant-red" />
      <Stat label="双向" value={dual} color="text-quant-gold" />
      <Stat label="在线单数" value={running.length} />
    </div>
  )
}

function Stat({ label, value, color }: { label: string; value: number; color?: string }) {
  return (
    <div className="flex items-center gap-1 px-2 py-1 rounded bg-quant-bg border border-quant-border">
      <span className="text-muted-foreground">{label}</span>
      <span className={cn('font-mono font-semibold', color || 'text-foreground')}>{value}</span>
    </div>
  )
}
