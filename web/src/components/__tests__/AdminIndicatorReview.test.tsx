import { describe, it, expect, vi, beforeEach } from 'vitest'
import React from 'react'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/community'
import { I18nProvider } from '@/i18n'
import { AdminIndicatorReview } from '@/components/community/AdminIndicatorReview'

const review = vi.fn((_id: number, _approve: boolean, _reason?: string) =>
  Promise.resolve({ code: 1, msg: 'reviewed' })
)

const pendingItems = [
  {
    id: 11,
    name: '动量突破 Pro',
    description: '突破回踩入场',
    pricing_type: 'paid',
    price: 99,
    purchase_count: 0,
    avg_rating: 0,
    view_count: 3,
    author_id: 7,
    author_name: '量化小王',
    created_at: 1759000000,
  },
  {
    id: 12,
    name: '均值回归 Lite',
    description: '',
    pricing_type: 'free',
    price: 0,
    purchase_count: 0,
    avg_rating: 0,
    view_count: 1,
    author_id: 8,
    author_name: '',
    created_at: 1759001000,
  },
]

vi.mock('@/lib/api', () => ({
  communityAdminApi: {
    pendingReviews: () =>
      Promise.resolve({ items: pendingItems, total: 2, page: 1, page_size: 20, total_pages: 1 }),
    review: (id: number, approve: boolean, reason?: string) => review(id, approve, reason),
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

describe('AdminIndicatorReview（社区指标上架审核）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders pending review list', async () => {
    render(<AdminIndicatorReview />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('动量突破 Pro')).toBeTruthy())
    expect(screen.getByText('均值回归 Lite')).toBeTruthy()
    expect(screen.getByText(/量化小王/)).toBeTruthy()
    expect(screen.getByText('99')).toBeTruthy()
    expect(screen.getByText('免费')).toBeTruthy()
  })

  it('approves an indicator after confirm', async () => {
    render(<AdminIndicatorReview />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('动量突破 Pro')).toBeTruthy())
    fireEvent.click(screen.getAllByRole('button', { name: '通过' })[0])
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('通过上架')).toBeTruthy()
    expect(within(dialog).getByText(/动量突破 Pro/)).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: '通过' }))
    await waitFor(() => expect(review).toHaveBeenCalledWith(11, true, ''))
  })

  it('rejects an indicator with reason via prompt', async () => {
    render(<AdminIndicatorReview />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('动量突破 Pro')).toBeTruthy())
    fireEvent.click(screen.getAllByRole('button', { name: '拒绝' })[0])
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('拒绝上架')).toBeTruthy()
    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: '回测数据不足' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '拒绝' }))
    await waitFor(() => expect(review).toHaveBeenCalledWith(11, false, '回测数据不足'))
  })

  it('does not call review when reject prompt cancelled', async () => {
    render(<AdminIndicatorReview />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('动量突破 Pro')).toBeTruthy())
    fireEvent.click(screen.getAllByRole('button', { name: '拒绝' })[0])
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(review).not.toHaveBeenCalled()
  })
})
