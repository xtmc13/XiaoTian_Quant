import type { Page, Route } from '@playwright/test'

/**
 * Shared helpers for the interaction audit spec.
 *
 * Mock philosophy: broad-but-shaped. The base fixture already returns `[]`
 * for well-known list endpoints and `{}` for the rest; here we add the
 * wrapper shapes ({jobs:[]}, {alerts:[]}, ...) that pages destructure, so a
 * failure means the PAGE is broken, not the mock. Registered after the
 * fixture routes, so these handlers win (Playwright: last added matches first).
 */

export const fulfillJson = (route: Route, body: unknown, status = 200) =>
  route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body ?? null) })

/** Kill every WebSocket (live ticks, exchange feeds) so audits are hermetic. */
export async function stubAllWebSockets(page: Page) {
  await page.addInitScript(() => {
    class DummyWS {
      static CONNECTING = 0
      static OPEN = 1
      readyState = 3
      onopen: unknown = null
      onclose: unknown = null
      onmessage: unknown = null
      onerror: unknown = null
      constructor(public url: string) {}
      send() {}
      close() {}
      addEventListener() {}
      removeEventListener() {}
    }
    ;(window as unknown as Record<string, unknown>).WebSocket = DummyWS as unknown
  })
}

const has = (p: string, seg: string) => p.includes(seg)

/** Ordered [segment, body] pairs; first path match wins. `undefined` body = skip. */
const SHAPES: [string, (() => unknown) | unknown][] = [
  // ── Market data ──
  ['/market/orderbook', () => ({ symbol: 'BTCUSDT', bids: [[50099, 0.4], [50098, 0.6]], asks: [[50100, 0.5], [50101, 0.3]], ts: Date.now() })],
  ['/market/klines', { klines: [] }],
  ['/market/trades', { trades: [] }],
  ['/market/funding', () => ({ funding_rate: 0.0001, mark_price: 50099.5, next_funding_time: Date.now() + 3600_000 })],
  ['/market/snapshot', { symbol: 'BTCUSDT', price: 67000, change_pct_24h: 1.5, volume_24h: 999, tickers: [] }],
  // ── Market board / listings ──
  ['/market/my-listings', { listings: [] }],
  ['/market/listings/', { listing: null, series: [] }],
  ['/market/listings', { listings: [], total: 0, page: 1, page_size: 20 }],
  ['/market/rules', { min_days: 30, min_trades: 10, max_drawdown_pct: 20 }],
  ['/admin/market/listings', { listings: [] }],
  // ── Dashboard / portfolio ──
  ['/dashboard/summary', {
    total_equity: 150000,
    total_pnl: 1250.5,
    equity_curve: [],
    ai_agents: [],
    ai_logs: [],
    calendar: {},
    win_rate: 0.55,
    profit_factor: 1.4,
    max_drawdown: 0.08,
    total_trades: 42,
  }],
  ['/portfolio/summary', { spot_balance: 100000, futures_balance: 50000, total_equity: 150000 }],
  ['/portfolio/positions', { positions: [] }],
  ['/portfolio/snapshots', { snapshots: [] }],
  // 已平仓持仓（资产页分享卡数据源）
  ['/positions/closed', { positions: [], limit: 20, offset: 0, has_more: false }],
  // ── Health / status / alerts / integrations ──
  ['/health/components', []],
  ['/alerts/active', { alerts: [] }],
  ['/alerts/history', { alerts: [] }],
  ['/integrations/status', { integrations: [] }],
  ['/exchanges/health-check/latest', { results: [] }],
  ['/exchanges/health-check/history', { results: [] }],
  ['/exchanges/health-check/jobs', { id: 'audit-job', status: 'completed', results: {} }],
  ['/exchanges/configured', []],
  ['/exchange/status', { connected: false, exchanges: [] }],
  ['/exchange/usdcny', { rate: 7.25 }],
  ['/config/rate', { rate: 7.25 }],
  ['/api/health', { status: 'ok', version: 'e2e-audit', uptime: '1h0m0s', log_level: 'info' }],
  // ── ML ──
  ['/ml/loop-status', { running: false, ml_server: { running: false }, scheduler: {}, stats: {} }],
  ['/ml/training-runs', { runs: [] }],
  ['/ml/retrain-jobs', { jobs: [] }],
  ['/ml/models', { models: [] }],
  ['/ml/strategy-models', { models: [] }],
  ['/ml/health', { status: 'ok' }],
  // ── Analysis (lookahead/recursive) ──
  ['/analysis/jobs', { jobs: [] }],
  ['/analysis/result', {}],
  // ── Dataproviders / onchain / social ──
  ['/dataproviders/sentiment', { fear_greed: 62, vix: 14.5, dxy: 104.2 }],
  ['/dataproviders/sources', { sources: [] }],
  ['/dataproviders/heatmap', () => ({ items: [], generated_at: Date.now() })],
  ['/dataproviders/calendar', { events: [] }],
  ['/dataproviders/macro', { series: [] }],
  ['/dataproviders/news', { items: [] }],
  ['/onchain/', {}],
  ['/social/providers', []],
  ['/social/earnings/withdrawals', []],
  ['/social/earnings', { available: 0, total: 0, records: [] }],
  ['/social/followers/configs', []],
  ['/social/signals', []],
  // ── Billing ──
  ['/billing/plans', []],
  ['/billing/chains', []],
  ['/billing/subscription', null],
  ['/billing/orders', []],
  // ── Orders (advanced / ladder) ──
  ['/orders/ladder', { orders: [], count: 0 }],
  ['/orders/oco', { orders: [] }],
  ['/orders/bracket', { orders: [] }],
  ['/orders/iceberg', { orders: [] }],
  // ── Hyperopt / pairlist / protection ──
  ['/hyperopt/jobs', { jobs: [] }],
  ['/hyperopt/spaces', { spaces: [] }],
  ['/pairlist/whitelist', { whitelist: [], blacklist: [] }],
  ['/protection/status', {}],
  ['/protection/config', []],
  // ── Arbitrage ──
  ['/arbitrage/opportunity', { opportunities: [] }],
  ['/arbitrage/positions', { positions: [] }],
  ['/arbitrage/history', { history: [] }],
  ['/arbitrage/exchanges', []],
  ['/arbitrage/performance', {}],
  ['/triangular/opportunities', { opportunities: [] }],
  ['/triangular/positions', { positions: [] }],
  ['/triangular/history', { trades: [] }],
  ['/triangular/performance', {}],
  ['/triangular/config', { config: { enabled: false, min_profit_pct: 0.3, max_position_usdt: 500, poll_interval: 5 } }],
  ['/arbitrage/config', { config: { enabled: false, min_profit_pct: 0.5, max_position_usdt: 1000, poll_interval: 5000000000 } }],
  // ── AI gate / review ──
  ['/ai/gate/decisions', { decisions: [], total: 0, page: 1, page_size: 20 }],
  ['/ai/gate/stats', {
    stats: { total: 0, evaluated: 0, approved: 0, blocked: 0, abstained: 0, fail_open: 0, bypassed_exit: 0, skipped: 0 },
    block_rate: 0,
    fail_open_rate: 0,
  }],
  ['/ai/gate/config', {
    config: {
      enabled: false,
      min_confidence: 0.6,
      abstain_action: 'allow',
      paper_only: true,
      timeout_seconds: 30,
      provider: 'deepseek',
      context_bars: 120,
      excluded_sources: [],
    },
  }],
  ['/ai/review/reports', { reports: [] }],
  // ── Misc ──
  ['/notifications/unread-count', { count: 0 }],
  ['/notifications', []],
  ['/notify/routes', []],
  ['/watchlist', []],
  ['/tensorboard/runs', { runs: [] }],
  ['/rl/models', { models: [] }],
  ['/rl/worker/status', {}],
  ['/admin/audit-log', { logs: [], total: 0 }],
  ['/admin/users', []],
  ['/admin/stats', {}],
  ['/agent/tokens', []],
  ['/indicator/list', { indicators: [] }],
  ['/data/coverage', { coverage: [] }],
  ['/executor/positions', { positions: [] }],
  ['/executor/records', { records: [] }],
  ['/executor/signal-sources', { sources: [] }],
  ['/ai-bots/catalog', []],
  ['/ai-bots/instances', []],
  ['/ai-bots/subscriptions', []],
]

export async function applyAuditMocks(page: Page) {
  await page.route('**/api/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    // 系统日志是 text/plain 字符串（区别于 /strategies/logs 等 JSON 数组端点）
    if (path === '/api/logs') {
      return fulfillJson(route, '[2026-09-26 10:00:00] INFO gateway started\n[2026-09-26 10:00:01] INFO audit mock log')
    }
    for (const [seg, body] of SHAPES) {
      if (has(path, seg)) {
        const payload = typeof body === 'function' ? (body as () => unknown)() : body
        return fulfillJson(route, payload)
      }
    }
    return route.fallback()
  })
}

// ── Issue collectors ──

export interface AuditIssues {
  consoleErrors: string[]
  pageErrors: string[]
  failedApis: string[]
  rawI18nKeys: string[]
}

export function freshIssues(): AuditIssues {
  return { consoleErrors: [], pageErrors: [], failedApis: [], rawI18nKeys: [] }
}

/** Console errors matching these are environmental noise, not page bugs. */
const BENIGN_CONSOLE = [
  /Download the React DevTools/i,
  /\[vite\]/i,
  /React Router Future Flag/i,
  /WebSocket/i,
  /Failed to load resource.*(favicon|\/ws)/i,
  /net::ERR/,
  /Third-party cookie/i,
  /Content Security Policy directive.*frame-ancestors/i,
]

/** API failures on these paths are expected in mock mode (fake token). */
const BENIGN_API = [/\/api\/auth\//]

const RAW_KEY_RE =
  /\b((?:nav|common|settings|market|arb|dash|chrome|dashboard|marketdata|ai|bots|billing|risk|hyperopt|pairlist|backtest|strategy|social|onchain|freqai|ml|rl)\.[a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9_-]+)+)\b/g

export function attachCollectors(page: Page, issues: AuditIssues) {
  page.on('console', (msg) => {
    if (msg.type() !== 'error') return
    const text = msg.text()
    if (BENIGN_CONSOLE.some((re) => re.test(text))) return
    issues.consoleErrors.push(text.slice(0, 300))
  })
  page.on('pageerror', (err) => {
    issues.pageErrors.push(String(err).slice(0, 300))
  })
  page.on('response', (res) => {
    const status = res.status()
    if (status < 400) return
    const url = res.url()
    if (BENIGN_API.some((re) => re.test(url))) return
    // 401/403 from unmocked edge endpoints are expected with a fake token
    if (status === 401 || status === 403) return
    issues.failedApis.push(`${status} ${res.request().method()} ${url}`)
  })
}

export async function scanRawI18nKeys(page: Page): Promise<string[]> {
  const bodyText = await page.locator('body').innerText().catch(() => '')
  const found = new Set<string>()
  for (const m of bodyText.matchAll(RAW_KEY_RE)) {
    found.add(m[1])
  }
  return [...found]
}

/** Wait for a lazy page to finish first render (vite cold compile tolerant). */
export async function settlePage(page: Page, ms = 1200) {
  await page.waitForLoadState('domcontentloaded')
  await page.waitForTimeout(ms)
}
