import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AnalysisPage } from '../AnalysisPage'
import { analysisApi } from '@/lib/api'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    analysisApi: {
      startLookahead: vi.fn(),
      startRecursive: vi.fn(),
      jobs: vi.fn(),
      job: vi.fn(),
    },
  }
})

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      {children}
    </QueryClientProvider>
  )
}

const biasedJob = {
  id: 'ana-1',
  user_id: 1,
  kind: 'lookahead' as const,
  status: 'completed' as const,
  symbol: 'BTCUSDT',
  interval: '1h',
  strategy_type: 'sma_cross',
  params: '{}',
  result: JSON.stringify({
    conclusion: 'biased',
    biased: true,
    confidence: 'high',
    total_entries: 12,
    checked_entries: 10,
    false_entry_count: 3,
    variant_count: 15,
    baseline_entries: [],
    variants: [
      {
        name: 'entry_cut@1700000000000',
        kind: 'entry_cut',
        n: 100,
        variant_bars: 100,
        total_entries: 4,
        false_entries: [
          {
            signal: { time: 1700000000000, index: 99, direction: 'LONG' },
            variant: 'entry_cut@1700000000000',
            kind: 'missing',
            detail: '数据截断到入场根即消失',
          },
        ],
        missing_count: 1,
        displaced_count: 0,
      },
    ],
    summary: '检出前视偏差：15 个变体回测中共 3 处异常入场信号',
  }),
  created_at: 1700000000000,
  updated_at: 1700000001000,
}

describe('AnalysisPage 偏差检测', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(analysisApi.jobs).mockResolvedValue([])
    vi.mocked(analysisApi.job).mockResolvedValue(null as never)
    vi.mocked(analysisApi.startLookahead).mockResolvedValue({ job_id: 'ana-1', status: 'started' })
  })

  it('渲染页头与新建检测表单，可发起 lookahead 分析', async () => {
    render(<AnalysisPage />, { wrapper })
    expect(screen.getByText('偏差检测')).toBeTruthy()

    fireEvent.click(screen.getByText('新建检测'))
    await waitFor(() => expect(screen.getByText('开始检测')).toBeTruthy())
    fireEvent.click(screen.getByText('开始检测'))

    await waitFor(() =>
      expect(analysisApi.startLookahead).toHaveBeenCalledWith(
        expect.objectContaining({ strategy_type: 'sma_cross', symbol: 'BTCUSDT', interval: '1h' })
      )
    )
  })

  it('任务列表渲染并可查看检出偏差的结果详情', async () => {
    vi.mocked(analysisApi.jobs).mockResolvedValue([biasedJob])
    vi.mocked(analysisApi.job).mockResolvedValue({
      id: biasedJob.id,
      kind: 'lookahead',
      status: 'completed',
      symbol: 'BTCUSDT',
      interval: '1h',
      strategy_type: 'sma_cross',
      created_at: biasedJob.created_at,
      updated_at: biasedJob.updated_at,
      result: JSON.parse(biasedJob.result),
    })

    render(<AnalysisPage />, { wrapper })
    await waitFor(() => expect(screen.getByText('ana-1')).toBeTruthy())

    fireEvent.click(screen.getByText('ana-1'))
    await waitFor(() => expect(screen.getByText(/检出前视偏差：15/)).toBeTruthy())
    expect(screen.getByText('偏差嫌疑信号明细')).toBeTruthy()
    expect(screen.getByText('信号消失')).toBeTruthy()
  })
})
