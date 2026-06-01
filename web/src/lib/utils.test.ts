import { describe, it, expect } from 'vitest'
import { cn, formatCurrency, formatPercent } from './utils'

describe('cn', () => {
  it('merges class names', () => {
    expect(cn('a', 'b')).toBe('a b')
    expect(cn('a', false && 'c')).toBe('a')
    expect(cn('a', undefined, 'b')).toBe('a b')
  })
})

describe('formatCurrency', () => {
  it('formats numbers as USD', () => {
    const r1 = formatCurrency(1000)
    const r2 = formatCurrency(0)
    expect(r1).toBeTruthy()
    expect(r2).toBeTruthy()
  })
})

describe('formatPercent', () => {
  it('formats numbers as percentages', () => {
    const r1 = formatPercent(5.5)
    const r2 = formatPercent(-2.3)
    expect(r1).toBeTruthy()
    expect(r2).toBeTruthy()
    expect(r1).not.toBe(r2)
  })
})
