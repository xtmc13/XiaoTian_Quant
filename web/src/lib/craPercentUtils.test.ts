import { describe, it, expect } from 'vitest'
import { percentToDecimal, decimalToPercent, PERCENTAGE_FIELD_THRESHOLDS } from './craPercentUtils'

describe('craPercentUtils', () => {
  describe('percentToDecimal', () => {
    it('converts percentage values to decimal ratios', () => {
      expect(percentToDecimal(2)).toBe(0.02)
      expect(percentToDecimal(3.5)).toBe(0.035)
      expect(percentToDecimal(40)).toBe(0.4)
      expect(percentToDecimal(0.3)).toBe(0.003)
    })
  })

  describe('decimalToPercent', () => {
    it('converts backend decimal ratios to UI percentages', () => {
      expect(decimalToPercent(0.02, PERCENTAGE_FIELD_THRESHOLDS.movingTPRatio)).toBe(2)
      expect(decimalToPercent(0.035, PERCENTAGE_FIELD_THRESHOLDS.addPositionSpread)).toBe(3.5)
      expect(decimalToPercent(0.4, PERCENTAGE_FIELD_THRESHOLDS.stopLossRatio)).toBe(40)
      expect(decimalToPercent(0.003, PERCENTAGE_FIELD_THRESHOLDS.addPositionCallback)).toBe(0.3)
    })

    it('leaves values that already look like percentages untouched', () => {
      expect(decimalToPercent(2, PERCENTAGE_FIELD_THRESHOLDS.movingTPRatio)).toBe(2)
      expect(decimalToPercent(3.5, PERCENTAGE_FIELD_THRESHOLDS.addPositionSpread)).toBe(3.5)
      expect(decimalToPercent(40, PERCENTAGE_FIELD_THRESHOLDS.stopLossRatio)).toBe(40)
      expect(decimalToPercent(0.3, PERCENTAGE_FIELD_THRESHOLDS.addPositionCallback)).toBe(0.3)
    })
  })
})
