import { test, expect } from './fixtures'
import type { Page, Route } from '@playwright/test'

/**
 * E2E (C1.2): 关键业务全链路——
 *   登录 → 创建 paper DCA 定投机器人 → 启动 → 产生通知 → 通知中心可见。
 *
 * 一个 spec 串起整条链路，所有 /api/** 请求被 mock（返回前端 axios
 * 解包后的形状，见 web/src/lib/api.ts），不依赖真实网关。
 * 断言均为强断言：任一环节 UI 失效，本测试必须失败。
 */

const fulfillJson = (route: Route, body: unknown, status = 200) =>
  route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })

const AUTH_USER = { id: 1, username: 'e2e_user', role: 'user', nickname: 'E2E Tester' }

/** Broad fallback for APIs this test doesn't care about (registered first;
 *  Playwright matches the most recently registered route first). */
async function mockGenericApis(page: Page) {
  await page.route('**/api/**', (route) => {
    const url = route.request().url()
    const method = route.request().method()
    if (url.includes('/auth/login')) return route.fallback()
    if (method === 'POST' || method === 'PUT' || method === 'DELETE') {
      return fulfillJson(route, { success: true })
    }
    if (
      url.includes('/strategies') ||
      url.includes('/bots') ||
      url.includes('/orders') ||
      url.includes('/trades') ||
      url.includes('/positions') ||
      url.includes('/markets') ||
      url.includes('/exchanges')
    ) {
      return fulfillJson(route, [])
    }
    return fulfillJson(route, {})
  })
}

test.describe('关键链路：登录 → 创建并启动 paper DCA 机器人 → 通知中心', () => {
  test('full chain: login, create & start paper DCA bot, see notification', async ({ page }) => {
    /* ── 0. 先注册全部 mock，再进入页面 ─────────────────────────── */
    await mockGenericApis(page)

    // 登录：真实表单提交，mock 后端返回 access_token
    let loginPayload: Record<string, unknown> | null = null
    await page.route('**/api/auth/login', async (route) => {
      loginPayload = route.request().postDataJSON() as Record<string, unknown>
      return fulfillJson(route, {
        access_token: 'e2e-login-token',
        token_type: 'bearer',
        user: AUTH_USER,
      })
    })

    // DCA 机器人列表/创建（状态驱动：创建后出现卡片，启动后变运行中）
    let dcaBots: Record<string, unknown>[] = []
    let botSeq = 0
    let createdPayload: Record<string, unknown> | null = null
    let startRequested = false
    // 三个 pattern 都要：精确（POST 创建）、带 query 的列表 GET、子路径 start/stop。
    // Playwright glob `*` 不跨 `/`，且 `**` 可匹配空串。
    const dcaHandler = async (route: Route) => {
      const url = route.request().url()
      const method = route.request().method()
      if (method === 'POST' && url.endsWith('/api/dca-bots/')) {
        createdPayload = route.request().postDataJSON() as Record<string, unknown>
        botSeq += 1
        const bot = {
          id: `dca-e2e-${botSeq}`,
          name: createdPayload.name,
          symbol: createdPayload.symbol,
          exchange: 'paper',
          quote_amount: createdPayload.quote_amount,
          interval_minutes: createdPayload.interval_minutes,
          max_orders: createdPayload.max_orders,
          period_budget: createdPayload.period_budget,
          take_profit_pct: createdPayload.take_profit_pct,
          stop_loss_pct: createdPayload.stop_loss_pct,
          trailing_enabled: createdPayload.trailing_enabled,
          is_running: false,
          status: 'stopped',
          realized_pnl: 0,
          filled_orders: 0,
          total_invested: 0,
          avg_price: 0,
        }
        dcaBots = [bot]
        return fulfillJson(route, bot)
      }
      if (method === 'GET' && /\/api\/dca-bots\/?(\?|$)/.test(url)) {
        return fulfillJson(route, { bots: dcaBots })
      }
      if (method === 'POST' && url.includes('/start')) {
        startRequested = true
        dcaBots = dcaBots.map((b) => ({ ...b, is_running: true, status: 'running' }))
        return fulfillJson(route, { started: true, id: 'dca-e2e-1', price: 50000 })
      }
      return route.fallback()
    }
    await page.route('**/api/dca-bots/', dcaHandler)
    await page.route('**/api/dca-bots/*', dcaHandler)
    await page.route('**/api/dca-bots/**', dcaHandler)

    // 通知中心：启动前为空，启动后出现一条「已启动」通知（模拟 bot 启动事件）
    let botStarted = false
    const notification = {
      id: 1,
      title: 'DCA 机器人已启动',
      message: 'E2E 定投 已在模拟盘启动',
      content: 'DCA 机器人 "E2E 定投" 已在 paper 交易所启动，将按固定间隔买入 BTCUSDT。',
      level: 'SUCCESS',
      type: 'success',
      read: false,
      created_at: Date.now(),
    }
    await page.route('**/api/notifications**', (route) => {
      const url = route.request().url()
      if (url.includes('/unread-count')) {
        return fulfillJson(route, { count: botStarted ? 1 : 0 })
      }
      return fulfillJson(route, { notifications: botStarted ? [notification] : [], total: botStarted ? 1 : 0 })
    })

    /* ── 1. 登录（真实登录表单 → mock 后端） ─────────────────────── */
    await page.goto('/login')
    await expect(page.getByText('小天量化').first()).toBeVisible()
    await page.locator('input[type="text"]').fill('e2e_user')
    await page.locator('input[type="password"]').fill('e2e-password-123')
    await page.locator('form').getByRole('button', { name: '登录' }).click()

    // 登录成功后重定向到仪表盘，顶栏出现已连接态
    await expect(page).toHaveURL(/\/dashboard/)
    expect(loginPayload).toMatchObject({ username: 'e2e_user', password: 'e2e-password-123' })

    /* ── 2. 创建 paper DCA 机器人 ──────────────────────────────── */
    await page.goto('/bots/dca')
    await expect(page.getByRole('main').getByText('DCA 定投机器人', { exact: true })).toBeVisible()

    await page.getByRole('button', { name: '新建定投' }).first().click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await expect(dialog).toContainText('新建 DCA 定投机器人')

    // 只填名称，其余用表单默认值（BTCUSDT / 100 USDT / 60 分钟 / 止盈 5% / 止损 10%）
    await dialog.locator('input').first().fill('E2E 定投')
    await dialog.getByRole('button', { name: '创建定投机器人' }).click()

    // 创建成功的持久信号：新卡片出现在列表（toast 4s 自动消失，不作断言依据）
    expect(createdPayload).toMatchObject({
      name: 'E2E 定投',
      symbol: 'BTCUSDT',
      quote_amount: 100,
      interval_minutes: 60,
      take_profit_pct: 0.05,
      stop_loss_pct: 0.1,
      trailing_enabled: false,
    })

    // 列表出现新卡片：paper = 模拟盘（卡片根节点 class 特征：rounded-xl p-3）
    const card = page.locator('div.rounded-xl.p-3').filter({ hasText: 'E2E 定投' })
    await expect(card).toBeVisible()
    await expect(card).toContainText('模拟盘')
    await expect(card).toContainText('每 60 分钟买 100 USDT')

    /* ── 3. 启动机器人（paper 模式） ───────────────────────────── */
    await page.getByRole('button', { name: '启动' }).click()
    expect(startRequested).toBe(true)
    botStarted = true // 此后通知接口返回「已启动」通知

    // 卡片状态变为运行中（持久信号）
    await expect(card).toContainText('运行中')

    /* ── 4. 通知中心可见 ───────────────────────────────────────── */
    await page.getByRole('button', { name: '通知', exact: true }).click()
    // 通知下拉面板（TopBar 铃铛弹层，w-80 定宽容器）
    const notifPanel = page.locator('div.w-80').filter({ hasText: '通知' })
    await expect(notifPanel).toBeVisible()
    await expect(notifPanel).toContainText('DCA 机器人已启动')
    await expect(notifPanel).toContainText('E2E 定投')
    await expect(notifPanel).toContainText('paper')
    // 未读标记（金点）
    await expect(notifPanel.locator('.bg-quant-gold').first()).toBeVisible()
  })
})
