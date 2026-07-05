import { describe, it, expect } from 'vitest'
import {
  getMultiplierSequence,
  createDefaultAddPositions,
  createDefaultMovingTPTiers,
  calculateAddPositionTotal,
  strategyTypeToMultiplierPreset,
  createDefaultCRAParams,
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
      expect(positions[0]).toMatchObject({ order: 1, multiplier: 1, spread: 3.5, callback: 0.3, ema: false })
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
