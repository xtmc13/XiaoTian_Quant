import { describe, it, expect, vi, beforeEach } from 'vitest'
import React from 'react'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/community'
import { I18nProvider } from '@/i18n'
import { AuthorDashboard } from '../../pages/AuthorDashboard'
import type { AuthorRevenueData } from '@/lib/api'

const authorRevenue = vi.fn<() => Promise<AuthorRevenueData>>(() =>
  Promise.resolve({
    total_sales: 6,
    total_revenue: 210.5,
    details: [{ indicator_id: 21, sales: 6, revenue: 210.5 }],
  })
)

vi.mock('@/lib/api', () => ({
  indicatorApi: {
    list: () =>
      Promise.resolve([
        {
          id: 21,
          name: '我的指标A',
          pricing_type: 'paid',
          price: 50,
          purchase_count: 4,
          avg_rating: 4.5,
          rating_count: 2,
          view_count: 10,
          review_status: 'approved',
          created_at: 0,
          updated_at: 0,
        },
      ]),
    delete: () => Promise.resolve({ success: true }),
  },
  communityApi: {
    authorRevenue: () => authorRevenue(),
  },
}))

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

function Wrapper({ children }: { children: React.ReactNode }) {
  return (
    <MemoryRouter>
      <I18nProvider>{children}</I18nProvider>
    </MemoryRouter>
  )
}

describe('AuthorDashboard（作者收益卡）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders revenue totals and per-indicator breakdown from /community/author/revenue', async () => {
    render(<AuthorDashboard />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('作者收益')).toBeTruthy())
    expect(authorRevenue).toHaveBeenCalled()
    // 总收益 / 累计销量（实账）
    expect(screen.getByText('累计收益')).toBeTruthy()
    expect(screen.getByText('累计销量')).toBeTruthy()
    // 总计与明细行各出现一次
    expect(screen.getAllByText('210.50').length).toBeGreaterThan(0)
    // 分指标明细：indicator_id 映射为指标名
    await waitFor(() => expect(screen.getAllByText('我的指标A').length).toBeGreaterThan(0))
    expect(screen.getByText('分指标明细')).toBeTruthy()
  })

  it('does not crash when revenue details is null (backend returns null slice)', async () => {
    authorRevenue.mockResolvedValueOnce({
      total_sales: 0,
      total_revenue: 0,
      details: null,
    })
    render(<AuthorDashboard />, { wrapper: Wrapper })
    await waitFor(() => expect(screen.getByText('作者收益')).toBeTruthy())
    // 空明细兜底为「暂无收益记录」，而不是读取 null.length 崩溃
    await waitFor(() => expect(screen.getByText('暂无收益记录')).toBeTruthy())
  })
})
