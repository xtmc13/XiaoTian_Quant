import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { I18nProvider } from '@/i18n'
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/orderkit'
import { AIBotTradesCard } from '@/components/bots/AIBotTradesCard'
import { aiBotApi } from '@/lib/api'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    aiBotApi: { ...actual.aiBotApi, trades: vi.fn() },
  }
})

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <I18nProvider>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        {children}
      </QueryClientProvider>
    </I18nProvider>
  )
}

describe('AIBotTradesCard', () => {
  beforeEach(() => vi.clearAllMocks())

  it('渲染实例成交记录（含盈亏与平仓原因）', async () => {
    vi.mocked(aiBotApi.trades).mockResolvedValue({
      bot: { id: 'bot-1' } as never,
      trades: [
        {
          id: 1,
          bot_instance_id: 'bot-1',
          symbol: 'BTCUSDT',
          side: 'LONG',
          entry_price: 70000,
          exit_price: 71000,
          quantity: 0.01,
          pnl: 10,
          pnl_pct: 1.43,
          tp_price: 72000,
          sl_price: 69000,
          close_reason: 'take_profit',
          opened_at: 1700000000,
          closed_at: 1700003600,
        },
      ],
    })
    render(<AIBotTradesCard botId="bot-1" />, { wrapper })

    await waitFor(() => expect(screen.getByText('BTCUSDT')).toBeTruthy())
    expect(screen.getByText('LONG')).toBeTruthy()
    expect(screen.getByText(/\+10\.00/)).toBeTruthy()
    expect(screen.getByText('take_profit')).toBeTruthy()
    expect(aiBotApi.trades).toHaveBeenCalledWith('bot-1', 50)
  })

  it('无成交时显示空态', async () => {
    vi.mocked(aiBotApi.trades).mockResolvedValue({ bot: { id: 'bot-1' } as never, trades: [] })
    render(<AIBotTradesCard botId="bot-1" />, { wrapper })

    await waitFor(() => expect(screen.getByText('暂无成交记录')).toBeTruthy())
  })
})
