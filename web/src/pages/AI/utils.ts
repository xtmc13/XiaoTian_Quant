import type { CalendarEvent, HeatmapItem, HeatmapType } from './types'

export function formatNum(n: number | undefined | null, digits = 2): string {
  if (n === undefined || n === null || Number.isNaN(n)) return '--'
  return Number(n).toFixed(digits)
}

export function formatPrice(price?: number | null): string {
  if (price === undefined || price === null || Number.isNaN(price)) return '--'
  if (price >= 10000) return (price / 1000).toFixed(1) + 'K'
  if (price >= 1000) return price.toFixed(0)
  return price.toFixed(2)
}

export function formatHeatmapPrice(price: number, type: HeatmapType): string {
  const prefix = type === 'hk_stocks' ? 'HK$' : '$'
  if (price >= 10000) return prefix + (price / 1000).toFixed(1) + 'K'
  if (price >= 1000) return prefix + price.toFixed(0)
  if (price >= 1) return prefix + price.toFixed(2)
  return prefix + price.toFixed(4)
}

export function getFearGreedClass(val?: number | null): string {
  if (val === undefined || val === null || Number.isNaN(val)) return ''
  if (val <= 25) return 'extreme-fear'
  if (val <= 45) return 'fear'
  if (val <= 55) return 'neutral'
  if (val <= 75) return 'greed'
  return 'extreme-greed'
}

export function getVixClass(val?: number | null): string {
  if (val === undefined || val === null || Number.isNaN(val)) return ''
  if (val < 15) return 'low'
  if (val < 25) return 'medium'
  return 'high'
}

export function getHeatmapStyle(value: number, isDark: boolean) {
  const v = parseFloat(String(value)) || 0
  const intensity = Math.min(Math.abs(v) / 5, 1)
  if (v >= 0) {
    const color = isDark ? (v > 2 ? '#fff' : '#4ade80') : v > 2 ? '#fff' : '#166534'
    return { background: `rgba(34, 197, 94, ${0.15 + intensity * 0.6})`, color }
  }
  const color = isDark ? (v < -2 ? '#fff' : '#f87171') : v < -2 ? '#fff' : '#991b1b'
  return { background: `rgba(239, 68, 68, ${0.15 + intensity * 0.6})`, color }
}

export function getImpactClass(evt: CalendarEvent): 'bullish' | 'bearish' | 'neutral' {
  return evt.actual_impact || evt.expected_impact || 'neutral'
}

export function formatCalendarDate(dateStr?: string): string {
  if (!dateStr) return ''
  try {
    const date = new Date(dateStr)
    const today = new Date()
    const tomorrow = new Date(today)
    tomorrow.setDate(tomorrow.getDate() + 1)
    if (date.toDateString() === today.toDateString()) return '今天'
    if (date.toDateString() === tomorrow.toDateString()) return '明天'
    return `${date.getMonth() + 1}/${date.getDate()}`
  } catch {
    return dateStr
  }
}

export function getHeatmapName(item: HeatmapItem, type: HeatmapType, isZh: boolean): string {
  if (type === 'hk_stocks') return isZh ? item.name_cn || item.fullName || item.name : item.name || item.fullName || ''
  if (type === 'us_stocks') return item.name || item.fullName || ''
  if (type === 'sectors' || type === 'commodities' || type === 'forex') {
    return isZh ? item.name_cn || item.name : item.name_en || item.name
  }
  return item.name
}
