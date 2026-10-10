import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import React from 'react'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom'
// 测试环境不经 main.tsx，需显式注册词条（默认语言 zh-CN）
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/market'
import { I18nProvider } from '@/i18n'
import { marketListingApi } from '@/lib/api'
import { MarketListingDetail } from '@/components/market/MarketListingDetail'
import { MarketBoard } from '@/components/market/MarketBoard'

const listedCard = {
  id: 'ml-1',
  author_user_id: 7,
  bot_instance_id: 'aibot-1',
  kind: 'robot',
  name: '稳健网格 Pro',
  description: '平台验证业绩的示例策略',
  fee_model: 'profit_share',
  fee_percent: 20,
  monthly_fee: 0,
  status: 'listed' as const,
  probation_passed: true,
}

const dayStats = {
  listing_id: 'ml-1',
  total_return_pct: 12.5,
  annualized_return_pct: 150,
  max_drawdown_pct: 8.2,
  win_rate: 66.7,
  profit_factor: 2.1,
  sharpe_ratio: 1.5,
  total_trades: 42,
  monthly_return_pct: 6.25,
  followers: 18,
  running_days: 60,
}

const statsResp = {
  listing: { ...listedCard, stats: { ...dayStats, date: '2026-10-09' } },
  series: [
    { ...dayStats, date: '2026-10-08', total_return_pct: 10 },
    { ...dayStats, date: '2026-10-09' },
  ],
  trades: [
    {
      id: 1,
      bot_instance_id: 'aibot-1',
      symbol: 'BTCUSDT',
      side: 'LONG',
      entry_price: 70000,
      exit_price: 71000,
      quantity: 0.01,
      pnl: 21,
      pnl_pct: 1.43,
      close_reason: 'take_profit',
      opened_at: 1760000000,
      closed_at: 1760003600,
    },
  ],
  trades_total: 1,
  probation: {
    rule_min_days: 30,
    rule_min_trades: 10,
    rule_max_drawdown_pct: 50,
    days_elapsed: 60,
    trades_in_window: 42,
    max_drawdown_pct: 8.2,
  },
  metric_source: 'platform_verified',
}

const activeSub = {
  id: 11,
  user_id: 1,
  listing_id: 'ml-1',
  fee_type: 'profit_share',
  fee_percent: 20,
  monthly_fee: 0,
  status: 'active',
  expires_at: 0,
  created_at: 1760000000,
  listing_name: '稳健网格 Pro',
  fee_model: 'profit_share',
  listing_status: 'listed',
}

const mocks = vi.hoisted(() => ({
  subscribe: vi.fn(),
  mySubscriptions: vi.fn(),
  cancelSubscription: vi.fn(),
  instantiate: vi.fn(),
  // ECharts setOption 调用记录（双曲线断言用）
  chartOptions: [] as any[],
}))

vi.mock('@/lib/api', () => ({
  marketListingApi: {
    stats: vi.fn(() => Promise.resolve(statsResp)),
    list: vi.fn(() => Promise.resolve({ listings: [{ ...listedCard, stats: { ...dayStats, date: '2026-10-09' } }], total: 1, page: 1, page_size: 12 })),
    subscribe: mocks.subscribe,
    mySubscriptions: mocks.mySubscriptions,
    cancelSubscription: mocks.cancelSubscription,
    instantiate: mocks.instantiate,
  },
}))

// ECharts 懒加载打桩：jsdom 无 canvas，记录 setOption 供双曲线断言
vi.mock('@/lib/echarts', () => ({
  getEcharts: () =>
    Promise.resolve({
      init: () => ({ setOption: (o: any) => mocks.chartOptions.push(o), dispose: vi.fn(), resize: vi.fn() }),
    }),
}))

vi.stubGlobal('ResizeObserver', class {
  observe() {}
  unobserve() {}
  disconnect() {}
})

function Probe() {
  const loc = useLocation()
  return <div data-testid="loc">{loc.pathname}</div>
}

function renderWithRouter(ui: React.ReactNode, initial: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider>
        <MemoryRouter initialEntries={[initial]}>
          <Routes>
            <Route path="/bots/ai/market/:id" element={<MarketListingDetail />} />
            <Route path="*" element={null} />
          </Routes>
          {ui}
          <Probe />
        </MemoryRouter>
      </I18nProvider>
    </QueryClientProvider>
  )
}

describe('MarketListingDetail（条目详情页）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.subscribe.mockReset()
    mocks.mySubscriptions.mockReset()
    mocks.mySubscriptions.mockResolvedValue([])
    mocks.instantiate.mockReset()
    mocks.chartOptions.length = 0
  })
  afterEach(() => localStorage.removeItem('xt-locale'))

  it('renders verified badge, metric cards, probation info, chart and trades', async () => {
    renderWithRouter(null, '/bots/ai/market/ml-1')
    await waitFor(() => expect(screen.getByText('稳健网格 Pro')).toBeTruthy())
    // 平台验证业绩徽标 + 订阅按钮（未订阅，可点击）
    expect(screen.getByText('平台验证业绩')).toBeTruthy()
    const subscribeBtn = screen.getByText('订阅').closest('button')
    expect(subscribeBtn?.disabled).toBe(false)
    // 关键指标卡（最新日快照）
    expect(screen.getByText('月均收益')).toBeTruthy()
    expect(screen.getByText('最大回撤')).toBeTruthy()
    expect(screen.getByText('胜率')).toBeTruthy()
    expect(screen.getByText('盈亏比')).toBeTruthy()
    expect(screen.getByText('+6.25%')).toBeTruthy()
    expect(screen.getByText('2.10')).toBeTruthy()
    expect(screen.getByText('60天')).toBeTruthy()
    // 考核通过信息：规则快照 vs 达标值
    expect(screen.getByText('考核通过信息')).toBeTruthy()
    expect(screen.getByText('≥ 30天')).toBeTruthy()
    expect(screen.getByText('< 50%')).toBeTruthy()
    expect(screen.getAllByText(/8\.20%/).length).toBeGreaterThan(0)
    // 收益曲线容器（ECharts 打桩）+ 成交明细
    expect(screen.getByTestId('market-return-chart')).toBeTruthy()
    expect(screen.getByText('成交明细')).toBeTruthy()
    expect(screen.getByText('BTCUSDT')).toBeTruthy()
    expect(screen.getByText('take_profit')).toBeTruthy()
    // stats 调用透传成交分页参数
    expect(marketListingApi.stats).toHaveBeenCalledWith('ml-1', { limit: 90, trades_limit: 20, trades_offset: 0 })
  })

  it('renders English labels after locale switch', async () => {
    localStorage.setItem('xt-locale', 'en-US')
    renderWithRouter(null, '/bots/ai/market/ml-1')
    await waitFor(() => expect(screen.getByText('Platform Verified')).toBeTruthy())
    expect(screen.getByText('Profit Factor')).toBeTruthy()
    expect(screen.getByText('Probation Passed')).toBeTruthy()
    expect(screen.getByText('Trade History')).toBeTruthy()
    const subscribeBtn = screen.getByText('Subscribe').closest('button')
    expect(subscribeBtn?.disabled).toBe(false)
  })

  it('profit_share：确认弹窗直订，成功后显示已订阅', async () => {
    let subs: any[] = []
    mocks.mySubscriptions.mockImplementation(() => Promise.resolve(subs))
    mocks.subscribe.mockImplementation(() => {
      subs = [activeSub]
      return Promise.resolve({ subscription: activeSub, already: false })
    })
    renderWithRouter(null, '/bots/ai/market/ml-1')
    await waitFor(() => expect(screen.getByText('稳健网格 Pro')).toBeTruthy())

    fireEvent.click(screen.getByText('订阅').closest('button')!)
    // 确认弹窗（含分成比例提示）
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('确认订阅')).toBeTruthy()
    expect(dialog.textContent).toContain('20%')
    fireEvent.click(within(dialog).getByText('订阅'))

    await waitFor(() => expect(mocks.subscribe).toHaveBeenCalledWith('ml-1'))
    await waitFor(() => expect(screen.getByText('已订阅')).toBeTruthy())
  })

  it('monthly：支付弹窗展示金额/链选择，创建订单后展示收款信息并引导 Billing', async () => {
    ;(marketListingApi.stats as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ...statsResp,
      listing: {
        ...listedCard,
        fee_model: 'monthly',
        fee_percent: 0,
        monthly_fee: 49.5,
        stats: { ...dayStats, date: '2026-10-09' },
      },
    })
    const pendingSub = {
      ...activeSub,
      fee_type: 'monthly',
      fee_percent: 0,
      monthly_fee: 49.5,
      status: 'pending_payment',
    }
    mocks.subscribe.mockResolvedValue({
      subscription: pendingSub,
      already: false,
      order: {
        order_id: 'bill_test_1',
        chain: 'TRC20',
        address: 'TReceiverAddr123',
        amount_usdt: 49_500_000,
        status: 'pending',
        created_at: 1760000000,
      },
      expires_at: 1760001800,
      period_days: 30,
    })
    renderWithRouter(null, '/bots/ai/market/ml-1')
    await waitFor(() => expect(screen.getByText('稳健网格 Pro')).toBeTruthy())

    fireEvent.click(screen.getByText('订阅').closest('button')!)
    // 支付弹窗：金额（条目快照）+ 链选择
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('支付订阅费')).toBeTruthy()
    expect(dialog.textContent).toContain('49.5')
    expect(within(dialog).getByText('支付链')).toBeTruthy()

    fireEvent.click(within(dialog).getByText('创建订单'))
    await waitFor(() => expect(mocks.subscribe).toHaveBeenCalledWith('ml-1', { chain: 'TRC20' }))
    // 订单收款信息 + 前往支付
    await waitFor(() => expect(screen.getByTestId('market-sub-order-amount').textContent).toBe('49.50'))
    expect(screen.getByText('TReceiverAddr123')).toBeTruthy()
    expect(screen.getByText('前往支付')).toBeTruthy()
  })

  it('已订阅状态：active 显示已订阅与到期时间；pending_payment 显示待支付', async () => {
    mocks.mySubscriptions.mockResolvedValue([
      { ...activeSub, fee_type: 'monthly', monthly_fee: 49.5, expires_at: 1762592000 },
    ])
    const { unmount } = renderWithRouter(null, '/bots/ai/market/ml-1')
    await waitFor(() => expect(screen.getByText('稳健网格 Pro')).toBeTruthy())
    await waitFor(() => expect(screen.getByText('已订阅')).toBeTruthy())
    expect(screen.getByText(/到期时间/).textContent).toContain('到期时间')
    // active 订阅按钮禁用
    expect(screen.getByText('已订阅').closest('button')?.disabled).toBe(true)
    unmount()

    mocks.mySubscriptions.mockResolvedValue([{ ...activeSub, status: 'pending_payment' }])
    renderWithRouter(null, '/bots/ai/market/ml-1')
    await waitFor(() => expect(screen.getByText('稳健网格 Pro')).toBeTruthy())
    await waitFor(() => expect(screen.getByText('待支付')).toBeTruthy())
    expect(screen.getByText('待支付').closest('button')?.disabled).toBe(false)
  })

  it('实例化三态：可创建 → 点击创建成功 → 已创建显示实例链接；非 active 不可创建', async () => {
    // ① 可创建：active 订阅且未实例化 → 按钮可用
    let subs: any[] = [{ ...activeSub }]
    mocks.mySubscriptions.mockImplementation(() => Promise.resolve(subs))
    mocks.instantiate.mockImplementation(() => {
      subs = [{ ...activeSub, instance_id: 'aibot-clone-1' }]
      return Promise.resolve({ kind: 'ai_bot', instance_id: 'aibot-clone-1', already: false })
    })
    renderWithRouter(null, '/bots/ai/market/ml-1')
    await waitFor(() => expect(screen.getByText('已订阅')).toBeTruthy())
    const createBtn = screen.getByText('创建实例').closest('button')!
    expect(createBtn.disabled).toBe(false)

    // ② 点击创建 → 调 instantiate → 成功后切换为"查看实例"链接
    fireEvent.click(createBtn)
    await waitFor(() => expect(mocks.instantiate).toHaveBeenCalledWith(11))
    await waitFor(() => expect(screen.getByText('查看实例')).toBeTruthy())
    expect(screen.queryByText('创建实例')).toBeNull()

    // 已创建态：点击跳转机器人管理页
    fireEvent.click(screen.getByText('查看实例').closest('button')!)
    await waitFor(() => expect(screen.getByTestId('loc').textContent).toBe('/bots'))
  })

  it('已创建状态直接进入实例链接；pending_payment 不显示创建按钮', async () => {
    // 已创建：显示查看实例而非创建按钮
    mocks.mySubscriptions.mockResolvedValue([{ ...activeSub, instance_id: 'aibot-clone-9' }])
    const { unmount } = renderWithRouter(null, '/bots/ai/market/ml-1')
    await waitFor(() => expect(screen.getByText('查看实例')).toBeTruthy())
    expect(screen.queryByText('创建实例')).toBeNull()
    unmount()

    // 不可创建：pending_payment → 仅待支付订阅按钮，无实例化入口
    mocks.mySubscriptions.mockResolvedValue([{ ...activeSub, status: 'pending_payment' }])
    renderWithRouter(null, '/bots/ai/market/ml-1')
    await waitFor(() => expect(screen.getByText('待支付')).toBeTruthy())
    expect(screen.queryByText('创建实例')).toBeNull()
    expect(screen.queryByText('查看实例')).toBeNull()
  })

  it('grid_bot 条目：叠加同期回测虚线，偏差卡正绿 + 口径说明', async () => {
    ;(marketListingApi.stats as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ...statsResp,
      listing: {
        ...listedCard,
        object_kind: 'grid_bot',
        object_id: 'grid-1',
        stats: { ...dayStats, date: '2026-10-09' },
      },
      backtest_series: [
        { time: Date.parse('2026-10-08T00:00:00Z'), return_pct: 9.8 },
        { time: Date.parse('2026-10-09T00:00:00Z'), return_pct: 10.1 },
      ],
      backtest_note: '回测对照：同一网格引擎在考核窗口同期 1h K线上重放（手续费 0.1%、无滑点）',
      deviation: { live_return_pct: 12.5, backtest_return_pct: 10.1, diff_pct: 2.4, direction: 'higher' },
    })
    renderWithRouter(null, '/bots/ai/market/ml-1')
    await waitFor(() => expect(screen.getByText('稳健网格 Pro')).toBeTruthy())
    // 网格徽标
    expect(screen.getByText('网格机器人')).toBeTruthy()
    // 偏差卡：实盘-回测偏差 +2.40%（正绿）
    const dev = screen.getByTestId('market-deviation')
    expect(dev.textContent).toContain('+2.40%')
    expect(dev.textContent).toContain('实盘-回测偏差')
    expect(dev.querySelector('.text-quant-green')).toBeTruthy()
    // 口径说明（后端 backtest_note 原文展示）
    expect(screen.getByText(/同一网格引擎/)).toBeTruthy()
    // 双曲线：初始化含 3 条 series（实盘实线 + 回撤 + 回测虚线），图例注明口径
    const init = mocks.chartOptions.find((o) => Array.isArray(o?.series) && o.series.length === 3)
    expect(init).toBeTruthy()
    expect(init.legend.data.length).toBe(3)
    expect(init.series[2].lineStyle.type).toBe('dashed')
    // 数据更新带两个回测点
    const update = mocks.chartOptions.find((o) => o?.series?.[2]?.data?.length === 2)
    expect(update.series[2].data[0][1]).toBe(9.8)
    expect(update.series[2].data[1][1]).toBe(10.1)
  })

  it('grid_bot 条目：实盘跑输回测时偏差卡负红', async () => {
    ;(marketListingApi.stats as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ...statsResp,
      listing: { ...listedCard, object_kind: 'grid_bot', object_id: 'grid-1', stats: { ...dayStats, date: '2026-10-09' } },
      backtest_series: [{ time: Date.parse('2026-10-09T00:00:00Z'), return_pct: 13.8 }],
      backtest_note: '回测对照：同一网格引擎重放',
      deviation: { live_return_pct: 12.5, backtest_return_pct: 13.8, diff_pct: -1.3, direction: 'lower' },
    })
    renderWithRouter(null, '/bots/ai/market/ml-1')
    await waitFor(() => expect(screen.getByTestId('market-deviation')).toBeTruthy())
    const dev = screen.getByTestId('market-deviation')
    expect(dev.textContent).toContain('-1.30%')
    expect(dev.querySelector('.text-quant-red')).toBeTruthy()
  })

  it('ai_bot 条目：显示无回测对照说明，不显示偏差卡与虚线', async () => {
    ;(marketListingApi.stats as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ...statsResp,
      backtest_series: null,
      backtest_note: 'AI 信号驱动策略无回测对照：信号由模型实时生成，历史行情无法确定性重放',
    })
    renderWithRouter(null, '/bots/ai/market/ml-1')
    await waitFor(() => expect(screen.getByText(/AI 信号驱动策略无回测对照/)).toBeTruthy())
    expect(screen.queryByTestId('market-deviation')).toBeNull()
    // 图表保持两条 series（累计收益 + 回撤），无第三条虚线
    await waitFor(() => expect(screen.getByTestId('market-return-chart')).toBeTruthy())
    expect(mocks.chartOptions.some((o) => Array.isArray(o?.series) && o.series.length === 3)).toBe(false)
  })

  it('grid_bot 数据不足：显示降级说明，不显示偏差卡与虚线', async () => {
    ;(marketListingApi.stats as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ...statsResp,
      listing: { ...listedCard, object_kind: 'grid_bot', object_id: 'grid-x', stats: { ...dayStats, date: '2026-10-09' } },
      backtest_series: null,
      backtest_error: '本地 GRIDUSDT 1h K线数据不足（0 根），无法生成回测对照',
    })
    renderWithRouter(null, '/bots/ai/market/ml-1')
    await waitFor(() => expect(screen.getByText(/K线数据不足/)).toBeTruthy())
    expect(screen.queryByTestId('market-deviation')).toBeNull()
    await waitFor(() => expect(screen.getByTestId('market-return-chart')).toBeTruthy())
    expect(mocks.chartOptions.some((o) => Array.isArray(o?.series) && o.series.length === 3)).toBe(false)
  })
})

describe('MarketBoard 卡片点击跳转', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.mySubscriptions.mockReset()
    mocks.mySubscriptions.mockResolvedValue([])
  })
  afterEach(() => localStorage.removeItem('xt-locale'))

  it('clicking a listed card navigates to /bots/ai/market/:id', async () => {
    renderWithRouter(<MarketBoard />, '/bots/ai')
    await waitFor(() => expect(screen.getByText('稳健网格 Pro')).toBeTruthy())
    fireEvent.click(screen.getByText('稳健网格 Pro'))
    await waitFor(() => expect(screen.getByTestId('loc').textContent).toBe('/bots/ai/market/ml-1'))
  })

  it('renders object kind badge: AI by default, 网格 for grid_bot listings', async () => {
    ;(marketListingApi.list as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      listings: [
        { ...listedCard, stats: { ...dayStats, date: '2026-10-09' } },
        {
          ...listedCard,
          id: 'ml-grid-1',
          object_kind: 'grid_bot',
          object_id: 'grid-1',
          name: 'ETH 网格 Pro',
          stats: { ...dayStats, date: '2026-10-09' },
        },
      ],
      total: 2,
      page: 1,
      page_size: 12,
    })
    renderWithRouter(<MarketBoard />, '/bots/ai')
    await waitFor(() => expect(screen.getByText('ETH 网格 Pro')).toBeTruthy())
    // 存量条目（无 object_kind）默认 AI 徽标，grid_bot 条目显示网格徽标
    expect(screen.getByText('AI')).toBeTruthy()
    expect(screen.getByText('网格')).toBeTruthy()
  })
})
