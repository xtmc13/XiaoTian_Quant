import React, { useMemo } from 'react'
import { cn } from '@/lib/utils'
import { Star } from 'lucide-react'
import { formatPrice } from '@/lib/tradingHelpers'

export interface WatchlistItemData {
  sym: string
  active: boolean
  onClick: () => void
  price?: number
  changePct?: number
}

export const WatchlistItem = React.memo(function WatchlistItem({
  sym,
  active,
  onClick,
  price,
  changePct,
}: WatchlistItemData) {
  const isUp = (changePct || 0) >= 0
  return (
    <button
      onClick={onClick}
      className={cn(
        'w-full flex items-center justify-between px-3 py-2 text-xs transition-colors',
        active ? 'bg-quant-gold/10 text-quant-gold' : 'hover:bg-white/5'
      )}
    >
      <div className="flex items-center gap-2">
        <Star className={cn('w-3 h-3', active ? 'fill-quant-gold text-quant-gold' : 'text-muted-foreground')} />
        <span className="font-semibold tracking-tight">{sym.replace('USDT', '/USDT')}</span>
      </div>
      <div className="text-right">
        <div className="font-mono font-medium text-foreground">{price ? formatPrice(price) : '--'}</div>
        <div className={cn('font-mono text-[10px]', isUp ? 'text-[#0ECB81]' : 'text-[#F6465D]')}>
          {isUp ? '+' : ''}
          {changePct?.toFixed(2) ?? '--'}%
        </div>
      </div>
    </button>
  )
})

export interface WatchlistPanelProps {
  symbols: string[]
  activeSymbol: string
  onSymbolChange: (sym: string) => void
  priceMap: Record<string, number>
  snapshotPrice?: number
  changePct?: number
  search: string
  onSearchChange: (s: string) => void
}

export function WatchlistPanel({
  symbols,
  activeSymbol,
  onSymbolChange,
  priceMap,
  snapshotPrice,
  changePct,
  search,
  onSearchChange,
}: WatchlistPanelProps) {
  const filtered = useMemo(() => {
    if (!search.trim()) return symbols
    const q = search.toUpperCase()
    return symbols.filter((s) => s.includes(q))
  }, [symbols, search])

  return (
    <div className="h-[220px] shrink-0 border-b border-quant-border flex flex-col">
      <div className="h-8 flex items-center px-3 border-b border-quant-border justify-between">
        <span className="text-xs font-medium text-muted-foreground">自选</span>
        <div className="relative">
          <input
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder="搜索"
            aria-label="搜索交易对"
            className="w-24 h-7 pl-6 pr-2 text-[10px] bg-quant-bg border border-quant-border rounded focus:outline-none focus:border-quant-gold text-foreground placeholder:text-muted-foreground"
          />
        </div>
      </div>
      <div className="flex-1 overflow-y-auto">
        {filtered.map((sym) => {
          const isActive = sym === activeSymbol
          const symPrice = isActive ? snapshotPrice : priceMap[sym] || undefined
          return (
            <WatchlistItem
              key={sym}
              sym={sym}
              active={isActive}
              onClick={() => onSymbolChange(sym)}
              price={symPrice}
              changePct={isActive ? changePct : undefined}
            />
          )
        })}
      </div>
    </div>
  )
}
