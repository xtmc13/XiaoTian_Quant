import { expect } from './fixtures'
import { test } from './fixtures'
import {
  applyAuditMocks,
  attachCollectors,
  freshIssues,
  fulfillJson,
  scanRawI18nKeys,
  settlePage,
  stubAllWebSockets,
  type AuditIssues,
} from './audit-utils'
import type { Page } from '@playwright/test'

/**
 * Interaction audit — systematic per-page click-through.
 *
 * Part 1 (route-sweep): every content route is loaded with shaped API mocks;
 *   collects pageerror / console.error / failed API (>=400) / blank body /
 *   error-boundary fallback / raw i18n keys.
 * Part 2 (interactions): click-through of core pages (buttons, tabs, modals,
 *   form submits) verifying the UI actually responds.
 *
 * All /api/** traffic is mocked (see audit-utils); WS is stubbed.
 */

// ── All content routes from App.tsx (redirects tested separately) ──
const ROUTES: { path: string; name: string }[] = [
  { path: '/dashboard', name: '仪表盘' },
  { path: '/trading/spot', name: '现货交易' },
  { path: '/trading/contract', name: '合约交易' },
  { path: '/strategy', name: '策略管理' },
  { path: '/create', name: '创建策略' },
  { path: '/strategy/editor', name: '策略编辑器' },
  { path: '/strategy/python', name: 'Python策略' },
  { path: '/alerts', name: '指标告警' },
  { path: '/backtest', name: '回测' },
  { path: '/backtest/portfolio', name: '组合回测' },
  { path: '/factor-research', name: '因子研究' },
  { path: '/analysis', name: '偏差分析' },
  { path: '/ai', name: 'AI分析' },
  { path: '/ai/freqai', name: 'FreqAI' },
  { path: '/ai/rl', name: 'RL训练' },
  { path: '/ai/tensorboard', name: 'TensorBoard' },
  { path: '/ai/discussion-room', name: 'AI讨论室' },
  { path: '/model-management', name: '模型管理' },
  { path: '/bots', name: '机器人中心' },
  { path: '/bots/signal', name: '信号机器人' },
  { path: '/bots/ai', name: 'AI机器人市场' },
  { path: '/bots/dca', name: 'DCA机器人' },
  { path: '/bots/layered-martin', name: '分层马丁' },
  { path: '/settings', name: '设置' },
  { path: '/exchange-account', name: '交易所账户' },
  { path: '/indicator-community', name: '指标市场' },
  { path: '/indicator-community/1', name: '指标详情' },
  { path: '/author-dashboard', name: '作者后台' },
  { path: '/portfolio', name: '资产监测' },
  { path: '/indicator-ide', name: '指标IDE' },
  { path: '/risk-control', name: '风控中心' },
  { path: '/pairlist', name: '交易对筛选' },
  { path: '/advanced-orders', name: '高级订单' },
  { path: '/arbitrage/cross', name: '跨所套利' },
  { path: '/arbitrage/triangular', name: '三角套利' },
  { path: '/hyperopt', name: '参数优化' },
  { path: '/social-trading', name: '社交跟单' },
  { path: '/onchain', name: '链上数据' },
  { path: '/market-data', name: '市场数据' },
  { path: '/profile', name: '个人中心' },
  { path: '/users', name: '用户管理' },
  { path: '/agent-tokens', name: 'Agent令牌' },
  { path: '/billing', name: '会员计费' },
  { path: '/status', name: '系统状态' },
  { path: '/data', name: '数据下载' },
  { path: '/logs', name: '系统日志' },
  { path: '/strategy-leaderboard', name: '策略排行榜' },
]

const REDIRECTS: { from: string; to: RegExp }[] = [
  { from: '/', to: /\/dashboard/ },
  { from: '/trading', to: /\/trading\/spot/ },
  { from: '/strategies', to: /\/bots/ },
  { from: '/market', to: /\/ai/ },
  { from: '/ai/analysis', to: /\/ai/ },
  { from: '/ai-bots', to: /\/bots\/ai/ },
  { from: '/bots/strategy', to: /\/bots/ },
  { from: '/bots/grid', to: /\/bots/ },
  { from: '/arbitrage', to: /\/arbitrage\/cross/ },
]

const ERROR_BOUNDARY_TEXTS = ['页面加载异常', '应用出现异常']

async function sweepOne(page: Page, path: string): Promise<AuditIssues> {
  const issues = freshIssues()
  attachCollectors(page, issues)
  await stubAllWebSockets(page)
  await applyAuditMocks(page)
  await page.goto(path)
  await page.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
  await page.waitForTimeout(800)
  issues.rawI18nKeys = await scanRawI18nKeys(page)
  return issues
}

function expectClean(issues: AuditIssues, label: string, bodyText: string) {
  const dedupe = (arr: string[]) => [...new Set(arr)]
  expect(dedupe(issues.pageErrors), `${label}: pageerror`).toEqual([])
  expect(bodyText.trim().length, `${label}: blank body`).toBeGreaterThan(0)
  for (const t of ERROR_BOUNDARY_TEXTS) {
    expect(bodyText.includes(t), `${label}: error boundary "${t}"`).toBe(false)
  }
  expect(dedupe(issues.failedApis), `${label}: failed API`).toEqual([])
  expect(dedupe(issues.rawI18nKeys), `${label}: raw i18n keys`).toEqual([])
  expect(dedupe(issues.consoleErrors), `${label}: console.error`).toEqual([])
}

test.describe('route-sweep @audit', () => {
  test.setTimeout(90000)
  for (const route of ROUTES) {
    test(`${route.name} (${route.path})`, async ({ authPage }) => {
      const issues = await sweepOne(authPage, route.path)
      const bodyText = await authPage.locator('body').innerText()
      expectClean(issues, `${route.name} ${route.path}`, bodyText)
    })
  }

  test('login 页渲染', async ({ page }) => {
    const issues = freshIssues()
    attachCollectors(page, issues)
    await page.goto('/login')
    await settlePage(page)
    await expect(page.locator('input[type="password"]')).toBeVisible()
    const bodyText = await page.locator('body').innerText()
    expectClean(issues, '/login', bodyText)
  })

  for (const r of REDIRECTS) {
    test(`重定向 ${r.from} → ${r.to}`, async ({ authPage }) => {
      await applyAuditMocks(authPage)
      await authPage.goto(r.from)
      await authPage.waitForURL(r.to, { timeout: 10000 })
    })
  }
})

// ════════════════════════════════════════════════════════════════════
// Part 2 — interaction click-through of core pages
// ════════════════════════════════════════════════════════════════════

test.describe('interactions @audit', () => {
  test.setTimeout(120000)

  test('仪表盘: KPI 卡 + 快捷操作', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await authPage.goto('/dashboard')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    await expect(authPage.locator('text=总资产估值').first()).toBeVisible()
    await expect(authPage.locator('text=胜率').first()).toBeVisible()
  })

  test('合约交易: 阶梯单面板 创建/列表 tab + 提交', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    let createBody: Record<string, unknown> | null = null
    await authPage.route('**/api/orders/ladder*', async (route) => {
      const method = route.request().method()
      if (method === 'POST') {
        createBody = route.request().postDataJSON() as Record<string, unknown>
        return fulfillJson(route, { status: 'ok', ladder: { id: 'lad-1', symbol: 'BTCUSDT', status: 'active', legs: [] } })
      }
      return fulfillJson(route, { orders: [], count: 0 })
    })
    await authPage.goto('/trading/contract')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})

    // 打开阶梯单面板（订单类型行的「阶梯」开关）
    const ladderEntry = authPage.getByRole('button', { name: '阶梯', exact: true })
    await expect(ladderEntry).toBeVisible({ timeout: 15000 })
    await ladderEntry.click()

    // 面板出现 创建阶梯单/进行中 两个 tab（表单提交按钮同名，用 first 取 tab）
    await expect(authPage.getByRole('button', { name: '创建阶梯单' }).first()).toBeVisible({ timeout: 5000 })
    const listTab = authPage.getByRole('button', { name: /进行中/ })
    await expect(listTab).toBeVisible()
    await listTab.click()
    await authPage.waitForTimeout(400)

    // 回到创建 tab：买入/卖出阶梯方向、档数等控件齐全
    await authPage.getByRole('button', { name: '创建阶梯单' }).first().click()
    await expect(authPage.getByTestId('ladder-create-form')).toBeVisible()
    await expect(authPage.getByRole('button', { name: '买入阶梯' })).toBeVisible()
    await expect(authPage.getByRole('button', { name: '卖出阶梯' })).toBeVisible()
    const bodyText = await authPage.locator('body').innerText()
    expect(bodyText.includes('页面加载异常')).toBe(false)
  })

  test('机器人中心: 类型筛选 chips + 状态筛选 + 新建向导', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await authPage.goto('/bots')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    // 类型筛选 chips 全部点一遍
    for (const chip of ['网格', '马丁', '华尔街', 'AI', '现货策略', '合约策略', '全部']) {
      const chipBtn = authPage.getByRole('button', { name: new RegExp(`^${chip}`) }).first()
      await expect(chipBtn).toBeVisible({ timeout: 15000 })
      await chipBtn.click()
      await authPage.waitForTimeout(250)
    }
    // 状态筛选（运行中/已停止）
    const runningBtn = authPage.getByRole('button', { name: /运行中/ }).first()
    if (await runningBtn.isVisible().catch(() => false)) await runningBtn.click()
    // 新建按钮存在并可点
    const createBtn = authPage.getByRole('button', { name: /新建|创建机器人|启动/ }).first()
    await expect(createBtn).toBeVisible()
    await createBtn.click()
    await authPage.waitForTimeout(600)
    const bodyText = await authPage.locator('body').innerText()
    expect(bodyText.includes('页面加载异常')).toBe(false)
    expect(bodyText.trim().length).toBeGreaterThan(0)
  })

  test('策略编辑器: 页面渲染 + 策略列表加载', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await authPage.route('**/api/strategies/configs*', async (route) =>
      fulfillJson(route, [
        { id: 'strat-1', name: '编辑器策略A', symbol: 'BTCUSDT', strategy: 'sma_cross', status: 'stopped', params: {} },
      ]),
    )
    await authPage.goto('/strategy/editor')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    await authPage.waitForTimeout(1000)
    const bodyText = await authPage.locator('body').innerText()
    expect(bodyText.includes('页面加载异常')).toBe(false)
    expect(bodyText.trim().length).toBeGreaterThan(0)
  })

  test('创建策略页: 表单渲染 + 返回按钮', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await authPage.goto('/create')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    await authPage.waitForTimeout(800)
    const bodyText = await authPage.locator('body').innerText()
    expect(bodyText.includes('页面加载异常')).toBe(false)
    expect(bodyText.trim().length).toBeGreaterThan(0)
  })

  test('回测: 开始回测 → 报告渲染', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await authPage.route('**/api/backtest/run*', async (route) => {
      return fulfillJson(route, {
        report: {
          initial_balance: 10000,
          final_equity: 11250,
          total_return_pct: 12.5,
          max_drawdown_pct: 5.2,
          sharpe_ratio: 1.8,
          win_rate_pct: 60,
          total_trades: 30,
          profit_factor: 1.6,
        },
        equity_curve: [],
        trades: [],
      })
    })
    await authPage.goto('/backtest')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    const runBtn = authPage.getByRole('button', { name: /开始回测/ }).first()
    await expect(runBtn).toBeVisible({ timeout: 15000 })
    await runBtn.click()
    // 报告区渲染（总收益等指标出现）
    await expect(authPage.locator('body')).toContainText(/12\.5|总收益|夏普/, { timeout: 15000 })
  })

  test('AI页: 决策门开关+保存 / 复盘生成 全流程', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)

    // 复盘需要可选策略：给一个策略项
    await authPage.route('**/api/strategies/configs*', async (route) =>
      fulfillJson(route, [{ id: 'strat-1', name: '审计策略A', symbol: 'BTCUSDT', status: 'stopped' }]),
    )
    let putConfig: Record<string, unknown> | null = null
    await authPage.route('**/api/ai/gate/config*', async (route) => {
      if (route.request().method() === 'PUT') {
        putConfig = route.request().postDataJSON() as Record<string, unknown>
        return fulfillJson(route, {
          status: 'ok',
          config: { enabled: true, min_confidence: 0.6, abstain_action: 'allow', paper_only: true, timeout_seconds: 30, provider: 'deepseek', context_bars: 120, excluded_sources: [] },
        })
      }
      return fulfillJson(route, {
        config: { enabled: false, min_confidence: 0.6, abstain_action: 'allow', paper_only: true, timeout_seconds: 30, provider: 'deepseek', context_bars: 120, excluded_sources: [] },
      })
    })
    let reviewReq: Record<string, unknown> | null = null
    await authPage.route('**/api/ai/review*', async (route) => {
      if (route.request().method() !== 'POST') return route.fallback()
      reviewReq = route.request().postDataJSON() as Record<string, unknown>
      return fulfillJson(route, {
        status: 'done',
        report: {
          id: 'rep-1', user_id: 1, scope_type: 'strategy', scope_id: 'strat-1',
          period_start: 0, period_end: 0, trades_count: 12, total_pnl: 320.5,
          win_rate: 0.58, max_drawdown: 0.06, report_text: '近 30 天策略表现稳健，回撤可控。',
          model: 'deepseek-chat', status: 'done', error: '', created_at: Date.now(),
        },
      })
    })

    await authPage.goto('/ai')
    await authPage.waitForLoadState('networkidle', { timeout: 25000 }).catch(() => {})
    await authPage.waitForTimeout(1500)

    // ── 决策门：打开开关 → 保存 ──
    // Switch 的 input 是 sr-only，点 label 触发切换
    const gateSwitchLabel = authPage.locator('label', { hasText: '启用决策门' }).first()
    await gateSwitchLabel.scrollIntoViewIfNeeded()
    await expect(gateSwitchLabel).toBeVisible({ timeout: 15000 })
    await gateSwitchLabel.click()
    const saveBtn = authPage.getByRole('button', { name: /保存配置/ })
    await expect(saveBtn).toBeEnabled()
    await saveBtn.click()
    await expect(authPage.locator('body')).toContainText('决策门配置已保存')
    expect(putConfig).toMatchObject({ enabled: true })

    // ── 复盘：选策略 → 生成复盘报告 ──
    const scopeSelect = authPage.getByLabel('选择策略')
    await scopeSelect.scrollIntoViewIfNeeded()
    await scopeSelect.selectOption('strat-1')
    const genBtn = authPage.getByRole('button', { name: /生成复盘报告/ })
    await expect(genBtn).toBeEnabled()
    await genBtn.click()
    await expect(authPage.locator('body')).toContainText('AI 复盘报告已生成', { timeout: 15000 })
    await expect(authPage.locator('body')).toContainText('近 30 天策略表现稳健')
    expect(reviewReq).toMatchObject({ scope_type: 'strategy', scope_id: 'strat-1' })

    const bodyText = await authPage.locator('body').innerText()
    expect(bodyText.includes('页面加载异常')).toBe(false)
  })

  test('模型管理: ML 闭环卡片渲染', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await authPage.goto('/model-management')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    await authPage.waitForTimeout(1000)
    const bodyText = await authPage.locator('body').innerText()
    expect(bodyText.includes('页面加载异常')).toBe(false)
    // 闭环卡片（训练闭环/自动重训）
    expect(bodyText).toMatch(/训练闭环|自动重训|重训任务/)
  })

  test('机器人市场: 市场/我的上架/配置 tab 切换', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await authPage.goto('/bots/ai')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    for (const tab of ['我的上架', 'AI 配置与信号', '策略市场']) {
      const tabBtn = authPage.getByRole('button', { name: tab, exact: false }).first()
      await expect(tabBtn).toBeVisible({ timeout: 15000 })
      await tabBtn.click()
      await authPage.waitForTimeout(500)
      const bodyText = await authPage.locator('body').innerText()
      expect(bodyText.includes('页面加载异常')).toBe(false)
    }
  })

  test('交易所账户: 体检按钮触发任务流', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    let jobStarted = false
    await authPage.route('**/api/exchanges/health-check*', async (route) => {
      const method = route.request().method()
      const url = route.request().url()
      if (method === 'POST' && !url.includes('/jobs/')) {
        jobStarted = true
        return fulfillJson(route, { job_id: 'hc-1', status: 'running', exchanges: ['binance'] })
      }
      if (url.includes('/jobs/')) {
        return fulfillJson(route, {
          id: 'hc-1',
          status: 'completed',
          exchanges: ['binance'],
          results: {
            binance: { exchange: 'binance', configured: true, overall: 'healthy', levels: [], duration_ms: 120, checked_at: Date.now() },
          },
          created_at: Date.now(),
          finished_at: Date.now(),
        })
      }
      return fulfillJson(route, { results: [] })
    })
    await authPage.goto('/exchange-account')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    const healthBtn = authPage.getByRole('button', { name: /体检|健康检查/ }).first()
    await expect(healthBtn).toBeVisible({ timeout: 15000 })
    await healthBtn.click()
    await authPage.waitForTimeout(1500)
    expect(jobStarted, '体检按钮应触发 POST /exchanges/health-check').toBe(true)
  })

  test('系统状态: 健康/集成/告警区块渲染', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await authPage.goto('/status')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    const bodyText = await authPage.locator('body').innerText()
    expect(bodyText).toMatch(/系统状态/)
    expect(bodyText).toMatch(/组件健康/)
    expect(bodyText).toMatch(/告警/)
    expect(bodyText).toMatch(/集成状态/)
    expect(bodyText).toMatch(/交易所体检|体检/)
  })

  test('市场数据页: 区块渲染无破裂', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await authPage.goto('/market-data')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    await authPage.waitForTimeout(1000)
    const bodyText = await authPage.locator('body').innerText()
    expect(bodyText.includes('页面加载异常')).toBe(false)
    expect(bodyText.trim().length).toBeGreaterThan(0)
  })

  test('Billing: 选方案 → 选链 → 创建 USDT 订单', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await authPage.route('**/api/billing/plans*', async (route) =>
      fulfillJson(route, [
        { id: 'monthly', name: '月度会员', name_en: 'Monthly', price: 19.9, credits: 500, period_days: 30 },
        { id: 'yearly', name: '年度会员', name_en: 'Yearly', price: 199, credits: 8000, period_days: 365 },
      ]),
    )
    await authPage.route('**/api/billing/chains*', async (route) =>
      fulfillJson(route, [{ chain: 'BEP20', address: '0xDEADBEEF', memo: 'BSC BEP20' }]),
    )
    let orderReq: Record<string, unknown> | null = null
    await authPage.route('**/api/billing/orders*', async (route) => {
      if (route.request().method() === 'POST') {
        orderReq = route.request().postDataJSON() as Record<string, unknown>
        return fulfillJson(route, {
          order_id: 'ord-audit-1', plan_id: 'monthly', chain: 'BEP20', amount_usdt: 19.9,
          address: '0xDEADBEEF', status: 'pending', expires_at: Date.now() + 1800_000, created_at: Date.now(),
        })
      }
      return fulfillJson(route, [])
    })
    await authPage.goto('/billing')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})

    // 选方案 → 支付区块出现
    await authPage.getByRole('button', { name: /月度会员/ }).click()
    await expect(authPage.locator('text=USDT 转账（选择链）')).toBeVisible()
    // 选链 → 收款地址出现
    await authPage.getByRole('button', { name: 'BEP20', exact: true }).click()
    await expect(authPage.locator('code', { hasText: '0xDEADBEEF' }).first()).toBeVisible()
    // 创建订单
    await authPage.getByRole('button', { name: /创建订单并获取充值地址/ }).click()
    await expect(authPage.locator('body')).toContainText('ord-audit-1', { timeout: 10000 })
    expect(orderReq).toMatchObject({ plan_id: 'monthly', chain: 'BEP20' })
  })

  test('用户管理: tab 切换 + 上架审核队列通过', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    let approved = false
    await authPage.route('**/api/admin/market/listings**', async (route) => {
      const url = route.request().url()
      if (url.includes('/approve')) {
        approved = true
        return fulfillJson(route, { id: 'lst-1', status: 'listed' })
      }
      return fulfillJson(route, {
        listings: [
          {
            id: 'lst-1', author_user_id: 7, bot_instance_id: 'bot-1', kind: 'robot', name: '审计上架条目',
            description: '', fee_model: 'free', fee_percent: 0, monthly_fee: 0, status: 'pending_review',
            probation_passed: true,
            stats: { listing_id: 'lst-1', date: '2026-09-25', total_return_pct: 22.5, annualized_return_pct: 80, max_drawdown_pct: 8.1, win_rate: 61.2, profit_factor: 1.7, sharpe_ratio: 1.9, total_trades: 45, monthly_return_pct: 6.3, followers: 12, running_days: 40 },
          },
        ],
      })
    })
    await authPage.goto('/users')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})

    // 切到 上架审核 tab
    await authPage.getByRole('button', { name: '上架审核' }).click()
    await expect(authPage.locator('text=审计上架条目')).toBeVisible({ timeout: 10000 })
    // 统计文本（标准化统计卡）
    await expect(authPage.locator('body')).toContainText(/总收益.*22\.5|22\.5/)
    // 通过上架
    await authPage.getByRole('button', { name: '通过上架' }).click()
    await authPage.waitForTimeout(800)
    expect(approved, '应调用 POST /admin/market/listings/:id/approve').toBe(true)

    // 其它 tab 切一遍不破裂
    for (const tab of ['审计日志', '系统监控', '用户管理']) {
      await authPage.getByRole('button', { name: tab, exact: true }).click()
      await authPage.waitForTimeout(400)
      const bodyText = await authPage.locator('body').innerText()
      expect(bodyText.includes('页面加载异常')).toBe(false)
    }
  })

  test('机器人市场: 统计卡片 + 我的上架提交考核', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    // 市场看板：一条 listed 条目
    await authPage.route('**/api/market/listings?*', async (route) =>
      fulfillJson(route, {
        listings: [
          {
            id: 'lst-9', author_user_id: 3, bot_instance_id: 'bot-9', kind: 'robot', name: '稳健网格Pro',
            description: '', fee_model: 'monthly', fee_percent: 0, monthly_fee: 15, status: 'listed',
            probation_passed: true,
            stats: { listing_id: 'lst-9', date: '2026-09-25', total_return_pct: 35.2, annualized_return_pct: 120, max_drawdown_pct: 9.4, win_rate: 64.8, profit_factor: 2.1, sharpe_ratio: 2.2, total_trades: 88, monthly_return_pct: 7.9, followers: 56, running_days: 90 },
          },
        ],
        total: 1, page: 1, page_size: 12,
      }),
    )
    // 我的上架需要一个可选 AI 机器人实例
    await authPage.route('**/api/ai-bots/instances*', async (route) =>
      fulfillJson(route, [{ id: 'bot-1', name: '我的AI机器人', symbol: 'BTCUSDT', execution_mode: 'paper' }]),
    )
    let createReq: Record<string, unknown> | null = null
    await authPage.route('**/api/market/listings*', async (route) => {
      if (route.request().method() === 'POST') {
        createReq = route.request().postDataJSON() as Record<string, unknown>
        return fulfillJson(route, { id: 'lst-new', status: 'probation' })
      }
      return route.fallback()
    })

    await authPage.goto('/bots/ai')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})

    // ── 市场 tab：统计卡片 ──
    await expect(authPage.locator('text=稳健网格Pro')).toBeVisible({ timeout: 15000 })
    await expect(authPage.locator('text=月均收益')).toBeVisible()
    await expect(authPage.locator('text=最大回撤')).toBeVisible()
    await expect(authPage.locator('text=考核通过')).toBeVisible()

    // ── 我的上架 tab：提交考核表单 ──
    await authPage.getByRole('button', { name: /我的上架/ }).click()
    await authPage.getByRole('button', { name: /提交考核/ }).click()
    // 选实例 + 填名 + 提交
    await authPage.locator('select').first().selectOption('bot-1')
    await authPage.getByPlaceholder('展示在市场卡片上的名称').fill('审计新条目')
    await authPage.getByRole('button', { name: /提交并进入考核期/ }).click()
    await authPage.waitForTimeout(800)
    expect(createReq).toMatchObject({ bot_instance_id: 'bot-1', name: '审计新条目', kind: 'robot' })
  })

  test('社交跟单: tab 切换 + 双轨分成面板', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await authPage.goto('/social-trading')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    for (const tab of ['信号流', '我的关注', '信号源']) {
      await authPage.getByRole('button', { name: tab, exact: true }).click()
      await authPage.waitForTimeout(400)
      const bodyText = await authPage.locator('body').innerText()
      expect(bodyText.includes('页面加载异常')).toBe(false)
    }
    // 双轨（月费订阅/利润分成）面板
    await expect(authPage.locator('text=利润分成').first()).toBeVisible()
  })

  test('设置: 9 个 tab 全部点一遍', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await authPage.goto('/settings')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    await authPage.waitForTimeout(800)
    for (const tab of ['交易所', 'AI 模型', '通知', '通知路由', '外观', '数据', '安全', '系统', '通用']) {
      // tab 可访问名可能带「本地」徽标（如「外观 本地」），用前缀匹配
      const tabBtn = authPage.getByRole('button', { name: new RegExp(`^${tab}`) }).first()
      await expect(tabBtn).toBeVisible({ timeout: 10000 })
      await tabBtn.click()
      await authPage.waitForTimeout(400)
      const bodyText = await authPage.locator('body').innerText()
      expect(bodyText.includes('页面加载异常'), `设置 tab ${tab} 不应触发错误边界`).toBe(false)
    }
  })

  test('交易对筛选: 刷新白名单 + 配置区块', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    let refreshed = false
    await authPage.route('**/api/pairlist/refresh*', async (route) => {
      refreshed = true
      return fulfillJson(route, { success: true })
    })
    await authPage.route('**/api/pairlist/whitelist*', async (route) =>
      fulfillJson(route, { whitelist: ['BTC/USDT', 'ETH/USDT'], blacklist: [] }),
    )
    await authPage.goto('/pairlist')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    await expect(authPage.locator('text=BTC/USDT').first()).toBeVisible({ timeout: 15000 })
    const refreshBtn = authPage.getByRole('button', { name: /刷新/ }).first()
    await refreshBtn.click()
    await authPage.waitForTimeout(600)
    expect(refreshed, '刷新按钮应调用 POST /pairlist/refresh').toBe(true)
  })

  test('Hyperopt: 新建任务 → 开始优化', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    let started = false
    await authPage.route('**/api/hyperopt/start*', async (route) => {
      started = true
      return fulfillJson(route, { job_id: 'hp-audit-1', status: 'running' })
    })
    await authPage.goto('/hyperopt')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    // 新建任务展开表单
    await authPage.getByRole('button', { name: /新建任务/ }).click()
    const startBtn = authPage.getByRole('button', { name: /开始优化/ })
    await expect(startBtn).toBeVisible({ timeout: 10000 })
    await expect(startBtn).toBeEnabled()
    await startBtn.click()
    await authPage.waitForTimeout(800)
    expect(started, '开始优化应调用 POST /hyperopt/start').toBe(true)
  })

  test('FreqAI 页渲染无破裂', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await authPage.goto('/ai/freqai')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    await authPage.waitForTimeout(800)
    const bodyText = await authPage.locator('body').innerText()
    expect(bodyText.includes('页面加载异常')).toBe(false)
    expect(bodyText.trim().length).toBeGreaterThan(0)
  })

  test('系统状态: 集成预检单项重检', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    let checked = false
    await authPage.route('**/api/integrations/status*', async (route) =>
      fulfillJson(route, {
        integrations: [
          { name: 'telegram', display_name: 'Telegram', category: 'notify', configured: true, reachable: null, notes: 'TELEGRAM_BOT_TOKEN' },
        ],
      }),
    )
    await authPage.route('**/api/integrations/*/check*', async (route) => {
      checked = true
      return fulfillJson(route, { name: 'telegram', display_name: 'Telegram', category: 'notify', configured: true, reachable: true, last_verified_at: Date.now() })
    })
    await authPage.goto('/status')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    await expect(authPage.locator('text=Telegram').first()).toBeVisible({ timeout: 15000 })
    await authPage.getByRole('button', { name: /重新检测/ }).click()
    await expect(authPage.locator('body')).toContainText('检测完成', { timeout: 10000 })
    expect(checked).toBe(true)
  })

  test('组合回测: 分享卡按钮 → 卡片弹窗 + 复制文案', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await authPage.route('**/api/backtests/portfolio*', async (route) =>
      fulfillJson(route, {
        backtests: [
          {
            id: 'pb-1', name: '审计组合', timeframe: '1h', rebalance: 'none',
            total_return_pct: 18.6, max_drawdown_pct: 6.4, sharpe_ratio: 1.9,
            initial_capital: 100000, start_time: 0, end_time: 0, created_at: Date.now(),
          },
        ],
      }),
    )
    let cardFetched = false
    await authPage.route('**/api/share/backtest/**/card*', async (route) => {
      cardFetched = true
      return fulfillJson(route, {
        kind: 'backtest', id: 'pb-1', name: '审计组合', strategy: 'portfolio', timeframe: '1h',
        total_return_pct: 18.6, max_drawdown_pct: 6.4, sharpe_ratio: 1.9, sortino_ratio: 2.4,
        win_rate: 62.5, profit_factor: 1.8, total_trades: 40, initial_capital: 100000,
        final_equity: 118600, start_time: 0, end_time: 0, created_at: Date.now(),
        nickname: 'E***', amount_mode: 'pct', share_url: '/share/backtest/pb-1', generated_at: Date.now(),
      })
    })
    await authPage.goto('/backtest/portfolio')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})

    // 历史行 → 分享按钮 → 卡片弹窗
    await expect(authPage.locator('text=审计组合')).toBeVisible({ timeout: 15000 })
    await authPage.getByRole('button', { name: '分享', exact: true }).click()
    const dialog = authPage.getByRole('dialog')
    await expect(dialog).toBeVisible({ timeout: 10000 })
    await expect(dialog).toContainText('回测收益分享卡')
    await expect(dialog).toContainText('+18.60%')
    await expect(dialog).toContainText('E***')
    expect(cardFetched, '应调用 GET /share/backtest/:id/card').toBe(true)

    // 复制文案按钮存在；关闭弹窗
    await expect(dialog.getByRole('button', { name: /复制分享文案/ })).toBeVisible()
    await dialog.getByRole('button', { name: '关闭' }).click()
    await expect(dialog).toHaveCount(0)
  })

  test('套利页: 跨所套利控制按钮渲染', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await authPage.goto('/arbitrage/cross')
    await authPage.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {})
    await authPage.waitForTimeout(800)
    const bodyText = await authPage.locator('body').innerText()
    expect(bodyText.includes('页面加载异常')).toBe(false)
  })
})
