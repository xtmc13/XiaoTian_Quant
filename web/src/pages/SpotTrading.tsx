import React, { useEffect, useRef, useCallback, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { marketApi, orderApi, portfolioApi, accountApi, tradesApi } from '@/lib/api'
import { KLineChartPro } from '@klinecharts/pro'
import '@klinecharts/pro/dist/klinecharts-pro.css'
import { createBackendDatafeed, handlePriceTick, runBackfill, setChartUpdater, clearChartUpdater } from '@/lib/klineDatafeed'
import { TRADING_INTERVALS } from '@/lib/constants'
import { extractArray, safeNumber, safeString } from '@/lib/typeHelpers'
import { cn } from '@/lib/utils'
import { useWebSocket } from '@/hooks/useWebSocket'
import { toast, ToastContainer } from '@/lib/useToast'
import { OrderBookPanel } from '@/components/trading/OrderBookPanel'
import { OrderForm } from '@/components/trading/OrderForm'
import { TradeHistory } from '@/components/trading/TradeHistory'
import { DepthChart } from '@/components/trading/DepthChart'
import { ErrorBoundary } from '@/components/ErrorBoundary'
import {
  parseInterval, formatPrice,
  SPOT_WATCHLIST,
} from '@/lib/tradingHelpers'
import { getPrecision } from '@/lib/tradingPrecision'
import type { TickerSnapshot } from '@/types'
import type { ChartApi } from '@/lib/tradingHelpers'
import {
  Search, Activity, Star
} from 'lucide-react'

const WATCHLIST = SPOT_WATCHLIST

/* WatchlistItem */
const WatchlistItem = React.memo(function WatchlistItem({ sym, active, onClick, price, changePct }:{
  sym:string; active:boolean; onClick:()=>void; price?:number; changePct?:number
}) {
  const isUp = (changePct||0)>=0
  return (
    <button onClick={onClick} className={cn('w-full flex items-center justify-between px-3 py-2 text-xs transition-colors', active?'bg-quant-gold/10 text-quant-gold':'hover:bg-white/5')}>
      <div className="flex items-center gap-2">
        <Star className={cn('w-3 h-3', active?'fill-quant-gold text-quant-gold':'text-muted-foreground')}/>
        <span className="font-semibold tracking-tight">{sym.replace('USDT','/USDT')}</span>
      </div>
      <div className="text-right">
        <div className="font-mono font-medium text-foreground">{price?formatPrice(price):'--'}</div>
        <div className={cn('font-mono text-[10px]', isUp?'text-[#0ECB81]':'text-[#F6465D]')}>{isUp?'+':''}{changePct?.toFixed(2)??'--'}%</div>
      </div>
    </button>
  )
})

/* ════════════════════════════════════════
   SPOT TRADING PAGE — 币安现货风格
   ════════════════════════════════════════ */
export function SpotTrading() {
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [interval, setInterval] = useState('1h')
  const [obPrecision, setObPrecision] = useState('0.1')
  const [activeBottomTab, setActiveBottomTab] = useState<'positions'|'orders'|'history'|'fills'|'assets'>('positions')
  const [bottomHeight, setBottomHeight] = useState(0)
  const bottomCollapsed = bottomHeight < 20
  const dragRef = useRef<{startY:number;startH:number}|null>(null)
  const [watchlistSearch, setWatchlistSearch] = useState('')

  const chartRef = useRef<HTMLDivElement>(null)
  const chartApiRef = useRef<ChartApi | null>(null)
  const klineProRef = useRef<unknown>(null)
  const datafeed = useMemo(()=>createBackendDatafeed(),[])
  const queryClient = useQueryClient()

  /* queries */
  const {data:klines} = useQuery({queryKey:['klines',symbol,interval], queryFn:()=>marketApi.klines(symbol,interval,1000), refetchInterval:5000})
  const {data:orderbook, isLoading:obLoading} = useQuery({queryKey:['orderbook',symbol], queryFn:()=>marketApi.orderBook(symbol,20), refetchInterval:2000})
  const {data:recentTrades} = useQuery({queryKey:['trades',symbol], queryFn:()=>marketApi.trades(symbol,50), refetchInterval:3000})
  const {data:orders, isLoading:ordersLoading} = useQuery({queryKey:['orders'], queryFn:()=>orderApi.list(), refetchInterval:5000})
  const {data:historyOrders, isLoading:historyLoading} = useQuery({queryKey:['orders-history'], queryFn:()=>orderApi.history({status:'filled'}), refetchInterval:10000})
  const {data:snapshot} = useQuery({queryKey:['snapshot',symbol], queryFn:()=>marketApi.snapshot(symbol).then(d => d as TickerSnapshot), refetchInterval:5000})
  const {data:allSnapshots} = useQuery({queryKey:['snapshot','all'], queryFn:()=>marketApi.snapshot(), refetchInterval:5000})
  const priceMap = useMemo(()=>{
    if(!allSnapshots) return {}
    const map:Record<string,number> = {}
    const list = extractArray<Record<string, unknown>>(allSnapshots, 'tickers', 'prices', 'list', 'data', 'result')
    list.forEach((item)=>{
      const sym = safeString(item.symbol || item.ticker)
      if(sym) map[sym] = safeNumber(item.price ?? item.last ?? item.close)
    })
    return map
  },[allSnapshots])
  const {data:portfolio} = useQuery({queryKey:['portfolio'], queryFn:()=>portfolioApi.summary(), refetchInterval:10000})
  const spotBalance = useMemo(()=>{
    if(!portfolio) return 0
    return parseFloat(String(portfolio.spot_balance ?? 0)) || 0
  },[portfolio])
  const {data:fillTrades, isLoading:fillsLoading} = useQuery({queryKey:['fills'], queryFn:()=>tradesApi.list({limit:'30'}), refetchInterval:5000})
  const {data:allBalances, isLoading:balLoading} = useQuery({queryKey:['balances','all'], queryFn:()=>accountApi.balance(), refetchInterval:10000})
  const holdingsList = useMemo(()=>{
    if(!allBalances) return []
    const list = extractArray<Record<string, unknown>>(allBalances, 'balances', 'currencies', 'list', 'data', 'result')
    return list.filter((b)=> safeNumber(b.free ?? b.available) > 0)
  },[allBalances])

  /* websocket */
  const {on:wsOn} = useWebSocket('/ws',{
    onReconnect:()=>{
      queryClient.invalidateQueries({queryKey:['klines',symbol,interval]})
      queryClient.invalidateQueries({queryKey:['orderbook',symbol]})
      queryClient.invalidateQueries({queryKey:['snapshot',symbol]})
      runBackfill()
    }
  })
  const [liveTrades,setLiveTrades] = useState<{id:string;price:number;quantity:number;side:'buy'|'sell';time:number}[]>([])
  useEffect(()=>{
    const unsub=wsOn('trade',(data: unknown)=>{
      const d = data as Record<string, unknown>
      if(d.symbol===symbol){
        setLiveTrades(prev=>[{id:String(d.id||Date.now()),price:Number(d.price),quantity:Number(d.quantity),side:String(d.side) as 'buy'|'sell',time:Number(d.time)||Date.now()},...prev.slice(0,99)])
      }
    })
    return unsub
  },[wsOn,symbol])
  useEffect(()=>{
    const unsub=wsOn('price',(msg: unknown)=>{
      const m = msg as Record<string, unknown>
      if(m.symbol) {
        const data = m.data as Record<string, unknown> | undefined
        handlePriceTick(String(m.symbol), Number(data?.last??data?.price??0), Number(data?.volume??0))
      }
    })
    return unsub
  },[wsOn])

  /* clear liveTrades when symbol changes */
  useEffect(() => {
    setLiveTrades([])
  }, [symbol])

  /* computed */
  const precision = useMemo(() => getPrecision(symbol), [symbol])
  const lastPrice = useMemo(()=>{
    if(snapshot?.price) return parseFloat(String(snapshot.price))
    if(klines?.length) return parseFloat(String(klines[klines.length-1].close))
    return 0
  },[snapshot,klines])
  const prevClose = useMemo(()=>{
    if(klines&&klines.length>1) return parseFloat(String(klines[klines.length-2].close))
    return lastPrice
  },[klines,lastPrice])
  const change = lastPrice-prevClose
  const changePct = prevClose?(change/prevClose)*100:0
  const bestBid = orderbook?.bids?.[0]?.[0] != null ? String(orderbook.bids[0][0]) : ''
  const bestAsk = orderbook?.asks?.[0]?.[0] != null ? String(orderbook.asks[0][0]) : ''

  const filteredWatchlist = useMemo(()=>{
    if(!watchlistSearch.trim()) return WATCHLIST
    const q=watchlistSearch.toUpperCase()
    return WATCHLIST.filter(s=>s.includes(q))
  },[watchlistSearch])

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
        periods: TRADING_INTERVALS.map((i) => ({ ...parseInterval(i), text: i })),
        datafeed, drawingBarVisible: true,
        mainIndicators: ["MA", "EMA"], subIndicators: ["VOL", "MACD"],
        theme: "dark", locale: "zh-CN",
      })
      klineProRef.current = chart

      const checkApi = () => {
        const chartApi = (chart as unknown as { _chartApi?: unknown })._chartApi as ChartApi | undefined
        if (chartApi) {
          chartApiRef.current = chartApi
          // Zoom to show enough bars after data loads
          try { chartApi.scrollToRealTime() } catch { /* ignore chart api error */ }
          try { chartApi.setBarSpace(4) } catch { /* ignore chart api error */ }
          // Wire running bar updates directly to chart (avoids timestamp conflict
          // with the last historical bar for the current period)
          if (typeof chartApi.updateData === 'function') {
            setChartUpdater((bar) => { try { chartApi.updateData(bar) } catch { /* ignore update error */ } })
          }
        } else {
          intervalId = window.setTimeout(checkApi, 100)
        }
      }
      checkApi()
    } catch { /* KLineChartPro 初始化失败，已在 UI 中处理 */ }
    return () => { if (intervalId) window.clearTimeout(intervalId) }
  }, [datafeed, symbol, interval])

  // Init on mount & when interval changes (SolidJS setPeriod workaround)
  useEffect(() => {
    const el = chartRef.current
    const cancelTimer = initChart()
    return () => {
      cancelTimer?.()
      clearChartUpdater()
      if (el) el.innerHTML = ""
      klineProRef.current = null
      chartApiRef.current = null
    }
  }, [initChart])


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
          if (txt && TRADING_INTERVALS.includes(txt as typeof TRADING_INTERVALS[number])) {
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

  /* order handlers */
  const handleCancelOrder = useCallback(async (id:string)=>{
    try{
      await orderApi.cancel(id)
      toast('success', '订单已取消')
      queryClient.invalidateQueries({queryKey:['orders']})
    }catch(e: unknown){
      const err = e instanceof Error ? e : new Error(String(e))
      toast('error', err.message || '取消失败')
    }
  },[queryClient])

  return (
    <div className="h-full flex flex-col">
      {/* MAIN GRID: Orderbook | Chart | Trade */}
      <div className="flex-1 grid grid-cols-[270px_1fr_310px] gap-px bg-quant-border min-h-0">

        {/* LEFT: Orderbook + Trades (270px) */}
        <OrderBookPanel
          orderbook={orderbook}
          obLoading={obLoading}
          obPrecision={obPrecision}
          onPrecisionChange={setObPrecision}
          onPriceClick={() => {}}
          recentTrades={recentTrades}
          liveTrades={liveTrades}
          lastPrice={lastPrice}
          bestBid={bestBid}
          bestAsk={bestAsk}
          symbol={symbol}
        />
        <DepthChart bids={orderbook?.bids} asks={orderbook?.asks} lastPrice={lastPrice} className="shrink-0" />

        {/* CHART (1fr) */}
        <div className="bg-quant-bg flex flex-col min-h-0 overflow-hidden">
          <ErrorBoundary fallback={<div className="flex-1 flex flex-col items-center justify-center text-red-400 text-sm">
            <Activity className="w-12 h-12 mb-3 opacity-50" />
            <span>图表加载失败</span>
            <span className="text-xs opacity-60 mt-1">请刷新页面重试</span>
          </div>}>
            <div ref={chartRef} className="flex-1 min-h-0" />
          </ErrorBoundary>
        </div>

        {/* RIGHT: Watchlist + Trade Form (310px) */}
        <div className="bg-quant-bg-secondary overflow-y-auto flex flex-col">
          {/* Watchlist */}
          <div className="h-[220px] shrink-0 border-b border-quant-border flex flex-col">
            <div className="h-8 flex items-center px-3 border-b border-quant-border justify-between">
              <span className="text-xs font-medium text-muted-foreground">自选</span>
              <div className="relative">
                <Search className="w-3 h-3 absolute left-2 top-1/2 -translate-y-1/2 text-muted-foreground"/>
                <input value={watchlistSearch} onChange={e=>setWatchlistSearch(e.target.value)} placeholder="搜索" aria-label="搜索交易对" className="w-24 h-7 pl-6 pr-2 text-[10px] bg-quant-bg border border-quant-border rounded focus:outline-none focus:border-quant-gold text-foreground placeholder:text-muted-foreground"/>
              </div>
            </div>
            <div className="flex-1 overflow-y-auto">
              {filteredWatchlist.map(sym=>{
                const isActive = sym===symbol
                const symPrice = isActive?(snapshot?.price?parseFloat(String(snapshot.price)):undefined):(priceMap[sym]||undefined)
                return (
                  <WatchlistItem key={sym} sym={sym} active={isActive} onClick={()=>setSymbol(sym)} price={symPrice} changePct={isActive?changePct:undefined}/>
                )
              })}
            </div>
          </div>

          {/* Trade Form */}
          <OrderForm
            mode="spot"
            symbol={symbol}
            lastPrice={lastPrice}
            precision={precision}
            balance={spotBalance}
            holdings={holdingsList}
            onOrderPlaced={() => {
              queryClient.invalidateQueries({ queryKey: ['orders'] })
              queryClient.invalidateQueries({ queryKey: ['portfolio'] })
            }}
          />
        </div>
      </div>

      {/* ─── Bottom Panel ─── */}
      <div className="shrink-0 border-t border-quant-border bg-quant-bg-secondary flex flex-col" style={{height:bottomCollapsed?'auto':bottomHeight}}>
        <div className="h-1.5 cursor-row-resize hover:bg-quant-gold/20 active:bg-quant-gold/30 shrink-0 relative" onMouseDown={e=>{
          dragRef.current={startY:e.clientY,startH:bottomHeight}
          const onMove=(ev:MouseEvent)=>{
            if(!dragRef.current) return
            const h=Math.max(60,Math.min(600,dragRef.current.startH-(ev.clientY-dragRef.current.startY)))
            setBottomHeight(h)
          }
          const onUp=()=>{dragRef.current=null; document.removeEventListener('mousemove',onMove); document.removeEventListener('mouseup',onUp)}
          document.addEventListener('mousemove',onMove); document.addEventListener('mouseup',onUp)
        }}>
          <div className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 w-8 h-0.5 rounded bg-quant-border/60"/>
        </div>
        <TradeHistory
          activeTab={activeBottomTab}
          onTabChange={(tab) => { setActiveBottomTab(tab); setBottomHeight(h => Math.max(h, 180)) }}
          holdingsList={holdingsList}
          orders={orders}
          ordersLoading={ordersLoading}
          historyOrders={historyOrders}
          historyLoading={historyLoading}
          fillTrades={fillTrades}
          fillsLoading={fillsLoading}
          allBalances={allBalances}
          balLoading={balLoading}
          onCancelOrder={handleCancelOrder}
        />
      </div>
      <ToastContainer />
    </div>
  )
}
