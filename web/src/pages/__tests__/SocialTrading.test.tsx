import { describe, it, expect, vi, beforeEach } from 'vitest'
import React from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { SocialTrading } from '../../pages/SocialTrading'

// Mock must be self-contained (vitest hoists vi.mock above module-level vars)
// Use plain arrow functions instead of vi.fn() to avoid clearAllMocks side effects
vi.mock('@/lib/api', () => ({
  socialApi: {
    providers: () => Promise.resolve([
      {
        provider_id: 1,
        total_signals: 10,
        win_count: 7,
        loss_count: 3,
        win_rate: 0.7,
        avg_return_pct: 5.2,
        sharpe_ratio: 1.8,
        max_drawdown_pct: 8.5,
        follower_count: 42,
        monthly_fee: 0,
        is_public: true,
      },
      {
        provider_id: 2,
        total_signals: 5,
        win_count: 2,
        loss_count: 3,
        win_rate: 0.4,
        avg_return_pct: -1.5,
        sharpe_ratio: 0.5,
        max_drawdown_pct: 12.0,
        follower_count: 10,
        monthly_fee: 9.99,
        is_public: true,
      },
    ]),
    signals: () => Promise.resolve([
      {
        id: 'sig-1',
        provider_id: 1,
        provider_name: 'Trader A',
        symbol: 'BTCUSDT',
        direction: 'buy',
        price: 50000,
        stop_loss: 48000,
        take_profit: 55000,
        size: 0.1,
        confidence: 85,
        strategy: 'breakout',
        reason: '突破阻力位',
        timestamp: Date.now(),
        expires_at: Date.now() + 3600000,
      },
    ]),
    follow: () => Promise.resolve({ success: true }),
    unfollow: () => Promise.resolve({ success: true }),
    publishSignal: () => Promise.resolve({ signal: { id: 'new-sig' } }),
  },
}))

// Mock toast store
vi.mock('@/stores/toastStore', () => ({
  useToastStore: () => ({
    addToast: vi.fn(),
  }),
}))

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: 0 },
      mutations: { retry: false },
    },
  })
}

function Wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={createTestQueryClient()}>
      {children}
    </QueryClientProvider>
  )
}

describe('SocialTrading', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders providers tab by default', async () => {
    render(<SocialTrading />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText('Provider #1')).toBeTruthy()
    })
    expect(screen.getByText('Provider #2')).toBeTruthy()
  })

  it('displays provider stats correctly', async () => {
    render(<SocialTrading />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText('70.0%')).toBeTruthy() // win rate
    })
    expect(screen.getByText('5.20%')).toBeTruthy() // avg return
    expect(screen.getByText('1.80')).toBeTruthy() // sharpe
  })

  it('switches to signals tab', async () => {
    render(<SocialTrading />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText('Provider #1')).toBeTruthy()
    })
    const signalsTab = screen.getByText('信号流')
    fireEvent.click(signalsTab)
    await waitFor(() => {
      expect(screen.getByText('BTCUSDT')).toBeTruthy()
    })
  })

  it('switches to following tab', async () => {
    render(<SocialTrading />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText('Provider #1')).toBeTruthy()
    })
    const followingTab = screen.getByText('我的关注')
    fireEvent.click(followingTab)
    expect(screen.getByText('暂无关注的信号源')).toBeTruthy()
  })

  it('shows monthly fee badge for paid providers', async () => {
    render(<SocialTrading />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText('$9.99/月')).toBeTruthy()
    })
  })

  it('opens publish signal modal', async () => {
    render(<SocialTrading />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText('Provider #1')).toBeTruthy()
    })
    const publishBtn = screen.getByText('发布信号')
    fireEvent.click(publishBtn)
    expect(screen.getByText('发布交易信号')).toBeTruthy()
  })
})
