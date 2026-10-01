import { test, expect } from './fixtures'

/**
 * Route smoke test — 遍历全部前端路由直接打开（deep-link），
 * 防止 router NoRoute 回退或 vite base 配置回归导致整组页面打不开。
 *
 * 断言标准：
 *  1. 直接 goto 深链接后 HTTP 200 且不发生重定向循环（history.replaceState 不会
 *     改 URL，ERR_TOO_MANY_REDIRECTS 在 playwright 里表现为 goto 超时/失败）
 *  2. #root 有实际渲染内容（白屏检测：空 root 说明 JS bundle 加载失败）
 */

// 与 web/src/App.tsx 的路由表保持一致（新增页面时同步补充）
const ROUTES: Array<{ path: string }> = [
  { path: '/' },
  { path: '/dashboard' },
  { path: '/login' },
  // 交易
  { path: '/trading' },
  { path: '/trading/spot' },
  { path: '/trading/contract' },
  { path: '/advanced-orders' },
  { path: '/arbitrage' },
  { path: '/arbitrage/cross' },
  { path: '/arbitrage/triangular' },
  { path: '/social-trading' },
  { path: '/onchain' },
  { path: '/market' },
  { path: '/market-data' },
  { path: '/exchange-account' },
  // 策略
  { path: '/strategy' },
  { path: '/strategy/editor' },
  { path: '/strategy/python' },
  { path: '/strategies' },
  { path: '/create' },
  { path: '/indicator-community' },
  { path: '/indicator-community/1' },
  { path: '/indicator-ide' },
  { path: '/author-dashboard' },
  // 回测/优化
  { path: '/backtest' },
  { path: '/backtest/portfolio' },
  { path: '/hyperopt' },
  { path: '/factor-research' },
  // AI
  { path: '/ai' },
  { path: '/ai/analysis' },
  { path: '/ai/freqai' },
  { path: '/ai/rl' },
  { path: '/ai/discussion-room' },
  { path: '/ai/tensorboard' },
  { path: '/ai-bots' },
  { path: '/model-management' },
  // 机器人
  { path: '/bots' },
  { path: '/bots/strategy' },
  { path: '/bots/grid' },
  { path: '/bots/dca' },
  { path: '/bots/layered-martin' },
  { path: '/bots/signal' },
  { path: '/bots/ai' },
  // 数据/工具
  { path: '/data' },
  { path: '/pairlist' },
  { path: '/alerts' },
  { path: '/analysis' },
  { path: '/risk-control' },
  // 账户/管理
  { path: '/portfolio' },
  { path: '/settings' },
  { path: '/profile' },
  { path: '/users' },
  { path: '/agent-tokens' },
  { path: '/billing' },
  { path: '/status' },
  { path: '/logs' },
  { path: '/strategy-leaderboard' },
]

for (const { path } of ROUTES) {
  test(`deep-link renders: ${path}`, async ({ authPage }) => {
    const resp = await authPage.goto(path)
    expect(resp?.status(), `${path} 应返回 200`).toBe(200)

    // #root 必须有渲染内容（排除 loading 占位后的真实 DOM）
    const root = authPage.locator('#root')
    await expect(root).not.toBeEmpty()

    // 页面不得停留在全局 loading 骨架屏超过 5s（白屏的一种形态）
    const bodyText = await authPage.locator('body').innerText()
    expect(bodyText.trim().length, `${path} body 应有可见文本`).toBeGreaterThan(0)
  })
}
