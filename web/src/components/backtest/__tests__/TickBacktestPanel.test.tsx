import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { I18nProvider } from '@/i18n'
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/tick'
import { TickBacktestPanel } from '@/components/backtest/TickBacktestPanel'
import { tickBacktestApi } from '@/lib/api'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    tickBacktestApi: {
      run: vi.fn(),
      jobs: vi.fn(),
      job: vi.fn(),
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

const doneJob = {
  id: 'tick_bt_1',
  user_id: 1,
  status: 'done',
  started_at: 1700000000000,
  ended_at: 1700000005000,
  result: {
    total_return: 250.5,
    total_return_pct: 2.51,
    max_drawdown: 120.0,
    max_drawdown_pct: 1.2,
    sharpe_ratio: 1.35,
    sortino_ratio: 1.5,
    calmar_ratio: 1.1,
    win_rate: 0.6,
    profit_factor: 1.8,
    total_trades: 10,
    winning_trades: 6,
    losing_trades: 4,
    avg_win: 60,
    avg_loss: -30,
    best_trade: 120,
    worst_trade: -55,
    duration_ms: 5000,
    ticks_processed: 8800,
  },
}

describe('TickBacktestPanel', () => {
  beforeEach(() => vi.clearAllMocks())

  it('提交 Tick 回测并轮询任务列表', async () => {
    vi.mocked(tickBacktestApi.jobs).mockResolvedValue([])
    vi.mocked(tickBacktestApi.run).mockResolvedValue({ status: 'started', job_id: 'tick_bt_9' })
    render(<TickBacktestPanel />, { wrapper })

    await waitFor(() => expect(screen.getByText('暂无 Tick 回测任务')).toBeTruthy())
    fireEvent.click(screen.getByText('运行 Tick 回测'))

    await waitFor(() =>
      expect(tickBacktestApi.run).toHaveBeenCalledWith(
        expect.objectContaining({ strategy: 'sma_cross', symbol: 'BTCUSDT', start: 0 })
      )
    )
  })

  it('任务列表渲染完成状态与结果指标', async () => {
    vi.mocked(tickBacktestApi.jobs).mockResolvedValue([doneJob])
    render(<TickBacktestPanel />, { wrapper })

    await waitFor(() => expect(screen.getByText('tick_bt_1')).toBeTruthy())
    fireEvent.click(screen.getByText('tick_bt_1'))

    await waitFor(() => expect(screen.getByText('+2.51%')).toBeTruthy())
    expect(screen.getByText('1.20%')).toBeTruthy()
    expect(screen.getByText('8,800')).toBeTruthy()
  })

  it('失败任务展示错误信息', async () => {
    vi.mocked(tickBacktestApi.jobs).mockResolvedValue([
      { ...doneJob, id: 'tick_bt_2', status: 'failed', result: undefined, error: 'insufficient tick data' },
    ])
    render(<TickBacktestPanel />, { wrapper })

    await waitFor(() => expect(screen.getByText('tick_bt_2')).toBeTruthy())
    fireEvent.click(screen.getByText('tick_bt_2'))
    await waitFor(() => expect(screen.getByText(/insufficient tick data/)).toBeTruthy())
  })
})
