/**
 * 外部数据生态类型（对标 QuantDinger data_providers）。
 * 与 gateway/internal/dataprovider 的返回结构一一对应。
 */

/** 每个数据块的统一包装：ok | stale | not_configured | unavailable | refreshing */
export interface ProviderResult<T> {
  source: string
  status: 'ok' | 'stale' | 'not_configured' | 'unavailable' | 'refreshing' | 'unknown_source'
  data?: T
  fetched_at?: number
  age_sec?: number
  error?: string
}

export interface FearGreedPoint {
  value: number
  classification: string
  timestamp: number
}

export interface FearGreedData {
  value: number
  classification: string
  updated_at: number
  history?: FearGreedPoint[]
}

export interface FundingRateEntry {
  exchange: string
  symbol: string
  rate: number
  rate_pct: number
  time?: number
}

export interface LongShortRatio {
  long_pct: number
  short_pct: number
  ratio: number
  time?: number
}

export interface LiquidationStats {
  total_usd: number
  long_usd: number
  short_usd: number
  time?: number
}

export interface CoinglassData {
  symbol: string
  funding: FundingRateEntry[]
  long_short?: LongShortRatio
  liquidations?: LiquidationStats
  updated_at: number
}

export interface SentimentResponse {
  fear_greed: ProviderResult<FearGreedData>
  derivatives: ProviderResult<CoinglassData>
}

export interface MacroObs {
  date: string
  value: number
}

export interface MacroSeries {
  id: string
  name: string
  name_en: string
  unit: string
  observations: MacroObs[]
}

export interface MacroData {
  series: MacroSeries[]
  updated_at: number
}

export interface NewsItem {
  id: string
  title: string
  url: string
  source: string
  published_at: number
  categories: string[]
  summary?: string
}

export interface NewsData {
  items: NewsItem[]
  updated_at: number
}

export interface HeatmapEntry {
  symbol: string
  base: string
  price: number
  change_pct_24h: number
  volume_24h: number
  weight: number
}

export interface HeatmapData {
  entries: HeatmapEntry[]
  updated_at: number
}

export interface EconCalendarEvent {
  id: string
  name: string
  currency: string
  date: string
  time: string
  impact: 'high' | 'medium' | 'low' | 'holiday' | string
  forecast: string
  previous: string
}

export interface CalendarData {
  events: EconCalendarEvent[]
  updated_at: number
}

export interface SourceHealth {
  name: string
  description: string
  configured: boolean
  requires_key: boolean
  state: 'ok' | 'stale' | 'no_data' | 'not_configured'
  circuit: 'closed' | 'open' | 'half_open'
  failures: number
  last_success?: number
  last_error?: string
  ttl_sec: number
}
