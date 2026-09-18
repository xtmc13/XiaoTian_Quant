import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { SpotStrategyForm } from '../SpotStrategyForm'
import * as useToastModule from '@/lib/useToast'
import { configApi, strategyApi } from '@/lib/api'
import type { ExchangeConfiguredStatus } from '@/types'

const mockConfigured: Record<string, ExchangeConfiguredStatus> = {
  binance: { enabled: true, has_credentials: true, testnet: false, futures: true },
}

vi.mock('@/lib/useToast', async () => {
  const actual = await vi.importActual<typeof import('@/lib/useToast')>('@/lib/useToast')
  return { ...actual, toast: vi.fn() }
})

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    strategyApi: { ...(actual.strategyApi || {}), create: vi.fn(), update: vi.fn() },
    configApi: { exchangesConfigured: vi.fn() },
  }
})

function wrapper({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}

describe('SpotStrategyForm（现货专用表单）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(configApi.exchangesConfigured).mockResolvedValue(mockConfigured)
    vi.mocked(strategyApi.create).mockResolvedValue({ id: 's1' } as never)
    vi.mocked(strategyApi.update).mockResolvedValue({} as never)
  })

  it('渲染现货参数区字段（区间/格数/每格金额/循环/费率）', () => {
    render(<SpotStrategyForm onSaved={vi.fn()} />, { wrapper })
    expect(screen.getByText('价格下限')).toBeTruthy()
    expect(screen.getByText('价格上限')).toBeTruthy()
    expect(screen.getByText('格数')).toBeTruthy()
    expect(screen.getByText('每格金额')).toBeTruthy()
    expect(screen.getByText('手续费率')).toBeTruthy()
    expect(screen.getByText('循环模式')).toBeTruthy()
    // 顺势多/顺势空预设已整体下线（合约/现货均不显示）
    expect(screen.queryByText('顺势多')).toBeFalsy()
    expect(screen.queryByText('顺势空')).toBeFalsy()
    expect(screen.getByText('MACD')).toBeTruthy()
  })

  it('提交：自定义键 + CRA 映射键双写，strategy_type=cra_spot', async () => {
    const create = vi.mocked(strategyApi.create)
    const ref = { current: null as null | { submit: () => void } }
    render(<SpotStrategyForm ref={(r) => { ref.current = r }} onSaved={vi.fn()} />, { wrapper })

    fireEvent.change(screen.getByPlaceholderText('输入策略名称'), { target: { value: '现货网格' } })
    fireEvent.change(screen.getByPlaceholderText('40000'), { target: { value: '40000' } })
    fireEvent.change(screen.getByPlaceholderText('50000'), { target: { value: '50000' } })
    fireEvent.click(screen.getByText('点击选择'))
    await waitFor(() => expect(screen.getByText('Binance')).toBeTruthy())
    fireEvent.click(screen.getByText('Binance'))
    fireEvent.click(screen.getByText('确认选择'))

    await waitFor(() => expect(ref.current).toBeTruthy())
    ref.current!.submit()
    await waitFor(() => expect(create).toHaveBeenCalled())
    const payload = create.mock.calls[0][0] as Record<string, unknown>
    expect(payload.strategy_type).toBe('cra_spot')
    expect(payload.market_type).toBe('spot')
    const config = JSON.parse(payload.config_json as string) as Record<string, unknown>
    // 自定义键
    expect(config.price_lower).toBe(40000)
    expect(config.price_upper).toBe(50000)
    expect(config.grid_count).toBe(5)
    expect(config.per_grid_amount).toBe(50)
    expect(config.loop_mode).toBe('cycle')
    // CRA 引擎映射键
    expect(config.first_order_amount).toBe(50)
    expect(config.order_count).toBe(5)
    expect(config.direction).toBe('long')
    const ladder = config.add_positions as { order: number; spread: number }[]
    expect(ladder.length).toBe(4)
    expect(ladder[0].order).toBe(1)
    expect(ladder[3].spread).toBeCloseTo(((50000 - 40000) / 50000 / 5) * 4, 6)
    // 止盈 = 单格跌幅
    expect(config.take_profit_ratio).toBeCloseTo((50000 - 40000) / 50000 / 5, 6)
  })

  it('策略类型预设：切「马丁趋势」→ payload.strategy_type=martin_trend 且映射 CRA 键齐全', async () => {
    const create = vi.mocked(strategyApi.create)
    const ref = { current: null as null | { submit: () => void } }
    render(<SpotStrategyForm ref={(r) => { ref.current = r }} onSaved={vi.fn()} />, { wrapper })

    fireEvent.click(screen.getByText('马丁趋势'))
    fireEvent.change(screen.getByPlaceholderText('输入策略名称'), { target: { value: '马丁现货' } })
    fireEvent.change(screen.getByPlaceholderText('40000'), { target: { value: '40000' } })
    fireEvent.change(screen.getByPlaceholderText('50000'), { target: { value: '50000' } })
    fireEvent.click(screen.getByText('点击选择'))
    await waitFor(() => expect(screen.getByText('Binance')).toBeTruthy())
    fireEvent.click(screen.getByText('Binance'))
    fireEvent.click(screen.getByText('确认选择'))

    await waitFor(() => expect(ref.current).toBeTruthy())
    ref.current!.submit()
    await waitFor(() => expect(create).toHaveBeenCalled())
    const payload = create.mock.calls[0][0] as Record<string, unknown>
    expect(payload.strategy_type).toBe('martin_trend')
    const config = JSON.parse(payload.config_json as string) as Record<string, unknown>
    // 马丁档案：7 格 / 每格 20U；映射 CRA 键齐全（checkStrategyTypeConfigMatch 要求）
    expect(config.grid_count).toBe(7)
    expect(config.per_grid_amount).toBe(20)
    expect(config.first_order_amount).toBe(20)
    expect(config.order_count).toBe(7)
    expect(Array.isArray(config.add_positions)).toBe(true)
    expect((config.add_positions as unknown[]).length).toBe(6)
    expect(config.trade_count_mode).toBe('cycle')
  })

  it('编辑回填：strategy_type 为 wallstreet 时预设卡选中态正确', () => {
    render(
      <SpotStrategyForm
        onSaved={vi.fn()}
        editId="x1"
        initial={{
          strategyType: 'wallstreet',
          priceLower: 40000,
          priceUpper: 50000,
          gridCount: 8,
          perGridAmount: 30,
          loopMode: 'cycle',
          feeRate: 0.0008,
        }}
      />,
      { wrapper }
    )
    // 华尔街卡片处于选中高亮（含档案摘要 8格 · 30U/格）
    expect(screen.getByText('8格 · 30U/格 · 循环')).toBeTruthy()
    // 未知类型回退 cra_spot（组件内 normalizeSpotType）：默认渲染即 cra_spot 选中
  })

  it('区间校验：上限≤下限时拦截不提交', async () => {
    const create = vi.mocked(strategyApi.create)
    const ref = { current: null as null | { submit: () => void } }
    render(<SpotStrategyForm ref={(r) => { ref.current = r }} onSaved={vi.fn()} />, { wrapper })
    fireEvent.change(screen.getByPlaceholderText('输入策略名称'), { target: { value: '坏区间' } })
    fireEvent.change(screen.getByPlaceholderText('40000'), { target: { value: '50000' } })
    fireEvent.change(screen.getByPlaceholderText('50000'), { target: { value: '40000' } })
    await waitFor(() => expect(ref.current).toBeTruthy())
    ref.current!.submit()
    await waitFor(() =>
      expect(
        (useToastModule.toast as unknown as ReturnType<typeof vi.fn>).mock.calls.some((c) => c[0] === 'error')
      ).toBe(true)
    )
    expect(create).not.toHaveBeenCalled()
  })
})
