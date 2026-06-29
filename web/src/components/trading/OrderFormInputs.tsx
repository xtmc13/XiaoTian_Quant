import React from 'react'
import { cn } from '@/lib/utils'
import { Search } from 'lucide-react'

export interface OrderFormHeaderProps {
  orderType: 'LIMIT' | 'MARKET' | 'STOP_LIMIT'
  onOrderTypeChange: (t: 'LIMIT' | 'MARKET' | 'STOP_LIMIT') => void
  showTpSl: boolean
  onToggleTpSl: () => void
  showAdvanced: boolean
  onToggleAdvanced: () => void
  contract?: boolean
}

export function OrderFormHeader({
  orderType,
  onOrderTypeChange,
  showTpSl,
  onToggleTpSl,
  showAdvanced,
  onToggleAdvanced,
  contract = false,
}: OrderFormHeaderProps) {
  const types: Array<{ key: 'LIMIT' | 'MARKET' | 'STOP_LIMIT'; label: string }> = contract
    ? [
        { key: 'LIMIT', label: '限价' },
        { key: 'MARKET', label: '市价' },
        { key: 'STOP_LIMIT', label: '条件' },
      ]
    : [
        { key: 'LIMIT', label: '限价' },
        { key: 'MARKET', label: '市价' },
      ]

  return (
    <div className="flex gap-1 bg-quant-bg p-0.5 rounded">
      {types.map((t) => (
        <button
          key={t.key}
          onClick={() => onOrderTypeChange(t.key)}
          className={cn(
            'flex-1 py-1.5 text-[11px] font-medium rounded transition-colors',
            orderType === t.key
              ? 'bg-quant-bg-secondary text-foreground'
              : 'text-muted-foreground hover:text-foreground'
          )}
        >
          {t.label}
        </button>
      ))}
      <button
        onClick={onToggleTpSl}
        className={cn(
          'flex-1 py-1.5 text-[11px] rounded transition-colors',
          showTpSl ? 'bg-quant-bg-secondary text-foreground' : 'text-muted-foreground hover:text-foreground'
        )}
      >
        止盈止损
      </button>
      <button
        onClick={onToggleAdvanced}
        className={cn(
          'flex-1 py-1.5 text-[11px] rounded transition-colors',
          showAdvanced ? 'bg-quant-bg-secondary text-foreground' : 'text-muted-foreground hover:text-foreground'
        )}
      >
        高级
      </button>
    </div>
  )
}

export interface PriceInputProps {
  label: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
  unit?: string
  ariaLabel: string
}

export function PriceInput({ label, value, onChange, placeholder, unit, ariaLabel }: PriceInputProps) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex justify-between text-[10px] text-muted-foreground">
        <span>{label}</span>
        {unit && <span>{unit}</span>}
      </div>
      <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-10 focus-within:border-quant-gold transition-all">
        <input
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          aria-label={ariaLabel}
          className="flex-1 bg-transparent text-sm font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"
        />
        {unit && <span className="text-[10px] text-muted-foreground ml-2">{unit}</span>}
      </div>
    </div>
  )
}

export interface PriceShortcutsProps {
  lastPrice: number
  precision: number
  onPriceChange: (p: string) => void
  contract?: boolean
}

export function PriceShortcuts({ lastPrice, precision, onPriceChange, contract = false }: PriceShortcutsProps) {
  const buttons = contract
    ? [
        { label: '-0.1%', factor: 0.999 },
        { label: '-0.5%', factor: 0.995 },
        { label: '最新价', factor: 1, highlight: true },
        { label: '+0.5%', factor: 1.005 },
        { label: '+0.1%', factor: 1.001 },
      ]
    : [
        { label: '-1%', factor: 0.99 },
        { label: '-0.5%', factor: 0.995 },
        { label: '最新价', factor: 1, highlight: true },
        { label: '+0.5%', factor: 1.005 },
        { label: '+1%', factor: 1.01 },
      ]

  return (
    <div className="flex gap-1">
      {buttons.map((b) => (
        <button
          key={b.label}
          onClick={() => {
            if (lastPrice) onPriceChange((lastPrice * b.factor).toFixed(precision))
          }}
          className={cn(
            'flex-1 py-1 text-[10px] rounded transition-colors',
            b.highlight
              ? 'text-quant-gold hover:text-quant-gold/80 bg-quant-bg border border-quant-gold/30 hover:bg-quant-gold/10'
              : 'text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border hover:border-quant-gold/50'
          )}
        >
          {b.label}
        </button>
      ))}
    </div>
  )
}

export interface SideToggleProps {
  side: 'BUY' | 'SELL'
  onChange: (s: 'BUY' | 'SELL') => void
  buyLabel?: string
  sellLabel?: string
}

export function SideToggle({ side, onChange, buyLabel = '买入', sellLabel = '卖出' }: SideToggleProps) {
  return (
    <div className="flex gap-1.5">
      <button
        onClick={() => onChange('BUY')}
        className={cn(
          'flex-1 py-2.5 text-sm font-bold rounded-lg transition-all duration-200',
          side === 'BUY'
            ? 'bg-[#0ECB81] hover:bg-[#0ECB81]/90 text-black shadow-lg shadow-[#0ECB81]/20'
            : 'bg-quant-bg hover:bg-[#0ECB81]/10 text-muted-foreground border border-quant-border hover:border-[#0ECB81]/50'
        )}
      >
        {buyLabel}
      </button>
      <button
        onClick={() => onChange('SELL')}
        className={cn(
          'flex-1 py-2.5 text-sm font-bold rounded-lg transition-all duration-200',
          side === 'SELL'
            ? 'bg-[#F6465D] hover:bg-[#F6465D]/90 text-white shadow-lg shadow-[#F6465D]/20'
            : 'bg-quant-bg hover:bg-[#F6465D]/10 text-muted-foreground border border-quant-border hover:border-[#F6465D]/50'
        )}
      >
        {sellLabel}
      </button>
    </div>
  )
}

export interface AmountModeToggleProps {
  mode: 'quantity' | 'amount'
  onChange: (m: 'quantity' | 'amount') => void
  quantityLabel: string
  amountLabel: string
}

export function AmountModeToggle({ mode, onChange, quantityLabel, amountLabel }: AmountModeToggleProps) {
  return (
    <div className="flex gap-1">
      <button
        onClick={() => onChange('quantity')}
        className={cn(
          'px-2 py-0.5 rounded text-[10px] transition-colors',
          mode === 'quantity' ? 'bg-quant-gold/20 text-quant-gold' : 'hover:bg-white/5'
        )}
      >
        {quantityLabel}
      </button>
      <button
        onClick={() => onChange('amount')}
        className={cn(
          'px-2 py-0.5 rounded text-[10px] transition-colors',
          mode === 'amount' ? 'bg-quant-gold/20 text-quant-gold' : 'hover:bg-white/5'
        )}
      >
        {amountLabel}
      </button>
    </div>
  )
}

export interface SliderInputProps {
  value: number
  onChange: (v: number) => void
}

export function SliderInput({ value, onChange }: SliderInputProps) {
  return (
    <div className="flex flex-col gap-1">
      <input
        type="range"
        min="0"
        max="100"
        step="1"
        value={value}
        onChange={(e) => onChange(parseInt(e.target.value))}
        className="w-full h-1 bg-quant-border rounded-lg appearance-none cursor-pointer
          [&::-webkit-slider-thumb]:appearance-none
          [&::-webkit-slider-thumb]:w-3 [&::-webkit-slider-thumb]:h-3
          [&::-webkit-slider-thumb]:bg-quant-gold [&::-webkit-slider-thumb]:rounded-full
          [&::-webkit-slider-thumb]:cursor-pointer"
      />
      <div className="flex justify-between text-[9px] text-muted-foreground">
        <span>0%</span>
        <span>{value}%</span>
        <span>100%</span>
      </div>
    </div>
  )
}

export interface PctButtonsProps {
  activePct: number
  onClick: (pct: number) => void
}

export function PctButtons({ activePct, onClick }: PctButtonsProps) {
  return (
    <div className="flex gap-1">
      {[0.25, 0.5, 0.75, 1].map((pct) => {
        const pctLabel = Math.round(pct * 100) + '%'
        return (
          <button
            key={pctLabel}
            onClick={() => onClick(pct)}
            className={cn(
              'flex-1 py-1.5 text-[10px] font-medium rounded-lg transition-all',
              activePct === Math.round(pct * 100)
                ? 'bg-quant-gold/20 text-quant-gold border border-quant-gold/50'
                : 'text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border hover:border-quant-gold/50'
            )}
          >
            {pctLabel}
          </button>
        )
      })}
    </div>
  )
}

export interface TpSlInputsProps {
  tpPrice: string
  onTpChange: (v: string) => void
  slPrice: string
  onSlChange: (v: string) => void
  onSmartSet?: () => void
}

export function TpSlInputs({ tpPrice, onTpChange, slPrice, onSlChange, onSmartSet }: TpSlInputsProps) {
  return (
    <div className="flex flex-col gap-2 p-2 bg-quant-bg/50 rounded-lg border border-quant-border/50">
      <div className="flex items-center justify-between text-[10px] text-muted-foreground mb-1">
        <span>止盈止损</span>
        {onSmartSet && (
          <button onClick={onSmartSet} className="text-quant-gold hover:text-quant-gold/80 transition-colors">
            智能设置
          </button>
        )}
      </div>
      <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-9 focus-within:border-quant-gold transition-all">
        <span className="text-[10px] text-[#0ECB81] w-6">止盈</span>
        <input
          value={tpPrice}
          onChange={(e) => onTpChange(e.target.value)}
          placeholder="--"
          aria-label="止盈价格"
          className="flex-1 bg-transparent text-xs font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"
        />
        <span className="text-[10px] text-muted-foreground ml-1">USDT</span>
      </div>
      <div className="flex items-center bg-quant-bg border border-quant-border rounded-lg px-3 h-9 focus-within:border-quant-gold transition-all">
        <span className="text-[10px] text-[#F6465D] w-6">止损</span>
        <input
          value={slPrice}
          onChange={(e) => onSlChange(e.target.value)}
          placeholder="--"
          aria-label="止损价格"
          className="flex-1 bg-transparent text-xs font-mono border-0 ring-0 focus:ring-0 focus:ring-offset-0 focus:outline-0 focus-visible:outline-0 text-foreground placeholder:text-muted-foreground"
        />
        <span className="text-[10px] text-muted-foreground ml-1">USDT</span>
      </div>
    </div>
  )
}

export interface AdvancedSettingsProps {
  orderType: 'LIMIT' | 'MARKET' | 'STOP_LIMIT'
  timeInForce: 'GTC' | 'IOC' | 'FOK'
  onTimeInForceChange: (t: 'GTC' | 'IOC' | 'FOK') => void
  postOnly: boolean
  onPostOnlyChange: (v: boolean) => void
  slippage: string
  onSlippageChange: (v: string) => void
}

export function AdvancedSettings({
  orderType,
  timeInForce,
  onTimeInForceChange,
  postOnly,
  onPostOnlyChange,
  slippage,
  onSlippageChange,
}: AdvancedSettingsProps) {
  return (
    <div className="flex flex-col gap-2 p-2 bg-quant-bg/50 rounded-lg border border-quant-border/50">
      {orderType === 'LIMIT' && (
        <div className="flex flex-col gap-1.5">
          <span className="text-[10px] text-muted-foreground">订单有效期</span>
          <div className="flex gap-1">
            {(['GTC', 'IOC', 'FOK'] as const).map((t) => (
              <button
                key={t}
                onClick={() => onTimeInForceChange(t)}
                className={cn(
                  'flex-1 py-1 text-[10px] rounded transition-colors',
                  timeInForce === t
                    ? 'bg-quant-gold/20 text-quant-gold border border-quant-gold/50'
                    : 'text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border'
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

      {orderType === 'LIMIT' && (
        <label className="flex items-center gap-2 cursor-pointer">
          <input
            type="checkbox"
            checked={postOnly}
            onChange={(e) => onPostOnlyChange(e.target.checked)}
            className="w-3 h-3 accent-quant-gold"
          />
          <span className="text-[10px] text-muted-foreground">只做 Maker（Post-Only）</span>
          <span className="text-[9px] text-muted-foreground/60">确保订单只作为挂单成交</span>
        </label>
      )}

      {orderType === 'MARKET' && (
        <div className="flex flex-col gap-1.5">
          <span className="text-[10px] text-muted-foreground">滑点容忍度</span>
          <div className="flex items-center gap-2">
            <div className="flex gap-1">
              {['0.1', '0.5', '1', '2'].map((s) => (
                <button
                  key={s}
                  onClick={() => onSlippageChange(s)}
                  className={cn(
                    'px-2 py-1 text-[10px] rounded transition-colors',
                    slippage === s
                      ? 'bg-quant-gold/20 text-quant-gold border border-quant-gold/50'
                      : 'text-muted-foreground hover:text-foreground bg-quant-bg border border-quant-border'
                  )}
                >
                  {s}%
                </button>
              ))}
            </div>
            <input
              value={slippage}
              onChange={(e) => onSlippageChange(e.target.value)}
              className="w-16 px-2 py-1 text-[10px] bg-quant-bg border border-quant-border rounded text-foreground"
              placeholder="0.5"
            />
            <span className="text-[10px] text-muted-foreground">%</span>
          </div>
        </div>
      )}
    </div>
  )
}

export interface AccountInfoProps {
  rows: Array<{ label: string; value: React.ReactNode }>
}

export function AccountInfo({ rows }: AccountInfoProps) {
  return (
    <div className="space-y-1.5 text-[10px]">
      {rows.map((r, i) => (
        <div key={i} className="flex justify-between text-muted-foreground">
          <span>{r.label}</span>
          <span className="font-mono text-foreground">{r.value}</span>
        </div>
      ))}
    </div>
  )
}
