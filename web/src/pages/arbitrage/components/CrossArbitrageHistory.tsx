import { cn } from '@/lib/utils'
import { CheckCircle2, X } from 'lucide-react'
import type { ArbitrageHistoryItem } from '@/types'
import { useI18n } from '@/i18n'

interface CrossArbitrageHistoryProps {
  history: ArbitrageHistoryItem[] | undefined
  onClose: () => void
}

export function CrossArbitrageHistory({ history, onClose }: CrossArbitrageHistoryProps) {
  const { t } = useI18n()
  return (
    <div
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
      onClick={onClose}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onClose()
      }}
      tabIndex={-1}
    >
      <div
        role="document"
        className="w-full max-w-2xl max-h-[80vh] flex flex-col rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
          <h3 className="text-sm font-bold">{t('arb.ui.history-record')}</h3>
          <button onClick={onClose} aria-label={t('arb.ui.close')} className="text-muted-foreground hover:text-foreground">
            <X className="w-4 h-4" />
          </button>
        </div>
        <div className="flex-1 overflow-y-auto p-6">
          <div className="space-y-2">
            {!history || history.length === 0 ? (
              <div className="text-sm text-muted-foreground text-center py-4">{t('arb.ui.no-history')}</div>
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
        </div>
      </div>
    </div>
  )
}
