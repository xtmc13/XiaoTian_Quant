import { describe, it, expect, vi, beforeEach } from 'vitest'
import React from 'react'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { I18nProvider } from '@/i18n'
import { MarketData } from '../MarketData'
// 测试环境不经 main.tsx，需显式注册词条（默认语言 zh-CN）
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/nav'
import '@/i18n/locales/marketdata'

// Mock dataProviderApi（vi.mock 会被提升，必须自包含）
vi.mock('@/lib/api', () => ({
  dataProviderApi: {
    sentiment: () =>
      Promise.resolve({
        fear_greed: {
          source: 'fear_greed',
          status: 'ok',
          data: {
            value: 23,
            classification: 'Extreme Fear',
            updated_at: 1758700000,
            history: [
              { value: 23, classification: 'Extreme Fear', timestamp: 1758700000 },
              { value: 31, classification: 'Fear', timestamp: 1758613600 },
            ],
          },
          fetched_at: 1758700000,
          age_sec: 12,
        },
        derivatives: { source: 'coinglass', status: 'not_configured' },
      }),
    heatmap: () =>
      Promise.resolve({
        source: 'heatmap',
        status: 'ok',
        data: {
          entries: [
            { symbol: 'BTCUSDT', base: 'BTC', price: 67000, change_pct_24h: 2.5, volume_24h: 1e9, weight: 1 },
            { symbol: 'ETHUSDT', base: 'ETH', price: 3500, change_pct_24h: -1.2, volume_24h: 5e8, weight: 0.7 },
          ],
          updated_at: 1758700000,
        },
      }),
    news: () =>
      Promise.resolve({
        source: 'news',
        status: 'ok',
        data: {
          items: [
            {
              id: '1',
              title: 'BTC hits new high',
              url: 'https://example.com/1',
              source: 'coindesk',
              published_at: 1758700000,
              categories: ['BTC'],
              summary: 'bitcoin rally',
            },
          ],
          updated_at: 1758700000,
        },
      }),
    macro: () => Promise.resolve({ source: 'fred', status: 'not_configured' }),
    calendar: () =>
      Promise.resolve({
        source: 'calendar',
        status: 'ok',
        data: {
          events: [
            {
              id: 'e1',
              name: 'CPI m/m',
              currency: 'USD',
              date: '2026-09-23',
              time: '8:30am',
              impact: 'high',
              forecast: '0.2%',
              previous: '0.3%',
            },
          ],
          updated_at: 1758700000,
        },
      }),
    sources: () =>
      Promise.resolve([
        {
          name: 'fear_greed',
          description: 'fg',
          configured: true,
          requires_key: false,
          state: 'ok',
          circuit: 'closed',
          failures: 0,
          ttl_sec: 1800,
        },
        {
          name: 'fred',
          description: 'fred',
          configured: false,
          requires_key: true,
          state: 'not_configured',
          circuit: 'closed',
          failures: 0,
          ttl_sec: 21600,
        },
      ]),
  },
}))

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 0 } } })
  return (
    <QueryClientProvider client={client}>
      <I18nProvider>{children}</I18nProvider>
    </QueryClientProvider>
  )
}

describe('MarketData', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders fear & greed gauge with value', async () => {
    render(<MarketData />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText('恐惧贪婪指数')).toBeTruthy()
    })
    await waitFor(() => {
      expect(screen.getByText('23')).toBeTruthy()
    })
    // 23 落在 fear 档（21-40）
    expect(screen.getByText('恐惧')).toBeTruthy()
  })

  it('shows degraded hint for unconfigured derivatives source', async () => {
    render(<MarketData />, { wrapper: Wrapper })
    await waitFor(() => {
      // 未配置 key 的源降级展示，不报错炸页
      expect(screen.getAllByText(/未配置/).length).toBeGreaterThan(0)
    })
  })

  it('renders heatmap entries with change colors', async () => {
    render(<MarketData />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText('市值热力图（24h）')).toBeTruthy()
    })
    await waitFor(() => {
      // 'BTC' 同时出现在热力图块与新闻分类标签中，用 getAllByText
      expect(screen.getAllByText('BTC').length).toBeGreaterThan(0)
      expect(screen.getByText('+2.50%')).toBeTruthy()
      expect(screen.getByText('-1.20%')).toBeTruthy()
    })
  })

  it('renders news feed and economic calendar', async () => {
    render(<MarketData />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText('BTC hits new high')).toBeTruthy()
    })
    await waitFor(() => {
      expect(screen.getByText('CPI m/m')).toBeTruthy()
      expect(screen.getByText('0.2%')).toBeTruthy()
      expect(screen.getByText('0.3%')).toBeTruthy()
    })
  })

  it('shows macro not-configured hint and source health chips', async () => {
    render(<MarketData />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText(/FRED_API_KEY/)).toBeTruthy()
    })
    await waitFor(() => {
      expect(screen.getByText('fear_greed')).toBeTruthy()
      expect(screen.getByText('fred')).toBeTruthy()
    })
  })
})
