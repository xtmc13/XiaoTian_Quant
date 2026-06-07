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
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { ErrorBoundary } from '@/components/ErrorBoundary'
import {
  parseInterval, formatPrice, formatTime, formatDateTime,
  StatusTag, SPOT_WATCHLIST,
} from '@/lib/tradingHelpers'
import { getPrecision } from '@/lib/tradingPrecision'
import type { Trade, Order, TickerSnapshot } from '@/types'
import type { ChartApi } from '@/lib/tradingHelpers'
import {
  Search, TrendingUp, Clock, XCircle,
  CheckCircle2, Activity, ChevronUp, ChevronDown, Star
} from 'lucide-react'

const WATCHLIST = SPOT_WATCHLIST

/* Extended types for fields not yet in base definitions */
interface HistoryOrder extends Order {
  updated_at?: string
  avg_price?: number
  filled_quantity?: number
  realized_pnl?: number
}

interface FillTrade extends Trade {
  created_at?: string
  timestamp?: number
  avg_price?: number
  filled_quantity?: number
  fee?: number
}

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
  const [side, setSide] = useState<'BUY'|'SELL'>('BUY')
  const [orderType, setOrderType] = useState<'LIMIT'|'MARKET'>('LIMIT')
  const [price, setPrice] = useState('')
  const [quantity, setQuantity] = useState('')
  const [obPrecision, setObPrecision] = useState('0.1')
  const [activeBottomTab, setActiveBottomTab] = useState<'positions'|'orders'|'history'|'fills'|'assets'>('positions')
  const [bottomHeight, setBottomHeight] = useState(0)
  const bottomCollapsed = bottomHeight < 20
  const dragRef = useRef<{startY:number;startH:number}|null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [watchlistSearch, setWatchlistSearch] = useState('')
  const [tpPrice, setTpPrice] = useState('')
  const [slPrice, setSlPrice] = useState('')
  const [showTpSl, setShowTpSl] = useState(false)

  // ── 新增：下单增强功能 ──
  const [amountMode, setAmountMode] = useState<'quantity'|'amount'>('quantity') // 数量/金额模式
  const [amountValue, setAmountValue] = useState('') // 金额输入值（USDT）
  const [sliderValue, setSliderValue] = useState(0) // 滑块值 0-100
  const [timeInForce, setTimeInForce] = useState<'GTC'|'IOC'|'FOK'>('GTC') // 订单有效期
  const [postOnly, setPostOnly] = useState(false) // 只做 Maker
  const [slippage, setSlippage] = useState('0.5') // 滑点容忍度（%）
  const [showAdvanced, setShowAdvanced] = useState(false) // 高级设置展开

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
  const totalEstUsdt = useMemo(()=>{
    if(!portfolio) return 0
    return parseFloat(String(portfolio.total_equity ?? 0)) || 0
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
  const isUp = change>=0
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
  const handlePlaceOrder = useCallback(async ()=>{
    // 根据模式计算实际数量
    let qty: number
    if (amountMode === 'amount') {
      // 金额模式：根据金额计算数量
      const amount = parseFloat(amountValue)
      if (!amount || amount <= 0) { toast('error', '请输入有效金额'); return }
      const calcPrice = orderType === 'MARKET' ? lastPrice : (parseFloat(price) || lastPrice)
      if (!calcPrice || calcPrice <= 0) { toast('error', '无法获取有效价格'); return }
      qty = amount / calcPrice
    } else {
      // 数量模式
      qty = parseFloat(quantity)
      if (!qty || qty <= 0) { toast('error', '请输入有效数量'); return }
    }
    
    if (orderType === 'LIMIT') {
      const p = parseFloat(price)
      if (!p || p <= 0) { toast('error', '请输入有效价格'); return }
    }
    const finalPrice = orderType==='MARKET' ? 0 : (parseFloat(price) || 0)
    setSubmitting(true)
    try{
      await orderApi.place({
        symbol, side, order_type: orderType,
        price: finalPrice, quantity: qty,
        market_type: 'spot',
        tp_price: tpPrice ? parseFloat(tpPrice) : undefined,
        sl_price: slPrice ? parseFloat(slPrice) : undefined,
        time_in_force: timeInForce,
        post_only: postOnly,
        slippage: orderType === 'MARKET' ? parseFloat(slippage) / 100 : undefined,
      })
      toast('success', '订单已提交')
      setQuantity(''); setPrice(''); setAmountValue(''); setSliderValue(0)
      queryClient.invalidateQueries({queryKey:['orders']})
      queryClient.invalidateQueries({queryKey:['portfolio']})
    }catch(e: unknown){
      const err = e instanceof Error ? e : new Error(String(e))
      toast('error', err.message || '下单失败')
    }finally{ setSubmitting(false) }
  },[symbol,side,orderType,price,quantity,amountMode,amountValue,lastPrice,tpPrice,slPrice,timeInForce,postOnly,slippage,queryClient])

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

  /* preview */
  const preview = useMemo(()=>{
    // 根据模式计算数量
    let qty: number
    if (amountMode === 'amount') {
      const amount = parseFloat(amountValue) || 0
      const calcPrice = orderType === 'MARKET' ? lastPrice : (parseFloat(price) || lastPrice)
      qty = calcPrice > 0 ? amount / calcPrice : 0
    } else {
      qty = parseFloat(quantity) || 0
    }
    const pr=orderType==='MARKET'?lastPrice:(parseFloat(price)||lastPrice)
    const notional=qty*pr
    const feeRate=0.0005
    return {notional, fee:notional*feeRate, qty}
  },[quantity,amountMode,amountValue,price,lastPrice,orderType])

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
          onPriceClick={(price) => setPrice(price)}
          recentTrades={recentTrades}
          liveTrades={liveTrades}
          lastPrice={lastPrice}
          bestBid={bestBid}
          bestAsk={bestAsk}
          symbol={symbol}
        />

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
          <div className="flex-1 p-3 flex flex-col gap-3 overflow-y-auto">
            {/* 订单类型切换 */}
            <div className="flex gap-1 bg-quant-bg p-0.5 rounded">
              {(['LIMIT','MARKET'] as const).map(t=>(
                <button key={t} onClick={()=>setOrderType(t)} className={cn("flex-1 py-1.5 text-[11px] font-medium rounded transition-colors", orderType===t?"bg-quant-bg-secondary text-foreground":"text-muted-foreground hover:text-foreground")}>{t==='LIMIT'?'限价':'市价'}</button>
              ))}
              <button onClick={()=>setShowTpSl(!showTpSl)} className={cn("flex-1 py-1.5 text-[11px] rounded transition-colors", showTpSl?"bg-quant-bg-secondary text-foreground":"text-muted-foreground hover:text-foreground")}>止盈止损</button>
              <button onClick={()=>setShowAdvanced(!showAdvanced)} className={cn("flex-1 py-1.5 text-[11px] rounded transition-colors", showAdvanced?"bg-quant-bg-secondary text-foreground":"text-muted-foreground hover:text-foreground")}>高级</button>
            </div>

            {/* 方向选择 - 买入/卖出 */}
            <div className="flex gap-1.5">
              <button onClick={()=>setSide('BUY')} className={cn("flex-1 py-2.5 text-sm font-bold rounded-lg transition-all duration-200", side==='BUY'?"bg-[#0ECB81] hover:bg-[#0ECB81]/90 text-black shadow-lg shadow-[#0ECB81]/20":"bg-quant-bg hover:bg-[#0ECB81]/10 text-muted-foreground border border-quant-border hover:border-[#0ECB81]/50")}>买入</button>
              <button onClick={()=>setSide('SELL')} className={cn("flex-1 py-2.5 text-sm font-bold rounded-lg transition-all duration-200", side==='SELL'?"bg-[#F6465D] hover:bg-[#F6465D]/90 text-white shadow-lg shadow-[#F6465D]/20":"bg-quant-bg hover:bg-[#F6465D]/10 text-muted-foreground border border-quant-border hover:border-[#F6465D]/50")}>卖出</button>
            </div>

            {/* 价格输入 + 快捷按钮 */}
            {orderType==='LIMIT'&&(
              <div className="flex flex-col gap-1.5">
                <div className="flex justify-between text-[10px] text-muted-foreground"><span>价格</span><span>USDT</span></div>
                <div className="flex flex-col gap-1">
                  <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-10 focus-within:border-quant-gold transition-all">
                    <input 
                      value={price} 
                      onChange={e=>setPrice(e.target.value)} 
                      placeholder={lastPrice?lastPrice.toFixed(precision.price):'0'.padEnd(precision.price+2, '0')} 
                      aria-label="价格" 
                      className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"/>
                    <span className="text-[10px] text-muted-foreground ml-2">USDT</span>
                  </div>
                  {/* 价格快捷按钮 */}
                  <div className="flex gap-1">
                    <button onClick={() => { if(lastPrice) setPrice((lastPrice * 0.99).toFixed(precision.price)) }} className="flex-1 py-1 text-[10px] text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border rounded hover:border-quant-gold/50 transition-colors">-1%</button>
                    <button onClick={() => { if(lastPrice) setPrice((lastPrice * 0.995).toFixed(precision.price)) }} className="flex-1 py-1 text-[10px] text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border rounded hover:border-quant-gold/50 transition-colors">-0.5%</button>
                    <button onClick={() => { if(lastPrice) setPrice(lastPrice.toFixed(precision.price)) }} className="flex-1 py-1 text-[10px] text-quant-gold hover:text-quant-gold/80 bg-quant-bg border border-quant-gold/30 rounded hover:bg-quant-gold/10 transition-colors font-medium">最新价</button>
                    <button onClick={() => { if(lastPrice) setPrice((lastPrice * 1.005).toFixed(precision.price)) }} className="flex-1 py-1 text-[10px] text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border rounded hover:border-quant-gold/50 transition-colors">+0.5%</button>
                    <button onClick={() => { if(lastPrice) setPrice((lastPrice * 1.01).toFixed(precision.price)) }} className="flex-1 py-1 text-[10px] text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border rounded hover:border-quant-gold/50 transition-colors">+1%</button>
                  </div>
                </div>
              </div>
            )}

            {/* 数量/金额输入 - 支持单位切换 */}
            <div className="flex flex-col gap-1.5">
              <div className="flex justify-between items-center text-[10px] text-muted-foreground">
                <span>{amountMode === 'quantity' ? '数量' : '金额'}</span>
                <div className="flex gap-1">
                  <button 
                    onClick={() => setAmountMode('quantity')} 
                    className={cn("px-2 py-0.5 rounded text-[10px] transition-colors", amountMode === 'quantity' ? "bg-quant-gold/20 text-quant-gold" : "hover:bg-white/5")}
                  >
                    {symbol.replace('USDT','')}
                  </button>
                  <button 
                    onClick={() => setAmountMode('amount')} 
                    className={cn("px-2 py-0.5 rounded text-[10px] transition-colors", amountMode === 'amount' ? "bg-quant-gold/20 text-quant-gold" : "hover:bg-white/5")}
                  >
                    USDT
                  </button>
                </div>
              </div>
              
              {amountMode === 'quantity' ? (
                // 数量模式
                <>
                  <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-10 focus-within:border-quant-gold transition-all">
                    <input 
                      value={quantity} 
                      onChange={e=>{setQuantity(e.target.value); setSliderValue(0);}} 
                      placeholder={'0'.padEnd(precision.quantity+2, '0')} 
                      aria-label="数量" 
                      className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"/>
                    <span className="text-[10px] text-muted-foreground ml-2">{symbol.replace('USDT','')}</span>
                  </div>
                </>
              ) : (
                // 金额模式（USDT）
                <>
                  <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-10 focus-within:border-quant-gold transition-all">
                    <input 
                      value={amountValue} 
                      onChange={e=>{setAmountValue(e.target.value); setSliderValue(0);}} 
                      placeholder="0.00" 
                      aria-label="金额" 
                      className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"/>
                    <span className="text-[10px] text-muted-foreground ml-2">USDT</span>
                  </div>
                  {/* 显示对应的数量 */}
                  {(() => {
                    const calcPrice = orderType === 'MARKET' ? lastPrice : (parseFloat(price) || lastPrice)
                    const amount = parseFloat(amountValue) || 0
                    const qty = calcPrice > 0 ? amount / calcPrice : 0
                    return qty > 0 ? (
                      <div className="text-[10px] text-muted-foreground text-right">
                        ≈ {qty.toFixed(precision.quantity)} {symbol.replace('USDT','')}
                      </div>
                    ) : null
                  })()}
                </>
              )}

              {/* 滑块选择器 */}
              <div className="flex flex-col gap-1">
                <input
                  type="range"
                  min="0"
                  max="100"
                  step="1"
                  value={sliderValue}
                  onChange={(e) => {
                    const val = parseInt(e.target.value)
                    setSliderValue(val)
                    const calcPrice = orderType === 'MARKET' ? lastPrice : (parseFloat(price) || lastPrice)
                    const pct = val / 100
                    
                    if (amountMode === 'amount') {
                      // 金额模式
                      const amount = spotBalance * pct
                      setAmountValue(amount > 0 ? amount.toFixed(2) : '')
                    } else {
                      // 数量模式
                      const baseAsset = symbol.replace('USDT', '')
                      const assetHolding = holdingsList.find((b: unknown) => {
                        const bal = b as Record<string, unknown>
                        return String(bal.asset || bal.currency) === baseAsset
                      })
                      const assetFree = assetHolding ? parseFloat(String((assetHolding as Record<string, unknown>).free ?? (assetHolding as Record<string, unknown>).available ?? 0)) : 0
                      const calcQty = side === 'BUY'
                        ? (calcPrice > 0 ? (spotBalance * pct) / calcPrice : 0)
                        : (assetFree * pct)
                      setQuantity(calcQty > 0 ? calcQty.toFixed(precision.quantity) : '')
                    }
                  }}
                  className="w-full h-1 bg-quant-border rounded-lg appearance-none cursor-pointer [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:w-3 [&::-webkit-slider-thumb]:h-3 [&::-webkit-slider-thumb]:bg-quant-gold [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:cursor-pointer"
                />
                <div className="flex justify-between text-[9px] text-muted-foreground">
                  <span>0%</span>
                  <span>{sliderValue}%</span>
                  <span>100%</span>
                </div>
              </div>

              {/* 百分比快捷按钮 */}
              <div className="flex gap-1">
                {(() => {
                  const baseAsset = symbol.replace('USDT', '')
                  const assetHolding = holdingsList.find((b: unknown) => {
                    const bal = b as Record<string, unknown>
                    return String(bal.asset || bal.currency) === baseAsset
                  })
                  const assetFree = assetHolding ? parseFloat(String((assetHolding as Record<string, unknown>).free ?? (assetHolding as Record<string, unknown>).available ?? 0)) : 0
                  return [0.25, 0.5, 0.75, 1].map((pct) => {
                    const pctLabel = Math.round(pct * 100) + '%'
                    return (
                      <button 
                        key={pctLabel} 
                        onClick={() => {
                          setSliderValue(Math.round(pct * 100))
                          const calcPrice = orderType === 'MARKET' ? lastPrice : (parseFloat(price) || lastPrice)
                          
                          if (amountMode === 'amount') {
                            const amount = spotBalance * pct
                            setAmountValue(amount > 0 ? amount.toFixed(2) : '')
                          } else {
                            const calcQty = side === 'BUY'
                              ? (calcPrice > 0 ? (spotBalance * pct) / calcPrice : 0)
                              : (assetFree * pct)
                            setQuantity(calcQty > 0 ? calcQty.toFixed(precision.quantity) : '')
                          }
                        }} 
                        className={cn(
                          "flex-1 py-1.5 text-[10px] font-medium rounded-lg transition-all",
                          sliderValue === Math.round(pct * 100)
                            ? "bg-quant-gold/20 text-quant-gold border border-quant-gold/50"
                            : "text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border hover:border-quant-gold/50"
                        )}
                      >
                        {pctLabel}
                      </button>
                    )
                  })
                })()}
              </div>
            </div>

            {/* 高级设置 */}
            {showAdvanced && (
              <div className="flex flex-col gap-2 p-2 bg-quant-bg/50 rounded-lg border border-quant-border/50">
                {/* 订单有效期 */}
                {orderType === 'LIMIT' && (
                  <div className="flex flex-col gap-1.5">
                    <span className="text-[10px] text-muted-foreground">订单有效期</span>
                    <div className="flex gap-1">
                      {(['GTC', 'IOC', 'FOK'] as const).map(t => (
                        <button 
                          key={t} 
                          onClick={() => setTimeInForce(t)} 
                          className={cn(
                            "flex-1 py-1 text-[10px] rounded transition-colors",
                            timeInForce === t 
                              ? "bg-quant-gold/20 text-quant-gold border border-quant-gold/50" 
                              : "text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border"
                          )}
                        >
                          {t === 'GTC' ? '一直有效' : t === 'IOC' ? '立即成交' : '全部成交'}
                        </button>
                      ))}
                    </div>
                    <div className="text-[9px] text-muted-foreground">
                      {timeInForce === 'GTC' && '订单会一直有效，直到被成交或取消'}
                      {timeInForce === 'IOC' && '订单必须立即成交，未成交部分会被取消'}
                      {timeInForce === 'FOK' && '订单必须全部立即成交，否则会被取消'}
                    </div>
                  </div>
                )}

                {/* 只做 Maker */}
                {orderType === 'LIMIT' && (
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input 
                      type="checkbox" 
                      checked={postOnly} 
                      onChange={e => setPostOnly(e.target.checked)} 
                      className="w-3 h-3 accent-quant-gold"
                    />
                    <span className="text-[10px] text-muted-foreground">只做 Maker（Post-Only）</span>
                    <span className="text-[9px] text-muted-foreground/60">确保订单只作为挂单成交</span>
                  </label>
                )}

                {/* 市价单滑点 */}
                {orderType === 'MARKET' && (
                  <div className="flex flex-col gap-1.5">
                    <span className="text-[10px] text-muted-foreground">滑点容忍度</span>
                    <div className="flex items-center gap-2">
                      <div className="flex gap-1">
                        {['0.1', '0.5', '1', '2'].map(s => (
                          <button 
                            key={s} 
                            onClick={() => setSlippage(s)} 
                            className={cn(
                              "px-2 py-1 text-[10px] rounded transition-colors",
                              slippage === s 
                                ? "bg-quant-gold/20 text-quant-gold border border-quant-gold/50" 
                                : "text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border"
                            )}
                          >
                            {s}%
                          </button>
                        ))}
                      </div>
                      <input 
                        value={slippage} 
                        onChange={e => setSlippage(e.target.value)} 
                        className="w-16 px-2 py-1 text-[10px] bg-quant-bg border border-quant-border rounded text-foreground"
                        placeholder="0.5"
                      />
                      <span className="text-[10px] text-muted-foreground">%</span>
                    </div>
                  </div>
                )}
              </div>
            )}

            {/* TP/SL 设置 */}
            {showTpSl&&(
              <div className="flex flex-col gap-2 p-2 bg-quant-bg/50 rounded-lg border border-quant-border/50">
                <div className="flex items-center justify-between text-[10px] text-muted-foreground mb-1">
                  <span>止盈止损</span>
                  <button onClick={() => {
                    if (lastPrice) {
                      setTpPrice((lastPrice * (side === 'BUY' ? 1.02 : 0.98)).toFixed(precision.price))
                      setSlPrice((lastPrice * (side === 'BUY' ? 0.98 : 1.02)).toFixed(precision.price))
                    }
                  }} className="text-quant-gold hover:text-quant-gold/80 transition-colors">智能设置</button>
                </div>
                <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-9 focus-within:border-quant-gold transition-all">
                  <span className="text-[10px] text-[#0ECB81] w-6">止盈</span>
                  <input value={tpPrice} onChange={e=>setTpPrice(e.target.value)} placeholder="--" aria-label="止盈价格" className="flex-1 bg-transparent text-xs font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"/>
                  <span className="text-[10px] text-muted-foreground ml-1">USDT</span>
                </div>
                <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-9 focus-within:border-quant-gold transition-all">
                  <span className="text-[10px] text-[#F6465D] w-6">止损</span>
                  <input value={slPrice} onChange={e=>setSlPrice(e.target.value)} placeholder="--" aria-label="止损价格" className="flex-1 bg-transparent text-xs font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"/>
                  <span className="text-[10px] text-muted-foreground ml-1">USDT</span>
                </div>
              </div>
            )}

            {/* 账户信息 */}
            <div className="space-y-1.5 text-[10px]">
              <div className="flex justify-between text-muted-foreground">
                <span>可用</span>
                <span className="font-mono text-foreground">{spotBalance.toFixed(2)} USDT</span>
              </div>
              <div className="flex justify-between text-muted-foreground">
                <span>成交额</span>
                <span className="font-mono text-foreground">{preview.notional>0?preview.notional.toFixed(2):'--'} USDT</span>
              </div>
              <div className="flex justify-between text-muted-foreground">
                <span>手续费</span>
                <span className="font-mono text-foreground">{preview.fee>0?preview.fee.toFixed(4):'--'} USDT</span>
              </div>
            </div>

            {/* 主要下单按钮 */}
            <button onClick={handlePlaceOrder} disabled={submitting} className={cn(
              "w-full py-3 rounded-lg text-sm font-bold transition-all duration-200 shadow-lg",
              submitting&&"opacity-60 cursor-not-allowed",
              side==='BUY'
                ?"bg-[#0ECB81] hover:bg-[#0ECB81]/90 active:scale-[0.98] text-black"
                :"bg-[#F6465D] hover:bg-[#F6465D]/90 active:scale-[0.98] text-white"
            )}>
              {submitting?'提交中...':`${side==='BUY'?'买入':'卖出'} ${symbol.replace('USDT','')}`}
            </button>

            {/* 快捷下单按钮 - 25%/50%/75%/100% */}
            <div className="grid grid-cols-4 gap-1.5">
              {(() => {
                const baseAsset = symbol.replace('USDT', '')
                const assetHolding = holdingsList.find((b: unknown) => {
                  const bal = b as Record<string, unknown>
                  return String(bal.asset || bal.currency) === baseAsset
                })
                const assetFree = assetHolding ? parseFloat(String((assetHolding as Record<string, unknown>).free ?? (assetHolding as Record<string, unknown>).available ?? 0)) : 0
                return [0.25, 0.5, 0.75, 1].map((pct) => {
                  const calcPrice = orderType === 'MARKET' ? lastPrice : (parseFloat(price) || lastPrice)
                  const calcQty = side === 'BUY'
                    ? (calcPrice > 0 ? (spotBalance * pct) / calcPrice : 0)
                    : (assetFree * pct)
                  const quickOrder = async () => {
                    if (!calcQty || calcQty <= 0) {
                      toast('error', side === 'BUY' ? '余额不足' : '持仓不足')
                      return
                    }
                    setSubmitting(true)
                    try {
                      await orderApi.place({
                        symbol, 
                        side, 
                        order_type: orderType,
                        price: orderType === 'MARKET' ? 0 : (parseFloat(price) || 0), 
                        quantity: calcQty,
                        market_type: 'spot',
                        time_in_force: timeInForce,
                        post_only: postOnly,
                        slippage: orderType === 'MARKET' ? parseFloat(slippage) / 100 : undefined,
                      })
                      toast('success', `${side === 'BUY' ? '买入' : '卖出'} ${calcQty.toFixed(precision.quantity)} ${symbol.replace('USDT','')}`)
                      queryClient.invalidateQueries({queryKey:['orders']})
                      queryClient.invalidateQueries({queryKey:['portfolio']})
                    } catch (e: unknown) {
                      const err = e instanceof Error ? e : new Error(String(e))
                      toast('error', err.message || '下单失败')
                    } finally {
                      setSubmitting(false)
                    }
                  }
                  return (
                    <button 
                      key={pct} 
                      onClick={quickOrder}
                      disabled={submitting}
                      className={cn(
                        "py-2 text-[11px] font-bold rounded-lg transition-all duration-200 disabled:opacity-50",
                        side === 'BUY'
                          ? "bg-[#0ECB81]/10 hover:bg-[#0ECB81]/20 text-[#0ECB81] border border-[#0ECB81]/20 hover:border-[#0ECB81]/40"
                          : "bg-[#F6465D]/10 hover:bg-[#F6465D]/20 text-[#F6465D] border border-[#F6465D]/20 hover:border-[#F6465D]/40"
                      )}
                    >
                      {Math.round(pct * 100)}%
                    </button>
                  )
                })
              })()}
            </div>
          </div>
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
        <div className="flex border-b border-quant-border px-2 items-center justify-between shrink-0">
          <div className="flex">
            {([
              {key:'positions',label:'持有币种',count:holdingsList.length||0,icon:TrendingUp},
              {key:'orders',label:'当前委托',count:orders?.length||0,icon:Clock},
              {key:'history',label:'历史委托',count:historyOrders?.length||0,icon:XCircle},
              {key:'fills',label:'成交记录',count:fillTrades?.length||0,icon:CheckCircle2},
              {key:'assets',label:'资产',count:holdingsList.length||0,icon:Activity},
            ] as const).map(t=>{
              return <button key={t.key} onClick={()=>{setActiveBottomTab(t.key); setBottomHeight(h=>Math.max(h,180))}} className={cn('px-4 py-2 text-xs font-medium transition-colors relative flex items-center gap-1.5', activeBottomTab===t.key?'text-quant-gold':'text-muted-foreground hover:text-foreground')}>
                <t.icon className="w-3.5 h-3.5"/>{t.label}
                {t.count>0&&<span className={cn('ml-1 px-1.5 py-0 rounded-full text-[10px] font-bold', activeBottomTab===t.key?'bg-quant-gold/20 text-quant-gold':'bg-quant-bg-tertiary text-muted-foreground')}>{t.count}</span>}
                {activeBottomTab===t.key&&<span className="absolute bottom-0 left-0 right-0 h-0.5 bg-quant-gold"/>}
              </button>
            })}
          </div>
          <button onClick={()=>setBottomHeight(h=>h<20?180:0)} className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5 transition-colors" title={bottomCollapsed?'展开':'收起'}>
            {bottomCollapsed?<ChevronUp className="w-3.5 h-3.5"/>:<ChevronDown className="w-3.5 h-3.5"/>}
          </button>
        </div>
        {!bottomCollapsed&&(
          <div className="overflow-y-auto flex-1" style={{maxHeight:bottomHeight-40}}>
            {activeBottomTab==='positions'&&(
              <div className="overflow-x-auto" key="tab-positions">
                {holdingsList.length?(
                  <table className="w-full text-[11px] whitespace-nowrap">
                    <thead className="sticky top-0 bg-quant-bg-secondary z-10">
                      <tr className="text-muted-foreground border-b border-quant-border">
                        <th scope="col" className="text-left font-medium px-3 py-2">币种</th>
                        <th scope="col" className="text-right font-medium px-3 py-2">可用</th>
                        <th scope="col" className="text-right font-medium px-3 py-2">冻结</th>
                        <th scope="col" className="text-right font-medium px-3 py-2">总量</th>
                      </tr>
                    </thead>
                    <tbody>
                      {holdingsList.map((b: unknown, i: number)=>{
                        const bal = b as Record<string, unknown>
                        const free = parseFloat(String(bal.free??bal.available??0)) || 0
                        const locked = parseFloat(String(bal.locked??bal.frozen??0)) || 0
                        return (
                          <tr key={String(bal.asset||bal.currency||i)} className="border-b border-quant-border/40 hover:bg-white/[0.03]">
                            <td className="px-3 py-2.5 font-medium">{String(bal.asset||bal.currency||'--')}</td>
                            <td className="px-3 py-2.5 text-right font-mono">{free.toFixed(6)}</td>
                            <td className="px-3 py-2.5 text-right font-mono">{locked>0?locked.toFixed(6):'--'}</td>
                            <td className="px-3 py-2.5 text-right font-mono">{(free+locked).toFixed(6)}</td>
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                ):<div className="py-8 text-center text-muted-foreground text-xs">暂无持仓</div>}
              </div>
            )}
            {activeBottomTab==='orders'&&(
              <div key="tab-orders">
                {ordersLoading?(
                  <div className="p-4 space-y-2">{Array.from({length:4}).map((_,i)=><Skeleton key={i} variant="text" height={32}/>)}</div>
                ):orders?.length?(
                  <div className="overflow-x-auto">
                    <table className="w-full text-[11px] whitespace-nowrap">
                      <thead className="sticky top-0 bg-quant-bg-secondary z-10"><tr className="text-muted-foreground text-left"><th scope="col" className="px-1.5 py-1 font-medium">时间</th><th scope="col" className="px-1.5 py-1 font-medium">币种</th><th scope="col" className="px-1.5 py-1 font-medium">方向</th><th scope="col" className="px-1.5 py-1 font-medium">类型</th><th scope="col" className="px-1.5 py-1 font-medium">价格</th><th scope="col" className="px-1.5 py-1 font-medium">数量</th><th scope="col" className="px-1.5 py-1 font-medium">状态</th><th scope="col" className="px-1.5 py-1 font-medium">操作</th></tr></thead>
                      <tbody>
                        {(orders||[]).map((o: Order)=>{
                          return <tr key={o.id} className="border-t border-quant-border/40 hover:bg-white/[0.02]">
                            <td className="px-1.5 py-1 text-muted-foreground">{formatDateTime(o.created_at)}</td>
                            <td className="px-1.5 py-1 font-semibold">{o.symbol}</td>
                            <td className="px-1.5 py-1"><span className={cn('text-[9px] font-bold', o.side==='BUY'?'text-[#0ECB81]':'text-[#F6465D]')}>{o.side==='BUY'?'买入':'卖出'}</span></td>
                            <td className="px-1.5 py-1 text-muted-foreground">{o.type}</td>
                            <td className="px-1.5 py-1 font-mono">${formatPrice(o.price,2)}</td>
                            <td className="px-1.5 py-1 font-mono">{formatPrice(o.quantity,4)}</td>
                            <td className="px-1.5 py-1"><StatusTag status={o.status}/></td>
                            <td className="px-1.5 py-1"><button onClick={()=>handleCancelOrder(o.id)} className="px-1.5 py-0.5 bg-[#F6465D]/10 text-[#F6465D] rounded text-[9px] font-medium hover:bg-[#F6465D]/20 transition-colors flex items-center gap-1"><XCircle className="w-3 h-3"/>取消</button></td>
                          </tr>
                        })}
                      </tbody>
                    </table>
                  </div>
                ):<div className="py-6 flex items-center justify-center"><EmptyState title="暂无委托" description="当前没有进行中的委托订单" className="py-1 border-0 text-[10px] [&>div:first-child]:hidden"/></div>}
              </div>
            )}
            {activeBottomTab==='history'&&(
              <div key="tab-history">
                {historyLoading?(
                  <div className="p-4 space-y-2">{Array.from({length:4}).map((_,i)=><Skeleton key={i} variant="text" height={32}/>)}</div>
                ):historyOrders?.length?(
                  <div className="overflow-x-auto">
                    <table className="w-full text-[11px] whitespace-nowrap">
                      <thead className="sticky top-0 bg-quant-bg-secondary z-10"><tr className="text-muted-foreground text-left"><th scope="col" className="px-1.5 py-1 font-medium">时间</th><th scope="col" className="px-1.5 py-1 font-medium">币种</th><th scope="col" className="px-1.5 py-1 font-medium">方向</th><th scope="col" className="px-1.5 py-1 font-medium">价格</th><th scope="col" className="px-1.5 py-1 font-medium">数量</th><th scope="col" className="px-1.5 py-1 font-medium">盈亏</th><th scope="col" className="px-1.5 py-1 font-medium">状态</th></tr></thead>
                      <tbody>
                        {(historyOrders||[]).map((o: HistoryOrder)=>{
                          const pnl=o.realized_pnl||0
                          return <tr key={o.id} className="border-t border-quant-border/40 hover:bg-white/[0.02]">
                            <td className="px-1.5 py-1 text-muted-foreground">{formatDateTime(o.updated_at||o.created_at)}</td>
                            <td className="px-1.5 py-1 font-semibold">{o.symbol}</td>
                            <td className="px-1.5 py-1"><span className={cn('text-[9px] font-bold', o.side==='BUY'?'text-[#0ECB81]':'text-[#F6465D]')}>{o.side==='BUY'?'买入':'卖出'}</span></td>
                            <td className="px-1.5 py-1 font-mono">${formatPrice(o.avg_price||o.price,2)}</td>
                            <td className="px-1.5 py-1 font-mono">{formatPrice(o.filled_quantity,4)}</td>
                            <td className={cn('px-1.5 py-1 font-mono font-bold', pnl>=0?'text-[#0ECB81]':'text-[#F6465D]')}>{pnl>=0?'+':''}{pnl.toFixed(2)}</td>
                            <td className="px-1.5 py-1"><StatusTag status={o.status}/></td>
                          </tr>
                        })}
                      </tbody>
                    </table>
                  </div>
                ):<div className="py-6 flex items-center justify-center"><EmptyState title="暂无历史成交" description="还没有已成交的订单记录" className="py-1 border-0 text-[10px] [&>div:first-child]:hidden"/></div>}
              </div>
            )}
            {activeBottomTab==='fills'&&(
              <div key="tab-fills">
                {fillsLoading?(
                  <div className="p-4 space-y-2">{Array.from({length:4}).map((_,i)=><Skeleton key={i} variant="text" height={32}/>)}</div>
                ):fillTrades?.length?(
                  <div className="overflow-x-auto">
                    <table className="w-full text-[11px] whitespace-nowrap">
                      <thead className="sticky top-0 bg-quant-bg-secondary z-10"><tr className="text-muted-foreground text-left"><th scope="col" className="px-1.5 py-1 font-medium">时间</th><th scope="col" className="px-1.5 py-1 font-medium">币种</th><th scope="col" className="px-1.5 py-1 font-medium">方向</th><th scope="col" className="px-1.5 py-1 font-medium">价格</th><th scope="col" className="px-1.5 py-1 font-medium">数量</th><th scope="col" className="px-1.5 py-1 font-medium">手续费</th></tr></thead>
                      <tbody>
                        {(fillTrades||[]).map((t: FillTrade, i: number)=>{
                          return <tr key={t.id||i} className="border-t border-quant-border/40 hover:bg-white/[0.02]">
                            <td className="px-1.5 py-1 text-muted-foreground">{formatDateTime(t.time || t.created_at || t.timestamp || 0)}</td>
                            <td className="px-1.5 py-1 font-semibold">{t.symbol||symbol}</td>
                            <td className="px-1.5 py-1"><span className={cn('text-[9px] font-bold', t.side==='buy'?'text-[#0ECB81]':'text-[#F6465D]')}>{t.side==='buy'?'买入':'卖出'}</span></td>
                            <td className="px-1.5 py-1 font-mono">${formatPrice(t.price||t.avg_price,2)}</td>
                            <td className="px-1.5 py-1 font-mono">{formatPrice(t.quantity||t.filled_quantity,4)}</td>
                            <td className="px-1.5 py-1 font-mono text-muted-foreground">{t.fee?formatPrice(t.fee,4):'--'}</td>
                          </tr>
                        })}
                      </tbody>
                    </table>
                  </div>
                ):<div className="py-6 flex items-center justify-center"><EmptyState title="暂无成交记录" description="还没有成交记录" className="py-1 border-0 text-[10px] [&>div:first-child]:hidden"/></div>}
              </div>
            )}
            {activeBottomTab==='assets'&&(
              <div key="tab-assets">
                {balLoading?(
                  <div className="p-4 space-y-2">{Array.from({length:3}).map((_,i)=><Skeleton key={i} variant="text" height={32}/>)}</div>
                ):(()=>{
                  const raw = allBalances as Record<string, unknown>
                  const list = ((raw?.balances as unknown[]) || (raw?.currencies as unknown[]) || (raw?.list as unknown[]) || (Array.isArray(raw) ? raw : [])) as Record<string, unknown>[]
                  if(!Array.isArray(list) || !list.length) return <div className="py-6 flex items-center justify-center"><EmptyState title="暂无资产数据" description="等待资产数据加载..." className="py-1 border-0 text-[10px] [&>div:first-child]:hidden"/></div>
                  return (
                  <div className="overflow-x-auto">
                    <table className="w-full text-[11px] whitespace-nowrap">
                      <thead className="sticky top-0 bg-quant-bg-secondary z-10"><tr className="text-muted-foreground text-left"><th scope="col" className="px-3 py-2 font-medium">币种</th><th scope="col" className="text-right px-3 py-2 font-medium">可用</th><th scope="col" className="text-right px-3 py-2 font-medium">冻结</th><th scope="col" className="text-right px-3 py-2 font-medium">总计</th><th scope="col" className="text-right px-3 py-2 font-medium">估值(USDT)</th></tr></thead>
                      <tbody>
                        {list.map((b, i: number)=>{
                          const free = parseFloat(String(b.free ?? b.available ?? b.balance ?? 0))
                          const locked = parseFloat(String(b.locked ?? b.frozen ?? 0))
                          const total = free+locked
                          return <tr key={String(b.asset || b.symbol || i)} className="border-t border-quant-border/40 hover:bg-white/[0.02]">
                            <td className="px-3 py-2 font-semibold">{String(b.asset || b.symbol || '--')}</td>
                            <td className="px-3 py-2 text-right font-mono">{free.toFixed(4)}</td>
                            <td className="px-3 py-2 text-right font-mono">{locked>0?locked.toFixed(4):'--'}</td>
                            <td className="px-3 py-2 text-right font-mono">{total.toFixed(4)}</td>
                            <td className="px-3 py-2 text-right font-mono text-muted-foreground">--</td>
                          </tr>
                        })}
                      </tbody>
                    </table>
                  </div>
                )})()}
              </div>
            )}
          </div>
        )}
      </div>
      <ToastContainer />
    </div>
  )
}
