import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { useSearchParams, useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { marketApi, orderApi, portfolioApi, tradesApi } from '@/lib/api'
import { KLineChartPro } from '@klinecharts/pro'
import '@klinecharts/pro/dist/klinecharts-pro.css'
import { createBackendDatafeed } from '@/lib/klineDatafeed'
import { cn, formatCurrency, formatPercent } from '@/lib/utils'
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
  const [activeIndicators, setActiveIndicators] = useState<Set<string>>(new Set())
  const [leftTab, setLeftTab] = useState<'watchlist' | 'orderbook' | 'trades'>('watchlist')
  const [activeBottomTab, setActiveBottomTab] = useState<'positions' | 'orders' | 'history'>('positions')
  const [watchlistSearch, setWatchlistSearch] = useState('')
  const [tpPrice, setTpPrice] = useState('')
  const [slPrice, setSlPrice] = useState('')
  const [searchParams] = useSearchParams()
  const tradeMode = (searchParams.get('mode') as 'contract' | 'spot') || 'spot'
  const [bottomCollapsed, setBottomCollapsed] = useState(true)
  const navigate = useNavigate()

  const chartRef = useRef<HTMLDivElement>(null)
  const chartInstance = useRef<any>(null)
  const chartInited = useRef(false)
  const indicatorSeries = useRef<Map<string, any>>(new Map())
  const queryClient = useQueryClient()
  const [chartKey, setChartKey] = useState(0)

  /* ─── Data Queries ─── */
  const { data: klines, isLoading: klinesLoading } = useQuery({
    queryKey: ['klines', symbol, interval],
    queryFn: () => marketApi.klines(symbol, interval, 300),
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

  const datafeed = useMemo(() => createBackendDatafeed(), [])

  /* ─── Capture-phase period button click interceptor ───
   *
   * KLineChartPro 0.1.1 has a SolidJS createEffect dependency tracking bug:
   * when setPeriod() is called, its createEffect((prev)=>{...}) does NOT
   * re-run for period signal changes, so getHistoryKLineData is never called.
   *
   * Workaround: intercept clicks on period bar buttons in capture phase,
   * stop propagation to prevent KLineChartPro's SolidJS handler from
   * updating its stale internal state, then update React state and
   * destroy/recreate the chart with a fresh KLineChartPro instance.
   *
   * The key insight: period bar buttons are <span> elements inside
   * .klinecharts-pro-period-bar with class "item period" and text matching
   * INTERVALS entries (1m, 5m, 15m, 30m, 1h, 4h, 1d, 1w).
   */
  useEffect(() => {
    const el = chartRef.current
    if (!el) return

    const handler = (e: MouseEvent) => {
      // Walk up from target looking for a period bar button
      let target = e.target as HTMLElement | null
      while (target && target !== el) {
        if (
          target.classList?.contains('period') &&
          target.parentElement?.classList?.contains('klinecharts-pro-period-bar')
        ) {
          const text = target.textContent?.trim()
          if (text && INTERVALS.includes(text)) {
            e.stopPropagation()
            e.preventDefault()
            console.log('[Trading] Period interceptor: clicking', text)
            setInterval(text)
            setChartKey((k) => k + 1)
          }
          return
        }
        target = target.parentElement
      }
    }

    // Capture phase runs BEFORE SolidJS delegated handler
    el.addEventListener('click', handler, true)
    return () => el.removeEventListener('click', handler, true)
  }, [])

  /* ─── Chart Init (recreates on chartKey or symbol change) ─── */
  useEffect(() => {
    if (!chartRef.current) return
    console.log('[Trading] Initializing KLineChartPro chartKey=', chartKey, 'symbol=', symbol, 'interval=', interval)
    // Destroy old chart: clear container completely
    chartRef.current.innerHTML = ''
    try {
      const chart = new KLineChartPro({
        container: chartRef.current,
        symbol: {
          ticker: symbol,
          name: symbol.replace('USDT', '/USDT'),
          shortName: symbol,
          market: 'crypto',
          exchange: 'BINANCE',
        },
        period: { ...parseInterval(interval), text: interval },
        periods: INTERVALS.map((i) => ({ ...parseInterval(i), text: i })),
        datafeed,
        drawingBarVisible: true,
        mainIndicators: ['MA', 'EMA'],
        subIndicators: ['VOL', 'MACD'],
        theme: 'dark',
        locale: 'zh-CN',
      })
      chartInstance.current = chart
      console.log('[Trading] KLineChartPro initialized successfully')
    } catch (e) {
      console.error('[Trading] KLineChart init failed:', e)
    }
    return () => {
      console.log('[Trading] Cleaning up KLineChartPro')
      if (chartRef.current) chartRef.current.innerHTML = ''
      chartInstance.current = null
    }
  }, [chartKey, symbol])

  /* ─── Resync chart period on interval change (from left-panel or other UI) ─── */
  useEffect(() => {
    console.log('[Trading] interval changed to:', interval)
    // The chart init effect above already handles the initial render.
    // Additional interval changes (non-chartKey path) are handled by
    // the capture-phase interceptor which calls both setInterval and setChartKey.
  }, [interval])

  /* ─── Resize chart when bottom panel toggles ─── */
  useEffect(() => {
    if (chartInstance.current && chartRef.current) {
      // Dispatch resize to trigger KLineChart internal observer
      setTimeout(() => window.dispatchEvent(new Event('resize')), 100)
    }
  }, [bottomCollapsed])

  /* ─── Indicators Toggle ─── */
  const toggleIndicator = useCallback((name: string) => {
    setActiveIndicators((prev) => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }, [])

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
      <div className="flex-1 flex min-h-0">
        {/* ════════════════════════════════════════
            LEFT: Watchlist / Orderbook / Trades
        ════════════════════════════════════════ */}
        <div className="hidden md:flex w-64 shrink-0 border-r border-quant-border flex-col bg-quant-bg-secondary">
          {/* Tabs */}
          <div className="flex border-b border-quant-border">
            {([
              { key: 'watchlist', label: '自选', icon: List },
              { key: 'orderbook', label: '订单簿', icon: BarChart3 },
              { key: 'trades', label: '成交', icon: Activity },
            ] as const).map((t) => (
              <button
                key={t.key}
                onClick={() => setLeftTab(t.key)}
                className={cn(
                  'flex-1 py-2 text-[11px] font-medium transition-colors flex items-center justify-center gap-1',
                  leftTab === t.key ? 'text-quant-gold border-b-2 border-quant-gold' : 'text-muted-foreground hover:text-foreground'
                )}
              >
                <t.icon className="w-3 h-3" />
                {t.label}
              </button>
            ))}
          </div>

          {/* Watchlist Panel */}
          {leftTab === 'watchlist' && (
            <div className="flex-1 flex flex-col min-h-0">
              <div className="p-2 border-b border-quant-border">
                <div className="relative">
                  <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3 h-3 text-muted-foreground" />
                  <input
                    type="text"
                    value={watchlistSearch}
                    onChange={(e) => setWatchlistSearch(e.target.value)}
                    placeholder="搜索币种..."
                    className="w-full bg-quant-bg border border-quant-border rounded pl-7 pr-2 py-1.5 text-xs focus:outline-none focus:border-quant-gold"
                  />
                </div>
              </div>
              <div className="flex-1 overflow-y-auto p-1.5 space-y-0.5">
                {filteredWatchlist.map((sym) => (
                  <WatchlistItem
                    key={sym}
                    sym={sym}
                    active={symbol === sym}
                    onClick={() => setSymbol(sym)}
                    price={sym === symbol ? lastPrice : undefined}
                    changePct={sym === symbol ? changePct : undefined}
                  />
                ))}
                {filteredWatchlist.length === 0 && (
                  <div className="py-8">
                    <EmptyState title="未找到币种" description={`未找到匹配 "${watchlistSearch}" 的币种`} className="py-6" />
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Orderbook Panel */}
          {leftTab === 'orderbook' && (
            <div className="flex-1 flex flex-col min-h-0">
              <div className="flex text-[10px] text-muted-foreground px-3 py-1.5 border-b border-quant-border">
                <span className="flex-1">价格 (USDT)</span>
                <span className="flex-1 text-right">数量</span>
                <span className="flex-1 text-right">累计</span>
              </div>
              <div className="flex-1 overflow-y-auto">
                {obLoading && (
                  <div className="p-3 space-y-1">
                    {Array.from({ length: 12 }).map((_, i) => (
                      <Skeleton key={i} variant="text" height={16} />
                    ))}
                  </div>
                )}
                {!obLoading && orderbook && (
                  <>
                    {/* Asks (reversed) */}
                    <div className="flex flex-col-reverse">
                      {(orderbook.asks || []).slice(0, 10).map((ask: any[], i: number) => {
                        const p = parseFloat(ask[0])
                        const q = parseFloat(ask[1])
                        return (
                          <div key={`ask-${i}`} className="relative flex px-3 py-0.5 text-[11px] font-mono">
                            <DepthBar value={q} max={obMax} type="ask" />
                            <span className="flex-1 text-quant-red relative z-10">{p.toFixed(2)}</span>
                            <span className="flex-1 text-right text-muted-foreground relative z-10">{q.toFixed(4)}</span>
                            <span className="flex-1 text-right text-muted-foreground relative z-10">{(p * q).toFixed(2)}</span>
                          </div>
                        )
                      })}
                    </div>
                    {/* Spread */}
                    <div className="flex items-center justify-center py-1 border-y border-quant-border bg-quant-bg-tertiary">
                      <span className={cn('text-sm font-bold font-mono', isUp ? 'text-quant-green' : 'text-quant-red')}>
                        {lastPrice ? lastPrice.toFixed(2) : '--'}
                      </span>
                      <span className="text-[10px] text-muted-foreground ml-2">
                         spread {bestAsk && bestBid ? (parseFloat(bestAsk) - parseFloat(bestBid)).toFixed(2) : '--'}
                      </span>
                    </div>
                    {/* Bids */}
                    <div>
                      {(orderbook.bids || []).slice(0, 10).map((bid: any[], i: number) => {
                        const p = parseFloat(bid[0])
                        const q = parseFloat(bid[1])
                        return (
                          <div key={`bid-${i}`} className="relative flex px-3 py-0.5 text-[11px] font-mono">
                            <DepthBar value={q} max={obMax} type="bid" />
                            <span className="flex-1 text-quant-green relative z-10">{p.toFixed(2)}</span>
                            <span className="flex-1 text-right text-muted-foreground relative z-10">{q.toFixed(4)}</span>
                            <span className="flex-1 text-right text-muted-foreground relative z-10">{(p * q).toFixed(2)}</span>
                          </div>
                        )
                      })}
                    </div>
                  </>
                )}
                {!obLoading && !orderbook && (
                  <div className="py-8">
                    <EmptyState title="暂无订单簿数据" description="等待市场数据连接..." />
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Trades Panel */}
          {leftTab === 'trades' && (
            <div className="flex-1 flex flex-col min-h-0">
              <div className="flex text-[10px] text-muted-foreground px-3 py-1.5 border-b border-quant-border">
                <span className="flex-1">时间</span>
                <span className="flex-1 text-right">价格</span>
                <span className="flex-1 text-right">数量</span>
              </div>
              <div className="flex-1 overflow-y-auto">
                {tradesLoading && !liveTrades.length && (
                  <div className="p-3 space-y-1">
                    {Array.from({ length: 12 }).map((_, i) => (
                      <Skeleton key={i} variant="text" height={16} />
                    ))}
                  </div>
                )}
                {displayTrades.map((t: Trade, i: number) => (
                  <div
                    key={`${t.id}-${i}`}
                    className={cn(
                      'flex px-3 py-0.5 text-[11px] font-mono transition-colors',
                      i === 0 && liveTrades.length ? 'bg-quant-gold/5' : ''
                    )}
                  >
                    <span className="flex-1 text-muted-foreground">{formatTime(t.time)}</span>
                    <span className={cn('flex-1 text-right', t.side === 'buy' ? 'text-quant-green' : 'text-quant-red')}>
                      {formatPrice(t.price)}
                    </span>
                    <span className="flex-1 text-right text-muted-foreground">{t.quantity.toFixed(4)}</span>
                  </div>
                ))}
                {!displayTrades.length && (
                  <div className="py-8">
                    <EmptyState title="暂无成交记录" description="等待实时成交数据..." />
                  </div>
                )}
              </div>
            </div>
          )}
        </div>

        {/* ════════════════════════════════════════
            CENTER: Chart + Toolbar
        ════════════════════════════════════════ */}
        <div className="flex-1 flex flex-col min-w-0">
          {/* Chart Area — KLineChart Pro handles its own toolbar */}
          <div ref={chartRef} className="flex-1 min-h-0 bg-quant-bg" />
        </div>

        {/* ════════════════════════════════════════
            RIGHT: Quick Trade Panel (hidden on mobile)
        ════════════════════════════════════════ */}
        <div className="hidden md:block">
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
          onTradeModeChange={(m) => navigate(`/trading?mode=${m}`, { replace: true })}
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
          BOTTOM: Positions / Orders / History
      ════════════════════════════════════════ */}
      <div className="shrink-0 border-t border-quant-border bg-quant-bg-secondary flex flex-col">
        {/* Bottom Tabs */}
        <div className="flex border-b border-quant-border px-2 items-center justify-between">
          <div className="flex">
            {([
              { key: 'positions', label: '持仓', count: positions?.length || 0, icon: TrendingUp },
              { key: 'orders', label: '当前委托', count: orders?.length || 0, icon: Clock },
              { key: 'history', label: '历史成交', count: historyOrders?.length || 0, icon: CheckCircle2 },
            ] as const).map((t) => (
              <button
                key={t.key}
                onClick={() => { setActiveBottomTab(t.key); setBottomCollapsed(false) }}
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
            onClick={() => setBottomCollapsed(!bottomCollapsed)}
            className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5 transition-colors"
            title={bottomCollapsed ? '展开' : '收起'}
          >
            {bottomCollapsed ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
          </button>
        </div>

        {/* Bottom Content */}
        {!bottomCollapsed && (
          <div className="max-h-24 overflow-y-auto">
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
