import { KPICard } from '@/components/ui/KPICard'
import { Activity, AlertCircle, DollarSign, Target, Zap } from 'lucide-react'

interface CrossArbitrageKPIsProps {
  isRunning: boolean
  stats: Record<string, string | number | undefined>
}

export function CrossArbitrageKPIs({ isRunning, stats }: CrossArbitrageKPIsProps) {
  return (
    <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
      <KPICard
        label="引擎状态"
        value={isRunning ? '运行中' : '已停止'}
        icon={
          isRunning ? <Activity className="w-4 h-4 text-green-400" /> : <AlertCircle className="w-4 h-4 text-red-400" />
        }
        subValue={isRunning ? '监控中' : '点击启动'}
        trend={isRunning ? 'up' : 'down'}
      />
      <KPICard
        label="检测次数"
        value={stats.checks ?? 0}
        icon={<Target className="w-4 h-4 text-quant-gold" />}
        subValue="总扫描"
        trend="neutral"
      />
      <KPICard
        label="执行次数"
        value={stats.executions ?? 0}
        icon={<Zap className="w-4 h-4 text-quant-gold" />}
        subValue="已执行"
        trend="up"
      />
      <KPICard
        label="总利润"
        value={stats.total_profit ? `$${(stats.total_profit as number).toFixed(2)}` : '$0.00'}
        icon={<DollarSign className="w-4 h-4 text-quant-gold" />}
        subValue="累计"
        trend="up"
      />
    </div>
  )
}
