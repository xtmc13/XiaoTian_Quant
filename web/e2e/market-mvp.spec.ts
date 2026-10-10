import { test, expect } from './fixtures'
import { applyAuditMocks, fulfillJson, stubAllWebSockets } from './audit-utils'
import type { Page, BrowserContext } from '@playwright/test'

/**
 * 市场 MVP 闭环 E2E（全 mock，不依赖真实后端）：
 *  1. 浏览市场 → 卡片进详情 → 指标/曲线/成交表渲染 → free 订阅 → 我的订阅 tab 出现该订阅
 *  2. admin：上架审核队列 approve；提现审核队列打款（tx_hash）
 */

const LISTING = {
  id: 'ml-001',
  author_user_id: 9,
  bot_instance_id: 'ai-1',
  object_kind: 'ai_bot',
  object_id: 'ai-1',
  kind: 'robot',
  name: 'Alpha 趋势',
  description: '日线趋势跟踪策略',
  fee_model: 'free',
  fee_percent: 0,
  monthly_fee: 0,
  status: 'listed',
  probation_passed: true,
  stats: {
    listing_id: 'ml-001',
    date: '2026-10-09',
    total_return_pct: 45.6,
    annualized_return_pct: 120.5,
    max_drawdown_pct: 8.1,
    win_rate: 61.5,
    profit_factor: 1.8,
    sharpe_ratio: 1.6,
    total_trades: 87,
    monthly_return_pct: 12.3,
    followers: 12,
    running_days: 45,
  },
}

const SERIES = [
  { ...LISTING.stats, date: '2026-10-07', total_return_pct: 40.1 },
  { ...LISTING.stats, date: '2026-10-08', total_return_pct: 43.0 },
  LISTING.stats,
]

const TRADE = {
  id: 1,
  bot_instance_id: 'ai-1',
  symbol: 'BTCUSDT',
  side: 'LONG',
  entry_price: 60000,
  exit_price: 61500,
  quantity: 0.05,
  pnl: 75,
  pnl_pct: 2.5,
  close_reason: 'take_profit',
  opened_at: 1759900000,
  closed_at: 1759950000,
}

const nowSec = () => Math.floor(Date.now() / 1000)

/** 已订阅后的 my-subscriptions 行（free 订阅直达 active，无到期）。 */
const SUB_VIEW = {
  id: 501,
  user_id: 1,
  listing_id: LISTING.id,
  fee_type: 'free',
  fee_percent: 0,
  monthly_fee: 0,
  status: 'active',
  expires_at: 0,
  created_at: nowSec(),
  listing_name: LISTING.name,
  object_kind: 'ai_bot',
  fee_model: 'free',
  listing_status: 'listed',
  instance_id: '',
}

/** 市场域路由：列表/详情统计/订阅/我的订阅，订阅后 my-subscriptions 带出新订阅。 */
async function mockMarketRoutes(page: Page) {
  let subscribed = false
  await page.route('**/api/market/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    const method = route.request().method()
    if (path === '/api/market/listings' && method === 'GET') {
      return fulfillJson(route, { listings: [LISTING], total: 1, page: 1, page_size: 12 })
    }
    if (path === `/api/market/listings/${LISTING.id}/stats`) {
      return fulfillJson(route, {
        listing: LISTING,
        series: SERIES,
        trades: [TRADE],
        trades_total: 1,
        metric_source: 'platform_verified',
      })
    }
    if (path === `/api/market/listings/${LISTING.id}/subscribe` && method === 'POST') {
      subscribed = true
      return fulfillJson(route, { subscription: SUB_VIEW, already: false })
    }
    if (path === '/api/market/my-subscriptions') {
      return fulfillJson(route, { subscriptions: subscribed ? [SUB_VIEW] : [] })
    }
    if (path === '/api/market/rules') {
      return fulfillJson(route, { min_days: 30, min_trades: 10, max_drawdown_pct: 50 })
    }
    return route.fallback()
  })
}

/** 切换当前会话为 admin（覆盖 fixture 的 user 角色：localStorage + /auth/me 双路）。 */
async function asAdmin(page: Page, context: BrowserContext) {
  await context.addInitScript(() => {
    localStorage.setItem('xt-token', 'e2e-test-token')
    localStorage.setItem(
      'xt-auth',
      JSON.stringify({
        state: {
          token: 'e2e-test-token',
          user: { id: 99, username: 'e2e_admin', role: 'admin', nickname: 'E2E Admin' },
          isAuthenticated: true,
        },
        version: 0,
      })
    )
  })
  await page.route('**/api/auth/me', (route) =>
    fulfillJson(route, { id: 99, username: 'e2e_admin', role: 'admin', nickname: 'E2E Admin' })
  )
}

test.describe('市场 MVP：浏览 → 详情 → 订阅 → 我的订阅', () => {
  test('free 订阅闭环', async ({ authPage }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await mockMarketRoutes(authPage)

    // 浏览市场（/bots/ai 默认即市场 tab）
    await authPage.goto('/bots/ai')
    await expect(authPage.getByText(LISTING.name)).toBeVisible({ timeout: 15000 })

    // 卡片点击进详情
    await authPage.getByText(LISTING.name).click()
    await authPage.waitForURL(`**/bots/ai/market/${LISTING.id}`)

    // 详情页：名称、指标卡、收益曲线容器、成交表
    await expect(authPage.getByText(LISTING.name).first()).toBeVisible()
    await expect(authPage.getByText('平台验证业绩')).toBeVisible()
    await expect(authPage.getByText('月均收益')).toBeVisible()
    await expect(authPage.getByText('最大回撤').first()).toBeVisible()
    await expect(authPage.locator('[data-testid="market-return-chart"]')).toBeVisible()
    await expect(authPage.getByText('成交明细')).toBeVisible()
    await expect(authPage.getByText('BTCUSDT')).toBeVisible()

    // free 订阅：确认对话框直达（无支付弹窗）
    await authPage.getByRole('button', { name: '订阅', exact: true }).click()
    const dlg = authPage.getByRole('dialog')
    await expect(dlg.getByText('确认订阅')).toBeVisible()
    await dlg.getByRole('button', { name: '订阅', exact: true }).click()

    // 订阅成功：按钮切换为 已订阅 + 创建实例
    await expect(authPage.getByRole('button', { name: '已订阅' })).toBeVisible({ timeout: 10000 })
    await expect(authPage.getByRole('button', { name: '创建实例' })).toBeVisible()

    // 我的订阅 tab 出现该订阅
    await authPage.goto('/bots/ai')
    await authPage.getByRole('button', { name: '我的订阅' }).click()
    await expect(authPage.getByRole('button', { name: LISTING.name })).toBeVisible({ timeout: 10000 })
    await expect(authPage.getByText('生效中')).toBeVisible()
    await expect(authPage.getByRole('button', { name: '创建实例' })).toBeVisible()
  })
})

test.describe('市场 MVP：admin 审核队列', () => {
  test('上架审核队列 approve', async ({ authPage, context }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await asAdmin(authPage, context)

    const pendingListing = { ...LISTING, id: 'ml-pending', name: '待审策略A', status: 'pending_review', probation_passed: true }
    let approved = false
    await authPage.route('**/api/admin/market/**', async (route) => {
      const url = new URL(route.request().url())
      const path = url.pathname
      const method = route.request().method()
      if (path === '/api/admin/market/listings' && method === 'GET') {
        return fulfillJson(route, { listings: approved ? [] : [pendingListing] })
      }
      if (path === `/api/admin/market/listings/${pendingListing.id}/approve` && method === 'POST') {
        approved = true
        return fulfillJson(route, { ...pendingListing, status: 'listed' })
      }
      return route.fallback()
    })

    await authPage.goto('/users')
    await authPage.getByRole('button', { name: '上架审核' }).click()
    await expect(authPage.getByText(pendingListing.name)).toBeVisible({ timeout: 15000 })

    await authPage.getByRole('button', { name: '通过上架' }).click()
    // approve 后队列刷新为空
    await expect(authPage.getByText('当前队列暂无条目')).toBeVisible({ timeout: 10000 })
    expect(approved).toBe(true)
  })

  test('提现审核队列打款', async ({ authPage, context }) => {
    await stubAllWebSockets(authPage)
    await applyAuditMocks(authPage)
    await asAdmin(authPage, context)

    const withdrawal = {
      id: 'wd-1',
      provider_user_id: 42,
      amount: 88.5,
      chain: 'trc20',
      address: 'TXyzWithdrawAddress123',
      tx_hash: '',
      status: 'pending',
      admin_note: '',
      created_at: Date.now(),
    }
    let paidTx = ''
    await authPage.route('**/api/social/admin/withdrawals**', async (route) => {
      const url = new URL(route.request().url())
      const path = url.pathname
      const method = route.request().method()
      if (path === '/api/social/admin/withdrawals' && method === 'GET') {
        return fulfillJson(route, { withdrawals: paidTx ? [] : [withdrawal] })
      }
      if (path === `/api/social/admin/withdrawals/${withdrawal.id}/pay` && method === 'POST') {
        paidTx = JSON.parse(route.request().postData() || '{}').tx_hash ?? ''
        return fulfillJson(route, { withdrawal: { ...withdrawal, status: 'paid', tx_hash: paidTx } })
      }
      return route.fallback()
    })

    await authPage.goto('/users')
    await authPage.getByRole('button', { name: '提现审核' }).click()
    await expect(authPage.getByText('88.50 USDT')).toBeVisible({ timeout: 15000 })
    await expect(authPage.getByText(withdrawal.address)).toBeVisible()

    // 确认打款：弹窗表单填 tx_hash
    await authPage.getByRole('button', { name: '确认打款' }).click()
    const dlg = authPage.getByRole('dialog')
    await expect(dlg.getByText('链上交易哈希（tx_hash，必填）')).toBeVisible()
    await dlg.getByRole('textbox').fill('0xdeadbeefcafe')
    await dlg.getByRole('button', { name: '确认打款' }).click()

    await expect(authPage.getByText('当前队列暂无提现申请')).toBeVisible({ timeout: 10000 })
    expect(paidTx).toBe('0xdeadbeefcafe')
  })
})
