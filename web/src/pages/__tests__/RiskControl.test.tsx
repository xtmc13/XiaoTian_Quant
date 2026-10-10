import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RiskControl } from '../RiskControl'
import { protectionApi, riskApi, paperApi } from '@/lib/api'
import { useAuthStore } from '@/stores/authStore'
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
    paperApi: {
      safety: vi.fn(async () => ({
        live_trading_enabled: false,
        confirm_required: true,
        paper_trading_default: true,
        env_live_trading: false,
        config_live_trading: false,
        runtime_override_locked: false,
      })),
      unlock: vi.fn(async () => ({ message: 'ok' })),
      lock: vi.fn(async () => ({ message: 'ok' })),
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
      online_order_limit: 7,
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

  it('在线单量限制（合约策略）字段渲染并 GET 回填', async () => {
    render(<RiskControl />, { wrapper })
    expect(screen.getByText(/在线单量限制（合约策略/)).toBeTruthy()
    await waitFor(() => expect((screen.getByDisplayValue('7') as HTMLInputElement).value).toBe('7'))
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
      online_order_limit: 7,
    })
    await waitFor(() =>
      expect(useToastModule.toast).toHaveBeenCalledWith('success', '风控参数已保存并即时生效')
    )
  })

  it('保存前前端校验：在线单量限制越界拦截不提交', async () => {
    render(<RiskControl />, { wrapper })
    await waitFor(() => expect(screen.getByDisplayValue('7')).toBeTruthy())
    fireEvent.change(screen.getByDisplayValue('7'), { target: { value: '0' } })
    fireEvent.click(screen.getByText('保存'))
    await new Promise((r) => setTimeout(r, 50))
    expect(riskApi.updateConfig).not.toHaveBeenCalled()
    expect(
      (useToastModule.toast as unknown as ReturnType<typeof vi.fn>).mock.calls.some(
        (c) => c[0] === 'error' && String(c[1]).includes('在线单量限制')
      )
    ).toBe(true)
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

describe('RiskControl 实盘总闸卡片', () => {
  const setRole = (role: string) => {
    ;(useAuthStore as unknown as ReturnType<typeof vi.fn>).mockImplementation(
      (selector: (s: unknown) => unknown) => selector({ user: { role } })
    )
  }

  beforeEach(() => {
    vi.clearAllMocks()
    setRole('admin')
    vi.mocked(protectionApi.status).mockResolvedValue({} as never)
    vi.mocked(protectionApi.getConfig).mockResolvedValue({ protections: [] } as never)
    vi.mocked(riskApi.getConfig).mockResolvedValue({} as never)
    vi.mocked(paperApi.safety).mockResolvedValue({
      live_trading_enabled: false,
      confirm_required: true,
      paper_trading_default: true,
      env_live_trading: false,
      config_live_trading: false,
      runtime_override_locked: false,
    } as never)
    vi.mocked(paperApi.unlock).mockResolvedValue({ message: 'ok' } as never)
    vi.mocked(paperApi.lock).mockResolvedValue({ message: 'ok' } as never)
  })

  it('admin 视角：渲染总闸状态、三层来源明细与解锁按钮', async () => {
    render(<RiskControl />, { wrapper })
    expect(screen.getByText('实盘总闸')).toBeTruthy()
    await waitFor(() => expect(screen.getByText(/confirmed=true/)).toBeTruthy())
    expect(screen.getByText('已关闭')).toBeTruthy()
    expect(screen.getByText('当前为模拟盘，真实交易所下单被拦截')).toBeTruthy()
    expect(screen.getByText('配置文件 trading.live_enabled')).toBeTruthy()
    expect(screen.getByText('环境变量 LIVE_TRADING_ENABLED')).toBeTruthy()
    expect(screen.getByText('运行时覆盖')).toBeTruthy()
    expect(screen.getByText('未设置（跟随环境变量/配置）')).toBeTruthy()
    expect(screen.getByText('解锁实盘交易')).toBeTruthy()
    expect(screen.queryByText('锁定（回到模拟盘）')).toBeNull()
  })

  it('admin 解锁：确认弹窗文案不匹配不调用，输入正确文案后调用 paperApi.unlock', async () => {
    render(<RiskControl />, { wrapper })
    await waitFor(() => expect(screen.getByText('解锁实盘交易')).toBeTruthy())

    fireEvent.click(screen.getByText('解锁实盘交易'))
    await waitFor(() => expect(screen.getByRole('dialog')).toBeTruthy())
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '随便写点' } })
    fireEvent.click(screen.getByText('解锁实盘'))
    await waitFor(() =>
      expect(
        (useToastModule.toast as unknown as ReturnType<typeof vi.fn>).mock.calls.some(
          (c) => c[0] === 'error' && String(c[1]).includes('确认文案不匹配')
        )
      ).toBe(true)
    )
    expect(paperApi.unlock).not.toHaveBeenCalled()

    fireEvent.click(screen.getByText('解锁实盘交易'))
    await waitFor(() => expect(screen.getByRole('dialog')).toBeTruthy())
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '我明白将使用真实资金下单' } })
    fireEvent.click(screen.getByText('解锁实盘'))
    await waitFor(() => expect(paperApi.unlock).toHaveBeenCalledTimes(1))
    await waitFor(() =>
      expect(useToastModule.toast).toHaveBeenCalledWith('success', '实盘交易已解锁（运行时，重启后失效）')
    )
  })

  it('非 admin：只读状态，无操作按钮', async () => {
    setRole('user')
    render(<RiskControl />, { wrapper })
    await waitFor(() => expect(screen.getByText('已关闭')).toBeTruthy())
    expect(screen.getByText('实盘总闸')).toBeTruthy()
    expect(screen.queryByText('解锁实盘交易')).toBeNull()
    expect(screen.queryByText('锁定（回到模拟盘）')).toBeNull()
  })

  it('解锁被后端风控口径拦截时透出后端 error 原文', async () => {
    vi.mocked(paperApi.unlock).mockRejectedValue(
      new Error('拒绝解锁实盘：risk.position_limit_pct 当前为 2500%，是 paper 模式放宽值') as never
    )
    render(<RiskControl />, { wrapper })
    await waitFor(() => expect(screen.getByText('解锁实盘交易')).toBeTruthy())
    fireEvent.click(screen.getByText('解锁实盘交易'))
    await waitFor(() => expect(screen.getByRole('dialog')).toBeTruthy())
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '我明白将使用真实资金下单' } })
    fireEvent.click(screen.getByText('解锁实盘'))
    await waitFor(() => expect(paperApi.unlock).toHaveBeenCalledTimes(1))
    await waitFor(() =>
      expect(screen.getByText(/拒绝解锁实盘：risk\.position_limit_pct 当前为 2500%/)).toBeTruthy()
    )
  })
})
