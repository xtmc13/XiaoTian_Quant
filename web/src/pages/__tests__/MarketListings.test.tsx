import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import React from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
// 测试环境不经 main.tsx，需显式注册词条（默认语言 zh-CN）
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/market'
import { I18nProvider } from '@/i18n'
import { MarketBoard } from '@/components/market/MarketBoard'
import { MyListings } from '@/components/market/MyListings'
import { AdminMarketReview } from '@/components/market/AdminMarketReview'

const listedCard = {
  id: 'ml-1',
  author_user_id: 7,
  bot_instance_id: 'aibot-1',
  kind: 'robot',
  name: '稳健网格 Pro',
  description: '',
  fee_model: 'profit_share',
  fee_percent: 20,
  monthly_fee: 0,
  status: 'listed' as const,
  probation_passed: true,
  stats: {
    listing_id: 'ml-1',
    date: '2026-09-25',
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
  },
}

const probationListing = {
  id: 'ml-2',
  author_user_id: 7,
  bot_instance_id: 'aibot-2',
  kind: 'robot',
  name: '马丁实验',
  description: '',
  fee_model: 'free',
  fee_percent: 0,
  monthly_fee: 0,
  status: 'probation' as const,
  probation_passed: false,
  probation_started_at: 1758000000,
  progress: {
    days_elapsed: 12,
    min_days: 30,
    remaining_days: 18,
    trades_in_window: 4,
    min_trades: 10,
    remaining_trades: 6,
    max_drawdown_pct: 12.5,
    max_drawdown_limit: 50,
    days_ok: false,
    trades_ok: false,
    drawdown_ok: true,
    passed: false,
  },
}

const rejectedListing = {
  ...probationListing,
  id: 'ml-3',
  bot_instance_id: 'aibot-3',
  name: '激进合约',
  status: 'rejected' as const,
  progress: undefined,
  reject_reason: '回撤数据异常',
}

vi.mock('@/lib/api', () => ({
  marketListingApi: {
    list: () => Promise.resolve({ listings: [listedCard], total: 1, page: 1, page_size: 12 }),
    myListings: () => Promise.resolve([probationListing, rejectedListing]),
    rules: () => Promise.resolve({ min_days: 30, min_trades: 10, max_drawdown_pct: 50 }),
    create: () => Promise.resolve({}),
    submit: () => Promise.resolve({}),
    cancel: () => Promise.resolve({}),
  },
  aiBotApi: {
    list: () => Promise.resolve([
      { id: 'aibot-9', name: '空闲实例', symbol: 'BTCUSDT', execution_mode: 'paper', status: 'stopped' },
    ]),
  },
  adminMarketApi: {
    listings: () => Promise.resolve([
      { ...probationListing, id: 'ml-4', status: 'pending_review', stats: listedCard.stats },
    ]),
    approve: () => Promise.resolve({}),
    reject: () => Promise.resolve({}),
    delist: () => Promise.resolve({}),
    saveRules: () => Promise.resolve({}),
  },
}))

vi.mock('@/lib/useToast', () => ({
  toast: vi.fn(),
}))

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return (
    <QueryClientProvider client={client}>
      <I18nProvider>{children}</I18nProvider>
    </QueryClientProvider>
  )
}

describe('MarketBoard（公开策略市场）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders listed card with standardized stats and probation badge', async () => {
    render(<MarketBoard />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('稳健网格 Pro')).toBeTruthy())
    expect(screen.getByText('考核通过')).toBeTruthy()
    // 标准化统计六格
    expect(screen.getByText('月均收益')).toBeTruthy()
    expect(screen.getByText('最大回撤')).toBeTruthy()
    expect(screen.getByText('胜率')).toBeTruthy()
    expect(screen.getByText('交易数')).toBeTruthy()
    expect(screen.getByText('运行天数')).toBeTruthy()
    expect(screen.getByText('+6.25%')).toBeTruthy() // 月均收益
    expect(screen.getByText('-8.20%')).toBeTruthy() // 最大回撤
    expect(screen.getByText('66.7%')).toBeTruthy() // 胜率
    expect(screen.getByText('42')).toBeTruthy() // 交易数
    expect(screen.getByText('60天')).toBeTruthy() // 运行天数
    expect(screen.getByText('18')).toBeTruthy() // 跟踪数
    expect(screen.getByText('分成 20%')).toBeTruthy() // 付费模式
  })

  it('renders sort controls', async () => {
    render(<MarketBoard />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText(/按收益/)).toBeTruthy())
    expect(screen.getByText(/按回撤/)).toBeTruthy()
    expect(screen.getByText(/按跟踪数/)).toBeTruthy()
  })
})

describe('MyListings（我的上架）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows probation progress with remaining days/trades', async () => {
    render(<MyListings />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('马丁实验')).toBeTruthy())
    expect(screen.getByText('考核中')).toBeTruthy()
    expect(screen.getByText(/12\/30/)).toBeTruthy() // 考核第 12 天/共 30 天
    expect(screen.getByText(/4\/10/)).toBeTruthy() // 4/10 笔交易
    expect(screen.getAllByText(/还差/).length).toBeGreaterThan(0)
  })

  it('shows reject reason to author', async () => {
    render(<MyListings />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('激进合约')).toBeTruthy())
    expect(screen.getByText('已驳回')).toBeTruthy()
    expect(screen.getByText(/回撤数据异常/)).toBeTruthy()
  })

  it('opens submit form listing candidate instances', async () => {
    render(<MyListings />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('马丁实验')).toBeTruthy())
    fireEvent.click(screen.getByText('提交考核'))
    await waitFor(() => expect(screen.getByText(/空闲实例/)).toBeTruthy())
  })
})

describe('AdminMarketReview（审核队列）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders pending review queue with approve/reject actions', async () => {
    render(<AdminMarketReview />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('通过上架')).toBeTruthy())
    expect(screen.getByText('驳回')).toBeTruthy()
    expect(screen.getByText('上架考核规则')).toBeTruthy()
    expect(screen.getByText(/马丁实验/)).toBeTruthy()
  })
})

describe('i18n: English locale（切换 en-US 后关键文案渲染英文）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.setItem('xt-locale', 'en-US')
  })
  afterEach(() => localStorage.removeItem('xt-locale'))

  it('MarketBoard renders English stats labels and badges', async () => {
    render(<MarketBoard />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('稳健网格 Pro')).toBeTruthy())
    expect(screen.getByText('Probation Passed')).toBeTruthy()
    expect(screen.getByText('Monthly Return')).toBeTruthy()
    expect(screen.getByText('Max Drawdown')).toBeTruthy()
    expect(screen.getByText('Win Rate')).toBeTruthy()
    expect(screen.getByText('Trades')).toBeTruthy()
    expect(screen.getByText('Days Running')).toBeTruthy()
    expect(screen.getByText('60d')).toBeTruthy() // running days + unit.day
    expect(screen.getByText('Share 20%')).toBeTruthy() // fee badge
    expect(screen.getByText(/By Return/)).toBeTruthy() // sort control (active, has ▼ suffix)
  })

  it('MyListings renders English status badge, progress and rules hint', async () => {
    render(<MyListings />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('马丁实验')).toBeTruthy())
    expect(screen.getByText('Probation')).toBeTruthy() // status badge
    expect(screen.getByText(/Day 12\/30/)).toBeTruthy() // probation day progress
    expect(screen.getByText(/18 days remaining/)).toBeTruthy()
    expect(screen.getByText('Rejected')).toBeTruthy()
    expect(screen.getByText(/Probation rules: ≥30 days · ≥10 trades · drawdown <50%/)).toBeTruthy()
    expect(screen.getByText('Submit for Probation')).toBeTruthy()
  })

  it('AdminMarketReview renders English queue tabs and actions', async () => {
    render(<AdminMarketReview />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('Approve')).toBeTruthy())
    expect(screen.getByText('Reject')).toBeTruthy()
    expect(screen.getByText('Listing Probation Rules')).toBeTruthy()
    expect(screen.getByText('Under Review')).toBeTruthy()
    expect(screen.getByText(/Total Return \+12\.50%/)).toBeTruthy() // statText line
  })
})
