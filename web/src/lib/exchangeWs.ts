/**
 * Exchange WebSocket K-line stream client — TypeScript port of XiaoTianQuant exchangeWs.js.
 * Supports: Binance, OKX, Bitget, Bybit, Gate. Falls back to Binance on failure.
 *
 * Features:
 *  - Multi-exchange real-time K-line streaming
 *  - Auto-reconnect with exponential backoff (max 20 attempts)
 *  - Fallback to Binance when primary exchange fails
 *  - Ping/pong keepalive
 *  - Gap tracking: records last bar timestamp for backfill after reconnect
 *  - Connection timeout detection (8s connect, 12s data for non-Binance)
 */

export interface BarData {
  timestamp: number
  open: number
  high: number
  low: number
  close: number
  volume: number
  isClosed: boolean
}

/** Info emitted on reconnection so consumers can backfill missing data */
export interface ReconnectInfo {
  /** Timestamp (ms) of the last bar received before disconnect, or 0 if none */
  lastBarTimestamp: number
  /** Symbol being subscribed */
  symbol: string
}

export interface WSCallbacks {
  onTick: (bar: BarData) => void
  onNewBar: (bar: BarData) => void
  onError?: (err: Error) => void
  onReconnecting?: () => void
  /** Emitted after a successful reconnection with gap info for backfill */
  onReconnected?: (info: ReconnectInfo) => void
}

/* ── Exchange WebSocket configs ─────────────────────────────────── */

interface ExchangeConf {
  base: string
  buildUrl(symbol: string, interval: string): string
  subscribe?(ws: WebSocket, symbol: string, interval: string): void
  parseBar(data: Record<string, unknown>): BarData | null
  ping(ws: WebSocket): void
}

const BINANCE_TF: Record<string, string> = {
  '1m': '1m', '5m': '5m', '15m': '15m', '30m': '30m',
  '1H': '1h', '4H': '4h', '1D': '1d', '1W': '1w', '1M': '1M',
}
const OKX_TF: Record<string, string> = {
  '1m': '1m', '5m': '5m', '15m': '15m', '30m': '30m',
  '1H': '1H', '4H': '4H', '1D': '1D', '1W': '1W', '1M': '1M',
}
const BITGET_TF: Record<string, string> = {
  '1m': '1m', '5m': '5m', '15m': '15m', '30m': '30m',
  '1H': '1h', '4H': '4h', '1D': '1d', '1W': '1w', '1M': '1M',
}
const BYBIT_TF: Record<string, string> = {
  '1m': '1', '5m': '5', '15m': '15', '30m': '30',
  '1H': '60', '4H': '240', '1D': 'D', '1W': 'W', '1M': 'M',
}
const GATE_TF: Record<string, string> = {
  '1m': '1m', '5m': '5m', '15m': '15m', '30m': '30m',
  '1H': '1h', '4H': '4h', '1D': '1d', '1W': '7d', '1M': '30d',
}

function getInterval(exchange: string, timeframe: string): string {
  const map: Record<string, Record<string, string>> = {
    binance: BINANCE_TF, okx: OKX_TF, bitget: BITGET_TF, bybit: BYBIT_TF, gate: GATE_TF,
  }
  return (map[exchange] || BINANCE_TF)[timeframe] || '1h'
}

function toOkxInstId(symbol: string): string {
  const parts = symbol.split('/')
  if (parts.length === 2) return `${parts[0].toUpperCase()}-${parts[1].toUpperCase()}`
  return symbol.replace(/[^a-zA-Z0-9]/g, '-').toUpperCase()
}

function toBitgetInstId(symbol: string): string {
  const parts = symbol.split('/')
  if (parts.length === 2) return `${parts[0].toUpperCase()}${parts[1].toUpperCase()}`
  return symbol.replace(/[^a-zA-Z0-9]/g, '').toUpperCase()
}

export function resolveExchangeId(id: string): string {
  const lower = (id || '').toLowerCase().replace(/[^a-z0-9]/g, '')
  const aliases: Record<string, string> = {
    okx: 'okx', okex: 'okx', binance: 'binance', bitget: 'bitget',
    bybit: 'bybit', gate: 'gate', gateio: 'gate',
    coinbase: 'binance', htx: 'binance', huobi: 'binance',
    kraken: 'binance', kucoin: 'binance',
  }
  return aliases[lower] || 'binance'
}

const EXCHANGE_WS: Record<string, ExchangeConf> = {
  binance: {
    base: 'wss://stream.binance.com:9443/ws',
    buildUrl(symbol, interval) {
      const s = symbol.replace(/[^a-zA-Z0-9]/g, '').toLowerCase()
      return `${this.base}/${s}@kline_${interval}`
    },
    parseBar(data) {
      if (data.e !== 'kline' || !data.k) return null
      const k = data.k as Record<string, unknown>
      return {
        timestamp: k.t as number, open: parseFloat(k.o as string), high: parseFloat(k.h as string),
        low: parseFloat(k.l as string), close: parseFloat(k.c as string), volume: parseFloat(k.v as string),
        isClosed: !!(k.x as boolean),
      }
    },
    ping(ws) { try { ws.send(JSON.stringify({ pong: Date.now() })) } catch { /* ignore */ } },
  },

  okx: {
    base: 'wss://ws.okx.com:8443/ws/v5/business',
    buildUrl() { return this.base },
    subscribe(ws, symbol, interval) {
      ws.send(JSON.stringify({
        op: 'subscribe',
        args: [{ channel: 'candle' + interval, instId: toOkxInstId(symbol) }],
      }))
    },
    parseBar(data) {
      const d = data.data as unknown[]
      const arg = data.arg as Record<string, unknown>
      if (!d || !arg?.channel?.toString().startsWith('candle')) return null
      const c = d[0] as string[]
      if (!c) return null
      return {
        timestamp: parseInt(c[0]), open: parseFloat(c[1]), high: parseFloat(c[2]),
        low: parseFloat(c[3]), close: parseFloat(c[4]), volume: parseFloat(c[5]),
        isClosed: !!c[8],
      }
    },
    ping(ws) { try { ws.send('ping') } catch { /* ignore */ } },
  },

  bitget: {
    base: 'wss://ws.bitget.com/v2/ws/public',
    buildUrl() { return this.base },
    subscribe(ws, symbol, interval) {
      ws.send(JSON.stringify({
        op: 'subscribe',
        args: [{ instType: 'SPOT', channel: 'candle' + interval, instId: toBitgetInstId(symbol) }],
      }))
    },
    parseBar(data) {
      const d = data.data as unknown[]
      const arg = data.arg as Record<string, unknown>
      if (!Array.isArray(d) || !d.length) return null
      if (!String(arg?.channel || '').startsWith('candle')) return null
      const c = d[0] as string[]
      if (!Array.isArray(c)) return null
      return {
        timestamp: parseInt(c[0]), open: parseFloat(c[1]), high: parseFloat(c[2]),
        low: parseFloat(c[3]), close: parseFloat(c[4]), volume: parseFloat(c[5]),
        isClosed: true,
      }
    },
    ping(ws) { try { ws.send('ping') } catch { /* ignore */ } },
  },

  bybit: {
    base: 'wss://stream.bybit.com/v5/public/spot',
    buildUrl() { return this.base },
    subscribe(ws, symbol, interval) {
      const s = symbol.replace(/[^a-zA-Z0-9]/g, '').toUpperCase()
      ws.send(JSON.stringify({ op: 'subscribe', args: [`kline.${interval}.${s}`] }))
    },
    parseBar(data) {
      const d = data.data as Record<string, string>[]
      if (!d || !data.topic?.toString().startsWith('kline.')) return null
      const c = d[0]
      if (!c) return null
      return {
        timestamp: parseInt(c.start), open: parseFloat(c.open), high: parseFloat(c.high),
        low: parseFloat(c.low), close: parseFloat(c.close), volume: parseFloat(c.volume),
        isClosed: !!c.confirm,
      }
    },
    ping(ws) { try { ws.send(JSON.stringify({ op: 'ping' })) } catch { /* ignore */ } },
  },

  gate: {
    base: 'wss://api.gateio.ws/ws/v4/',
    buildUrl() { return this.base },
    subscribe(ws, symbol, interval) {
      const s = symbol.replace('/', '_').toUpperCase()
      ws.send(JSON.stringify({
        time: Math.floor(Date.now() / 1000),
        channel: 'spot.candlesticks',
        event: 'subscribe',
        payload: [interval, s],
      }))
    },
    parseBar(data) {
      if (data.channel !== 'spot.candlesticks' || data.event !== 'update') return null
      const c = data.result as Record<string, unknown>
      if (!c) return null
      return {
        timestamp: parseInt(c.t as string) * 1000, open: parseFloat(c.o as string), high: parseFloat(c.h as string),
        low: parseFloat(c.l as string), close: parseFloat(c.c as string), volume: parseFloat(c.v as string),
        isClosed: !!(c.n as boolean),
      }
    },
    ping(ws) {
      try {
        ws.send(JSON.stringify({ time: Math.floor(Date.now() / 1000), channel: 'spot.ping' }))
      } catch { /* ignore */ }
    },
  },
}

/* ── Main class ────────────────────────────────────────────────── */

const FALLBACK_EXCHANGE = 'binance'

export class ExchangeKlineWs {
  private _ws: WebSocket | null = null
  private _url = ''
  private _exchangeId = 'binance'
  private _exchangeConf: ExchangeConf = EXCHANGE_WS.binance
  private _onTick: ((bar: BarData) => void) | null = null
  private _onNewBar: ((bar: BarData) => void) | null = null
  private _onError: ((err: Error) => void) | null = null
  private _onReconnecting: (() => void) | null = null
  private _onReconnected: ((info: ReconnectInfo) => void) | null = null
  private _reconnectAttempts = 0
  private _maxReconnectAttempts = 20
  private _reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private _pingTimer: ReturnType<typeof setInterval> | null = null
  private _closed = false
  private _symbol = ''
  private _timeframe = ''
  private _everConnected = false
  private _fallbackUsed = false
  private _connectTimeout: ReturnType<typeof setTimeout> | null = null
  private _dataTimeout: ReturnType<typeof setTimeout> | null = null
  private _openGen = 0
  private _gotData = false

  /** Timestamp (ms) of the last bar (tick or new bar) received. Used for gap detection on reconnect. */
  private _lastBarTimestamp = 0

  /** Real-time timestamp (ms) of when we last received any data. Used for data timeout detection. */
  private _lastDataTime = 0

  connect(
    symbol: string,
    timeframe: string,
    callbacks: WSCallbacks,
    exchangeId?: string,
  ): void {
    this.disconnect()
    this._closed = false
    this._everConnected = false
    this._fallbackUsed = false
    this._lastBarTimestamp = 0
    this._lastDataTime = 0
    this._symbol = symbol
    this._timeframe = timeframe
    this._onTick = callbacks.onTick
    this._onNewBar = callbacks.onNewBar
    this._onError = callbacks.onError ?? null
    this._onReconnecting = callbacks.onReconnecting ?? null
    this._onReconnected = callbacks.onReconnected ?? null

    const resolved = resolveExchangeId(exchangeId || 'binance')
    this._exchangeId = resolved === FALLBACK_EXCHANGE && resolved !== 'binance'
      ? FALLBACK_EXCHANGE
      : resolved
    this._openConnection()
  }

  disconnect(): void {
    this._closed = true
    this._clearTimers()
    if (this._ws) {
      try { this._ws.close() } catch { /* ignore */ }
      this._ws = null
    }
  }

  isConnected(): boolean {
    return this._ws?.readyState === WebSocket.OPEN && !this._closed
  }

  /** Returns the timestamp of the last received bar (for external backfill logic) */
  lastBarTimestamp(): number {
    return this._lastBarTimestamp
  }

  private _clearTimers(): void {
    if (this._reconnectTimer) { clearTimeout(this._reconnectTimer); this._reconnectTimer = null }
    if (this._pingTimer) { clearInterval(this._pingTimer); this._pingTimer = null }
    if (this._connectTimeout) { clearTimeout(this._connectTimeout); this._connectTimeout = null }
    if (this._dataTimeout) { clearTimeout(this._dataTimeout); this._dataTimeout = null }
  }

  private _openConnection(): void {
    if (this._closed) return
    this._clearTimers()
    this._gotData = false
    this._openGen++
    const gen = this._openGen

    const conf = EXCHANGE_WS[this._exchangeId] || EXCHANGE_WS.binance
    this._exchangeConf = conf
    const interval = getInterval(this._exchangeId, this._timeframe)
    const url = conf.buildUrl(this._symbol, interval)
    this._url = url

    try {
      const ws = new WebSocket(url)
      this._ws = ws

      ws.onopen = () => {
        if (this._closed || gen !== this._openGen) return
        if (conf.subscribe) conf.subscribe(ws, this._symbol, this._timeframe)
        this._reconnectAttempts = 0
        this._startPing()
      }

      ws.onmessage = (event) => {
        if (this._closed || gen !== this._openGen) return
        try {
          const data = JSON.parse(event.data)
          // Skip subscription confirmations
          if (data.event === 'subscribe' || data.op === 'subscribe') return

          const bar = conf.parseBar(data)
          if (!bar) return

          // Update tracking timestamps
          this._lastDataTime = Date.now()
          if (bar.timestamp > this._lastBarTimestamp) {
            this._lastBarTimestamp = bar.timestamp
          }

          if (!this._gotData) {
            this._gotData = true
            if (this._dataTimeout) { clearTimeout(this._dataTimeout); this._dataTimeout = null }
          }

          // On first data after (re)connection, emit reconnected event
          if (!this._everConnected) {
            this._everConnected = true
            if (this._onReconnected) {
              this._onReconnected({
                lastBarTimestamp: this._lastBarTimestamp,
                symbol: this._symbol,
              })
            }
          }

          // Emit to callbacks
          this._onTick?.(bar)
          if (bar.isClosed) this._onNewBar?.(bar)
        } catch { /* ignore */ }
      }

      ws.onclose = () => {
        if (this._closed || gen !== this._openGen) return
        this._scheduleReconnect()
      }

      ws.onerror = () => {} // handled by onclose

      // Connection timeout: if not connected within 8s, fallback
      this._connectTimeout = setTimeout(() => {
        if (gen !== this._openGen || this._closed) return
        if (!this._everConnected) this._fallbackToBinance()
      }, 8000)

      // Data timeout for non-Binance: if no bars received within 12s, fallback
      if (this._exchangeId !== 'binance') {
        this._dataTimeout = setTimeout(() => {
          if (gen !== this._openGen || this._closed || this._gotData) return
          this._fallbackToBinance()
        }, 12000)
      }
    } catch (_) {
      this._scheduleReconnect()
    }
  }

  private _fallbackToBinance(): void {
    if (this._fallbackUsed || this._exchangeId === 'binance') return
    this._fallbackUsed = true
    this._exchangeId = 'binance'
    this._clearTimers()
    try { this._ws?.close() } catch { /* ignore */ }
    this._ws = null
    this._openConnection()
  }

  private _scheduleReconnect(): void {
    if (this._closed) return
    this._clearTimers()

    if (this._reconnectAttempts >= this._maxReconnectAttempts) {
      this._onError?.(new Error(
        `WebSocket reconnect limit reached (${this._maxReconnectAttempts} attempts) for ${this._symbol}`
      ))
      return
    }

    // Fire reconnecting callback on first retry
    if (this._reconnectAttempts === 0) this._onReconnecting?.()

    const delay = Math.min(1000 * Math.pow(2, this._reconnectAttempts), 30000)
    this._reconnectAttempts++

    console.warn(
      `[ExchangeWS:${this._exchangeId}] Reconnecting in ${delay}ms ` +
      `(attempt ${this._reconnectAttempts}/${this._maxReconnectAttempts}, ` +
      `last bar: ${this._lastBarTimestamp ? new Date(this._lastBarTimestamp).toISOString() : 'none'})`
    )

    this._reconnectTimer = setTimeout(() => this._openConnection(), delay)
  }

  private _startPing(): void {
    if (this._pingTimer) clearInterval(this._pingTimer)
    this._pingTimer = setInterval(() => {
      if (this._ws?.readyState === WebSocket.OPEN) {
        this._exchangeConf.ping(this._ws)
      }
    }, 120000) // 2 minutes
  }
}

export default ExchangeKlineWs
