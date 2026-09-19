import { KPICard } from '@/components/ui/KPICard'
import { Activity, AlertCircle, DollarSign, Target, Zap } from 'lucide-react'
import { useI18n } from '@/i18n'

interface CrossArbitrageKPIsProps {
  isRunning: boolean
  stats: Record<string, string | number | undefined>
}

export function CrossArbitrageKPIs({ isRunning, stats }: CrossArbitrageKPIsProps) {
  const { t } = useI18n()
  return (
    <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
      <KPICard
        label={t('arb.ui.engine-status')}
        value={isRunning ? t('arb.ui.running') : t('arb.ui.stopped')}
        icon={
          isRunning ? <Activity className="w-4 h-4 text-green-400" /> : <AlertCircle className="w-4 h-4 text-red-400" />
        }
        subValue={isRunning ? t('arb.ui.monitoring') : t('arb.ui.click-to-start')}
        trend={isRunning ? 'up' : 'down'}
      />
      <KPICard
        label={t('arb.cross.checks')}
        value={stats.checks ?? 0}
        icon={<Target className="w-4 h-4 text-quant-gold" />}
        subValue={t('arb.ui.sub-total-scan')}
        trend="neutral"
      />
      <KPICard
        label={t('arb.cross.executions')}
        value={stats.executions ?? 0}
        icon={<Zap className="w-4 h-4 text-quant-gold" />}
        subValue={t('arb.cross.sub-executed')}
        trend="up"
      />
      <KPICard
        label={t('arb.ui.total-profit')}
        value={stats.total_profit ? `$${(stats.total_profit as number).toFixed(2)}` : '$0.00'}
        icon={<DollarSign className="w-4 h-4 text-quant-gold" />}
        subValue={t('arb.ui.cumulative')}
        trend="up"
      />
    </div>
  )
}
