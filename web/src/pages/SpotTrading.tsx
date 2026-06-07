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
import { EmptyState } from '@/components/ui/EmptyState'
import { Skeleton } from '@/components/ui/Skeleton'
import {
  parseInterval, formatPrice, formatTime, formatDateTime,
  StatusTag, SPOT_WATCHLIST,
} from '@/lib/tradingHelpers'
import type { Trade, Order } from '@/types'
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

function obFormatPrice(p: number, precision: string): string {
  const tick = parseFloat(precision) || 0.1
  const decimals = precision.includes('.') ? precision.split('.')[1].length : 0
  return (Math.round(p / tick) * tick).toFixed(decimals)
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
  const {data:snapshot} = useQuery({queryKey:['snapshot',symbol], queryFn:()=>marketApi.snapshot(symbol), refetchInterval:5000})
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
    const qty = parseFloat(quantity)
    if (!qty || qty <= 0) { toast('error', '请输入有效数量'); return }
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
      })
      toast('success', '订单已提交')
      setQuantity(''); setPrice('')
      queryClient.invalidateQueries({queryKey:['orders']})
      queryClient.invalidateQueries({queryKey:['portfolio']})
    }catch(e: unknown){
      const err = e instanceof Error ? e : new Error(String(e))
      toast('error', err.message || '下单失败')
    }finally{ setSubmitting(false) }
  },[symbol,side,orderType,price,quantity,tpPrice,slPrice,queryClient])

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

  /* orderbook helpers */
  const obMax = useMemo(()=>{
    if(!orderbook) return 1
    const bidMax=Math.max(...(orderbook.bids||[]).map((b: [number, number])=>Number(b[1])||0),0)
    const askMax=Math.max(...(orderbook.asks||[]).map((a: [number, number])=>Number(a[1])||0),0)
    return Math.max(bidMax,askMax,1)
  },[orderbook])

  const displayTrades = useMemo(()=>{
    const src=liveTrades.length?liveTrades:(recentTrades||[])
    return src.slice(0,50)
  },[liveTrades,recentTrades])

  /* preview */
  const preview = useMemo(()=>{
    const qty=parseFloat(quantity)||0
    const pr=orderType==='MARKET'?lastPrice:(parseFloat(price)||lastPrice)
    const notional=qty*pr
    const feeRate=0.0005
    return {notional, fee:notional*feeRate}
  },[quantity,price,lastPrice,orderType])

  return (
    <div className="h-full flex flex-col">
      {/* MAIN GRID: Orderbook | Chart | Trade */}
      <div className="flex-1 grid grid-cols-[270px_1fr_310px] gap-px bg-quant-border min-h-0">

        {/* LEFT: Orderbook + Trades (270px) */}
        <div className="bg-quant-bg-secondary flex flex-col overflow-hidden min-h-0">
          <div className="h-8 shrink-0 border-b border-quant-border flex items-center justify-between px-3">
            <div className="flex gap-3">
              <span className="text-xs font-medium text-foreground">订单簿</span>
            </div>
            <div className="flex gap-1">
              {['0.1','1','10'].map(p=>(
                <span key={p} onClick={()=>setObPrecision(p)} className={cn("text-[10px] px-1 py-0.5 rounded cursor-pointer", obPrecision===p?"bg-quant-hover text-foreground":"text-muted-foreground hover:text-foreground")}>{p}</span>
              ))}
            </div>
          </div>
          <div className="flex text-[10px] text-muted-foreground px-3 py-1.5 border-b border-quant-border shrink-0">
            <span className="flex-1">价格 (USDT)</span>
            <span className="flex-1 text-right">数量</span>
            <span className="flex-1 text-right">累计</span>
          </div>
          <div className="flex-1 overflow-y-auto">
            {obLoading ? (
              <div className="p-3 space-y-1">{Array.from({length:12}).map((_,i)=><Skeleton key={i} variant="text" height={16}/>)}</div>
            ) : orderbook ? (
              <>
                <div className="flex flex-col-reverse">
                  {(orderbook.asks||[]).slice(0,10).map((ask: [number, number],i:number)=>{
                    const p=Number(ask[0]), q=Number(ask[1])
                    return (
                      <div key={"ask-"+i} className="relative flex px-3 py-0.5 text-[11px] font-mono cursor-pointer hover:bg-white/[0.04]" onClick={() => setPrice(obFormatPrice(p, obPrecision))}>
                        <div className="absolute top-0 bottom-0 right-0 opacity-20 z-0" style={{background:'#F6465D',width:Math.min((q/obMax)*100,100)+'%'}}/>
                        <span className="flex-1 text-quant-red relative z-10">{obFormatPrice(p,obPrecision)}</span>
                        <span className="flex-1 text-right text-muted-foreground relative z-10">{q.toFixed(4)}</span>
                        <span className="flex-1 text-right text-muted-foreground relative z-10">{(p*q).toFixed(2)}</span>
                      </div>
                    )
                  })}
                </div>
                <div className="flex items-center justify-center py-1.5 border-y border-quant-border bg-quant-bg-tertiary shrink-0">
                  <span className={cn("text-sm font-bold font-mono", isUp?"text-quant-green":"text-quant-red")}>
                    {lastPrice?lastPrice.toFixed(2):"--"}
                  </span>
                  <span className="text-[10px] text-muted-foreground ml-2">
                    spread {bestAsk&&bestBid?(parseFloat(bestAsk)-parseFloat(bestBid)).toFixed(2):"--"}
                  </span>
                </div>
                <div>
                  {(orderbook.bids||[]).slice(0,10).map((bid: [number, number],i:number)=>{
                    const p=Number(bid[0]), q=Number(bid[1])
                    return (
                      <div key={"bid-"+i} className="relative flex px-3 py-0.5 text-[11px] font-mono cursor-pointer hover:bg-white/[0.04]" onClick={() => setPrice(obFormatPrice(p, obPrecision))}>
                        <div className="absolute top-0 bottom-0 left-0 opacity-20 z-0" style={{background:'#2EBD85',width:Math.min((q/obMax)*100,100)+'%'}}/>
                        <span className="flex-1 text-quant-green relative z-10">{obFormatPrice(p,obPrecision)}</span>
                        <span className="flex-1 text-right text-muted-foreground relative z-10">{q.toFixed(4)}</span>
                        <span className="flex-1 text-right text-muted-foreground relative z-10">{(p*q).toFixed(2)}</span>
                      </div>
                    )
                  })}
                </div>
              </>
            ) : (
              <div className="py-8"><EmptyState title="暂无订单簿数据" description="等待市场数据连接..."/></div>
            )}
          </div>
          <div className="h-[150px] shrink-0 border-t border-quant-border overflow-y-auto" role="region" aria-label="最新成交">
            <div className="flex items-center h-7 px-3 border-b border-quant-border bg-quant-bg-secondary">
              <span className="text-[11px] font-medium text-muted-foreground">最新成交</span>
            </div>
            <div className="flex text-[10px] text-muted-foreground px-3 py-1 border-b border-quant-border sticky top-0 bg-quant-bg-secondary">
              <span className="flex-1">时间</span>
              <span className="flex-1 text-right">价格</span>
              <span className="flex-1 text-right">数量</span>
            </div>
            {displayTrades.slice(0,20).map((t: Trade, i: number)=>{
              return (
                <div key={t.id||i} className="flex px-3 py-0.5 text-[11px] font-mono">
                  <span className="flex-1 text-muted-foreground">{formatTime(t.time)}</span>
                  <span className={cn("flex-1 text-right", t.side==='buy'?"text-quant-green":"text-quant-red")}>{formatPrice(t.price)}</span>
                  <span className="flex-1 text-right text-muted-foreground">{t.quantity.toFixed(4)}</span>
                </div>
              )
            })}
            {!displayTrades.length && (
              <div className="py-4"><EmptyState title="暂无成交记录" description="等待实时成交数据..."/></div>
            )}
          </div>
        </div>

        {/* CHART (1fr) */}
        <div className="bg-quant-bg flex flex-col min-h-0 overflow-hidden">
          <div ref={chartRef} className="flex-1 min-h-0" />
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
          <div className="flex-1 p-3 flex flex-col gap-3">
            <div className="flex gap-1 bg-quant-bg p-0.5 rounded">
              {(['LIMIT','MARKET'] as const).map(t=>(
                <button key={t} onClick={()=>setOrderType(t)} className={cn("flex-1 py-1 text-[11px] font-medium rounded transition-colors", orderType===t?"bg-quant-bg-secondary text-foreground":"text-muted-foreground hover:text-foreground")}>{t==='LIMIT'?'限价':'市价'}</button>
              ))}
              <button onClick={()=>setShowTpSl(!showTpSl)} className={cn("flex-1 py-1 text-[11px] rounded transition-colors", showTpSl?"bg-quant-bg-secondary text-foreground":"text-muted-foreground hover:text-foreground")}>止盈止损</button>
            </div>
            <div className="flex gap-1">
              <button onClick={()=>setSide('BUY')} className={cn("flex-1 py-2 text-xs font-bold rounded transition-colors", side==='BUY'?"bg-[#0ECB81]/20 text-[#0ECB81] border border-[#0ECB81]/30":"bg-quant-bg text-muted-foreground border border-quant-border")}>买入</button>
              <button onClick={()=>setSide('SELL')} className={cn("flex-1 py-2 text-xs font-bold rounded transition-colors", side==='SELL'?"bg-[#F6465D]/20 text-[#F6465D] border border-[#F6465D]/30":"bg-quant-bg text-muted-foreground border border-quant-border")}>卖出</button>
            </div>
            {orderType==='LIMIT'&&(
              <div className="flex flex-col gap-1">
                <div className="flex justify-between text-[10px] text-muted-foreground"><span>价格</span><span>USDT</span></div>
                <div className="flex items-center bg-quant-bg border border-quant-border rounded px-2 h-8 focus-within:border-quant-gold transition-colors">
                  <input value={price} onChange={e=>setPrice(e.target.value)} placeholder={lastPrice?lastPrice.toFixed(2):'0.00'} aria-label="价格" className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"/>
                  <span className="text-[10px] text-muted-foreground">USDT</span>
                </div>
              </div>
            )}
            <div className="flex flex-col gap-1">
              <div className="flex justify-between text-[10px] text-muted-foreground"><span>数量</span><span>{symbol.replace('USDT','')}</span></div>
              <div className="flex items-center bg-quant-bg border border-quant-border rounded px-2 h-8 focus-within:border-quant-gold transition-colors">
                <input value={quantity} onChange={e=>setQuantity(e.target.value)} placeholder="0.00" aria-label="数量" className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"/>
                <span className="text-[10px] text-muted-foreground">{symbol.replace('USDT','')}</span>
              </div>
            </div>
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
                  const calcPrice = orderType === 'MARKET' ? lastPrice : (parseFloat(price) || lastPrice)
                  const calcQty = side === 'BUY'
                    ? (calcPrice > 0 ? (spotBalance * pct) / calcPrice : 0)
                    : (assetFree * pct)
                  return <button key={pctLabel} onClick={() => setQuantity(calcQty > 0 ? calcQty.toFixed(6) : '')} className="flex-1 py-1 text-[10px] text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border rounded hover:border-quant-gold/50 transition-colors">{pctLabel}</button>
                })
              })()}
            </div>
            <div className="flex items-center gap-2 mt-1">
              <label className="flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer">
                <input type="checkbox" checked={showTpSl} onChange={e=>setShowTpSl(e.target.checked)} className="w-3 h-3 accent-quant-gold" aria-label="显示止盈止损"/>
                止盈止损
              </label>
              <span className="text-[11px] text-muted-foreground">止盈/止损</span>
            </div>
            {showTpSl&&(
              <div className="flex flex-col gap-2">
                <div className="flex items-center bg-quant-bg border border-quant-border rounded px-2 h-8 focus-within:border-quant-gold transition-colors">
                  <span className="text-[10px] text-muted-foreground w-8">止盈</span>
                  <input value={tpPrice} onChange={e=>setTpPrice(e.target.value)} placeholder="最新价" aria-label="止盈价格" className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"/>
                  <span className="text-[10px] text-muted-foreground">USDT</span>
                </div>
                <div className="flex items-center bg-quant-bg border border-quant-border rounded px-2 h-8 focus-within:border-quant-gold transition-colors">
                  <span className="text-[10px] text-muted-foreground w-8">止损</span>
                  <input value={slPrice} onChange={e=>setSlPrice(e.target.value)} placeholder="最新价" aria-label="止损价格" className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"/>
                  <span className="text-[10px] text-muted-foreground">USDT</span>
                </div>
              </div>
            )}
            <div className="flex justify-between text-[10px] text-muted-foreground">
              <span>总估值</span>
              <span className="font-mono text-foreground">{totalEstUsdt.toFixed(8)} USDT</span>
            </div>
            <div className="flex justify-between text-[10px] text-muted-foreground">
              <span>可用</span>
              <span className="font-mono text-foreground">{spotBalance.toFixed(8)} USDT</span>
            </div>
            <div className="flex justify-between text-[10px] text-muted-foreground border-t border-quant-border pt-2">
              <span>成交额</span>
              <span className="font-mono text-foreground">{preview.notional>0?preview.notional.toFixed(2):'--'} USDT</span>
            </div>
            <div className="flex justify-between text-[10px] text-muted-foreground">
              <span>手续费</span>
              <span className="font-mono text-foreground">{preview.fee>0?preview.fee.toFixed(4):'--'} USDT</span>
            </div>
            <button onClick={handlePlaceOrder} disabled={submitting} className={cn("w-full py-2.5 rounded text-sm font-bold transition-colors", submitting&&"opacity-60 cursor-not-allowed", side==='BUY'?"bg-[#0ECB81] hover:bg-[#0ECB81]/90 text-black":"bg-[#F6465D] hover:bg-[#F6465D]/90 text-white")}>
              {submitting?'提交中...':`${side==='BUY'?'买入':'卖出'} ${symbol.replace('USDT','')}`}
            </button>
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
