/* ── Trading time intervals ──────────────────────────────────────── */

/** Standard K-line intervals for trading charts */
export const TRADING_INTERVALS = ['1m', '5m', '15m', '30m', '1h', '4h', '1d', '1w'] as const

/** Interval options with Chinese labels */
export const INTERVAL_OPTIONS = [
  { value: '1m', label: '1分钟' },
  { value: '5m', label: '5分钟' },
  { value: '15m', label: '15分钟' },
  { value: '30m', label: '30分钟' },
  { value: '1h', label: '1小时' },
  { value: '4h', label: '4小时' },
  { value: '1d', label: '日线' },
  { value: '1w', label: '周线' },
] as const

/** Hyperopt / ML model intervals (coarser granularity) */
export const MODEL_INTERVALS = [
  { value: '1m', label: '1分钟' },
  { value: '5m', label: '5分钟' },
  { value: '15m', label: '15分钟' },
  { value: '30m', label: '30分钟' },
  { value: '1h', label: '1小时' },
  { value: '4h', label: '4小时' },
  { value: '1d', label: '日线' },
] as const
