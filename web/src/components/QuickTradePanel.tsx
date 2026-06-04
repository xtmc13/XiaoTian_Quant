import { useState, useMemo } from 'react'
import { cn, formatCurrency } from '@/lib/utils'
import {
  ArrowUpRight,
  ArrowDownRight,
  Info,
  Shield,
  ChevronDown,
} from 'lucide-react'

interface PositionPreview {
  notional: number
  margin: number
  fee: number
  maxLoss: number
}

interface QuickTradePanelProps {
  symbol: string
  side: 'BUY' | 'SELL'
  orderType: 'LIMIT' | 'MARKET'
  bestBid?: string | number
  bestAsk?: string | number
  lastPrice?: number
  leverage: number
  tradeMode: 'contract' | 'spot'
  tpPrice: string
  slPrice: string
  price: string
  quantity: string
  preview: PositionPreview
  onSideChange: (side: 'BUY' | 'SELL') => void
  onOrderTypeChange: (type: 'LIMIT' | 'MARKET') => void
  onPlaceOrder: (side: 'BUY' | 'SELL') => void
  onLeverageChange: (v: number) => void
  onTradeModeChange: (m: 'contract' | 'spot') => void
  onTpChange: (v: string) => void
  onSlChange: (v: string) => void
  onPriceChange: (v: string) => void
  onQuantityChange: (v: string) => void
}

const LEVERAGE_PRESETS = [1, 5, 10, 20, 50, 75, 100, 125]
const SIZE_PRESETS = [25, 50, 75, 100]

export function QuickTradePanel({
  symbol,
  side,
  orderType,
  bestBid,
  bestAsk,
  lastPrice,
  leverage,
  tradeMode,
  tpPrice,
  slPrice,
  price,
  quantity,
  preview,
  onSideChange,
  onOrderTypeChange,
  onPlaceOrder,
  onLeverageChange,
  onTradeModeChange,
  onTpChange,
  onSlChange,
  onPriceChange,
  onQuantityChange,
}: QuickTradePanelProps) {
  const [showLeverageDropdown, setShowLeverageDropdown] = useState(false)
  const isUp = side === "BUY"
  const activeBg = isUp ? "bg-quant-green" : "bg-quant-red"


  const markPrice = lastPrice || 0
  const qtyNum = parseFloat(quantity) || 0
  const priceNum = orderType === 'MARKET' ? markPrice : (parseFloat(price) || markPrice)

  const notional = qtyNum * priceNum
  const margin = tradeMode === 'contract' && leverage > 0 ? notional / leverage : notional
  const feeRate = 0.0005
  const fee = notional * feeRate

  const maxLoss = useMemo(() => {
    if (!slPrice || !parseFloat(slPrice)) return 0
    const sl = parseFloat(slPrice)
    return Math.abs(qtyNum * (priceNum - sl))
  }, [slPrice, qtyNum, priceNum])

const handlePresetSize = (pct: number) => {
    // Mock: assume balance = 10000 USDT for demo
    const balance = 10000
    const targetNotional = balance * (pct / 100)
    if (orderType === 'MARKET' && markPrice > 0) {
      const q = targetNotional / markPrice
      onQuantityChange(q.toFixed(4))
    } else if (priceNum > 0) {
      const q = targetNotional / priceNum
      onQuantityChange(q.toFixed(4))
    }
  }

  return (
    <div className="w-80 shrink-0 border-l border-quant-border bg-quant-bg-secondary flex flex-col overflow-y-auto">

      {/* Order Type */}
      <div className="px-4 pt-3 pb-3">
        <div className="flex rounded-md overflow-hidden border border-quant-border">
          {(['LIMIT', 'MARKET'] as const).map((t) => (
            <button
              key={t}
              onClick={() => onOrderTypeChange(t)}
              className={cn(
                'flex-1 py-1.5 text-xs font-medium transition-colors',
                orderType === t ? 'bg-quant-gold/10 text-quant-gold' : 'bg-quant-bg text-muted-foreground'
              )}
            >
              {t === 'LIMIT' ? '限价' : '市价'}
            </button>
          ))}
        </div>
      </div>

      {/* Side Toggle */}
      <div className="px-4 pb-3">
        <div className="flex rounded-md overflow-hidden">
          {(['BUY', 'SELL'] as const).map((s) => (
            <button
              key={s}
              onClick={() => onSideChange(s)}
              className={cn(
                'flex-1 py-2.5 text-sm font-bold transition-all flex items-center justify-center gap-1',
                side === s
                  ? s === 'BUY'
                    ? 'bg-quant-green text-white shadow-[0_0_12px_rgba(3,166,109,0.25)]'
                    : 'bg-quant-red text-white shadow-[0_0_12px_rgba(207,48,74,0.25)]'
                  : 'bg-quant-bg text-muted-foreground hover:text-foreground'
              )}
            >
              {s === 'BUY' ? <ArrowUpRight className="w-4 h-4" /> : <ArrowDownRight className="w-4 h-4" />}
              {s === 'BUY' ? '买入/做多' : '卖出/做空'}
            </button>
          ))}
        </div>
      </div>

      {/* Leverage (contract only) */}
      {tradeMode === 'contract' && (
        <div className="px-4 pb-3">
          <div className="flex items-center justify-between mb-1.5">
            <label className="text-[11px] text-muted-foreground flex items-center gap-1">
              <Shield className="w-3 h-3" />
              杠杆
            </label>
            <div className="relative">
              <button
                onClick={() => setShowLeverageDropdown(!showLeverageDropdown)}
                className="text-[11px] font-bold text-quant-gold bg-quant-gold/10 px-2 py-0.5 rounded flex items-center gap-1"
              >
                {leverage}x
                <ChevronDown className="w-3 h-3" />
              </button>
              {showLeverageDropdown && (
                <div className="absolute right-0 top-full mt-1 bg-quant-card border border-quant-border rounded-md shadow-xl z-50 py-1 min-w-[80px]">
                  {LEVERAGE_PRESETS.map((l) => (
                    <button
                      key={l}
                      onClick={() => {
                        onLeverageChange(l)
                        setShowLeverageDropdown(false)
                      }}
                      className={cn(
                        'w-full text-left px-3 py-1 text-xs transition-colors',
                        leverage === l ? 'text-quant-gold bg-quant-gold/10' : 'text-muted-foreground hover:text-foreground hover:bg-white/5'
                      )}
                    >
                      {l}x
                    </button>
                  ))}
                </div>
              )}
            </div>
          </div>
          <input
            type="range"
            min={1}
            max={125}
            step={1}
            value={leverage}
            onChange={(e) => onLeverageChange(Number(e.target.value))}
            className="w-full accent-quant-gold h-1"
          />
          <div className="flex justify-between mt-1">
            {[1, 25, 50, 75, 100, 125].map((l) => (
              <button
                key={l}
                onClick={() => onLeverageChange(l)}
                className={cn(
                  'text-[9px] transition-colors',
                  leverage === l ? 'text-quant-gold font-bold' : 'text-muted-foreground hover:text-foreground'
                )}
              >
                {l}x
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Price Input */}
      {orderType === 'LIMIT' && (
        <div className="px-4 pb-3">
          <label className="text-[11px] text-muted-foreground mb-1 block">价格 (USDT)</label>
          <div className="relative">
            <input
              type="number"
              value={price}
              onChange={(e) => onPriceChange(e.target.value)}
              className="w-full bg-quant-bg border border-quant-border rounded px-3 py-2 text-sm font-mono focus:outline-none focus:border-quant-gold pr-16"
              placeholder="0.00"
            />
            <span className="absolute right-3 top-1/2 -translate-y-1/2 text-[10px] text-muted-foreground">USDT</span>
          </div>
          <div className="flex gap-1 mt-1.5">
            <button
              onClick={() => onPriceChange(String(bestBid || ''))}
              className="flex-1 py-1 bg-quant-green/10 text-quant-green rounded text-[10px] font-medium hover:bg-quant-green/20 transition-colors"
            >
              买一 {bestBid || '--'}
            </button>
            <button
              onClick={() => onPriceChange(String(bestAsk || ''))}
              className="flex-1 py-1 bg-quant-red/10 text-quant-red rounded text-[10px] font-medium hover:bg-quant-red/20 transition-colors"
            >
              卖一 {bestAsk || '--'}
            </button>
          </div>
        </div>
      )}

      {/* Quantity Input */}
      <div className="px-4 pb-3">
        <label className="text-[11px] text-muted-foreground mb-1 block">数量 ({symbol.replace('USDT', '')})</label>
        <div className="relative">
          <input
            type="number"
            value={quantity}
            onChange={(e) => onQuantityChange(e.target.value)}
            className="w-full bg-quant-bg border border-quant-border rounded px-3 py-2 text-sm font-mono focus:outline-none focus:border-quant-gold pr-16"
            placeholder="0"
          />
          <span className="absolute right-3 top-1/2 -translate-y-1/2 text-[10px] text-muted-foreground">{symbol.replace('USDT', '')}</span>
        </div>
        <div className="flex gap-1 mt-1.5">
          {SIZE_PRESETS.map((pct) => (
            <button
              key={pct}
              onClick={() => handlePresetSize(pct)}
              className="flex-1 py-1 bg-quant-bg-tertiary rounded text-[10px] text-muted-foreground hover:text-foreground hover:bg-white/5 transition-colors font-medium"
            >
              {pct}%
            </button>
          ))}
        </div>
      </div>

      {/* TP / SL */}
      <div className="px-4 pb-3">
        <div className="flex items-center gap-2 mb-1.5">
          <label className="text-[11px] text-muted-foreground flex items-center gap-1">
            <Info className="w-3 h-3" />
            止盈 / 止损
          </label>
        </div>
        <div className="flex gap-2">
          <div className="relative flex-1">
            <input
              type="number"
              value={tpPrice}
              onChange={(e) => onTpChange(e.target.value)}
              className="w-full bg-quant-bg border border-quant-border rounded px-3 py-2 text-xs font-mono focus:outline-none focus:border-quant-gold pr-8"
              placeholder="止盈价"
            />
            <span className="absolute right-2 top-1/2 -translate-y-1/2 text-[9px] text-quant-green font-bold">TP</span>
          </div>
          <div className="relative flex-1">
            <input
              type="number"
              value={slPrice}
              onChange={(e) => onSlChange(e.target.value)}
              className="w-full bg-quant-bg border border-quant-border rounded px-3 py-2 text-xs font-mono focus:outline-none focus:border-quant-gold pr-8"
              placeholder="止损价"
            />
            <span className="absolute right-2 top-1/2 -translate-y-1/2 text-[9px] text-quant-red font-bold">SL</span>
          </div>
        </div>
      </div>

      {/* Position Preview */}
      <div className="px-4 pb-3">
        <div className="bg-quant-bg rounded-lg border border-quant-border p-3 space-y-1.5">
          <div className="flex justify-between text-[11px]">
            <span className="text-muted-foreground">名义价值</span>
            <span className="font-mono">{notional > 0 ? `$${formatCurrency(notional)}` : '--'}</span>
          </div>
          <div className="flex justify-between text-[11px]">
            <span className="text-muted-foreground">所需保证金</span>
            <span className="font-mono font-bold text-quant-gold">{margin > 0 ? `$${formatCurrency(margin)}` : '--'}</span>
          </div>
          <div className="flex justify-between text-[11px]">
            <span className="text-muted-foreground">预估手续费</span>
            <span className="font-mono text-muted-foreground">{fee > 0 ? `$${fee.toFixed(4)}` : '--'}</span>
          </div>
          {maxLoss > 0 && (
            <div className="flex justify-between text-[11px]">
              <span className="text-muted-foreground">预估最大亏损</span>
              <span className="font-mono text-quant-red">-${formatCurrency(maxLoss)}</span>
            </div>
          )}
          {tpPrice && parseFloat(tpPrice) > 0 && (
            <div className="flex justify-between text-[11px]">
              <span className="text-muted-foreground">预估止盈</span>
              <span className="font-mono text-quant-green">
                +${formatCurrency(Math.abs(qtyNum * (parseFloat(tpPrice) - priceNum)))}
              </span>
            </div>
          )}
        </div>
      </div>

      {/* ── Position Preview (contract) ── */}
      {tradeMode === 'contract' && (
        <div className="px-4 pb-3 space-y-1.5">
          <div className="bg-quant-bg rounded border border-quant-border px-3 py-2 space-y-1.5">
            <div className="flex justify-between text-[11px]">
              <span className="text-muted-foreground">保证金</span>
              <span className="font-mono font-bold text-quant-gold">{margin > 0 ? '$' + formatCurrency(margin) : '--'}</span>
            </div>
            <div className="flex justify-between text-[11px]">
              <span className="text-muted-foreground">可开</span>
              <span className="font-mono text-foreground">≈ {margin > 0 ? (margin / priceNum).toFixed(3) : '0'} {symbol.replace('USDT','')}</span>
            </div>
            <div className="flex justify-between text-[11px]">
              <span className="text-muted-foreground">可用保证金</span>
              <span className="font-mono text-foreground">12,450.50 USDT</span>
            </div>
            <div className="flex justify-between text-[11px]">
              <span className="text-muted-foreground">预估手续费</span>
              <span className="font-mono text-muted-foreground">{fee > 0 ? '$' + fee.toFixed(4) : '--'}</span>
            </div>
          </div>
        </div>
      )}

      {/* ── Total (spot) ── */}
      {tradeMode !== 'contract' && (
        <div className="px-4 pb-3">
          <div className="bg-quant-bg rounded border border-quant-border px-3 py-2 flex justify-between text-xs">
            <span className="text-muted-foreground">成交金额</span>
            <span className="font-mono font-medium text-foreground">≈ {qtyNum * priceNum > 0 ? (qtyNum * priceNum).toFixed(2) : '0.00'} USDT</span>
          </div>
        </div>
      )}

      {/* ── Balance (spot) ── */}
      {tradeMode !== 'contract' && (
        <div className="px-4 pb-2 flex justify-between text-[11px]">
          <span className="text-muted-foreground">可用 <span className="text-foreground font-mono">12,450.50</span> USDT</span>
          <span className="text-muted-foreground">可用 <span className="text-foreground font-mono">0.8450</span> BTC</span>
        </div>
      )}

      {/* ── Submit Buttons ── */}
      <div className="px-4 pb-4 mt-auto">
        {tradeMode === 'contract' ? (
          /* Contract: one main button + opposite secondary */
          <>
          <button onClick={() => onPlaceOrder(side)} className={cn(
            'w-full py-3 rounded-md font-bold text-sm transition-all flex items-center justify-center gap-1.5',
            activeBg, 'hover:opacity-90 active:scale-[0.98]'
          )}>
            {side === 'BUY' ? '买入/做多' : '卖出/做空'}
          </button>
          <button onClick={() => { const opposite = side === 'BUY' ? 'SELL' : 'BUY'; onSideChange(opposite); onPlaceOrder(opposite); }}
            className="w-full mt-2 py-2 rounded-md text-xs font-medium border text-muted-foreground hover:text-foreground transition-colors"
            style={{borderColor: side === 'BUY' ? 'var(--sell)' : 'var(--buy)', color: side === 'BUY' ? 'var(--sell)' : 'var(--buy)'}}
          >
            {side === 'BUY' ? '卖出/做空' : '买入/做多'}
          </button>
          </>
        ) : (
          /* Spot: dual buy/sell buttons */
          <div className="flex gap-2">
            <button onClick={() => { onSideChange('BUY'); onPlaceOrder('BUY'); }}
              className="flex-1 py-3 rounded-md font-bold text-sm bg-[#2EBD85] text-white hover:opacity-90 active:scale-[0.98] transition-all">
              买入
            </button>
            <button onClick={() => { onSideChange('SELL'); onPlaceOrder('SELL'); }}
              className="flex-1 py-3 rounded-md font-bold text-sm bg-[#F6465D] text-white hover:opacity-90 active:scale-[0.98] transition-all">
              卖出
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
