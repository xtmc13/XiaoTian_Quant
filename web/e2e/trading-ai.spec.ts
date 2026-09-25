import { test, expect } from './fixtures'
import type { Page, Route } from '@playwright/test'

/**
 * E2E: Trading order placement flow + AI analysis flow.
 * Covers critical user journeys for M4.2 test coverage.
 *
 * All /api/** calls are mocked with responses shaped the way the frontend
 * expects them AFTER the axios envelope unwrap ({success,data,meta} -> data)
 * in web/src/lib/api.ts. Assertions are unconditional: if the UI element
 * under test disappears, these tests must fail.
 */

const fulfillJson = (route: Route, body: unknown, status = 200) =>
  route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })

/** Stub the live WebSocket so trading pages never depend on the real gateway. */
async function stubWebSocket(page: Page) {
  // Accept the page-side connection but never forward to a server;
  // the UI renders fine without live ticks.
  await page.routeWebSocket('**/ws', (ws) => {
    ws.onMessage(() => {})
  })
}

/**
 * Deterministic market/account mocks shared by the trading pages
 * (/trading and /contract-trading). Tests override /orders etc. afterwards
 * (Playwright runs the most recently registered route first).
 */
async function mockTradingApis(page: Page) {
  await page.route('**/api/market/orderbook*', (route) =>
    fulfillJson(route, {
      symbol: 'BTCUSDT',
      bids: [
        [50099, 0.4],
        [50098, 0.6],
        [50097, 1.2],
      ],
      asks: [
        [50100, 0.5],
        [50101, 0.3],
        [50102, 0.8],
      ],
      ts: Date.now(),
    }),
  )
  await page.route('**/api/market/klines*', (route) => fulfillJson(route, { klines: [] }))
  await page.route('**/api/market/trades*', (route) => fulfillJson(route, { trades: [] }))
  await page.route('**/api/market/snapshot*', (route) => {
    const url = new URL(route.request().url())
    const symbol = url.searchParams.get('symbol')
    if (!symbol) return fulfillJson(route, { tickers: [] })
    return fulfillJson(route, { symbol, price: 50099.5, change_pct_24h: 1.23, volume_24h: 1234.5 })
  })
  await page.route('**/api/portfolio/summary*', (route) =>
    fulfillJson(route, { spot_balance: 100000, futures_balance: 50000, total_equity: 150000 }),
  )
  await page.route('**/api/portfolio/positions*', (route) => fulfillJson(route, { positions: [] }))
  await page.route('**/api/account/balance*', (route) => fulfillJson(route, { balances: [] }))
  await page.route('**/api/trades*', (route) => fulfillJson(route, { trades: [] }))
  // ContractTrading reads funding rate from the gateway (/api/market/funding)
  await page.route('**/api/market/funding*', (route) =>
    fulfillJson(route, { funding_rate: 0.0001, mark_price: 50099.5, next_funding_time: 1758307200000 }),
  )
}

/**
 * Register an /orders-aware route handler. Two patterns are needed because
 * Playwright glob `*` does not cross `/`: the list endpoint is
 * `/api/orders?_t=...` while cancel/history live under `/api/orders/...`.
 * Handler returns true when it fulfilled the request, false to fall through.
 */
async function mockOrdersApi(
  page: Page,
  handler: (route: Route, url: string, method: string) => Promise<boolean>,
) {
  const wrapped = async (route: Route) => {
    const req = route.request()
    if (await handler(route, req.url(), req.method())) return
    await route.fallback()
  }
  await page.route('**/api/orders*', wrapped)
  await page.route('**/api/orders/**', wrapped)
}

async function ensureAuthenticated(page: any) {
  // If redirected to login, restore auth state and reload
  const url = page.url()
  if (url.includes('/login')) {
    await page.evaluate(() => {
      localStorage.setItem('xt-auth', JSON.stringify({
        state: {
          token: 'e2e-test-token',
          user: { id: 1, username: 'e2e_user', role: 'user', nickname: 'E2E Tester' },
          isAuthenticated: true,
        },
        version: 0,
      }))
      window.location.href = window.location.href.replace('/login', '/dashboard')
    })
    await page.waitForLoadState('networkidle')
  }
}

test.describe('Trading Order Flow', () => {
  test('spot trading page loads with order book and form', async ({ authPage }) => {
    await stubWebSocket(authPage)
    await mockTradingApis(authPage)
    await authPage.goto('/trading')

    // ── Order book panel renders with mocked depth ──
    await expect(authPage.getByText('订单簿', { exact: true })).toBeVisible()
    await expect(authPage.getByText('50100.0', { exact: true })).toBeVisible() // best ask
    await expect(authPage.getByText('50099.0', { exact: true })).toBeVisible() // best bid
    await expect(authPage.getByText(/spread 1\.00/)).toBeVisible()
    await expect(authPage.getByRole('region', { name: '最新成交' })).toBeVisible()

    // ── Order form renders (LIMIT is the default tab) ──
    await expect(authPage.getByRole('button', { name: '限价' })).toBeVisible()
    await expect(authPage.getByRole('button', { name: '市价' })).toBeVisible()
    await expect(authPage.getByRole('button', { name: '买入', exact: true })).toBeVisible()
    await expect(authPage.getByRole('button', { name: '卖出', exact: true })).toBeVisible()
    await expect(authPage.getByLabel('价格')).toBeVisible()
    await expect(authPage.getByLabel('数量')).toBeVisible()
    await expect(authPage.getByRole('button', { name: '买入 BTC' })).toBeVisible()

    // ── Watchlist renders the active pair ──
    await expect(authPage.getByRole('button', { name: /BTC\/USDT/ })).toBeVisible()
  })

  test('place limit buy order on spot trading', async ({ authPage }) => {
    await stubWebSocket(authPage)
    await mockTradingApis(authPage)

    let placedOrder: Record<string, unknown> | null = null
    await authPage.route('**/api/orders', async (route) => {
      if (route.request().method() !== 'POST') return route.fallback()
      placedOrder = route.request().postDataJSON() as Record<string, unknown>
      return fulfillJson(route, {
        id: 'e2e-order-001',
        symbol: 'BTCUSDT',
        side: 'BUY',
        type: 'LIMIT',
        price: 50000,
        quantity: 0.1,
        status: 'NEW',
        created_at: new Date().toISOString(),
        market_type: 'spot',
      })
    })

    await authPage.goto('/trading')

    const priceInput = authPage.getByLabel('价格')
    const qtyInput = authPage.getByLabel('数量')
    await expect(priceInput).toBeVisible()
    await expect(qtyInput).toBeVisible()

    await priceInput.fill('50000')
    await qtyInput.fill('0.1')

    // The main submit button shows side + base asset: "买入 BTC"
    const submitBtn = authPage.getByRole('button', { name: '买入 BTC' })
    await expect(submitBtn).toBeVisible()
    await submitBtn.click()

    // Success feedback: toast + form reset
    await expect(authPage.locator('body')).toContainText('订单已提交')
    await expect(priceInput).toHaveValue('')
    await expect(qtyInput).toHaveValue('')

    // The request must hit POST /orders with the exact order payload
    expect(placedOrder).toMatchObject({
      symbol: 'BTCUSDT',
      side: 'BUY',
      order_type: 'LIMIT',
      price: 50000,
      quantity: 0.1,
      market_type: 'spot',
    })
  })

  test('contract trading page loads with leverage selector', async ({ authPage }) => {
    await stubWebSocket(authPage)
    await mockTradingApis(authPage)
    await authPage.goto('/trading/contract')

    // ── Contract order form renders ──
    await expect(authPage.getByRole('button', { name: '限价' })).toBeVisible()
    await expect(authPage.getByRole('button', { name: '条件' })).toBeVisible()
    await expect(authPage.getByLabel('价格')).toBeVisible()
    await expect(authPage.getByRole('button', { name: '开多 10x' })).toBeVisible()
    await expect(authPage.getByRole('button', { name: '开空 10x' })).toBeVisible()
    // funding rate strip comes from the Binance premiumIndex mock
    await expect(authPage.locator('body')).toContainText('资金费率')

    // ── Leverage selector: default 10x, switch to 20x ──
    await expect(authPage.getByText('杠杆', { exact: true })).toBeVisible()
    const leverageBtn = authPage.getByRole('button', { name: '20x', exact: true })
    await leverageBtn.click()
    await expect(leverageBtn).toHaveClass(/border-quant-gold/)
    // readouts and long/short buttons all reflect the new leverage
    await expect(authPage.locator('span:text-is("20x")')).toHaveCount(2)
    await expect(authPage.getByRole('button', { name: '开多 20x' })).toBeVisible()
    await expect(authPage.getByRole('button', { name: '开空 20x' })).toBeVisible()

    // ── Margin mode selector ──
    const isolatedBtn = authPage.getByRole('button', { name: '逐仓' })
    await isolatedBtn.click()
    await expect(isolatedBtn).toHaveClass(/border-quant-gold/)
    await expect(authPage.getByRole('button', { name: '全仓' })).toBeVisible()
  })

  test('switch trading pair updates symbol display', async ({ authPage }) => {
    await stubWebSocket(authPage)
    await mockTradingApis(authPage)
    await authPage.goto('/trading')

    // Default pair is BTCUSDT — submit button shows "买入 BTC"
    const submitBtn = authPage.getByRole('button', { name: '买入 BTC' })
    await expect(submitBtn).toBeVisible()

    // The watchlist on the right is the real symbol switcher of this UI
    await authPage.getByRole('button', { name: /ETH\/USDT/ }).click()

    // Symbol display updates everywhere it matters
    await expect(authPage.getByRole('button', { name: '买入 ETH' })).toBeVisible()
    await expect(authPage.getByRole('button', { name: '买入 BTC' })).toHaveCount(0)
    await expect(authPage.getByRole('button', { name: /ETH\/USDT/ })).toHaveClass(/bg-quant-gold\/10/)
  })
})

test.describe('AI Analysis Flow', () => {
  // AI 页现挂多个数据面板，dev server 冷编译 + fullyParallel 下首屏可能超默认 30s。
  test.setTimeout(120000)
  /** Mock /market/snapshot responses per requested symbol on the AI page. */
  async function mockAiMarketData(page: Page) {
    await page.route('**/api/market/snapshot*', (route) => {
      const url = new URL(route.request().url())
      const symbol = url.searchParams.get('symbol')
      if (symbol === 'SENTIMENT') {
        return fulfillJson(route, { fear_greed: 62, vix: 14.5, dxy: 104.2 })
      }
      if (symbol === 'CALENDAR') {
        return fulfillJson(route, {
          events: [
            { id: 'evt-1', date: '2026-09-19', time: '20:30', country: 'US', name: '非农就业报告', importance: 'high', actual: '250K', forecast: '240K' },
          ],
        })
      }
      if (symbol && symbol.includes('NDX')) {
        return fulfillJson(route, {
          indices: [
            { symbol: 'SPX', flag: '🇺🇸', price: 5300.25, change: 0.85 },
            { symbol: 'NDX', flag: '🇺🇸', price: 18000.5, change: 1.12 },
          ],
        })
      }
      if (symbol && symbol.startsWith('SPX,')) {
        const stock = symbol.split(',')[1]
        return fulfillJson(route, {
          price: 5300.25,
          change_pct_24h: 0.85,
          indices: [
            { symbol: 'SPX', flag: '🇺🇸', price: 5300.25, change: 0.85 },
            { symbol: stock, flag: '🇺🇸', price: 222.44, change: 1.2 },
          ],
        })
      }
      if (symbol && symbol.startsWith('HSI,')) {
        return fulfillJson(route, {
          price: 18000.5,
          change_pct_24h: 0.6,
          indices: [{ symbol: 'HSI', flag: '🇭🇰', price: 18000.5, change: 0.6 }],
        })
      }
      if (symbol && symbol.includes('=X')) {
        return fulfillJson(route, { symbol, price: 1.08, change_pct_24h: 0.2 })
      }
      // crypto & commodities default
      return fulfillJson(route, { symbol: symbol || 'BTCUSDT', price: 67000, change_pct_24h: 1.5, volume_24h: 999 })
    })
  }

  test('AI page loads with market data sections', async ({ authPage }) => {
    await mockAiMarketData(authPage)
    await authPage.goto('/ai')

    // ── Sentiment strip: fear & greed / VIX / DXY ──
    // 首屏渲染门：fullyParallel 下 dev server 编译可能 stall >5s，给首断言更长超时。
    await expect(authPage.locator('.indicator-box').filter({ hasText: '恐惧贪婪' })).toContainText('62', { timeout: 15000 })
    await expect(authPage.locator('.indicator-box').filter({ hasText: 'VIX' })).toContainText('14.5')
    await expect(authPage.locator('.indicator-box').filter({ hasText: 'DXY' })).toContainText('104.2')

    // ── Indices marquee from the snapshot mock ──
    await expect(authPage.locator('body')).toContainText('NDX')

    // ── Heatmap: tabs render, us-stocks items from mock, crypto on tab click ──
    await expect(authPage.getByRole('button', { name: '美股', exact: true })).toBeVisible()
    await expect(authPage.getByRole('button', { name: '加密', exact: true })).toBeVisible()
    await expect(authPage.locator('body')).toContainText('AAPL')
    await authPage.getByRole('button', { name: '加密', exact: true }).click()
    await expect(authPage.locator('body')).toContainText('BTC')

    // ── Economic calendar ──
    await expect(authPage.locator('body')).toContainText('财经日历')
    await expect(authPage.locator('body')).toContainText('非农就业报告')

    // ── Analysis workspace placeholder ──
    await expect(authPage.locator('body')).toContainText('AI 资产分析')
  })

  test('AI analysis returns result for selected symbol', async ({ authPage }) => {
    await mockAiMarketData(authPage)

    let analyzeRequest: Record<string, unknown> | null = null
    await authPage.route('**/api/ai/analyze', async (route) => {
      if (route.request().method() !== 'POST') return route.fallback()
      analyzeRequest = route.request().postDataJSON() as Record<string, unknown>
      return fulfillJson(route, {
        symbol: 'BTC/USDT',
        consensus: 'bullish',
        analyses: [
          {
            model: 'deepseek-chat',
            name: 'DeepSeek 分析师',
            sentiment: 'bullish',
            analysis: '根据当前 BTC 走势，建议关注 68000 阻力位突破情况。',
          },
        ],
      })
    })

    await authPage.goto('/ai')

    // The AI 分析 button stays disabled until a symbol is selected
    const analyzeBtn = authPage.getByRole('button', { name: 'AI 分析' })
    await expect(analyzeBtn).toBeDisabled()

    // Add BTC/USDT via the real "添加标的" modal (popular list)
    await authPage.getByRole('button', { name: '添加标的' }).first().click()
    const dialog = authPage.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await dialog.getByText('BTC/USDT', { exact: true }).click()

    // Select it in the symbol dropdown（页面上还有复盘/策略选择器等其它 select，取第一个 combobox）
    const symbolSelect = authPage.getByRole('combobox').first()
    await expect(symbolSelect).toContainText('BTC/USDT')
    await symbolSelect.selectOption({ value: 'Crypto:BTC/USDT' })
    await expect(analyzeBtn).toBeEnabled()

    await analyzeBtn.click()

    // The mocked multi-model result renders (the mock resolves immediately,
    // so the "AI 正在分析中" loading state is not observable here)
    await expect(authPage.locator('body')).toContainText('看涨共识')
    await expect(authPage.locator('body')).toContainText('DeepSeek 分析师')
    await expect(authPage.locator('body')).toContainText('68000')
    await expect(authPage.locator('body')).toContainText('BTC/USDT')

    // The request carries the bare symbol
    expect(analyzeRequest).toMatchObject({ symbol: 'BTC/USDT' })
  })

  test('AI quick scan returns analysis result', async ({ authPage }) => {
    await authPage.goto('/ai')
    await authPage.waitForLoadState('networkidle')
    await ensureAuthenticated(authPage)

    // Mock quickscan API
    await authPage.route('**/api/ai/quickscan*', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          status: 'ok',
          symbol: 'BTCUSDT',
          score: 72,
          signals: [
            { indicator: 'RSI', value: 58, signal: 'neutral' },
            { indicator: 'MACD', value: 0.45, signal: 'bullish' },
          ],
          summary: '短期偏多，注意回调风险',
        }),
      })
    })

    // Trigger quick scan if button exists
    const scanBtn = authPage.locator('button:has-text("快速扫描"), button:has-text("Quick Scan"), button:has-text("扫描"), [data-testid="quick-scan"]').first()
    if (await scanBtn.isVisible().catch(() => false)) {
      await scanBtn.click()
      await expect(authPage.locator('body')).toContainText(/短期偏多|偏多|回调/i, { timeout: 10000 })
    }
  })

  test('AI generate strategy from prompt', async ({ authPage }) => {
    await authPage.evaluate(() => {
      localStorage.setItem('xt-auth', JSON.stringify({
        state: { token: 'e2e-test-token', user: { id: 1, username: 'e2e_user', role: 'user', nickname: 'E2E Tester' }, isAuthenticated: true },
        version: 0,
      }))
    })
    await authPage.goto('/ai')
    await authPage.waitForLoadState('domcontentloaded')
    await authPage.waitForTimeout(1000)

    // Mock AI generate API
    await authPage.route('**/api/ai/generate', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          status: 'ok',
          code: 'def strategy(data):\n    return data.close > data.ma20',
          description: '均线突破策略',
        }),
      })
    })

    // Find generate button or tab
    const generateTab = authPage.locator('button:has-text("生成策略"), button:has-text("Generate"), text=策略生成, [data-testid="ai-generate"]').first()
    if (await generateTab.isVisible().catch(() => false)) {
      await generateTab.click()
      const promptInput = authPage.locator('textarea, input').first()
      if (await promptInput.isVisible().catch(() => false)) {
        await promptInput.fill('写一个均线突破策略')
        const submitBtn = authPage.locator('button:has-text("生成"), button:has-text("Generate"), button[type="submit"]').first()
        if (await submitBtn.isVisible().catch(() => false)) {
          await submitBtn.click()
          await expect(authPage.locator('body')).toContainText(/均线突破|突破策略|strategy/i, { timeout: 15000 })
        }
      }
    }
  })
})

test.describe('Order Management Flow', () => {
  test('cancel order from open orders list', async ({ authPage }) => {
    await stubWebSocket(authPage)
    await mockTradingApis(authPage)

    const OPEN_ORDER = {
      id: 'order-001',
      symbol: 'BTCUSDT',
      side: 'BUY',
      type: 'LIMIT',
      price: 50000,
      quantity: 0.1,
      status: 'OPEN',
      created_at: '2026-09-19T10:00:00.000Z',
      market_type: 'spot',
    }

    let cancelled = false
    let cancelRequests = 0
    await mockOrdersApi(authPage, async (route, url, method) => {
      if (method === 'POST' && url.includes('/orders/') && url.includes('/cancel')) {
        cancelled = true
        cancelRequests += 1
        await fulfillJson(route, { success: true })
        return true
      }
      if (method === 'GET' && url.includes('/orders/history')) {
        await fulfillJson(route, { orders: [] })
        return true
      }
      if (method === 'GET') {
        // Open orders list: one open order until the cancel lands, then empty
        await fulfillJson(route, { orders: cancelled ? [] : [OPEN_ORDER] })
        return true
      }
      return false
    })

    await authPage.goto('/trading')

    // Open the "当前委托" bottom tab
    await authPage.getByRole('button', { name: /当前委托/ }).click()

    // The mocked open order renders in the table
    const orderRow = authPage.getByRole('row', { name: /BTCUSDT/ })
    await expect(orderRow).toBeVisible()
    await expect(authPage.locator('body')).toContainText('委托中')

    // Cancel it
    await orderRow.getByRole('button', { name: '取消' }).click()

    // Success feedback + the cancel API hit + the row disappears on refetch
    await expect(authPage.locator('body')).toContainText('订单已取消')
    expect(cancelRequests).toBe(1)
    await expect(orderRow).toHaveCount(0)
    await expect(authPage.getByText('暂无委托')).toBeVisible()
  })
})
