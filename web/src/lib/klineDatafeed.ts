/**
 * Custom KLineChart Pro Datafeed — connects to XiaoTianQuant Go backend
 *
 * Features:
 *  - Real Binance historical data via REST API
 *  - Live simulated price ticks via WebSocket (/ws)
 *  - WebSocket auto-reconnect with exponential backoff
 *  - Gap detection & backfill on reconnect (REST API fetches missing bars)
 *  - Deduplication: won't push bars already sent to subscribers
 */
import type { SymbolInfo, Period, Datafeed, DatafeedSubscribeCallback } from '@klinecharts/pro'
import type { KLineData } from 'klinecharts'
import { api } from './api'

/* ── Period → backend interval mapping ── */
const PERIOD_MAP: Record<string, string> = {
  '1min': '1m', '3min': '3m', '5min': '5m', '15min': '15m',
  '30min': '30m', '1hour': '1h', '4hour': '4h',
  '1day': '1d', '1week': '1w', '1month': '1M',
}

/* ── Period → duration in milliseconds (for gap detection) ── */
const PERIOD_MS: Record<string, number> = {
  '1minute': 60_000,
  '3minute': 180_000,
  '5minute': 300_000,
  '15minute': 900_000,
  '30minute': 1_800_000,
  '1hour': 3_600_000,
  '4hour': 14_400_000,
  '1day': 86_400_000,
  '1week': 604_800_000,
  '1month': 2_592_000_000,
}

/** Max bars to backfill after a disconnect (avoid overwhelming the chart) */
const MAX_BACKFILL_BARS = 200

function periodKey(p: Period): string {
  return `${p.multiplier}${p.timespan}`
}

function toInterval(p: Period): string {
  return PERIOD_MAP[`${p.multiplier}${p.timespan}`] || '1h'
}

function periodMs(p: Period): number {
  return PERIOD_MS[periodKey(p)] || 3_600_000
}

/* ── KLine transform: backend {time} → KLineData {timestamp} ── */
function toKLineData(raw: any): KLineData {
  return {
    timestamp: raw.time || raw.timestamp || 0,
    open: parseFloat(raw.open) || 0,
    high: parseFloat(raw.high) || 0,
    low: parseFloat(raw.low) || 0,
    close: parseFloat(raw.close) || 0,
    volume: parseFloat(raw.volume) || 0,
  }
}

/* ── WebSocket subscriptions ── */
interface SubEntry {
  symbol: SymbolInfo
  period: Period
  callbacks: Set<DatafeedSubscribeCallback>
  /** Timestamp (ms) of the most recent bar pushed to subscribers */
  lastBarTimestamp: number
}

const subscriptions = new Map<string, SubEntry>()

function wsSubKey(symbol: SymbolInfo, period: Period): string {
  return `${symbol.ticker}:${periodKey(period)}`
}

/* ── WebSocket connection state ── */
let ws: WebSocket | null = null
let wsReconnectTimer: ReturnType<typeof setTimeout> | null = null
let wsReconnectAttempts = 0
let wsEverConnected = false
let wsIntentionallyClosed = false

/** Resolve full WS URL */
function wsUrl(): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${window.location.host}/ws`
}

/** Exponential backoff delay: 1s, 2s, 4s, 8s, ... capped at 30s */
function backoffDelay(attempt: number): number {
  return Math.min(1000 * Math.pow(2, attempt), 30_000)
}

/* ── Gap detection & backfill ── */

/**
 * Check if a subscription has a data gap (disconnected long enough to miss bars).
 * Returns the timestamp range to fetch, or null if no gap.
 */
function detectGap(entry: SubEntry): { from: number; to: number } | null {
  const now = Date.now()
  const barMs = periodMs(entry.period)

  // No data ever pushed — can't backfill, let KLineChart handle initial load
  if (entry.lastBarTimestamp <= 0) return null

  // How many bars should have elapsed since the last one we pushed?
  const elapsedMs = now - entry.lastBarTimestamp
  const missedBars = Math.floor(elapsedMs / barMs)

  // Allow 1 bar of tolerance (the current forming bar)
  if (missedBars <= 1) return null

  // Cap backfill to avoid fetching too many bars
  const cappedBars = Math.min(missedBars, MAX_BACKFILL_BARS)

  // from = last known bar timestamp + 1 bar (the first missing bar)
  // to = now
  const from = entry.lastBarTimestamp + barMs
  const to = now

  console.log(
    `[KLineDatafeed] Gap detected for ${entry.symbol.ticker} ${periodKey(entry.period)}: ` +
    `${missedBars} bars missed (capped to ${cappedBars}), fetching ${new Date(from).toISOString()} → ${new Date(to).toISOString()}`
  )

  return { from, to }
}

/**
 * Fetch missing bars from REST API and push them to subscribers.
 * Called after WebSocket reconnection.
 */
async function backfillGap(entry: SubEntry): Promise<void> {
  const gap = detectGap(entry)
  if (!gap) return

  try {
    const interval = toInterval(entry.period)
    const limit = Math.min(
      Math.ceil((gap.to - gap.from) / periodMs(entry.period)) + 10,
      MAX_BACKFILL_BARS
    )

    const data: any = await api.get('/market/klines', {
      params: {
        symbol: entry.symbol.ticker,
        interval,
        limit,
        from: gap.from,
        to: gap.to,
      },
    })

    const klines: any[] = data?.klines || data || []
    if (!Array.isArray(klines) || klines.length === 0) {
      console.log('[KLineDatafeed] Backfill: no bars returned from REST API')
      return
    }

    // Convert and filter: only push bars with timestamp > last known
    const newBars = klines
      .map(toKLineData)
      .filter((bar) => bar.timestamp > entry.lastBarTimestamp)
      .sort((a, b) => a.timestamp - b.timestamp)

    if (newBars.length === 0) {
      console.log('[KLineDatafeed] Backfill: all returned bars already known')
      return
    }

    console.log(
      `[KLineDatafeed] Backfill: pushing ${newBars.length} bars for ${entry.symbol.ticker} ` +
      `(${new Date(newBars[0].timestamp).toISOString()} → ${new Date(newBars[newBars.length - 1].timestamp).toISOString()})`
    )

    // Push bars in chronological order
    for (const bar of newBars) {
      entry.lastBarTimestamp = Math.max(entry.lastBarTimestamp, bar.timestamp)
      entry.callbacks.forEach((cb) => {
        try { cb(bar) } catch {}
      })
    }
  } catch (err) {
    console.error('[KLineDatafeed] Backfill failed:', err)
  }
}

/** Run backfill for all active subscriptions (called after WS reconnect) */
async function backfillAll(): Promise<void> {
  const entries = Array.from(subscriptions.values())
  if (entries.length === 0) return

  console.log(`[KLineDatafeed] Running backfill for ${entries.length} subscription(s)...`)
  // Sequential to avoid flooding the backend
  for (const entry of entries) {
    await backfillGap(entry)
  }
  console.log('[KLineDatafeed] Backfill complete')
}

/* ── WebSocket lifecycle ── */

function ensureWS() {
  if (ws?.readyState === WebSocket.OPEN) return
  if (ws?.readyState === WebSocket.CONNECTING) return
  if (wsIntentionallyClosed) return

  try {
    ws = new WebSocket(wsUrl())

    ws.onopen = () => {
      console.log('[KLineDatafeed] WS connected')
      wsReconnectAttempts = 0

      if (wsEverConnected) {
        // This is a reconnection — backfill missing data
        backfillAll()
      }
      wsEverConnected = true
    }

    ws.onclose = (event) => {
      console.log(`[KLineDatafeed] WS closed (code=${event.code}, reason=${event.reason})`)
      ws = null

      if (wsIntentionallyClosed) return

      wsReconnectAttempts++
      const delay = backoffDelay(wsReconnectAttempts)
      console.log(
        `[KLineDatafeed] Reconnecting in ${delay}ms (attempt ${wsReconnectAttempts})`
      )
      wsReconnectTimer = setTimeout(ensureWS, delay)
    }

    ws.onerror = () => {
      // onclose will be called after onerror, handle there
    }

    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)

        // Only handle price events (live ticks)
        if (data.type === 'price' && data.symbol) {
          const now = Date.now()

          // Notify all subscribers whose key starts with this symbol
          subscriptions.forEach((entry, key) => {
            if (!key.startsWith(data.symbol + ':')) return

            const kline: KLineData = {
              timestamp: now,
              open: data.data?.last || data.data?.price || 0,
              high: data.data?.high || 0,
              low: data.data?.low || 0,
              close: data.data?.last || data.data?.price || 0,
              volume: data.data?.volume || 0,
            }

            // Update last known timestamp for gap detection
            entry.lastBarTimestamp = Math.max(entry.lastBarTimestamp, kline.timestamp)

            entry.callbacks.forEach((cb) => {
              try { cb(kline) } catch {}
            })
          })
        }
      } catch { /* ignore malformed messages */ }
    }
  } catch (err) {
    console.error('[KLineDatafeed] WS creation failed:', err)
    wsReconnectAttempts++
    wsReconnectTimer = setTimeout(ensureWS, backoffDelay(wsReconnectAttempts))
  }
}

/** Graceful shutdown — call on page unmount if needed */
export function disconnectDatafeed(): void {
  wsIntentionallyClosed = true
  if (wsReconnectTimer) {
    clearTimeout(wsReconnectTimer)
    wsReconnectTimer = null
  }
  if (ws) {
    try { ws.close(1000, 'client disconnect') } catch {}
    ws = null
  }
  subscriptions.clear()
  wsEverConnected = false
  wsReconnectAttempts = 0
}

/* ── Exported Datafeed ── */
export function createBackendDatafeed(): Datafeed {
  return {
    async searchSymbols(search?: string): Promise<SymbolInfo[]> {
      try {
        const results: string[] = await api.get(`/symbols/search?q=${search || ''}`)
        return (results || []).slice(0, 20).map((s) => ({
          ticker: s,
          name: s,
          shortName: s,
          market: 'crypto',
          exchange: s.includes('USDT') ? 'BINANCE' : 'UNKNOWN',
        }))
      } catch {
        return []
      }
    },

    async getHistoryKLineData(
      symbol: SymbolInfo,
      period: Period,
      from: number,
      to: number,
    ): Promise<KLineData[]> {
      try {
        const interval = toInterval(period)
        // KLineChart passes timestamp in seconds, backend expects milliseconds
        const fromMs = from * 1000
        const toMs = to * 1000
        const limit = Math.min(Math.ceil((toMs - fromMs) / 3600000) + 200, 1500)

        const data: any = await api.get('/market/klines', {
          params: { symbol: symbol.ticker, interval, limit, from: fromMs, to: toMs },
        })

        const klines = data?.klines || data || []
        return (Array.isArray(klines) ? klines : []).map(toKLineData)
      } catch {
        return []
      }
    },

    subscribe(symbol: SymbolInfo, period: Period, callback: DatafeedSubscribeCallback): void {
      const key = wsSubKey(symbol, period)

      let entry = subscriptions.get(key)
      if (!entry) {
        entry = {
          symbol,
          period,
          callbacks: new Set(),
          lastBarTimestamp: 0,
        }
        subscriptions.set(key, entry)
      }

      entry.callbacks.add(callback)

      // Ensure WS is connected (starts connection if first subscription)
      ensureWS()
    },

    unsubscribe(symbol: SymbolInfo, period: Period): void {
      const key = wsSubKey(symbol, period)
      const entry = subscriptions.get(key)
      if (!entry) return

      entry.callbacks.clear()
      subscriptions.delete(key)

      // If no more subscriptions, close the WebSocket to save resources
      if (subscriptions.size === 0) {
        disconnectDatafeed()
        // Reset flag so it can reconnect if subscribe is called again
        wsIntentionallyClosed = false
      }
    },
  }
}
