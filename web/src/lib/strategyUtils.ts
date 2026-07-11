import type { AddPositionItem, MovingTPTier } from '@/types'
import { apiPayloadToCraParams } from '@/components/strategy/CRAParamForm'
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
      emaEnabled: false,
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

  // Convert known backend decimal fields to UI percentages and map snake_case keys.
  const migrated = apiPayloadToCraParams(parsed as Partial<Parameters<typeof apiPayloadToCraParams>[0]>)

  // Determine multiplier preset from strategy_type if present
  const strategyTypeHint = typeof parsed.strategy_type === 'string' ? parsed.strategy_type : undefined
  const multiplierPreset = strategyTypeToMultiplierPreset(strategyTypeHint ?? '', market)

  // Reconcile addPositions: use correct multipliers for the strategy type,
  // but preserve spread/callback/ema from the migrated data.
  let addPositions = migrated.addPositions
  if (Array.isArray(parsed.add_positions)) {
    const defaults = createDefaultAddPositions(multiplierPreset, migrated.orderCount)
    addPositions = defaults.map((defaultRow, index) => {
      const migratedRow = migrated.addPositions[index]
      return {
        ...defaultRow,
        spread: migratedRow?.spread ?? defaultRow.spread,
        callback: migratedRow?.callback ?? defaultRow.callback,
        emaEnabled: migratedRow?.emaEnabled ?? defaultRow.emaEnabled,
      }
    })
  }

  return {
    ...migrated,
    addPositions,
  }
}
