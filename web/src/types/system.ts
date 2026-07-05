import type { Trade } from './market'

export interface NotificationItem {
  id: number
  title: string
  message: string
  content?: string
  level?: string
  category?: string
  type: 'info' | 'success' | 'warning' | 'error'
  read: boolean
  created_at: number
  link?: string
}

export interface BillingPlan {
  id: string
  name: string
  name_en: string
  price: number
  credits: number | string
  period_days: number
}

export interface ChainInfo {
  chain: string
  address: string
  memo: string
}

export interface BillingOrder {
  order_id: string
  status: string
  plan_id: string
  chain: string
  tx_hash?: string
  created_at: number
}

export interface RawConfig {
  [key: string]: unknown
}

export interface ExchangeConfiguredStatus {
  enabled: boolean
  has_credentials: boolean
  testnet: boolean
  futures: boolean
}

export interface ExchangeTestResult {
  success: boolean
  message?: string
  status?: string
  detail?: string
  balance?: number
}

export interface ExchangeSaveResult {
  success: boolean
  id?: string
}

import type { AIModel } from './ai'

export type AgentModel = AIModel

export interface DefaultSettings {
  [key: string]: unknown
}

export interface ExchangeSettings {
  id: string
  name: string
  api_key?: string
  api_secret?: string
  secret?: string
  passphrase?: string
  testnet?: boolean
  enabled: boolean
  [key: string]: unknown
}

export interface AgentToken {
  id: string
  name: string
  token: string
  scopes: string[]
  created_at: string
  expires_at?: string
  last_used?: string
  [key: string]: unknown
}

export interface NotifyRoute {
  id: string
  channel: string
  enabled: boolean
  events?: string[]
  config?: Record<string, unknown>
  [key: string]: unknown
}

export interface AdminUser {
  id: string
  username: string
  nickname?: string
  email: string
  role: string
  is_active?: number
  created_at: string
  [key: string]: unknown
}

export interface AdminStats {
  total_users: number
  active_users: number
  admin_count?: number
  user_count?: number
  total_orders?: number
  total_revenue?: number
  daily_active?: number
  monthly_active?: number
  system?: {
    goroutines?: number
    heap_alloc_mb?: number
    uptime_seconds?: number
    go_version?: string
  }
  trading?: {
    total_orders?: number
    pending_orders?: number
    total_trades?: number
    active_strategies?: number
  }
  [key: string]: unknown
}

export interface AdminAuditLog {
  id: number
  user_id?: number
  actor?: string
  action: string
  details?: string
  detail?: string
  created_at: number
  [key: string]: unknown
}

export interface ExchangeStatusItem {
  id: string
  name: string
  connected: boolean
  enabled: boolean
  status: string
  last_error?: string
}

export interface ExchangeStatusResponse {
  exchanges: ExchangeStatusItem[]
  default_exchange_id?: string
}

export interface RustOrderBookLevel {
  price: number
  quantity: number
}

export interface RustOrderBookSnapshot {
  symbol: string
  timestamp: number
  bids: RustOrderBookLevel[]
  asks: RustOrderBookLevel[]
}

export interface RustTradeResponse {
  symbol: string
  trades: Trade[]
}

export interface RustEngineStatsResponse {
  symbol?: string
  tps?: number
  total_orders?: number
  total_trades?: number
  latency_ms?: number
  engine_count?: number
}
