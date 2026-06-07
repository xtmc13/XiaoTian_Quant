import { useState, useEffect, useMemo, useCallback } from 'react'
import { Zap, BrainCircuit, History, Search } from 'lucide-react'
import { cn } from '@/lib/utils'
import { aiApi, marketApi } from '@/lib/api'
import type { AIAnalysisResult, AIModelAnalysis, MLModelInfo, RLModelInfo } from '@/types'

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
    fearGreed: 52, vix: 18.5, dxy: 103.2,
    indices: [
      { flag: '🇺🇸', symbol: 'SPX', price: 5234.12, change: 0.45 },
      { flag: '🇺🇸', symbol: 'NDX', price: 18342.5, change: 0.82 },
      { flag: '🇺🇸', symbol: 'DJI', price: 39123.8, change: 0.12 },
      { flag: '🇨🇳', symbol: 'SH', price: 3052.3, change: -0.34 },
      { flag: '🇭🇰', symbol: 'HSI', price: 16782.4, change: 0.56 },
      { flag: '🇯🇵', symbol: 'N225', price: 39852.1, change: 1.12 },
      { flag: '🇬🇧', symbol: 'FTSE', price: 7934.2, change: -0.21 },
      { flag: '🇩🇪', symbol: 'DAX', price: 17892.3, change: 0.38 },
    ],
    heatmap: {
      us_stocks: [
        { name: 'AAPL', value: 1.24, price: 178.35 }, { name: 'MSFT', value: 0.86, price: 412.2 },
        { name: 'NVDA', value: 3.45, price: 892.1 }, { name: 'GOOGL', value: -0.34, price: 156.8 },
        { name: 'AMZN', value: 0.92, price: 178.9 }, { name: 'META', value: 1.56, price: 498.2 },
        { name: 'TSLA', value: -2.12, price: 172.4 }, { name: 'AMD', value: 2.34, price: 198.5 },
        { name: 'NFLX', value: -0.78, price: 612.3 }, { name: 'CRM', value: 1.12, price: 298.4 },
        { name: 'INTC', value: -1.45, price: 42.3 }, { name: 'BABA', value: -0.92, price: 78.5 },
      ],
      hk_stocks: [
        { name: '00700', name_cn: '腾讯', value: 1.12, price: 298.4 }, { name: '09988', name_cn: '阿里', value: -0.82, price: 78.5 },
        { name: '03690', name_cn: '美团', value: 2.34, price: 112.3 }, { name: '01810', name_cn: '小米', value: 0.56, price: 16.8 },
        { name: '09618', name_cn: '京东', value: -1.23, price: 112.5 }, { name: '01299', name_cn: '友邦', value: 0.34, price: 56.2 },
        { name: '02318', name_cn: '平安', value: -0.45, price: 38.9 }, { name: '00883', name_cn: '中海油', value: 1.78, price: 12.4 },
        { name: '00939', name_cn: '建行', value: 0.12, price: 4.56 }, { name: '01398', name_cn: '工行', value: -0.23, price: 3.89 },
        { name: '02899', name_cn: '紫金', value: 2.12, price: 14.5 }, { name: '09888', name_cn: '百度', value: -0.67, price: 98.4 },
      ],
      crypto: [
        { name: 'BTC', value: 2.34, price: 67234.5 }, { name: 'ETH', value: 1.56, price: 3521.2 },
        { name: 'SOL', value: 4.12, price: 178.9 }, { name: 'BNB', value: -0.34, price: 612.3 },
        { name: 'XRP', value: 0.78, price: 0.62 }, { name: 'DOGE', value: 3.45, price: 0.18 },
        { name: 'ADA', value: -1.23, price: 0.48 }, { name: 'AVAX', value: 2.89, price: 38.5 },
        { name: 'LINK', value: 1.12, price: 18.9 }, { name: 'MATIC', value: -0.56, price: 0.78 },
        { name: 'DOT', value: 0.92, price: 7.85 }, { name: 'LTC', value: -0.78, price: 82.4 },
      ],
      commodities: [
        { name: 'Gold', name_cn: '黄金', value: 0.45, price: 2345.6 }, { name: 'Silver', name_cn: '白银', value: 1.23, price: 28.4 },
        { name: 'Crude Oil', name_cn: '原油', value: -0.78, price: 78.5 }, { name: 'Brent', name_cn: '布油', value: -0.56, price: 82.3 },
        { name: 'Copper', name_cn: '铜', value: 1.89, price: 4.56 }, { name: 'Natural Gas', name_cn: '天然气', value: -2.34, price: 2.12 },
        { name: 'Wheat', name_cn: '小麦', value: 0.34, price: 612.5 }, { name: 'Corn', name_cn: '玉米', value: -0.12, price: 445.2 },
        { name: 'Coffee', name_cn: '咖啡', value: 2.12, price: 178.5 }, { name: 'Sugar', name_cn: '糖', value: 1.45, price: 19.8 },
        { name: 'Cotton', name_cn: '棉花', value: -0.89, price: 78.4 }, { name: 'Aluminum', name_cn: '铝', value: 0.67, price: 2456.8 },
      ],
      sectors: [
        { name: 'Tech', name_cn: '科技', value: 1.56 }, { name: 'Finance', name_cn: '金融', value: 0.34 },
        { name: 'Health', name_cn: '医疗', value: -0.78 }, { name: 'Energy', name_cn: '能源', value: -1.23 },
        { name: 'Consumer', name_cn: '消费', value: 0.89 }, { name: 'Industry', name_cn: '工业', value: 0.12 },
        { name: 'Materials', name_cn: '材料', value: 1.12 }, { name: 'Utilities', name_cn: '公用', value: -0.34 },
        { name: 'Real Estate', name_cn: '地产', value: -1.56 }, { name: 'Telecom', name_cn: '电信', value: 0.45 },
        { name: 'Auto', name_cn: '汽车', value: 2.34 }, { name: 'Media', name_cn: '传媒', value: -0.67 },
      ],
      forex: [
        { name: 'EUR/USD', value: 0.12, price: 1.0845 }, { name: 'GBP/USD', value: -0.34, price: 1.2634 },
        { name: 'USD/JPY', value: 0.56, price: 151.23 }, { name: 'USD/CHF', value: -0.12, price: 0.9034 },
        { name: 'AUD/USD', value: 0.78, price: 0.6543 }, { name: 'USD/CAD', value: -0.45, price: 1.3567 },
        { name: 'NZD/USD', value: 0.23, price: 0.5987 }, { name: 'EUR/GBP', value: 0.45, price: 0.8589 },
        { name: 'EUR/JPY', value: 0.67, price: 163.98 }, { name: 'GBP/JPY', value: 0.34, price: 190.87 },
        { name: 'USD/CNH', value: -0.23, price: 7.2345 }, { name: 'EUR/CHF', value: 0.12, price: 0.9789 },
      ],
    },
    calendar: [
      { id: '1', date: new Date().toISOString(), time: '08:30', country: 'US', name: '非农就业人口', importance: 'high', actual: 22.5, forecast: 20.0, actual_impact: 'bullish' },
      { id: '2', date: new Date().toISOString(), time: '10:00', country: 'CN', name: '制造业PMI', importance: 'medium', actual: 50.2, forecast: 49.8, actual_impact: 'neutral' },
      { id: '3', date: new Date().toISOString(), time: '14:00', country: 'UK', name: 'GDP月率', importance: 'medium', actual: 0.3, forecast: 0.2, actual_impact: 'bullish' },
      { id: '4', date: new Date().toISOString(), time: '20:30', country: 'US', name: 'CPI月率', importance: 'high', actual: 0.4, forecast: 0.3, actual_impact: 'bearish' },
      { id: '5', date: new Date().toISOString(), time: '22:00', country: 'US', name: '零售销售月率', importance: 'medium', actual: 0.6, forecast: 0.4, actual_impact: 'bullish' },
    ],
  })

  const [heatmapType, setHeatmapType] = useState<HeatmapType>('us_stocks')

  /* -- Watchlist states -- */
  const [watchlist, setWatchlist] = useState<WatchlistItem[]>([
    { market: 'USStock', symbol: 'AAPL', name: 'Apple Inc.', price: 178.35, change: 1.24 },
    { market: 'USStock', symbol: 'NVDA', name: 'NVIDIA Corp.', price: 892.1, change: 3.45 },
    { market: 'USStock', symbol: 'TSLA', name: 'Tesla Inc.', price: 172.4, change: -2.12 },
    { market: 'Crypto', symbol: 'BTC/USDT', name: 'Bitcoin', price: 67234.5, change: 2.34 },
    { market: 'Crypto', symbol: 'ETH/USDT', name: 'Ethereum', price: 3521.2, change: 1.56 },
  ])
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
      // Try to fetch real prices for major indices & crypto via market snapshot
      const snapshotSymbols = [
        { flag: '🇺🇸', symbol: 'BTCUSDT', name: 'BTC' },
        { flag: '🇺🇸', symbol: 'ETHUSDT', name: 'ETH' },
        { flag: '🇺🇸', symbol: 'SOLUSDT', name: 'SOL' },
        { flag: '🇺🇸', symbol: 'BNBUSDT', name: 'BNB' },
        { flag: '🇺🇸', symbol: 'XRPUSDT', name: 'XRP' },
        { flag: '🇺🇸', symbol: 'DOGEUSDT', name: 'DOGE' },
      ]
      const snapshots = await Promise.allSettled(
        snapshotSymbols.map((s) => marketApi.snapshot(s.symbol).then((d) => ({ ...s, price: d.price, change: d.change_pct_24h ?? 0 })))
      )
      const cryptoPrices = snapshots
        .filter((r): r is PromiseFulfilledResult<typeof snapshotSymbols[0] & { price: number; change: number }> => r.status === 'fulfilled')
        .map((r) => r.value)

      setMarketData((prev) => {
        const next = { ...prev }
        // Update crypto heatmap with real prices if available
        if (cryptoPrices.length > 0) {
          next.heatmap = { ...prev.heatmap }
          next.heatmap.crypto = prev.heatmap.crypto.map((item) => {
            const real = cryptoPrices.find((p) => p.name === item.name)
            if (real && real.price > 0) {
              return { ...item, price: real.price, value: real.change }
            }
            return item
          })
        }
        return next
      })
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
          return marketApi.snapshot(apiSymbol).then((d) => ({
            key: `${stock.market}:${stock.symbol}`,
            price: d.price,
            change: d.change_pct_24h ?? 0,
          }))
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

  const handleStockSearch = useCallback(() => {
    const q = stockSearchQuery.trim().toUpperCase()
    if (!q) return
    const all = ['AAPL', 'MSFT', 'NVDA', 'TSLA', 'GOOGL', 'AMZN', 'META', 'BTC/USDT', 'ETH/USDT', 'BNB/USDT', 'SOL/USDT', 'XRP/USDT']
    const matched = all.filter((s) => s.includes(q))
    if (matched.length === 0 && !q.includes('/')) {
      matched.push(q + '/USDT')
    }
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
