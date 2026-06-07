import React, { useMemo } from 'react'
import { cn } from '@/lib/utils'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { formatPrice, formatTime } from '@/lib/tradingHelpers'
import type { Trade } from '@/types'

interface OrderBookPanelProps {
  orderbook?: { bids?: [number, number][]; asks?: [number, number][] }
  obLoading: boolean
  obPrecision: string
  onPrecisionChange: (p: string) => void
  onPriceClick: (price: string) => void
  recentTrades?: Trade[]
  liveTrades: Array<{ id: string; price: number; quantity: number; side: 'buy' | 'sell'; time: number }>
  lastPrice: number
  bestBid: string
  bestAsk: string
  symbol: string
  midPriceContent?: React.ReactNode
  recentTradesHeader?: React.ReactNode
}

function obFormatPrice(p: number, precision: string): string {
  const tick = parseFloat(precision) || 0.1
  const decimals = precision.includes('.') ? precision.split('.')[1].length : 0
  return (Math.round(p / tick) * tick).toFixed(decimals)
}

export const OrderBookPanel = React.memo(function OrderBookPanel({
  orderbook,
  obLoading,
  obPrecision,
  onPrecisionChange,
  onPriceClick,
  recentTrades,
  liveTrades,
  lastPrice,
  bestBid,
  bestAsk,
  symbol,
  midPriceContent,
  recentTradesHeader,
}: OrderBookPanelProps) {
  const obMax = useMemo(() => {
    if (!orderbook) return 1
    const bidMax = Math.max(
      ...(orderbook.bids || []).map((b: [number, number]) => Number(b[1]) || 0),
      0
    )
    const askMax = Math.max(
      ...(orderbook.asks || []).map((a: [number, number]) => Number(a[1]) || 0),
      0
    )
    return Math.max(bidMax, askMax, 1)
  }, [orderbook])

  const displayTrades = useMemo(() => {
    const src = liveTrades.length ? liveTrades : (recentTrades || [])
    return src.slice(0, 50)
  }, [liveTrades, recentTrades])

  const isUp = useMemo(() => {
    if (!orderbook?.bids?.[0] || !orderbook?.asks?.[0]) return true
    return (orderbook.bids[0][0] + orderbook.asks[0][0]) / 2 >= lastPrice
  }, [orderbook, lastPrice])

  return (
    <div className="bg-quant-bg-secondary flex flex-col overflow-hidden min-h-0">
      {/* Header */}
      <div className="h-8 shrink-0 border-b border-quant-border flex items-center justify-between px-3">
        <div className="flex gap-3 items-center">
          <span className="text-xs font-medium text-foreground">订单簿</span>
        </div>
        <div className="flex gap-1">
          {['0.1', '1', '10'].map((p) => (
            <span
              key={p}
              onClick={() => onPrecisionChange(p)}
              className={cn(
                'text-[10px] px-1 py-0.5 rounded cursor-pointer',
                obPrecision === p
                  ? 'bg-quant-hover text-foreground'
                  : 'text-muted-foreground hover:text-foreground'
              )}
            >
              {p}
            </span>
          ))}
        </div>
      </div>

      {/* Column headers */}
      <div className="flex text-[10px] text-muted-foreground px-3 py-1.5 border-b border-quant-border shrink-0">
        <span className="flex-1">价格 (USDT)</span>
        <span className="flex-1 text-right">数量</span>
        <span className="flex-1 text-right">累计</span>
      </div>

      {/* Orderbook rows */}
      <div className="flex-1 overflow-y-auto">
        {obLoading ? (
          <div className="p-3 space-y-1">
            {Array.from({ length: 12 }).map((_, i) => (
              <Skeleton key={i} variant="text" height={16} />
            ))}
          </div>
        ) : orderbook ? (
          <>
            {/* Asks (reversed) */}
            <div className="flex flex-col-reverse">
              {(orderbook.asks || [])
                .slice(0, 10)
                .map((ask: [number, number], i: number) => {
                  const p = Number(ask[0]),
                    q = Number(ask[1])
                  return (
                    <div
                      key={'ask-' + i}
                      className="relative flex px-3 py-0.5 text-[11px] font-mono cursor-pointer hover:bg-white/[0.04]"
                      onClick={() => onPriceClick(obFormatPrice(p, obPrecision))}
                    >
                      <div
                        className="absolute top-0 bottom-0 right-0 opacity-20 z-0"
                        style={{
                          background: '#F6465D',
                          width: Math.min((q / obMax) * 100, 100) + '%',
                        }}
                      />
                      <span className="flex-1 text-quant-red relative z-10">
                        {obFormatPrice(p, obPrecision)}
                      </span>
                      <span className="flex-1 text-right text-muted-foreground relative z-10">
                        {q.toFixed(4)}
                      </span>
                      <span className="flex-1 text-right text-muted-foreground relative z-10">
                        {(p * q).toFixed(2)}
                      </span>
                    </div>
                  )
                })}
            </div>

            {/* Mid price */}
            <div className="flex items-center justify-center py-1.5 border-y border-quant-border bg-quant-bg-tertiary shrink-0">
              {midPriceContent || (
                <>
                  <span
                    className={cn(
                      'text-sm font-bold font-mono',
                      isUp ? 'text-quant-green' : 'text-quant-red'
                    )}
                  >
                    {lastPrice ? lastPrice.toFixed(2) : '--'}
                  </span>
                  <span className="text-[10px] text-muted-foreground ml-2">
                    spread{' '}
                    {bestAsk && bestBid
                      ? (parseFloat(bestAsk) - parseFloat(bestBid)).toFixed(2)
                      : '--'}
                  </span>
                </>
              )}
            </div>

            {/* Bids */}
            <div>
              {(orderbook.bids || [])
                .slice(0, 10)
                .map((bid: [number, number], i: number) => {
                  const p = Number(bid[0]),
                    q = Number(bid[1])
                  return (
                    <div
                      key={'bid-' + i}
                      className="relative flex px-3 py-0.5 text-[11px] font-mono cursor-pointer hover:bg-white/[0.04]"
                      onClick={() => onPriceClick(obFormatPrice(p, obPrecision))}
                    >
                      <div
                        className="absolute top-0 bottom-0 left-0 opacity-20 z-0"
                        style={{
                          background: '#2EBD85',
                          width: Math.min((q / obMax) * 100, 100) + '%',
                        }}
                      />
                      <span className="flex-1 text-quant-green relative z-10">
                        {obFormatPrice(p, obPrecision)}
                      </span>
                      <span className="flex-1 text-right text-muted-foreground relative z-10">
                        {q.toFixed(4)}
                      </span>
                      <span className="flex-1 text-right text-muted-foreground relative z-10">
                        {(p * q).toFixed(2)}
                      </span>
                    </div>
                  )
                })}
            </div>
          </>
        ) : (
          <div className="py-8">
            <EmptyState title="暂无订单簿数据" description="等待市场数据连接..." />
          </div>
        )}
      </div>

      {/* Recent trades */}
      <div
        className="h-[150px] shrink-0 border-t border-quant-border overflow-y-auto"
        role="region"
        aria-label="最新成交"
      >
        <div className="flex items-center h-7 px-3 border-b border-quant-border bg-quant-bg-secondary gap-3">
          {recentTradesHeader || (
            <span className="text-[11px] font-medium text-muted-foreground">最新成交</span>
          )}
        </div>
        <div className="flex text-[10px] text-muted-foreground px-3 py-1 border-b border-quant-border sticky top-0 bg-quant-bg-secondary">
          <span className="flex-1">时间</span>
          <span className="flex-1 text-right">价格</span>
          <span className="flex-1 text-right">数量</span>
        </div>
        {displayTrades.slice(0, 20).map((t, i) => (
          <div key={t.id || i} className="flex px-3 py-0.5 text-[11px] font-mono">
            <span className="flex-1 text-muted-foreground">{formatTime(t.time)}</span>
            <span
              className={cn(
                'flex-1 text-right',
                t.side === 'buy' ? 'text-quant-green' : 'text-quant-red'
              )}
            >
              {formatPrice(t.price)}
            </span>
            <span className="flex-1 text-right text-muted-foreground">
              {t.quantity.toFixed(4)}
            </span>
          </div>
        ))}
        {!displayTrades.length && (
          <div className="py-4">
            <EmptyState title="暂无成交记录" description="等待实时成交数据..." />
          </div>
        )}
      </div>
    </div>
  )
})
