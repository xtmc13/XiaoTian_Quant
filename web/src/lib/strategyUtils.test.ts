import { describe, it, expect } from 'vitest'
import {
  getMultiplierSequence,
  createDefaultAddPositions,
  createDefaultMovingTPTiers,
  calculateAddPositionTotal,
  strategyTypeToMultiplierPreset,
  createDefaultCRAParams,
  isCRAStrategyType,
  stripCRAFeatureKeys,
  CRA_FEATURE_KEYS,
  MARKET_DEFAULTS,
} from './strategyUtils'
import type { AddPositionItem } from '@/types'

describe('strategyUtils', () => {
  describe('getMultiplierSequence', () => {
    it('returns martin sequence 1,2,4,8,16,32,64', () => {
      expect(getMultiplierSequence('martin').slice(0, 7)).toEqual([1, 2, 4, 8, 16, 32, 64])
    })

    it('returns contract_martin variant sequence 1,2,4,4,8,8,16,16,32', () => {
      expect(getMultiplierSequence('contract_martin').slice(0, 9)).toEqual([1, 2, 4, 4, 8, 8, 16, 16, 32])
    })

    it('returns wallstreet fibonacci sequence 1,2,3,5,8,13,21,34,55', () => {
      expect(getMultiplierSequence('wallstreet').slice(0, 9)).toEqual([1, 2, 3, 5, 8, 13, 21, 34, 55])
    })

    it('returns aggressive as martin style', () => {
      expect(getMultiplierSequence('aggressive').slice(0, 4)).toEqual([1, 2, 4, 8])
    })

    it('returns conservative as martin style', () => {
      expect(getMultiplierSequence('conservative').slice(0, 4)).toEqual([1, 2, 4, 8])
    })

    it('returns high_frequency as martin style', () => {
      expect(getMultiplierSequence('high_frequency').slice(0, 4)).toEqual([1, 2, 4, 8])
    })
  })

  describe('createDefaultAddPositions', () => {
    it('creates 7 martin positions with correct multipliers', () => {
      const positions = createDefaultAddPositions('martin', 7)
      expect(positions).toHaveLength(7)
      expect(positions.map((p) => p.multiplier)).toEqual([1, 2, 4, 8, 16, 32, 64])
      expect(positions[0]).toMatchObject({ order: 1, multiplier: 1, spread: 3.5, callback: 0.3, emaEnabled: false })
    })

    it('uses CRA实拍 spot spread sequence 3.5/5/7/9/11/13/15', () => {
      const positions = createDefaultAddPositions('martin', 7)
      expect(positions.map((p) => p.spread)).toEqual([3.5, 5, 7, 9, 11, 13, 15])
    })

    it('uses CRA实拍 contract spread sequence 3/4/5/7/9/10/11/12/15', () => {
      const positions = createDefaultAddPositions('contract_martin', 9)
      expect(positions.map((p) => p.spread)).toEqual([3, 4, 5, 7, 9, 10, 11, 12, 15])
    })

    it('creates 9 contract_martin positions with variant multipliers', () => {
      const positions = createDefaultAddPositions('contract_martin', 9)
      expect(positions).toHaveLength(9)
      expect(positions.map((p) => p.multiplier)).toEqual([1, 2, 4, 4, 8, 8, 16, 16, 32])
    })

    it('creates wallstreet positions with fibonacci multipliers', () => {
      const positions = createDefaultAddPositions('wallstreet', 5)
      expect(positions.map((p) => p.multiplier)).toEqual([1, 2, 3, 5, 8])
    })

    it('limits order count to a safe maximum', () => {
      const positions = createDefaultAddPositions('martin', 25)
      expect(positions.length).toBeLessThanOrEqual(20)
    })

    it('uses provided base spread and callback', () => {
      const positions = createDefaultAddPositions('martin', 3, 2, 0.5)
      expect(positions[0].spread).toBe(2)
      expect(positions[0].callback).toBe(0.5)
    })
  })

  describe('createDefaultMovingTPTiers', () => {
    it('returns 4 spot tiers', () => {
      const tiers = createDefaultMovingTPTiers('spot')
      expect(tiers).toHaveLength(4)
      expect(tiers[0]).toEqual({ ratio: 2, drawback: 20 })
      expect(tiers[3]).toEqual({ ratio: 5, drawback: 10 })
    })

    it('returns 4 contract tiers', () => {
      const tiers = createDefaultMovingTPTiers('contract')
      expect(tiers).toHaveLength(4)
      expect(tiers[0]).toEqual({ ratio: 1.1, drawback: 15 })
      expect(tiers[3]).toEqual({ ratio: 4, drawback: 10 })
    })
  })

  describe('calculateAddPositionTotal', () => {
    it('calculates total from first order and add positions', () => {
      const addPositions: AddPositionItem[] = [
        { order: 1, multiplier: 1, spread: 3.5, callback: 0.3 },
        { order: 2, multiplier: 2, spread: 5, callback: 0.5 },
        { order: 3, multiplier: 4, spread: 7, callback: 0.5 },
      ]
      // first = 10 * 1 = 10; total = 10 + 10*1 + 10*2 + 10*4 = 80
      expect(calculateAddPositionTotal(10, 1, addPositions)).toBe(80)
    })

    it('applies first order multiplier', () => {
      const addPositions: AddPositionItem[] = [{ order: 1, multiplier: 1, spread: 3.5, callback: 0.3 }]
      // first = 10 * 2 = 20; total = 20 + 20*1 = 40
      expect(calculateAddPositionTotal(10, 2, addPositions)).toBe(40)
    })

    it('returns first order total when no add positions', () => {
      expect(calculateAddPositionTotal(100, 1, [])).toBe(100)
    })
  })

  describe('strategyTypeToMultiplierPreset', () => {
    it('maps wallstreet strategy to wallstreet preset', () => {
      expect(strategyTypeToMultiplierPreset('wallstreet', 'spot')).toBe('wallstreet')
    })

    it('maps martin_trend to martin preset for spot', () => {
      expect(strategyTypeToMultiplierPreset('martin_trend', 'spot')).toBe('martin')
    })

    it('maps contract trend strategy to contract_martin preset', () => {
      expect(strategyTypeToMultiplierPreset('trend_long', 'contract')).toBe('contract_martin')
    })

    it('maps unknown spot strategy to martin preset', () => {
      expect(strategyTypeToMultiplierPreset('unknown', 'spot')).toBe('martin')
    })
  })

  describe('createDefaultCRAParams', () => {
    it('returns spot defaults aligned with CRA spec', () => {
      const params = createDefaultCRAParams('spot')
      expect(params.firstOrderAmount).toBe(MARKET_DEFAULTS.spot.firstOrderAmount)
      expect(params.orderCount).toBe(MARKET_DEFAULTS.spot.orderCount)
      expect(params.loopCount).toBe(MARKET_DEFAULTS.spot.loopCount)
      expect(params.stopLossRatio).toBe(MARKET_DEFAULTS.spot.stopLossRatio)
      expect(params.leverage).toBe(MARKET_DEFAULTS.spot.leverage)
      expect(params.addPositions).toHaveLength(MARKET_DEFAULTS.spot.orderCount)
      expect(params.movingTPTiers).toEqual(createDefaultMovingTPTiers('spot'))
      expect(params.stopLossEnabled).toBe(false)
    })

    it('returns contract defaults aligned with CRA spec', () => {
      const params = createDefaultCRAParams('contract')
      expect(params.firstOrderAmount).toBe(MARKET_DEFAULTS.contract.firstOrderAmount)
      expect(params.orderCount).toBe(MARKET_DEFAULTS.contract.orderCount)
      expect(params.loopCount).toBe(MARKET_DEFAULTS.contract.loopCount)
      expect(params.stopLossRatio).toBe(MARKET_DEFAULTS.contract.stopLossRatio)
      expect(params.leverage).toBe(MARKET_DEFAULTS.contract.leverage)
      expect(params.addPositions).toHaveLength(MARKET_DEFAULTS.contract.orderCount)
      expect(params.addPositions.map((p) => p.multiplier).slice(0, 9)).toEqual([1, 2, 4, 4, 8, 8, 16, 16, 32])
      expect(params.movingTPTiers).toEqual(createDefaultMovingTPTiers('contract'))
      expect(params.stopLossEnabled).toBe(true)
    })
  })
})

describe('P0-4 类型-参数防呆', () => {
  it('isCRAStrategyType accepts cra_contract/cra_spot and backend CRA aliases', () => {
    expect(isCRAStrategyType('cra_contract')).toBe(true)
    expect(isCRAStrategyType('cra_spot')).toBe(true)
    expect(isCRAStrategyType('trend_long')).toBe(true)
    expect(isCRAStrategyType('trend_short')).toBe(true)
    expect(isCRAStrategyType('martin_trend')).toBe(true)
    expect(isCRAStrategyType('wallstreet')).toBe(true)
    expect(isCRAStrategyType('counter_safe')).toBe(true)
    expect(isCRAStrategyType('head_tail_arbitrage')).toBe(true)
  })

  it('isCRAStrategyType rejects indicator/script types', () => {
    expect(isCRAStrategyType('macd')).toBe(false)
    expect(isCRAStrategyType('rsi')).toBe(false)
    expect(isCRAStrategyType('trend')).toBe(false)
    expect(isCRAStrategyType('ScriptStrategy')).toBe(false)
    expect(isCRAStrategyType('')).toBe(false)
  })

  it('stripCRAFeatureKeys removes CRA-only keys and keeps generic fields', () => {
    const config = {
      symbol: 'BTCUSDT',
      leverage: 10,
      margin_mode: 'cross',
      first_order_amount: 100,
      add_positions: [{ order: 1, multiplier: 1, spread: 0.03, callback: 0.003 }],
      tp_mode: 'static',
      moving_take_profit_tiers: [],
      timeframe: '15m',
    }
    const stripped = stripCRAFeatureKeys(config)
    for (const k of CRA_FEATURE_KEYS) {
      expect(stripped).not.toHaveProperty(k)
    }
    expect(stripped).toMatchObject({ symbol: 'BTCUSDT', leverage: 10, margin_mode: 'cross', timeframe: '15m' })
  })
})

describe('applyServerStrategyDefaults（服务端默认参数兜底）', () => {
  it('现货 martin_trend 映射 martin_trend_v2 档案（小数→百分数口径换算）', async () => {
    const { applyServerStrategyDefaults, createDefaultCRAParams } = await import('./strategyUtils')
    const base = {
      ...createDefaultCRAParams('spot'),
      addPositions: [
        { order: 1, multiplier: 2, spread: 3, callback: 0.5 },
        { order: 2, multiplier: 4, spread: 6, callback: 0.5 },
      ],
    }
    const next = applyServerStrategyDefaults(base, {
      market: 'spot',
      strategyType: 'martin_trend',
      defaults: {
        strategies: [
          {
            key: 'martin_trend_v2',
            parameters: {
              first_order_amount: 100,
              take_profit_ratio: 0.013,
              profit_callback: 0.003,
              flash_crash_protection: 0.02,
              double_first_order: true,
              add_position_spread: 0.03,
              add_position_callback: 0.003,
            },
          },
        ],
      },
    })
    expect(next.firstOrderAmount).toBe(100)
    expect(next.tpRatio).toBeCloseTo(1.3)
    expect(next.profitCallback).toBeCloseTo(0.3)
    expect(next.waterfall).toBeCloseTo(2)
    expect(next.waterfallEnabled).toBe(true)
    expect(next.firstOrderMultiplier).toBe(2)
    // ladder 档数/multiplier 不变，spread 等差重算、callback 统一
    expect(next.addPositions.map((p) => p.multiplier)).toEqual([2, 4])
    expect(next.addPositions[0].spread).toBeCloseTo(3)
    expect(next.addPositions[1].spread).toBeCloseTo(6)
    expect(next.addPositions[0].callback).toBeCloseTo(0.3)
  })

  it('合约应用 contract_defaults（杠杆 + both→dual）', async () => {
    const { applyServerStrategyDefaults, createDefaultCRAParams } = await import('./strategyUtils')
    const next = applyServerStrategyDefaults(createDefaultCRAParams('contract'), {
      market: 'contract',
      strategyType: 'cra_contract',
      defaults: { contract_defaults: { leverage: 20, direction: 'both' } },
    })
    expect(next.leverage).toBe(20)
    expect(next.direction).toBe('dual')
  })

  it('无匹配档案/空 defaults 时原样返回', async () => {
    const { applyServerStrategyDefaults, createDefaultCRAParams } = await import('./strategyUtils')
    const base = createDefaultCRAParams('spot')
    expect(
      applyServerStrategyDefaults(base, { market: 'spot', strategyType: 'cra_spot', defaults: { strategies: [] } })
    ).toBe(base)
    expect(applyServerStrategyDefaults(base, { market: 'spot', strategyType: 'martin_trend', defaults: null })).toBe(base)
  })
})
