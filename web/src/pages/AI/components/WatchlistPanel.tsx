import { Star, Plus, Wallet, Clock, Trash2, ChevronUp, ChevronDown } from 'lucide-react'
import { cn } from '@/lib/utils'
import { formatNum, formatPrice } from '../utils'
import { MARKET_NAMES } from '../constants'
import type { WatchlistItem, WatchlistPrice, PositionSummary } from '../types'

export function WatchlistPanel({
  watchlist,
  watchlistPrices,
  positionSummaryMap,
  selectedSymbol,
  onSelect,
  onRemove,
  onAdd,
}: {
  watchlist: WatchlistItem[]
  watchlistPrices: Record<string, WatchlistPrice>
  positionSummaryMap: Record<string, PositionSummary>
  selectedSymbol: string | undefined
  onSelect: (stock: WatchlistItem) => void
  onRemove: (stock: WatchlistItem) => void
  onAdd: () => void
}) {
  return (
    <div className="hidden md:flex w-[280px] shrink-0 bg-quant-card border border-quant-border rounded-xl shadow-sm flex-col overflow-hidden">
      <div className="flex items-center justify-between px-3.5 py-3 border-b border-quant-border bg-quant-bg-tertiary rounded-t-xl">
        <div className="flex items-center gap-1.5 text-xs font-bold text-foreground">
          <Star className="w-3.5 h-3.5 text-quant-gold fill-quant-gold" />
          自选股
        </div>
        <div className="flex items-center gap-1">
          <button
            onClick={onAdd}
            className="p-1 rounded text-muted-foreground hover:text-quant-gold hover:bg-quant-gold/10 transition-colors"
          >
            <Plus className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto min-h-0 p-2 scrollbar-thin">
        {watchlist.length === 0 ? (
          <div className="text-center py-8 text-muted-foreground">
            <Star className="w-8 h-8 mx-auto mb-2 opacity-30" />
            <p className="text-xs mb-3">暂无自选股</p>
            <button
              onClick={onAdd}
              className="px-3 py-1.5 rounded-md bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity"
            >
              添加标的
            </button>
          </div>
        ) : (
          watchlist.map((stock) => {
            const key = `${stock.market}:${stock.symbol}`
            const priceData = watchlistPrices[key]
            const pos = positionSummaryMap[key]
            const isActive = selectedSymbol === key
            const change = priceData?.change ?? stock.change ?? 0
            const price = priceData?.price ?? stock.price ?? 0

            return (
              <div
                key={key}
                onClick={() => onSelect(stock)}
                className={cn(
                  'relative group rounded-lg p-2.5 mb-1 cursor-pointer transition-all border border-transparent',
                  isActive
                    ? 'bg-quant-gold/5 border-quant-gold/30 shadow-sm'
                    : 'hover:bg-quant-bg-secondary hover:border-quant-border'
                )}
              >
                <div className="flex items-center justify-between">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                      <span className="text-xs font-bold text-foreground truncate">{stock.symbol}</span>
                      <span className="text-[9px] text-muted-foreground px-1 py-0.5 bg-quant-bg-secondary rounded">
                        {MARKET_NAMES[stock.market] || stock.market}
                      </span>
                    </div>
                    {stock.name && stock.name !== stock.symbol && (
                      <div className="text-[10px] text-muted-foreground truncate">{stock.name}</div>
                    )}
                  </div>
                  <div className="text-right shrink-0 ml-2">
                    <div className="text-xs font-mono font-semibold text-foreground">{formatPrice(price)}</div>
                    <div
                      className={cn(
                        'text-[10px] font-mono font-semibold inline-flex items-center gap-0.5 px-1 py-0.5 rounded',
                        change >= 0 ? 'text-quant-green bg-quant-green/10' : 'text-quant-red bg-quant-red/10'
                      )}
                    >
                      {change >= 0 ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
                      {formatNum(change)}%
                    </div>
                  </div>
                </div>

                {/* Position row */}
                {pos && pos.quantity > 0 && (
                  <div className="flex items-center justify-between mt-1.5 text-[10px] font-mono">
                    <span className="text-muted-foreground">
                      {formatNum(pos.quantity, 4)} @ {formatPrice(pos.avgEntry)}
                    </span>
                    <span className={cn(pos.pnl >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                      {pos.pnl >= 0 ? '+' : ''}
                      {formatNum(pos.pnl)} ({pos.pnlPercent >= 0 ? '+' : ''}
                      {formatNum(pos.pnlPercent)}%)
                    </span>
                  </div>
                )}

                {/* Hover actions */}
                <div className="absolute top-0 right-0 bottom-0 flex items-center gap-1 pr-2 opacity-0 group-hover:opacity-100 transition-opacity bg-gradient-to-l from-quant-bg via-quant-bg/80 to-transparent rounded-r-lg">
                  <button
                    onClick={(e) => { e.stopPropagation() }}
                    className="p-1 rounded bg-quant-card border border-quant-border text-muted-foreground hover:text-quant-gold transition-colors"
                    title="持仓"
                  >
                    <Wallet className="w-3 h-3" />
                  </button>
                  <button
                    onClick={(e) => { e.stopPropagation() }}
                    className="p-1 rounded bg-quant-card border border-quant-border text-muted-foreground hover:text-quant-gold transition-colors"
                    title="任务"
                  >
                    <Clock className="w-3 h-3" />
                  </button>
                  <button
                    onClick={(e) => { e.stopPropagation(); onRemove(stock) }}
                    className="p-1 rounded bg-quant-card border border-quant-border text-muted-foreground hover:text-quant-red transition-colors"
                    title="删除"
                  >
                    <Trash2 className="w-3 h-3" />
                  </button>
                </div>
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}
