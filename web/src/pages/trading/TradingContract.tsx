import React, { useEffect, useMemo, useState, useCallback, useRef } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { NavLink } from 'react-router-dom'
import { marketApi, orderApi, portfolioApi, accountApi, tradesApi } from '@/lib/api'
import {
  createBackendDatafeed,
  handlePriceTick,
  runBackfill,
  setChartUpdater,
  clearChartUpdater,
} from '@/lib/klineDatafeed'
import { TRADING_INTERVALS } from '@/lib/constants'
import { extractArray, safeNumber, safeString } from '@/lib/typeHelpers'
import { cn } from '@/lib/utils'
import { useWebSocket } from '@/hooks/useWebSocket'
import { toast } from '@/lib/useToast'
import { OrderBookPanel } from '@/components/trading/OrderBookPanel'
import { ChartTrading } from '@/components/trading/ChartTrading'
import { computeSpotAvgEntryPrice, formatLinePrice } from '@/components/trading/chartOverlays'
import { ErrorBoundary } from '@/components/ErrorBoundary'
import { parseInterval, SPOT_WATCHLIST } from '@/lib/tradingHelpers'
import { getPrecision } from '@/lib/tradingPrecision'
import type { Trade, Order, TickerSnapshot } from '@/types'
import type { ChartApi } from '@/lib/tradingHelpers'
import {
  Search, Activity, X, ChevronRight, BookOpen, ListOrdered,
  Briefcase, History, ArrowUp, ArrowDown,
} from 'lucide-react'
import { KLineChartPro } from '@klinecharts/pro'
import '@klinecharts/pro/dist/klinecharts-pro.css'

const WATCHLIST = SPOT_WATCHLIST

/* 页面 UI 状态本地持久化:交易对/周期/面板开合/侧栏标签,重进页面恢复原样 */
const UI_STATE_KEY = 'xt-trading-ui-contract'
interface TradingUIState {
  symbol?: string
  interval?: string
  panelOpen?: boolean
  sideTab?: string
}
function loadUIState(): TradingUIState {
  try {
    return JSON.parse(localStorage.getItem(UI_STATE_KEY) || '{}') as TradingUIState
  } catch {
    return {}
  }
}

/* Extended types for fields not yet in base definitions */
interface HistoryOrder extends Order {
  updated_at?: string
  avg_price?: number
  filled_quantity?: number
  realized_pnl?: number
}

/* ════════════════════════════════════════════════════
   CONTRACT (SWAP) TRADING — 整页图表交易版（TradingView 范式）
   图表铺满全页；点击图上价位弹出下单卡（开多/开空、杠杆、
   逐仓/全仓、止盈止损）；右侧滑出面板：订单簿 / 当前订单 /
   持仓（可市价平仓）/ 成交。旧三栏布局见 TradingContractClassic.tsx。
   ════════════════════════════════════════════════════ */

type SideTab = 'book' | 'orders' | 'positions' | 'fills'

const SIDE_TABS: { key: SideTab; label: string; icon: React.ReactNode }[] = [
  { key: 'book', label: '订单簿', icon: <BookOpen className="w-3.5 h-3.5" /> },
  { key: 'orders', label: '当前订单', icon: <ListOrdered className="w-3.5 h-3.5" /> },
  { key: 'positions', label: '持仓', icon: <Briefcase className="w-3.5 h-3.5" /> },
  { key: 'fills', label: '成交', icon: <History className="w-3.5 h-3.5" /> },
]

interface OrderPopupState {
  price: number
  x: number
  y: number
}

interface ContractPosition {
  symbol: string
  quantity: number
  avg_entry_price: number
  current_price: number
  unrealized_pnl: number
  realized_pnl: number
  side: string
  margin: number
  liquidation_price: number
  leverage: number
  market_type: string
  margin_mode: string
}

export function TradingContract() {
  const initialUI = useMemo(loadUIState, [])
  const [symbol, setSymbol] = useState(initialUI.symbol || 'BTCUSDT')
  const [interval, setInterval] = useState(initialUI.interval || '1h')
  const [popup, setPopup] = useState<OrderPopupState | null>(null)
  const [sideTab, setSideTab] = useState<SideTab>((initialUI.sideTab as SideTab) || 'book')
  const [panelOpen, setPanelOpen] = useState(initialUI.panelOpen ?? true)
  const [symbolOpen, setSymbolOpen] = useState(false)
  const [watchlistSearch, setWatchlistSearch] = useState('')
  const [obPrecision, setObPrecision] = useState('0.1')

  const chartRef = useRef<HTMLDivElement>(null)
  const chartApiRef = useRef<ChartApi | null>(null)
  const klineProRef = useRef<unknown>(null)
  const datafeed = useMemo(() => createBackendDatafeed(), [])
  const queryClient = useQueryClient()

  /* queries */
  const { data: klines } = useQuery({
    queryKey: ['klines', symbol, interval],
    queryFn: () => marketApi.klines(symbol, interval, 1000),
    refetchInterval: 5000,
  })
  const { data: orderbook, isLoading: obLoading } = useQuery({
    queryKey: ['orderbook', symbol],
    queryFn: () => marketApi.orderBook(symbol, 20),
    refetchInterval: 2000,
  })
  const { data: recentTrades } = useQuery({
    queryKey: ['trades', symbol],
    queryFn: () => marketApi.trades(symbol, 50),
    refetchInterval: 3000,
  })
  const { data: orders } = useQuery({
    queryKey: ['orders'],
    queryFn: () => orderApi.list(),
    refetchInterval: 5000,
  })
  const { data: historyOrders } = useQuery({
    queryKey: ['orders-history'],
    queryFn: () => orderApi.history({ status: 'filled' }),
    refetchInterval: 10000,
  })
  const { data: snapshot } = useQuery({
    queryKey: ['snapshot', symbol],
    queryFn: () => marketApi.snapshot(symbol).then((d) => d as TickerSnapshot),
    refetchInterval: 5000,
  })
  const { data: portfolio } = useQuery({
    queryKey: ['portfolio'],
    queryFn: () => portfolioApi.summary(),
    refetchInterval: 10000,
  })
  const { data: fillTrades } = useQuery({
    queryKey: ['fills'],
    queryFn: () => tradesApi.list({ limit: '30' }),
    refetchInterval: 5000,
  })
  const { data: allBalances } = useQuery({
    queryKey: ['balances', 'all'],
    queryFn: () => accountApi.balance(),
    refetchInterval: 10000,
  })

  const spotBalance = useMemo(() => {
    if (!portfolio) return 0
    return parseFloat(String(portfolio.spot_balance ?? 0)) || 0
  }, [portfolio])
  const holdingsList = useMemo(() => {
    if (!allBalances) return []
    const list = extractArray<Record<string, unknown>>(allBalances, 'balances', 'currencies', 'list', 'data', 'result')
    return list.filter((b) => safeNumber(b.free ?? b.available) > 0)
  }, [allBalances])
  const baseHolding = useMemo(() => {
    const base = symbol.replace('USDT', '')
    const holding = holdingsList.find((b: unknown) => {
      const bal = b as Record<string, unknown>
      return String(bal.asset || bal.currency) === base
    }) as Record<string, unknown> | undefined
    if (!holding) return 0
    return safeNumber(holding.free ?? holding.available)
  }, [symbol, holdingsList])

  /* 现货持仓成本(由已有成交历史估算),用于图表持仓均价线 */
  const spotPosition = useMemo(() => {
    if (!(baseHolding > 0)) return null
    const avg = computeSpotAvgEntryPrice(symbol, historyOrders as HistoryOrder[])
    return avg != null ? { avg_entry_price: avg } : null
  }, [symbol, baseHolding, historyOrders])

  /* 合约持仓（swap） */
  const { data: contractPositionsData } = useQuery({
    queryKey: ['contract-positions'],
    queryFn: () => portfolioApi.positions(),
    refetchInterval: 5000,
  })
  const contractPositions = useMemo(
    () => (contractPositionsData?.positions ?? []) as ContractPosition[],
    [contractPositionsData]
  )
  const contractPos = useMemo(
    () => contractPositions.find((p) => p.symbol === symbol && safeNumber(p.quantity) > 0) ?? null,
    [contractPositions, symbol]
  )

  /* websocket */
  const { on: wsOn } = useWebSocket('/ws', {
    onReconnect: () => {
      queryClient.invalidateQueries({ queryKey: ['klines', symbol, interval] })
      queryClient.invalidateQueries({ queryKey: ['orderbook', symbol] })
      queryClient.invalidateQueries({ queryKey: ['snapshot', symbol] })
      runBackfill()
    },
  })
  const [liveTrades, setLiveTrades] = useState<
    { id: string; price: number; quantity: number; side: 'buy' | 'sell'; time: number }[]
  >([])
  useEffect(() => {
    const unsub = wsOn('trade', (data: unknown) => {
      const d = data as Record<string, unknown>
      if (d.symbol === symbol) {
        setLiveTrades((prev) => [
          {
            id: String(d.id || Date.now()),
            price: Number(d.price),
            quantity: Number(d.quantity),
            side: String(d.side) as 'buy' | 'sell',
            time: Number(d.time) || Date.now(),
          },
          ...prev.slice(0, 99),
        ])
      }
    })
    return unsub
  }, [wsOn, symbol])
  useEffect(() => {
    const unsub = wsOn('price', (msg: unknown) => {
      const m = msg as Record<string, unknown>
      if (m.symbol) {
        const data = m.data as Record<string, unknown> | undefined
        handlePriceTick(String(m.symbol), Number(data?.last ?? data?.price ?? 0), Number(data?.volume ?? 0))
      }
    })
    return unsub
  }, [wsOn])

  useEffect(() => {
    setLiveTrades([])
  }, [symbol])

  /* computed */
  const precision = useMemo(() => getPrecision(symbol), [symbol])
  const lastPrice = useMemo(() => {
    if (snapshot?.price) return parseFloat(String(snapshot.price))
    if (klines?.length) return parseFloat(String(klines[klines.length - 1].close))
    return 0
  }, [snapshot, klines])
  const prevClose = useMemo(() => {
    if (klines && klines.length > 1) return parseFloat(String(klines[klines.length - 2].close))
    return lastPrice
  }, [klines, lastPrice])
  const change = lastPrice - prevClose
  const changePct = prevClose ? (change / prevClose) * 100 : 0
  const isUp = change >= 0
  const bestBid = orderbook?.bids?.[0]?.[0] != null ? String(orderbook.bids[0][0]) : ''
  const bestAsk = orderbook?.asks?.[0]?.[0] != null ? String(orderbook.asks[0][0]) : ''

  /* ─── KLineChartPro init（周期切换需重建，规避 SolidJS setPeriod bug）─── */
  const initChart = useCallback(() => {
    if (!chartRef.current) return
    if (klineProRef.current) {
      chartRef.current.innerHTML = ''
      klineProRef.current = null
      chartApiRef.current = null
    }
    let intervalId: number | null = null
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
        periods: TRADING_INTERVALS.map((i) => ({ ...parseInterval(i), text: i })),
        datafeed,
        drawingBarVisible: true,
        mainIndicators: ['MA', 'EMA'],
        subIndicators: ['VOL', 'MACD'],
        theme: 'dark',
        locale: 'zh-CN',
      })
      klineProRef.current = chart
      const checkApi = () => {
        const chartApi = (chart as unknown as { _chartApi?: unknown })._chartApi as ChartApi | undefined
        if (chartApi) {
          chartApiRef.current = chartApi
          ;(window as unknown as Record<string, unknown>).__chartApi = chartApi
          try { chartApi.scrollToRealTime() } catch { /* ignore */ }
          try { chartApi.setBarSpace(4) } catch { /* ignore */ }
          if (typeof chartApi.updateData === 'function') {
            setChartUpdater((bar) => {
              try { chartApi.updateData(bar) } catch { /* ignore */ }
            })
          }
        } else {
          intervalId = window.setTimeout(checkApi, 100)
        }
      }
      checkApi()
    } catch {
      /* KLineChartPro 初始化失败 */
    }
    return () => {
      if (intervalId) window.clearTimeout(intervalId)
    }
  }, [datafeed, symbol, interval])

  useEffect(() => {
    const el = chartRef.current
    const cancelTimer = initChart()
    return () => {
      cancelTimer?.()
      clearChartUpdater()
      if (el) el.innerHTML = ''
      klineProRef.current = null
      chartApiRef.current = null
    }
  }, [initChart])

  /* Period click observer（KLineChartPro 周期栏不暴露 onChange） */
  useEffect(() => {
    const el = chartRef.current
    if (!el) return
    const handleClick = (e: MouseEvent) => {
      let t = e.target as HTMLElement | null
      while (t && t !== el) {
        if (t.classList?.contains('period') && t.parentElement?.classList?.contains('klinecharts-pro-period-bar')) {
          const txt = t.textContent?.trim()
          if (txt && TRADING_INTERVALS.includes(txt as (typeof TRADING_INTERVALS)[number])) {
            setInterval(txt)
          }
          return
        }
        t = t.parentElement
      }
    }
    el.addEventListener('click', handleClick, true)
    return () => el.removeEventListener('click', handleClick, true)
  }, [])

  /* 面板开合时让图表自适应 */
  useEffect(() => {
    if (!klineProRef.current) return
    const t = setTimeout(() => window.dispatchEvent(new Event('resize')), 150)
    return () => clearTimeout(t)
  }, [panelOpen])

  /* ─── 图表点价 → 弹出下单卡 ─── */
  const handleChartPriceSelect = useCallback(
    (p: number, pos?: { x: number; y: number }) => {
      const formatted = formatLinePrice(p, precision.price)
      setPopup({ price: parseFloat(formatted) || p, x: pos?.x ?? 120, y: pos?.y ?? 120 })
    },
    [precision.price]
  )

  /* 长按拖动改价:实时参考线;拖动起始即弹出下单卡并实时跟随价格 */
  const [dragPrice, setDragPrice] = useState<{ price: number; y: number } | null>(null)
  const handleChartPriceDrag = useCallback(
    (p: number, pos: { x: number; y: number }, phase: 'start' | 'move' | 'end') => {
      if (phase === 'end') {
        setDragPrice(null)
        return
      }
      const formatted = formatLinePrice(p, precision.price)
      const price = parseFloat(formatted) || p
      setDragPrice({ price, y: pos.y })
      if (phase === 'start') {
        setPopup({ price, x: pos.x, y: pos.y })
      }
    },
    [precision.price]
  )

  /* UI 状态持久化 */
  useEffect(() => {
    const state: TradingUIState = { symbol, interval, panelOpen, sideTab }
    try {
      localStorage.setItem(UI_STATE_KEY, JSON.stringify(state))
    } catch {
      /* ignore */
    }
  }, [symbol, interval, panelOpen, sideTab])

  const closePopup = useCallback(() => setPopup(null), [])

  const filteredWatchlist = useMemo(() => {
    if (!watchlistSearch.trim()) return WATCHLIST
    const q = watchlistSearch.toUpperCase()
    return WATCHLIST.filter((s) => s.includes(q))
  }, [watchlistSearch])

  const openOrders = useMemo(
    () => (orders ?? []).filter((o) => ['open', 'pending', 'new', 'partially_filled'].includes(String(o.status).toLowerCase())),
    [orders]
  )

  return (
    <div className="flex flex-col h-full min-h-0 bg-quant-bg">
      {/* ── 顶栏：现货/合约切换 + 交易对 + 最新价 ── */}
      <div className="flex items-center gap-2 px-3 h-11 border-b border-quant-border shrink-0">
        <div className="flex items-center rounded-md border border-quant-border overflow-hidden text-xs">
          <NavLink
            to="/trading/spot"
            className={({ isActive }) =>
              cn('px-3 h-7 flex items-center', isActive ? 'bg-quant-gold/20 text-quant-gold font-medium' : 'text-muted-foreground hover:text-foreground')
            }
          >
            现货
          </NavLink>
          <NavLink
            to="/trading/contract"
            className={({ isActive }) =>
              cn('px-3 h-7 flex items-center', isActive ? 'bg-quant-gold/20 text-quant-gold font-medium' : 'text-muted-foreground hover:text-foreground')
            }
          >
            合约
          </NavLink>
        </div>

        {/* 交易对选择 */}
        <div className="relative">
          <button
            onClick={() => setSymbolOpen((v) => !v)}
            className="flex items-center gap-1.5 px-2 h-8 rounded hover:bg-quant-bg-secondary"
            aria-label="选择交易对"
          >
            <span className="text-sm font-semibold">{symbol.replace('USDT', '/USDT')}</span>
            <Search className="w-3.5 h-3.5 text-muted-foreground" />
          </button>
          {symbolOpen && (
            <>
              <div className="fixed inset-0 z-30" onClick={() => setSymbolOpen(false)} />
              <div className="absolute left-0 top-9 z-40 w-64 rounded-md border border-quant-border bg-quant-bg-secondary shadow-xl">
                <div className="p-2 border-b border-quant-border">
                  <div className="relative">
                    <Search className="w-3.5 h-3.5 absolute left-2 top-1/2 -translate-y-1/2 text-muted-foreground" />
                    <input
                      autoFocus
                      value={watchlistSearch}
                      onChange={(e) => setWatchlistSearch(e.target.value)}
                      placeholder="搜索交易对"
                      className="w-full h-8 pl-7 pr-2 text-xs bg-quant-bg border border-quant-border rounded focus:outline-none focus:border-quant-gold"
                    />
                  </div>
                </div>
                <div className="max-h-72 overflow-y-auto">
                  {filteredWatchlist.map((sym) => (
                    <button
                      key={sym}
                      onClick={() => { setSymbol(sym); setSymbolOpen(false); setWatchlistSearch('') }}
                      className={cn(
                        'w-full flex items-center px-3 h-8 text-xs hover:bg-quant-bg',
                        sym === symbol && 'text-quant-gold'
                      )}
                    >
                      {sym.replace('USDT', '/USDT')}
                    </button>
                  ))}
                </div>
              </div>
            </>
          )}
        </div>

        {/* 最新价 */}
        <div className="flex items-center gap-2 text-xs">
          <span className={cn('text-base font-bold tabular-nums', isUp ? 'text-quant-up' : 'text-quant-down')}>
            {lastPrice ? formatLinePrice(lastPrice, precision.price) : '—'}
          </span>
          <span className={cn('tabular-nums', isUp ? 'text-quant-up' : 'text-quant-down')}>
            {isUp ? '+' : ''}{changePct.toFixed(2)}%
          </span>
        </div>

        <div className="flex-1" />

        {/* 面板开关 */}
        <button
          onClick={() => setPanelOpen((v) => !v)}
          className="flex items-center gap-1 px-2 h-8 rounded text-xs text-muted-foreground hover:text-foreground hover:bg-quant-bg-secondary"
          aria-label={panelOpen ? '收起面板' : '展开面板'}
        >
          <ChevronRight className={cn('w-4 h-4 transition-transform', !panelOpen && 'rotate-180')} />
          {panelOpen ? '收起' : '面板'}
        </button>
      </div>

      {/* ── 主区：整页图表 + 滑出面板 ── */}
      <div className="flex-1 flex min-h-0 relative">
        {/* 图表 */}
        <div className="flex-1 min-w-0 relative bg-quant-bg">
          <ErrorBoundary
            fallback={
              <div className="absolute inset-0 flex flex-col items-center justify-center text-red-400 text-sm">
                <Activity className="w-12 h-12 mb-3 opacity-50" />
                <span>图表加载失败</span>
                <span className="text-xs opacity-60 mt-1">请刷新页面重试</span>
              </div>
            }
          >
            <div ref={chartRef} className="absolute inset-0" />
          </ErrorBoundary>

          <ChartTrading
            chartContainerRef={chartRef}
            chartApiRef={chartApiRef}
            symbol={symbol}
            orders={orders}
            position={contractPos ? { avg_entry_price: contractPos.avg_entry_price } : null}
            positionLabel="持仓成本"
            pricePrecision={precision.price}
            onPriceSelect={handleChartPriceSelect}
            onPriceDrag={handleChartPriceDrag}
          />

          {/* 长按拖动改价参考线 */}
          {dragPrice && (
            <div className="pointer-events-none absolute inset-x-0 z-20" style={{ top: dragPrice.y }}>
              <div className="border-t border-dashed border-quant-gold" />
              <div className="absolute left-2 -top-5 rounded bg-quant-gold/90 px-1.5 py-0.5 text-[10px] font-mono text-black">
                {formatLinePrice(dragPrice.price, precision.price)}
              </div>
            </div>
          )}

          {/* ── 下单浮卡 ── */}
          {popup && (
            <ChartOrderPopup
              symbol={symbol}
              price={popup.price}
              pos={popup}
              lastPrice={lastPrice}
              precision={precision.price}
              availableQuote={spotBalance}
              contractPos={contractPos}
              livePrice={dragPrice?.price ?? null}
              onClose={closePopup}
              onSubmitted={() => {
                queryClient.invalidateQueries({ queryKey: ['orders'] })
                queryClient.invalidateQueries({ queryKey: ['portfolio'] })
                queryClient.invalidateQueries({ queryKey: ['contract-positions'] })
              }}
            />
          )}
        </div>

        {/* ── 右侧滑出面板 ── */}
        <div
          className={cn(
            'shrink-0 border-l border-quant-border bg-quant-bg-secondary flex flex-col min-h-0 transition-all duration-200',
            panelOpen ? 'w-[340px]' : 'w-0 border-l-0 overflow-hidden'
          )}
        >
          {/* Tab 栏 */}
          <div className="flex items-center h-9 border-b border-quant-border shrink-0">
            {SIDE_TABS.map((t) => (
              <button
                key={t.key}
                onClick={() => setSideTab(t.key)}
                className={cn(
                  'flex-1 h-9 flex items-center justify-center gap-1 text-xs',
                  sideTab === t.key ? 'text-quant-gold border-b-2 border-quant-gold font-medium' : 'text-muted-foreground hover:text-foreground'
                )}
              >
                {t.icon}
                {t.label}
              </button>
            ))}
          </div>

          <div className="flex-1 min-h-0 overflow-hidden flex flex-col">
            {sideTab === 'book' && (
              <div className="flex-1 min-h-0 overflow-hidden">
                <OrderBookPanel
                  orderbook={orderbook}
                  obLoading={obLoading}
                  obPrecision={obPrecision}
                  onPrecisionChange={setObPrecision}
                  onPriceClick={() => undefined}
                  recentTrades={recentTrades}
                  liveTrades={liveTrades}
                  lastPrice={lastPrice}
                  bestBid={bestBid}
                  bestAsk={bestAsk}
                  symbol={symbol}
                />
              </div>
            )}

            {sideTab === 'orders' && (
              <div className="flex-1 overflow-y-auto p-2 space-y-1.5">
                {openOrders.length === 0 && (
                  <div className="text-center text-xs text-muted-foreground py-8">暂无未成交订单</div>
                )}
                {openOrders.map((o) => (
                  <div key={o.id} className="flex items-center justify-between rounded border border-quant-border px-2 py-1.5 text-xs">
                    <div>
                      <div className={cn('font-medium', o.side === 'BUY' ? 'text-quant-up' : 'text-quant-down')}>
                        {o.side === 'BUY' ? '买入' : '卖出'} {String(o.order_type)}
                      </div>
                      <div className="text-muted-foreground tabular-nums">
                        价 {safeString(o.price)} · 量 {safeString(o.quantity)}
                      </div>
                    </div>
                    <button
                      onClick={async () => {
                        try {
                          await orderApi.cancel(String(o.id))
                          toast('success', '撤单成功')
                          queryClient.invalidateQueries({ queryKey: ['orders'] })
                        } catch (e) {
                          toast('error', e instanceof Error ? e.message : '撤单失败')
                        }
                      }}
                      className="px-2 h-6 rounded text-[10px] border border-quant-border text-muted-foreground hover:text-quant-down hover:border-quant-down"
                    >
                      撤单
                    </button>
                  </div>
                ))}
              </div>
            )}

            {sideTab === 'positions' && (
              <div className="flex-1 overflow-y-auto p-2 space-y-1.5">
                {contractPositions.length === 0 && (
                  <div className="text-center text-xs text-muted-foreground py-8">暂无持仓</div>
                )}
                {contractPositions.map((p) => {
                  const qty = safeNumber(p.quantity)
                  const pnl = safeNumber(p.unrealized_pnl)
                  const isLong = ['LONG', 'BUY'].includes(String(p.side).toUpperCase())
                  return (
                    <div key={`${p.symbol}-${p.side}`} className="rounded border border-quant-border px-2 py-1.5 text-xs space-y-1">
                      <div className="flex items-center justify-between">
                        <span className="font-medium">{p.symbol.replace('USDT', '/USDT')}</span>
                        <span className={cn('px-1.5 rounded text-[10px]', isLong ? 'bg-quant-up/20 text-quant-up' : 'bg-quant-down/20 text-quant-down')}>
                          {isLong ? '多' : '空'} {qty}
                        </span>
                      </div>
                      <div className="flex justify-between text-muted-foreground tabular-nums">
                        <span>开仓 {formatLinePrice(safeNumber(p.avg_entry_price), precision.price)}</span>
                        <span>杠杆 {safeNumber(p.leverage) > 0 ? `${safeNumber(p.leverage)}x` : '—'}</span>
                      </div>
                      <div className="flex justify-between tabular-nums">
                        <span className="text-muted-foreground">未实现盈亏</span>
                        <span className={pnl >= 0 ? 'text-quant-up' : 'text-quant-down'}>
                          {pnl >= 0 ? '+' : ''}{pnl.toFixed(2)} USDT
                        </span>
                      </div>
                      <button
                        onClick={async () => {
                          try {
                            await orderApi.place({
                              symbol: p.symbol,
                              side: isLong ? 'SELL' : 'BUY',
                              order_type: 'MARKET',
                              price: 0,
                              quantity: qty,
                              market_type: 'swap',
                              position_side: String(p.side).toUpperCase(),
                              close_position: true,
                            })
                            toast('success', '平仓订单已提交')
                            queryClient.invalidateQueries({ queryKey: ['orders'] })
                            queryClient.invalidateQueries({ queryKey: ['contract-positions'] })
                            queryClient.invalidateQueries({ queryKey: ['portfolio'] })
                          } catch (e) {
                            toast('error', e instanceof Error ? e.message : '平仓失败')
                          }
                        }}
                        className="w-full h-6 rounded text-[10px] border border-quant-border text-muted-foreground hover:text-quant-down hover:border-quant-down"
                      >
                        市价平仓
                      </button>
                    </div>
                  )
                })}
                <div className="text-[10px] text-muted-foreground px-1 pt-2">
                  可用保证金 {spotBalance.toFixed(2)} USDT
                </div>
              </div>
            )}

            {sideTab === 'fills' && (
              <div className="flex-1 overflow-y-auto p-2 space-y-1.5">
                {(!fillTrades || fillTrades.length === 0) && (
                  <div className="text-center text-xs text-muted-foreground py-8">暂无成交记录</div>
                )}
                {(fillTrades ?? []).slice(0, 30).map((t: Trade) => (
                  <div key={t.id ?? `${t.symbol}-${t.time}`} className="flex items-center justify-between rounded border border-quant-border px-2 py-1.5 text-xs">
                    <div>
                      <div className={cn('font-medium', t.side === 'buy' ? 'text-quant-up' : 'text-quant-down')}>
                        {t.side === 'buy' ? '买入' : '卖出'} {t.symbol}
                      </div>
                      <div className="text-muted-foreground tabular-nums">
                        {safeString(t.price)} × {safeString(t.quantity ?? t.amount)}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}


/* ════════════════════════════════════════════════════
   合约下单浮卡：图上点击后弹出，支持 开多/开空、限价/市价、
   杠杆(1-125x)、逐仓/全仓、余额百分比、止盈止损。
   ════════════════════════════════════════════════════ */
interface ChartOrderPopupProps {
  symbol: string
  price: number
  pos: { x: number; y: number }
  lastPrice: number
  precision: number
  /** 可用保证金(USDT) */
  availableQuote: number
  /** 当前合约持仓(用于平仓模式) */
  contractPos: ContractPosition | null
  /** 长按拖动中的实时价格:非 null 时限价单价格输入框实时跟随 */
  livePrice?: number | null
  onClose: () => void
  onSubmitted: () => void
}

function ChartOrderPopup({
  symbol, price, pos, lastPrice, precision, availableQuote, contractPos, livePrice, onClose, onSubmitted,
}: ChartOrderPopupProps) {
  const [direction, setDirection] = useState<'LONG' | 'SHORT'>('LONG')
  const [orderType, setOrderType] = useState<'LIMIT' | 'MARKET'>('LIMIT')
  const [limitPrice, setLimitPrice] = useState(String(price))
  const [quantity, setQuantity] = useState('')
  const [leverage, setLeverage] = useState(10)
  const [marginMode, setMarginMode] = useState<'cross' | 'isolated'>('cross')
  const [mode, setMode] = useState<'open' | 'close'>('open')
  const [tpPrice, setTpPrice] = useState('')
  const [slPrice, setSlPrice] = useState('')
  const [showTpSl, setShowTpSl] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  /* 长按拖动:价格实时跟随 */
  useEffect(() => {
    if (livePrice != null && orderType === 'LIMIT') {
      setLimitPrice(String(livePrice))
    }
  }, [livePrice, orderType])

  // 定位：以图表容器为参照，防出界
  const vpW = typeof window !== 'undefined' ? window.innerWidth : 1280
  const vpH = typeof window !== 'undefined' ? window.innerHeight : 800
  const left = Math.min(pos.x + 12, Math.max(0, vpW - 300))
  const top = Math.min(Math.max(pos.y - 20, 8), Math.max(8, vpH - 520))

  const effPrice = orderType === 'MARKET' ? lastPrice : parseFloat(limitPrice) || 0
  const closing = mode === 'close' && contractPos != null
  const maxBase =
    closing && contractPos
      ? safeNumber(contractPos.quantity)
      : effPrice > 0
        ? (availableQuote * leverage) / effPrice
        : 0
  const marginNeeded = effPrice > 0 ? (parseFloat(quantity) || 0) * effPrice / leverage : 0

  const setPct = (pct: number) => {
    if (maxBase > 0) {
      setQuantity((maxBase * (pct / 100)).toFixed(6).replace(/0+$/, '').replace(/\.$/, ''))
    }
  }

  const submit = async () => {
    const qty = parseFloat(quantity)
    if (!qty || qty <= 0) {
      toast('error', '请输入有效数量')
      return
    }
    if (orderType === 'LIMIT' && !(parseFloat(limitPrice) > 0)) {
      toast('error', '请输入有效价格')
      return
    }
    setSubmitting(true)
    try {
      const isLong = closing ? ['LONG', 'BUY'].includes(String(contractPos!.side).toUpperCase()) : direction === 'LONG'
      await orderApi.place({
        symbol,
        side: closing ? (isLong ? 'SELL' : 'BUY') : direction === 'LONG' ? 'BUY' : 'SELL',
        order_type: orderType,
        price: orderType === 'MARKET' ? 0 : parseFloat(limitPrice),
        quantity: qty,
        market_type: 'swap',
        position_side: isLong ? 'LONG' : 'SHORT',
        leverage,
        margin_mode: marginMode,
        close_position: closing,
        tp_price: tpPrice ? parseFloat(tpPrice) : undefined,
        sl_price: slPrice ? parseFloat(slPrice) : undefined,
      })
      toast('success', closing ? '平仓订单已提交' : `${direction === 'LONG' ? '开多' : '开空'}订单已提交`)
      onSubmitted()
      onClose()
    } catch (e) {
      toast('error', e instanceof Error ? e.message : '下单失败')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <>
      <div className="fixed inset-0 z-40" onClick={onClose} />
      <div
        className="absolute z-50 w-[268px] rounded-lg border border-quant-border bg-quant-bg-secondary shadow-2xl p-3 space-y-2.5"
        style={{ left, top }}
        role="dialog"
        aria-label="合约图表下单"
      >
        <div className="flex items-center justify-between">
          <span className="text-xs font-semibold">{symbol.replace('USDT', '/USDT')} 合约</span>
          <button onClick={onClose} className="text-muted-foreground hover:text-foreground" aria-label="关闭">
            <X className="w-3.5 h-3.5" />
          </button>
        </div>

        {/* 开仓/平仓 */}
        {contractPos && (
          <div className="flex items-center gap-1 text-[10px]">
            {([['open', '开仓'], ['close', '平仓']] as const).map(([k, label]) => (
              <button
                key={k}
                onClick={() => setMode(k)}
                className={cn(
                  'px-2 py-0.5 rounded',
                  mode === k ? 'bg-quant-gold/20 text-quant-gold' : 'text-muted-foreground'
                )}
              >
                {label}
              </button>
            ))}
          </div>
        )}

        {/* 开多/开空 */}
        <div className="grid grid-cols-2 gap-1.5">
          <button
            onClick={() => { setDirection('LONG'); setMode('open') }}
            className={cn(
              'h-8 rounded text-xs font-medium flex items-center justify-center gap-1',
              !closing && direction === 'LONG' ? 'bg-quant-up text-white' : 'border border-quant-border text-muted-foreground'
            )}
          >
            <ArrowUp className="w-3 h-3" /> 开多
          </button>
          <button
            onClick={() => { setDirection('SHORT'); setMode('open') }}
            className={cn(
              'h-8 rounded text-xs font-medium flex items-center justify-center gap-1',
              !closing && direction === 'SHORT' ? 'bg-quant-down text-white' : 'border border-quant-border text-muted-foreground'
            )}
          >
            <ArrowDown className="w-3 h-3" /> 开空
          </button>
        </div>

        {/* 限价/市价 + 逐仓/全仓 */}
        <div className="flex items-center justify-between text-[10px]">
          <div className="flex items-center gap-1">
            {(['LIMIT', 'MARKET'] as const).map((t) => (
              <button
                key={t}
                onClick={() => setOrderType(t)}
                className={cn(
                  'px-2 py-0.5 rounded',
                  orderType === t ? 'bg-quant-gold/20 text-quant-gold' : 'text-muted-foreground'
                )}
              >
                {t === 'LIMIT' ? '限价' : '市价'}
              </button>
            ))}
          </div>
          <div className="flex items-center gap-1">
            {([['cross', '全仓'], ['isolated', '逐仓']] as const).map(([k, label]) => (
              <button
                key={k}
                onClick={() => setMarginMode(k)}
                className={cn(
                  'px-2 py-0.5 rounded',
                  marginMode === k ? 'bg-quant-gold/20 text-quant-gold' : 'text-muted-foreground'
                )}
              >
                {label}
              </button>
            ))}
          </div>
        </div>

        {/* 杠杆 */}
        <div>
          <div className="flex justify-between text-[10px] text-muted-foreground mb-0.5">
            <span>杠杆</span>
            <span className="text-quant-gold font-medium">{leverage}x</span>
          </div>
          <input
            type="range"
            min={1}
            max={125}
            value={leverage}
            onChange={(e) => setLeverage(parseInt(e.target.value, 10))}
            className="w-full h-1 accent-quant-gold"
          />
        </div>

        {/* 价格 */}
        <div>
          <label className="block text-[10px] text-muted-foreground mb-0.5">价格 (USDT)</label>
          <input
            type="number"
            value={orderType === 'MARKET' ? '' : limitPrice}
            disabled={orderType === 'MARKET'}
            placeholder={orderType === 'MARKET' ? `市价 ≈ ${lastPrice ? formatLinePrice(lastPrice, precision) : '—'}` : ''}
            onChange={(e) => setLimitPrice(e.target.value)}
            className="w-full h-7 px-2 text-xs bg-quant-bg border border-quant-border rounded focus:outline-none focus:border-quant-gold tabular-nums disabled:opacity-50"
          />
        </div>

        {/* 数量 */}
        <div>
          <label className="block text-[10px] text-muted-foreground mb-0.5">数量 ({symbol.replace('USDT', '')})</label>
          <input
            type="number"
            value={quantity}
            onChange={(e) => setQuantity(e.target.value)}
            placeholder="0.00"
            className="w-full h-7 px-2 text-xs bg-quant-bg border border-quant-border rounded focus:outline-none focus:border-quant-gold tabular-nums"
          />
          <div className="flex gap-1 mt-1">
            {[25, 50, 75, 100].map((p) => (
              <button
                key={p}
                onClick={() => setPct(p)}
                className="flex-1 h-5 rounded text-[9px] border border-quant-border text-muted-foreground hover:text-quant-gold hover:border-quant-gold"
              >
                {p}%
              </button>
            ))}
          </div>
          <div className="text-[9px] text-muted-foreground mt-0.5 tabular-nums">
            可开: {maxBase.toFixed(6).replace(/0+$/, '').replace(/\.$/, '')} {symbol.replace('USDT', '')}（可用 {availableQuote.toFixed(2)} USDT × {leverage}x）
            {marginNeeded > 0 && ` · 占用保证金 ≈ ${marginNeeded.toFixed(2)} USDT`}
          </div>
        </div>

        {/* 止盈止损 */}
        <button
          onClick={() => setShowTpSl((v) => !v)}
          className="text-[10px] text-muted-foreground hover:text-foreground"
        >
          {showTpSl ? '▾' : '▸'} 止盈止损（可选）
        </button>
        {showTpSl && (
          <div className="grid grid-cols-2 gap-1.5">
            <input
              type="number"
              value={tpPrice}
              onChange={(e) => setTpPrice(e.target.value)}
              placeholder="止盈价"
              className="h-7 px-2 text-xs bg-quant-bg border border-quant-border rounded focus:outline-none focus:border-quant-gold tabular-nums"
            />
            <input
              type="number"
              value={slPrice}
              onChange={(e) => setSlPrice(e.target.value)}
              placeholder="止损价"
              className="h-7 px-2 text-xs bg-quant-bg border border-quant-border rounded focus:outline-none focus:border-quant-gold tabular-nums"
            />
          </div>
        )}

        {/* 提交 */}
        <button
          onClick={submit}
          disabled={submitting}
          className={cn(
            'w-full h-8 rounded text-xs font-semibold text-white disabled:opacity-50',
            closing || direction === 'LONG' ? 'bg-quant-up hover:opacity-90' : 'bg-quant-down hover:opacity-90'
          )}
        >
          {submitting
            ? '提交中…'
            : closing
              ? `平仓 ${symbol.replace('USDT', '')}`
              : `${direction === 'LONG' ? '开多' : '开空'} ${symbol.replace('USDT', '')}`}
        </button>
      </div>
    </>
  )
}
