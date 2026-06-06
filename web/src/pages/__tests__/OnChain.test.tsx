import { describe, it, expect, vi, beforeEach } from 'vitest'
import React from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { OnChain } from '../../pages/OnChain'

// Mock must be self-contained (vitest hoists vi.mock above module-level vars)
// Use plain arrow functions instead of vi.fn() to avoid clearAllMocks side effects
vi.mock('@/lib/api', () => ({
  onchainApi: {
    btcMetrics: () => Promise.resolve({
      hash_rate_eh: 450,
      active_addresses: 890000,
      tx_count_24h: 320000,
      avg_tx_fee_usd: 2.5,
      exchange_inflow_btc: 1200,
      exchange_outflow_btc: 3500,
      net_exchange_flow_btc: -2300,
      sopr: 1.02,
      mvrv_ratio: 2.1,
      nupl: 0.35,
      puell_multiple: 0.8,
      stock_to_flow: 55,
    }),
    ethMetrics: () => Promise.resolve({
      gas_price_gwei: 25,
      active_addresses: 520000,
      tx_count_24h: 1100000,
      avg_tx_fee_usd: 1.8,
      staking_apr_pct: 3.8,
      eth_burned_24h: 1200,
      exchange_inflow_eth: 5000,
      exchange_outflow_eth: 8000,
      net_exchange_flow_eth: -3000,
      mvrv_ratio: 1.8,
      nupl: 0.25,
    }),
    btcSignal: () => Promise.resolve({
      symbol: 'BTC',
      direction: 'bullish',
      strength: 72,
      indicators: ['SOPR', 'MVRV', 'Exchange Flow'],
      timestamp: Date.now(),
    }),
    ethSignal: () => Promise.resolve({
      symbol: 'ETH',
      direction: 'neutral',
      strength: 45,
      indicators: ['Gas', 'Staking'],
      timestamp: Date.now(),
    }),
  },
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

describe('OnChain', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders BTC tab by default', async () => {
    render(<OnChain />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText('Bitcoin')).toBeTruthy()
    })
    expect(screen.getByText('算力')).toBeTruthy()
    expect(screen.getByText('450')).toBeTruthy()
  })

  it('displays BTC signal card', async () => {
    render(<OnChain />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText('BTC')).toBeTruthy()
    })
    expect(screen.getByText('看涨')).toBeTruthy()
    expect(screen.getByText('72%')).toBeTruthy()
  })

  it('switches to ETH tab', async () => {
    render(<OnChain />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText('Bitcoin')).toBeTruthy()
    })
    const ethTab = screen.getByText('Ethereum')
    fireEvent.click(ethTab)
    await waitFor(() => {
      expect(screen.getByText('Gas 价格')).toBeTruthy()
    })
    expect(screen.getByText('25')).toBeTruthy()
  })

  it('displays ETH signal card', async () => {
    render(<OnChain />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText('Bitcoin')).toBeTruthy()
    })
    const ethTab = screen.getByText('Ethereum')
    fireEvent.click(ethTab)
    await waitFor(() => {
      expect(screen.getByText('ETH')).toBeTruthy()
    })
    expect(screen.getByText('中性')).toBeTruthy()
  })

  it('shows exchange flow interpretation', async () => {
    render(<OnChain />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText('交易所流向解读')).toBeTruthy()
    })
    expect(screen.getByText(/BTC 流出交易所/)).toBeTruthy()
  })

  it('shows gas analysis for ETH', async () => {
    render(<OnChain />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText('Bitcoin')).toBeTruthy()
    })
    const ethTab = screen.getByText('Ethereum')
    fireEvent.click(ethTab)
    await waitFor(() => {
      expect(screen.getByText('Gas 价格分析')).toBeTruthy()
    })
    expect(screen.getByText(/网络畅通/)).toBeTruthy()
  })

  it('has refresh button', async () => {
    render(<OnChain />, { wrapper: Wrapper })
    await waitFor(() => {
      expect(screen.getByText('刷新')).toBeTruthy()
    })
  })
})
