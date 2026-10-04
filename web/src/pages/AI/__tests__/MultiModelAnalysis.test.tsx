import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { I18nProvider } from '@/i18n'
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/aiasync'
import { MultiModelAnalysis, mapAsyncToAnalysisResult } from '@/pages/AI/components/MultiModelAnalysis'
import { aiApi, aiRobotApi } from '@/lib/api'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    aiApi: { ...actual.aiApi, analysisStart: vi.fn(), analysisResult: vi.fn() },
    aiRobotApi: { ...actual.aiRobotApi, getModels: vi.fn() },
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

describe('mapAsyncToAnalysisResult', () => {
  it('映射共识与逐模型结果为页面渲染契约', () => {
    const mapped = mapAsyncToAnalysisResult({
      status: 'completed',
      symbol: 'BTCUSDT',
      interval: '1h',
      results: [
        { model: 'deepseek', signal: 'bullish', confidence: 80, reasoning: '趋势向上', timestamp: 1 },
        { model: 'kimi', signal: 'neutral', confidence: 50, reasoning: '震荡', timestamp: 1 },
      ],
      consensus: { signal: 'bullish', bullish: 1, bearish: 0, neutral: 1, total: 2, agreement: 50 },
    })
    expect(mapped.symbol).toBe('BTCUSDT')
    expect(mapped.consensus).toBe('bullish')
    expect(mapped.analyses).toHaveLength(2)
    expect(mapped.analyses[0]).toMatchObject({ model: 'deepseek', sentiment: 'bullish', analysis: '趋势向上' })
    expect(mapped.analyses[1].sentiment).toBe('neutral')
  })
})

describe('MultiModelAnalysis', () => {
  beforeEach(() => vi.clearAllMocks())

  it('模型清单渲染并可启动异步分析，完成后回调 onResult', async () => {
    vi.mocked(aiRobotApi.getModels).mockResolvedValue(['deepseek', 'kimi'])
    vi.mocked(aiApi.analysisStart).mockResolvedValue({ status: 'ok', task_id: 'analysis-1' })
    vi.mocked(aiApi.analysisResult).mockResolvedValue({
      status: 'completed',
      symbol: 'BTCUSDT',
      interval: '1h',
      results: [{ model: 'deepseek', signal: 'bullish', confidence: 90, reasoning: '多头排列', timestamp: 1 }],
      consensus: { signal: 'bullish', bullish: 1, bearish: 0, neutral: 0, total: 1, agreement: 100 },
    })
    const onResult = vi.fn()
    const onClose = vi.fn()
    render(<MultiModelAnalysis open symbol="BTCUSDT" onClose={onClose} onResult={onResult} />, { wrapper })

    // 等模型加载（默认全选）
    await waitFor(() => expect(screen.getByText('deepseek')).toBeTruthy())
    fireEvent.click(screen.getByText('开始分析'))

    await waitFor(() =>
      expect(aiApi.analysisStart).toHaveBeenCalledWith(
        expect.objectContaining({ symbol: 'BTCUSDT', interval: '1h', enabled_models: ['deepseek', 'kimi'] })
      )
    )
    await waitFor(() => expect(aiApi.analysisResult).toHaveBeenCalledWith('analysis-1'))
    await waitFor(() =>
      expect(onResult).toHaveBeenCalledWith(
        expect.objectContaining({ symbol: 'BTCUSDT', consensus: 'bullish' })
      )
    )
    expect(onClose).toHaveBeenCalled()
  })

  it('取消全部模型后启动被拦截并提示', async () => {
    vi.mocked(aiRobotApi.getModels).mockResolvedValue(['deepseek'])
    const { toast } = await import('@/lib/useToast')
    render(<MultiModelAnalysis open symbol="BTCUSDT" onClose={() => {}} onResult={() => {}} />, { wrapper })

    await waitFor(() => expect(screen.getByText('deepseek')).toBeTruthy())
    // 默认全选 → 点掉唯一模型
    fireEvent.click(screen.getByText('deepseek'))
    fireEvent.click(screen.getByText('开始分析'))

    await waitFor(() => expect(vi.mocked(toast)).toHaveBeenCalledWith('warning', '请至少选择一个模型'))
    expect(aiApi.analysisStart).not.toHaveBeenCalled()
  })
})
