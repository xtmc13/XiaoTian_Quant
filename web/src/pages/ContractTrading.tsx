import { useEffect, useRef, useCallback, useMemo, useState } from 'react'
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query'
import { marketApi, orderApi, portfolioApi, accountApi, tradesApi } from '@/lib/api'
import { KLineChartPro } from '@klinecharts/pro'
import '@klinecharts/pro/dist/klinecharts-pro.css'
import { createBackendDatafeed, handlePriceTick, runBackfill, setChartUpdater, clearChartUpdater } from '@/lib/klineDatafeed'
import { TRADING_INTERVALS } from '@/lib/constants'
import { cn } from '@/lib/utils'
import { useWebSocket } from '@/hooks/useWebSocket'
import { toast, ToastContainer } from '@/lib/useToast'
import { OrderBookPanel } from '@/components/trading/OrderBookPanel'
import { EmptyState } from '@/components/ui/EmptyState'
import { Skeleton } from '@/components/ui/Skeleton'
import { ErrorBoundary } from '@/components/ErrorBoundary'
import {
  parseInterval, formatPrice, formatTime, formatDateTime,
  StatusTag, CONTRACT_LEVERAGES,
} from '@/lib/tradingHelpers'
import { getPrecision } from '@/lib/tradingPrecision'
import type { Trade, Order, PortfolioPosition, TickerSnapshot } from '@/types'
import type { ChartApi } from '@/lib/tradingHelpers'
import {
  TrendingUp, Clock, XCircle,
  CheckCircle2, AlertCircle, Activity, ChevronUp, ChevronDown,
  Settings, X, ArrowRightLeft, Wallet, Repeat, DollarSign
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
  const {data:positionsRaw, isLoading:posLoading} = useQuery({queryKey:['positions'], queryFn:()=>portfolioApi.positions(), refetchInterval:5000})
  const positions = Array.isArray(positionsRaw)?positionsRaw:positionsRaw?.positions||[]
  const {data:orders, isLoading:ordersLoading} = useQuery({queryKey:['orders'], queryFn:()=>orderApi.list(), refetchInterval:5000})
  const {data:historyOrders, isLoading:historyLoading} = useQuery({queryKey:['orders-history'], queryFn:()=>orderApi.history({status:'filled'}), refetchInterval:10000})
  const {data:snapshot} = useQuery({queryKey:['snapshot',symbol], queryFn:()=>marketApi.snapshot(symbol).then(d => d as TickerSnapshot), refetchInterval:5000})
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

  /* ── Modal States ── */
  const [showSettingsModal, setShowSettingsModal] = useState(false)
  const [showTransferModal, setShowTransferModal] = useState(false)
  const [showBuyModal, setShowBuyModal] = useState(false)
  const [showSwapModal, setShowSwapModal] = useState(false)

  /* ── Transfer Form ── */
  const [transferFrom, setTransferFrom] = useState('futures')
  const [transferTo, setTransferTo] = useState('spot')
  const [transferCurrency, setTransferCurrency] = useState('USDT')
  const [transferAmount, setTransferAmount] = useState('')

  /* ── Buy Form ── */
  const [buyCurrency, setBuyCurrency] = useState('BTC')
  const [buyAmount, setBuyAmount] = useState('')
  const [buyMethod, setBuyMethod] = useState('credit_card')

  /* ── Swap Form ── */
  const [swapFrom, setSwapFrom] = useState('BTC')
  const [swapTo, setSwapTo] = useState('ETH')
  const [swapAmount, setSwapAmount] = useState('')

  /* ── Mutations ── */
  const transferMut = useMutation({
    mutationFn: (data: { from: string; to: string; currency: string; amount: number }) => accountApi.transfer(data),
    onSuccess: (res) => {
      if (res?.success) { toast('success', res.message); setShowTransferModal(false); queryClient.invalidateQueries({ queryKey: ['portfolio'] }) }
      else { toast('error', res?.message || '划转失败') }
    },
    onError: (err: Error) => toast('error', err.message),
  })
  const buyMut = useMutation({
    mutationFn: (data: { currency: string; amount: number; payment_method?: string }) => accountApi.buy(data),
    onSuccess: (res) => {
      if (res?.success) { toast('success', res.message); setShowBuyModal(false); queryClient.invalidateQueries({ queryKey: ['portfolio'] }) }
      else { toast('error', res?.message || '买入失败') }
    },
    onError: (err: Error) => toast('error', err.message),
  })
  const swapMut = useMutation({
    mutationFn: (data: { from_currency: string; to_currency: string; amount: number }) => accountApi.swap(data),
    onSuccess: (res) => {
      if (res?.success) { toast('success', res.message); setShowSwapModal(false); queryClient.invalidateQueries({ queryKey: ['portfolio'] }) }
      else { toast('error', res?.message || '兑换失败') }
    },
    onError: (err: Error) => toast('error', err.message),
  })

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
    // 根据模式计算实际数量
    let qty: number
    if (amountMode === 'amount') {
      // 金额模式：根据金额计算数量
      const amount = parseFloat(amountValue)
      if (!amount || amount <= 0) { toast('error', '请输入有效金额'); return }
      const calcPrice = orderType === 'MARKET' ? lastPrice : (parseFloat(price) || lastPrice)
      if (!calcPrice || calcPrice <= 0) { toast('error', '无法获取有效价格'); return }
      // 合约：金额 = 数量 * 价格 / 杠杆
      qty = (amount * leverage) / calcPrice
    } else {
      // 数量模式
      qty = parseFloat(quantity)
      if (!qty || qty <= 0) { toast('error', '请输入有效数量'); return }
    }
    
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
        time_in_force: timeInForce,
        post_only: postOnly,
        slippage: orderType === 'MARKET' ? parseFloat(slippage) / 100 : undefined,
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
      setQuantity(''); setPrice(''); setTpPrice(''); setSlPrice(''); setAmountValue(''); setSliderValue(0)
      queryClient.invalidateQueries({queryKey:['orders']})
      queryClient.invalidateQueries({queryKey:['positions']})
      queryClient.invalidateQueries({queryKey:['portfolio']})
    }catch(e: unknown){
      const err = e instanceof Error ? e : new Error(String(e))
      toast('error', err.message || '下单失败')
    }finally{ setSubmitting(false) }
  },[symbol,orderType,price,quantity,amountMode,amountValue,lastPrice,leverage,tpPrice,slPrice,showTpSl,timeInForce,postOnly,slippage,marginMode,queryClient])

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

  /* contract preview */
  const preview = useMemo(()=>{
    // 根据模式计算数量
    let qty: number
    if (amountMode === 'amount') {
      const amount = parseFloat(amountValue) || 0
      const calcPrice = orderType === 'MARKET' ? lastPrice : (parseFloat(price) || lastPrice)
      // 合约：金额 = 数量 * 价格 / 杠杆
      qty = calcPrice > 0 ? (amount * leverage) / calcPrice : 0
    } else {
      qty = parseFloat(quantity) || 0
    }
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
  },[quantity,amountMode,amountValue,price,lastPrice,orderType,leverage,slPrice])

  return (
    <div className="h-full flex flex-col">
      {/* MAIN GRID: Chart | Orderbook | Trade */}
      <div className="flex-1 grid grid-cols-[1fr_270px_310px] gap-px bg-quant-border min-h-0">

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

        {/* ORDERBOOK + TRADES (270px) */}
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
          midPriceContent={
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
          }
          recentTradesHeader={
            <>
              <span className="text-[11px] font-medium text-foreground">最新成交</span>
              <span className="text-[11px] text-muted-foreground cursor-pointer hover:text-foreground">市场异动</span>
            </>
          }
        />

        {/* RIGHT: Contract Trade Panel (310px) */}
        <div className="bg-quant-bg-secondary overflow-y-auto flex flex-col">
          {/* Position Mode: open/close */}
          <div className="h-8 shrink-0 border-b border-quant-border flex items-center justify-between px-3">
            <div className="flex gap-2">
              <button className={cn("text-xs px-2 py-1 rounded font-medium", positionMode==='open'?"bg-quant-gold/10 text-quant-gold":"text-muted-foreground hover:text-foreground")} onClick={()=>setPositionMode('open')}>开仓</button>
              <button className={cn("text-xs px-2 py-1 rounded font-medium", positionMode==='close'?"bg-quant-gold/10 text-quant-gold":"text-muted-foreground hover:text-foreground")} onClick={()=>{ setPositionMode('close'); if (positions.length===0){ toast('info', '当前无持仓，无需平仓') } else { setActiveBottomTab('positions'); setBottomHeight(h=>Math.max(h,180)) } }}>平仓</button>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-[11px] text-muted-foreground">{marginMode==='cross'?'全仓':'逐仓'}</span>
              <span className="text-[11px] text-quant-gold font-bold">{leverage}x</span>
              <Settings className="w-3.5 h-3.5 text-muted-foreground cursor-pointer hover:text-foreground" onClick={() => setShowSettingsModal(true)}/>
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
          <div className="flex-1 p-3 flex flex-col gap-3 overflow-y-auto">
            {/* 订单类型切换 */}
            <div className="flex gap-1 bg-quant-bg p-0.5 rounded">
              {(['LIMIT','MARKET','STOP_LIMIT'] as const).map(t=>(
                <button key={t} onClick={()=>setOrderType(t)} className={cn("flex-1 py-1 text-[11px] font-medium rounded transition-colors", orderType===t?"bg-quant-bg-secondary text-foreground":"text-muted-foreground hover:text-foreground")}>{t==='LIMIT'?'限价':t==='MARKET'?'市价':'条件'}</button>
              ))}
              <button onClick={()=>setShowTpSl(!showTpSl)} className={cn("flex-1 py-1 text-[11px] rounded transition-colors", showTpSl?"bg-quant-bg-secondary text-foreground":"text-muted-foreground hover:text-foreground")}>止盈止损</button>
              <button onClick={()=>setShowAdvanced(!showAdvanced)} className={cn("flex-1 py-1 text-[11px] rounded transition-colors", showAdvanced?"bg-quant-bg-secondary text-foreground":"text-muted-foreground hover:text-foreground")}>高级</button>
            </div>

            {/* 触发价格输入 */}
            {orderType==='STOP_LIMIT'&&(
              <div className="flex flex-col gap-1.5">
                <div className="flex justify-between text-[10px] text-muted-foreground"><span>触发价格</span><span>USDT</span></div>
                <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-10 focus-within:border-quant-gold transition-all">
                  <input value={tpPrice} onChange={e=>setTpPrice(e.target.value)} placeholder={lastPrice?lastPrice.toFixed(precision.price):'0'.padEnd(precision.price+2, '0')} aria-label="触发价格" className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"/>
                  <span className="text-[10px] text-muted-foreground ml-2">USDT</span>
                </div>
              </div>
            )}

            {/* 委托价格输入 + 快捷按钮 */}
            {orderType==='LIMIT'&&(
              <div className="flex flex-col gap-1.5">
                <div className="flex justify-between text-[10px] text-muted-foreground"><span>委托价格</span><span>USDT</span></div>
                <div className="flex flex-col gap-1">
                  <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-10 focus-within:border-quant-gold transition-all">
                    <input value={price} onChange={e=>setPrice(e.target.value)} placeholder={lastPrice?lastPrice.toFixed(precision.price):'0'.padEnd(precision.price+2, '0')} aria-label="价格" className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"/>
                    <span className="text-[10px] text-muted-foreground ml-2">USDT</span>
                  </div>
                  {/* 价格快捷按钮 */}
                  <div className="flex gap-1">
                    <button onClick={() => { if(lastPrice) setPrice((lastPrice * 0.999).toFixed(precision.price)) }} className="flex-1 py-1 text-[10px] text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border rounded hover:border-quant-gold/50 transition-colors">-0.1%</button>
                    <button onClick={() => { if(lastPrice) setPrice((lastPrice * 0.995).toFixed(precision.price)) }} className="flex-1 py-1 text-[10px] text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border rounded hover:border-quant-gold/50 transition-colors">-0.5%</button>
                    <button onClick={() => { if(lastPrice) setPrice(lastPrice.toFixed(precision.price)) }} className="flex-1 py-1 text-[10px] text-quant-gold hover:text-quant-gold/80 bg-quant-bg border border-quant-gold/30 rounded hover:bg-quant-gold/10 transition-colors">最新价</button>
                    <button onClick={() => { if(lastPrice) setPrice((lastPrice * 1.005).toFixed(precision.price)) }} className="flex-1 py-1 text-[10px] text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border rounded hover:border-quant-gold/50 transition-colors">+0.5%</button>
                    <button onClick={() => { if(lastPrice) setPrice((lastPrice * 1.001).toFixed(precision.price)) }} className="flex-1 py-1 text-[10px] text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border rounded hover:border-quant-gold/50 transition-colors">+0.1%</button>
                  </div>
                </div>
              </div>
            )}

            {/* 数量/金额输入 - 支持单位切换 */}
            <div className="flex flex-col gap-1.5">
              <div className="flex justify-between items-center text-[10px] text-muted-foreground">
                <span>{amountMode === 'quantity' ? '数量' : '保证金'}</span>
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
                // 金额模式（USDT保证金）
                <>
                  <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-10 focus-within:border-quant-gold transition-all">
                    <input 
                      value={amountValue} 
                      onChange={e=>{setAmountValue(e.target.value); setSliderValue(0);}} 
                      placeholder="0.00" 
                      aria-label="保证金" 
                      className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"/>
                    <span className="text-[10px] text-muted-foreground ml-2">USDT</span>
                  </div>
                  {/* 显示对应的数量 */}
                  {(() => {
                    const calcPrice = orderType === 'MARKET' ? lastPrice : (parseFloat(price) || lastPrice)
                    const amount = parseFloat(amountValue) || 0
                    const qty = calcPrice > 0 ? (amount * leverage) / calcPrice : 0
                    return qty > 0 ? (
                      <div className="text-[10px] text-muted-foreground text-right">
                        ≈ {qty.toFixed(precision.quantity)} {symbol.replace('USDT','')} (名义价值: {(qty * calcPrice).toFixed(2)} USDT)
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
                      // 金额模式：保证金百分比
                      const margin = futuresBalance * pct
                      setAmountValue(margin > 0 ? margin.toFixed(2) : '')
                    } else {
                      // 数量模式：根据保证金计算数量
                      const calcQty = calcPrice > 0 ? (futuresBalance * pct * leverage) / calcPrice : 0
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
                {[0.25, 0.5, 0.75, 1].map((pct) => {
                  const pctLabel = Math.round(pct * 100) + '%'
                  return (
                    <button 
                      key={pctLabel} 
                      onClick={() => {
                        setSliderValue(Math.round(pct * 100))
                        const calcPrice = orderType === 'MARKET' ? lastPrice : (parseFloat(price) || lastPrice)
                        
                        if (amountMode === 'amount') {
                          const margin = futuresBalance * pct
                          setAmountValue(margin > 0 ? margin.toFixed(2) : '')
                        } else {
                          const calcQty = calcPrice > 0 ? (futuresBalance * pct * leverage) / calcPrice : 0
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
                })}
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
                      setTpPrice((lastPrice * 1.02).toFixed(precision.price))
                      setSlPrice((lastPrice * 0.99).toFixed(precision.price))
                    }
                  }} className="text-[10px] text-quant-gold hover:text-quant-gold/80 transition-colors">智能设置</button>
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
                <span>可用保证金</span>
                <span className="font-mono text-foreground">{futuresBalance.toFixed(2)} USDT</span>
              </div>
              <div className="flex justify-between text-muted-foreground">
                <span>成交额</span>
                <span className="font-mono text-foreground">{preview.notional>0?preview.notional.toFixed(2):'--'} USDT</span>
              </div>
              <div className="flex justify-between text-muted-foreground">
                <span>保证金</span>
                <span className="font-mono text-foreground">{preview.margin>0?preview.margin.toFixed(2):'--'} USDT</span>
              </div>
              <div className="flex justify-between text-muted-foreground">
                <span>手续费</span>
                <span className="font-mono text-foreground">{preview.fee>0?preview.fee.toFixed(4):'--'} USDT</span>
              </div>
            </div>

            {/* 主要下单按钮 */}
            {positionMode === 'close' ? (
              <div className="py-2 text-center text-[11px] text-muted-foreground">
                当前为平仓模式，请在下方持仓列表中操作平仓
              </div>
            ) : (
              <>
                <button onClick={()=>handlePlaceOrder('BUY')} disabled={submitting} className={cn(
                  "w-full py-3 rounded-lg text-sm font-bold transition-all duration-200 shadow-lg disabled:opacity-60",
                  submitting?"bg-[#0ECB81]":"bg-[#0ECB81] hover:bg-[#0ECB81]/90 active:scale-[0.98] text-black"
                )}>
                  {submitting?'提交中...':`开多 ${leverage}x`}
                </button>
                <button onClick={()=>handlePlaceOrder('SELL')} disabled={submitting} className={cn(
                  "w-full py-3 rounded-lg text-sm font-bold transition-all duration-200 shadow-lg disabled:opacity-60",
                  submitting?"bg-[#F6465D]":"bg-[#F6465D] hover:bg-[#F6465D]/90 active:scale-[0.98] text-white"
                )}>
                  {submitting?'提交中...':`开空 ${leverage}x`}
                </button>
              </>
            )}

            {/* 快捷下单按钮 - 25%/50%/75%/100% */}
            <div className="grid grid-cols-4 gap-1.5">
              {[0.25,0.5,0.75,1].map(pct=>{
                const pctLabel = Math.round(pct*100)+'%'
                const calcPrice = orderType==='MARKET'?lastPrice:(parseFloat(price)||lastPrice)
                const calcQty = calcPrice>0?(futuresBalance*pct*leverage)/calcPrice:0
                const quickOrder = async (side: 'BUY' | 'SELL') => {
                  if (!calcQty || calcQty <= 0) {
                    toast('error', '可用保证金不足')
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
                      market_type: 'swap',
                      position_side: side === 'BUY' ? 'LONG' : 'SHORT',
                      leverage,
                      margin_mode: marginMode,
                      time_in_force: timeInForce,
                      post_only: postOnly,
                      slippage: orderType === 'MARKET' ? parseFloat(slippage) / 100 : undefined,
                    })
                    toast('success', `${side === 'BUY' ? '开多' : '开空'} ${calcQty.toFixed(precision.quantity)} ${symbol.replace('USDT','')} @${leverage}x`)
                    queryClient.invalidateQueries({queryKey:['orders']})
                    queryClient.invalidateQueries({queryKey:['positions']})
                    queryClient.invalidateQueries({queryKey:['portfolio']})
                  } catch (e: unknown) {
                    const err = e instanceof Error ? e : new Error(String(e))
                    toast('error', err.message || '下单失败')
                  } finally {
                    setSubmitting(false)
                  }
                }
                return (
                  <div key={pctLabel} className="flex gap-1">
                    <button 
                      onClick={() => quickOrder('BUY')}
                      disabled={submitting}
                      className="flex-1 py-2 text-[11px] font-bold rounded-lg transition-all duration-200 bg-[#0ECB81]/10 hover:bg-[#0ECB81]/20 text-[#0ECB81] border border-[#0ECB81]/20 hover:border-[#0ECB81]/40 disabled:opacity-50"
                    >
                      多{Math.round(pct*100)}%
                    </button>
                    <button 
                      onClick={() => quickOrder('SELL')}
                      disabled={submitting}
                      className="flex-1 py-2 text-[11px] font-bold rounded-lg transition-all duration-200 bg-[#F6465D]/10 hover:bg-[#F6465D]/20 text-[#F6465D] border border-[#F6465D]/20 hover:border-[#F6465D]/40 disabled:opacity-50"
                    >
                      空{Math.round(pct*100)}%
                    </button>
                  </div>
                )
              })}
            </div>
          </div>

          {/* Account Info Panel */}
          <div className="border-t border-quant-border p-3">
            <div className="flex items-center justify-between mb-2">
              <span className="text-xs font-medium text-foreground">账户</span>
              <span className="text-[10px] text-quant-gold cursor-pointer hover:underline" onClick={() => setShowTransferModal(true)}>划转</span>
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
              <button onClick={() => setShowTransferModal(true)} className="flex-1 py-1.5 text-[10px] bg-quant-bg border border-quant-border rounded text-muted-foreground hover:text-foreground transition-colors">划转</button>
              <button onClick={() => setShowBuyModal(true)} className="flex-1 py-1.5 text-[10px] bg-quant-bg border border-quant-border rounded text-muted-foreground hover:text-foreground transition-colors">买币</button>
              <button onClick={() => setShowSwapModal(true)} className="flex-1 py-1.5 text-[10px] bg-quant-bg border border-quant-border rounded text-muted-foreground hover:text-foreground transition-colors">兑换</button>
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

      {/* ═══════════════════════════════════════════════
         Settings Modal
         ═══════════════════════════════════════════════ */}
      {showSettingsModal && (
        <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4" onClick={() => setShowSettingsModal(false)}>
          <div className="w-full max-w-md rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between px-5 py-4 border-b border-quant-border">
              <h3 className="text-sm font-bold flex items-center gap-2"><Settings className="w-4 h-4 text-quant-gold"/> 合约设置</h3>
              <button onClick={() => setShowSettingsModal(false)} className="p-1 rounded text-muted-foreground hover:text-foreground"><X className="w-4 h-4"/></button>
            </div>
            <div className="p-5 space-y-6">
              <div>
                <label className="text-[11px] text-muted-foreground mb-2 block">杠杆倍数</label>
                <div className="flex gap-1.5 flex-wrap">
                  {LEVERAGES.map(l => (
                    <button key={l} onClick={() => setLeverage(l)} className={cn("px-4 py-2 text-xs rounded-lg border transition-colors", leverage===l ? "border-quant-gold text-quant-gold bg-quant-gold/10" : "border-quant-border text-muted-foreground hover:text-foreground")}>{l}x</button>
                  ))}
                </div>
              </div>
              <div>
                <label className="text-[11px] text-muted-foreground mb-2 block">保证金模式</label>
                <div className="flex gap-2">
                  <button onClick={() => setMarginMode('cross')} className={cn("flex-1 py-3 text-xs rounded-lg border transition-colors", marginMode==='cross' ? "border-quant-gold text-quant-gold bg-quant-gold/10" : "border-quant-border text-muted-foreground hover:text-foreground")}>全仓 (Cross)</button>
                  <button onClick={() => setMarginMode('isolated')} className={cn("flex-1 py-3 text-xs rounded-lg border transition-colors", marginMode==='isolated' ? "border-quant-gold text-quant-gold bg-quant-gold/10" : "border-quant-border text-muted-foreground hover:text-foreground")}>逐仓 (Isolated)</button>
                </div>
              </div>
              <div>
                <label className="text-[11px] text-muted-foreground mb-2 block">持仓模式</label>
                <div className="flex gap-2">
                  <button onClick={() => setPositionMode('open')} className={cn("flex-1 py-3 text-xs rounded-lg border transition-colors", positionMode==='open' ? "border-quant-gold text-quant-gold bg-quant-gold/10" : "border-quant-border text-muted-foreground hover:text-foreground")}>开仓</button>
                  <button onClick={() => setPositionMode('close')} className={cn("flex-1 py-3 text-xs rounded-lg border transition-colors", positionMode==='close' ? "border-quant-gold text-quant-gold bg-quant-gold/10" : "border-quant-border text-muted-foreground hover:text-foreground")}>平仓</button>
                </div>
              </div>
              <div className="text-[10px] text-muted-foreground bg-quant-bg-secondary rounded-lg p-3">
                调整杠杆和保证金模式会影响当前持仓。更改将在下一次开仓时生效。
              </div>
              <button onClick={() => { setShowSettingsModal(false); toast('success', '合约设置已保存') }} className="w-full py-2.5 rounded-lg bg-quant-gold text-black text-xs font-bold hover:opacity-90 transition-opacity">保存设置</button>
            </div>
          </div>
        </div>
      )}

      {/* ═══════════════════════════════════════════════
         Transfer Modal
         ═══════════════════════════════════════════════ */}
      {showTransferModal && (
        <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4" onClick={() => setShowTransferModal(false)}>
          <div className="w-full max-w-md rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between px-5 py-4 border-b border-quant-border">
              <h3 className="text-sm font-bold flex items-center gap-2"><ArrowRightLeft className="w-4 h-4 text-quant-gold"/> 资金划转</h3>
              <button onClick={() => setShowTransferModal(false)} className="p-1 rounded text-muted-foreground hover:text-foreground"><X className="w-4 h-4"/></button>
            </div>
            <div className="p-5 space-y-5">
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="text-[10px] text-muted-foreground mb-1.5 block">从</label>
                  <select value={transferFrom} onChange={e => { setTransferFrom(e.target.value); setTransferTo(e.target.value === 'spot' ? 'futures' : 'spot') }} className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2.5 text-xs text-foreground outline-none focus:border-quant-gold">
                    <option value="futures">合约钱包</option>
                    <option value="spot">现货钱包</option>
                    <option value="funding">资金钱包</option>
                  </select>
                </div>
                <div>
                  <label className="text-[10px] text-muted-foreground mb-1.5 block">到</label>
                  <select value={transferTo} onChange={e => setTransferTo(e.target.value)} className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2.5 text-xs text-foreground outline-none focus:border-quant-gold">
                    <option value="spot">现货钱包</option>
                    <option value="futures">合约钱包</option>
                    <option value="funding">资金钱包</option>
                  </select>
                </div>
              </div>
              <div>
                <label className="text-[10px] text-muted-foreground mb-1.5 block">币种</label>
                <select value={transferCurrency} onChange={e => setTransferCurrency(e.target.value)} className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2.5 text-xs text-foreground outline-none focus:border-quant-gold">
                  <option value="USDT">USDT</option>
                  <option value="BTC">BTC</option>
                  <option value="ETH">ETH</option>
                </select>
              </div>
              <div>
                <label className="text-[10px] text-muted-foreground mb-1.5 block">数量</label>
                <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-10 focus-within:border-quant-gold transition-colors">
                  <input type="number" value={transferAmount} onChange={e => setTransferAmount(e.target.value)} placeholder="0.00" min={0.01} step={0.01} className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 outline-none text-foreground placeholder:text-muted-foreground"/>
                  <span className="text-[10px] text-muted-foreground ml-2">{transferCurrency}</span>
                </div>
              </div>
              <button
                onClick={() => {
                  const amt = parseFloat(transferAmount)
                  if (!amt || amt <= 0) { toast('error', '请输入有效数量'); return }
                  transferMut.mutate({ from: transferFrom, to: transferTo, currency: transferCurrency, amount: amt })
                }}
                disabled={transferMut.isPending}
                className="w-full py-2.5 rounded-lg bg-quant-gold text-black text-xs font-bold hover:opacity-90 transition-opacity disabled:opacity-50"
              >
                {transferMut.isPending ? '划转中...' : '确认划转'}
              </button>
              <div className="text-[10px] text-muted-foreground bg-quant-bg-secondary rounded-lg p-3 leading-relaxed">
                提示：划转将在内部钱包之间移动资金。实际划转可能需要交易所处理时间。
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ═══════════════════════════════════════════════
         Buy Crypto Modal
         ═══════════════════════════════════════════════ */}
      {showBuyModal && (
        <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4" onClick={() => setShowBuyModal(false)}>
          <div className="w-full max-w-md rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between px-5 py-4 border-b border-quant-border">
              <h3 className="text-sm font-bold flex items-center gap-2"><DollarSign className="w-4 h-4 text-quant-gold"/> 买币</h3>
              <button onClick={() => setShowBuyModal(false)} className="p-1 rounded text-muted-foreground hover:text-foreground"><X className="w-4 h-4"/></button>
            </div>
            <div className="p-5 space-y-5">
              <div className="flex gap-3">
                <div className="flex-1">
                  <label className="text-[10px] text-muted-foreground mb-1.5 block">购买币种</label>
                  <select value={buyCurrency} onChange={e => setBuyCurrency(e.target.value)} className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2.5 text-xs text-foreground outline-none focus:border-quant-gold">
                    <option value="BTC">BTC</option>
                    <option value="ETH">ETH</option>
                    <option value="SOL">SOL</option>
                    <option value="BNB">BNB</option>
                    <option value="USDT">USDT</option>
                  </select>
                </div>
                <div className="flex-1">
                  <label className="text-[10px] text-muted-foreground mb-1.5 block">支付方式</label>
                  <select value={buyMethod} onChange={e => setBuyMethod(e.target.value)} className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2.5 text-xs text-foreground outline-none focus:border-quant-gold">
                    <option value="credit_card">信用卡</option>
                    <option value="bank_transfer">银行卡</option>
                    <option value="alipay">支付宝</option>
                    <option value="wechat">微信支付</option>
                  </select>
                </div>
              </div>
              <div>
                <label className="text-[10px] text-muted-foreground mb-1.5 block">金额 (USDT)</label>
                <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-10 focus-within:border-quant-gold transition-colors">
                  <input type="number" value={buyAmount} onChange={e => setBuyAmount(e.target.value)} placeholder="100" min={1} step={1} className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 outline-none text-foreground placeholder:text-muted-foreground"/>
                  <span className="text-[10px] text-muted-foreground">USDT</span>
                </div>
              </div>
              <button
                onClick={() => {
                  const amt = parseFloat(buyAmount)
                  if (!amt || amt <= 0) { toast('error', '请输入有效金额'); return }
                  buyMut.mutate({ currency: buyCurrency, amount: amt, payment_method: buyMethod })
                }}
                disabled={buyMut.isPending}
                className="w-full py-2.5 rounded-lg bg-quant-gold text-black text-xs font-bold hover:opacity-90 transition-opacity disabled:opacity-50"
              >
                {buyMut.isPending ? '购买中...' : `使用 ${buyMethod==='credit_card'?'信用卡':buyMethod==='bank_transfer'?'银行卡':buyMethod==='alipay'?'支付宝':'微信支付'} 购买 ${buyCurrency}`}
              </button>
              <div className="text-[10px] text-muted-foreground bg-quant-bg-secondary rounded-lg p-3 leading-relaxed">
                买币功能通过第三方服务商提供。实际成交价格和可用性以服务商为准。
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ═══════════════════════════════════════════════
         Swap Modal
         ═══════════════════════════════════════════════ */}
      {showSwapModal && (
        <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4" onClick={() => setShowSwapModal(false)}>
          <div className="w-full max-w-md rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between px-5 py-4 border-b border-quant-border">
              <h3 className="text-sm font-bold flex items-center gap-2"><Repeat className="w-4 h-4 text-quant-gold"/> 兑换</h3>
              <button onClick={() => setShowSwapModal(false)} className="p-1 rounded text-muted-foreground hover:text-foreground"><X className="w-4 h-4"/></button>
            </div>
            <div className="p-5 space-y-5">
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="text-[10px] text-muted-foreground mb-1.5 block">从</label>
                  <select value={swapFrom} onChange={e => setSwapFrom(e.target.value)} className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2.5 text-xs text-foreground outline-none focus:border-quant-gold">
                    <option value="BTC">BTC</option>
                    <option value="ETH">ETH</option>
                    <option value="SOL">SOL</option>
                    <option value="BNB">BNB</option>
                    <option value="USDT">USDT</option>
                  </select>
                </div>
                <div>
                  <label className="text-[10px] text-muted-foreground mb-1.5 block">到</label>
                  <select value={swapTo} onChange={e => setSwapTo(e.target.value)} className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2.5 text-xs text-foreground outline-none focus:border-quant-gold">
                    <option value="ETH">ETH</option>
                    <option value="BTC">BTC</option>
                    <option value="SOL">SOL</option>
                    <option value="BNB">BNB</option>
                    <option value="USDT">USDT</option>
                  </select>
                </div>
              </div>
              <div>
                <label className="text-[10px] text-muted-foreground mb-1.5 block">数量</label>
                <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-10 focus-within:border-quant-gold transition-colors">
                  <input type="number" value={swapAmount} onChange={e => setSwapAmount(e.target.value)} placeholder="0.00" min={0.001} step={0.001} className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 outline-none text-foreground placeholder:text-muted-foreground"/>
                  <span className="text-[10px] text-muted-foreground ml-2">{swapFrom}</span>
                </div>
              </div>
              <button
                onClick={() => {
                  const amt = parseFloat(swapAmount)
                  if (!amt || amt <= 0) { toast('error', '请输入有效数量'); return }
                  swapMut.mutate({ from_currency: swapFrom, to_currency: swapTo, amount: amt })
                }}
                disabled={swapMut.isPending}
                className="w-full py-2.5 rounded-lg bg-quant-gold text-black text-xs font-bold hover:opacity-90 transition-opacity disabled:opacity-50"
              >
                {swapMut.isPending ? '兑换中...' : `兑换 ${swapFrom} → ${swapTo}`}
              </button>
              <div className="text-[10px] text-muted-foreground bg-quant-bg-secondary rounded-lg p-3 leading-relaxed">
                兑换价格参考币安实时行情，实际以成交价格为准。兑换将在现货钱包内完成。
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
