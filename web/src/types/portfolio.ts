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

export interface PortfolioSummary {
  total_equity: number
  total_pnl: number
  total_pnl_pct: number
  spot_balance: number
  futures_balance: number
  futures_unrealized_pnl: number
  futures_wallet_balance: number
  funding_balance: number
  earn_balance: number
  margin_used?: number
  available_balance?: number
  drawdown_pct?: number
  position_count?: number
  other_exchanges: Record<string, number>
  usd_cny_rate: number
  conversion_rate: number
  preferred_currency: string
  exchanges: {
    name: string
    exchange: string
    balance: number
    connected: boolean
    enabled: boolean
    configured: boolean
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
  win_rate?: number
  profit_factor?: number
  max_drawdown?: number
  total_trades?: number
}

export interface Balance {
  asset: string
  free: number
  locked: number
  total: number
  [key: string]: unknown
}

export interface PortfolioPosition {
  id?: string
  symbol: string
  quantity: number
  avg_entry_price: number
  current_price?: number
  unrealized_pnl: number
  realized_pnl?: number
  side?: 'LONG' | 'SHORT'
  margin?: number
  liquidation_price?: number
  leverage?: number
  // Component-local aliases used by contract trading pages
  entryPrice?: number
  entry_price?: number
  openPrice?: number
  avgPrice?: number
  amount?: number
  positionMargin?: number
  liquidation?: number
}

export interface EquitySnapshot {
  timestamp: number
  total_equity: number
  drawdown?: number
  [key: string]: unknown
}

export interface CalendarMonth {
  month_key: string
  year: number
  month: number
  days_in_month?: number
  first_weekday?: number
  days: Record<string, number>
  total: number
  win_days: number
  lose_days: number
}
