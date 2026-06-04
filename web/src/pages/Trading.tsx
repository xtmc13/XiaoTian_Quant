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
        'w-full flex items-center justify-between px-3 py-2 rounded text-xs transition-colors',
        active ? 'bg-quant-gold/10 text-quant-gold' : 'hover:bg-white/5'
      )}
    >
      <div className="flex items-center gap-2">
        <span className={cn('w-1 h-1 rounded-full', isUp ? 'bg-quant-green' : 'bg-quant-red')} />
        <span className="font-semibold tracking-tight">{sym.replace('USDT', '/USDT')}</span>
      </div>
      <div className="text-right">
        <div className="font-mono font-medium">{price ? formatPrice(price) : '--'}</div>
        <div className={cn('font-mono text-[10px]', isUp ? 'text-quant-green' : 'text-quant-red')}>
          {isUp ? '+' : ''}{changePct?.toFixed(2) ?? '--'}%
        </div>
      </div>
    </button>
  )
}

/* ─── Orderbook Depth Bar ─── */
function DepthBar({ value, max, type }: { value: number; max: number; type: 'bid' | 'ask' }) {
  const pct = max > 0 ? (value / max) * 100 : 0
  return (
    <div className="absolute inset-y-0 right-0 overflow-hidden" style={{ width: `${pct}%`, opacity: 0.12 }}>
      <div className={cn('h-full w-full', type === 'bid' ? 'bg-quant-green' : 'bg-quant-red')} />
    </div>
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
  const [activeBottomTab, setActiveBottomTab] = useState<'positions' | 'orders' | 'history'>('positions')
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
      // Refetch critical market data immediately after reconnect
      queryClient.invalidateQueries({ queryKey: ['klines', symbol, interval] })
      queryClient.invalidateQueries({ queryKey: ['orderbook', symbol] })
      queryClient.invalidateQueries({ queryKey: ['snapshot', symbol] })
      queryClient.invalidateQueries({ queryKey: ['trades'] })
      // Backfill any K-line bars missed during disconnect
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

  /* ─── Pipe WS price ticks to K-line datafeed (no duplicate WS connection) ─── */
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

  /* ─── KLineChartPro init (recreates when interval changes) ───
   *
   * KLineChartPro 0.1.1 has a SolidJS createEffect bug: setPeriod() does NOT
   * trigger getHistoryKLineData, so we must re-create the chart on period change.
   * Symbol changes use setSymbol() which works fine.
   */
  const initChart = useCallback(() => {
    if (!chartRef.current) return
    // Destroy previous instance
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
          // Zoom to show enough bars after data loads
          try { api.scrollToRealTime() } catch (_) {}
          try { api.setBarSpace(4) } catch (_) {}
          // Wire running bar updates directly to chart (avoids timestamp conflict
          // with the last historical bar for the current period)
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

  // Init on mount & when interval changes (SolidJS setPeriod workaround)
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


  /* ─── Period click observer ───
     KLineChartPro's period bar doesn't expose onChange.
     We observe clicks and sync to React state — chart recreation is
     triggered by [interval] dependency in the init effect above.   */
  useEffect(() => {
    const el = chartRef.current
    if (!el) return
    const handleClick = (e: MouseEvent) => {
      let t = e.target as HTMLElement | null
      while (t && t !== el) {
        if (t.classList?.contains("period") && t.parentElement?.classList?.contains("klinecharts-pro-period-bar")) {
          const txt = t.textContent?.trim()
          if (txt && INTERVALS.includes(txt)) {
            // Don't stopPropagation — let KLineChartPro handle its own UI highlight
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
    <div className="h-full flex flex-col">
      {/* MAIN GRID: Chart | Orderbook | Trade */}
      <div className="flex-1 grid grid-cols-[1fr_270px_310px] gap-px bg-quant-border min-h-0">
        {/* CHART */}
        <div className="bg-quant-bg flex flex-col min-h-0 overflow-hidden">
          <div ref={chartRef} className="flex-1 min-h-0" />
        </div>

        {/* ORDERBOOK + TRADES (270px) */}
        <div className="bg-quant-bg-secondary flex flex-col overflow-hidden min-h-0">
          <div className="h-9 shrink-0 border-b border-quant-border flex items-center px-3 gap-3">
            <span className={cn("text-xs font-semibold", obTab === "book" ? "text-foreground" : "text-muted-foreground cursor-pointer hover:text-foreground")} onClick={() => setObTab("book")}>订单簿</span>
            <span className={cn("text-xs", obTab === "trades" ? "text-foreground font-semibold" : "text-muted-foreground cursor-pointer hover:text-foreground")} onClick={() => setObTab("trades")}>最新成交</span>
            <div className="ml-auto flex gap-1">
              {["0.1","1","10"].map(function(p) { return (
                <span key={p} className={cn("text-[10px] px-1 py-0.5 rounded cursor-pointer", obPrecision === p ? "bg-quant-hover text-foreground" : "text-muted-foreground hover:text-foreground")} onClick={() => setObPrecision(p)}>{p}</span>
              );})}
            </div>
          </div>
          <div className="flex text-[10px] text-muted-foreground px-3 py-1.5 border-b border-quant-border shrink-0">
            <span className="flex-1">价格 (USDT)</span>
            <span className="flex-1 text-right">数量</span>
            <span className="flex-1 text-right">累计</span>
          </div>
          <div className="flex-1 flex flex-col min-h-0">
            <div className="flex-1 overflow-y-auto">
              {obLoading ? (
                <div className="p-3 space-y-1">
                  {Array.from({ length: 12 }).map(function(_, i) { return <Skeleton key={i} variant="text" height={16} />; })}
                </div>
              ) : orderbook ? (
                <>
                  <div className="flex flex-col-reverse">
                    {(orderbook.asks || []).slice(0, 10).map(function(ask, i) {
                      var p = parseFloat(ask[0]); var q = parseFloat(ask[1]);
                      return (
                        <div key={"ask-" + i} className="relative flex px-3 py-0.5 text-[11px] font-mono cursor-pointer hover:bg-white/[0.04]">
                          <div className="absolute top-0 bottom-0 right-0 opacity-20 z-0" style={{background: "#F6465D", width: Math.min((q / obMax) * 100, 100) + "%"}} />
                          <span className="flex-1 text-quant-red relative z-10">{p.toFixed(2)}</span>
                          <span className="flex-1 text-right text-muted-foreground relative z-10">{q.toFixed(4)}</span>
                          <span className="flex-1 text-right text-muted-foreground relative z-10">{(p * q).toFixed(2)}</span>
                        </div>
                      );
                    })}
                  </div>
                  <div className="flex items-center justify-center py-1.5 border-y border-quant-border bg-quant-bg-tertiary shrink-0">
                    <span className={cn("text-sm font-bold font-mono", isUp ? "text-quant-green" : "text-quant-red")}>
                      {lastPrice ? lastPrice.toFixed(2) : "--"}
                    </span>
                    <span className="text-[10px] text-muted-foreground ml-2">
                      spread {bestAsk && bestBid ? (parseFloat(bestAsk) - parseFloat(bestBid)).toFixed(2) : "--"}
                    </span>
                  </div>
                  <div>
                    {(orderbook.bids || []).slice(0, 10).map(function(bid, i) {
                      var p = parseFloat(bid[0]); var q = parseFloat(bid[1]);
                      return (
                        <div key={"bid-" + i} className="relative flex px-3 py-0.5 text-[11px] font-mono cursor-pointer hover:bg-white/[0.04]">
                          <div className="absolute top-0 bottom-0 left-0 opacity-20 z-0" style={{background: "#2EBD85", width: Math.min((q / obMax) * 100, 100) + "%"}} />
                          <span className="flex-1 text-quant-green relative z-10">{p.toFixed(2)}</span>
                          <span className="flex-1 text-right text-muted-foreground relative z-10">{q.toFixed(4)}</span>
                          <span className="flex-1 text-right text-muted-foreground relative z-10">{(p * q).toFixed(2)}</span>
                        </div>
                      );
                    })}
                  </div>
                </>
              ) : (
                <div className="py-8"><EmptyState title="暂无订单簿数据" description="等待市场数据连接..." /></div>
              )}
            </div>

          </div>
        </div>

        {/* TRADE PANEL (310px) */}
        <div className="bg-quant-bg-secondary overflow-y-auto h-full">
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
            onTradeModeChange={function(m) { navigate("/trading?mode=" + m, {replace: true}); }}
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

      {/* ════════════════════════════════════════
          BOTTOM: Positions / Orders / History (draggable)
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
              { key: 'history', label: '历史成交', count: historyOrders?.length || 0, icon: CheckCircle2 },
            ] as const).map((t) => (
              <button
                key={t.key}
                onClick={() => { setActiveBottomTab(t.key); setBottomHeight(h => Math.max(h, 180)) }}
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
            onClick={() => setBottomHeight(h => h < 20 ? 180 : 0)}
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
              <div>
                {posLoading ? (
                  <div className="p-4 space-y-2">
                    {Array.from({ length: 4 }).map((_, i) => (
                      <Skeleton key={i} variant="text" height={32} />
                    ))}
                  </div>
                ) : positions?.length ? (
                  <div className="overflow-x-auto">
                  <table className="w-full text-[10px] whitespace-nowrap">
                    <thead className="sticky top-0 bg-quant-bg-secondary z-10">
                      {tradeMode === 'contract' ? (
                        <tr className="text-muted-foreground text-left">
                          <th className="px-1.5 py-1 font-medium">币种</th>
                          <th className="px-1.5 py-1 font-medium">方向</th>
                          <th className="px-1.5 py-1 font-medium">杠杆</th>
                          <th className="px-1.5 py-1 font-medium">开仓价</th>
                          <th className="px-1.5 py-1 font-medium">标记价</th>
                          <th className="px-1.5 py-1 font-medium">盈亏</th>
                          <th className="px-1.5 py-1 font-medium">保证金</th>
                          <th className="px-1.5 py-1 font-medium">操作</th>
                        </tr>
                      ) : (
                        <tr className="text-muted-foreground text-left">
                          <th className="px-1.5 py-1 font-medium">币种</th>
                          <th className="px-1.5 py-1 font-medium">持仓量</th>
                          <th className="px-1.5 py-1 font-medium">均价</th>
                          <th className="px-1.5 py-1 font-medium">当前价</th>
                          <th className="px-1.5 py-1 font-medium">盈亏</th>
                          <th className="px-1.5 py-1 font-medium">操作</th>
                        </tr>
                      )}
                    </thead>
                    <tbody>
                      {(positions || []).map((pos: any) => {
                        const pnl = pos.unrealized_pnl || 0
                        const pnlPct = pos.margin && pos.margin > 0 ? (pnl / pos.margin) * 100 : 0
                        const liqPrice = pos.liquidation_price || 0
                        const qty = pos.quantity || 0
                        const cost = (pos.entry_price || 0) * qty
                        return tradeMode === 'contract' ? (
                          <tr key={pos.id || pos.symbol + pos.side} className="border-t border-quant-border/40 hover:bg-white/[0.02] transition-colors">
                            <td className="px-1.5 py-1 font-semibold">{pos.symbol}</td>
                            <td className="px-1.5 py-1">
                              <span className={cn(
                                'px-1 py-0.5 rounded text-[9px] font-bold',
                                pos.side === 'LONG' ? 'bg-quant-green/10 text-quant-green' : 'bg-quant-red/10 text-quant-red'
                              )}>
                                {pos.side === 'LONG' ? '多' : '空'}
                              </span>
                            </td>
                            <td className="px-1.5 py-1 font-mono">{pos.leverage}x</td>
                            <td className="px-1.5 py-1 font-mono">${formatPrice(pos.entry_price, 2)}</td>
                            <td className="px-1.5 py-1 font-mono">${formatPrice(pos.mark_price, 2)}</td>
                            <td className={cn('px-1.5 py-1 font-mono font-bold', pnl >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                              {pnl >= 0 ? '+' : ''}${formatPrice(pnl)}
                            </td>
                            <td className="px-1.5 py-1 font-mono">${formatPrice(pos.margin, 0)}</td>
                            <td className="px-1.5 py-1">
                              <button className="px-1.5 py-0.5 bg-quant-red/10 text-quant-red rounded text-[9px] font-medium hover:bg-quant-red/20 transition-colors">
                                平仓
                              </button>
                            </td>
                          </tr>
                        ) : (
                          <tr key={pos.id || pos.symbol} className="border-t border-quant-border/40 hover:bg-white/[0.02] transition-colors">
                            <td className="px-1.5 py-1 font-semibold">{pos.symbol}</td>
                            <td className="px-1.5 py-1 font-mono">{qty.toFixed(4)}</td>
                            <td className="px-1.5 py-1 font-mono">${formatPrice(pos.entry_price, 2)}</td>
                            <td className="px-1.5 py-1 font-mono">${formatPrice(pos.mark_price, 2)}</td>
                            <td className={cn('px-1.5 py-1 font-mono font-bold', pnl >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                              {pnl >= 0 ? '+' : ''}${formatPrice(pnl)}
                            </td>
                            <td className="px-1.5 py-1">
                              <button className="px-1.5 py-0.5 bg-quant-red/10 text-quant-red rounded text-[9px] font-medium hover:bg-quant-red/20 transition-colors">
                                卖出
                              </button>
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
                      title="暂无持仓"
                      description="当前没有持仓，请在右侧面板下单"
                      className="py-1 border-0 text-[10px] [&>div:first-child]:hidden"
                    />
                  </div>
                )}
              </div>
            )}

            {/* ── Orders Table ── */}
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
                  <table className="w-full text-[10px] whitespace-nowrap">
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
                  <table className="w-full text-[10px] whitespace-nowrap">
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
function MarketStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5 whitespace-nowrap">
      <span className="text-[10px] text-muted-foreground">{label}</span>
      <span className="text-xs font-medium text-foreground">{value}</span>
    </div>
  )
}

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
