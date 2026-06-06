import type { AIAnalysisResult } from '@/types'

export type HeatmapType = 'us_stocks' | 'hk_stocks' | 'crypto' | 'commodities' | 'sectors' | 'forex'

export interface MarketIndex {
  flag: string
  symbol: string
  price: number
  change: number
}

export interface HeatmapItem {
  name: string
  name_cn?: string
  name_en?: string
  fullName?: string
  price?: number
  value: number
}

export interface CalendarEvent {
  id: string
  date: string
  time?: string
  country: string
  name: string
  name_en?: string
  importance: 'high' | 'medium' | 'low'
  actual?: string | number
  forecast?: string | number
  actual_impact?: 'bullish' | 'bearish' | 'neutral'
  expected_impact?: 'bullish' | 'bearish' | 'neutral'
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
