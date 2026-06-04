import { useEffect, useRef, useCallback, useMemo, useState } from 'react'
import { useSearchParams, useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { marketApi, orderApi, portfolioApi, tradesApi } from '@/lib/api'
import { KLineChartPro } from '@klinecharts/pro'
import '@klinecharts/pro/dist/klinecharts-pro.css'
import { createBackendDatafeed, handlePriceTick, runBackfill, setChartUpdater, clearChartUpdater } from '@/lib/klineDatafeed'
import type { Chart } from 'klinecharts'
import { cn } from '@/lib/utils'
import { useWebSocket } from '@/hooks/useWebSocket'
import { QuickTradePanel } from '@/components/QuickTradePanel'
import { EmptyState } from '@/components/ui/EmptyState'
import { Skeleton } from '@/components/ui/Skeleton'
import {
  Search,
  ArrowUpRight,
  ArrowDownRight,
  TrendingUp,
  TrendingDown,
  Clock,
  XCircle,
  CheckCircle2,
  AlertCircle,
  Activity,
  BarChart3,
  List,
  Zap,
  ChevronUp,
  ChevronDown,
  Star,
} from 'lucide-react'

const INTERVALS = ['1m', '5m', '15m', '30m', '1h', '4h', '1d', '1w']
const INDICATORS = ['MA', 'EMA', 'BOLL', 'MACD', 'RSI', 'VOL']

function parseInterval(i: string): { multiplier: number; timespan: string } {
  const num = parseInt(i) || 1
  const unit = i.replace(/[0-9]/g, '')
  const map: Record<string, string> = {
    'm': 'minute', 'h': 'hour', 'D': 'day', 'd': 'day', 'w': 'week', 'M': 'month',
  }
  return { multiplier: num, timespan: map[unit] || 'hour' }
}
const WATCHLIST = [
  'BTCUSDT', 'ETHUSDT', 'BNBUSDT', 'SOLUSDT', 'ADAUSDT',
  'DOGEUSDT', 'XRPUSDT', 'AVAXUSDT', 'DOTUSDT', 'LINKUSDT',
  'MATICUSDT', 'LTCUSDT', 'UNIUSDT', 'ATOMUSDT', 'ETCUSDT',
]

/* ─── Types ─── */
interface Kline {
  time: number
  open: number
  high: number
  low: number
  close: number
  volume: number
}

interface Trade {
  id: string
  price: number
  quantity: number
  side: 'buy' | 'sell'
  time: number
}

/* ─── Helpers ─── */
function formatPrice(n: number | string | undefined, digits = 2) {
  if (n === undefined || n === null || n === '') return '--'
  const val = typeof n === 'string' ? parseFloat(n) : n
  if (Number.isNaN(val)) return '--'
  return val.toFixed(digits)
}

function formatTime(ts: number | string) {
  const d = new Date(ts)
  return d.toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function formatDateTime(ts: number | string) {
  const d = new Date(ts)
  return d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function formatVolume(n: number | string | undefined) {
  if (!n) return '--'
  const val = typeof n === 'string' ? parseFloat(n) : n
  if (val >= 1e9) return (val / 1e9).toFixed(2) + 'B'
  if (val >= 1e6) return (val / 1e6).toFixed(2) + 'M'
  if (val >= 1e3) return (val / 1e3).toFixed(2) + 'K'
  return val.toFixed(2)
}

/* ─── Watchlist Item ─── */
function WatchlistItem({
  sym,
  active,
  onClick,
  price,
  changePct,
}: {
  sym: string
  active: boolean
  onClick: () => void
  price?: number
  changePct?: number
}) {
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
        <Star className={cn("w-3 h-3", active ? "fill-quant-gold text-quant-gold" : "text-muted-foreground")} />
        <span className="font-semibold tracking-tight">{sym.replace('USDT', '/USDT')}</span>
      </div>
      <div className="text-right">
        <div className="font-mono font-medium text-foreground">{price ? formatPrice(price) : '--'}</div>
        <div className={cn('font-mono text-[10px]', isUp ? 'text-quant-green' : 'text-quant-red')}>
          {isUp ? '+' : ''}{changePct?.toFixed(2) ?? '--'}%
        </div>
      </div>
    </button>
  )
}

/* ─── Main Page ─── */
export function Trading() {
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [interval, setInterval] = useState('1h')
  const [side, setSide] = useState<'BUY' | 'SELL'>('BUY')
  const [orderType, setOrderType] = useState<'LIMIT' | 'MARKET'>('LIMIT')
  const [price, setPrice] = useState('')
  const [quantity, setQuantity] = useState('')
  const [leverage, setLeverage] = useState(1)
  const [obTab, setObTab] = useState<'book' | 'trades'>('book')
  const [obPrecision, setObPrecision] = useState<string>('0.1')
  const [activeBottomTab, setActiveBottomTab] = useState<"positions" | "orders" | "plans" | "history" | "fills" | "assets">('positions')
  const [bottomHeight, setBottomHeight] = useState(0) // px, 0=collapsed
  const bottomCollapsed = bottomHeight < 20
  const dragRef = useRef<{ startY: number; startH: number } | null>(null)
  const [watchlistSearch, setWatchlistSearch] = useState('')
  const [tpPrice, setTpPrice] = useState('')
  const [slPrice, setSlPrice] = useState('')
  const [searchParams] = useSearchParams()
  const tradeMode = (searchParams.get('mode') as 'contract' | 'spot') || 'spot'
  const navigate = useNavigate()

  const chartRef = useRef<HTMLDivElement>(null)
  const chartApiRef = useRef<Chart | null>(null)
  const klineProRef = useRef<any>(null)
  const datafeed = useMemo(() => createBackendDatafeed(), [])
  const queryClient = useQueryClient()

  /* ─── Data Queries ─── */
  const { data: klines, isLoading: klinesLoading } = useQuery({
    queryKey: ['klines', symbol, interval],
    queryFn: () => marketApi.klines(symbol, interval, 1000),
    refetchInterval: 5000,
  })

  const { data: orderbook, isLoading: obLoading } = useQuery({
    queryKey: ['orderbook', symbol],
    queryFn: () => marketApi.orderBook(symbol, 20),
    refetchInterval: 2000,
  })

  const { data: recentTrades, isLoading: tradesLoading } = useQuery({
    queryKey: ['trades', symbol],
    queryFn: () => marketApi.trades(symbol, 50),
    refetchInterval: 3000,
  })

  const { data: positionsRaw, isLoading: posLoading } = useQuery<any>({
    queryKey: ['positions'],
    queryFn: () => portfolioApi.positions(),
    refetchInterval: 5000,
  })
  const positions = Array.isArray(positionsRaw) ? positionsRaw : positionsRaw?.positions || []

  const { data: orders, isLoading: ordersLoading } = useQuery({
    queryKey: ['orders'],
    queryFn: () => orderApi.list(),
    refetchInterval: 5000,
  })

  const { data: historyOrders, isLoading: historyLoading } = useQuery({
    queryKey: ['orders-history'],
    queryFn: () => orderApi.history({ status: 'filled' }),
    refetchInterval: 10000,
  })

  const { data: snapshot } = useQuery({
    queryKey: ['snapshot', symbol],
    queryFn: () => marketApi.snapshot(symbol),
    refetchInterval: 5000,
  })

  /* ─── WebSocket live trades ─── */
  const { on: wsOn } = useWebSocket('/ws', {
    onReconnect: () => {
      queryClient.invalidateQueries({ queryKey: ['klines', symbol, interval] })
      queryClient.invalidateQueries({ queryKey: ['orderbook', symbol] })
      queryClient.invalidateQueries({ queryKey: ['snapshot', symbol] })
      queryClient.invalidateQueries({ queryKey: ['trades'] })
      runBackfill()
    },
  })
  const [liveTrades, setLiveTrades] = useState<Trade[]>([])

  useEffect(() => {
    const unsub = wsOn('trade', (data: any) => {
      if (data.symbol === symbol) {
        setLiveTrades((prev) => [
          { id: String(data.id || Date.now()), price: data.price, quantity: data.quantity, side: data.side, time: data.time || Date.now() },
          ...prev.slice(0, 99),
        ])
      }
    })
    return unsub
  }, [wsOn, symbol])

  /* ─── Pipe WS price ticks to K-line datafeed ─── */
  useEffect(() => {
    const unsub = wsOn('price', (msg: any) => {
      if (msg.symbol) {
        handlePriceTick(
          msg.symbol,
          Number(msg.data?.last ?? msg.data?.price ?? 0),
          Number(msg.data?.volume ?? 0),
        )
      }
    })
    return unsub
  }, [wsOn])

  /* ─── Computed ─── */
  const lastPrice = useMemo(() => {
    if (snapshot?.price) return parseFloat(String(snapshot.price))
    if (klines?.length) return parseFloat(klines[klines.length - 1].close)
    return 0
  }, [snapshot, klines])

  const prevClose = useMemo(() => {
    if (klines && klines.length > 1) return parseFloat(klines[klines.length - 2].close)
    return lastPrice
  }, [klines, lastPrice])

  const change = lastPrice - prevClose
  const changePct = prevClose ? (change / prevClose) * 100 : 0
  const isUp = change >= 0

  const bestBid = orderbook?.bids?.[0]?.[0] ?? ''
  const bestAsk = orderbook?.asks?.[0]?.[0] ?? ''

  const filteredWatchlist = useMemo(() => {
    if (!watchlistSearch.trim()) return WATCHLIST
    const q = watchlistSearch.toUpperCase()
    return WATCHLIST.filter((s) => s.includes(q))
  }, [watchlistSearch])

  /* ─── KLineChartPro init ─── */
  const initChart = useCallback(() => {
    if (!chartRef.current) return
    if (klineProRef.current) {
      chartRef.current.innerHTML = ""
      klineProRef.current = null
      chartApiRef.current = null
    }

    let intervalId: number | null = null
    try {
      const chart = new KLineChartPro({
        container: chartRef.current,
        symbol: { ticker: symbol, name: symbol.replace("USDT", "/USDT"), shortName: symbol, market: "crypto", exchange: "BINANCE" },
        period: { ...parseInterval(interval), text: interval },
        periods: INTERVALS.map((i) => ({ ...parseInterval(i), text: i })),
        datafeed, drawingBarVisible: true,
        mainIndicators: ["MA", "EMA"], subIndicators: ["VOL", "MACD"],
        theme: "dark", locale: "zh-CN",
      })
      klineProRef.current = chart

      const checkApi = () => {
        if ((chart as any)._chartApi) {
          const api = (chart as any)._chartApi
          chartApiRef.current = api
          try { api.scrollToRealTime() } catch (_) {}
          try { api.setBarSpace(4) } catch (_) {}
          if (typeof api.updateData === 'function') {
            setChartUpdater((bar) => { try { api.updateData(bar) } catch {} })
          }
        } else {
          intervalId = window.setTimeout(checkApi, 100)
        }
      }
      checkApi()
    } catch (e) { console.error("[Trading] KLineChartPro init failed:", e) }
    return () => { if (intervalId) window.clearTimeout(intervalId) }
  }, [datafeed, symbol, interval])

  useEffect(() => {
    const cancelTimer = initChart()
    return () => { cancelTimer?.(); cleanupChart() }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initChart])

  function cleanupChart() {
    clearChartUpdater()
    if (chartRef.current) chartRef.current.innerHTML = ""
    klineProRef.current = null
    chartApiRef.current = null
  }

  /* ─── Period click observer ─── */
  useEffect(() => {
    const el = chartRef.current
    if (!el) return
    const handleClick = (e: MouseEvent) => {
      let t = e.target as HTMLElement | null
      while (t && t !== el) {
        if (t.classList?.contains("period") && t.parentElement?.classList?.contains("klinecharts-pro-period-bar")) {
          const txt = t.textContent?.trim()
          if (txt && INTERVALS.includes(txt)) {
            setInterval(txt)
          }
          return
        }
        t = t.parentElement
      }
    }
    el.addEventListener("click", handleClick, true)
    return () => el.removeEventListener("click", handleClick, true)
  }, [])

  /* ─── Resize chart when bottom panel toggles ─── */
  useEffect(() => {
    if (!klineProRef.current) return
    const t = setTimeout(() => window.dispatchEvent(new Event("resize")), 100)
    return () => clearTimeout(t)
  }, [bottomCollapsed])

  /* ─── Order Handlers ─── */
  const handlePlaceOrder = useCallback(async () => {
    try {
      await orderApi.place({
        symbol,
        side,
        type: orderType,
        price: orderType === 'MARKET' ? 0 : parseFloat(price),
        quantity: parseFloat(quantity),
        leverage: tradeMode === 'contract' ? leverage : undefined,
        tp_price: tpPrice ? parseFloat(tpPrice) : undefined,
        sl_price: slPrice ? parseFloat(slPrice) : undefined,
      })
      alert('订单已提交')
      setQuantity('')
    } catch (e) {
      alert('下单失败')
    }
  }, [symbol, side, orderType, price, quantity, leverage, tradeMode, tpPrice, slPrice])

  const handleCancelOrder = useCallback(async (id: string) => {
    try {
      await orderApi.cancel(id)
    } catch (e) {
      alert('取消失败')
    }
  }, [])

  /* ─── Orderbook helpers ─── */
  const obMax = useMemo(() => {
    if (!orderbook) return 1
    const bidMax = Math.max(...(orderbook.bids || []).map((b: any[]) => parseFloat(b[1]) || 0), 0)
    const askMax = Math.max(...(orderbook.asks || []).map((a: any[]) => parseFloat(a[1]) || 0), 0)
    return Math.max(bidMax, askMax, 1)
  }, [orderbook])

  const displayTrades = useMemo(() => {
    const src = liveTrades.length ? liveTrades : (recentTrades || [])
    return src.slice(0, 50)
  }, [liveTrades, recentTrades])

  /* ─── Position preview calc ─── */
  const positionPreview = useMemo(() => {
    const qty = parseFloat(quantity) || 0
    const pr = orderType === 'MARKET' ? lastPrice : (parseFloat(price) || lastPrice)
    const notional = qty * pr
    const margin = tradeMode === 'contract' && leverage > 0 ? notional / leverage : notional
    const feeRate = 0.0005
    const fee = notional * feeRate
    let maxLoss = 0
    if (slPrice && parseFloat(slPrice) > 0) {
      const sl = parseFloat(slPrice)
      maxLoss = Math.abs(qty * (pr - sl))
    }
    return { notional, margin, fee, maxLoss }
  }, [quantity, price, lastPrice, orderType, leverage, tradeMode, slPrice])

  /* ─── Render ─── */
  return (
    <div className="h-full flex flex-col bg-quant-bg text-foreground">

      {/* ════════════════════════════════════════
          TOP: Ticker Bar (币安风格行情栏)
      ════════════════════════════════════════ */}
      <div className="h-11 shrink-0 border-b border-quant-border bg-quant-bg-secondary flex items-center px-4 gap-4 select-none">
        {/* Symbol */}
        <div className="flex items-center gap-2">
          <div className="w-6 h-6 rounded-full bg-quant-gold/20 flex items-center justify-center text-[10px] font-bold text-quant-gold">
            {symbol.slice(0, 1)}
          </div>
          <div className="flex flex-col">
            <span className="text-sm font-bold leading-none">{symbol.replace('USDT', '/USDT')}</span>
            <span className="text-[10px] text-muted-foreground leading-none mt-0.5">Bitcoin</span>
          </div>
        </div>

        <div className="h-6 w-px bg-quant-border mx-1" />

        {/* Price */}
        <div className="flex items-baseline gap-2">
          <span className={cn("text-lg font-mono font-bold", isUp ? "text-quant-green" : "text-quant-red")}>
            {lastPrice ? lastPrice.toFixed(2) : '--'}
          </span>
          <span className={cn("text-xs font-mono", isUp ? "text-quant-green" : "text-quant-red")}>
            {isUp ? '+' : ''}{changePct.toFixed(2)}%
          </span>
          {isUp ? <ArrowUpRight className="w-3.5 h-3.5 text-quant-green" /> : <ArrowDownRight className="w-3.5 h-3.5 text-quant-red" />}
        </div>

        <div className="h-6 w-px bg-quant-border mx-1" />

        {/* 24h Stats */}
        <div className="flex items-center gap-4 text-[11px]">
          <div className="flex flex-col leading-tight">
            <span className="text-muted-foreground text-[10px]">24h高</span>
            <span className="font-mono text-foreground">{snapshot?.high ? formatPrice(snapshot.high) : '--'}</span>
          </div>
          <div className="flex flex-col leading-tight">
            <span className="text-muted-foreground text-[10px]">24h低</span>
            <span className="font-mono text-foreground">{snapshot?.low ? formatPrice(snapshot.low) : '--'}</span>
          </div>
          <div className="flex flex-col leading-tight">
            <span className="text-muted-foreground text-[10px]">24h量</span>
            <span className="font-mono text-foreground">{snapshot?.volume ? formatVolume(snapshot.volume) : '--'}</span>
          </div>
          <div className="flex flex-col leading-tight">
            <span className="text-muted-foreground text-[10px]">24h额</span>
            <span className="font-mono text-foreground">{snapshot?.quoteVolume ? formatVolume(snapshot.quoteVolume) : '--'} USDT</span>
          </div>
          {tradeMode === 'contract' && (
            <>
              <div className="flex flex-col leading-tight">
                <span className="text-muted-foreground text-[10px]">资金费率</span>
                <span className="font-mono text-quant-gold">0.01%</span>
              </div>
              <div className="flex flex-col leading-tight">
                <span className="text-muted-foreground text-[10px]">标记价格</span>
                <span className="font-mono text-foreground">{lastPrice ? lastPrice.toFixed(2) : '--'}</span>
              </div>
            </>
          )}
        </div>
      </div>

      {/* ════════════════════════════════════════
          MAIN: Orderbook | Chart | Trade Panel
      ════════════════════════════════════════ */}
      <div className="flex-1 flex min-h-0">

        {/* ─── LEFT: Orderbook (280px) ─── */}
        <div className="w-[280px] shrink-0 border-r border-quant-border bg-quant-bg-secondary flex flex-col">
          {/* OB Header */}
          <div className="h-8 shrink-0 border-b border-quant-border flex items-center justify-between px-3">
            <div className="flex gap-3">
              <button
                onClick={() => setObTab('book')}
                className={cn("text-xs font-medium transition-colors", obTab === 'book' ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
              >
                订单簿
              </button>
              <button
                onClick={() => setObTab('trades')}
                className={cn("text-xs font-medium transition-colors", obTab === 'trades' ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
              >
                最新成交
              </button>
            </div>
            <div className="flex gap-1">
              {["0.1","1","10"].map((p) => (
                <span
                  key={p}
                  className={cn(
                    "text-[10px] px-1 py-0.5 rounded cursor-pointer transition-colors",
                    obPrecision === p ? "bg-quant-hover text-foreground" : "text-muted-foreground hover:text-foreground"
                  )}
                  onClick={() => setObPrecision(p)}
                >
                  {p}
                </span>
              ))}
            </div>
          </div>

          {/* OB Column Headers */}
          <div className="flex text-[10px] text-muted-foreground px-3 py-1 border-b border-quant-border shrink-0">
            <span className="flex-1">价格(USDT)</span>
            <span className="flex-1 text-right">数量</span>
            <span className="flex-1 text-right">累计</span>
          </div>

          {obTab === 'book' ? (
            <>
              {/* Asks (red, top, reversed) */}
              <div className="flex-1 overflow-hidden flex flex-col-reverse min-h-0">
                {obLoading ? (
                  <div className="p-2 space-y-1">
                    {Array.from({ length: 8 }).map((_, i) => <Skeleton key={i} variant="text" height={16} />)}
                  </div>
                ) : (
                  (orderbook?.asks || []).slice(0, 10).map((ask: any[], i: number) => {
                    const p = parseFloat(ask[0])
                    const q = parseFloat(ask[1])
                    const total = p * q
                    return (
                      <div key={"ask-" + i} className="relative flex px-3 py-[3px] text-[11px] font-mono cursor-pointer hover:bg-white/[0.04]">
                        <div className="absolute top-0 bottom-0 right-0 opacity-15 z-0" style={{ background: "#F6465D", width: Math.min((q / obMax) * 100, 100) + "%" }} />
                        <span className="flex-1 text-quant-red relative z-10">{p.toFixed(2)}</span>
                        <span className="flex-1 text-right text-foreground relative z-10">{q.toFixed(4)}</span>
                        <span className="flex-1 text-right text-muted-foreground relative z-10">{total.toFixed(2)}</span>
                      </div>
                    )
                  })
                )}
              </div>

              {/* Middle Price */}
              <div className="h-9 shrink-0 flex items-center justify-center border-y border-quant-border bg-quant-bg">
                <span className={cn("text-base font-bold font-mono", isUp ? "text-quant-green" : "text-quant-red")}>
                  {lastPrice ? lastPrice.toFixed(2) : "--"}
                </span>
                <span className="text-[10px] text-muted-foreground ml-2">
                  ≈ ${lastPrice ? lastPrice.toFixed(2) : "--"}
                </span>
                {bestAsk && bestBid && (
                  <span className="text-[10px] text-muted-foreground ml-3">
                    Spread {(parseFloat(bestAsk) - parseFloat(bestBid)).toFixed(2)}
                  </span>
                )}
              </div>

              {/* Bids (green, bottom) */}
              <div className="flex-1 overflow-hidden min-h-0">
                {obLoading ? (
                  <div className="p-2 space-y-1">
                    {Array.from({ length: 8 }).map((_, i) => <Skeleton key={i} variant="text" height={16} />)}
                  </div>
                ) : (
                  (orderbook?.bids || []).slice(0, 10).map((bid: any[], i: number) => {
                    const p = parseFloat(bid[0])
                    const q = parseFloat(bid[1])
                    const total = p * q
                    return (
                      <div key={"bid-" + i} className="relative flex px-3 py-[3px] text-[11px] font-mono cursor-pointer hover:bg-white/[0.04]">
                        <div className="absolute top-0 bottom-0 left-0 opacity-15 z-0" style={{ background: "#0ECB81", width: Math.min((q / obMax) * 100, 100) + "%" }} />
                        <span className="flex-1 text-quant-green relative z-10">{p.toFixed(2)}</span>
                        <span className="flex-1 text-right text-foreground relative z-10">{q.toFixed(4)}</span>
                        <span className="flex-1 text-right text-muted-foreground relative z-10">{total.toFixed(2)}</span>
                      </div>
                    )
                  })
                )}
              </div>
            </>
          ) : (
            <div className="flex-1 overflow-y-auto">
              <div className="flex text-[10px] text-muted-foreground px-3 py-1 border-b border-quant-border sticky top-0 bg-quant-bg-secondary">
                <span className="flex-1">时间</span>
                <span className="flex-1 text-right">价格</span>
                <span className="flex-1 text-right">数量</span>
              </div>
              {displayTrades.slice(0, 50).map((t, i) => (
                <div key={t.id || i} className="flex px-3 py-[3px] text-[11px] font-mono">
                  <span className="flex-1 text-muted-foreground">{formatTime(t.time)}</span>
                  <span className={cn("flex-1 text-right", t.side === "buy" ? "text-quant-green" : "text-quant-red")}>
                    {formatPrice(t.price)}
                  </span>
                  <span className="flex-1 text-right text-foreground">{t.quantity.toFixed(4)}</span>
                </div>
              ))}
              {!displayTrades.length && (
                <div className="py-4"><EmptyState title="暂无成交" description="等待数据..." /></div>
              )}
            </div>
          )}

          {/* Mini Recent Trades (always visible below orderbook) */}
          <div className="h-[130px] shrink-0 border-t border-quant-border overflow-y-auto">
            <div className="flex text-[10px] text-muted-foreground px-3 py-1 border-b border-quant-border sticky top-0 bg-quant-bg-secondary">
              <span className="flex-1">时间</span>
              <span className="flex-1 text-right">价格</span>
              <span className="flex-1 text-right">数量</span>
            </div>
            {displayTrades.slice(0, 20).map((t, i) => (
              <div key={t.id || i} className="flex px-3 py-[3px] text-[11px] font-mono">
                <span className="flex-1 text-muted-foreground">{formatTime(t.time)}</span>
                <span className={cn("flex-1 text-right", t.side === "buy" ? "text-quant-green" : "text-quant-red")}>
                  {formatPrice(t.price)}
                </span>
                <span className="flex-1 text-right text-foreground">{t.quantity.toFixed(4)}</span>
              </div>
            ))}
            {!displayTrades.length && (
              <div className="py-4 text-center text-muted-foreground text-[10px]">等待实时成交数据...</div>
            )}
          </div>
        </div>

        {/* ─── CENTER: Chart (flex-1) ─── */}
        <div className="flex-1 flex flex-col min-w-0 border-r border-quant-border bg-quant-bg">
          {/* Chart Toolbar */}
          <div className="h-9 shrink-0 border-b border-quant-border bg-quant-bg-secondary flex items-center px-3 gap-1">
            {INTERVALS.map((i) => (
              <button
                key={i}
                onClick={() => setInterval(i)}
                className={cn(
                  "px-2 py-1 text-[11px] rounded transition-colors",
                  interval === i ? "bg-quant-hover text-foreground font-medium" : "text-muted-foreground hover:text-foreground"
                )}
              >
                {i}
              </button>
            ))}
            <div className="w-px h-4 bg-quant-border mx-1" />
            {INDICATORS.map((ind) => (
              <button key={ind} className="px-2 py-1 text-[11px] text-muted-foreground hover:text-foreground transition-colors">
                {ind}
              </button>
            ))}
            <div className="ml-auto flex items-center gap-2 text-[11px] text-muted-foreground">
              <span className="hover:text-foreground cursor-pointer">TradingView</span>
              <span className="hover:text-foreground cursor-pointer text-foreground font-medium">基本版</span>
              <span className="hover:text-foreground cursor-pointer">深度图</span>
            </div>
          </div>
          <div ref={chartRef} className="flex-1 min-h-0" />
        </div>

        {/* ─── RIGHT: Trade Panel (320px) ─── */}
        <div className="w-[320px] shrink-0 flex flex-col bg-quant-bg-secondary overflow-y-auto">
          {/* Spot / Contract Toggle */}
          <div className="flex border-b border-quant-border shrink-0">
            <button
              onClick={() => navigate("/trading?mode=spot", { replace: true })}
              className={cn(
                "flex-1 py-2.5 text-xs font-medium border-b-2 transition-colors",
                tradeMode === 'spot' ? "border-quant-gold text-quant-gold" : "border-transparent text-muted-foreground hover:text-foreground"
              )}
            >
              现货
            </button>
            <button
              onClick={() => navigate("/trading?mode=contract", { replace: true })}
              className={cn(
                "flex-1 py-2.5 text-xs font-medium border-b-2 transition-colors",
                tradeMode === 'contract' ? "border-quant-gold text-quant-gold" : "border-transparent text-muted-foreground hover:text-foreground"
              )}
            >
              合约
            </button>
          </div>

          {/* Watchlist (Spot only) */}
          {tradeMode === 'spot' && (
            <div className="h-[220px] shrink-0 border-b border-quant-border flex flex-col">
              <div className="h-8 flex items-center px-3 border-b border-quant-border justify-between">
                <span className="text-xs font-medium text-muted-foreground">自选</span>
                <div className="relative">
                  <Search className="w-3 h-3 absolute left-2 top-1/2 -translate-y-1/2 text-muted-foreground" />
                  <input
                    value={watchlistSearch}
                    onChange={(e) => setWatchlistSearch(e.target.value)}
                    placeholder="搜索"
                    className="w-24 h-5 pl-6 pr-2 text-[10px] bg-quant-bg border border-quant-border rounded focus:outline-none focus:border-quant-gold text-foreground placeholder:text-muted-foreground"
                  />
                </div>
              </div>
              <div className="flex-1 overflow-y-auto">
                {filteredWatchlist.map((sym) => (
                  <WatchlistItem
                    key={sym}
                    sym={sym}
                    active={sym === symbol}
                    onClick={() => setSymbol(sym)}
                    price={snapshot?.price ? parseFloat(String(snapshot.price)) : undefined}
                    changePct={changePct}
                  />
                ))}
              </div>
            </div>
          )}

          {/* Trade Form */}
          <div className="flex-1 p-3">
            <QuickTradePanel
              symbol={symbol}
              side={side}
              orderType={orderType}
              bestBid={bestBid}
              bestAsk={bestAsk}
              lastPrice={lastPrice}
              leverage={leverage}
              tradeMode={tradeMode}
              tpPrice={tpPrice}
              slPrice={slPrice}
              onSideChange={setSide}
              onOrderTypeChange={setOrderType}
              onPlaceOrder={handlePlaceOrder}
              onLeverageChange={setLeverage}
              onTradeModeChange={function (m) { navigate("/trading?mode=" + m, { replace: true }); }}
              onTpChange={setTpPrice}
              onSlChange={setSlPrice}
              price={price}
              quantity={quantity}
              onPriceChange={setPrice}
              onQuantityChange={setQuantity}
              preview={positionPreview}
            />
          </div>
        </div>
      </div>

      {/* ════════════════════════════════════════
          BOTTOM: Positions / Orders / History
      ════════════════════════════════════════ */}
      <div
        className="shrink-0 border-t border-quant-border bg-quant-bg-secondary flex flex-col"
        style={{ height: bottomCollapsed ? 'auto' : bottomHeight }}
      >
        {/* Drag Handle */}
        <div
          className="h-1.5 cursor-row-resize hover:bg-quant-gold/20 active:bg-quant-gold/30 shrink-0 relative"
          onMouseDown={(e) => {
            dragRef.current = { startY: e.clientY, startH: bottomHeight }
            const onMove = (ev: MouseEvent) => {
              if (!dragRef.current) return
              const h = Math.max(60, Math.min(600, dragRef.current.startH - (ev.clientY - dragRef.current.startY)))
              setBottomHeight(h)
            }
            const onUp = () => {
              dragRef.current = null
              document.removeEventListener('mousemove', onMove)
              document.removeEventListener('mouseup', onUp)
            }
            document.addEventListener('mousemove', onMove)
            document.addEventListener('mouseup', onUp)
          }}
        >
          <div className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 w-8 h-0.5 rounded bg-quant-border/60" />
        </div>

        {/* Bottom Tabs */}
        <div className="flex border-b border-quant-border px-2 items-center justify-between shrink-0">
          <div className="flex">
            {([
              { key: 'positions', label: '持仓', count: positions?.length || 0, icon: TrendingUp },
              { key: 'orders', label: '当前委托', count: orders?.length || 0, icon: Clock },
              { key: 'plans', label: '计划委托', count: 0, icon: AlertCircle },
              { key: 'history', label: '历史委托', count: historyOrders?.length || 0, icon: XCircle },
              { key: 'fills', label: '成交记录', count: 0, icon: CheckCircle2 },
              { key: 'assets', label: '资产', count: 0, icon: Activity },
            ] as const).map((t) => (
              <button
                key={t.key}
                onClick={() => { setActiveBottomTab(t.key); setBottomHeight((h) => Math.max(h, 180)) }}
                className={cn(
                  'px-4 py-2 text-xs font-medium transition-colors relative flex items-center gap-1.5',
                  activeBottomTab === t.key ? 'text-quant-gold' : 'text-muted-foreground hover:text-foreground'
                )}
              >
                <t.icon className="w-3.5 h-3.5" />
                {t.label}
                {t.count > 0 && (
                  <span className={cn(
                    'ml-1 px-1.5 py-0 rounded-full text-[10px] font-bold',
                    activeBottomTab === t.key ? 'bg-quant-gold/20 text-quant-gold' : 'bg-quant-bg-tertiary text-muted-foreground'
                  )}>
                    {t.count}
                  </span>
                )}
                {activeBottomTab === t.key && <span className="absolute bottom-0 left-0 right-0 h-0.5 bg-quant-gold" />}
              </button>
            ))}
          </div>
          <button
            onClick={() => setBottomHeight((h) => h < 20 ? 180 : 0)}
            className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5 transition-colors"
            title={bottomCollapsed ? '展开' : '收起'}
          >
            {bottomCollapsed ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
          </button>
        </div>

        {/* Bottom Content */}
        {!bottomCollapsed && (
          <div className="overflow-y-auto flex-1" style={{ maxHeight: bottomHeight - 40 }}>
            {/* ── Positions Table ── */}
            {activeBottomTab === 'positions' && (
              <div className="overflow-x-auto">
                {posLoading ? (
                  <div className="p-4 space-y-2">
                    {Array.from({ length: 3 }).map((_, i) => (<Skeleton key={i} variant="text" height={32} />))}
                  </div>
                ) : positions?.length ? (
                  <table className="w-full text-[11px] whitespace-nowrap">
                    <thead className="sticky top-0 bg-quant-bg-secondary z-10">
                      <tr className="text-muted-foreground border-b border-quant-border">
                        <th className="text-left font-medium px-3 py-2">合约</th>
                        <th className="text-left font-medium px-3 py-2">数量</th>
                        <th className="text-right font-medium px-3 py-2">开仓价</th>
                        <th className="text-right font-medium px-3 py-2">标记价</th>
                        <th className="text-right font-medium px-3 py-2">强平价</th>
                        <th className="text-right font-medium px-3 py-2">保证金</th>
                        <th className="text-right font-medium px-3 py-2">未实现盈亏</th>
                        <th className="text-right font-medium px-3 py-2">收益率</th>
                      </tr>
                    </thead>
                    <tbody>
                      {positions.map(function (pos, i) {
                        var isLong = (pos.side || '').toUpperCase() === 'LONG' || (pos.side || '').toUpperCase() === 'BUY';
                        var entryPx = parseFloat(pos.entryPrice || pos.openPrice || pos.avgPrice || 0);
                        var markPx = lastPrice || 0;
                        var qty = parseFloat(pos.quantity || pos.amount || 0);
                        var margin = parseFloat(pos.margin || pos.positionMargin || 0);
                        var upnl = isLong ? (markPx - entryPx) * qty : (entryPx - markPx) * qty;
                        var upnlPct = margin > 0 ? (upnl / margin) * 100 : 0;
                        var liqPx = parseFloat(pos.liquidationPrice || pos.liquidation || 0);
                        return (
                          <tr key={pos.id || i} className="border-b border-quant-border/40 hover:bg-white/[0.03] transition-colors">
                            <td className="px-3 py-2.5 font-medium">{pos.symbol || symbol} 永续</td>
                            <td className="px-3 py-2.5">
                              <span className={cn("inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-bold", isLong ? "bg-quant-green/10 text-quant-green" : "bg-quant-red/10 text-quant-red")}>
                                <span className={cn("w-1.5 h-1.5 rounded-full", isLong ? "bg-quant-green" : "bg-quant-red")} />
                                {isLong ? '多' : '空'} {qty.toFixed(3)}
                              </span>
                            </td>
                            <td className="px-3 py-2.5 text-right font-mono">{entryPx > 0 ? entryPx.toFixed(2) : '--'}</td>
                            <td className="px-3 py-2.5 text-right font-mono">{markPx > 0 ? markPx.toFixed(2) : '--'}</td>
                            <td className="px-3 py-2.5 text-right font-mono text-quant-red">{liqPx > 0 ? liqPx.toFixed(2) : '--'}</td>
                            <td className="px-3 py-2.5 text-right font-mono">{margin > 0 ? margin.toFixed(2) : '--'} USDT</td>
                            <td className={cn("px-3 py-2.5 text-right font-mono", upnl >= 0 ? "text-quant-green" : "text-quant-red")}>{upnl >= 0 ? '+' : ''}{upnl.toFixed(2)} USDT</td>
                            <td className={cn("px-3 py-2.5 text-right font-mono", upnlPct >= 0 ? "text-quant-green" : "text-quant-red")}>{upnlPct >= 0 ? '+' : ''}{upnlPct.toFixed(2)}%</td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                ) : (
                  <div className="py-8 text-center text-muted-foreground text-xs">暂无持仓</div>
                )}
              </div>
            )}

            {activeBottomTab === 'orders' && (
              <div>
                {ordersLoading ? (
                  <div className="p-4 space-y-2">
                    {Array.from({ length: 4 }).map((_, i) => (
                      <Skeleton key={i} variant="text" height={32} />
                    ))}
                  </div>
                ) : orders?.length ? (
                  <div className="overflow-x-auto">
                    <table className="w-full text-[11px] whitespace-nowrap">
                      <thead className="sticky top-0 bg-quant-bg-secondary z-10">
                        <tr className="text-muted-foreground text-left">
                          <th className="px-1.5 py-1 font-medium">时间</th>
                          <th className="px-1.5 py-1 font-medium">币种</th>
                          <th className="px-1.5 py-1 font-medium">方向</th>
                          <th className="px-1.5 py-1 font-medium">类型</th>
                          <th className="px-1.5 py-1 font-medium">价格</th>
                          <th className="px-1.5 py-1 font-medium">数量</th>
                          <th className="px-1.5 py-1 font-medium">状态</th>
                          <th className="px-1.5 py-1 font-medium">操作</th>
                        </tr>
                      </thead>
                      <tbody>
                        {(orders || []).map((o: any) => (
                          <tr key={o.id} className="border-t border-quant-border/40 hover:bg-white/[0.02] transition-colors">
                            <td className="px-1.5 py-1 text-muted-foreground">{formatDateTime(o.created_at)}</td>
                            <td className="px-1.5 py-1 font-semibold">{o.symbol}</td>
                            <td className="px-1.5 py-1">
                              <span className={cn('text-[9px] font-bold', o.side === 'BUY' ? 'text-quant-green' : 'text-quant-red')}>
                                {o.side === 'BUY' ? '买入' : '卖出'}
                              </span>
                            </td>
                            <td className="px-1.5 py-1 text-muted-foreground">{o.type}</td>
                            <td className="px-1.5 py-1 font-mono">${formatPrice(o.price, 2)}</td>
                            <td className="px-1.5 py-1 font-mono">{formatPrice(o.quantity, 4)}</td>
                            <td className="px-1.5 py-1">
                              <StatusTag status={o.status} />
                            </td>
                            <td className="px-1.5 py-1">
                              <button
                                onClick={() => handleCancelOrder(o.id)}
                                className="px-1.5 py-0.5 bg-quant-red/10 text-quant-red rounded text-[9px] font-medium hover:bg-quant-red/20 transition-colors flex items-center gap-1"
                              >
                                <XCircle className="w-3 h-3" />
                                取消
                              </button>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                ) : (
                  <div className="py-6 flex items-center justify-center">
                    <EmptyState
                      title="暂无委托"
                      description="当前没有进行中的委托订单"
                      className="py-1 border-0 text-[10px] [&>div:first-child]:hidden"
                    />
                  </div>
                )}
              </div>
            )}

            {/* ── History Table ── */}
            {activeBottomTab === 'history' && (
              <div>
                {historyLoading ? (
                  <div className="p-4 space-y-2">
                    {Array.from({ length: 4 }).map((_, i) => (
                      <Skeleton key={i} variant="text" height={32} />
                    ))}
                  </div>
                ) : historyOrders?.length ? (
                  <div className="overflow-x-auto">
                    <table className="w-full text-[11px] whitespace-nowrap">
                      <thead className="sticky top-0 bg-quant-bg-secondary z-10">
                        <tr className="text-muted-foreground text-left">
                          <th className="px-1.5 py-1 font-medium">时间</th>
                          <th className="px-1.5 py-1 font-medium">币种</th>
                          <th className="px-1.5 py-1 font-medium">方向</th>
                          <th className="px-1.5 py-1 font-medium">价格</th>
                          <th className="px-1.5 py-1 font-medium">数量</th>
                          <th className="px-1.5 py-1 font-medium">盈亏</th>
                          <th className="px-1.5 py-1 font-medium">状态</th>
                        </tr>
                      </thead>
                      <tbody>
                        {(historyOrders || []).map((o: any) => {
                          const realizedPnl = o.realized_pnl || 0
                          return (
                            <tr key={o.id} className="border-t border-quant-border/40 hover:bg-white/[0.02] transition-colors">
                              <td className="px-1.5 py-1 text-muted-foreground">{formatDateTime(o.updated_at || o.created_at)}</td>
                              <td className="px-1.5 py-1 font-semibold">{o.symbol}</td>
                              <td className="px-1.5 py-1">
                                <span className={cn('text-[9px] font-bold', o.side === 'BUY' ? 'text-quant-green' : 'text-quant-red')}>
                                  {o.side === 'BUY' ? '买入' : '卖出'}
                                </span>
                              </td>
                              <td className="px-1.5 py-1 font-mono">${formatPrice(o.avg_price || o.price, 2)}</td>
                              <td className="px-1.5 py-1 font-mono">{formatPrice(o.filled_quantity, 4)}</td>
                              <td className={cn('px-1.5 py-1 font-mono font-bold', realizedPnl >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                                {realizedPnl >= 0 ? '+' : ''}{realizedPnl.toFixed(2)}
                              </td>
                              <td className="px-1.5 py-1">
                                <StatusTag status={o.status} />
                              </td>
                            </tr>
                          )
                        })}
                      </tbody>
                    </table>
                  </div>
                ) : (
                  <div className="py-6 flex items-center justify-center">
                    <EmptyState
                      title="暂无历史成交"
                      description="还没有已成交的订单记录"
                      className="py-1 border-0 text-[10px] [&>div:first-child]:hidden"
                    />
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

/* ─── Status Tag Component ─── */
function StatusTag({ status }: { status: string }) {
  const config: Record<string, { cls: string; icon: React.ReactNode; label: string }> = {
    PENDING: { cls: 'bg-yellow-500/10 text-yellow-500', icon: <Clock className="w-3 h-3" />, label: '待成交' },
    OPEN: { cls: 'bg-quant-gold/10 text-quant-gold', icon: <Clock className="w-3 h-3" />, label: '委托中' },
    PARTIALLY_FILLED: { cls: 'bg-quant-orange/10 text-quant-orange', icon: <Zap className="w-3 h-3" />, label: '部分成交' },
    FILLED: { cls: 'bg-quant-green/10 text-quant-green', icon: <CheckCircle2 className="w-3 h-3" />, label: '已成交' },
    CANCELLED: { cls: 'bg-quant-border/40 text-muted-foreground', icon: <XCircle className="w-3 h-3" />, label: '已取消' },
    REJECTED: { cls: 'bg-quant-red/10 text-quant-red', icon: <AlertCircle className="w-3 h-3" />, label: '已拒绝' },
  }
  const c = config[status] || { cls: 'bg-quant-border/40 text-muted-foreground', icon: null, label: status }
  return (
    <span className={cn('inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-medium', c.cls)}>
      {c.icon}
      {c.label}
    </span>
  )
}