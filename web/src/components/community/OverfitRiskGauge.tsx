import { cn } from '@/lib/utils'
import { AlertTriangle, CheckCircle2, Info } from 'lucide-react'

interface OverfitRiskGaugeProps {
  result?: {
    score: number
    risk_level: 'low' | 'medium' | 'high' | 'insufficient_data'
  } | null
  size?: 'sm' | 'md' | 'lg'
  showLabel?: boolean
}

const LEVEL_META: Record<string, { label: string; color: string; bg: string; icon: typeof AlertTriangle }> = {
  low: { label: '低风险', color: 'text-quant-green', bg: 'bg-quant-green/10', icon: CheckCircle2 },
  medium: { label: '中风险', color: 'text-amber-400', bg: 'bg-amber-500/10', icon: AlertTriangle },
  high: { label: '高风险', color: 'text-quant-red', bg: 'bg-quant-red/10', icon: AlertTriangle },
  insufficient_data: { label: '数据不足', color: 'text-muted-foreground', bg: 'bg-quant-bg-tertiary', icon: Info },
}

export function OverfitRiskGauge({ result, size = 'md', showLabel = true }: OverfitRiskGaugeProps) {
  if (!result) {
    return (
      <span className="text-[10px] text-muted-foreground">未检测</span>
    )
  }

  const meta = LEVEL_META[result.risk_level] || LEVEL_META.insufficient_data
  const Icon = meta.icon

  const sizeClasses = {
    sm: 'h-1.5 w-16',
    md: 'h-2 w-24',
    lg: 'h-2.5 w-32',
  }

  const pct = Math.min(100, Math.max(0, result.score))

  let barColor = 'bg-quant-green'
  if (result.risk_level === 'medium') barColor = 'bg-amber-400'
  if (result.risk_level === 'high') barColor = 'bg-quant-red'

  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-1.5">
        <span className={cn('flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-medium', meta.bg, meta.color)}>
          <Icon className="h-3 w-3" />
          {showLabel && meta.label}
        </span>
        <span className="text-[10px] font-mono text-muted-foreground">{result.score.toFixed(1)}</span>
      </div>
      <div className={cn('rounded-full bg-quant-bg-tertiary overflow-hidden', sizeClasses[size])}>
        <div
          className={cn('h-full rounded-full transition-all', barColor)}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  )
}
