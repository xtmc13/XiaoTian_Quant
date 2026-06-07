import type { AIAnalysisResult, MarketIndex, CalendarEvent } from '@/types'

// Re-export for backward compatibility
export type { MarketIndex, CalendarEvent }

export type HeatmapType = 'us_stocks' | 'hk_stocks' | 'crypto' | 'commodities' | 'sectors' | 'forex'

export interface HeatmapItem {
  name: string
  name_cn?: string
  name_en?: string
  fullName?: string
  price?: number
  value: number
}

export interface WatchlistItem {
  market: string
  symbol: string
  name?: string
  price?: number
  change?: number
  changePercent?: number
}

export interface WatchlistPrice {
  price: number
  change: number
}

export interface PositionSummary {
  quantity: number
  avgEntry: number
  pnl: number
  pnlPercent: number
  monitorCount?: number
  activeMonitorCount?: number
  nextRunAtText?: string
}

export interface AnalysisHistoryItem {
  symbol: string
  result: AIAnalysisResult
  time: number
}
