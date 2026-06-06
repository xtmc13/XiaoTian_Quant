import { RefreshCw, ChevronUp, ChevronDown, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { SkeletonBox } from './Skeletons'
import { formatPrice, getFearGreedClass, getVixClass } from '../utils'
import type { MarketIndex } from '../types'

export function TopIndexBar({
  marketData,
  loadingSentiment,
  loadingIndices,
  onRefresh,
  loadingMarket,
}: {
  marketData: { fearGreed?: number; vix?: number; dxy?: number; indices: MarketIndex[] }
  loadingSentiment: boolean
  loadingIndices: boolean
  onRefresh: () => void
  loadingMarket: boolean
}) {
  return (
    <div className="flex items-center gap-2 px-4 py-2 border-b border-quant-border bg-quant-bg-tertiary">
      {loadingSentiment ? (
        <>
          <SkeletonBox />
          <SkeletonBox />
          <SkeletonBox />
        </>
      ) : (
        <>
          <div
            className={cn(
              'indicator-box flex flex-col items-center px-2.5 py-1 bg-quant-card border border-quant-border rounded-md min-w-[50px]',
              getFearGreedClass(marketData.fearGreed)
            )}
          >
            <span className="text-[9px] text-muted-foreground uppercase tracking-wide">恐惧贪婪</span>
            <span className="text-[13px] font-bold text-foreground">{marketData.fearGreed ?? '--'}</span>
          </div>
          <div
            className={cn(
              'indicator-box flex flex-col items-center px-2.5 py-1 bg-quant-card border border-quant-border rounded-md min-w-[50px]',
              getVixClass(marketData.vix)
            )}
          >
            <span className="text-[9px] text-muted-foreground uppercase tracking-wide">VIX</span>
            <span className="text-[13px] font-bold text-foreground">{marketData.vix ?? '--'}</span>
          </div>
          <div className="indicator-box flex flex-col items-center px-2.5 py-1 bg-quant-card border border-quant-border rounded-md min-w-[50px]">
            <span className="text-[9px] text-muted-foreground uppercase tracking-wide">DXY</span>
            <span className="text-[13px] font-bold text-quant-blue">{marketData.dxy ?? '--'}</span>
          </div>
        </>
      )}

      {/* Indices Marquee */}
      <div className="flex-1 overflow-hidden min-w-0">
        {loadingIndices ? (
          <div className="flex items-center justify-center h-full text-xs text-muted-foreground gap-1">
            <Loader2 className="w-3 h-3 animate-spin" /> 加载中...
          </div>
        ) : marketData.indices.length > 0 ? (
          <div className="flex gap-2 animate-marquee whitespace-nowrap">
            {[...marketData.indices, ...marketData.indices].map((idx, i) => (
              <div
                key={`${idx.symbol}-${i}`}
                className="inline-flex items-center gap-1 px-2 py-1 bg-quant-card border border-quant-border rounded text-[11px]"
              >
                <span>{idx.flag}</span>
                <span className="text-muted-foreground font-medium">{idx.symbol}</span>
                <span className="text-foreground font-semibold">{formatPrice(idx.price)}</span>
                <span className={cn('font-semibold flex items-center gap-0.5', idx.change >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                  {idx.change >= 0 ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
                  {Math.abs(idx.change).toFixed(2)}%
                </span>
              </div>
            ))}
          </div>
        ) : (
          <div className="text-xs text-muted-foreground text-center">--</div>
        )}
      </div>

      <button
        onClick={onRefresh}
        disabled={loadingMarket}
        className="text-muted-foreground hover:text-foreground transition-colors shrink-0 disabled:opacity-50"
      >
        <RefreshCw className={cn('w-4 h-4', loadingMarket && 'animate-spin')} />
      </button>
    </div>
  )
}
