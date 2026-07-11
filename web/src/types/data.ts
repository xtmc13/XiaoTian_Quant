import type { KlineBar } from './market'

export interface DataCoverageSymbol {
  symbol: string
  intervals: string[]
  from?: number
  to?: number
}

export interface DataCoverageResponse {
  symbols: DataCoverageSymbol[]
  total_symbols: number
}

export interface DataInfoResponse {
  symbol: string
  interval: string
  count: number
  from?: number
  to?: number
  size_bytes?: number
}

export interface DownloadConfig {
  symbol: string
  interval: string
  from: number
  to: number
  exchange?: string
}

export interface DownloadJobStatus {
  job_id: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  symbol: string
  interval: string
  progress_pct: number
  downloaded: number
  total: number
  message?: string
  created_at: number
  completed_at?: number
}

export interface BarDataResponse {
  symbol: string
  interval: string
  bars: KlineBar[]
}
