export interface User {
  id: string
  username: string
  email?: string
  role: 'admin' | 'user'
}

export interface Position {
  id: string
  symbol: string
  side: 'LONG' | 'SHORT'
  leverage: number
  entry_price: number
  mark_price: number
  liquidation_price?: number
  margin: number
  unrealized_pnl: number
  quantity: number
}

export interface Order {
  id: string
  symbol: string
  side: 'BUY' | 'SELL'
  type: 'LIMIT' | 'MARKET' | 'STOP_LIMIT'
  price: number
  quantity: number
  status: 'NEW' | 'FILLED' | 'CANCELLED' | 'PARTIALLY_FILLED'
  created_at: string
}

export interface StrategyConfig {
  id: string
  name: string
  category: 'contract' | 'spot' | 'grid' | 'freqtrade'
  strategy_type: string
  coin: string
  direction: 'long' | 'short' | 'dual'
  leverage: number
  status: 'draft' | 'running' | 'paused' | 'stopped'
  config_json: string
  created_at: number
  updated_at: number
}

export interface PortfolioSummary {
  total_equity: number
  total_pnl: number
  total_pnl_pct: number
  exchanges: {
    name: string
    exchange: string
    balance: number
    connected: boolean
  }[]
}

export interface DashboardSummary {
  total_equity: number
  total_pnl: number
  equity_curve: { time: number; value: number }[]
  ai_agents: {
    name: string
    status: string
    detail: string
  }[]
  ai_logs: { time: string; message: string }[]
  calendar: Record<string, number>
}

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

export type WSEvent = WSTick | WSOrderBook | { type: string; [key: string]: any }
