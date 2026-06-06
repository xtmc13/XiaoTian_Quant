/**
 * Shared trading helpers — used by SpotTrading and ContractTrading pages.
 * DRY extraction of formatters, StatusTag, and KLineChartPro utilities.
 */

import { cn } from '@/lib/utils'

/* ── Interval parsing ─────────────────────────────────────────────── */

export function parseInterval(i: string) {
  const num = parseInt(i) || 1
  const unit = i.replace(/[0-9]/g, '')
  const map: Record<string, string> = { m: 'minute', h: 'hour', d: 'day', w: 'week' }
  return { multiplier: num, timespan: map[unit] || 'hour' }
}

/* ── Formatters ────────────────────────────────────────────────────── */

export function formatPrice(n?: number | string, digits = 2) {
  if (n == null || n === '') return '--'
  const val = typeof n === 'string' ? parseFloat(n) : n
  if (Number.isNaN(val)) return '--'
  return val.toFixed(digits)
}

export function formatTime(ts: number | string) {
  const d = new Date(ts)
  return d.toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

export function formatDateTime(ts: number | string) {
  const d = new Date(ts)
  return d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

export function formatVolume(n?: number | string) {
  if (!n) return '--'
  const val = typeof n === 'string' ? parseFloat(n) : n
  if (val >= 1e9) return (val / 1e9).toFixed(2) + 'B'
  if (val >= 1e6) return (val / 1e6).toFixed(2) + 'M'
  if (val >= 1e3) return (val / 1e3).toFixed(2) + 'K'
  return val.toFixed(2)
}

/* ── StatusTag ─────────────────────────────────────────────────────── */

export function StatusTag({ status }: { status: string }) {
  const cfg: Record<string, { cls: string; label: string }> = {
    PENDING: { cls: 'bg-yellow-500/10 text-yellow-500', label: '待成交' },
    OPEN: { cls: 'bg-quant-gold/10 text-quant-gold', label: '委托中' },
    PARTIALLY_FILLED: { cls: 'bg-quant-orange/10 text-quant-orange', label: '部分成交' },
    FILLED: { cls: 'bg-[#0ECB81]/10 text-[#0ECB81]', label: '已成交' },
    CANCELLED: { cls: 'bg-quant-border/40 text-muted-foreground', label: '已取消' },
    REJECTED: { cls: 'bg-red-500/10 text-red-500', label: '已拒绝' },
    EXPIRED: { cls: 'bg-quant-border/40 text-muted-foreground', label: '已过期' },
  }
  const c = cfg[status] || { cls: 'bg-quant-border/40 text-muted-foreground', label: status }
  return <span className={cn('inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-medium', c.cls)}>{c.label}</span>
}

/* ── KLineChartPro internal API access ─────────────────────────────── */

export interface ChartApi {
  scrollToRealTime: () => void
  setBarSpace: (n: number) => void
  updateData: (bar: unknown) => void
  [key: string]: (...args: any[]) => any
}

export function getChartApi(chart: unknown): ChartApi | null {
  const api = (chart as { _chartApi?: unknown })._chartApi
  return api ? (api as ChartApi) : null
}

/* ── Watchlist ─────────────────────────────────────────────────────── */

export const SPOT_WATCHLIST = [
  'BTCUSDT', 'ETHUSDT', 'BNBUSDT', 'SOLUSDT', 'ADAUSDT', 'DOGEUSDT',
  'XRPUSDT', 'AVAXUSDT', 'DOTUSDT', 'LINKUSDT', 'MATICUSDT', 'LTCUSDT',
  'UNIUSDT', 'ATOMUSDT', 'ETCUSDT',
]

export const CONTRACT_LEVERAGES = [1, 2, 3, 5, 10, 20, 50, 75, 100, 125]
