import { useState, useEffect, useCallback } from 'react'
import { Brain, Zap, History, Search, Plus } from 'lucide-react'
import { toast } from '@/lib/useToast'
import { aiApi, marketApi } from '@/lib/api'
import type { AIAnalysisResult, AIModelAnalysis } from '@/types'

import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { AnalysisPlaceholder, AnalysisResultView } from '../AI/components/AnalysisPanel'
import { WatchlistPanel } from '../AI/components/WatchlistPanel'
import { AddStockModal, HistoryModal } from '../AI/components/Modals'

import { MARKET_NAMES } from '../AI/constants'
import type { WatchlistItem, WatchlistPrice, PositionSummary } from '../AI/types'

export function AIAnalysis() {
  const [selectedSymbol, setSelectedSymbol] = useState<string | undefined>(undefined)
  const [analyzing, setAnalyzing] = useState(false)
  const [analysisResult, setAnalysisResult] = useState<AIAnalysisResult | null>(null)
  const [analysisError, setAnalysisError] = useState<string | null>(null)
  const [showHistoryModal, setShowHistoryModal] = useState(false)
  const [analysisHistory, setAnalysisHistory] = useState<{ symbol: string; result: AIAnalysisResult; time: number }[]>(
    () => {
      try {
        const raw = localStorage.getItem('ai-analysis-history')
        if (raw) return JSON.parse(raw)
      } catch {
        /* ignore corrupt data */
      }
      return []
    }
  )

  const [watchlist, setWatchlist] = useState<WatchlistItem[]>(() => {
    try {
      const raw = localStorage.getItem('ai-watchlist')
      if (raw) {
        const parsed = JSON.parse(raw)
        if (Array.isArray(parsed)) return parsed
      }
    } catch {
      /* ignore corrupt data */
    }
    return []
  })
  const [watchlistPrices, setWatchlistPrices] = useState<Record<string, WatchlistPrice>>({})
  const [positionSummaryMap] = useState<Record<string, PositionSummary>>({})

  const [showAddStockModal, setShowAddStockModal] = useState(false)
  const [stockSearchQuery, setStockSearchQuery] = useState('')
  const [stockSearchResults, setStockSearchResults] = useState<string[]>([])

  // Persist watchlist
  useEffect(() => {
    try {
      localStorage.setItem('ai-watchlist', JSON.stringify(watchlist))
    } catch {
      /* ignore storage errors */
    }
  }, [watchlist])

  // Watchlist price polling
  const loadWatchlistPrices = useCallback(async () => {
    if (watchlist.length === 0) return
    try {
      const snapshots = await Promise.allSettled(
        watchlist.map((stock) => {
          const apiSymbol = stock.symbol.replace('/', '')
          return marketApi.snapshot(apiSymbol).then((d) => {
            const t = d as { price: number; change_pct_24h?: number }
            return {
              key: `${stock.market}:${stock.symbol}`,
              price: t.price,
              change: t.change_pct_24h ?? 0,
            }
          })
        })
      )
      const updates: Record<string, WatchlistPrice> = {}
      snapshots.forEach((result) => {
        if (result.status === 'fulfilled') {
          updates[result.value.key] = { price: result.value.price, change: result.value.change }
        }
      })
      if (Object.keys(updates).length > 0) {
        setWatchlistPrices((prev) => ({ ...prev, ...updates }))
      }
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      toast('error', 'Watchlist price fetch failed: ' + err.message)
    }
  }, [watchlist])

  useEffect(() => {
    loadWatchlistPrices()
    const interval = setInterval(loadWatchlistPrices, 30000)
    return () => clearInterval(interval)
  }, [loadWatchlistPrices])

  const handleSymbolChange = useCallback((value: string) => {
    setSelectedSymbol(value)
    setAnalysisResult(null)
    setAnalysisError(null)
  }, [])

  const startFastAnalysis = useCallback(async () => {
    if (!selectedSymbol) return
    setAnalyzing(true)
    setAnalysisError(null)
    setAnalysisResult(null)
    try {
      const symbol = selectedSymbol.split(':').pop() || selectedSymbol
      const data = await aiApi.analyze({ symbol })
      const result: AIAnalysisResult = {
        symbol: data.symbol || symbol,
        consensus: data.consensus || 'neutral',
        analyses: (data.analyses || []).map((a: AIModelAnalysis) => ({
          model: a.model || '',
          name: a.name || a.model || '',
          sentiment: (a.sentiment || 'neutral') as 'bullish' | 'bearish' | 'neutral',
          analysis: a.analysis || a.content || '',
          content: a.content || '',
        })),
      }
      setAnalysisResult(result)
      setAnalysisHistory((prev) => {
        const next = [{ symbol: result.symbol, result, time: Date.now() }, ...prev.slice(0, 49)]
        try {
          localStorage.setItem('ai-analysis-history', JSON.stringify(next))
        } catch {
          /* ignore */
        }
        return next
      })
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      setAnalysisError(err.message || '分析失败')
    } finally {
      setAnalyzing(false)
    }
  }, [selectedSymbol])

  const handleRetry = useCallback(() => {
    startFastAnalysis()
  }, [startFastAnalysis])

  const handleStockSearch = useCallback(async () => {
    const q = stockSearchQuery.trim().toUpperCase()
    if (!q) return
    try {
      const res = await marketApi.symbolSearch(q)
      const symbols = res?.symbols || []
      if (symbols.length > 0) {
        setStockSearchResults(symbols)
        return
      }
    } catch (e) {
      toast('warning', 'Symbol search API failed, falling back to local pool')
    }
    const localPool = [
      'AAPL',
      'MSFT',
      'NVDA',
      'TSLA',
      'GOOGL',
      'AMZN',
      'META',
      'NFLX',
      'CRM',
      'AMD',
      'INTC',
      'BABA',
      'BTC/USDT',
      'ETH/USDT',
      'BNB/USDT',
      'SOL/USDT',
      'XRP/USDT',
      'DOGE/USDT',
      'ADA/USDT',
      'AVAX/USDT',
      'LINK/USDT',
      'MATIC/USDT',
      'DOT/USDT',
      'LTC/USDT',
      '00700',
      '09988',
      '03690',
      '01810',
      '09618',
      '01299',
      'SPX',
      'NDX',
      'DJI',
      'SH',
      'HSI',
      'N225',
      'FTSE',
      'DAX',
      'EUR/USD',
      'GBP/USD',
      'USD/JPY',
      'USD/CNH',
      'Gold',
      'Silver',
      'Crude Oil',
    ]
    const matched = localPool.filter((s) => s.includes(q))
    setStockSearchResults(matched)
  }, [stockSearchQuery])

  const handleLoadHistory = useCallback((item: { symbol: string; result: AIAnalysisResult }) => {
    setSelectedSymbol(item.symbol)
    setAnalysisResult(item.result)
    setShowHistoryModal(false)
  }, [])

  const removeFromWatchlist = useCallback((stock: WatchlistItem) => {
    setWatchlist((prev) => prev.filter((s) => !(s.market === stock.market && s.symbol === stock.symbol)))
  }, [])

  const selectWatchlistItem = useCallback((stock: WatchlistItem) => {
    setSelectedSymbol(`${stock.market}:${stock.symbol}`)
    setAnalysisResult(null)
    setAnalysisError(null)
  }, [])

  const addToWatchlist = useCallback((sym: string) => {
    setWatchlist((prev) => {
      if (prev.some((s) => s.symbol === sym)) return prev
      return [...prev, { market: sym.includes('/') ? 'Crypto' : 'USStock', symbol: sym }]
    })
    setShowAddStockModal(false)
    setStockSearchQuery('')
    setStockSearchResults([])
  }, [])

  const canAnalyze = !!selectedSymbol && !analyzing

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader title="AI 分析" subtitle="多模型 AI 市场分析" icon={<Brain className="w-5 h-5" />} />

        <div className="flex gap-3 min-h-0">
          {/* Center Panel */}
          <div className="flex-1 min-w-0 flex flex-col gap-3">
            <SectionCard title="标的分析">
              <div className="flex items-center gap-3 flex-wrap">
                <div className="relative flex-1 max-w-[320px]">
                  <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
                  <select
                    value={selectedSymbol || ''}
                    onChange={(e) => handleSymbolChange(e.target.value)}
                    className="w-full bg-quant-bg border border-quant-border rounded-lg pl-8 pr-3 py-2 text-xs focus:outline-none focus:border-quant-gold appearance-none"
                  >
                    <option value="">选择标的...</option>
                    {watchlist.map((stock) => (
                      <option key={`${stock.market}:${stock.symbol}`} value={`${stock.market}:${stock.symbol}`}>
                        [{MARKET_NAMES[stock.market] || stock.market}] {stock.symbol}{' '}
                        {stock.name ? `· ${stock.name}` : ''}
                      </option>
                    ))}
                  </select>
                </div>
                <button
                  onClick={startFastAnalysis}
                  disabled={!canAnalyze}
                  className="inline-flex items-center gap-1.5 px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-semibold hover:opacity-90 transition-opacity disabled:opacity-40 disabled:cursor-not-allowed"
                >
                  <Zap className="w-3.5 h-3.5" /> AI 分析
                </button>
                <button
                  onClick={() => setShowHistoryModal(true)}
                  className="inline-flex items-center gap-1.5 px-4 py-2 rounded-lg bg-quant-card border border-quant-border text-foreground text-xs font-medium hover:border-quant-gold/40 transition-colors"
                >
                  <History className="w-3.5 h-3.5" /> 历史
                </button>
                <button
                  onClick={() => setShowAddStockModal(true)}
                  className="inline-flex items-center gap-1.5 px-4 py-2 rounded-lg bg-quant-card border border-quant-border text-foreground text-xs font-medium hover:border-quant-gold/40 transition-colors"
                >
                  <Plus className="w-3.5 h-3.5" /> 添加标的
                </button>
              </div>
            </SectionCard>

            <SectionCard title="分析结果" className="flex-1 min-h-0">
              {!analysisResult && !analyzing && !analysisError ? (
                <AnalysisPlaceholder
                  onAddStock={() => setShowAddStockModal(true)}
                  onAnalyze={startFastAnalysis}
                  canAnalyze={canAnalyze}
                />
              ) : (
                <AnalysisResultView
                  result={analysisResult}
                  loading={analyzing}
                  error={analysisError}
                  onRetry={handleRetry}
                />
              )}
            </SectionCard>
          </div>

          {/* Right Panel */}
          <WatchlistPanel
            watchlist={watchlist}
            watchlistPrices={watchlistPrices}
            positionSummaryMap={positionSummaryMap}
            selectedSymbol={selectedSymbol}
            onSelect={selectWatchlistItem}
            onRemove={removeFromWatchlist}
            onAdd={() => setShowAddStockModal(true)}
          />
        </div>
      </div>

      <AddStockModal
        show={showAddStockModal}
        query={stockSearchQuery}
        results={stockSearchResults}
        onClose={() => setShowAddStockModal(false)}
        onQueryChange={setStockSearchQuery}
        onSearch={handleStockSearch}
        onSelect={addToWatchlist}
        onSelectPopular={addToWatchlist}
      />

      <HistoryModal
        show={showHistoryModal}
        history={analysisHistory}
        onClose={() => setShowHistoryModal(false)}
        onSelect={handleLoadHistory}
      />
    </div>
  )
}
