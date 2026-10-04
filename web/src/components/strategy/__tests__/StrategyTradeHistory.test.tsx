import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrategyTradeHistory } from '../StrategyTradeHistory'
import type { StrategyItem } from '@/types'
import { tradesApi } from '@/lib/api'

vi.mock('@/lib/api', () => ({
  tradesApi: { list: vi.fn() },
}))

const strategy = {
  id: 'abc123',
  name: '测试策略',
  symbol: 'BTCUSDT',
  timeframe: '15m',
} as unknown as StrategyItem

function renderWithClient(ui: React.ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>)
}

describe('StrategyTradeHistory 成交记录', () => {
  beforeEach(() => vi.clearAllMocks())

  it('按策略过滤渲染成交流水，K线链接携带 symbol/interval/t 跳转参数', async () => {
    vi.mocked(tradesApi.list).mockResolvedValue([
      {
        id: 'ord-1',
        symbol: 'BTCUSDT',
        side: 'BUY',
        price: 84644.45,
        qty: 0.001,
        notional: 84.64,
        pnl: 0,
        time: 1791025183000,
        exchange: 'paper',
        strategy_id: 'abc123',
      },
      {
        id: 'ord-2',
        symbol: 'BTCUSDT',
        side: 'SELL',
        price: 85000,
        qty: 0.001,
        notional: 85,
        pnl: 0.36,
        time: 1791030000000,
        exchange: 'paper',
        strategy_id: 'abc123',
      },
    ] as never)
    renderWithClient(<StrategyTradeHistory strategy={strategy} />)

    await waitFor(() => expect(screen.getByText('买入')).toBeTruthy())
    expect(screen.getByText('卖出')).toBeTruthy()
    expect(screen.getByText('+$0.36')).toBeTruthy()
    expect(vi.mocked(tradesApi.list)).toHaveBeenCalledWith(
      expect.objectContaining({ strategy_id: 'abc123' })
    )
    const linkEls = Array.from(document.querySelectorAll('a[href^="/trading/spot?"]'))
    expect(linkEls.length).toBe(2)
    expect(linkEls[0].getAttribute('href')).toContain(
      '/trading/spot?symbol=BTCUSDT&interval=15m&t=1791025183000'
    )
  })

  it('无成交时展示空态', async () => {
    vi.mocked(tradesApi.list).mockResolvedValue([] as never)
    renderWithClient(<StrategyTradeHistory strategy={strategy} />)
    await waitFor(() => expect(screen.getByText(/暂无成交/)).toBeTruthy())
  })
})
