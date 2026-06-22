import { test, expect } from './fixtures'

/**
 * E2E: AI Bots page end-to-end tests.
 * Covers the 5-tab AI Bots marketplace: catalog, providers, my bots,
 * analytics, subscriptions, plus the create/edit wizard and lifecycle.
 *
 * All backend APIs are mocked via page.route() so the tests are fast,
 * deterministic, and do not require exchange keys or a running Go gateway.
 */

// ── Test data ──

const CATALOG = [
  {
    id: 'cat-001',
    name: 'Optimus 稳定现货',
    description: '低风险现货策略，适合震荡行情。',
    strategy_type: 'optimus',
    market_type: 'spot',
    risk_level: 'low',
    fee_model: 'free',
    performance_json: JSON.stringify({ avg_monthly_profit: 3.5, win_rate: 0.62, max_drawdown: 2.1 }),
    config_json: JSON.stringify({ initial_balance: 10000, leverage: 1 }),
  },
  {
    id: 'cat-002',
    name: 'AI Alpha Futures',
    description: 'AI 驱动合约策略，中风险。',
    strategy_type: 'ai_alpha_futures',
    market_type: 'futures',
    risk_level: 'medium',
    fee_model: 'profit_share',
    fee_percent: 10,
    performance_json: JSON.stringify({ avg_monthly_profit: 8.2, win_rate: 0.55, max_drawdown: 5.4 }),
    config_json: JSON.stringify({ initial_balance: 5000, leverage: 5 }),
  },
  {
    id: 'cat-003',
    name: 'Terminator Volatility',
    description: '高波动套利策略。',
    strategy_type: 'terminator_volatility',
    market_type: 'futures',
    risk_level: 'high',
    fee_model: 'monthly',
    monthly_fee: 29,
    performance_json: JSON.stringify({ avg_monthly_profit: 12.1, win_rate: 0.48, max_drawdown: 9.8 }),
    config_json: JSON.stringify({ initial_balance: 8000, leverage: 10 }),
  },
]

const INSTANCES = [
  {
    id: 'bot-001',
    user_id: 1,
    catalog_id: 'cat-001',
    name: '我的 Optimus',
    strategy_type: 'optimus',
    symbol: 'BTCUSDT',
    market_type: 'spot',
    status: 'running',
    execution_mode: 'paper',
    exchange_id: 'binance',
    unrealized_pnl: 120.5,
    realized_pnl: 80,
    total_return_pct: 2.05,
    max_drawdown_pct: 1.2,
    sharpe_ratio: 1.8,
    win_rate: 0.6,
    total_trades: 12,
    initial_balance: 10000,
    config_json: JSON.stringify({ initial_balance: 10000, leverage: 1, risk_level: 'low' }),
  },
  {
    id: 'bot-002',
    user_id: 1,
    name: 'ETH 量化一号',
    strategy_type: 'ai_alpha',
    symbol: 'ETHUSDT',
    market_type: 'spot',
    status: 'stopped',
    execution_mode: 'paper',
    unrealized_pnl: -15,
    total_return_pct: -0.5,
    config_json: JSON.stringify({ initial_balance: 5000, leverage: 1, risk_level: 'medium' }),
  },
  {
    id: 'bot-003',
    user_id: 1,
    name: 'SOL 波动策略',
    strategy_type: 'terminator_volatility',
    symbol: 'SOLUSDT',
    market_type: 'futures',
    status: 'paused',
    execution_mode: 'paper',
    unrealized_pnl: 45,
    total_return_pct: 1.2,
    config_json: JSON.stringify({ initial_balance: 8000, leverage: 3, risk_level: 'high' }),
  },
]

const SUBSCRIPTIONS = [
  {
    id: 1,
    user_id: 1,
    bot_instance_id: 'bot-001',
    fee_type: 'monthly',
    monthly_fee: 0,
    status: 'active',
    next_billing_at: Math.floor(Date.now() / 1000) + 86400 * 30,
  },
  {
    id: 2,
    user_id: 1,
    bot_instance_id: 'bot-002',
    fee_type: 'profit_share',
    fee_percent: 10,
    status: 'active',
    next_billing_at: Math.floor(Date.now() / 1000) + 86400 * 30,
  },
]

const PROVIDERS = [
  {
    provider_id: 1,
    provider_name: 'Crypto Alpha',
    total_signals: 42,
    win_count: 27,
    loss_count: 15,
    win_rate: 0.65,
    avg_return_pct: 12.5,
    sharpe_ratio: 1.4,
    max_drawdown_pct: 4.2,
    follower_count: 128,
    monthly_fee: 0,
    is_public: true,
  },
  {
    provider_id: 2,
    provider_name: 'Quant Signals',
    total_signals: 31,
    win_count: 18,
    loss_count: 13,
    win_rate: 0.58,
    avg_return_pct: 8.3,
    sharpe_ratio: 1.1,
    max_drawdown_pct: 3.1,
    follower_count: 86,
    monthly_fee: 19,
    is_public: true,
  },
]

const FOLLOWER_CONFIGS = [
  {
    provider_id: 1,
    follower_id: 1,
    enabled: true,
    multiplier: 1,
    max_position: 0.1,
    max_daily_loss: 0.05,
    slippage_pct: 0.5,
    auto_execute: false,
    symbols: [],
  },
]

const PARAM_DEFS = {
  type: 'optimus',
  params: [
    { name: 'timeframe', type: 'string', default: '1h' },
    { name: 'position_size_pct', type: 'number', default: 10 },
  ],
}

const EXCHANGES = {
  exchanges: [
    { key: 'binance', label: 'Binance', status: 'active' },
    { key: 'okx', label: 'OKX', status: 'active' },
  ],
}

// Mutable state so lifecycle actions survive React Query refetches.
let instancesState: any[] = JSON.parse(JSON.stringify(INSTANCES))
function resetInstancesState() {
  instancesState = JSON.parse(JSON.stringify(INSTANCES))
}

const ANALYTICS = {
  bot: INSTANCES[0],
  snapshots: [
    {
      id: 1,
      bot_instance_id: 'bot-001',
      total_equity: 10120.5,
      unrealized_pnl: 120.5,
      realized_pnl: 80,
      total_return_pct: 2.05,
      timestamp: Math.floor(Date.now() / 1000) - 86400,
    },
    {
      id: 2,
      bot_instance_id: 'bot-001',
      total_equity: 10200,
      unrealized_pnl: 200,
      realized_pnl: 80,
      total_return_pct: 2.8,
      timestamp: Math.floor(Date.now() / 1000),
    },
  ],
}

const TRADES = {
  bot: INSTANCES[0],
  trades: [
    {
      id: 1,
      bot_instance_id: 'bot-001',
      symbol: 'BTCUSDT',
      side: 'buy',
      entry_price: 65000,
      exit_price: 66000,
      quantity: 0.1,
      pnl: 100,
      pnl_pct: 1.54,
      close_reason: 'tp',
      opened_at: Date.now() - 172800000,
      closed_at: Date.now() - 86400000,
    },
  ],
}

// ── Helpers ──

function json(body: unknown) {
  return { status: 200, contentType: 'application/json', body: JSON.stringify(body) }
}

async function mockAIBotAPIs(page: any) {
  // Note: append `**` to account for React Query cache-busting query strings.
  await page.route('**/api/ai-bots/catalog**', async (route: any) => route.fulfill(json(CATALOG)))
  await page.route('**/api/ai-bots/catalog/**', async (route: any) => route.fulfill(json(CATALOG[0])))
  await page.route('**/api/ai-bots/instances**', async (route: any) => {
    if (route.request().method() === 'POST') {
      const body = await route.request().postDataJSON()
      const created = { id: 'bot-new-001', ...body, status: 'stopped', user_id: 1 }
      instancesState.push(created)
      return route.fulfill(json(created))
    }
    return route.fulfill(json(instancesState))
  })
  await page.route('**/api/ai-bots/instances/*/analytics**', async (route: any) => route.fulfill(json(ANALYTICS)))
  await page.route('**/api/ai-bots/instances/*/trades**', async (route: any) => route.fulfill(json(TRADES)))
  await page.route('**/api/ai-bots/subscriptions**', async (route: any) => route.fulfill(json(SUBSCRIPTIONS)))
  await page.route('**/api/ai-bots/subscriptions/*/cancel**', async (route: any) => route.fulfill(json({ id: 2, status: 'cancelled' })))

  // Social / provider endpoints must match the wrapper shape returned by socialApi.
  await page.route('**/api/social/providers**', async (route: any) => route.fulfill(json({ providers: PROVIDERS })))
  await page.route('**/api/social/signals**', async (route: any) => route.fulfill(json({ signals: [] })))
  await page.route('**/api/social/followers/configs**', async (route: any) => {
    if (route.request().method() === 'POST' || route.request().method() === 'PUT') {
      return route.fulfill(json({ success: true }))
    }
    return route.fulfill(json({ configs: FOLLOWER_CONFIGS }))
  })
  await page.route('**/api/social/providers/*/follow**', async (route: any) => route.fulfill(json({ success: true })))
  await page.route('**/api/social/providers/*/unfollow**', async (route: any) => route.fulfill(json({ success: true })))

  await page.route('**/api/strategies/param-defs**', async (route: any) => route.fulfill(json(PARAM_DEFS)))
  await page.route('**/api/config/exchanges**', async (route: any) => route.fulfill(json(EXCHANGES)))

  // Generic instance handler (GET/PUT/DELETE) — registered BEFORE lifecycle routes
  // so that lifecycle routes take precedence.
  await page.route('**/api/ai-bots/instances/*', async (route: any) => {
    const url = route.request().url()
    const id = url.split('/').slice(-1)[0].split('?')[0]
    const instance = instancesState.find((b) => b.id === id) || instancesState[0]
    if (route.request().method() === 'DELETE') {
      instancesState = instancesState.filter((b) => b.id !== id)
      return route.fulfill(json({ id }))
    }
    if (route.request().method() === 'PUT') {
      const body = await route.request().postDataJSON()
      const idx = instancesState.findIndex((b) => b.id === id)
      if (idx >= 0) instancesState[idx] = { ...instance, ...body }
      return route.fulfill(json({ ...instance, ...body }))
    }
    return route.fulfill(json(instance))
  })
  await page.route('**/api/ai-bots/instances/batch-start**', async (route: any) => {
    const body = await route.request().postDataJSON()
    const ids = body.ids || []
    ids.forEach((id: string) => {
      const idx = instancesState.findIndex((b) => b.id === id)
      if (idx >= 0) instancesState[idx] = { ...instancesState[idx], status: 'running' }
    })
    return route.fulfill(json({ success: true, started: ids.length }))
  })
  await page.route('**/api/ai-bots/instances/batch-stop**', async (route: any) => {
    const body = await route.request().postDataJSON()
    const ids = body.ids || []
    ids.forEach((id: string) => {
      const idx = instancesState.findIndex((b) => b.id === id)
      if (idx >= 0) instancesState[idx] = { ...instancesState[idx], status: 'stopped' }
    })
    return route.fulfill(json({ success: true, stopped: ids.length }))
  })
  await page.route('**/api/ai-bots/instances/batch-delete**', async (route: any) => {
    const body = await route.request().postDataJSON()
    const ids = body.ids || []
    instancesState = instancesState.filter((b) => !ids.includes(b.id))
    return route.fulfill(json({ success: true, deleted: ids.length }))
  })

  // Lifecycle mutating endpoints return the updated instance and mutate state.
  // Registered LAST so they are checked FIRST by Playwright.
  const lifecycle = (status: string) => async (route: any) => {
    const url = route.request().url()
    const id = url.split('/').slice(-2)[0].split('?')[0]
    const idx = instancesState.findIndex((b) => b.id === id)
    if (idx >= 0) {
      instancesState[idx] = { ...instancesState[idx], status }
    }
    route.fulfill(json(instancesState[idx] || instancesState[0]))
  }
  await page.route('**/api/ai-bots/instances/*/start**', lifecycle('running'))
  await page.route('**/api/ai-bots/instances/*/pause**', lifecycle('paused'))
  await page.route('**/api/ai-bots/instances/*/resume**', lifecycle('running'))
  await page.route('**/api/ai-bots/instances/*/stop**', lifecycle('stopped'))
  await page.route('**/api/ai-bots/instances/*/clone**', async (route: any) => {
    const url = route.request().url()
    const id = url.split('/').slice(-2)[0].split('?')[0]
    const instance = instancesState.find((b) => b.id === id) || instancesState[0]
    const cloned = { ...instance, id: `${instance.id}-clone`, name: `${instance.name} 副本`, status: 'stopped' }
    instancesState.push(cloned)
    route.fulfill(json(cloned))
  })
}

function setupDialogHandler(page: any) {
  page.on('dialog', (dialog: any) => dialog.accept())
}

function setupWebsocketHandler(page: any) {
  page.on('websocket', (ws: any) => {
    try { ws.close() } catch { /* ignore */ }
  })
}

function botCard(page: any, name: string) {
  return page.locator('.rounded-xl').filter({ hasText: name })
}

// ── Tests ──

test.describe('AI Bots Page', () => {
  test.beforeEach(async ({ authPage }) => {
    resetInstancesState()
    await mockAIBotAPIs(authPage)
    setupDialogHandler(authPage)
    setupWebsocketHandler(authPage)
    await authPage.goto('/ai-bots')
    await authPage.waitForLoadState('domcontentloaded')
    await authPage.waitForTimeout(800)
  })

  test('page loads with all 5 tabs', async ({ authPage }) => {
    await expect(authPage.locator('h1:has-text("AI Bots")')).toBeVisible()
    for (const label of ['机器人市场', '信号源市场', '我的机器人', '数据分析', '订阅管理']) {
      await expect(authPage.locator(`button:has-text("${label}")`).first()).toBeVisible()
    }
  })

  test('tab switching updates URL query param', async ({ authPage }) => {
    await authPage.click('button:has-text("我的机器人")')
    await authPage.waitForURL('**/ai-bots?tab=mybots')
    await authPage.click('button:has-text("数据分析")')
    await authPage.waitForURL('**/ai-bots?tab=analytics')
  })

  test('marketplace renders catalog cards', async ({ authPage }) => {
    await expect(authPage.locator('text=Optimus 稳定现货')).toBeVisible()
    await expect(authPage.locator('text=AI Alpha Futures')).toBeVisible()
    await expect(authPage.locator('text=Terminator Volatility')).toBeVisible()
  })

  test('marketplace search narrows results', async ({ authPage }) => {
    await authPage.fill('input[placeholder*="搜索机器人"]', 'Optimus')
    await expect(authPage.locator('text=Optimus 稳定现货')).toBeVisible()
    await expect(authPage.locator('text=AI Alpha Futures')).not.toBeVisible()
  })

  test('marketplace filters by market type', async ({ authPage }) => {
    await authPage.click('button:has-text("合约")')
    await expect(authPage.locator('text=AI Alpha Futures')).toBeVisible()
    await expect(authPage.locator('text=Optimus 稳定现货')).not.toBeVisible()
  })

  test('marketplace filters by risk level', async ({ authPage }) => {
    await authPage.click('button:has-text("高风险")')
    await expect(authPage.locator('text=Terminator Volatility')).toBeVisible()
    await expect(authPage.locator('text=Optimus 稳定现货')).not.toBeVisible()
  })

  test('deploy button opens create wizard with catalog pre-selected', async ({ authPage }) => {
    await botCard(authPage, 'Optimus 稳定现货').getByRole('button', { name: '部署' }).click()
    await expect(authPage.locator('text=创建 AI Bot')).toBeVisible()
    await expect(authPage.locator('text=Optimus 稳定现货')).toBeVisible()
  })
})

test.describe('My Bots', () => {
  test.beforeEach(async ({ authPage }) => {
    resetInstancesState()
    await mockAIBotAPIs(authPage)
    setupDialogHandler(authPage)
    setupWebsocketHandler(authPage)
    await authPage.goto('/ai-bots?tab=mybots')
    await authPage.waitForLoadState('domcontentloaded')
    await authPage.waitForTimeout(800)
  })

  test('renders KPI cards and instance list', async ({ authPage }) => {
    await expect(authPage.locator('text=机器人列表')).toBeVisible()
    await expect(authPage.locator('text=运行中').first()).toBeVisible()
    await expect(authPage.locator('text=最佳收益')).toBeVisible()
    await expect(authPage.locator('text=我的 Optimus')).toBeVisible()
    await expect(authPage.locator('text=ETH 量化一号')).toBeVisible()
  })

  test('pause and resume a running bot', async ({ authPage }) => {
    await botCard(authPage, '我的 Optimus').getByRole('button', { name: '暂停' }).click()
    await expect(botCard(authPage, '我的 Optimus').getByText('暂停')).toBeVisible({ timeout: 5000 })
    await botCard(authPage, '我的 Optimus').getByRole('button', { name: '恢复' }).click()
    await expect(botCard(authPage, '我的 Optimus').getByText('运行中')).toBeVisible({ timeout: 5000 })
  })

  test('start a stopped bot', async ({ authPage }) => {
    await botCard(authPage, 'ETH 量化一号').getByRole('button', { name: '启动' }).click()
    await expect(botCard(authPage, 'ETH 量化一号').getByText('运行中')).toBeVisible({ timeout: 5000 })
  })

  test('stop a running bot', async ({ authPage }) => {
    await botCard(authPage, '我的 Optimus').getByRole('button', { name: '停止' }).click()
    await expect(botCard(authPage, '我的 Optimus').getByText('已停止')).toBeVisible({ timeout: 5000 })
  })

  test('clone a bot', async ({ authPage }) => {
    await botCard(authPage, 'ETH 量化一号').getByRole('button', { name: '克隆' }).click()
    await expect(authPage.locator('text=ETH 量化一号 副本')).toBeVisible({ timeout: 5000 })
  })

  test('delete a stopped bot', async ({ authPage }) => {
    await botCard(authPage, 'ETH 量化一号').getByRole('button', { name: '删除' }).click()
    await expect.poll(async () => await authPage.locator('text=ETH 量化一号').count()).toBe(0)
  })

  test('batch start stopped bots', async ({ authPage }) => {
    // Select ETH bot by clicking its checkbox
    const checkbox = botCard(authPage, 'ETH 量化一号').locator('button').first()
    await checkbox.click()
    await authPage.click('button:has-text("批量启动")')
    await expect(botCard(authPage, 'ETH 量化一号').getByText('运行中')).toBeVisible({ timeout: 5000 })
  })
})

test.describe('Create / Edit Wizard', () => {
  test.beforeEach(async ({ authPage }) => {
    resetInstancesState()
    await mockAIBotAPIs(authPage)
    setupDialogHandler(authPage)
    setupWebsocketHandler(authPage)
    await authPage.goto('/ai-bots?tab=mybots')
    await authPage.waitForLoadState('domcontentloaded')
    await authPage.waitForTimeout(800)
  })

  test('create custom bot through 4-step wizard', async ({ authPage }) => {
    await authPage.click('button:has-text("创建机器人")')
    await expect(authPage.locator('text=创建 AI Bot')).toBeVisible()

    // Step 1: choose custom source (default)
    await expect(authPage.locator('button:has-text("自定义机器人")')).toBeVisible()
    await authPage.click('button:has-text("下一步")')

    // Step 2: name & symbol
    await authPage.fill('input[placeholder*="BTC 网格策略"]', '测试机器人')
    await authPage.fill('input[placeholder="BTCUSDT"]', 'BTCUSDT')
    await authPage.click('button:has-text("下一步")')

    // Step 3: params
    await authPage.fill('input[type="number"]', '20000')
    await authPage.click('button:has-text("下一步")')

    // Step 4: confirm
    await expect(authPage.locator('text=测试机器人')).toBeVisible()
    await expect(authPage.locator('text=BTCUSDT').first()).toBeVisible()
    await authPage.click('button:has-text("部署机器人")')

    await expect(authPage.locator('text=测试机器人')).toBeVisible({ timeout: 5000 })
  })

  test('edit bot opens wizard with pre-filled data', async ({ authPage }) => {
    await botCard(authPage, 'ETH 量化一号').getByRole('button', { name: '编辑' }).click()
    await expect(authPage.locator('text=编辑 AI Bot')).toBeVisible()
    await expect(authPage.locator('input[value="ETH 量化一号"]')).toBeVisible()
  })
})

test.describe('Analytics & Subscriptions & Providers', () => {
  test.beforeEach(async ({ authPage }) => {
    resetInstancesState()
    await mockAIBotAPIs(authPage)
    setupDialogHandler(authPage)
    setupWebsocketHandler(authPage)
    await authPage.goto('/ai-bots')
    await authPage.waitForLoadState('domcontentloaded')
    await authPage.waitForTimeout(800)
  })

  test('analytics tab renders KPIs and tables', async ({ authPage }) => {
    await authPage.click('button:has-text("数据分析")')
    await authPage.waitForURL('**/ai-bots?tab=analytics')
    await authPage.waitForTimeout(800)
    await expect(authPage.locator('text=总收益率').first()).toBeVisible()
    await expect(authPage.locator('text=交易记录')).toBeVisible()
  })

  test('subscriptions tab lists items', async ({ authPage }) => {
    await authPage.click('button:has-text("订阅管理")')
    await authPage.waitForURL('**/ai-bots?tab=subscriptions')
    await authPage.waitForTimeout(800)
    await expect(authPage.locator('text=月费')).toBeVisible()
    await expect(authPage.locator('text=盈利分成')).toBeVisible()
  })

  test('provider market renders providers and follow toggle', async ({ authPage }) => {
    await authPage.click('button:has-text("信号源市场")')
    await authPage.waitForURL('**/ai-bots?tab=providers')
    await authPage.waitForTimeout(800)
    await expect(authPage.locator('text=Crypto Alpha')).toBeVisible()
    await expect(authPage.locator('text=Quant Signals')).toBeVisible()
  })
})
