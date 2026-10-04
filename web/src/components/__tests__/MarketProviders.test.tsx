import { describe, it, expect, vi, beforeEach } from 'vitest'
import React from 'react'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/social'
import { I18nProvider } from '@/i18n'
import { MarketProviders, AdminProviderReview } from '@/components/social/MarketProviders'
import type { SocialSubscribeResult } from '@/lib/api'

const authState = vi.hoisted(() => ({ role: 'user' }))

const subscribe = vi.fn((_id: number, _data: { track: string; chain?: string }): Promise<SocialSubscribeResult> =>
  Promise.resolve({
    order: { order_id: 'o-1', chain: 'TRC20', address: 'TAddr123abc', amount_usdt: 19900000, status: 'pending', created_at: 0 },
    expires_at: 1893456000,
    period_days: 30,
    provider_id: 3,
    track: 'subscription',
  })
)
const switchTrack = vi.fn((_id: number, _data: { track: string; chain?: string }): Promise<SocialSubscribeResult> =>
  Promise.resolve({
    subscription: { provider_id: 3, track: 'profit_share', status: 'active', track_expires_at: 0, created_at: 0 },
  })
)
const approveProvider = vi.fn((_id: number) => Promise.resolve({ provider: { id: 5 } }))
const rejectProvider = vi.fn((_id: number, _note?: string) => Promise.resolve({ provider: { id: 5 } }))

const marketProvider = {
  id: 1000003, // 引擎 offset id
  db_id: 3,
  name: '稳健趋势信号',
  description: '日线趋势跟踪',
  monthly_fee: 19.9,
  fee_mode: 'hybrid',
  pricing_model: 'both',
  available_tracks: ['subscription', 'profit_share'],
  profit_share_pct: 20,
  follower_count: 8,
}

// 订阅状态可变：默认无订阅
const subState = vi.hoisted(() => ({
  subscription: null as null | {
    provider_id: number
    track: string
    status: string
    track_expires_at: number
    created_at: number
  },
}))

vi.mock('@/lib/api', () => ({
  socialApi: {
    marketProviders: () => Promise.resolve([marketProvider]),
    mySubscription: () => Promise.resolve({ subscription: subState.subscription, available_tracks: ['subscription', 'profit_share'] }),
    subscribe: (id: number, data: { track: string; chain?: string }) => subscribe(id, data),
    switchTrack: (id: number, data: { track: string; chain?: string }) => switchTrack(id, data),
    approveProvider: (id: number) => approveProvider(id),
    rejectProvider: (id: number, note?: string) => rejectProvider(id, note),
  },
}))

vi.mock('@/stores/toastStore', () => ({
  useToastStore: (sel: (s: { addToast: ReturnType<typeof vi.fn> }) => unknown) => sel({ addToast: vi.fn() }),
}))

vi.mock('@/stores/authStore', () => ({
  useAuthStore: (sel: (s: { user: { role: string } }) => unknown) => sel({ user: { role: authState.role } }),
}))

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return (
    <QueryClientProvider client={client}>
      <I18nProvider>{children}</I18nProvider>
    </QueryClientProvider>
  )
}

describe('MarketProviders（市场 Provider 双轨订阅）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    authState.role = 'user'
    subState.subscription = null
  })

  it('renders market provider card with track badges', async () => {
    render(<MarketProviders />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('稳健趋势信号')).toBeTruthy())
    expect(screen.getByText(/订阅轨 · \$19.9\/月/)).toBeTruthy()
    expect(screen.getByText(/分成轨 · 盈利抽成 20%/)).toBeTruthy()
    expect(screen.getByRole('button', { name: '订阅' })).toBeTruthy()
  })

  it('subscribes subscription track with chain and shows payment order', async () => {
    render(<MarketProviders />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('稳健趋势信号')).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: '订阅' }))
    const dialog = await screen.findByRole('dialog')
    // 选择订阅轨（默认首项即 subscription）→ 出现支付链选择
    await waitFor(() => expect(within(dialog).getByText('支付链（USDT）')).toBeTruthy())
    fireEvent.click(within(dialog).getByRole('button', { name: '确认订阅' }))
    await waitFor(() => expect(subscribe).toHaveBeenCalledWith(3, { track: 'subscription', chain: 'trc20' }))
    // 返回计费订单 → 展示支付信息
    await waitFor(() => expect(within(dialog).getByText('收款地址')).toBeTruthy())
    expect(within(dialog).getByText('TAddr123abc')).toBeTruthy()
    expect(within(dialog).getByText('19.90')).toBeTruthy()
  })

  it('subscribes profit_share track without chain selector', async () => {
    render(<MarketProviders />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('稳健趋势信号')).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: '订阅' }))
    const dialog = await screen.findByRole('dialog')
    await waitFor(() => expect(within(dialog).getByText('支付链（USDT）')).toBeTruthy())
    fireEvent.click(within(dialog).getByLabelText(/分成轨/))
    expect(within(dialog).queryByText('支付链（USDT）')).toBeNull()
    subscribe.mockResolvedValueOnce({
      subscription: { provider_id: 3, track: 'profit_share', status: 'active', track_expires_at: 0, created_at: 0 },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: '确认订阅' }))
    await waitFor(() => expect(subscribe).toHaveBeenCalledWith(3, { track: 'profit_share', chain: 'trc20' }))
  })

  it('shows current subscription and switches track after confirm', async () => {
    subState.subscription = { provider_id: 3, track: 'profit_share', status: 'active', track_expires_at: 0, created_at: 0 }
    render(<MarketProviders />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('稳健趋势信号')).toBeTruthy())
    // 卡片显示当前轨道 + 管理入口
    await waitFor(() => expect(screen.getByText('分成轨')).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: '管理订阅' }))
    const dialog = await screen.findByRole('dialog')
    await waitFor(() => expect(within(dialog).getByText('当前轨道')).toBeTruthy())
    // 切到订阅轨
    fireEvent.click(within(dialog).getByLabelText(/订阅轨 ·/))
    fireEvent.click(within(dialog).getByRole('button', { name: '切换轨道' }))
    // ConfirmDialog 嵌套渲染在订阅弹窗内，取最内层标题为「切换轨道」的 dialog
    const dialogs = await screen.findAllByRole('dialog')
    const switchDlg = dialogs.filter((d) => within(d).queryByRole('heading', { name: '切换轨道' })).pop()!
    fireEvent.click(within(switchDlg).getByRole('button', { name: '切换轨道' }))
    await waitFor(() => expect(switchTrack).toHaveBeenCalledWith(3, { track: 'subscription', chain: 'trc20' }))
  })
})

describe('AdminProviderReview（provider 入驻审核，单条 ID 操作）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    authState.role = 'admin'
    subState.subscription = null
  })

  it('hidden for non-admin', () => {
    authState.role = 'user'
    render(<AdminProviderReview />, { wrapper: Wrapper })
    expect(screen.queryByText('Provider 入驻审核')).toBeNull()
  })

  it('approves by application id after confirm', async () => {
    render(<AdminProviderReview />, { wrapper: Wrapper })
    fireEvent.change(screen.getByPlaceholderText('申请 ID'), { target: { value: '5' } })
    fireEvent.click(screen.getByRole('button', { name: '通过' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/#5/)).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: '通过' }))
    await waitFor(() => expect(approveProvider).toHaveBeenCalledWith(5))
  })

  it('rejects by application id with note', async () => {
    render(<AdminProviderReview />, { wrapper: Wrapper })
    fireEvent.change(screen.getByPlaceholderText('申请 ID'), { target: { value: '5' } })
    fireEvent.click(screen.getByRole('button', { name: '拒绝' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: '资料不全' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '拒绝' }))
    await waitFor(() => expect(rejectProvider).toHaveBeenCalledWith(5, '资料不全'))
  })

  it('rejects invalid id without calling api', () => {
    render(<AdminProviderReview />, { wrapper: Wrapper })
    fireEvent.change(screen.getByPlaceholderText('申请 ID'), { target: { value: 'abc' } })
    fireEvent.click(screen.getByRole('button', { name: '通过' }))
    expect(approveProvider).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})
