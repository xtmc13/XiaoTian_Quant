export interface HealthResponse {
  status: string
  uptime: string
  version: string
  log_level: string
}

export interface ComponentHealthResponse {
  name: string
  status: 'healthy' | 'degraded' | 'unhealthy' | 'unknown'
  message?: string
  last_check?: string
}

export interface StatusResponse {
  status: string
  timestamp?: number
  [key: string]: unknown
}
