export interface ArbitrageConfig {
  symbol: string
  symbols?: string[]
  min_spread_pct: number
  order_size: number
  max_positions: number
  fee_a: number
  fee_b: number
  poll_interval: number // backend stores nanoseconds; UI uses seconds
  auto_execute: boolean
  dry_run: boolean
  adaptive_qty_enabled: boolean
  max_slippage_pct: number
  min_order_qty: number
  min_order_value: number
}

export interface ArbitrageStatus {
  running: boolean
  started_at?: string
  last_scan_at?: string
  scan_count: number
  opportunity_count: number
  stats?: Record<string, unknown>
  [key: string]: unknown
}

export interface ArbitrageOpportunity {
  symbol: string
  buy_exchange: string
  sell_exchange: string
  buy_price: number
  sell_price: number
  spread_pct: number
  spread_abs?: number
  executable_buy_price?: number
  executable_sell_price?: number
  buy_depth_qty?: number
  sell_depth_qty?: number
  slippage_buy_pct?: number
  slippage_sell_pct?: number
  max_executable_qty?: number
  adjusted_qty?: number
  viable?: boolean
  timestamp: number
  [key: string]: unknown
}

export interface ArbitragePosition {
  id: string
  symbol: string
  buy_exchange: string
  sell_exchange: string
  buy_price: number
  sell_price: number
  quantity: number
  net_profit: number
  status: string
  opened_at: number
  closed_at?: number
  [key: string]: unknown
}

export interface ArbitrageHistoryItem {
  id: string
  symbol: string
  buy_exchange: string
  sell_exchange: string
  buy_price: number
  sell_price: number
  quantity: number
  net_profit: number
  status: string
  opened_at: number
  closed_at?: number
  [key: string]: unknown
}

export interface ArbitragePerformance {
  total_pnl: number
  total_trades: number
  win_trades: number
  loss_trades: number
  win_rate: number
  avg_pnl: number
  max_win: number
  max_loss: number
  total_fees: number
  equity_curve: { time: number; value: number }[]
  daily_pnl: { time: number; value: number }[]
}

export interface ArbitrageExchange {
  name: string
  api_key?: string
  secret?: string
  passphrase?: string
  testnet?: boolean
  enabled?: boolean
  connected?: boolean
  latency_ms?: number
  last_error?: string
}

export interface TriangularLeg {
  symbol: string
  side: 'BUY' | 'SELL'
  order_type: string
  price: number
  executable_price: number
  quantity: number
  filled_qty: number
  order_id: string
  status: string
  fee: number
  slippage_pct: number
}

export interface TriangularConfig {
  exchange: string
  symbols: string[]
  quote_asset: string
  min_profit_pct: number
  order_size: number
  max_positions: number
  fee_rate: number
  auto_execute: boolean
  dry_run: boolean
  adaptive_qty_enabled: boolean
  max_slippage_pct: number
  min_order_qty: number
  execution_mode: string
  max_execution_ms: number
}

export interface TriangularOpportunity {
  id: string
  exchange: string
  cycle: string[]
  legs: TriangularLeg[]
  start_asset: string
  start_qty: number
  end_qty: number
  gross_profit: number
  net_profit: number
  net_profit_pct: number
  total_fees: number
  viable: boolean
  timestamp: number
}

export interface TriangularTrade {
  id: string
  exchange: string
  cycle: string[]
  legs: TriangularLeg[]
  start_asset: string
  start_qty: number
  end_qty: number
  gross_profit: number
  net_profit: number
  total_fees: number
  status: string
  opened_at: number
  closed_at?: number
}

export interface TriangularPerformance {
  total_pnl: number
  total_trades: number
  win_trades: number
  loss_trades: number
  win_rate: number
  avg_pnl: number
  max_win: number
  max_loss: number
  total_fees: number
  equity_curve: { time: number; value: number }[]
  daily_pnl: { time: number; value: number }[]
}
