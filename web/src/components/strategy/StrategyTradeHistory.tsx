import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { ExternalLink, History } from 'lucide-react'
import { tradesApi } from '@/lib/api'
import type { StrategyItem } from '@/types'
import { cn } from '@/lib/utils'

/** 本地/模拟盘成交（/api/trades source=local 的字段口径）。 */
interface LocalTrade {
  id: string
  symbol: string
  side: string
  price: number
  qty: number
  notional?: number
  /** SELL 行：该单实现盈亏（average-cost 口径）。 */
  pnl?: number
  time: number
  exchange?: string
  strategy_id?: string
}

function fmtTime(ms: number): string {
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function fmtPrice(v?: number): string {
  if (!v || !isFinite(v)) return '-'
  return v.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function fmtQty(v?: number): string {
  if (v == null || !isFinite(v)) return '-'
  return v.toLocaleString('en-US', { maximumFractionDigits: 8 })
}

/**
 * 机器人成交记录（2026-10-04）：按 sig: 打标归属过滤该策略的完整成交流水——
 * 时间/方向/价格/数量/金额/实现盈亏，每行"K线"跳转到内置交易图表的成交时刻
 * （/trading/spot?symbol=&interval=&t=，TradingSpot 消费参数后定位）。
 */
export function StrategyTradeHistory({ strategy }: { strategy: StrategyItem }) {
  const { data, isLoading } = useQuery({
    queryKey: ['strategy-trades', strategy.id],
    queryFn: () => tradesApi.list({ strategy_id: strategy.id, limit: '50' }),
    refetchInterval: 10_000,
  })
  const trades = (data ?? []) as unknown as LocalTrade[]

  return (
    <div className="rounded-lg bg-quant-bg border border-quant-border p-3 space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-[11px] text-muted-foreground flex items-center gap-1.5">
          <History className="w-3 h-3 text-quant-gold" />
          成交记录
        </span>
        <span className="text-[10px] text-muted-foreground font-mono">
          {trades.length > 0 ? `${trades.length} 笔` : ''}
        </span>
      </div>

      {isLoading ? (
        <div className="text-xs text-muted-foreground py-3 text-center">加载成交记录...</div>
      ) : trades.length === 0 ? (
        <div className="text-xs text-muted-foreground py-3 text-center">
          暂无成交——策略开仓/平仓后这里会显示完整流水
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-[11px]">
            <thead>
              <tr className="text-muted-foreground text-left">
                <th className="py-1 pr-2 font-normal">时间</th>
                <th className="py-1 pr-2 font-normal">方向</th>
                <th className="py-1 pr-2 font-normal text-right">价格</th>
                <th className="py-1 pr-2 font-normal text-right">数量</th>
                <th className="py-1 pr-2 font-normal text-right">金额</th>
                <th className="py-1 pr-2 font-normal text-right">实现盈亏</th>
                <th className="py-1 font-normal text-right">K线</th>
              </tr>
            </thead>
            <tbody>
              {trades.map((tr) => {
                const isBuy = tr.side === 'BUY'
                const tf = strategy.timeframe || '15m'
                return (
                  <tr key={tr.id} className="border-t border-quant-border/50">
                    <td className="py-1.5 pr-2 font-mono text-muted-foreground whitespace-nowrap">
                      {fmtTime(tr.time)}
                    </td>
                    <td className="py-1.5 pr-2">
                      <span
                        className={cn(
                          'inline-flex px-1.5 py-0.5 rounded text-[10px] font-medium',
                          isBuy ? 'bg-[#0ECB81]/10 text-[#0ECB81]' : 'bg-quant-red/10 text-quant-red'
                        )}
                      >
                        {isBuy ? '买入' : '卖出'}
                      </span>
                    </td>
                    <td className="py-1.5 pr-2 text-right font-mono">{fmtPrice(tr.price)}</td>
                    <td className="py-1.5 pr-2 text-right font-mono">{fmtQty(tr.qty)}</td>
                    <td className="py-1.5 pr-2 text-right font-mono text-muted-foreground">
                      {fmtPrice(tr.notional ?? tr.price * tr.qty)}
                    </td>
                    <td
                      className={cn(
                        'py-1.5 pr-2 text-right font-mono',
                        !tr.pnl ? 'text-muted-foreground' : tr.pnl >= 0 ? 'text-[#0ECB81]' : 'text-quant-red'
                      )}
                    >
                      {!tr.pnl ? '-' : `${tr.pnl >= 0 ? '+' : '-'}$${fmtPrice(Math.abs(tr.pnl))}`}
                    </td>
                    <td className="py-1.5 text-right">
                      <a
                        href={`/trading/spot?symbol=${tr.symbol}&interval=${tf}&t=${Math.floor(tr.time)}`}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-0.5 text-quant-gold hover:underline"
                      >
                        K线
                        <ExternalLink className="w-2.5 h-2.5" />
                      </a>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
