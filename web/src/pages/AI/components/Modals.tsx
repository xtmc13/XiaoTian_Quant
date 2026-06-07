import { memo } from 'react'
import { X, Plus } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { AIAnalysisResult } from '@/types'

export function AddStockModal({
  show,
  query,
  results,
  onClose,
  onQueryChange,
  onSearch,
  onSelect,
  onSelectPopular,
}: {
  show: boolean
  query: string
  results: string[]
  onClose: () => void
  onQueryChange: (q: string) => void
  onSearch: () => void
  onSelect: (sym: string) => void
  onSelectPopular: (sym: string) => void
}) {
  if (!show) return null

  return (
    <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-quant-card border border-quant-border rounded-xl shadow-xl w-[500px] max-w-[90vw] max-h-[80vh] flex flex-col">
        <div className="flex items-center justify-between px-4 py-3 border-b border-quant-border">
          <h3 className="text-sm font-bold text-foreground">添加标的</h3>
          <button onClick={onClose} aria-label="关闭" className="p-1 rounded text-muted-foreground hover:text-foreground transition-colors">
            <X className="w-4 h-4" />
          </button>
        </div>
        <div className="p-4 flex-1 overflow-auto">
          <div className="text-xs text-muted-foreground mb-3">搜索并选择要添加的标的</div>
          <div className="flex gap-2 mb-4">
            <input
              type="text"
              value={query}
              onChange={(e) => onQueryChange(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter') onSearch() }}
              placeholder="输入代码或名称..."
              className="flex-1 bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold"
            />
            <button onClick={onSearch} className="px-3 py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity">
              搜索
            </button>
          </div>
          {results.length > 0 && (
            <div className="space-y-1 mb-4">
              <div className="text-xs font-semibold text-foreground mb-2">搜索结果</div>
              {results.map((sym) => (
                <div key={sym} className="flex items-center justify-between p-2 rounded-lg hover:bg-quant-bg-secondary cursor-pointer transition-colors" onClick={() => onSelect(sym)}>
                  <span className="text-xs font-medium text-foreground">{sym}</span>
                  <Plus className="w-3.5 h-3.5 text-muted-foreground" />
                </div>
              ))}
            </div>
          )}
          <div className="text-xs font-semibold text-foreground mb-2">热门标的</div>
          <div className="space-y-1">
            {['AAPL', 'MSFT', 'NVDA', 'TSLA', 'BTC/USDT', 'ETH/USDT'].map((sym) => (
              <div key={sym} className="flex items-center justify-between p-2 rounded-lg hover:bg-quant-bg-secondary cursor-pointer transition-colors" onClick={() => onSelectPopular(sym)}>
                <span className="text-xs font-medium text-foreground">{sym}</span>
                <Plus className="w-3.5 h-3.5 text-muted-foreground" />
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}

const AnalysisHistoryItem = memo(function AnalysisHistoryItem({
  item,
  onClick,
}: {
  item: { symbol: string; result: AIAnalysisResult; time: number }
  onClick: () => void
}) {
  return (
    <div onClick={onClick} className="flex items-center justify-between p-3 rounded-lg bg-quant-bg-secondary hover:bg-white/5 cursor-pointer transition-colors">
      <div className="flex items-center gap-3">
        <span className="text-xs font-semibold text-foreground">{item.symbol}</span>
        <span className={cn(
          'text-[10px] px-1.5 py-0.5 rounded font-medium',
          item.result.consensus === 'bullish' ? 'bg-quant-green/10 text-quant-green' :
          item.result.consensus === 'bearish' ? 'bg-quant-red/10 text-quant-red' :
          'bg-quant-gold/10 text-quant-gold'
        )}>
          {item.result.consensus === 'bullish' ? '看涨' : item.result.consensus === 'bearish' ? '看跌' : '中性'}
        </span>
      </div>
      <span className="text-[10px] text-muted-foreground">
        {new Date(item.time).toLocaleString('zh-CN')}
      </span>
    </div>
  )
})

export function HistoryModal({
  show,
  history,
  onClose,
  onSelect,
}: {
  show: boolean
  history: { symbol: string; result: AIAnalysisResult; time: number }[]
  onClose: () => void
  onSelect: (item: { symbol: string; result: AIAnalysisResult }) => void
}) {
  if (!show) return null

  return (
    <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-quant-card border border-quant-border rounded-xl shadow-xl w-[700px] max-w-[95vw] max-h-[70vh] flex flex-col">
        <div className="flex items-center justify-between px-4 py-3 border-b border-quant-border">
          <h3 className="text-sm font-bold text-foreground">分析历史</h3>
          <button onClick={onClose} aria-label="关闭" className="p-1 rounded text-muted-foreground hover:text-foreground transition-colors">
            <X className="w-4 h-4" />
          </button>
        </div>
        <div className="p-4 flex-1 overflow-auto">
          {history.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground text-xs">暂无历史记录</div>
          ) : (
            <div className="space-y-2">
              {history.map((h) => (
                <AnalysisHistoryItem key={`${h.symbol}-${h.time}`} item={h} onClick={() => onSelect(h)} />
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
