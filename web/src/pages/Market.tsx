import { useState, useEffect, useMemo, useCallback } from 'react'
import { Globe, RefreshCw } from 'lucide-react'
import { toast } from '@/lib/useToast'
import { marketApi } from '@/lib/api'
import type { TickerSnapshot } from '@/types'

import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { TopIndexBar } from './AI/components/TopIndexBar'
import { HeatmapSection } from './AI/components/HeatmapSection'
import { EconomicCalendar } from './AI/components/EconomicCalendar'
import { WatchlistPanel } from './AI/components/WatchlistPanel'

import type {
  HeatmapType,
  MarketIndex,
  HeatmapItem,
  CalendarEvent,
  WatchlistItem,
  WatchlistPrice,
  PositionSummary,
} from './AI/types'

const CRYPTO_SYMBOLS = [
  { name: 'BTC', symbol: 'BTCUSDT' },
  { name: 'ETH', symbol: 'ETHUSDT' },
  { name: 'SOL', symbol: 'SOLUSDT' },
  { name: 'BNB', symbol: 'BNBUSDT' },
  { name: 'XRP', symbol: 'XRPUSDT' },
  { name: 'DOGE', symbol: 'DOGEUSDT' },
  { name: 'ADA', symbol: 'ADAUSDT' },
  { name: 'AVAX', symbol: 'AVAXUSDT' },
  { name: 'LINK', symbol: 'LINKUSDT' },
  { name: 'DOT', symbol: 'DOTUSDT' },
  { name: 'LTC', symbol: 'LTCUSDT' },
  { name: 'MATIC', symbol: 'MATICUSDT' },
]

const US_STOCK_SYMBOLS = ['AAPL', 'MSFT', 'NVDA', 'GOOGL', 'AMZN', 'META', 'TSLA', 'NFLX']

const HK_SYMBOLS = [
  { name: '腾讯', symbol: '00700' },
  { name: '阿里', symbol: '09988' },
  { name: '美团', symbol: '03690' },
  { name: '小米', symbol: '01810' },
  { name: '京东', symbol: '09618' },
  { name: '友邦', symbol: '01299' },
  { name: '港交所', symbol: '00388' },
  { name: '快手', symbol: '01024' },
]

const COMMODITY_SYMBOLS = [
  { name: '黄金', symbol: 'GC=F' },
  { name: '白银', symbol: 'SI=F' },
  { name: '原油', symbol: 'CL=F' },
  { name: '天然气', symbol: 'NG=F' },
  { name: '铜', symbol: 'HG=F' },
  { name: '铂金', symbol: 'PL=F' },
]

const SECTOR_SYMBOLS = [
  { name: '科技', symbol: 'XLK' },
  { name: '金融', symbol: 'XLF' },
  { name: '医疗', symbol: 'XLV' },
  { name: '能源', symbol: 'XLE' },
  { name: '消费', symbol: 'XLY' },
  { name: '工业', symbol: 'XLI' },
  { name: '房地产', symbol: 'XLRE' },
  { name: '公用事业', symbol: 'XLU' },
  { name: '材料', symbol: 'XLB' },
  { name: '通信', symbol: 'XLC' },
]

const FOREX_SYMBOLS = [
  { name: 'EUR/USD', symbol: 'EURUSD=X' },
  { name: 'GBP/USD', symbol: 'GBPUSD=X' },
  { name: 'USD/JPY', symbol: 'JPY=X' },
  { name: 'USD/CNH', symbol: 'CNH=X' },
  { name: 'AUD/USD', symbol: 'AUDUSD=X' },
  { name: 'USD/CAD', symbol: 'CAD=X' },
]

function buildHeatmapItems(
  settled: PromiseSettledResult<{ name: string; price: number; value: number }>[]
): HeatmapItem[] {
  return settled
    .filter(
      (r): r is PromiseFulfilledResult<{ name: string; price: number; value: number }> => r.status === 'fulfilled'
    )
    .map((r) => r.value)
    .filter((item) => item.price > 0)
}

function extractIndices(res: unknown): MarketIndex[] {
  if (res && typeof res === 'object' && 'indices' in res && Array.isArray((res as Record<string, unknown>).indices)) {
    return (res as Record<string, unknown>).indices as MarketIndex[]
  }
  return []
}

function extractSentiment(res: unknown): { fearGreed?: number; vix?: number; dxy?: number } {
  if (res && typeof res === 'object' && 'fear_greed' in res) {
    return {
      fearGreed: (res as Record<string, unknown>).fear_greed as number | undefined,
      vix: (res as Record<string, unknown>).vix as number | undefined,
      dxy: (res as Record<string, unknown>).dxy as number | undefined,
    }
  }
  return {}
}

function extractCalendar(res: unknown): CalendarEvent[] {
  if (res && typeof res === 'object' && 'events' in res && Array.isArray((res as Record<string, unknown>).events)) {
    return (res as Record<string, unknown>).events as CalendarEvent[]
  }
  return []
}

export function Market() {
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

  const currentHeatmap = useMemo(() => marketData.heatmap[heatmapType] || [], [marketData.heatmap, heatmapType])

  const loadMarketData = useCallback(async () => {
    setLoadingMarket(true)
    setLoadingSentiment(true)
    setLoadingIndices(true)
    setLoadingHeatmap(true)
    setLoadingCalendar(true)
    try {
      const cryptoSettled = await Promise.allSettled(
        CRYPTO_SYMBOLS.map((s) =>
          marketApi.snapshot(s.symbol).then((d) => ({
            name: s.name,
            price: (d as TickerSnapshot).price ?? 0,
            value: (d as TickerSnapshot).change_pct_24h ?? 0,
          }))
        )
      )
      const cryptoItems = buildHeatmapItems(cryptoSettled)

      const usSettled = await Promise.allSettled(
        US_STOCK_SYMBOLS.map((s) =>
          marketApi.snapshot(`SPX,${s}`).then((d) => {
            const snapshot = d as TickerSnapshot & {
              indices?: Array<{ symbol: string; price: number; change: number }>
            }
            const stockIdx = snapshot?.indices?.find((i: { symbol: string }) => i.symbol === s)
            return {
              name: s,
              price: stockIdx?.price ?? snapshot?.price ?? 0,
              value: stockIdx?.change ?? snapshot?.change_pct_24h ?? 0,
            }
          })
        )
      )
      const usStockItems = buildHeatmapItems(usSettled)

      const hkSettled = await Promise.allSettled(
        HK_SYMBOLS.map((s) =>
          marketApi.snapshot(`HSI,${s.symbol}`).then((d) => {
            const snapshot = d as TickerSnapshot & {
              indices?: Array<{ symbol: string; price: number; change: number }>
            }
            const stockIdx = snapshot?.indices?.find((i: { symbol: string }) => i.symbol === s.symbol)
            return {
              name: s.name,
              price: stockIdx?.price ?? 0,
              value: stockIdx?.change ?? 0,
            }
          })
        )
      )
      const hkItems = buildHeatmapItems(hkSettled)

      const commoditySettled = await Promise.allSettled(
        COMMODITY_SYMBOLS.map((s) =>
          marketApi.snapshot(s.symbol).then((d) => ({
            name: s.name,
            price: (d as TickerSnapshot).price ?? 0,
            value: (d as TickerSnapshot).change_pct_24h ?? 0,
          }))
        )
      )
      const commodityItems = buildHeatmapItems(commoditySettled)

      const sectorSettled = await Promise.allSettled(
        SECTOR_SYMBOLS.map((s) =>
          marketApi.snapshot(s.symbol).then((d) => ({
            name: s.name,
            price: (d as TickerSnapshot).price ?? 0,
            value: (d as TickerSnapshot).change_pct_24h ?? 0,
          }))
        )
      )
      const sectorItems = buildHeatmapItems(sectorSettled)

      const forexSettled = await Promise.allSettled(
        FOREX_SYMBOLS.map((s) =>
          marketApi.snapshot(s.symbol).then((d) => ({
            name: s.name,
            price: (d as TickerSnapshot).price ?? 0,
            value: (d as TickerSnapshot).change_pct_24h ?? 0,
          }))
        )
      )
      const forexItems = buildHeatmapItems(forexSettled)

      setMarketData((prev) => ({
        ...prev,
        heatmap: {
          us_stocks: usStockItems.length > 0 ? usStockItems : prev.heatmap.us_stocks,
          hk_stocks: hkItems.length > 0 ? hkItems : prev.heatmap.hk_stocks,
          crypto: cryptoItems.length > 0 ? cryptoItems : prev.heatmap.crypto,
          commodities: commodityItems.length > 0 ? commodityItems : prev.heatmap.commodities,
          sectors: sectorItems.length > 0 ? sectorItems : prev.heatmap.sectors,
          forex: forexItems.length > 0 ? forexItems : prev.heatmap.forex,
        },
      }))

      try {
        const indicesRes = await marketApi.snapshot('SPX,NDX,DJI,SH,HSI,N225,FTSE,DAX')
        const indices = extractIndices(indicesRes)
        if (indices.length > 0) {
          setMarketData((prev) => ({ ...prev, indices }))
        }
      } catch {
        /* API may not exist */
      }

      try {
        const sentimentRes = await marketApi.snapshot('SENTIMENT')
        const sentiment = extractSentiment(sentimentRes)
        if (sentiment.fearGreed !== undefined) {
          setMarketData((prev) => ({ ...prev, ...sentiment }))
        }
      } catch {
        /* API may not exist */
      }

      try {
        const calendarRes = await marketApi.snapshot('CALENDAR')
        const events = extractCalendar(calendarRes)
        if (events.length > 0) {
          setMarketData((prev) => ({ ...prev, calendar: events }))
        }
      } catch {
        /* API may not exist */
      }
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      toast('error', 'Market data fetch failed: ' + err.message)
    } finally {
      setLoadingSentiment(false)
      setLoadingIndices(false)
      setLoadingHeatmap(false)
      setLoadingCalendar(false)
      setLoadingMarket(false)
    }
  }, [])

  useEffect(() => {
    loadMarketData()
  }, [loadMarketData])

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
      const err = e instanceof Error ? e : new Error(String(e))
      toast('error', 'Watchlist price fetch failed: ' + err.message)
    }
  }, [watchlist])

  useEffect(() => {
    loadWatchlistPrices()
    const interval = setInterval(loadWatchlistPrices, 30000)
    return () => clearInterval(interval)
  }, [loadWatchlistPrices])

  useEffect(() => {
    try {
      localStorage.setItem('ai-watchlist', JSON.stringify(watchlist))
    } catch {
      /* ignore storage errors */
    }
  }, [watchlist])

  const removeFromWatchlist = useCallback((stock: WatchlistItem) => {
    setWatchlist((prev) => prev.filter((s) => !(s.market === stock.market && s.symbol === stock.symbol)))
  }, [])

  const selectWatchlistItem = useCallback((stock: WatchlistItem) => {
    // No-op on Market page; watchlist selection is for navigation elsewhere
    void stock
  }, [])

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader
          title="市场数据"
          subtitle="全球市场热力图、行情指标与财经日历"
          icon={<Globe className="w-5 h-5" />}
          actions={
            <button
              onClick={() => loadMarketData()}
              disabled={loadingMarket}
              className="inline-flex items-center gap-1.5 px-3 py-2 rounded-lg bg-quant-card border border-quant-border text-foreground text-xs font-medium hover:border-quant-gold/40 transition-colors disabled:opacity-50"
            >
              <RefreshCw className={`w-3.5 h-3.5 ${loadingMarket ? 'animate-spin' : ''}`} />
              刷新
            </button>
          }
        />

        <TopIndexBar
          marketData={marketData}
          loadingSentiment={loadingSentiment}
          loadingIndices={loadingIndices}
          onRefresh={() => loadMarketData()}
          loadingMarket={loadingMarket}
        />

        <div className="flex gap-3 min-h-0">
          <div className="hidden md:flex w-[280px] shrink-0 flex-col gap-2.5 overflow-y-auto min-h-0">
            <SectionCard title="热力图">
              <HeatmapSection
                heatmapType={heatmapType}
                setHeatmapType={setHeatmapType}
                currentHeatmap={currentHeatmap}
                loadingHeatmap={loadingHeatmap}
                isDark={false}
              />
            </SectionCard>
            <SectionCard title="财经日历" className="flex-1 flex flex-col min-h-0 overflow-hidden">
              <EconomicCalendar events={marketData.calendar} loadingCalendar={loadingCalendar} />
            </SectionCard>
          </div>

          <div className="flex-1 min-w-0">
            <SectionCard title="行情概览">
              <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-3">
                {marketData.indices.map((idx) => (
                  <div
                    key={idx.symbol}
                    className="flex flex-col items-center p-3 bg-quant-card border border-quant-border rounded-lg"
                  >
                    <span className="text-[10px] text-muted-foreground uppercase tracking-wide">{idx.symbol}</span>
                    <span className="text-sm font-bold text-foreground">{idx.price?.toFixed(2) ?? '--'}</span>
                    <span
                      className={`text-xs font-semibold ${idx.change >= 0 ? 'text-quant-green' : 'text-quant-red'}`}
                    >
                      {idx.change >= 0 ? '+' : ''}
                      {idx.change?.toFixed(2) ?? '--'}%
                    </span>
                  </div>
                ))}
                {marketData.indices.length === 0 && !loadingIndices && (
                  <div className="col-span-full text-center py-8 text-xs text-muted-foreground">暂无指数数据</div>
                )}
                {loadingIndices && (
                  <div className="col-span-full text-center py-8 text-xs text-muted-foreground">加载中...</div>
                )}
              </div>
            </SectionCard>
          </div>

          <WatchlistPanel
            watchlist={watchlist}
            watchlistPrices={watchlistPrices}
            positionSummaryMap={positionSummaryMap}
            selectedSymbol={undefined}
            onSelect={selectWatchlistItem}
            onRemove={removeFromWatchlist}
            onAdd={() => {
              /* No-op on Market page */
            }}
          />
        </div>
      </div>
    </div>
  )
}
