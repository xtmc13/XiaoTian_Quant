import { useState, useCallback } from 'react'
import { cn } from '@/lib/utils'
import { TRADING_INTERVALS } from '@/lib/constants'

/**
 * Extended CRA params — superset used by StrategyCreateModal.
 * Includes all fields from the modal's defaultCraParams.
 */
export interface CRAParams {
  // Core CRA (matches CRAParamForm)
  orderCount: number
  firstOrderAmount: number
  addPosSpread: number
  addPosCallback: number
  tpRatio: number
  profitCallback: number
  tpMethod: 'full' | 'tail' | 'head_tail' | 'moving'
  openInd: string
  addInd: string
  waterfall: number
  openDouble: boolean
  trendInd: boolean
  trendTf: string
  followTrend: boolean
  followTrendMax?: number
  burnCut: boolean
  closeAddPos: boolean
  leverage: number
  direction: 'long' | 'short' | 'dual'
  tradeCountMode?: 'single' | 'cycle'
  // Extended fields (StrategyCreateModal)
  addPosMultiple?: number
  movingTP?: { enabled: boolean; tier1_ratio: number; tier1_drawback: number; tier2_drawback: number }
  reverseTP?: boolean
  reverseSL?: boolean
  amplitude?: Record<string, number>
  burnCutExtra?: { enabled: boolean; dual_burn_start: number; global_burn_start: number }
  customReduce?: boolean
  onlineOrderLimit?: number
  profitProtection?: boolean
  stopLossRatio?: number
  stopLossAmount?: number
  stopLossPrice?: number
  firstOrderPrice?: number
}

export const DEFAULT_CRA_PARAMS: CRAParams = {
  orderCount: 7,
  firstOrderAmount: 100,
  addPosSpread: 3,
  addPosCallback: 0.1,
  tpRatio: 1.3,
  profitCallback: 0.1,
  tpMethod: 'full',
  openInd: 'macd_golden',
  addInd: 'macd',
  waterfall: 2,
  openDouble: false,
  trendInd: false,
  trendTf: '15m',
  followTrend: false,
  burnCut: false,
  closeAddPos: false,
  leverage: 5,
  direction: 'long',
  tradeCountMode: 'single',
  addPosMultiple: 1,
  movingTP: { enabled: false, tier1_ratio: 1.5, tier1_drawback: 30, tier2_drawback: 20 },
  reverseTP: false,
  reverseSL: false,
  amplitude: { '5m': 2, '15m': 4, '30m': 7, '1h': 10 },
  burnCutExtra: { enabled: false, dual_burn_start: 3, global_burn_start: 5 },
  customReduce: false,
  onlineOrderLimit: 10,
  profitProtection: false,
  stopLossRatio: 0,
  stopLossAmount: 0,
  stopLossPrice: 0,
  firstOrderPrice: 0,
}

/** Default for Settings page (simplified CRA) */
export const DEFAULT_CRA_SETTINGS: CRAParams = { ...DEFAULT_CRA_PARAMS }

/* ─── useCRAConfig hook ─────────────────────────────────────────────── */
export function useCRAConfig() {
  const [config, setConfig] = useState<CRAParams>(() => {
    try {
      const raw = localStorage.getItem('xt-cra-config')
      return raw ? { ...DEFAULT_CRA_PARAMS, ...JSON.parse(raw) } : DEFAULT_CRA_PARAMS
    } catch {
      return DEFAULT_CRA_PARAMS
    }
  })
  const update = useCallback((key: keyof CRAParams, val: CRAParams[keyof CRAParams]) => {
    setConfig((prev) => {
      const next = { ...prev, [key]: val }
      localStorage.setItem('xt-cra-config', JSON.stringify(next))
      return next
    })
  }, [])
  const setAll = useCallback((next: CRAParams) => {
    localStorage.setItem('xt-cra-config', JSON.stringify(next))
    setConfig(next)
  }, [])
  return { config, update, setAll }
}

/* ─── CRA↔ StrategyCreateModal conversion ────────────────────────────── */
/** StrategyCreateModal uses snake_case field names and nested burnCut object */
export interface ModalCraParams {
  orderCount: number; firstOrderAmount: number; addPosSpread: number; addPosCallback: number
  takeProfitRatio: number; profitCallback: number; tradeCountMode: 'single' | 'cycle'
  openIndicator: string; addPosIndicator: string; addPosMultiple: number; waterfallProtection: number
  openDouble: boolean; trendIndicator: boolean; trendTimeframe: string
  takeProfitMethod: 'full' | 'tail' | 'head_tail' | 'moving'
  movingTP: { enabled: boolean; tier1_ratio: number; tier1_drawback: number; tier2_drawback: number }
  reverseTP: boolean; reverseSL: boolean; amplitude: Record<string, number>
  burnCut: { enabled: boolean; dual_burn_start: number; global_burn_start: number }
  customReduce: boolean; onlineOrderLimit: number; profitProtection: boolean
  followTrend: boolean; followTrendMax: number
  stopLossRatio: number; stopLossAmount: number; stopLossPrice: number; firstOrderPrice: number
  closeAddPosition: boolean
  leverage?: number
  direction?: 'long' | 'short' | 'dual'
}

/** Convert from CRAParams (camelCase) to ModalCraParams (snake_case) */
export function craToModal(p: CRAParams): ModalCraParams {
  return {
    orderCount: p.orderCount, firstOrderAmount: p.firstOrderAmount,
    addPosSpread: p.addPosSpread, addPosCallback: p.addPosCallback,
    takeProfitRatio: p.tpRatio, profitCallback: p.profitCallback,
    tradeCountMode: p.tradeCountMode ?? 'cycle',
    openIndicator: p.openInd, addPosIndicator: p.addInd,
    addPosMultiple: p.addPosMultiple ?? 1, waterfallProtection: p.waterfall,
    openDouble: p.openDouble, trendIndicator: p.trendInd, trendTimeframe: p.trendTf,
    takeProfitMethod: p.tpMethod,
    movingTP: p.movingTP ?? { enabled: false, tier1_ratio: 1.5, tier1_drawback: 30, tier2_drawback: 20 },
    reverseTP: p.reverseTP ?? false, reverseSL: p.reverseSL ?? false,
    amplitude: p.amplitude ?? { '5m': 2, '15m': 4, '30m': 7, '1h': 10 },
    burnCut: p.burnCutExtra ?? { enabled: false, dual_burn_start: 3, global_burn_start: 5 },
    customReduce: p.customReduce ?? false, onlineOrderLimit: p.onlineOrderLimit ?? 10,
    profitProtection: p.profitProtection ?? false,
    followTrend: p.followTrend, followTrendMax: 5,
    stopLossRatio: p.stopLossRatio ?? 0, stopLossAmount: p.stopLossAmount ?? 0,
    stopLossPrice: p.stopLossPrice ?? 0, firstOrderPrice: p.firstOrderPrice ?? 0,
    closeAddPosition: p.closeAddPos,
  }
}

/** Convert from ModalCraParams (snake_case) to CRAParams (camelCase) */
export function modalToCra(m: ModalCraParams): CRAParams {
  return {
    orderCount: m.orderCount, firstOrderAmount: m.firstOrderAmount,
    addPosSpread: m.addPosSpread, addPosCallback: m.addPosCallback,
    tpRatio: m.takeProfitRatio, profitCallback: m.profitCallback,
    tradeCountMode: m.tradeCountMode,
    openInd: m.openIndicator, addInd: m.addPosIndicator,
    addPosMultiple: m.addPosMultiple, waterfall: m.waterfallProtection,
    openDouble: m.openDouble, trendInd: m.trendIndicator, trendTf: m.trendTimeframe,
    tpMethod: m.takeProfitMethod,
    movingTP: m.movingTP,
    reverseTP: m.reverseTP, reverseSL: m.reverseSL,
    amplitude: m.amplitude,
    burnCut: m.burnCut.enabled,
    burnCutExtra: m.burnCut,
    customReduce: m.customReduce, onlineOrderLimit: m.onlineOrderLimit,
    profitProtection: m.profitProtection,
    followTrend: m.followTrend,
    stopLossRatio: m.stopLossRatio, stopLossAmount: m.stopLossAmount,
    stopLossPrice: m.stopLossPrice, firstOrderPrice: m.firstOrderPrice,
    closeAddPos: m.closeAddPosition,
    leverage: m.leverage ?? 5,
    direction: m.direction ?? 'long',
  }
}

const inputCls =
  'w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold'

interface CRAParamFormProps {
  value: CRAParams
  onChange: (next: CRAParams) => void
  showTradeCountMode?: boolean
  /** Show extended fields for StrategyCreateModal (reverseTP/SL, stop loss, etc.) */
  showExtended?: boolean
  className?: string
}

export function CRAParamForm({ value, onChange, showTradeCountMode = false, showExtended = false, className }: CRAParamFormProps) {
  const update = <K extends keyof CRAParams>(key: K, val: CRAParams[K]) => {
    onChange({ ...value, [key]: val })
  }

  return (
    <div className={cn('rounded-xl border border-quant-border bg-quant-bg-tertiary p-4 space-y-4', className)}>
      <div className="text-xs font-semibold text-quant-gold">CRA 量化参数</div>

      {/* 基础参数 */}
      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className="text-[11px] text-muted-foreground mb-1.5 block">做单数量</label>
          <input
            type="number" min={1} max={20}
            value={value.orderCount}
            onChange={(e) => update('orderCount', Number(e.target.value))}
            className={inputCls}
          />
        </div>
        <div>
          <label className="text-[11px] text-muted-foreground mb-1.5 block">首单仓位 (USDT)</label>
          <input
            type="number" min={10} max={10000} step={10}
            value={value.firstOrderAmount}
            onChange={(e) => update('firstOrderAmount', Number(e.target.value))}
            className={inputCls}
          />
        </div>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className="text-[11px] text-muted-foreground mb-1.5 block">补仓价差 (%)</label>
          <input
            type="number" min={0.5} max={50} step={0.5}
            value={value.addPosSpread}
            onChange={(e) => update('addPosSpread', Number(e.target.value))}
            className={inputCls}
          />
        </div>
        <div>
          <label className="text-[11px] text-muted-foreground mb-1.5 block">补仓回调 (%)</label>
          <input
            type="number" min={0.01} max={0.5} step={0.01}
            value={value.addPosCallback}
            onChange={(e) => update('addPosCallback', Number(e.target.value))}
            className={inputCls}
          />
        </div>
      </div>

      {/* 止盈设置 */}
      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className="text-[11px] text-muted-foreground mb-1.5 block">止盈比例 (%)</label>
          <input
            type="number" min={0.1} max={50} step={0.1}
            value={value.tpRatio}
            onChange={(e) => update('tpRatio', Number(e.target.value))}
            className={inputCls}
          />
        </div>
        <div>
          <label className="text-[11px] text-muted-foreground mb-1.5 block">盈利回调 (%)</label>
          <input
            type="number" min={0.01} max={0.5} step={0.01}
            value={value.profitCallback}
            onChange={(e) => update('profitCallback', Number(e.target.value))}
            className={inputCls}
          />
        </div>
      </div>

      <div>
        <label className="text-[11px] text-muted-foreground mb-1.5 block">止盈方式</label>
        <div className="flex gap-2">
          {([
            { key: 'full', label: '全仓止盈' },
            { key: 'tail', label: '尾单止盈' },
            { key: 'head_tail', label: '首尾止盈' },
            { key: 'moving', label: '移动止盈' },
          ] as const).map((m) => (
            <button
              key={m.key}
              onClick={() => update('tpMethod', m.key)}
              className={cn(
                'flex-1 py-2 rounded-lg text-xs border transition-colors',
                value.tpMethod === m.key
                  ? 'bg-quant-gold/10 border-quant-gold/20 text-quant-gold'
                  : 'border-quant-border text-muted-foreground hover:text-foreground'
              )}
            >
              {m.label}
            </button>
          ))}
        </div>
      </div>

      {/* 指标策略 */}
      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className="text-[11px] text-muted-foreground mb-1.5 block">开仓指标</label>
          <select
            value={value.openInd}
            onChange={(e) => update('openInd', e.target.value)}
            className={inputCls}
          >
            <option value="macd_golden">MACD金叉开多</option>
            <option value="macd_death">MACD死叉开空</option>
            <option value="ema">EMA拐点开仓</option>
            <option value="close">关闭（无脑买入）</option>
          </select>
        </div>
        <div>
          <label className="text-[11px] text-muted-foreground mb-1.5 block">补仓指标</label>
          <select
            value={value.addInd}
            onChange={(e) => update('addInd', e.target.value)}
            className={inputCls}
          >
            <option value="macd">MACD补仓</option>
            <option value="ema">EMA4补仓</option>
            <option value="close">仅按跌幅</option>
          </select>
        </div>
      </div>

      {/* 防瀑布 */}
      <div>
        <label className="text-[11px] text-muted-foreground mb-1.5 block">防瀑布 (%)</label>
        <input
          type="number" min={0.5} max={20} step={0.5}
          value={value.waterfall}
          onChange={(e) => update('waterfall', Number(e.target.value))}
          className={inputCls}
        />
      </div>

      {/* 开关选项 */}
      <div className="flex flex-wrap gap-3">
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input
            type="checkbox"
            checked={value.openDouble}
            onChange={(e) => update('openDouble', e.target.checked)}
            className="rounded"
          />
          开仓加倍
        </label>
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input
            type="checkbox"
            checked={value.trendInd}
            onChange={(e) => update('trendInd', e.target.checked)}
            className="rounded"
          />
          趋势指标(EMA4)
        </label>
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input
            type="checkbox"
            checked={value.followTrend}
            onChange={(e) => update('followTrend', e.target.checked)}
            className="rounded"
          />
          顺势而为
        </label>
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input
            type="checkbox"
            checked={value.burnCut}
            onChange={(e) => update('burnCut', e.target.checked)}
            className="rounded"
          />
          斩仓燃烧
        </label>
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input
            type="checkbox"
            checked={value.closeAddPos}
            onChange={(e) => update('closeAddPos', e.target.checked)}
            className="rounded"
          />
          关闭补仓
        </label>
     </div>

      {/* 交易次数模式 */}
      {showTradeCountMode && (
        <div>
          <label className="text-[11px] text-muted-foreground mb-1.5 block">交易次数</label>
          <div className="flex gap-2">
            {([
              { key: 'single', label: '单次循环' },
              { key: 'cycle', label: '策略循环' },
            ] as const).map((m) => (
              <button
                key={m.key}
                onClick={() => update('tradeCountMode', m.key)}
                className={cn(
                  'flex-1 py-2 rounded-lg text-xs border transition-colors',
                  value.tradeCountMode === m.key
                    ? 'bg-quant-gold/10 border-quant-gold/20 text-quant-gold'
                    : 'border-quant-border text-muted-foreground hover:text-foreground'
                )}
              >
                {m.label}
              </button>
            ))}
          </div>
        </div>
      )}

      {/* ── Extended fields (StrategyCreateModal) ── */}
      {showExtended && (
        <>
          <div className="border-t border-quant-border/50 pt-4 space-y-4">
            <div className="text-[11px] font-semibold text-quant-gold">扩展风控参数</div>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="text-[11px] text-muted-foreground mb-1.5 block">止损比例 (%)</label>
                <input
                  type="number" min={0} max={100} step={0.1}
                  value={value.stopLossRatio ?? 0}
                  onChange={(e) => update('stopLossRatio', Number(e.target.value))}
                  className={inputCls}
                />
              </div>
              <div>
                <label className="text-[11px] text-muted-foreground mb-1.5 block">止损金额 (USDT)</label>
                <input
                  type="number" min={0}
                  value={value.stopLossAmount ?? 0}
                  onChange={(e) => update('stopLossAmount', Number(e.target.value))}
                  className={inputCls}
                />
              </div>
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="text-[11px] text-muted-foreground mb-1.5 block">在线单量限制</label>
                <input
                  type="number" min={1} max={50}
                  value={value.onlineOrderLimit ?? 10}
                  onChange={(e) => update('onlineOrderLimit', Number(e.target.value))}
                  className={inputCls}
                />
              </div>
           </div>
            <div className="flex flex-wrap gap-3">
              <label className="flex items-center gap-2 text-xs text-muted-foreground">
                <input type="checkbox" checked={value.reverseTP ?? false} onChange={(e) => update('reverseTP', e.target.checked)} className="rounded" />
                反向止盈
              </label>
              <label className="flex items-center gap-2 text-xs text-muted-foreground">
                <input type="checkbox" checked={value.reverseSL ?? false} onChange={(e) => update('reverseSL', e.target.checked)} className="rounded" />
                反向止损
              </label>
              <label className="flex items-center gap-2 text-xs text-muted-foreground">
                <input type="checkbox" checked={value.profitProtection ?? false} onChange={(e) => update('profitProtection', e.target.checked)} className="rounded" />
                盈利保护
              </label>
              <label className="flex items-center gap-2 text-xs text-muted-foreground">
                <input type="checkbox" checked={value.customReduce ?? false} onChange={(e) => update('customReduce', e.target.checked)} className="rounded" />
                自定义减仓
              </label>
            </div>
          </div>
        </>
      )}
    </div>
  )
}

/** 将 CRAParams 转换为后端 API 所需的 snake_case 参数对象 */
export function craParamsToApiPayload(p: CRAParams) {
  return {
    order_count: p.orderCount,
    first_order_amount: p.firstOrderAmount,
    add_position_spread: p.addPosSpread,
    add_position_callback: p.addPosCallback,
    add_position_multiple: p.addPosMultiple ?? 1,
    take_profit_ratio: p.tpRatio,
    profit_callback: p.profitCallback,
    take_profit_method: p.tpMethod,
    moving_take_profit: p.movingTP,
    open_indicator: p.openInd,
    add_position_indicator: p.addInd,
    waterfall_protection: p.waterfall,
    open_double: p.openDouble,
    trend_indicator: p.trendInd,
    trend_timeframe: p.trendTf,
    follow_trend: p.followTrend,
    follow_trend_max: 5,
    reverse_take_profit: p.reverseTP ?? false,
    reverse_stop_loss: p.reverseSL ?? false,
    amplitude: p.amplitude,
    burn_cut: p.burnCutExtra ?? { enabled: p.burnCut, dual_burn_start: 3, global_burn_start: 5 },
    custom_reduce: p.customReduce ?? false,
    online_order_limit: p.onlineOrderLimit ?? 10,
    profit_protection: p.profitProtection ?? false,
    stop_loss_ratio: p.stopLossRatio ?? 0,
    stop_loss_amount: p.stopLossAmount ?? 0,
    stop_loss_price: p.stopLossPrice ?? 0,
    first_order_price: p.firstOrderPrice ?? 0,
    close_add_position: p.closeAddPos,
    leverage: p.leverage,
    direction: p.direction,
    trade_count_mode: p.tradeCountMode,
  }
}
