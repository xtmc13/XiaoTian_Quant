import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BotsSignal } from '../../../pages/bots/BotsSignal'
import { executorApi } from '@/lib/api'

vi.mock('@/lib/api', () => ({
  executorApi: {
    getStatus: vi.fn(),
    getActivePositions: vi.fn(),
    getExecutionRecords: vi.fn(),
    getSignalSources: vi.fn(),
    getStats: vi.fn(),
  },
}))

const STATS = {
  total_signals: 128,
  today_signals: 5,
  success_rate: 62.5,
  tp1_rate: 70,
  tp2_rate: 45,
  tp3_rate: 20,
  avg_signals_per_day: 4.3,
  total_pnl: 320.5,
  pnl_curve: [
    { date: '2026-09-24', pnl: 100, signals: 4 },
    { date: '2026-09-25', pnl: 220, signals: 6 },
    { date: '2026-09-26', pnl: 320.5, signals: 5 },
  ],
  by_symbol: [
    { symbol: 'BTCUSDT', success_rate: 70, pnl: 200, signals: 60 },
    { symbol: 'ETHUSDT', success_rate: 50, pnl: -30, signals: 40 },
  ],
}

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      {children}
    </QueryClientProvider>
  )
}

describe('BotsSignal 信号机器人统计页', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(executorApi.getStats).mockResolvedValue(STATS as never)
    vi.mocked(executorApi.getStatus).mockResolvedValue({
      data: {
        active_positions: 0,
        pending_signals: 0,
        today_executed: 0,
        today_pnl: 0,
        tp1_executed: 0,
        tp2_executed: 0,
        tp3_executed: 0,
        sl_triggered: 0,
        status: 'running',
      },
    } as never)
    vi.mocked(executorApi.getActivePositions).mockResolvedValue({ data: { positions: [] } } as never)
    vi.mocked(executorApi.getExecutionRecords).mockResolvedValue({ data: { records: [] } } as never)
    vi.mocked(executorApi.getSignalSources).mockResolvedValue({ data: { sources: [] } } as never)
  })

  it('渲染统计 KPI：总信号/今日信号/成功率/阶梯达成/日均/累计盈亏', async () => {
    const { container } = render(<BotsSignal />, { wrapper })
    expect(screen.getByText('信号统计')).toBeTruthy()
    await waitFor(() => expect(executorApi.getStats).toHaveBeenCalled())
    await waitFor(() => expect(screen.getByText('128')).toBeTruthy())
    expect(screen.getByText('5')).toBeTruthy()
    // 阶梯达成：T1 主值 + T2/T3 副值
    expect(screen.getByText('T1 70%')).toBeTruthy()
    expect(screen.getByText('T2 45% / T3 20%')).toBeTruthy()
    expect(screen.getByText('4.3')).toBeTruthy()
    // 62.5% 同时出现在卡片值与环形进度上
    expect(screen.getAllByText('62.5%').length).toBeGreaterThan(0)
    expect(screen.getByText('+320.50')).toBeTruthy()
    // 盈亏曲线 SVG
    expect(container.querySelector('svg')).toBeTruthy()
  })

  it('渲染分交易对统计表', async () => {
    render(<BotsSignal />, { wrapper })
    await waitFor(() => expect(screen.getByText('BTCUSDT')).toBeTruthy())
    expect(screen.getByText('ETHUSDT')).toBeTruthy()
    expect(screen.getByText('分交易对统计')).toBeTruthy()
    expect(screen.getByText('+200.00')).toBeTruthy()
    expect(screen.getByText('-30.00')).toBeTruthy()
  })

  it('统计接口失败时展示空态而不阻塞执行器面板', async () => {
    vi.mocked(executorApi.getStats).mockRejectedValue(new Error('stats not ready') as never)
    render(<BotsSignal />, { wrapper })
    await waitFor(() => expect(screen.getByText('暂无信号统计数据')).toBeTruthy())
    // 执行器面板照常渲染
    expect(screen.getByText('信号执行器')).toBeTruthy()
    await waitFor(() => expect(screen.getByText(/运行中/)).toBeTruthy())
  })

  it('信号来源展示定价信息（免费/固定月费/盈利分成）', async () => {
    vi.mocked(executorApi.getSignalSources).mockResolvedValue({
      data: {
        sources: [
          {
            id: 'src-1',
            name: 'TradingView Webhook',
            type: 'webhook',
            enabled: true,
            signal_count_today: 2,
            signal_count_total: 10,
            fee_model: 'profit_share',
            fee_percent: 20,
          },
          {
            id: 'src-2',
            name: '内部策略',
            type: 'internal',
            enabled: true,
            signal_count_today: 1,
            signal_count_total: 3,
            fee_model: 'free',
          },
        ],
      },
    } as never)
    render(<BotsSignal />, { wrapper })
    await waitFor(() => expect(screen.getByText('TradingView Webhook')).toBeTruthy())
    expect(screen.getByText('盈利分成 20%')).toBeTruthy()
    expect(screen.getByText('免费')).toBeTruthy()
  })
})
