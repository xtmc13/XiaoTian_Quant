export interface WSTick {
  type: 'tick'
  symbol: string
  price: number
  ts: number
}

export interface WSOrderBook {
  type: 'orderbook'
  symbol: string
  bids: [number, number][]
  asks: [number, number][]
}

export type WSEvent = WSTick | WSOrderBook | { type: string; [key: string]: unknown }

export interface TickerSnapshot {
  symbol: string
  price: number
  change_24h?: number
  change_pct_24h?: number
  volume_24h?: number
  high_24h?: number
  low_24h?: number
}

export interface MarketIndex {
  flag: string
  symbol: string
  name?: string
  price: number
  change: number
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

export interface IndicesSnapshot {
  indices: MarketIndex[]
  status: string
  source?: string
}

export interface SentimentSnapshot {
  fear_greed: number
  fear_greed_label?: string
  vix: number
  vix_change?: number
  dxy: number
  dxy_change?: number
  status: string
  source?: string
}

export interface CalendarSnapshot {
  events: CalendarEvent[]
  status: string
  source?: string
}

export type MarketSnapshotResponse = TickerSnapshot | IndicesSnapshot | SentimentSnapshot | CalendarSnapshot

export interface KlineBar {
  timestamp: number
  open: number
  high: number
  low: number
  close: number
  volume: number
}

export interface OrderBook {
  symbol: string
  bids: [number, number][]
  asks: [number, number][]
  ts?: number
}

export interface Trade {
  id: string
  symbol: string
  price: number
  quantity: number
  side: 'buy' | 'sell'
  time: number
  [key: string]: unknown
}
