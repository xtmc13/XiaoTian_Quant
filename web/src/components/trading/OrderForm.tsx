import React, { useState, useMemo, useCallback } from 'react'
import { cn } from '@/lib/utils'
import { toast } from '@/lib/useToast'
import { orderApi } from '@/lib/api'
import { useQueryClient } from '@tanstack/react-query'
import type { Order } from '@/types'

export type OrderFormMode = 'spot' | 'contract'
export type OrderFormSide = 'BUY' | 'SELL'
export type OrderFormType = 'LIMIT' | 'MARKET' | 'STOP_LIMIT'
export type TimeInForce = 'GTC' | 'IOC' | 'FOK'
export type MarginMode = 'cross' | 'isolated'
export type PositionMode = 'open' | 'close'
export type AmountMode = 'quantity' | 'amount'

export interface OrderFormProps {
  mode: OrderFormMode
  symbol: string
  lastPrice: number
  precision: { price: number; quantity: number }
  balance: number
  holdings?: Array<Record<string, unknown>>
  leverage?: number
  marginMode?: MarginMode
  positionMode?: PositionMode
  onOrderPlaced?: () => void
}

export const OrderForm = React.memo(function OrderForm({
  mode,
  symbol,
  lastPrice,
  precision,
  balance,
  holdings = [],
  leverage = 1,
  marginMode = 'cross',
  positionMode = 'open',
  onOrderPlaced,
}: OrderFormProps) {
  const queryClient = useQueryClient()
  const [side, setSide] = useState<OrderFormSide>('BUY')
  const [orderType, setOrderType] = useState<OrderFormType>('LIMIT')
  const [price, setPrice] = useState('')
  const [quantity, setQuantity] = useState('')
  const [amountMode, setAmountMode] = useState<AmountMode>('quantity')
  const [amountValue, setAmountValue] = useState('')
  const [sliderValue, setSliderValue] = useState(0)
  const [timeInForce, setTimeInForce] = useState<TimeInForce>('GTC')
  const [postOnly, setPostOnly] = useState(false)
  const [slippage, setSlippage] = useState('0.5')
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [showTpSl, setShowTpSl] = useState(false)
  const [tpPrice, setTpPrice] = useState('')
  const [slPrice, setSlPrice] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const isContract = mode === 'contract'
  const availableTypes: OrderFormType[] = isContract
    ? ['LIMIT', 'MARKET', 'STOP_LIMIT']
    : ['LIMIT', 'MARKET']

  const baseAsset = symbol.replace('USDT', '')

  const assetHolding = useMemo(() => {
    return holdings.find((b) => String(b.asset || b.currency) === baseAsset)
  }, [holdings, baseAsset])

  const assetFree = useMemo(() => {
    return assetHolding ? parseFloat(String(assetHolding.free ?? assetHolding.available ?? 0)) : 0
  }, [assetHolding])

  const calcPrice = useMemo(() => {
    return orderType === 'MARKET' ? lastPrice : (parseFloat(price) || lastPrice)
  }, [orderType, price, lastPrice])

  const preview = useMemo(() => {
    let qty: number
    if (amountMode === 'amount') {
      const amount = parseFloat(amountValue) || 0
      qty = isContract
        ? calcPrice > 0 ? (amount * leverage) / calcPrice : 0
        : calcPrice > 0 ? amount / calcPrice : 0
    } else {
      qty = parseFloat(quantity) || 0
    }
    const pr = calcPrice
    const notional = qty * pr
    const margin = isContract && leverage > 0 ? notional / leverage : notional
    const feeRate = 0.0005
    const fee = notional * feeRate
    let maxLoss = 0
    if (slPrice && parseFloat(slPrice) > 0) {
      const sl = parseFloat(slPrice)
      maxLoss = Math.abs(qty * (pr - sl))
    }
    return { notional, margin, fee, maxLoss, qty, pr }
  }, [quantity, amountMode, amountValue, calcPrice, isContract, leverage, slPrice])

  const handlePlaceOrder = useCallback(async (orderSide?: OrderFormSide) => {
    const finalSide = orderSide || side
    let qty: number
    if (amountMode === 'amount') {
      const amount = parseFloat(amountValue)
      if (!amount || amount <= 0) { toast('error', '请输入有效金额'); return }
      if (!calcPrice || calcPrice <= 0) { toast('error', '无法获取有效价格'); return }
      qty = isContract ? (amount * leverage) / calcPrice : amount / calcPrice
    } else {
      qty = parseFloat(quantity)
      if (!qty || qty <= 0) { toast('error', '请输入有效数量'); return }
    }

    if ((orderType === 'LIMIT' || orderType === 'STOP_LIMIT') && !parseFloat(price)) {
      toast('error', '请输入有效价格'); return
    }

    setSubmitting(true)
    try {
      const req: Record<string, unknown> = {
        symbol,
        side: finalSide,
        order_type: orderType,
        price: orderType === 'MARKET' ? 0 : (parseFloat(price) || 0),
        quantity: qty,
        market_type: isContract ? 'swap' : 'spot',
        time_in_force: timeInForce,
        post_only: postOnly,
        slippage: orderType === 'MARKET' ? parseFloat(slippage) / 100 : undefined,
      }
      if (isContract) {
        req.leverage = leverage
        req.margin_mode = marginMode
        req.position_side = finalSide === 'BUY' ? 'LONG' : 'SHORT'
        if (orderType === 'STOP_LIMIT') {
          req.stop_price = parseFloat(tpPrice) || 0
        }
      }
      if (showTpSl) {
        req.tp_price = tpPrice ? parseFloat(tpPrice) : undefined
        req.sl_price = slPrice ? parseFloat(slPrice) : undefined
      }
      await orderApi.place(req)
      toast('success', '订单已提交')
      setQuantity(''); setPrice(''); setAmountValue(''); setSliderValue(0); setTpPrice(''); setSlPrice('')
      queryClient.invalidateQueries({ queryKey: ['orders'] })
      queryClient.invalidateQueries({ queryKey: ['portfolio'] })
      if (isContract) {
        queryClient.invalidateQueries({ queryKey: ['positions'] })
      }
      onOrderPlaced?.()
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      toast('error', err.message || '下单失败')
    } finally {
      setSubmitting(false)
    }
  }, [symbol, side, orderType, price, quantity, amountMode, amountValue, calcPrice, isContract, leverage, marginMode, tpPrice, slPrice, showTpSl, timeInForce, postOnly, slippage, queryClient, onOrderPlaced])

  const handleQuickOrder = useCallback(async (pct: number, quickSide: OrderFormSide) => {
    const calcQty = isContract
      ? calcPrice > 0 ? (balance * pct * leverage) / calcPrice : 0
      : quickSide === 'BUY'
        ? calcPrice > 0 ? (balance * pct) / calcPrice : 0
        : assetFree * pct

    if (!calcQty || calcQty <= 0) {
      toast('error', quickSide === 'BUY' ? '余额不足' : '持仓不足')
      return
    }
    setSubmitting(true)
    try {
      const req: Record<string, unknown> = {
        symbol,
        side: quickSide,
        order_type: orderType,
        price: orderType === 'MARKET' ? 0 : (parseFloat(price) || 0),
        quantity: calcQty,
        market_type: isContract ? 'swap' : 'spot',
        time_in_force: timeInForce,
        post_only: postOnly,
        slippage: orderType === 'MARKET' ? parseFloat(slippage) / 100 : undefined,
      }
      if (isContract) {
        req.leverage = leverage
        req.margin_mode = marginMode
        req.position_side = quickSide === 'BUY' ? 'LONG' : 'SHORT'
      }
      await orderApi.place(req)
      toast('success', `${quickSide === 'BUY' ? (isContract ? '开多' : '买入') : (isContract ? '开空' : '卖出')} ${calcQty.toFixed(precision.quantity)} ${baseAsset}${isContract ? ` @${leverage}x` : ''}`)
      queryClient.invalidateQueries({ queryKey: ['orders'] })
      queryClient.invalidateQueries({ queryKey: ['portfolio'] })
      if (isContract) queryClient.invalidateQueries({ queryKey: ['positions'] })
      onOrderPlaced?.()
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      toast('error', err.message || '下单失败')
    } finally {
      setSubmitting(false)
    }
  }, [symbol, orderType, price, calcPrice, isContract, balance, leverage, marginMode, baseAsset, precision, timeInForce, postOnly, slippage, assetFree, queryClient, onOrderPlaced])

  const updateSlider = useCallback((val: number) => {
    setSliderValue(val)
    const pct = val / 100
    if (amountMode === 'amount') {
      const amount = balance * pct
      setAmountValue(amount > 0 ? amount.toFixed(2) : '')
    } else {
      const calcQty = isContract
        ? calcPrice > 0 ? (balance * pct * leverage) / calcPrice : 0
        : side === 'BUY'
          ? calcPrice > 0 ? (balance * pct) / calcPrice : 0
          : assetFree * pct
      setQuantity(calcQty > 0 ? calcQty.toFixed(precision.quantity) : '')
    }
  }, [amountMode, balance, isContract, calcPrice, leverage, side, assetFree, precision])

  const updatePctButton = useCallback((pct: number) => {
    setSliderValue(Math.round(pct * 100))
    if (amountMode === 'amount') {
      const amount = balance * pct
      setAmountValue(amount > 0 ? amount.toFixed(2) : '')
    } else {
      const calcQty = isContract
        ? calcPrice > 0 ? (balance * pct * leverage) / calcPrice : 0
        : side === 'BUY'
          ? calcPrice > 0 ? (balance * pct) / calcPrice : 0
          : assetFree * pct
      setQuantity(calcQty > 0 ? calcQty.toFixed(precision.quantity) : '')
    }
  }, [amountMode, balance, isContract, calcPrice, leverage, side, assetFree, precision])

  if (isContract && positionMode === 'close') {
    return (
      <div className="flex-1 p-3 flex flex-col gap-3 overflow-y-auto">
        <div className="py-2 text-center text-[11px] text-muted-foreground">
          当前为平仓模式，请在下方持仓列表中操作平仓
        </div>
      </div>
    )
  }

  return (
    <div className="flex-1 p-3 flex flex-col gap-3 overflow-y-auto">
      {/* 订单类型切换 */}
      <div className="flex gap-1 bg-quant-bg p-0.5 rounded">
        {availableTypes.map(t => (
          <button key={t} onClick={() => setOrderType(t)} className={cn(
            "flex-1 py-1.5 text-[11px] font-medium rounded transition-colors",
            orderType === t ? "bg-quant-bg-secondary text-foreground" : "text-muted-foreground hover:text-foreground"
          )}>
            {t === 'LIMIT' ? '限价' : t === 'MARKET' ? '市价' : '条件'}
          </button>
        ))}
        <button onClick={() => setShowTpSl(!showTpSl)} className={cn(
          "flex-1 py-1.5 text-[11px] rounded transition-colors",
          showTpSl ? "bg-quant-bg-secondary text-foreground" : "text-muted-foreground hover:text-foreground"
        )}>止盈止损</button>
        <button onClick={() => setShowAdvanced(!showAdvanced)} className={cn(
          "flex-1 py-1.5 text-[11px] rounded transition-colors",
          showAdvanced ? "bg-quant-bg-secondary text-foreground" : "text-muted-foreground hover:text-foreground"
        )}>高级</button>
      </div>

      {/* 方向选择 */}
      {!isContract ? (
        <div className="flex gap-1.5">
          <button onClick={() => setSide('BUY')} className={cn(
            "flex-1 py-2.5 text-sm font-bold rounded-lg transition-all duration-200",
            side === 'BUY' ? "bg-[#0ECB81] hover:bg-[#0ECB81]/90 text-black shadow-lg shadow-[#0ECB81]/20" : "bg-quant-bg hover:bg-[#0ECB81]/10 text-muted-foreground border border-quant-border hover:border-[#0ECB81]/50"
          )}>买入</button>
          <button onClick={() => setSide('SELL')} className={cn(
            "flex-1 py-2.5 text-sm font-bold rounded-lg transition-all duration-200",
            side === 'SELL' ? "bg-[#F6465D] hover:bg-[#F6465D]/90 text-white shadow-lg shadow-[#F6465D]/20" : "bg-quant-bg hover:bg-[#F6465D]/10 text-muted-foreground border border-quant-border hover:border-[#F6465D]/50"
          )}>卖出</button>
        </div>
      ) : null}

      {/* 触发价格 */}
      {orderType === 'STOP_LIMIT' && (
        <div className="flex flex-col gap-1.5">
          <div className="flex justify-between text-[10px] text-muted-foreground"><span>触发价格</span><span>USDT</span></div>
          <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-10 focus-within:border-quant-gold transition-all">
            <input value={tpPrice} onChange={e => setTpPrice(e.target.value)} placeholder={lastPrice ? lastPrice.toFixed(precision.price) : '0'} aria-label="触发价格" className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground" />
            <span className="text-[10px] text-muted-foreground ml-2">USDT</span>
          </div>
        </div>
      )}

      {/* 价格输入 */}
      {orderType === 'LIMIT' && (
        <div className="flex flex-col gap-1.5">
          <div className="flex justify-between text-[10px] text-muted-foreground"><span>{isContract ? '委托价格' : '价格'}</span><span>USDT</span></div>
          <div className="flex flex-col gap-1">
            <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-10 focus-within:border-quant-gold transition-all">
              <input value={price} onChange={e => setPrice(e.target.value)} placeholder={lastPrice ? lastPrice.toFixed(precision.price) : '0'} aria-label="价格" className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground" />
              <span className="text-[10px] text-muted-foreground ml-2">USDT</span>
            </div>
            <div className="flex gap-1">
              {['-0.1%', '-0.5%', '最新价', '+0.5%', '+0.1%'].map((label, i) => {
                const multipliers = [0.999, 0.995, 1, 1.005, 1.001]
                const isMid = i === 2
                return (
                  <button key={label} onClick={() => {
                    if (lastPrice) setPrice((lastPrice * multipliers[i]).toFixed(precision.price))
                  }} className={cn(
                    "flex-1 py-1 text-[10px] rounded transition-colors",
                    isMid ? "text-quant-gold hover:text-quant-gold/80 bg-quant-bg border border-quant-gold/30 hover:bg-quant-gold/10 font-medium" : "text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border hover:border-quant-gold/50"
                  )}>{label}</button>
                )
              })}
            </div>
          </div>
        </div>
      )}

      {/* 数量/金额输入 */}
      <div className="flex flex-col gap-1.5">
        <div className="flex justify-between items-center text-[10px] text-muted-foreground">
          <span>{amountMode === 'quantity' ? '数量' : isContract ? '保证金' : '金额'}</span>
          <div className="flex gap-1">
            <button onClick={() => setAmountMode('quantity')} className={cn("px-2 py-0.5 rounded text-[10px] transition-colors", amountMode === 'quantity' ? "bg-quant-gold/20 text-quant-gold" : "hover:bg-white/5")}>{baseAsset}</button>
            <button onClick={() => setAmountMode('amount')} className={cn("px-2 py-0.5 rounded text-[10px] transition-colors", amountMode === 'amount' ? "bg-quant-gold/20 text-quant-gold" : "hover:bg-white/5")}>USDT</button>
          </div>
        </div>
        {amountMode === 'quantity' ? (
          <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-10 focus-within:border-quant-gold transition-all">
            <input value={quantity} onChange={e => { setQuantity(e.target.value); setSliderValue(0); }} placeholder={'0'.padEnd(precision.quantity + 2, '0')} aria-label="数量" className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground" />
            <span className="text-[10px] text-muted-foreground ml-2">{baseAsset}</span>
          </div>
        ) : (
          <>
            <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-10 focus-within:border-quant-gold transition-all">
              <input value={amountValue} onChange={e => { setAmountValue(e.target.value); setSliderValue(0); }} placeholder="0.00" aria-label={isContract ? '保证金' : '金额'} className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground" />
              <span className="text-[10px] text-muted-foreground ml-2">USDT</span>
            </div>
            {preview.qty > 0 && (
              <div className="text-[10px] text-muted-foreground text-right">
                ≈ {preview.qty.toFixed(precision.quantity)} {baseAsset}{isContract ? ` (名义价值: ${(preview.qty * calcPrice).toFixed(2)} USDT)` : ''}
              </div>
            )}
          </>
        )}

        {/* 滑块 */}
        <div className="flex flex-col gap-1">
          <input type="range" min="0" max="100" step="1" value={sliderValue} onChange={e => updateSlider(parseInt(e.target.value))}
            className="w-full h-1 bg-quant-border rounded-lg appearance-none cursor-pointer [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:w-3 [&::-webkit-slider-thumb]:h-3 [&::-webkit-slider-thumb]:bg-quant-gold [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:cursor-pointer" />
          <div className="flex justify-between text-[9px] text-muted-foreground"><span>0%</span><span>{sliderValue}%</span><span>100%</span></div>
        </div>

        {/* 百分比快捷按钮 */}
        <div className="flex gap-1">
          {[0.25, 0.5, 0.75, 1].map(pct => {
            const pctLabel = Math.round(pct * 100) + '%'
            return (
              <button key={pctLabel} onClick={() => updatePctButton(pct)} className={cn(
                "flex-1 py-1.5 text-[10px] font-medium rounded-lg transition-all",
                sliderValue === Math.round(pct * 100) ? "bg-quant-gold/20 text-quant-gold border border-quant-gold/50" : "text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border hover:border-quant-gold/50"
              )}>{pctLabel}</button>
            )
          })}
        </div>
      </div>

      {/* 高级设置 */}
      {showAdvanced && (
        <div className="flex flex-col gap-2 p-2 bg-quant-bg/50 rounded-lg border border-quant-border/50">
          {orderType === 'LIMIT' && (
            <div className="flex flex-col gap-1.5">
              <span className="text-[10px] text-muted-foreground">订单有效期</span>
              <div className="flex gap-1">
                {(['GTC', 'IOC', 'FOK'] as const).map(t => (
                  <button key={t} onClick={() => setTimeInForce(t)} className={cn(
                    "flex-1 py-1 text-[10px] rounded transition-colors",
                    timeInForce === t ? "bg-quant-gold/20 text-quant-gold border border-quant-gold/50" : "text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border"
                  )}>{t === 'GTC' ? '一直有效' : t === 'IOC' ? '立即成交' : '全部成交'}</button>
                ))}
              </div>
              <div className="text-[9px] text-muted-foreground">
                {timeInForce === 'GTC' && '订单会一直有效，直到被成交或取消'}
                {timeInForce === 'IOC' && '订单必须立即成交，未成交部分会被取消'}
                {timeInForce === 'FOK' && '订单必须全部立即成交，否则会被取消'}
              </div>
            </div>
          )}
          {orderType === 'LIMIT' && (
            <label className="flex items-center gap-2 cursor-pointer">
              <input type="checkbox" checked={postOnly} onChange={e => setPostOnly(e.target.checked)} className="w-3 h-3 accent-quant-gold" />
              <span className="text-[10px] text-muted-foreground">只做 Maker（Post-Only）</span>
              <span className="text-[9px] text-muted-foreground/60">确保订单只作为挂单成交</span>
            </label>
          )}
          {orderType === 'MARKET' && (
            <div className="flex flex-col gap-1.5">
              <span className="text-[10px] text-muted-foreground">滑点容忍度</span>
              <div className="flex items-center gap-2">
                <div className="flex gap-1">
                  {['0.1', '0.5', '1', '2'].map(s => (
                    <button key={s} onClick={() => setSlippage(s)} className={cn(
                      "px-2 py-1 text-[10px] rounded transition-colors",
                      slippage === s ? "bg-quant-gold/20 text-quant-gold border border-quant-gold/50" : "text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border"
                    )}>{s}%</button>
                  ))}
                </div>
                <input value={slippage} onChange={e => setSlippage(e.target.value)} className="w-16 px-2 py-1 text-[10px] bg-quant-bg border border-quant-border rounded text-foreground" placeholder="0.5" />
                <span className="text-[10px] text-muted-foreground">%</span>
              </div>
            </div>
          )}
        </div>
      )}

      {/* TP/SL */}
      {showTpSl && (
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
            <input value={tpPrice} onChange={e => setTpPrice(e.target.value)} placeholder="--" aria-label="止盈价格" className="flex-1 bg-transparent text-xs font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground" />
            <span className="text-[10px] text-muted-foreground ml-1">USDT</span>
          </div>
          <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-9 focus-within:border-quant-gold transition-all">
            <span className="text-[10px] text-[#F6465D] w-6">止损</span>
            <input value={slPrice} onChange={e => setSlPrice(e.target.value)} placeholder="--" aria-label="止损价格" className="flex-1 bg-transparent text-xs font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground" />
            <span className="text-[10px] text-muted-foreground ml-1">USDT</span>
          </div>
        </div>
      )}

      {/* 账户信息 */}
      <div className="space-y-1.5 text-[10px]">
        <div className="flex justify-between text-muted-foreground">
          <span>{isContract ? '可用保证金' : '可用'}</span>
          <span className="font-mono text-foreground">{balance.toFixed(2)} USDT</span>
        </div>
        <div className="flex justify-between text-muted-foreground">
          <span>成交额</span>
          <span className="font-mono text-foreground">{preview.notional > 0 ? preview.notional.toFixed(2) : '--'} USDT</span>
        </div>
        {isContract && (
          <div className="flex justify-between text-muted-foreground">
            <span>保证金</span>
            <span className="font-mono text-foreground">{preview.margin > 0 ? preview.margin.toFixed(2) : '--'} USDT</span>
          </div>
        )}
        <div className="flex justify-between text-muted-foreground">
          <span>手续费</span>
          <span className="font-mono text-foreground">{preview.fee > 0 ? preview.fee.toFixed(4) : '--'} USDT</span>
        </div>
      </div>

      {/* 主要下单按钮 */}
      {!isContract ? (
        <button onClick={() => handlePlaceOrder()} disabled={submitting} className={cn(
          "w-full py-3 rounded-lg text-sm font-bold transition-all duration-200 shadow-lg",
          submitting && "opacity-60 cursor-not-allowed",
          side === 'BUY' ? "bg-[#0ECB81] hover:bg-[#0ECB81]/90 active:scale-[0.98] text-black" : "bg-[#F6465D] hover:bg-[#F6465D]/90 active:scale-[0.98] text-white"
        )}>
          {submitting ? '提交中...' : `${side === 'BUY' ? '买入' : '卖出'} ${baseAsset}`}
        </button>
      ) : (
        <>
          <button onClick={() => handlePlaceOrder('BUY')} disabled={submitting} className={cn(
            "w-full py-3 rounded-lg text-sm font-bold transition-all duration-200 shadow-lg disabled:opacity-60",
            submitting ? "bg-[#0ECB81]" : "bg-[#0ECB81] hover:bg-[#0ECB81]/90 active:scale-[0.98] text-black"
          )}>
            {submitting ? '提交中...' : `开多 ${leverage}x`}
          </button>
          <button onClick={() => handlePlaceOrder('SELL')} disabled={submitting} className={cn(
            "w-full py-3 rounded-lg text-sm font-bold transition-all duration-200 shadow-lg disabled:opacity-60",
            submitting ? "bg-[#F6465D]" : "bg-[#F6465D] hover:bg-[#F6465D]/90 active:scale-[0.98] text-white"
          )}>
            {submitting ? '提交中...' : `开空 ${leverage}x`}
          </button>
        </>
      )}

      {/* 快捷下单 */}
      <div className="grid grid-cols-4 gap-1.5">
        {[0.25, 0.5, 0.75, 1].map(pct => {
          const pctLabel = Math.round(pct * 100) + '%'
          return (
            <button key={pctLabel} onClick={() => {
              if (!isContract) {
                handleQuickOrder(pct, side)
              } else {
                // Contract: first two buttons = BUY, last two = SELL
                const quickSide = pct <= 0.5 ? 'BUY' : 'SELL'
                handleQuickOrder(pct, quickSide)
              }
            }} disabled={submitting} className={cn(
              "py-2 text-[11px] font-bold rounded-lg transition-all duration-200 disabled:opacity-50",
              !isContract
                ? (side === 'BUY'
                  ? "bg-[#0ECB81]/10 hover:bg-[#0ECB81]/20 text-[#0ECB81] border border-[#0ECB81]/20 hover:border-[#0ECB81]/40"
                  : "bg-[#F6465D]/10 hover:bg-[#F6465D]/20 text-[#F6465D] border border-[#F6465D]/20 hover:border-[#F6465D]/40")
                : "bg-quant-bg hover:bg-white/5 text-muted-foreground border border-quant-border hover:border-quant-gold/30"
            )}>
              {pctLabel}
            </button>
          )
        })}
      </div>
    </div>
  )
})
