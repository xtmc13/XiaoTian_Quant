/**
 * Custom KLineChart Pro Datafeed — connects to XiaoTianQuant Go backend
 *
 * Features:
 *  - Real Binance historical data via REST API
 *  - Live price ticks fed from the app's primary WebSocket (no duplicate WS)
 *  - Running bar aggregation with throttle, updates chart via _chartApi.updateData
 *  - Gap detection & backfill via REST API (call on reconnect)
 */
import type { SymbolInfo, Period, Datafeed, DatafeedSubscribeCallback } from '@klinecharts/pro'
import type { KLineData } from 'klinecharts'
import { api } from './api'

/* ── Period → backend interval mapping ── */
const PERIOD_MAP: Record<string, string> = {
  '1minute': '1m', '3minute': '3m', '5minute': '5m', '15minute': '15m',
  '30minute': '30m', '1hour': '1h', '2hour': '2h', '4hour': '4h',
  '8hour': '8h', '12hour': '12h',
  '1day': '1d', '3day': '3d', '1week': '1w', '1month': '1M',
}

const PERIOD_MS: Record<string, number> = {
  '1minute': 60_000,
  '3minute': 180_000,
  '5minute': 300_000,
  '15minute': 900_000,
  '30minute': 1_800_000,
  '1hour': 3_600_000,
  '2hour': 7_200_000,
  '4hour': 14_400_000,
  '8hour': 28_800_000,
  '12hour': 43_200_000,
  '1day': 86_400_000,
  '3day': 259_200_000,
  '1week': 604_800_000,
  '1month': 2_592_000_000,
}

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
  lastBarTimestamp: number
  liveBar: KLineData | null
  /** Last bar from history — used to preserve open/high/vol when building running bar */
  historyLastBar: KLineData | null
  lastPushTs: number
  pendingBar: KLineData | null
  pushTimer: ReturnType<typeof setTimeout> | null
}

const PUSH_THROTTLE_MS = 200

const subscriptions = new Map<string, SubEntry>()

function wsSubKey(symbol: SymbolInfo, period: Period): string {
  return `${symbol.ticker}:${periodKey(period)}`
}

/* ── Gap detection & backfill ── */

function detectGap(entry: SubEntry): { from: number; to: number } | null {
  const now = Date.now()
  const barMs = periodMs(entry.period)
  if (entry.lastBarTimestamp <= 0) return null
  const elapsedMs = now - entry.lastBarTimestamp
  const missedBars = Math.floor(elapsedMs / barMs)
  if (missedBars <= 1) return null
  const cappedBars = Math.min(missedBars, MAX_BACKFILL_BARS)
  const from = entry.lastBarTimestamp + barMs
  const to = now
  console.log(
    `[KLineDatafeed] Gap detected for ${entry.symbol.ticker} ${periodKey(entry.period)}: ` +
    `${missedBars} bars missed (capped to ${cappedBars}), fetching ${new Date(from).toISOString()} → ${new Date(to).toISOString()}`
  )
  return { from, to }
}

async function backfillGap(entry: SubEntry): Promise<void> {
  const gap = detectGap(entry)
  if (!gap) return
  try {
    const interval = toInterval(entry.period)
    const limit = Math.min(Math.ceil((gap.to - gap.from) / periodMs(entry.period)) + 10, MAX_BACKFILL_BARS)
    const data: any = await api.get('/market/klines', {
      params: { symbol: entry.symbol.ticker, interval, limit, from: gap.from, to: gap.to },
    })
    const klines: any[] = data?.klines || data || []
    if (!Array.isArray(klines) || klines.length === 0) return
    const newBars = klines.map(toKLineData).filter((bar) => bar.timestamp > entry.lastBarTimestamp).sort((a, b) => a.timestamp - b.timestamp)
    if (newBars.length === 0) return
    for (const bar of newBars) {
      entry.lastBarTimestamp = Math.max(entry.lastBarTimestamp, bar.timestamp)
      entry.callbacks.forEach((cb) => { try { cb(bar) } catch {} })
    }
  } catch (err) {
    console.error('[KLineDatafeed] Backfill failed:', err)
  }
}

export async function runBackfill(): Promise<void> {
  const entries = Array.from(subscriptions.values())
  if (entries.length === 0) return
  for (const entry of entries) { await backfillGap(entry) }
}

export function clearDatafeedSubscriptions(): void {
  subscriptions.forEach((entry) => {
    if (entry.pushTimer) { clearTimeout(entry.pushTimer); entry.pushTimer = null }
  })
  subscriptions.clear()
}

/* ── Running bar update — called from Trading.tsx (main WS hook) ── */

/**
 * Ref to the klinecharts chart API (set by Trading.tsx once _chartApi is available).
 * Used to update the current forming bar in-place without going through
 * the subscribe callback, which avoids timestamp conflicts.
 */
let chartUpdater: ((bar: KLineData) => void) | null = null

/** Set the chart updater — called from Trading.tsx when _chartApi is ready */
export function setChartUpdater(fn: (bar: KLineData) => void): void {
  chartUpdater = fn
}

/** Clear the chart updater */
export function clearChartUpdater(): void {
  chartUpdater = null
}

export function handlePriceTick(symbol: string, lastPrice: number, volume: number): void {
  const now = Date.now()

  subscriptions.forEach((entry, key) => {
    if (!key.startsWith(symbol + ':')) return

    // Wait for history data to load before building running bars,
    // otherwise the bar uses tick-only data and flashes when history arrives.
    if (!entry.historyLastBar) return

    const barMs = periodMs(entry.period)
    const alignedTs = Math.floor(now / barMs) * barMs

    let bar = entry.liveBar
    if (!bar || bar.timestamp !== alignedTs) {
      // Use history data as base so open/high/low reflect the full period
      const hist = entry.historyLastBar
      bar = {
        timestamp: alignedTs,
        open: hist.open,
        high: Math.max(hist.high, lastPrice),
        low: Math.min(hist.low, lastPrice),
        close: lastPrice,
        volume: hist.volume,
      }
    } else {
      bar = {
        timestamp: bar.timestamp, open: bar.open,
        high: Math.max(bar.high, lastPrice), low: Math.min(bar.low, lastPrice),
        close: lastPrice, volume: (bar.volume ?? 0) + volume,
      }
    }
    entry.liveBar = bar

    const prevBarTs = entry.lastBarTimestamp
    const isNewBar = bar.timestamp !== prevBarTs
    entry.lastBarTimestamp = Math.max(entry.lastBarTimestamp, bar.timestamp)

    if (isNewBar) {
      // A new period's bar — push via callback (klinecharts creates a new bar)
      entry.lastPushTs = now
      if (entry.pushTimer) { clearTimeout(entry.pushTimer); entry.pushTimer = null }
      entry.pendingBar = null
      entry.callbacks.forEach((cb) => { try { cb(bar) } catch {} })
      return
    }

    // Same bar as last known — update in-place via chart API if available,
    // falling back to subscribe callback (with throttle).
    if (chartUpdater) {
      chartUpdater(bar)
      return
    }

    // Fallback: throttle push via subscribe callback
    entry.pendingBar = bar
    const elapsed = now - entry.lastPushTs
    if (elapsed >= PUSH_THROTTLE_MS) {
      entry.lastPushTs = now
      entry.pendingBar = null
      entry.callbacks.forEach((cb) => { try { cb(bar) } catch {} })
    } else if (!entry.pushTimer) {
      entry.pushTimer = setTimeout(() => {
        entry.pushTimer = null
        if (entry.pendingBar) {
          entry.lastPushTs = Date.now()
          const pending = entry.pendingBar
          entry.pendingBar = null
          entry.callbacks.forEach((cb) => { try { cb(pending) } catch {} })
        }
      }, PUSH_THROTTLE_MS - elapsed)
    }
  })
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
      } catch { return [] }
    },

    async getHistoryKLineData(symbol: SymbolInfo, period: Period, from: number, to: number): Promise<KLineData[]> {
      try {
        const interval = toInterval(period)
        const fromMs = from
        const toMs = to
        const barDuration = periodMs(period)
        const limit = Math.min(Math.ceil((toMs - fromMs) / barDuration) + 200, 1500)

        console.log('[KLineDatafeed] getHistoryKLineData CALLED:', {
          symbol: symbol.ticker, periodText: period.text, interval, limit,
          from: new Date(fromMs).toISOString(), to: new Date(toMs).toISOString(),
        })

        const data: any = await api.get('/market/klines', {
          params: { symbol: symbol.ticker, interval, limit, from: fromMs, to: toMs },
        })

        const klines = data?.klines || data || []
        const result = (Array.isArray(klines) ? klines : []).map(toKLineData)

        // Store the last (current period) bar in subscriptions so
        // handlePriceTick can preserve open/high when building running bars.
        const lastBar = result[result.length - 1] || null
        if (lastBar) {
          subscriptions.forEach((entry, key) => {
            if (key.startsWith(symbol.ticker + ':')) {
              entry.historyLastBar = lastBar
            }
          })
        }

        console.log('[KLineDatafeed] getHistoryKLineData RESULT:', {
          symbol: symbol.ticker, periodText: period.text, barsCount: result.length,
          firstBar: result[0] ? { time: new Date(result[0].timestamp).toISOString(), o: result[0].open, c: result[0].close } : null,
          lastBar: result[result.length - 1] ? { time: new Date(result[result.length - 1].timestamp).toISOString(), o: result[result.length - 1].open, c: result[result.length - 1].close } : null,
        })
        return result
      } catch (e) {
        console.warn('[KLineDatafeed] getHistoryKLineData ERROR:', e)
        return []
      }
    },

    subscribe(symbol: SymbolInfo, period: Period, callback: DatafeedSubscribeCallback): void {
      const key = wsSubKey(symbol, period)
      let entry = subscriptions.get(key)
      if (!entry) {
        entry = {
          symbol, period, callbacks: new Set(),
          lastBarTimestamp: 0, liveBar: null, historyLastBar: null,
          lastPushTs: 0, pendingBar: null, pushTimer: null,
        }
        subscriptions.set(key, entry)
      }
      entry.callbacks.add(callback)
    },

    unsubscribe(symbol: SymbolInfo, period: Period): void {
      const key = wsSubKey(symbol, period)
      const entry = subscriptions.get(key)
      if (!entry) return
      if (entry.pushTimer) { clearTimeout(entry.pushTimer); entry.pushTimer = null }
      entry.historyLastBar = null
      entry.callbacks.clear()
      subscriptions.delete(key)
    },
  }
}
