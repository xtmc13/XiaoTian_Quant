import type { AddPositionItem, MovingTPTier } from '@/types'
import type { CRAParams } from '@/components/strategy/CRAParamForm'

export type StrategyMultiplierPreset =
  | 'martin'
  | 'wallstreet'
  | 'aggressive'
  | 'conservative'
  | 'high_frequency'
  | 'contract_martin'

const MAX_ORDER_COUNT = 20

/**
 * CRA 实拍默认补仓差价序列。
 * 现货 7 笔：3.5, 5, 7, 9, 11, 13, 15
 * 合约 9 笔：3, 4, 5, 7, 9, 10, 11, 12, 15
 */
const DEFAULT_SPREAD_SEQUENCES: Record<StrategyMultiplierPreset, number[]> = {
  martin: [3.5, 5, 7, 9, 11, 13, 15],
  wallstreet: [3.5, 5, 7, 9, 11, 13, 15],
  aggressive: [3.5, 5, 7, 9, 11, 13, 15],
  conservative: [3.5, 5, 7, 9, 11, 13, 15],
  high_frequency: [3.5, 5, 7, 9, 11, 13, 15],
  contract_martin: [3, 4, 5, 7, 9, 10, 11, 12, 15],
}

/**
 * Default market-specific CRA parameters.
 * These values must stay aligned with the CRA product specification.
 */
export interface MarketDefaults {
  firstOrderAmount: number
  orderCount: number
  loopCount: number
  stopLossRatio: number
  leverage: number
  addPositions: (preset: StrategyMultiplierPreset) => AddPositionItem[]
}

export const MARKET_DEFAULTS: Record<'spot' | 'contract', MarketDefaults> = {
  spot: {
    firstOrderAmount: 10,
    orderCount: 7,
    loopCount: 100,
    stopLossRatio: 0,
    leverage: 1,
    addPositions: (preset) => createDefaultAddPositions(preset, 7),
  },
  contract: {
    firstOrderAmount: 5,
    orderCount: 9,
    loopCount: 5000,
    stopLossRatio: 40,
    leverage: 10,
    addPositions: (preset) => createDefaultAddPositions(preset, 9),
  },
}

/**
 * Return the raw multiplier sequence for a given strategy preset.
 * - martin / aggressive / conservative / high_frequency: powers of two, first order = 1.
 * - wallstreet: Fibonacci-style sequence 1,2,3,5,8,13,21,34,55...
 * - contract_martin: variant martingale used for contract strategies,
 *   where each multiplier repeats once before doubling (1,2,4,4,8,8,16,16,32...).
 */
export function getMultiplierSequence(preset: StrategyMultiplierPreset): number[] {
  if (preset === 'wallstreet') {
    const seq: number[] = [1, 2]
    while (seq.length < MAX_ORDER_COUNT) {
      seq.push(seq[seq.length - 1] + seq[seq.length - 2])
    }
    return seq
  }
  if (preset === 'contract_martin') {
    const seq: number[] = [1, 2]
    let current = 4
    while (seq.length < MAX_ORDER_COUNT) {
      seq.push(current)
      seq.push(current)
      current *= 2
    }
    return seq
  }
  // martin and all spot variants default to powers of two with first order = 1x
  return Array.from({ length: MAX_ORDER_COUNT }, (_, i) => Math.pow(2, i))
}

/**
 * Build a default per-order add-position table.
 * When baseSpread is omitted, uses the CRA实拍 sequences above.
 * When baseSpread is provided, falls back to an arithmetic 1.5% step sequence.
 */
export function createDefaultAddPositions(
  preset: StrategyMultiplierPreset,
  orderCount: number,
  baseSpread?: number,
  baseCallback = 0.3
): AddPositionItem[] {
  const count = Math.min(Math.max(1, orderCount), MAX_ORDER_COUNT)
  const multipliers = getMultiplierSequence(preset)
  const spreadSeq = baseSpread == null ? DEFAULT_SPREAD_SEQUENCES[preset] : undefined
  return Array.from({ length: count }, (_, i) => {
    const order = i + 1
    const spread = spreadSeq?.[i] ?? Number(((baseSpread ?? 3.5) + i * 1.5).toFixed(1))
    return {
      order,
      multiplier: multipliers[i] ?? 1,
      spread,
      callback: i === 0 ? baseCallback : 0.5,
      ema: false,
    }
  })
}

/**
 * Default 4-tier moving take-profit tiers.
 */
export function createDefaultMovingTPTiers(market: 'spot' | 'contract'): MovingTPTier[] {
  if (market === 'contract') {
    return [
      { ratio: 1.1, drawback: 15 },
      { ratio: 2, drawback: 10 },
      { ratio: 3, drawback: 10 },
      { ratio: 4, drawback: 10 },
    ]
  }
  return [
    { ratio: 2, drawback: 20 },
    { ratio: 3, drawback: 20 },
    { ratio: 4, drawback: 10 },
    { ratio: 5, drawback: 10 },
  ]
}

/**
 * Calculate total capital required for the add-position plan.
 * Includes the first order (after its multiplier) plus every add-position order.
 */
export function calculateAddPositionTotal(
  firstOrderAmount: number,
  firstOrderMultiplier: number,
  addPositions: AddPositionItem[]
): number {
  const first = firstOrderAmount * firstOrderMultiplier
  return addPositions.reduce((sum, pos) => sum + first * pos.multiplier, first)
}

/**
 * Map a strategy_type value to a multiplier preset.
 */
export function strategyTypeToMultiplierPreset(
  strategyType: string,
  market: 'spot' | 'contract'
): StrategyMultiplierPreset {
  if (strategyType === 'wallstreet') return 'wallstreet'
  if (strategyType === 'martin_trend' || strategyType === 'martin') return 'martin'
  if (strategyType === 'aggressive') return 'aggressive'
  if (strategyType === 'conservative') return 'conservative'
  if (strategyType === 'high_frequency' || strategyType === 'high_flat') return 'high_frequency'
  // contract trend/counter types use the variant martingale sequence
  if (market === 'contract') return 'contract_martin'
  return 'martin'
}

/**
 * Create default CRA params for a given market.
 */
export function createDefaultCRAParams(market: 'spot' | 'contract'): CRAParams {
  const defaults = MARKET_DEFAULTS[market]
  const preset = market === 'contract' ? 'contract_martin' : 'martin'
  return {
    firstOrderPrice: 0,
    firstOrderAmount: defaults.firstOrderAmount,
    firstOrderMultiplier: 1,
    tradeCountMode: 'single',
    loopCount: defaults.loopCount,
    enableAddPosition: true,
    orderCount: defaults.orderCount,
    addPositions: defaults.addPositions(preset),
    tpMethod: 'full',
    tpMode: 'static',
    tpRatio: 1.3,
    profitCallback: 0.1,
    movingTPTiers: createDefaultMovingTPTiers(market),
    openMacdEnabled: false,
    openMacdPeriod: 'close',
    openCounterEmaEnabled: false,
    openCounterEmaPeriod: '15m',
    openTrendEmaEnabled: false,
    openTrendEmaPeriod: '15m',
    addMacdEnabled: false,
    addMacdPeriod: 'close',
    addEmaEnabled: false,
    addEmaPeriod: '15m',
    waterfallEnabled: true,
    waterfall: 2,
    stopLossEnabled: market === 'contract',
    stopLossType: 'ratio',
    stopLossRatio: defaults.stopLossRatio,
    stopLossAmount: 0,
    stopLossPrice: 0,
    reverseTP: 'close',
    reverseSL: false,
    burnGlobalEnabled: false,
    burnGlobalThreshold: 5,
    burnDualEnabled: false,
    burnDualThreshold: 3,
    openDouble: false,
    followTrend: false,
    onlineOrderLimit: 10,
    leverage: defaults.leverage,
    direction: 'long',
  }
}

/* ─── Legacy config migration ───────────────────────────────────────── */

type LegacyMovingTP =
  | {
      enabled?: boolean
      tier1_ratio?: number
      tier1_drawback?: number
      tier2_drawback?: number
    }
  | undefined

function legacyMovingTpToTiers(legacy: LegacyMovingTP, market: 'spot' | 'contract'): MovingTPTier[] {
  if (!legacy || typeof legacy !== 'object') {
    return createDefaultMovingTPTiers(market)
  }
  const defaults = createDefaultMovingTPTiers(market)
  return [
    {
      ratio: legacy.tier1_ratio ?? defaults[0]?.ratio ?? 2,
      drawback: legacy.tier1_drawback ?? defaults[0]?.drawback ?? 20,
    },
    { ratio: defaults[1]?.ratio ?? 3, drawback: defaults[1]?.drawback ?? 20 },
    { ratio: defaults[2]?.ratio ?? 4, drawback: legacy.tier2_drawback ?? defaults[2]?.drawback ?? 10 },
    { ratio: defaults[3]?.ratio ?? 5, drawback: defaults[3]?.drawback ?? 10 },
  ]
}

function asBool(v: unknown, fallback: boolean): boolean {
  if (typeof v === 'boolean') return v
  if (typeof v === 'number') return v !== 0
  if (typeof v === 'string') return v === 'true' || v === '1'
  return fallback
}

function asNumber(v: unknown, fallback: number): number {
  const n = Number(v)
  return Number.isFinite(n) ? n : fallback
}

function asEnum<T extends string>(v: unknown, options: readonly T[], fallback: T): T {
  if (typeof v === 'string' && options.includes(v as T)) return v as T
  return fallback
}

/**
 * Migrate legacy config_json / localStorage / template data into the current
 * CRAParams shape. Unknown/deprecated fields are ignored.
 */
export function migrateLegacyConfigToCRAParams(
  parsed: Record<string, unknown> | null | undefined,
  market: 'spot' | 'contract'
): CRAParams {
  const base = createDefaultCRAParams(market)
  if (!parsed || typeof parsed !== 'object') return base

  const p = parsed

  // Helper to read both snake_case and camelCase keys
  const get = <T>(keys: string[], transform: (v: unknown) => T, fallback: T): T => {
    for (const key of keys) {
      if (key in p) {
        return transform(p[key])
      }
    }
    return fallback
  }

  const orderCount = get(['order_count', 'orderCount'], (v) => asNumber(v, base.orderCount), base.orderCount)

  // Determine multiplier preset from strategy_type if present
  const strategyTypeHint = typeof p.strategy_type === 'string' ? p.strategy_type : undefined
  const multiplierPreset = strategyTypeToMultiplierPreset(strategyTypeHint ?? '', market)

  // Build addPositions: prefer explicit array, otherwise generate from defaults
  let addPositions: AddPositionItem[] = base.addPositions
  if (Array.isArray(p.add_positions)) {
    addPositions = p.add_positions
      .filter((item): item is Record<string, unknown> => typeof item === 'object' && item !== null)
      .map((item, index) => {
        const defaults = createDefaultAddPositions(multiplierPreset, orderCount)
        const defaultRow = defaults[index] ?? defaults[defaults.length - 1]
        return {
          order: typeof item.order === 'number' ? item.order : index + 1,
          multiplier: asNumber(item.multiplier, defaultRow?.multiplier ?? 1),
          spread: asNumber(item.spread, defaultRow?.spread ?? 3.5),
          callback: asNumber(item.callback, defaultRow?.callback ?? 0.5),
          ema: asBool(item.ema, defaultRow?.ema ?? false),
        }
      })
  }

  // Moving take-profit: support old object shape and new array shape
  let movingTPTiers = base.movingTPTiers
  if (Array.isArray(p.moving_take_profit_tiers)) {
    movingTPTiers = p.moving_take_profit_tiers
      .filter((item): item is Record<string, unknown> => typeof item === 'object' && item !== null)
      .map((item, index) => {
        const defaults = createDefaultMovingTPTiers(market)
        const defaultTier = defaults[index]
        return {
          ratio: asNumber(item.ratio, defaultTier?.ratio ?? 2),
          drawback: asNumber(item.drawback, defaultTier?.drawback ?? 20),
        }
      })
  } else if (p.moving_take_profit !== undefined) {
    movingTPTiers = legacyMovingTpToTiers(p.moving_take_profit as LegacyMovingTP, market)
  }

  return {
    firstOrderPrice: get(
      ['first_order_price', 'firstOrderPrice'],
      (v) => asNumber(v, base.firstOrderPrice),
      base.firstOrderPrice
    ),
    firstOrderAmount: get(
      ['first_order_amount', 'firstOrderAmount'],
      (v) => asNumber(v, base.firstOrderAmount),
      base.firstOrderAmount
    ),
    firstOrderMultiplier: get(
      ['first_order_multiplier', 'firstOrderMultiplier'],
      (v) => asNumber(v, base.firstOrderMultiplier),
      base.firstOrderMultiplier
    ),
    tradeCountMode: get(
      ['trade_count_mode', 'tradeCountMode'],
      (v) => asEnum(v, ['single', 'cycle'] as const, base.tradeCountMode),
      base.tradeCountMode
    ),
    loopCount: get(['loop_count', 'loopCount'], (v) => asNumber(v, base.loopCount), base.loopCount),
    enableAddPosition: get(
      ['enable_add_position', 'enableAddPosition'],
      (v) => asBool(v, base.enableAddPosition),
      base.enableAddPosition
    ),
    orderCount,
    addPositions,
    tpMethod: get(
      ['take_profit_method', 'tpMethod'],
      (v) => asEnum(v, ['full', 'tail', 'head_tail'] as const, base.tpMethod),
      base.tpMethod
    ),
    tpMode: get(['tp_mode', 'tpMode'], (v) => asEnum(v, ['static', 'moving'] as const, base.tpMode), base.tpMode),
    tpRatio: get(['take_profit_ratio', 'tpRatio'], (v) => asNumber(v, base.tpRatio), base.tpRatio),
    profitCallback: get(
      ['profit_callback', 'profitCallback'],
      (v) => asNumber(v, base.profitCallback),
      base.profitCallback
    ),
    movingTPTiers,
    openMacdEnabled: get(
      ['open_macd_enabled', 'openMacdEnabled'],
      (v) => asBool(v, base.openMacdEnabled),
      base.openMacdEnabled
    ),
    openMacdPeriod: get(
      ['open_macd_period', 'openMacdPeriod'],
      (v) => asEnum(v, ['close', '5m', '15m'] as const, base.openMacdPeriod),
      base.openMacdPeriod
    ),
    openCounterEmaEnabled: get(
      ['open_counter_ema_enabled', 'openCounterEmaEnabled'],
      (v) => asBool(v, base.openCounterEmaEnabled),
      base.openCounterEmaEnabled
    ),
    openCounterEmaPeriod: get(
      ['open_counter_ema_period', 'openCounterEmaPeriod'],
      (v) => asEnum(v, ['close', '5m', '15m'] as const, base.openCounterEmaPeriod),
      base.openCounterEmaPeriod
    ),
    openTrendEmaEnabled: get(
      ['open_trend_ema_enabled', 'openTrendEmaEnabled'],
      (v) => asBool(v, base.openTrendEmaEnabled),
      base.openTrendEmaEnabled
    ),
    openTrendEmaPeriod: get(
      ['open_trend_ema_period', 'openTrendEmaPeriod'],
      (v) => asEnum(v, ['close', '5m', '15m'] as const, base.openTrendEmaPeriod),
      base.openTrendEmaPeriod
    ),
    addMacdEnabled: get(
      ['add_macd_enabled', 'addMacdEnabled'],
      (v) => asBool(v, base.addMacdEnabled),
      base.addMacdEnabled
    ),
    addMacdPeriod: get(
      ['add_macd_period', 'addMacdPeriod'],
      (v) => asEnum(v, ['close', '5m', '15m'] as const, base.addMacdPeriod),
      base.addMacdPeriod
    ),
    addEmaEnabled: get(['add_ema_enabled', 'addEmaEnabled'], (v) => asBool(v, base.addEmaEnabled), base.addEmaEnabled),
    addEmaPeriod: get(
      ['add_ema_period', 'addEmaPeriod'],
      (v) => asEnum(v, ['close', '5m', '15m'] as const, base.addEmaPeriod),
      base.addEmaPeriod
    ),
    waterfallEnabled: get(
      ['waterfall_enabled', 'waterfallEnabled'],
      (v) => asBool(v, base.waterfallEnabled),
      base.waterfallEnabled
    ),
    waterfall: get(['waterfall_protection', 'waterfall'], (v) => asNumber(v, base.waterfall), base.waterfall),
    stopLossEnabled: get(
      ['stop_loss_enabled', 'stopLossEnabled'],
      (v) => asBool(v, base.stopLossEnabled),
      base.stopLossEnabled
    ),
    stopLossType: get(
      ['stop_loss_type', 'stopLossType'],
      (v) => asEnum(v, ['ratio', 'amount', 'price'] as const, base.stopLossType),
      base.stopLossType
    ),
    stopLossRatio: get(
      ['stop_loss_ratio', 'stopLossRatio'],
      (v) => asNumber(v, base.stopLossRatio),
      base.stopLossRatio
    ),
    stopLossAmount: get(
      ['stop_loss_amount', 'stopLossAmount'],
      (v) => asNumber(v, base.stopLossAmount),
      base.stopLossAmount
    ),
    stopLossPrice: get(
      ['stop_loss_price', 'stopLossPrice'],
      (v) => asNumber(v, base.stopLossPrice),
      base.stopLossPrice
    ),
    reverseTP: get(
      ['reverse_take_profit_period', 'reverseTP'],
      (v) => asEnum(v, ['close', '5m', '15m'] as const, base.reverseTP),
      base.reverseTP
    ),
    reverseSL: get(['reverse_stop_loss', 'reverseSL'], (v) => asBool(v, base.reverseSL), base.reverseSL),
    burnGlobalEnabled: get(
      ['burn_global_enabled', 'burnGlobalEnabled'],
      (v) => asBool(v, base.burnGlobalEnabled),
      base.burnGlobalEnabled
    ),
    burnGlobalThreshold: get(
      ['burn_global_threshold', 'burnGlobalThreshold'],
      (v) => asNumber(v, base.burnGlobalThreshold),
      base.burnGlobalThreshold
    ),
    burnDualEnabled: get(
      ['burn_dual_enabled', 'burnDualEnabled'],
      (v) => asBool(v, base.burnDualEnabled),
      base.burnDualEnabled
    ),
    burnDualThreshold: get(
      ['burn_dual_threshold', 'burnDualThreshold'],
      (v) => asNumber(v, base.burnDualThreshold),
      base.burnDualThreshold
    ),
    openDouble: get(['open_double', 'openDouble'], (v) => asBool(v, base.openDouble), base.openDouble),
    followTrend: get(['follow_trend', 'followTrend'], (v) => asBool(v, base.followTrend), base.followTrend),
    onlineOrderLimit: get(
      ['online_order_limit', 'onlineOrderLimit'],
      (v) => asNumber(v, base.onlineOrderLimit),
      base.onlineOrderLimit
    ),
    leverage: get(['leverage'], (v) => asNumber(v, base.leverage), base.leverage),
    direction: get(['direction'], (v) => asEnum(v, ['long', 'short', 'dual'] as const, base.direction), base.direction),
  }
}
