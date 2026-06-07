import { useEffect, useRef, useCallback, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { marketApi, orderApi, portfolioApi, accountApi, tradesApi } from '@/lib/api'
import { KLineChartPro } from '@klinecharts/pro'
import '@klinecharts/pro/dist/klinecharts-pro.css'
import { createBackendDatafeed, handlePriceTick, runBackfill, setChartUpdater, clearChartUpdater } from '@/lib/klineDatafeed'
import { TRADING_INTERVALS } from '@/lib/constants'
import { cn } from '@/lib/utils'
import { useWebSocket } from '@/hooks/useWebSocket'
import { toast, ToastContainer } from '@/lib/useToast'
import { EmptyState } from '@/components/ui/EmptyState'
import { Skeleton } from '@/components/ui/Skeleton'
import {
  parseInterval, formatPrice, formatTime, formatDateTime,
  StatusTag, CONTRACT_LEVERAGES,
} from '@/lib/tradingHelpers'
import type { Trade, Order, PortfolioPosition } from '@/types'
import type { ChartApi } from '@/lib/tradingHelpers'
import {
  TrendingUp, Clock, XCircle,
  CheckCircle2, AlertCircle, Activity, ChevronUp, ChevronDown,
  Settings
} from 'lucide-react'

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

interface PositionItem extends PortfolioPosition {
  id?: string
  entryPrice?: number
  openPrice?: number
  avgPrice?: number
  amount?: number
  positionMargin?: number
  liquidationPrice?: number
  liquidation?: number
}

const LEVERAGES = CONTRACT_LEVERAGES

function obFormatPrice(p: number, precision: string): string {
  const tick = parseFloat(precision) || 0.1
  const decimals = precision.includes('.') ? precision.split('.')[1].length : 0
  return (Math.round(p / tick) * tick).toFixed(decimals)
}

/* ════════════════════════════════════════
   CONTRACT TRADING PAGE — 币安合约风格
   ════════════════════════════════════════ */
export function ContractTrading() {
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [interval, setInterval] = useState('15m')
  const [side, setSide] = useState<'BUY'|'SELL'>('BUY')
  const [orderType, setOrderType] = useState<'LIMIT'|'MARKET'|'STOP_LIMIT'>('LIMIT')
  const [price, setPrice] = useState('')
  const [quantity, setQuantity] = useState('')
  const [leverage, setLeverage] = useState(10)
  const [marginMode, setMarginMode] = useState<'cross'|'isolated'>('cross')
  const [positionMode, setPositionMode] = useState<'open'|'close'>('open')
  const [obPrecision, setObPrecision] = useState('0.1')
  const [activeBottomTab, setActiveBottomTab] = useState<'positions'|'orders'|'history'|'fills'|'assets'|'plans'>('positions')
  const [bottomHeight, setBottomHeight] = useState(0)
  const bottomCollapsed = bottomHeight < 20
  const dragRef = useRef<{startY:number;startH:number}|null>(null)
  const [tpPrice, setTpPrice] = useState('')
  const [slPrice, setSlPrice] = useState('')
  const [showTpSl, setShowTpSl] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  const chartRef = useRef<HTMLDivElement>(null)
  const chartApiRef = useRef<ChartApi | null>(null)
  const klineProRef = useRef<unknown>(null)
  const datafeed = useMemo(()=>createBackendDatafeed(),[])
  const queryClient = useQueryClient()

  /* queries */
  const {data:klines} = useQuery({queryKey:['klines',symbol,interval], queryFn:()=>marketApi.klines(symbol,interval,1000), refetchInterval:5000})
  const {data:orderbook, isLoading:obLoading} = useQuery({queryKey:['orderbook',symbol], queryFn:()=>marketApi.orderBook(symbol,20), refetchInterval:2000})
  const {data:recentTrades} = useQuery({queryKey:['trades',symbol], queryFn:()=>marketApi.trades(symbol,50), refetchInterval:3000})
  const {data:positionsRaw, isLoading:posLoading} = useQuery({queryKey:['positions'], queryFn:()=>portfolioApi.positions(), refetchInterval:5000})
  const positions = Array.isArray(positionsRaw)?positionsRaw:positionsRaw?.positions||[]
  const {data:orders, isLoading:ordersLoading} = useQuery({queryKey:['orders'], queryFn:()=>orderApi.list(), refetchInterval:5000})
  const {data:historyOrders, isLoading:historyLoading} = useQuery({queryKey:['orders-history'], queryFn:()=>orderApi.history({status:'filled'}), refetchInterval:10000})
  const {data:snapshot} = useQuery({queryKey:['snapshot',symbol], queryFn:()=>marketApi.snapshot(symbol), refetchInterval:5000})
  const {data:fillTrades, isLoading:fillsLoading} = useQuery({queryKey:['fills'], queryFn:()=>tradesApi.list({limit:'30'}), refetchInterval:5000})
  const {data:portfolio} = useQuery({queryKey:['portfolio'], queryFn:()=>portfolioApi.summary(), refetchInterval:10000})
  const totalEstUsdt = useMemo(()=>{
    if(!portfolio) return 0
    return parseFloat(String(portfolio.total_equity ?? 0)) || 0
  },[portfolio])
  const futuresBalance = useMemo(()=>{
    if(!portfolio) return 0
    return parseFloat(String(portfolio.futures_balance ?? 0)) || 0
  },[portfolio])
  const {data:allBalances, isLoading:balLoading} = useQuery({queryKey:['balances','all'], queryFn:()=>accountApi.balance(), refetchInterval:10000})

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

  /* funding rate & mark price (after lastPrice is defined) */
  const {data:fundingData} = useQuery({queryKey:['funding',symbol], queryFn:()=>marketApi.fundingRate(symbol), refetchInterval:30000})
  const fundingRate = fundingData?.fundingRate ?? 0
  const markPrice = fundingData?.markPrice ?? lastPrice
  const nextFundingTime = fundingData?.nextFundingTime ?? 0

  const prevClose = useMemo(()=>{
    if(klines&&klines.length>1) return parseFloat(String(klines[klines.length-2].close))
    return lastPrice
  },[klines,lastPrice])
  const change = lastPrice-prevClose
  const changePct = prevClose?(change/prevClose)*100:0
  const isUp = change>=0
  const bestBid = orderbook?.bids?.[0]?.[0] != null ? String(orderbook.bids[0][0]) : ''
  const bestAsk = orderbook?.asks?.[0]?.[0] != null ? String(orderbook.asks[0][0]) : ''

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
          try { chartApi.scrollToRealTime() } catch { /* ignore */ }
          try { chartApi.setBarSpace(4) } catch { /* ignore */ }
          // Wire running bar updates directly to chart (avoids timestamp conflict
          // with the last historical bar for the current period)
          if (typeof chartApi.updateData === 'function') {
            setChartUpdater((bar) => { try { chartApi.updateData(bar) } catch { /* ignore */ } })
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
  const handlePlaceOrder = useCallback(async (orderSide: 'BUY' | 'SELL') => {
    const qty = parseFloat(quantity)
    if (!qty || qty <= 0) { toast('error', '请输入有效数量'); return }
    if (orderType === 'LIMIT' || orderType === 'STOP_LIMIT') {
      const p = parseFloat(price)
      if (!p || p <= 0) { toast('error', '请输入有效价格'); return }
    }
    setSubmitting(true)
    try{
      const req: Record<string, unknown> = {
        symbol, side: orderSide, order_type: orderType,
        price: orderType==='MARKET' ? 0 : (parseFloat(price)||0),
        quantity: qty,
        market_type: 'swap',
        position_side: orderSide === 'BUY' ? 'LONG' : 'SHORT',
        leverage,
        margin_mode: marginMode,
      }
      if (orderType === 'STOP_LIMIT') {
        req.stop_price = parseFloat(tpPrice) || 0
      }
      if (showTpSl) {
        req.tp_price = tpPrice ? parseFloat(tpPrice) : undefined
        req.sl_price = slPrice ? parseFloat(slPrice) : undefined
      }
      await orderApi.place(req)
      setSide(orderSide)
      toast('success', '订单已提交')
      setQuantity(''); setPrice(''); setTpPrice(''); setSlPrice('')
      queryClient.invalidateQueries({queryKey:['orders']})
      queryClient.invalidateQueries({queryKey:['positions']})
      queryClient.invalidateQueries({queryKey:['portfolio']})
    }catch(e: unknown){
      const err = e instanceof Error ? e : new Error(String(e))
      toast('error', err.message || '下单失败')
    }finally{ setSubmitting(false) }
  },[symbol,orderType,price,quantity,leverage,tpPrice,slPrice,showTpSl,queryClient])

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

  const handleClosePosition = useCallback(async (pos: PositionItem) => {
    const isLong = (pos.side || '').toUpperCase() === 'LONG' || (pos.side || '').toUpperCase() === 'BUY'
    const closeSide = isLong ? 'SELL' : 'BUY'
    const qty = Number(pos.quantity || pos.amount || 0)
    if (!qty || qty <= 0) {
      toast('error', '持仓数量无效')
      return
    }
    setSubmitting(true)
    try {
      await orderApi.place({
        symbol: pos.symbol || symbol,
        side: closeSide,
        order_type: 'MARKET',
        price: 0,
        quantity: qty,
        market_type: 'swap',
        position_side: isLong ? 'LONG' : 'SHORT',
        leverage,
        margin_mode: marginMode,
        close_position: true,
      })
      toast('success', `平仓订单已提交: ${isLong ? '平多' : '平空'} ${qty} ${pos.symbol || symbol}`)
      queryClient.invalidateQueries({ queryKey: ['orders'] })
      queryClient.invalidateQueries({ queryKey: ['positions'] })
      queryClient.invalidateQueries({ queryKey: ['portfolio'] })
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      toast('error', err.message || '平仓失败')
    } finally {
      setSubmitting(false)
    }
  }, [symbol, leverage, marginMode, queryClient])

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

  /* contract preview */
  const preview = useMemo(()=>{
    const qty=parseFloat(quantity)||0
    const pr=orderType==='MARKET'?lastPrice:(parseFloat(price)||lastPrice)
    const notional=qty*pr
    const margin=leverage>0?notional/leverage:notional
    const feeRate=0.0005
    const fee=notional*feeRate
    let maxLoss=0
    if(slPrice&&parseFloat(slPrice)>0){
      const sl=parseFloat(slPrice)
      maxLoss=Math.abs(qty*(pr-sl))
    }
    return {notional,margin,fee,maxLoss,qty,pr}
  },[quantity,price,lastPrice,orderType,leverage,slPrice])

  return (
    <div className="h-full flex flex-col">
      {/* MAIN GRID: Chart | Orderbook | Trade */}
      <div className="flex-1 grid grid-cols-[1fr_270px_310px] gap-px bg-quant-border min-h-0">

        {/* CHART (1fr) */}
        <div className="bg-quant-bg flex flex-col min-h-0 overflow-hidden">
          <div ref={chartRef} className="flex-1 min-h-0" />
        </div>

        {/* ORDERBOOK + TRADES (270px) */}
        <div className="bg-quant-bg-secondary flex flex-col overflow-hidden min-h-0">
          <div className="h-8 shrink-0 border-b border-quant-border flex items-center justify-between px-3">
            <div className="flex gap-3 items-center">
              <span className="text-xs font-medium text-foreground">订单簿</span>
              <select
                value={symbol}
                onChange={(e) => setSymbol(e.target.value)}
                className="text-[11px] bg-quant-bg border border-quant-border rounded px-1.5 py-0.5 text-foreground focus:outline-none focus:border-quant-gold"
                aria-label="选择交易对"
              >
                {['BTCUSDT','ETHUSDT','BNBUSDT','SOLUSDT','XRPUSDT','ADAUSDT','DOGEUSDT','AVAXUSDT','DOTUSDT','LINKUSDT'].map((s) => (
                  <option key={s} value={s}>{s.replace('USDT','/USDT')}</option>
                ))}
              </select>
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
                  <div className="flex flex-col items-center">
                    <span className={cn("text-sm font-bold font-mono", isUp?"text-quant-green":"text-quant-red")}>
                      {markPrice?markPrice.toFixed(2):"--"}
                    </span>
                    <span className={cn("text-[10px] font-mono", isUp?"text-quant-green":"text-quant-red")}>
                      {isUp?'+':''}{changePct.toFixed(2)}%
                    </span>
                    {fundingRate !== 0 && (
                      <span className={cn("text-[10px] font-mono mt-0.5", fundingRate>0?"text-quant-red":"text-quant-green")}>
                        资金费率 {fundingRate>0?'+':''}{(fundingRate*100).toFixed(4)}%
                        {nextFundingTime>0 && <span className="text-muted-foreground ml-1">{formatTime(nextFundingTime)}</span>}
                      </span>
                    )}
                  </div>
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
            <div className="flex items-center h-7 px-3 border-b border-quant-border bg-quant-bg-secondary gap-3">
              <span className="text-[11px] font-medium text-foreground">最新成交</span>
              <span className="text-[11px] text-muted-foreground cursor-pointer hover:text-foreground">市场异动</span>
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

        {/* RIGHT: Contract Trade Panel (310px) */}
        <div className="bg-quant-bg-secondary overflow-y-auto flex flex-col">
          {/* Position Mode: open/close */}
          <div className="h-8 shrink-0 border-b border-quant-border flex items-center justify-between px-3">
            <div className="flex gap-2">
              <button className={cn("text-xs px-2 py-1 rounded font-medium", positionMode==='open'?"bg-quant-gold/10 text-quant-gold":"text-muted-foreground hover:text-foreground")} onClick={()=>setPositionMode('open')}>开仓</button>
              <button className={cn("text-xs px-2 py-1 rounded font-medium", positionMode==='close'?"bg-quant-gold/10 text-quant-gold":"text-muted-foreground hover:text-foreground")} onClick={()=>{ setPositionMode('close'); if (positions.length===0){ alert('当前无持仓，无需平仓') } else { setActiveBottomTab('positions'); setBottomHeight(h=>Math.max(h,180)) } }}>平仓</button>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-[11px] text-muted-foreground">{marginMode==='cross'?'全仓':'逐仓'}</span>
              <span className="text-[11px] text-quant-gold font-bold">{leverage}x</span>
              <Settings className="w-3.5 h-3.5 text-muted-foreground cursor-pointer hover:text-foreground" onClick={() => toast('warning', '合约设置功能开发中')}/>
            </div>
          </div>

          {/* Leverage & Margin */}
          <div className="p-3 border-b border-quant-border">
            <div className="flex justify-between items-center mb-2">
              <span className="text-xs font-medium">杠杆</span>
              <span className="text-xs text-quant-gold font-mono">{leverage}x</span>
            </div>
            <div className="flex gap-1 mb-2">
              {LEVERAGES.map(l=>(
                <button key={l} onClick={()=>setLeverage(l)} className={cn("flex-1 py-1 text-[10px] rounded border transition-colors", leverage===l?"border-quant-gold text-quant-gold bg-quant-gold/10":"border-quant-border text-muted-foreground hover:text-foreground")}>{l}x</button>
              ))}
            </div>
            <div className="flex gap-1">
              <button onClick={()=>setMarginMode('cross')} className={cn("flex-1 py-1 text-[11px] rounded border transition-colors", marginMode==='cross'?"border-quant-gold text-quant-gold bg-quant-gold/10":"border-quant-border text-muted-foreground")}>全仓</button>
              <button onClick={()=>setMarginMode('isolated')} className={cn("flex-1 py-1 text-[11px] rounded border transition-colors", marginMode==='isolated'?"border-quant-gold text-quant-gold bg-quant-gold/10":"border-quant-border text-muted-foreground")}>逐仓</button>
            </div>
          </div>

          {/* Order Form */}
          <div className="flex-1 p-3 flex flex-col gap-3">
            <div className="flex gap-1 bg-quant-bg p-0.5 rounded">
              {(['LIMIT','MARKET','STOP_LIMIT'] as const).map(t=>(
                <button key={t} onClick={()=>setOrderType(t)} className={cn("flex-1 py-1 text-[11px] font-medium rounded transition-colors", orderType===t?"bg-quant-bg-secondary text-foreground":"text-muted-foreground hover:text-foreground")}>{t==='LIMIT'?'限价':t==='MARKET'?'市价':'条件'}</button>
              ))}
              <button onClick={()=>setShowTpSl(!showTpSl)} className={cn("flex-1 py-1 text-[11px] rounded transition-colors", showTpSl?"bg-quant-bg-secondary text-foreground":"text-muted-foreground hover:text-foreground")}>止盈止损</button>
            </div>
            {orderType==='STOP_LIMIT'&&(
              <div className="flex flex-col gap-1">
                <div className="flex justify-between text-[10px] text-muted-foreground"><span>触发价格</span><span>USDT</span></div>
                <div className="flex items-center bg-quant-bg border border-quant-border rounded px-2 h-8 focus-within:border-quant-gold transition-colors">
                  <input value={tpPrice} onChange={e=>setTpPrice(e.target.value)} placeholder={lastPrice?lastPrice.toFixed(2):'0.00'} aria-label="触发价格" className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"/>
                  <span className="text-[10px] text-muted-foreground">USDT</span>
                </div>
              </div>
            )}
            {orderType==='LIMIT'&&(
              <div className="flex flex-col gap-1">
                <div className="flex justify-between text-[10px] text-muted-foreground"><span>委托价格</span><span>USDT</span></div>
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
            <div className="flex flex-col gap-1">
              <div className="flex justify-between text-[10px] text-muted-foreground"><span>成交额</span><span>USDT</span></div>
              <div className="flex items-center bg-quant-bg border border-quant-border rounded px-2 h-8 focus-within:border-quant-gold transition-colors">
                <input value={preview.notional>0?preview.notional.toFixed(2):''} readOnly placeholder="0.00" className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"/>
                <span className="text-[10px] text-muted-foreground">USDT</span>
              </div>
            </div>
            <div className="flex gap-1">
              {[0.25,0.5,0.75,1].map(pct=>{
                const pctLabel = Math.round(pct*100)+'%'
                const calcPrice = orderType==='MARKET'?lastPrice:(parseFloat(price)||lastPrice)
                const calcQty = calcPrice>0?(futuresBalance*pct*leverage)/calcPrice:0
                return <button key={pctLabel} onClick={()=>setQuantity(calcQty>0?calcQty.toFixed(6):'')} className="flex-1 py-1 text-[10px] text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border rounded hover:border-quant-gold/50 transition-colors">{pctLabel}</button>
              })}
            </div>
            <div className="flex items-center gap-2">
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
              <span>可用保证金</span>
              <span className="font-mono text-foreground">{futuresBalance.toFixed(2)} USDT</span>
            </div>
            <div className="flex justify-between text-[10px] text-muted-foreground">
              <span>可开{side==='BUY'?'多':'空'}</span>
              <span className="font-mono text-foreground">{futuresBalance>0&&lastPrice>0?(futuresBalance*leverage/lastPrice).toFixed(4):'0.0000'} {symbol.replace('USDT','')}</span>
            </div>
            <div className="flex justify-between text-[10px] text-muted-foreground">
              <span>保证金</span>
              <span className="font-mono text-foreground">{preview.margin>0?preview.margin.toFixed(2):'--'} USDT</span>
            </div>
            <div className="flex justify-between text-[10px] text-muted-foreground">
              <span>手续费</span>
              <span className="font-mono text-foreground">{preview.fee>0?preview.fee.toFixed(4):'--'} USDT</span>
            </div>
            <div className="flex flex-col gap-2 mt-1">
              {positionMode === 'close' ? (
                <div className="py-2 text-center text-[11px] text-muted-foreground">
                  当前为平仓模式，请在下方持仓列表中操作平仓
                </div>
              ) : (
                <>
                  <button onClick={()=>handlePlaceOrder('BUY')} disabled={submitting} className={cn("w-full py-2.5 rounded text-sm font-bold transition-colors", submitting?"opacity-60 cursor-not-allowed bg-[#0ECB81]":"bg-[#0ECB81] hover:bg-[#0ECB81]/90 text-black")}>
                    {submitting?'提交中...':`开多 ${leverage}x`}
                  </button>
                  <button onClick={()=>handlePlaceOrder('SELL')} disabled={submitting} className={cn("w-full py-2.5 rounded text-sm font-bold transition-colors", submitting?"opacity-60 cursor-not-allowed bg-[#F6465D]":"bg-[#F6465D] hover:bg-[#F6465D]/90 text-white")}>
                    {submitting?'提交中...':`开空 ${leverage}x`}
                  </button>
                </>
              )}
            </div>
          </div>

          {/* Account Info Panel */}
          <div className="border-t border-quant-border p-3">
            <div className="flex items-center justify-between mb-2">
              <span className="text-xs font-medium text-foreground">账户</span>
              <span className="text-[10px] text-quant-gold cursor-pointer hover:underline" onClick={() => toast('warning', '资金划转功能开发中')}>划转</span>
            </div>
            <div className="space-y-1.5">
              <div className="flex justify-between text-[10px]">
                <span className="text-muted-foreground">账户总权益</span>
                <span className="font-mono text-foreground">{totalEstUsdt.toFixed(4)} USDT</span>
              </div>
              <div className="flex justify-between text-[10px]">
                <span className="text-muted-foreground">合约余额</span>
                <span className="font-mono text-foreground">{futuresBalance.toFixed(6)} USDT</span>
              </div>
              <div className="flex justify-between text-[10px]">
                <span className="text-muted-foreground">已用保证金</span>
                <span className="font-mono text-foreground">{portfolio?.margin_used ? Number(portfolio.margin_used).toFixed(4)+' USDT' : '--'}</span>
              </div>
              <div className="flex justify-between text-[10px]">
                <span className="text-muted-foreground">未实现盈亏</span>
                <span className={cn("font-mono", (portfolio?.futures_unrealized_pnl||0)>=0?"text-[#0ECB81]":"text-[#F6465D]")}>{portfolio?.futures_unrealized_pnl ? (Number(portfolio.futures_unrealized_pnl)>=0?'+':'')+Number(portfolio.futures_unrealized_pnl).toFixed(4) : '--'}</span>
              </div>
              <div className="flex justify-between text-[10px]">
                <span className="text-muted-foreground">持仓数量</span>
                <span className="font-mono text-foreground">{positions?.length||0}</span>
              </div>
              <div className="flex justify-between text-[10px]">
                <span className="text-muted-foreground">总盈亏</span>
                <span className={cn("font-mono", (portfolio?.total_pnl||0)>=0?"text-[#0ECB81]":"text-[#F6465D]")}>{portfolio?.total_pnl ? (Number(portfolio.total_pnl)>=0?'+':'')+Number(portfolio.total_pnl).toFixed(4) : '--'}</span>
              </div>
            </div>
            <div className="flex gap-1 mt-3">
              <button onClick={() => toast('warning', '资金划转功能开发中')} className="flex-1 py-1.5 text-[10px] bg-quant-bg border border-quant-border rounded text-muted-foreground hover:text-foreground transition-colors">划转</button>
              <button onClick={() => toast('warning', '买币功能开发中')} className="flex-1 py-1.5 text-[10px] bg-quant-bg border border-quant-border rounded text-muted-foreground hover:text-foreground transition-colors">买币</button>
              <button onClick={() => toast('warning', '兑换功能开发中')} className="flex-1 py-1.5 text-[10px] bg-quant-bg border border-quant-border rounded text-muted-foreground hover:text-foreground transition-colors">兑换</button>
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
              {key:'positions',label:'持仓',count:positions?.length||0,icon:TrendingUp},
              {key:'orders',label:'当前委托',count:orders?.length||0,icon:Clock},
              {key:'plans',label:'计划委托',count:0,icon:AlertCircle},
              {key:'history',label:'历史委托',count:historyOrders?.length||0,icon:XCircle},
              {key:'fills',label:'成交记录',count:fillTrades?.length||0,icon:CheckCircle2},
              {key:'assets',label:'资产',count:0,icon:Activity},
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
              <div className="overflow-x-auto">
                {posLoading?(
                  <div className="p-4 space-y-2">{Array.from({length:3}).map((_,i)=><Skeleton key={i} variant="text" height={32}/>)}</div>
                ):positions?.length?(
                  <table className="w-full text-[11px] whitespace-nowrap">
                    <thead className="sticky top-0 bg-quant-bg-secondary z-10">
                      <tr className="text-muted-foreground border-b border-quant-border">
                        <th scope="col" className="text-left font-medium px-3 py-2">合约</th>
                        <th scope="col" className="text-left font-medium px-3 py-2">方向/数量</th>
                        <th scope="col" className="text-right font-medium px-3 py-2">开仓价</th>
                        <th scope="col" className="text-right font-medium px-3 py-2">标记价</th>
                        <th scope="col" className="text-right font-medium px-3 py-2">强平价</th>
                        <th scope="col" className="text-right font-medium px-3 py-2">保证金</th>
                        <th scope="col" className="text-right font-medium px-3 py-2">未实现盈亏</th>
                        <th scope="col" className="text-right font-medium px-3 py-2">保证金率/维持率</th>
                        <th scope="col" className="text-right font-medium px-3 py-2">操作</th>
                      </tr>
                    </thead>
                    <tbody>
                      {positions.map((pos: PositionItem, i: number)=>{
                        const isLong=(pos.side||'').toUpperCase()==='LONG'||(pos.side||'').toUpperCase()==='BUY'
                        const entryPx=Number(pos.entryPrice||pos.openPrice||pos.avgPrice||0)
                        const markPx=markPrice||lastPrice||0
                        const qty=Number(pos.quantity||pos.amount||0)
                        const margin=Number(pos.margin||pos.positionMargin||0)
                        const posLeverage=Number(pos.leverage||leverage||1)
                        const notional=qty*markPx
                        const upnl=isLong?(markPx-entryPx)*qty:(entryPx-markPx)*qty
                        const upnlPct=margin>0?(upnl/margin)*100:0
                        const liqPx=Number(pos.liquidationPrice||pos.liquidation||0)
                        // Margin ratio = margin / notional * 100
                        const marginRatio=notional>0?(margin/notional)*100:0
                        // Maintenance margin rate (simplified: 0.4% for tier 1)
                        const mmRate=0.4
                        return (
                          <tr key={pos.id||i} className="border-b border-quant-border/40 hover:bg-white/[0.03]">
                            <td className="px-3 py-2.5 font-medium">{pos.symbol||symbol} 永续</td>
                            <td className="px-3 py-2.5">
                              <span className={cn("inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-bold", isLong?"bg-[#0ECB81]/10 text-[#0ECB81]":"bg-[#F6465D]/10 text-[#F6465D]")}>
                                <span className={cn("w-1.5 h-1.5 rounded-full", isLong?"bg-[#0ECB81]":"bg-[#F6465D]")}/>
                                {isLong?'多':'空'} {qty.toFixed(3)}
                              </span>
                              <span className="text-[10px] text-muted-foreground ml-1">{posLeverage}x</span>
                            </td>
                            <td className="px-3 py-2.5 text-right font-mono">{entryPx>0?entryPx.toFixed(2):'--'}</td>
                            <td className="px-3 py-2.5 text-right font-mono">{markPx>0?markPx.toFixed(2):'--'}</td>
                            <td className="px-3 py-2.5 text-right font-mono text-[#F6465D]">{liqPx>0?liqPx.toFixed(2):'--'}</td>
                            <td className="px-3 py-2.5 text-right font-mono">{margin>0?margin.toFixed(2):'--'} USDT</td>
                            <td className="px-3 py-2.5 text-right font-mono">
                              <span className={cn(upnl>=0?"text-[#0ECB81]":"text-[#F6465D]")}>{upnl>=0?'+':''}{upnl.toFixed(2)}</span>
                              <span className="text-muted-foreground ml-1">({upnlPct>=0?'+':''}{upnlPct.toFixed(2)}%)</span>
                            </td>
                            <td className="px-3 py-2.5 text-right font-mono text-muted-foreground">
                              {marginRatio>0?marginRatio.toFixed(2):'--'}% / {mmRate}%
                            </td>
                            <td className="px-3 py-2.5 text-right">
                              <button
                                onClick={() => handleClosePosition(pos)}
                                disabled={submitting}
                                className={cn(
                                  "px-2 py-1 rounded text-[10px] font-medium transition-colors",
                                  submitting
                                    ? "bg-muted text-muted-foreground cursor-not-allowed"
                                    : isLong
                                      ? "bg-[#F6465D]/10 text-[#F6465D] hover:bg-[#F6465D]/20"
                                      : "bg-[#0ECB81]/10 text-[#0ECB81] hover:bg-[#0ECB81]/20"
                                )}
                              >
                                {submitting ? '平仓中...' : `平${isLong ? '多' : '空'}`}
                              </button>
                            </td>
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                ):<div className="py-8 text-center text-muted-foreground text-xs">暂无持仓</div>}
              </div>
            )}
            {activeBottomTab==='orders'&&(
              <div>
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
            {activeBottomTab==='plans'&&(
              <div>
                <div className="flex items-center justify-between px-3 py-2 border-b border-quant-border">
                  <span className="text-xs font-medium text-foreground">计划委托</span>
                  <span className="text-[10px] text-muted-foreground">止盈止损 / 条件单 / 跟踪止损</span>
                </div>
                <div className="overflow-x-auto">
                  <table className="w-full text-[11px] whitespace-nowrap">
                    <thead className="sticky top-0 bg-quant-bg-secondary z-10">
                      <tr className="text-muted-foreground border-b border-quant-border">
                        <th scope="col" className="text-left font-medium px-3 py-2">类型</th>
                        <th scope="col" className="text-left font-medium px-3 py-2">币种</th>
                        <th scope="col" className="text-right font-medium px-3 py-2">触发价</th>
                        <th scope="col" className="text-right font-medium px-3 py-2">委托价</th>
                        <th scope="col" className="text-right font-medium px-3 py-2">数量</th>
                        <th scope="col" className="text-right font-medium px-3 py-2">状态</th>
                        <th scope="col" className="text-right font-medium px-3 py-2">操作</th>
                      </tr>
                    </thead>
                    <tbody>
                      {/* Show TP/SL from active orders that have tp_price or sl_price */}
                      {(orders||[]).filter((o: Order)=>o.tp_price||o.sl_price).map((o: Order)=>{
                        return (
                          <tr key={`tp-${o.id}`} className="border-b border-quant-border/40 hover:bg-white/[0.03]">
                            <td className="px-3 py-2">
                              <span className={cn("text-[10px] px-1.5 py-0.5 rounded font-bold", o.tp_price?"bg-[#0ECB81]/10 text-[#0ECB81]":"bg-[#F6465D]/10 text-[#F6465D]")}>
                                {o.tp_price?'止盈':'止损'}
                              </span>
                            </td>
                            <td className="px-3 py-2 font-medium">{o.symbol}</td>
                            <td className="px-3 py-2 text-right font-mono">{o.tp_price?o.tp_price.toFixed(2):o.sl_price?.toFixed(2)}</td>
                            <td className="px-3 py-2 text-right font-mono text-muted-foreground">市价</td>
                            <td className="px-3 py-2 text-right font-mono">{o.quantity.toFixed(4)}</td>
                            <td className="px-3 py-2 text-right">
                              <span className="text-[10px] px-1.5 py-0.5 rounded bg-quant-bg-tertiary text-muted-foreground">监控中</span>
                            </td>
                            <td className="px-3 py-2 text-right">
                              <button onClick={()=>handleCancelOrder(o.id)} className="px-2 py-0.5 rounded text-[10px] bg-[#F6465D]/10 text-[#F6465D] hover:bg-[#F6465D]/20 transition-colors">取消</button>
                            </td>
                          </tr>
                        )
                      })}
                      {/* Show stop-limit orders */}
                      {(orders||[]).filter((o: Order)=>o.type==='STOP_LIMIT').map((o: Order)=>{
                        return (
                          <tr key={`stop-${o.id}`} className="border-b border-quant-border/40 hover:bg-white/[0.03]">
                            <td className="px-3 py-2">
                              <span className="text-[10px] px-1.5 py-0.5 rounded font-bold bg-quant-gold/10 text-quant-gold">条件单</span>
                            </td>
                            <td className="px-3 py-2 font-medium">{o.symbol}</td>
                            <td className="px-3 py-2 text-right font-mono">{o.tp_price?o.tp_price.toFixed(2):'--'}</td>
                            <td className="px-3 py-2 text-right font-mono">{o.price.toFixed(2)}</td>
                            <td className="px-3 py-2 text-right font-mono">{o.quantity.toFixed(4)}</td>
                            <td className="px-3 py-2 text-right">
                              <span className="text-[10px] px-1.5 py-0.5 rounded bg-quant-bg-tertiary text-muted-foreground">{o.status}</span>
                            </td>
                            <td className="px-3 py-2 text-right">
                              <button onClick={()=>handleCancelOrder(o.id)} className="px-2 py-0.5 rounded text-[10px] bg-[#F6465D]/10 text-[#F6465D] hover:bg-[#F6465D]/20 transition-colors">取消</button>
                            </td>
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                  {(orders||[]).filter((o: Order)=>o.tp_price||o.sl_price||o.type==='STOP_LIMIT').length===0 && (
                    <div className="py-6 flex items-center justify-center"><EmptyState title="暂无计划委托" description="计划委托包括止盈止损和条件委托" className="py-1 border-0 text-[10px] [&>div:first-child]:hidden"/></div>
                  )}
                </div>
              </div>
            )}
            {activeBottomTab==='history'&&(
              <div>
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
              <div>
                {fillsLoading?(
                  <div className="p-4 space-y-2">{Array.from({length:4}).map((_,i)=><Skeleton key={i} variant="text" height={32}/>)}</div>
                ):fillTrades?.length?(
                  <div className="overflow-x-auto">
                    <table className="w-full text-[11px] whitespace-nowrap">
                      <thead className="sticky top-0 bg-quant-bg-secondary z-10"><tr className="text-muted-foreground text-left"><th scope="col" className="px-1.5 py-1 font-medium">时间</th><th scope="col" className="px-1.5 py-1 font-medium">币种</th><th scope="col" className="px-1.5 py-1 font-medium">方向</th><th scope="col" className="px-1.5 py-1 font-medium">价格</th><th scope="col" className="px-1.5 py-1 font-medium">数量</th><th scope="col" className="px-1.5 py-1 font-medium">手续费</th></tr></thead>
                      <tbody>
                        {(fillTrades||[]).map((t: FillTrade,i:number)=>{
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
              <div>
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
                          const total = free + locked
                          return <tr key={String(b.asset || b.symbol || i)} className="border-t border-quant-border/40 hover:bg-white/[0.02]">
                            <td className="px-3 py-2 font-semibold">{String(b.asset || b.symbol || '--')}</td>
                            <td className="px-3 py-2 text-right font-mono">{free.toFixed(4)}</td>
                            <td className="px-3 py-2 text-right font-mono">{locked > 0 ? locked.toFixed(4) : '--'}</td>
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
