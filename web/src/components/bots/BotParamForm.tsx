import React from 'react'
import { CRAParamForm, DEFAULT_CRA_PARAMS, type CRAParams } from '@/components/strategy/CRAParamForm'
import type { AddPositionItem, MovingTPTier } from '@/types'

export function WizardField({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-1.5 flex items-center gap-2">
        <label className="text-xs font-medium text-[#aaaaaa]">{label}</label>
        {hint && <span className="text-[11px] text-[#8a8a8a]">{hint}</span>}
      </div>
      {children}
    </div>
  )
}

interface BotParamFormProps {
  form: Record<string, unknown>
  setForm: React.Dispatch<React.SetStateAction<Record<string, unknown>>>
  effectiveType: string
}

const CONTRACT_TYPES = new Set([
  'trend_long',
  'trend_short',
  'counter_stable',
  'counter_safe',
  'high_frequency',
  'head_tail_arbitrage',
  'macd_golden_long',
  'macd_death_short',
  'ema_follow_trend',
  'ema_counter_trend',
  'dual_burn',
  'global_burn',
])

function marketFromType(type: string): 'spot' | 'contract' {
  return CONTRACT_TYPES.has(type) ? 'contract' : 'spot'
}

function normalizeAddPositions(raw: unknown): AddPositionItem[] | undefined {
  if (!Array.isArray(raw)) return undefined
  return raw
    .filter((item): item is Record<string, unknown> => typeof item === 'object' && item !== null)
    .map((item, index) => ({
      order: typeof item.order === 'number' ? item.order : index + 1,
      multiplier: Number(item.multiplier) || 1,
      spread: Number(item.spread) || 3.5,
      callback: Number(item.callback) || 0.5,
      ema: Boolean(item.ema),
    }))
}

function normalizeMovingTPTiers(raw: unknown): MovingTPTier[] | undefined {
  if (!Array.isArray(raw)) return undefined
  return raw
    .filter((item): item is Record<string, unknown> => typeof item === 'object' && item !== null)
    .map((item) => ({
      ratio: Number(item.ratio) || 2,
      drawback: Number(item.drawback) || 20,
    }))
}

/* ─── CRA↔ snake_case form conversion ─────────────────────────────── */
function formToCra(f: Record<string, unknown>): CRAParams {
  return {
    firstOrderPrice: Number(f.first_order_price ?? DEFAULT_CRA_PARAMS.firstOrderPrice),
    firstOrderAmount: Number(f.first_order_amount ?? DEFAULT_CRA_PARAMS.firstOrderAmount),
    firstOrderMultiplier: Number(f.first_order_multiplier ?? DEFAULT_CRA_PARAMS.firstOrderMultiplier),
    tradeCountMode: (f.trade_count_mode as CRAParams['tradeCountMode']) || DEFAULT_CRA_PARAMS.tradeCountMode,
    loopCount: Number(f.loop_count ?? DEFAULT_CRA_PARAMS.loopCount),
    enableAddPosition: Boolean(f.enable_add_position ?? DEFAULT_CRA_PARAMS.enableAddPosition),
    orderCount: Number(f.order_count ?? DEFAULT_CRA_PARAMS.orderCount),
    addPositions: normalizeAddPositions(f.add_positions) ?? DEFAULT_CRA_PARAMS.addPositions,
    tpMethod: (f.take_profit_method as CRAParams['tpMethod']) || DEFAULT_CRA_PARAMS.tpMethod,
    tpMode: (f.tp_mode as CRAParams['tpMode']) || DEFAULT_CRA_PARAMS.tpMode,
    tpRatio: Number(f.take_profit_ratio ?? DEFAULT_CRA_PARAMS.tpRatio),
    profitCallback: Number(f.profit_callback ?? DEFAULT_CRA_PARAMS.profitCallback),
    movingTPTiers: normalizeMovingTPTiers(f.moving_take_profit_tiers) ?? DEFAULT_CRA_PARAMS.movingTPTiers,
    openMacdEnabled: Boolean(f.open_macd_enabled ?? DEFAULT_CRA_PARAMS.openMacdEnabled),
    openMacdPeriod: (f.open_macd_period as CRAParams['openMacdPeriod']) || DEFAULT_CRA_PARAMS.openMacdPeriod,
    openCounterEmaEnabled: Boolean(f.open_counter_ema_enabled ?? DEFAULT_CRA_PARAMS.openCounterEmaEnabled),
    openCounterEmaPeriod:
      (f.open_counter_ema_period as CRAParams['openCounterEmaPeriod']) || DEFAULT_CRA_PARAMS.openCounterEmaPeriod,
    openTrendEmaEnabled: Boolean(f.open_trend_ema_enabled ?? DEFAULT_CRA_PARAMS.openTrendEmaEnabled),
    openTrendEmaPeriod:
      (f.open_trend_ema_period as CRAParams['openTrendEmaPeriod']) || DEFAULT_CRA_PARAMS.openTrendEmaPeriod,
    addMacdEnabled: Boolean(f.add_macd_enabled ?? DEFAULT_CRA_PARAMS.addMacdEnabled),
    addMacdPeriod: (f.add_macd_period as CRAParams['addMacdPeriod']) || DEFAULT_CRA_PARAMS.addMacdPeriod,
    addEmaEnabled: Boolean(f.add_ema_enabled ?? DEFAULT_CRA_PARAMS.addEmaEnabled),
    addEmaPeriod: (f.add_ema_period as CRAParams['addEmaPeriod']) || DEFAULT_CRA_PARAMS.addEmaPeriod,
    waterfallEnabled: Boolean(f.waterfall_enabled ?? DEFAULT_CRA_PARAMS.waterfallEnabled),
    waterfall: Number(f.waterfall_protection ?? DEFAULT_CRA_PARAMS.waterfall),
    stopLossEnabled: Boolean(f.stop_loss_enabled ?? DEFAULT_CRA_PARAMS.stopLossEnabled),
    stopLossType: (f.stop_loss_type as CRAParams['stopLossType']) || DEFAULT_CRA_PARAMS.stopLossType,
    stopLossRatio: Number(f.stop_loss_ratio ?? DEFAULT_CRA_PARAMS.stopLossRatio),
    stopLossAmount: Number(f.stop_loss_amount ?? DEFAULT_CRA_PARAMS.stopLossAmount),
    stopLossPrice: Number(f.stop_loss_price ?? DEFAULT_CRA_PARAMS.stopLossPrice),
    reverseTP: (f.reverse_take_profit_period as CRAParams['reverseTP']) || DEFAULT_CRA_PARAMS.reverseTP,
    reverseSL: Boolean(f.reverse_stop_loss ?? DEFAULT_CRA_PARAMS.reverseSL),
    burnGlobalEnabled: Boolean(f.burn_global_enabled ?? DEFAULT_CRA_PARAMS.burnGlobalEnabled),
    burnGlobalThreshold: Number(f.burn_global_threshold ?? DEFAULT_CRA_PARAMS.burnGlobalThreshold),
    burnDualEnabled: Boolean(f.burn_dual_enabled ?? DEFAULT_CRA_PARAMS.burnDualEnabled),
    burnDualThreshold: Number(f.burn_dual_threshold ?? DEFAULT_CRA_PARAMS.burnDualThreshold),
    openDouble: Boolean(f.open_double ?? DEFAULT_CRA_PARAMS.openDouble),
    followTrend: Boolean(f.follow_trend ?? DEFAULT_CRA_PARAMS.followTrend),
    onlineOrderLimit: Number(f.online_order_limit ?? DEFAULT_CRA_PARAMS.onlineOrderLimit),
    leverage: Number(f.leverage ?? DEFAULT_CRA_PARAMS.leverage),
    direction: (f.direction as CRAParams['direction']) || DEFAULT_CRA_PARAMS.direction,
  }
}

function craToForm(cra: CRAParams, f: Record<string, unknown>): Record<string, unknown> {
  return {
    ...f,
    first_order_price: cra.firstOrderPrice,
    first_order_amount: cra.firstOrderAmount,
    first_order_multiplier: cra.firstOrderMultiplier,
    trade_count_mode: cra.tradeCountMode,
    loop_count: cra.loopCount,
    enable_add_position: cra.enableAddPosition,
    order_count: cra.orderCount,
    add_positions: cra.addPositions,
    take_profit_method: cra.tpMethod,
    tp_mode: cra.tpMode,
    take_profit_ratio: cra.tpRatio,
    profit_callback: cra.profitCallback,
    moving_take_profit_tiers: cra.movingTPTiers,
    open_macd_enabled: cra.openMacdEnabled,
    open_macd_period: cra.openMacdPeriod,
    open_counter_ema_enabled: cra.openCounterEmaEnabled,
    open_counter_ema_period: cra.openCounterEmaPeriod,
    open_trend_ema_enabled: cra.openTrendEmaEnabled,
    open_trend_ema_period: cra.openTrendEmaPeriod,
    add_macd_enabled: cra.addMacdEnabled,
    add_macd_period: cra.addMacdPeriod,
    add_ema_enabled: cra.addEmaEnabled,
    add_ema_period: cra.addEmaPeriod,
    waterfall_enabled: cra.waterfallEnabled,
    waterfall_protection: cra.waterfall,
    stop_loss_enabled: cra.stopLossEnabled,
    stop_loss_type: cra.stopLossType,
    stop_loss_ratio: cra.stopLossRatio,
    stop_loss_amount: cra.stopLossAmount,
    stop_loss_price: cra.stopLossPrice,
    reverse_take_profit_period: cra.reverseTP,
    reverse_stop_loss: cra.reverseSL,
    burn_global_enabled: cra.burnGlobalEnabled,
    burn_global_threshold: cra.burnGlobalThreshold,
    burn_dual_enabled: cra.burnDualEnabled,
    burn_dual_threshold: cra.burnDualThreshold,
    open_double: cra.openDouble,
    follow_trend: cra.followTrend,
    online_order_limit: cra.onlineOrderLimit,
    leverage: cra.leverage,
    direction: cra.direction,
  }
}

export function BotParamForm({ form, setForm, effectiveType }: BotParamFormProps) {
  const CRA_TYPES = new Set([
    'martin_trend',
    'wallstreet',
    'aggressive',
    'conservative',
    'high_frequency',
    'high_flat',
    'trend_long',
    'trend_short',
    'counter_stable',
    'counter_safe',
    'head_tail_arbitrage',
    'dual_burn',
    'global_burn',
    'macd_golden',
    'macd_death',
    'macd_golden_long',
    'macd_death_short',
    'ema_follow',
    'ema_counter',
    'ema_follow_trend',
    'ema_counter_trend',
  ])
  const isCraType = CRA_TYPES.has(effectiveType)

  const market = marketFromType(effectiveType)

  const handleCraChange = (cra: CRAParams) => {
    setForm(craToForm(cra, form))
  }

  return (
    <div className="space-y-4">
      {/* -- 基础策略参数 -- */}
      <div className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-4 space-y-4">
        <div className="text-xs font-semibold text-white">基础策略参数</div>
        <WizardField label="K线周期">
          <select
            value={(form.timeframe as string) || '1h'}
            onChange={(e) => setForm((f) => ({ ...f, timeframe: e.target.value }))}
            className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#4f6ed1]/40"
          >
            <option value="1m">1分钟</option>
            <option value="5m">5分钟</option>
            <option value="15m">15分钟</option>
            <option value="30m">30分钟</option>
            <option value="1h">1小时</option>
            <option value="4h">4小时</option>
            <option value="1d">1天</option>
          </select>
        </WizardField>
      </div>

      {/* -- CRA 参数配置 (shared component) -- */}
      {isCraType && (
        <div className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-4 space-y-4">
          <div className="text-xs font-semibold text-[#4f6ed1]">CRA 量化参数</div>
          <CRAParamForm value={formToCra(form)} onChange={handleCraChange} market={market} />
        </div>
      )}
    </div>
  )
}
