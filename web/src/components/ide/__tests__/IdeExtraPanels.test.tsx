import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { I18nProvider } from '@/i18n'
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/ide'
import { PythonSandboxPanel, ExperimentsPanel } from '@/components/ide/IdeExtraPanels'
import { indicatorApi } from '@/lib/api'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    indicatorApi: {
      ...actual.indicatorApi,
      runPython: vi.fn(),
      experiment: {
        ...actual.indicatorApi.experiment,
        list: vi.fn(),
        status: vi.fn(),
      },
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

const bars = [{ time: 1700000000000, open: 1, high: 2, low: 1, close: 2, volume: 10 }]

describe('PythonSandboxPanel（/strategies-python/run）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('沙箱运行提交代码与 K 线并渲染返回信号', async () => {
    vi.mocked(indicatorApi.runPython).mockResolvedValue({
      count: 1,
      signals: [{ time: 1700000000, bar_index: 5, action: 'buy', price: 70100, reason: '金叉' }],
    })
    render(
      <PythonSandboxPanel code="print(1)" symbol="BTCUSDT" interval="1h" params={{ fast: 9 }} bars={bars} />,
      { wrapper }
    )

    fireEvent.click(screen.getByText('Python 沙箱运行'))
    fireEvent.click(screen.getByText('沙箱运行'))

    await waitFor(() =>
      expect(indicatorApi.runPython).toHaveBeenCalledWith(
        expect.objectContaining({ mode: 'indicator', code: 'print(1)', symbol: 'BTCUSDT', interval: '1h' })
      )
    )
    await waitFor(() => expect(screen.getByText('金叉')).toBeTruthy())
    expect(screen.getByText('buy')).toBeTruthy()
  })

  it('引擎不可用时错误透出 toast', async () => {
    vi.mocked(indicatorApi.runPython).mockRejectedValue(new Error('python strategy engine is not available'))
    const { toast } = await import('@/lib/useToast')
    render(<PythonSandboxPanel code="print(1)" symbol="BTCUSDT" interval="1h" params={{}} bars={bars} />, { wrapper })

    fireEvent.click(screen.getByText('Python 沙箱运行'))
    fireEvent.click(screen.getByText('沙箱运行'))

    await waitFor(() =>
      expect(vi.mocked(toast)).toHaveBeenCalledWith(
        'error',
        expect.stringContaining('python strategy engine is not available')
      )
    )
  })
})

describe('ExperimentsPanel（/experiments）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('展开后渲染实验列表（状态/样本内外收益）', async () => {
    vi.mocked(indicatorApi.experiment.list).mockResolvedValue([
      {
        id: 'exp-1',
        experiment_id: 'exp-1',
        name: 'de-tune',
        status: 'completed',
        best_score: 88.5,
        duration_ms: 42000,
        created_at: '2026-10-01T00:00:00Z',
        is_return_pct: 12.3,
        oos_return_pct: -2.1,
      },
    ])
    render(<ExperimentsPanel />, { wrapper })

    fireEvent.click(screen.getByText('实验记录'))
    await waitFor(() => expect(screen.getByText('de-tune')).toBeTruthy())
    expect(screen.getByText('completed')).toBeTruthy()
    expect(screen.getByText('+12.30%')).toBeTruthy()
    expect(screen.getByText('-2.10%')).toBeTruthy()
  })

  it('空列表显示空态', async () => {
    vi.mocked(indicatorApi.experiment.list).mockResolvedValue([])
    render(<ExperimentsPanel />, { wrapper })

    fireEvent.click(screen.getByText('实验记录'))
    await waitFor(() => expect(screen.getByText(/暂无实验记录/)).toBeTruthy())
  })
})
