import { useState, useEffect, useMemo, useCallback } from 'react'
import { Zap, BrainCircuit, History, Search } from 'lucide-react'
import { cn } from '@/lib/utils'
import { aiApi, marketApi } from '@/lib/api'
import type { AIAnalysisResult, AIModelAnalysis, MLModelInfo, RLModelInfo, TickerSnapshot } from '@/types'

import { TopIndexBar } from './components/TopIndexBar'
import { HeatmapSection } from './components/HeatmapSection'
import { EconomicCalendar } from './components/EconomicCalendar'
import { AnalysisPlaceholder, AnalysisResultView } from './components/AnalysisPanel'
import { MLPanel } from './components/MLPanel'
import { WatchlistPanel } from './components/WatchlistPanel'
import { AddStockModal, HistoryModal } from './components/Modals'

import { MARKET_NAMES } from './constants'
import type { HeatmapType, MarketIndex, HeatmapItem, CalendarEvent, WatchlistItem, WatchlistPrice, PositionSummary } from './types'

export function AI() {
  /* -- Market data states -- */
  const [loadingMarket, setLoadingMarket] = useState(false)
  const [loadingSentiment, setLoadingSentiment] = useState(false)
  const [loadingIndices, setLoadingIndices] = useState(false)
  const [loadingHeatmap, setLoadingHeatmap] = useState(false)
  const [loadingCalendar, setLoadingCalendar] = useState(false)

  const [marketData, setMarketData] = useState<{
    fearGreed?: number
    vix?: number
    dxy?: number
    indices: MarketIndex[]
    heatmap: Record<HeatmapType, HeatmapItem[]>
    calendar: CalendarEvent[]
  }>({
    fearGreed: undefined,
    vix: undefined,
    dxy: undefined,
    indices: [],
    heatmap: {
      us_stocks: [],
      hk_stocks: [],
      crypto: [],
      commodities: [],
      sectors: [],
      forex: [],
    },
    calendar: [],
  })

  const [heatmapType, setHeatmapType] = useState<HeatmapType>('us_stocks')

  /* -- Watchlist states -- */
  const [watchlist, setWatchlist] = useState<WatchlistItem[]>(() => {
    try {
      const raw = localStorage.getItem('ai-watchlist')
      if (raw) {
        const parsed = JSON.parse(raw)
        if (Array.isArray(parsed)) return parsed
      }
    } catch { /* ignore corrupt data */ }
    return []
  })
  const [watchlistPrices, setWatchlistPrices] = useState<Record<string, WatchlistPrice>>({})
  const [positionSummaryMap, setPositionSummaryMap] = useState<Record<string, PositionSummary>>({}) // eslint-disable-line @typescript-eslint/no-unused-vars
  const [selectedSymbol, setSelectedSymbol] = useState<string | undefined>(undefined)
  const [showAddStockModal, setShowAddStockModal] = useState(false)
  const [stockSearchQuery, setStockSearchQuery] = useState('')
  const [stockSearchResults, setStockSearchResults] = useState<string[]>([])

  /* -- Analysis states -- */
  const [analyzing, setAnalyzing] = useState(false)
  const [analysisResult, setAnalysisResult] = useState<AIAnalysisResult | null>(null)
  const [analysisError, setAnalysisError] = useState<string | null>(null)
  const [showHistoryModal, setShowHistoryModal] = useState(false)
  const [analysisHistory, setAnalysisHistory] = useState<{ symbol: string; result: AIAnalysisResult; time: number }[]>(() => {
    try {
      const raw = localStorage.getItem('ai-analysis-history')
      if (raw) return JSON.parse(raw)
    } catch { /* ignore corrupt data */ }
    return []
  })

  /* -- ML states -- */
  const [mlMode, setMlMode] = useState(false)
  const [mlModels, setMlModels] = useState<MLModelInfo[]>([])
  const [rlModels, setRlModels] = useState<RLModelInfo[]>([])

  const loadRlModels = useCallback(async () => {
    try {
      const { rlApi } = await import('@/lib/api')
      const data = await rlApi.list()
      setRlModels(data || [])
    } catch (e: unknown) {
      console.error('Failed to load RL models:', e)
    }
  }, [])

  /* -- Derived -- */
  const currentHeatmap = useMemo(() => marketData.heatmap[heatmapType] || [], [marketData.heatmap, heatmapType])

  /* -- Actions -- */
  const loadMarketData = useCallback(async (_force = false) => {
    setLoadingMarket(true)
    setLoadingSentiment(true)
    setLoadingIndices(true)
    setLoadingHeatmap(true)
    setLoadingCalendar(true)
    try {
      // Fetch real prices for major crypto via market snapshot
      const snapshotSymbols = [
        { flag: '🇺🇸', symbol: 'BTCUSDT', name: 'BTC' },
        { flag: '🇺🇸', symbol: 'ETHUSDT', name: 'ETH' },
        { flag: '🇺🇸', symbol: 'SOLUSDT', name: 'SOL' },
        { flag: '🇺🇸', symbol: 'BNBUSDT', name: 'BNB' },
        { flag: '🇺🇸', symbol: 'XRPUSDT', name: 'XRP' },
        { flag: '🇺🇸', symbol: 'DOGEUSDT', name: 'DOGE' },
      ]
      const snapshots = await Promise.allSettled(
        snapshotSymbols.map((s) => marketApi.snapshot(s.symbol).then((d) => ({ ...s, price: (d as TickerSnapshot).price, change: (d as TickerSnapshot).change_pct_24h ?? 0 })))
      )
      const cryptoPrices = snapshots
        .filter((r): r is PromiseFulfilledResult<typeof snapshotSymbols[0] & { price: number; change: number }> => r.status === 'fulfilled')
        .map((r) => r.value)

      setMarketData((prev) => {
        const next = { ...prev }
        // Update crypto heatmap with real prices if available
        if (cryptoPrices.length > 0) {
          next.heatmap = { ...prev.heatmap }
          next.heatmap.crypto = cryptoPrices.map((p) => ({
            name: p.name,
            price: p.price,
            value: p.change,
          }))
        }
        return next
      })

      // Attempt to fetch additional market data (indices, sentiment, calendar)
      // These APIs may not exist yet; failures are silently ignored.
      try {
        const indicesRes = await marketApi.snapshot('SPX,NDX,DJI,SH,HSI,N225,FTSE,DAX')
        if (indicesRes && 'indices' in indicesRes && Array.isArray(indicesRes.indices)) {
          setMarketData((prev) => ({ ...prev, indices: indicesRes.indices }))
        }
      } catch { /* API may not exist */ }

      try {
        const sentimentRes = await marketApi.snapshot('SENTIMENT')
        if (sentimentRes && 'fear_greed' in sentimentRes && sentimentRes.fear_greed !== undefined) {
          setMarketData((prev) => ({
            ...prev,
            fearGreed: sentimentRes.fear_greed,
            vix: 'vix' in sentimentRes ? sentimentRes.vix : undefined,
            dxy: 'dxy' in sentimentRes ? sentimentRes.dxy : undefined,
          }))
        }
      } catch { /* API may not exist */ }

      try {
        const calendarRes = await marketApi.snapshot('CALENDAR')
        if (calendarRes && 'events' in calendarRes && Array.isArray(calendarRes.events)) {
          setMarketData((prev) => ({ ...prev, calendar: calendarRes.events }))
        }
      } catch { /* API may not exist */ }
    } catch (e: unknown) {
      console.error('Market data fetch failed:', e)
    } finally {
      setLoadingSentiment(false)
      setLoadingIndices(false)
      setLoadingHeatmap(false)
      setLoadingCalendar(false)
      setLoadingMarket(false)
    }
  }, [])

  useEffect(() => { loadMarketData() }, [loadMarketData])

  /* -- Watchlist price polling -- */
  const loadWatchlistPrices = useCallback(async () => {
    if (watchlist.length === 0) return
    try {
      const snapshots = await Promise.allSettled(
        watchlist.map((stock) => {
          const apiSymbol = stock.symbol.replace('/', '')
          return marketApi.snapshot(apiSymbol).then((d) => {
            const t = d as TickerSnapshot
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
      console.error('Watchlist price fetch failed:', e)
    }
  }, [watchlist])

  useEffect(() => {
    loadWatchlistPrices()
    const interval = setInterval(loadWatchlistPrices, 30000)
    return () => clearInterval(interval)
  }, [loadWatchlistPrices])

  // Persist watchlist to localStorage
  useEffect(() => {
    try {
      localStorage.setItem('ai-watchlist', JSON.stringify(watchlist))
    } catch { /* ignore storage errors */ }
  }, [watchlist])

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
      const result = {
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
        const next = [
          { symbol: result.symbol, result, time: Date.now() },
          ...prev.slice(0, 49),
        ]
        try { localStorage.setItem('ai-analysis-history', JSON.stringify(next)) } catch { /* ignore */ }
        return next
      })
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      setAnalysisError(err.message || '分析失败')
    } finally {
      setAnalyzing(false)
    }
  }, [selectedSymbol])

  const handleRetry = useCallback(() => { startFastAnalysis() }, [startFastAnalysis])

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
      console.warn('Symbol search API failed, falling back to local pool:', e)
    }
    // Fallback: expanded local symbol pool
    const localPool = [
      'AAPL', 'MSFT', 'NVDA', 'TSLA', 'GOOGL', 'AMZN', 'META', 'NFLX', 'CRM', 'AMD', 'INTC', 'BABA',
      'BTC/USDT', 'ETH/USDT', 'BNB/USDT', 'SOL/USDT', 'XRP/USDT', 'DOGE/USDT', 'ADA/USDT', 'AVAX/USDT',
      'LINK/USDT', 'MATIC/USDT', 'DOT/USDT', 'LTC/USDT',
      '00700', '09988', '03690', '01810', '09618', '01299',
      'SPX', 'NDX', 'DJI', 'SH', 'HSI', 'N225', 'FTSE', 'DAX',
      'EUR/USD', 'GBP/USD', 'USD/JPY', 'USD/CNH',
      'Gold', 'Silver', 'Crude Oil',
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

  const loadMlModels = useCallback(async () => {
    try {
      const { mlApi } = await import('@/lib/api')
      const data = await mlApi.list()
      setMlModels(data || [])
    } catch (e: unknown) {
      console.error('Failed to load ML models:', e)
    }
  }, [])

  const toggleMlMode = useCallback(() => {
    const next = !mlMode
    setMlMode(next)
    setAnalysisResult(null)
    setAnalysisError('')
    if (next) {
      loadMlModels()
      loadRlModels()
    }
  }, [mlMode, loadMlModels, loadRlModels])

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
    <div className="h-full flex flex-col">
      <TopIndexBar
        marketData={marketData}
        loadingSentiment={loadingSentiment}
        loadingIndices={loadingIndices}
        onRefresh={() => loadMarketData(true)}
        loadingMarket={loadingMarket}
      />

      <div className="flex-1 flex gap-3 p-3 min-h-0 overflow-hidden">
        {/* Left Panel */}
        <div className="hidden md:flex w-[280px] shrink-0 flex-col gap-2.5 overflow-y-auto min-h-0">
          <HeatmapSection
            heatmapType={heatmapType}
            setHeatmapType={setHeatmapType}
            currentHeatmap={currentHeatmap}
            loadingHeatmap={loadingHeatmap}
            isDark={false}
          />
          <EconomicCalendar events={marketData.calendar} loadingCalendar={loadingCalendar} />
        </div>

        {/* Center Panel */}
        <div className="flex-1 flex flex-col min-w-0 overflow-hidden bg-quant-card border border-quant-border rounded-xl shadow-sm">
          <div className="flex items-center gap-3 px-4 py-3 border-b border-quant-border bg-quant-bg-tertiary rounded-t-xl">
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
                    [{MARKET_NAMES[stock.market] || stock.market}] {stock.symbol} {stock.name ? `· ${stock.name}` : ''}
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
              onClick={toggleMlMode}
              className={cn(
                'inline-flex items-center gap-1.5 px-4 py-2 rounded-lg text-xs font-semibold transition-all',
                mlMode ? 'bg-quant-gold text-white' : 'bg-quant-card border border-quant-border text-foreground hover:border-quant-gold/40'
              )}
            >
              {mlMode ? <><Zap className="w-3.5 h-3.5" /> 返回 AI 分析</>
                      : <><BrainCircuit className="w-3.5 h-3.5" /> ML 预测</>}
            </button>
            <button
              onClick={() => setShowHistoryModal(true)}
              className="inline-flex items-center gap-1.5 px-4 py-2 rounded-lg bg-quant-card border border-quant-border text-foreground text-xs font-medium hover:border-quant-gold/40 transition-colors"
            >
              <History className="w-3.5 h-3.5" /> 历史
            </button>
          </div>

          <div className="flex-1 overflow-auto p-4 min-h-0">
            {mlMode ? (
              <MLPanel selectedSymbol={selectedSymbol} mlModels={mlModels} loadMlModels={loadMlModels} rlModels={rlModels} loadRlModels={loadRlModels} />
            ) : !analysisResult && !analyzing && !analysisError ? (
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
          </div>
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
