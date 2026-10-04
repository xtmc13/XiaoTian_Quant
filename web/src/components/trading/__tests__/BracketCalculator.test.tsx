import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { I18nProvider } from '@/i18n'
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/orderkit'
import { BracketCalculator } from '@/components/trading/BracketCalculator'
import { advancedOrderApi } from '@/lib/api'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    advancedOrderApi: {
      ...actual.advancedOrderApi,
      bracket: { ...actual.advancedOrderApi.bracket, calculate: vi.fn() },
    },
  }
})

vi.mock('@/lib/useToast', async () => {
  const actual = await vi.importActual<typeof import('@/lib/useToast')>('@/lib/useToast')
  return { ...actual, toast: vi.fn() }
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

describe('BracketCalculator', () => {
  beforeEach(() => vi.clearAllMocks())

  it('试算展示 TP/SL/仓位/盈亏比，应用到表单回调', async () => {
    vi.mocked(advancedOrderApi.bracket.calculate).mockResolvedValue({
      entry_price: 70000,
      take_profit: 72800,
      stop_loss: 68600,
      position_size: 0.714286,
      risk_amount: 100,
      reward_amount: 2000,
      risk_reward: 2,
    })
    const onApply = vi.fn()
    render(<BracketCalculator symbol="BTCUSDT" side="buy" entryPrice={70000} onApply={onApply} />, { wrapper })

    fireEvent.click(screen.getByText('试算'))
    await waitFor(() =>
      expect(advancedOrderApi.bracket.calculate).toHaveBeenCalledWith(
        expect.objectContaining({ entry_price: 70000, side: 'buy', stop_loss_pct: 0.02, take_profit_pct: 0.04 })
      )
    )
    await waitFor(() => expect(screen.getByText('72,800.00')).toBeTruthy())
    expect(screen.getByText('68,600.00')).toBeTruthy()
    expect(screen.getByText('1 : 2.00')).toBeTruthy()

    fireEvent.click(screen.getByText('应用到表单'))
    expect(onApply).toHaveBeenCalledWith({
      takeProfitPrice: 72800,
      stopLossPrice: 68600,
      quantity: 0.714286,
    })
  })

  it('非法输入被拦截并提示', async () => {
    const { toast } = await import('@/lib/useToast')
    render(<BracketCalculator symbol="BTCUSDT" side="buy" entryPrice={0} onApply={() => {}} />, { wrapper })

    fireEvent.click(screen.getByText('试算'))
    await waitFor(() =>
      expect(vi.mocked(toast)).toHaveBeenCalledWith('warning', '请输入有效的入场价与百分比')
    )
    expect(advancedOrderApi.bracket.calculate).not.toHaveBeenCalled()
  })
})
