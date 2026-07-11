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
