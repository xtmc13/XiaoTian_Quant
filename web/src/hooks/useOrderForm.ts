import { useState, useCallback, useMemo } from 'react'
import { orderApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import type { TradingPrecision } from '@/lib/tradingPrecision'

export type OrderType = 'LIMIT' | 'MARKET' | 'STOP_LIMIT'
export type AmountMode = 'quantity' | 'amount'
export type TimeInForce = 'GTC' | 'IOC' | 'FOK'
export type Side = 'BUY' | 'SELL'

export interface OrderFormState {
  side: Side
  setSide: (s: Side) => void
  orderType: OrderType
  setOrderType: (t: OrderType) => void
  price: string
  setPrice: (p: string) => void
  quantity: string
  setQuantity: (q: string) => void
  amountMode: AmountMode
  setAmountMode: (m: AmountMode) => void
  amountValue: string
  setAmountValue: (v: string) => void
  sliderValue: number
  setSliderValue: (v: number) => void
  timeInForce: TimeInForce
  setTimeInForce: (t: TimeInForce) => void
  postOnly: boolean
  setPostOnly: (v: boolean) => void
  slippage: string
  setSlippage: (s: string) => void
  showAdvanced: boolean
  setShowAdvanced: (v: boolean) => void
  showTpSl: boolean
  setShowTpSl: (v: boolean) => void
  tpPrice: string
  setTpPrice: (p: string) => void
  slPrice: string
  setSlPrice: (p: string) => void
  submitting: boolean
  resetForm: () => void
  handlePriceShortcut: (factor: number, opts: PriceShortcutOptions) => void
  handleSliderChange: (val: number, opts: SliderOptions) => void
  handlePctButton: (pct: number, opts: SliderOptions) => void
  preview: OrderPreview
  placeOrder: (opts: PlaceOrderOptions) => Promise<void>
}

export interface PriceShortcutOptions {
  lastPrice: number
  precision: TradingPrecision
}

export interface SliderOptions {
  lastPrice: number
  balance: number
  leverage?: number
  precision: TradingPrecision
  symbol: string
  side: Side
  orderType: OrderType
  price: string
  amountMode: AmountMode
  holdings?: Array<Record<string, unknown>>
}

export interface PlaceOrderOptions {
  symbol: string
  side: Side
  marketType: 'spot' | 'swap'
  lastPrice: number
  precision: TradingPrecision
  leverage?: number
  marginMode?: 'cross' | 'isolated'
  positionSide?: 'LONG' | 'SHORT'
  closePosition?: boolean
  onSuccess?: () => void
}

export interface OrderPreview {
  notional: number
  fee: number
  margin: number
  maxLoss: number
  qty: number
}

const FEE_RATE = 0.0005

function calcQtyFromAmount(amount: number, calcPrice: number, leverage: number, isContract: boolean): number {
  if (!amount || amount <= 0 || !calcPrice || calcPrice <= 0) return 0
  return isContract ? (amount * leverage) / calcPrice : amount / calcPrice
}

function getBaseAsset(symbol: string): string {
  return symbol.replace(/USDT|USD|BUSD/g, '')
}

function findHoldingFree(holdings: Array<Record<string, unknown>> | undefined, baseAsset: string): number {
  if (!holdings || holdings.length === 0) return 0
  const assetHolding = holdings.find((b) => {
    const asset = String(b.asset || b.currency || '')
    return asset === baseAsset
  })
  if (!assetHolding) return 0
  return parseFloat(String(assetHolding.free ?? assetHolding.available ?? 0)) || 0
}

function calcQtyFromSlider(
  pct: number,
  balance: number,
  calcPrice: number,
  leverage: number,
  isContract: boolean,
  side: Side,
  symbol: string,
  holdings?: Array<Record<string, unknown>>
): number {
  if (pct <= 0 || !calcPrice || calcPrice <= 0) return 0
  if (isContract) {
    return (balance * pct * leverage) / calcPrice
  }
  if (side === 'BUY') {
    return (balance * pct) / calcPrice
  }
  const baseAsset = getBaseAsset(symbol)
  const assetFree = findHoldingFree(holdings, baseAsset)
  return assetFree * pct
}

export function useOrderForm(): OrderFormState {
  const [side, setSide] = useState<Side>('BUY')
  const [orderType, setOrderType] = useState<OrderType>('LIMIT')
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

  const resetForm = useCallback(() => {
    setQuantity('')
    setPrice('')
    setTpPrice('')
    setSlPrice('')
    setAmountValue('')
    setSliderValue(0)
  }, [])

  const handlePriceShortcut = useCallback((factor: number, opts: PriceShortcutOptions) => {
    const { lastPrice, precision } = opts
    if (!lastPrice || lastPrice <= 0) return
    setPrice((lastPrice * factor).toFixed(precision.price))
  }, [])

  const handleSliderChange = useCallback((val: number, opts: SliderOptions) => {
    setSliderValue(val)
    const calcPrice = opts.orderType === 'MARKET' ? opts.lastPrice : parseFloat(opts.price) || opts.lastPrice
    const pct = val / 100
    if (opts.amountMode === 'amount') {
      const amount = opts.balance * pct
      setAmountValue(amount > 0 ? amount.toFixed(2) : '')
    } else {
      const isContract = opts.leverage !== undefined && opts.leverage > 1
      const qty = calcQtyFromSlider(
        pct,
        opts.balance,
        calcPrice,
        opts.leverage || 1,
        isContract,
        opts.side,
        opts.symbol,
        opts.holdings
      )
      setQuantity(qty > 0 ? qty.toFixed(opts.precision.quantity) : '')
    }
  }, [])

  const handlePctButton = useCallback((pct: number, opts: SliderOptions) => {
    setSliderValue(Math.round(pct * 100))
    const calcPrice = opts.orderType === 'MARKET' ? opts.lastPrice : parseFloat(opts.price) || opts.lastPrice
    if (opts.amountMode === 'amount') {
      const amount = opts.balance * pct
      setAmountValue(amount > 0 ? amount.toFixed(2) : '')
    } else {
      const isContract = opts.leverage !== undefined && opts.leverage > 1
      const qty = calcQtyFromSlider(
        pct,
        opts.balance,
        calcPrice,
        opts.leverage || 1,
        isContract,
        opts.side,
        opts.symbol,
        opts.holdings
      )
      setQuantity(qty > 0 ? qty.toFixed(opts.precision.quantity) : '')
    }
  }, [])

  const preview = useMemo(() => {
    let qty: number
    if (amountMode === 'amount') {
      const amount = parseFloat(amountValue) || 0
      qty = amount > 0 ? amount : 0
    } else {
      qty = parseFloat(quantity) || 0
    }
    // preview cannot compute price-dependent values without lastPrice; return qty only
    return { notional: 0, fee: 0, margin: 0, maxLoss: 0, qty }
  }, [quantity, amountMode, amountValue])

  const placeOrder = useCallback(
    async (opts: PlaceOrderOptions) => {
      let qty: number
      if (amountMode === 'amount') {
        const amount = parseFloat(amountValue)
        if (!amount || amount <= 0) {
          toast('error', '请输入有效金额')
          return
        }
        const calcPrice = orderType === 'MARKET' ? opts.lastPrice : parseFloat(price) || opts.lastPrice
        if (!calcPrice || calcPrice <= 0) {
          toast('error', '无法获取有效价格')
          return
        }
        const isContract = opts.marketType === 'swap'
        qty = calcQtyFromAmount(amount, calcPrice, opts.leverage || 1, isContract)
      } else {
        qty = parseFloat(quantity)
        if (!qty || qty <= 0) {
          toast('error', '请输入有效数量')
          return
        }
      }
      if (orderType === 'LIMIT' || orderType === 'STOP_LIMIT') {
        const p = parseFloat(price)
        if (!p || p <= 0) {
          toast('error', '请输入有效价格')
          return
        }
      }
      setSubmitting(true)
      try {
        const req: Record<string, unknown> = {
          symbol: opts.symbol,
          side: opts.side,
          order_type: orderType,
          price: orderType === 'MARKET' ? 0 : parseFloat(price) || 0,
          quantity: qty,
          market_type: opts.marketType,
          time_in_force: timeInForce,
          post_only: postOnly,
          slippage: orderType === 'MARKET' ? parseFloat(slippage) / 100 : undefined,
        }
        if (opts.leverage) req.leverage = opts.leverage
        if (opts.marginMode) req.margin_mode = opts.marginMode
        if (opts.positionSide) req.position_side = opts.positionSide
        if (opts.closePosition) req.close_position = true
        if (orderType === 'STOP_LIMIT') {
          req.stop_price = parseFloat(tpPrice) || 0
        }
        if (showTpSl) {
          req.tp_price = tpPrice ? parseFloat(tpPrice) : undefined
          req.sl_price = slPrice ? parseFloat(slPrice) : undefined
        }
        await orderApi.place(req)
        toast('success', '订单已提交')
        resetForm()
        opts.onSuccess?.()
      } catch (e: unknown) {
        const err = e instanceof Error ? e : new Error(String(e))
        toast('error', err.message || '下单失败')
      } finally {
        setSubmitting(false)
      }
    },
    [
      amountMode,
      amountValue,
      quantity,
      price,
      orderType,
      timeInForce,
      postOnly,
      slippage,
      tpPrice,
      slPrice,
      showTpSl,
      resetForm,
    ]
  )

  return {
    side,
    setSide,
    orderType,
    setOrderType,
    price,
    setPrice,
    quantity,
    setQuantity,
    amountMode,
    setAmountMode,
    amountValue,
    setAmountValue,
    sliderValue,
    setSliderValue,
    timeInForce,
    setTimeInForce,
    postOnly,
    setPostOnly,
    slippage,
    setSlippage,
    showAdvanced,
    setShowAdvanced,
    showTpSl,
    setShowTpSl,
    tpPrice,
    setTpPrice,
    slPrice,
    setSlPrice,
    submitting,
    resetForm,
    handlePriceShortcut,
    handleSliderChange,
    handlePctButton,
    preview,
    placeOrder,
  }
}
