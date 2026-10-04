import { describe, it, expect, vi, beforeEach } from 'vitest'
import React from 'react'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
// 测试环境不经 main.tsx，需显式注册词条（默认语言 zh-CN）
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/admin'
import '@/i18n/locales/community'
import { I18nProvider } from '@/i18n'
import { UserManage } from '../../pages/UserManage'

const disableUser = vi.fn((_id: string) => Promise.resolve({ status: 'ok' }))
const enableUser = vi.fn((_id: string) => Promise.resolve({ status: 'ok' }))

const users = [
  { id: '1', username: 'alice', nickname: '', email: 'a@x.com', role: 'user', is_active: 1, created_at: '2026-01-01 10:00:00' },
  { id: '2', username: 'bob', nickname: '', email: 'b@x.com', role: 'user', is_active: 0, created_at: '2026-01-02 10:00:00' },
]

vi.mock('@/lib/api', () => ({
  adminApi: {
    users: () => Promise.resolve(users),
    stats: () => Promise.resolve({ total_users: 2, active_users: 1, admin_count: 0, user_count: 2 }),
    enhancedStats: () => Promise.resolve({}),
    summary: () =>
      Promise.resolve({
        total_users: 2,
        active_users: 1,
        pending_orders: 3,
        total_trades: 42,
        active_strategies: 5,
        unread_alerts: 7,
        uptime_hours: 11,
        memory_mb: 128.5,
      }),
    activity: () =>
      Promise.resolve([
        { type: 'trade', message: 'BTCUSDT buy', timestamp: 1759000000 },
        { type: 'risk', message: 'drawdown exceeded', level: 'WARN', timestamp: 1759000100 },
      ]),
    auditLog: () => Promise.resolve({ logs: [], total: 0 }),
    referrals: () =>
      Promise.resolve([
        { user_id: 11, username: 'carol', code: 'XTABCDEFGH', commission_pct: 10, active: true, referral_count: 3, total_credits: 12800, created_at: 1759000000 },
        { user_id: 12, username: 'dave', code: 'XT12345678', commission_pct: 20, active: false, referral_count: 0, total_credits: 0, created_at: 1759000100 },
      ]),
    updateUser: () => Promise.resolve({ success: true }),
    disableUser: (id: string) => disableUser(id),
    enableUser: (id: string) => enableUser(id),
  },
  // AdminIndicatorReview / AdminMarketReview 仅在对应 tab 渲染，这里给空实现防 import 报错
  communityAdminApi: {
    pendingReviews: () => Promise.resolve({ items: [], total: 0, page: 1, page_size: 20, total_pages: 0 }),
    review: () => Promise.resolve({}),
  },
  adminMarketApi: {
    listings: () => Promise.resolve([]),
    approve: () => Promise.resolve({}),
    reject: () => Promise.resolve({}),
    delist: () => Promise.resolve({}),
    saveRules: () => Promise.resolve({}),
  },
  marketListingApi: {
    rules: () => Promise.resolve({ min_days: 30, min_trades: 10, max_drawdown_pct: 50 }),
  },
}))

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return (
    <QueryClientProvider client={client}>
      <I18nProvider>{children}</I18nProvider>
    </QueryClientProvider>
  )
}

describe('UserManage（用户禁用/启用 + 概览区块）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders user list with disabled badge and summary/activity blocks', async () => {
    render(<UserManage />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('alice')).toBeTruthy())
    // 禁用用户在状态列有红色「禁用」标识
    expect(screen.getByText('禁用')).toBeTruthy()
    // GET /admin/summary 概览区块
    expect(screen.getByText('运营概览')).toBeTruthy()
    expect(screen.getByText('42')).toBeTruthy() // total_trades
    // GET /admin/activity 最近活动区块
    expect(screen.getByText('最近活动')).toBeTruthy()
    expect(screen.getByText('BTCUSDT buy')).toBeTruthy()
    expect(screen.getByText('drawdown exceeded')).toBeTruthy()
  })

  it('disables a user after danger confirm', async () => {
    render(<UserManage />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('alice')).toBeTruthy())
    fireEvent.click(screen.getByTitle('禁用'))
    // 二次确认弹窗
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('禁用用户')).toBeTruthy()
    expect(within(dialog).getByText(/alice/)).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: '禁用' }))
    await waitFor(() => expect(disableUser).toHaveBeenCalledWith('1'))
    expect(enableUser).not.toHaveBeenCalled()
  })

  it('cancels disable when confirm dialog rejected', async () => {
    render(<UserManage />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('alice')).toBeTruthy())
    fireEvent.click(screen.getByTitle('禁用'))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(disableUser).not.toHaveBeenCalled()
  })

  it('enables a disabled user without confirm', async () => {
    render(<UserManage />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('bob')).toBeTruthy())
    fireEvent.click(screen.getByTitle('启用'))
    await waitFor(() => expect(enableUser).toHaveBeenCalledWith('2'))
    expect(disableUser).not.toHaveBeenCalled()
  })

  it('renders referrals tab with code/count/credits rows (GET /admin/referrals)', async () => {
    render(<UserManage />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('alice')).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: /推荐返佣/ }))
    await waitFor(() => expect(screen.getByText('XTABCDEFGH')).toBeTruthy())
    // 行内容：推荐人、佣金比例、推荐人数、累计积分、状态
    expect(screen.getByText('carol')).toBeTruthy()
    expect(screen.getByText('10%')).toBeTruthy()
    expect(screen.getByText('12800')).toBeTruthy()
    expect(screen.getByText('启用中')).toBeTruthy()
    expect(screen.getByText('已停用')).toBeTruthy()
  })
})
