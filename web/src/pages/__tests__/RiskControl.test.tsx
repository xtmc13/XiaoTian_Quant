import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RiskControl } from '../RiskControl'
import { protectionApi, riskApi } from '@/lib/api'
import * as useToastModule from '@/lib/useToast'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    protectionApi: {
      status: vi.fn(),
      getConfig: vi.fn(),
      config: vi.fn(),
      reset: vi.fn(),
    },
    riskApi: {
      getConfig: vi.fn(),
      updateConfig: vi.fn(),
    },
  }
})

vi.mock('@/lib/useToast', async () => {
  const actual = await vi.importActual<typeof import('@/lib/useToast')>('@/lib/useToast')
  return { ...actual, toast: vi.fn() }
})

// 默认 admin（保存按钮可见）；role 变更通过重新 mockReturnValue 实现。
vi.mock('@/stores/authStore', () => ({
  useAuthStore: vi.fn((selector: (s: unknown) => unknown) =>
    selector({ user: { role: 'admin' } })
  ),
}))

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      {children}
    </QueryClientProvider>
  )
}

describe('RiskControl 风控参数卡片', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(protectionApi.status).mockResolvedValue({} as never)
    vi.mocked(protectionApi.getConfig).mockResolvedValue({ protections: [] } as never)
    vi.mocked(riskApi.getConfig).mockResolvedValue({
      max_concurrent_orders: 3,
      position_limit_pct: 100,
      profit_protection_enabled: true,
      indicator_fail_open: true,
    })
    vi.mocked(riskApi.updateConfig).mockResolvedValue({} as never)
  })

  it('渲染风控参数卡片并 GET 回填', async () => {
    render(<RiskControl />, { wrapper })
    expect(screen.getByText('风控参数')).toBeTruthy()
    expect(screen.getByText('盈利保护')).toBeTruthy()
    // 回填：GET 返回 3 / 100
    await waitFor(() => expect((screen.getByDisplayValue('3') as HTMLInputElement).value).toBe('3'))
    expect((screen.getByDisplayValue('100') as HTMLInputElement).value).toBe('100')
  })

  it('保存 → PUT riskApi.updateConfig（含回填值），成功 toast', async () => {
    render(<RiskControl />, { wrapper })
    await waitFor(() => expect(screen.getByText('保存')).toBeTruthy())
    fireEvent.click(screen.getByText('保存'))
    await waitFor(() => expect(riskApi.updateConfig).toHaveBeenCalled())
    expect(vi.mocked(riskApi.updateConfig).mock.calls[0][0]).toEqual({
      max_concurrent_orders: 3,
      position_limit_pct: 100,
      profit_protection_enabled: true,
      indicator_fail_open: true,
    })
    await waitFor(() =>
      expect(useToastModule.toast).toHaveBeenCalledWith('success', '风控参数已保存并即时生效')
    )
  })

  it('保存前前端校验：最大挂单数越界拦截不提交', async () => {
    render(<RiskControl />, { wrapper })
    await waitFor(() => expect(screen.getByDisplayValue('3')).toBeTruthy())
    fireEvent.change(screen.getByDisplayValue('3'), { target: { value: '99' } })
    fireEvent.click(screen.getByText('保存'))
    await new Promise((r) => setTimeout(r, 50))
    expect(riskApi.updateConfig).not.toHaveBeenCalled()
    expect(
      (useToastModule.toast as unknown as ReturnType<typeof vi.fn>).mock.calls.some(
        (c) => c[0] === 'error' && String(c[1]).includes('最大挂单数')
      )
    ).toBe(true)
  })
})
