export interface Order {
  id: string
  symbol: string
  side: 'BUY' | 'SELL'
  type: 'LIMIT' | 'MARKET' | 'STOP_LIMIT'
  price: number
  quantity: number
  status: 'NEW' | 'FILLED' | 'CANCELLED' | 'PARTIALLY_FILLED'
  created_at: string

  // Contract fields
  market_type?: 'spot' | 'swap' | 'margin'
  position_side?: 'LONG' | 'SHORT'
  leverage?: number
  margin_mode?: 'cross' | 'isolated'
  tp_price?: number
  sl_price?: number
  close_position?: boolean

  // History / extended fields used by components
  updated_at?: string
  avg_price?: number
  filled_quantity?: number
  realized_pnl?: number
  order_id?: string
  order_type?: string
}

export interface OCOOrder {
  id: string
  symbol: string
  side: 'BUY' | 'SELL' | 'buy' | 'sell'
  quantity: number
  price: number
  stop_price: number
  limit_price?: number
  status: string
  created_at: string
  [key: string]: unknown
}

export interface BracketOrder {
  id: string
  symbol: string
  side: 'BUY' | 'SELL' | 'buy' | 'sell'
  quantity: number
  entry_price: number
  take_profit_price: number
  stop_loss_price: number
  status: string
  created_at: string
}

export interface IcebergOrder {
  id: string
  symbol: string
  side: 'BUY' | 'SELL' | 'buy' | 'sell'
  total_quantity: number
  visible_quantity: number
  price: number
  filled_quantity: number
  slice_size?: number
  executed_quantity?: number
  status: string
  created_at: string
  [key: string]: unknown
}

// ── 阶梯智能单（Ladder Smart Orders）──

export type LadderLegStatus = 'pending' | 'open' | 'partial' | 'filled' | 'cancelled' | 'waiting'

export type LadderStatus =
  | 'active'
  | 'stopping'
  | 'completed'
  | 'cancelled'
  | 'stopped'
  | 'flattened'
  | 'failed'

export interface LadderEntry {
  price: number
  qty: number
  amount_usdt?: number
  order_id?: string
  filled: number
  avg_price?: number
  status: LadderLegStatus
}

export interface LadderTarget {
  price: number
  close_pct: number
  order_id?: string
  assigned: number
  filled: number
  status: LadderLegStatus
}

export interface LadderOrder {
  id: string
  user_id: number
  symbol: string
  side: 'BUY' | 'SELL'
  exchange: string
  entries: LadderEntry[]
  targets: LadderTarget[]
  total_qty: number
  stop_loss: number
  breakeven_after_target: number
  trailing_step_pct: number
  status: LadderStatus
  current_sl: number
  breakeven_armed: boolean
  filled_qty: number
  closed_qty: number
  avg_entry: number
  stop_reason?: string
  fail_reason?: string
  created_at: number
  updated_at: number
}

export interface LadderCreateRequest {
  symbol: string
  side: 'BUY' | 'SELL'
  exchange?: string
  entries: { price: number; qty?: number; amount_usdt?: number }[]
  targets: { price: number; close_pct: number }[]
  stop_loss?: number
  breakeven_after_target?: number
  trailing_step_pct?: number
}

export interface LadderAmendRequest {
  entries?: { index: number; price: number }[]
  targets?: { index: number; price: number }[]
  stop_loss?: number
  breakeven_after_target?: number
  trailing_step_pct?: number
}
