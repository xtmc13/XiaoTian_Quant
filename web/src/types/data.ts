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
  from?: number
  to?: number
  count?: number
  /** 后端 model.Bar 口径：time(ms)/open/high/low/close/volume */
  bars: { time: number; open: number; high: number; low: number; close: number; volume: number }[]
}
