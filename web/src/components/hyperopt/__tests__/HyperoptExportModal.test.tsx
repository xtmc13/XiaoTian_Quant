import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { I18nProvider } from '@/i18n'
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/orderkit'
import { HyperoptExportModal } from '@/components/hyperopt/HyperoptExportModal'
import { hyperoptApi } from '@/lib/api'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    hyperoptApi: { ...actual.hyperoptApi, exportParams: vi.fn() },
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

const bestParams = { fast_period: 12, slow_period: 26 }

describe('HyperoptExportModal', () => {
  beforeEach(() => vi.clearAllMocks())

  it('确认导出携带恒等参数映射与可选策略名', async () => {
    vi.mocked(hyperoptApi.exportParams).mockResolvedValue({
      status: 'exported',
      job_id: 'job-1',
      strategy_type: 'sma_cross',
      mapped_params: { fast_period: 12, slow_period: 26 },
      config_path: 'strategy_configs.json',
    })
    render(<HyperoptExportModal jobId="job-1" bestParams={bestParams} onClose={() => {}} />, { wrapper })

    // 覆盖警告（二次确认语义）可见
    expect(screen.getByText(/将覆盖目标策略的同名字段/)).toBeTruthy()

    fireEvent.click(screen.getByText('确认导出'))
    await waitFor(() =>
      expect(hyperoptApi.exportParams).toHaveBeenCalledWith('job-1', {
        strategy_id: undefined,
        strategy_name: undefined,
        param_map: { fast_period: 'fast_period', slow_period: 'slow_period' },
      })
    )
    // 成功后展示已映射参数
    await waitFor(() => expect(screen.getByText('已映射参数')).toBeTruthy())
  })

  it('可编辑映射目标字段并随请求提交', async () => {
    vi.mocked(hyperoptApi.exportParams).mockResolvedValue({
      status: 'exported',
      job_id: 'job-1',
      strategy_type: 'sma_cross',
      mapped_params: { fast: 12, slow_period: 26 },
      config_path: 'strategy_configs.json',
    })
    render(<HyperoptExportModal jobId="job-1" bestParams={bestParams} onClose={() => {}} />, { wrapper })

    fireEvent.change(screen.getByLabelText('map-fast_period'), { target: { value: 'fast' } })
    fireEvent.click(screen.getByText('确认导出'))
    await waitFor(() =>
      expect(hyperoptApi.exportParams).toHaveBeenCalledWith(
        'job-1',
        expect.objectContaining({ param_map: { fast_period: 'fast', slow_period: 'slow_period' } })
      )
    )
  })

  it('清空全部映射后拦截并提示', async () => {
    const { toast } = await import('@/lib/useToast')
    render(<HyperoptExportModal jobId="job-1" bestParams={bestParams} onClose={() => {}} />, { wrapper })

    fireEvent.change(screen.getByLabelText('map-fast_period'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('map-slow_period'), { target: { value: '' } })
    fireEvent.click(screen.getByText('确认导出'))

    await waitFor(() => expect(vi.mocked(toast)).toHaveBeenCalledWith('warning', '至少保留一条参数映射'))
    expect(hyperoptApi.exportParams).not.toHaveBeenCalled()
  })
})
