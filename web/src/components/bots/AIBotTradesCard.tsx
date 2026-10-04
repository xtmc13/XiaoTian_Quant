import { useQuery } from '@tanstack/react-query'
import { aiBotApi } from '@/lib/api'
import type { AIBotTrade } from '@/types'
import { useI18n } from '@/i18n'
import { SectionCard } from '@/components/ui/SectionCard'
import { Skeleton } from '@/components/ui/Skeleton'
import { History } from 'lucide-react'
import { cn } from '@/lib/utils'

/** ai_bot_trades 的 opened_at/closed_at 为 Unix 秒（handler/ai_bot_simulator 写入口径）。 */
function fmtTime(sec: number): string {
  if (!sec) return '-'
  const d = new Date(sec * 1000)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function fmtPrice(v?: number): string {
  if (v == null || !isFinite(v)) return '-'
  return v.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 6 })
}

/**
 * AI 机器人实例成交记录（GET /ai-bots/instances/:id/trades）：
 * 与策略成交历史（StrategyTradeHistory）同口径的流水表——平仓时间/方向/数量/
 * 开平仓价/实现盈亏/平仓原因。
 */
export function AIBotTradesCard({ botId }: { botId: string }) {
  const { t } = useI18n()
  const { data, isLoading } = useQuery({
    queryKey: ['ai-bots', 'trades', botId],
    queryFn: () => aiBotApi.trades(botId, 50),
    refetchInterval: 15000,
    retry: false,
  })
  const trades: AIBotTrade[] = data?.trades ?? []

  return (
    <SectionCard
      title={
        <span className="flex items-center gap-1.5">
          <History className="h-4 w-4 text-quant-gold" />
          {t('aibots.trades.title')}
          {trades.length > 0 && (
            <span className="text-[10px] font-normal text-muted-foreground">
              {t('aibots.trades.count').replace('{n}', String(trades.length))}
            </span>
          )}
        </span>
      }
    >
      {isLoading ? (
        <Skeleton className="h-32 rounded-lg" />
      ) : trades.length === 0 ? (
        <div className="flex h-32 items-center justify-center rounded-lg border border-dashed border-[#1c1c1c] bg-[#0a0a0a]">
          <p className="text-xs text-[#8a8a8a]">{t('aibots.trades.empty')}</p>
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-[11px]">
            <thead>
              <tr className="text-left text-muted-foreground">
                <th className="py-1 pr-2 font-normal">{t('aibots.trades.closedAt')}</th>
                <th className="py-1 pr-2 font-normal">{t('aibots.trades.symbol')}</th>
                <th className="py-1 pr-2 font-normal">{t('aibots.trades.side')}</th>
                <th className="py-1 pr-2 font-normal text-right">{t('aibots.trades.qty')}</th>
                <th className="py-1 pr-2 font-normal text-right">{t('aibots.trades.entry')}</th>
                <th className="py-1 pr-2 font-normal text-right">{t('aibots.trades.exit')}</th>
                <th className="py-1 pr-2 font-normal text-right">{t('aibots.trades.pnl')}</th>
                <th className="py-1 font-normal">{t('aibots.trades.reason')}</th>
              </tr>
            </thead>
            <tbody>
              {trades.map((tr) => (
                <tr key={tr.id} className="border-t border-[#1c1c1c]">
                  <td className="py-1.5 pr-2 font-mono text-muted-foreground whitespace-nowrap">
                    {fmtTime(tr.closed_at || tr.opened_at)}
                  </td>
                  <td className="py-1.5 pr-2 font-mono">{tr.symbol}</td>
                  <td className="py-1.5 pr-2">
                    <span
                      className={cn(
                        'inline-flex rounded px-1.5 py-0.5 text-[10px] font-medium',
                        tr.side === 'LONG' ? 'bg-[#0ECB81]/10 text-[#0ECB81]' : 'bg-red-500/10 text-red-400'
                      )}
                    >
                      {tr.side}
                    </span>
                  </td>
                  <td className="py-1.5 pr-2 text-right font-mono">{fmtPrice(tr.quantity)}</td>
                  <td className="py-1.5 pr-2 text-right font-mono">{fmtPrice(tr.entry_price)}</td>
                  <td className="py-1.5 pr-2 text-right font-mono">{fmtPrice(tr.exit_price)}</td>
                  <td
                    className={cn(
                      'py-1.5 pr-2 text-right font-mono',
                      !tr.pnl ? 'text-muted-foreground' : tr.pnl >= 0 ? 'text-[#0ECB81]' : 'text-red-400'
                    )}
                  >
                    {tr.pnl ? `${tr.pnl >= 0 ? '+' : ''}${fmtPrice(tr.pnl)}` : '-'}
                    {tr.pnl_pct ? (
                      <span className="ml-1 text-[10px] text-muted-foreground">
                        ({tr.pnl_pct >= 0 ? '+' : ''}
                        {tr.pnl_pct.toFixed(2)}%)
                      </span>
                    ) : null}
                  </td>
                  <td className="py-1.5 text-muted-foreground">{tr.close_reason || '-'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </SectionCard>
  )
}
