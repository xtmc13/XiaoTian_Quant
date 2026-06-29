import { cn } from '@/lib/utils'
import { SectionCard } from '@/components/ui/SectionCard'
import { CheckCircle2, ChevronUp } from 'lucide-react'
import type { ArbitrageHistoryItem } from '@/types'

interface CrossArbitrageHistoryProps {
  history: ArbitrageHistoryItem[] | undefined
  onClose: () => void
}

export function CrossArbitrageHistory({ history, onClose }: CrossArbitrageHistoryProps) {
  return (
    <SectionCard
      title={
        <button onClick={onClose} className="flex items-center gap-2">
          历史记录 <ChevronUp className="w-4 h-4" />
        </button>
      }
    >
      <div className="space-y-2 max-h-80 overflow-y-auto">
        {!history || history.length === 0 ? (
          <div className="text-sm text-muted-foreground text-center py-4">无历史记录</div>
        ) : (
          history.map((trade: ArbitrageHistoryItem, i: number) => (
            <div key={trade.id || i} className="flex items-center justify-between p-3 rounded-md bg-quant-bg-secondary">
              <div className="flex items-center gap-3">
                <CheckCircle2 className={cn('w-4 h-4', trade.net_profit > 0 ? 'text-green-400' : 'text-red-400')} />
                <div>
                  <div className="text-sm font-medium">{trade.symbol}</div>
                  <div className="text-xs text-muted-foreground">
                    {trade.buy_exchange} → {trade.sell_exchange}
                  </div>
                </div>
              </div>
              <div className="text-right">
                <div className={cn('text-sm font-semibold', trade.net_profit > 0 ? 'text-green-400' : 'text-red-400')}>
                  {trade.net_profit > 0 ? '+' : ''}${trade.net_profit.toFixed(2)}
                </div>
                <div className="text-xs text-muted-foreground">
                  {trade.closed_at ? new Date(trade.closed_at).toLocaleString() : '-'}
                </div>
              </div>
            </div>
          ))
        )}
      </div>
    </SectionCard>
  )
}
