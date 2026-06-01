import { describe, it, expect } from 'vitest'
import {
  calculateSMA, calculateEMA, calculateBollingerBands, calculateRSI, calculateMACD,
  calculateATR, calculateCCI, calculateWilliamsR, calculateMFI, calculateADX, calculateOBV, calculateKDJ,
  type KLineBar,
} from './technicalIndicators'

const makeSampleData = (n: number, base: number): KLineBar[] => {
  const result: KLineBar[] = []
  for (let i = 0; i < n; i++) {
    result.push({
      timestamp: i * 3600000,
      open: base + i * 0.5 - 0.1,
      high: base + i * 0.5 + 1,
      low: base + i * 0.5 - 1,
      close: base + i * 0.5,
      volume: 1000 + i * 100,
    })
  }
  return result
}

const last = (arr: (number | null)[]) => arr[arr.length - 1]

describe('SMA', () => {
  it('calculates simple moving average', () => {
    const data = makeSampleData(20, 100)
    const result = calculateSMA(data, 5)
    expect(result.length).toBe(20)
    expect(last(result)).toBeCloseTo(108.5, 0)
  })
})

describe('EMA', () => {
  it('calculates exponential moving average', () => {
    const data = makeSampleData(20, 100)
    const result = calculateEMA(data, 12)
    expect(result.length).toBe(20)
    expect(last(result)).toBeGreaterThan(105)
  })
})

describe('Bollinger Bands', () => {
  it('returns upper, middle, lower bands', () => {
    const data = makeSampleData(20, 100)
    const bands = calculateBollingerBands(data, 5, 2)
    expect(bands.length).toBe(20)
    const last = bands[bands.length - 1]
    expect(last.upper).toBeGreaterThan(last.middle!)
    expect(last.middle!).toBeGreaterThan(last.lower!)
  })
})

describe('RSI', () => {
  it('calculates RSI between 0 and 100', () => {
    const data = makeSampleData(30, 100)
    const result = calculateRSI(data, 14)
    const v = last(result)
    expect(v).toBeGreaterThan(0)
    expect(v).toBeLessThan(100)
  })
})

describe('MACD', () => {
  it('returns macd, signal, histogram', () => {
    const data = makeSampleData(30, 100)
    const result = calculateMACD(data, 12, 26, 9)
    expect(result.macd.length).toBe(30)
    expect(result.signal.length).toBe(30)
    expect(result.histogram.length).toBe(30)
  })
})

describe('ATR', () => {
  it('calculates average true range', () => {
    const data = makeSampleData(20, 100)
    const result = calculateATR(data, 14)
    expect(result.length).toBe(20)
    expect(last(result)).toBeGreaterThan(0)
  })
})

describe('CCI', () => {
  it('calculates commodity channel index', () => {
    const data = makeSampleData(25, 100)
    const result = calculateCCI(data, 20)
    expect(result.length).toBe(25)
  })
})

describe('Williams %R', () => {
  it('returns values between -100 and 0', () => {
    const data = makeSampleData(20, 100)
    const result = calculateWilliamsR(data, 14)
    expect(last(result)).toBeGreaterThanOrEqual(-100)
    expect(last(result)).toBeLessThanOrEqual(0)
  })
})

describe('MFI', () => {
  it('returns values between 0 and 100', () => {
    const data = makeSampleData(20, 100)
    const result = calculateMFI(data, 14)
    expect(last(result)).toBeGreaterThanOrEqual(0)
    expect(last(result)).toBeLessThanOrEqual(100)
  })
})

describe('ADX', () => {
  it('returns adx, plusDI, minusDI', () => {
    const data = makeSampleData(30, 100)
    const { adx, plusDI, minusDI } = calculateADX(data, 14)
    expect(adx.length).toBe(30)
    expect(plusDI.length).toBe(30)
    expect(minusDI.length).toBe(30)
  })
})

describe('OBV', () => {
  it('calculates on-balance volume', () => {
    const data = makeSampleData(10, 100)
    const result = calculateOBV(data)
    expect(result.length).toBe(10)
    expect(result[9]!).toBeGreaterThan(result[0]!)
  })
})

describe('KDJ', () => {
  it('returns K, D, J values', () => {
    const data = makeSampleData(20, 100)
    const { k, d, j } = calculateKDJ(data, 9, 3, 3)
    expect(k.length).toBe(20)
    expect(d.length).toBe(20)
    expect(j.length).toBe(20)
  })
})
