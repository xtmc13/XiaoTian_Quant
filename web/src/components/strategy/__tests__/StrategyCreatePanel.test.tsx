import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrategyCreatePanel } from '../StrategyCreatePanel'
import * as useStrategyDataModule from '@/hooks/useStrategyData'
import * as useToastModule from '@/lib/useToast'
import { configApi, strategyApi } from '@/lib/api'
import type { ExchangeConfiguredStatus } from '@/types'

const mockConfigured: Record<string, ExchangeConfiguredStatus> = {
  binance: { enabled: true, has_credentials: true, testnet: false, futures: true },
  okx: { enabled: true, has_credentials: true, testnet: false, futures: true },
}

vi.mock('@/hooks/useStrategyData', () => ({
  useStrategyData: vi.fn(),
}))

vi.mock('@/lib/useToast', async () => {
  const actual = await vi.importActual<typeof import('@/lib/useToast')>('@/lib/useToast')
  return {
    ...actual,
    toast: vi.fn(),
  }
})

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    configApi: {
      exchangesConfigured: vi.fn(),
    },
    strategyApi: {
      ...(actual.strategyApi || {}),
      createTemplate: vi.fn(),
    },
  }
})

function wrapper({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}

describe('StrategyCreatePanel', () => {
  const create = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(useStrategyDataModule.useStrategyData).mockReturnValue({
      create,
    } as unknown as ReturnType<typeof useStrategyDataModule.useStrategyData>)
    vi.mocked(configApi.exchangesConfigured).mockResolvedValue(mockConfigured)
  })

  it('renders basic fields and exchange selector', () => {
    render(<StrategyCreatePanel market="spot" onClose={vi.fn()} onSaved={vi.fn()} />, { wrapper })
    expect(screen.getByPlaceholderText('输入策略名称')).toBeTruthy()
    expect(screen.getByText('点击选择')).toBeTruthy()
  })

  it('opens exchange modal and selects an exchange', async () => {
    render(<StrategyCreatePanel market="spot" onClose={vi.fn()} onSaved={vi.fn()} />, { wrapper })
    fireEvent.click(screen.getByText('点击选择'))
    await waitFor(() => expect(screen.getByText('Binance')).toBeTruthy())
    fireEvent.click(screen.getByText('Binance'))
    fireEvent.click(screen.getByText('确认选择'))
    expect(screen.getByText('binance')).toBeTruthy()
  })

  it('shows validation error when no exchange is selected', async () => {
    render(<StrategyCreatePanel market="spot" onClose={vi.fn()} onSaved={vi.fn()} />, { wrapper })
    fireEvent.change(screen.getByPlaceholderText('输入策略名称'), { target: { value: 'Test' } })
    fireEvent.click(screen.getByText('保存策略'))
    await waitFor(() => expect(useToastModule.toast).toHaveBeenCalledWith('error', '请至少选择一个交易所'))
    expect(create).not.toHaveBeenCalled()
  })

  it('submits payload with selected exchanges', async () => {
    const onSaved = vi.fn()
    render(<StrategyCreatePanel market="spot" onClose={vi.fn()} onSaved={onSaved} />, { wrapper })

    fireEvent.change(screen.getByPlaceholderText('输入策略名称'), { target: { value: 'Test Strategy' } })
    // 现货网格校验：补填区间（下限/上限），否则提交被即时校验拦截
    fireEvent.change(screen.getByPlaceholderText('40000'), { target: { value: '40000' } })
    fireEvent.change(screen.getByPlaceholderText('50000'), { target: { value: '50000' } })
    fireEvent.click(screen.getByText('点击选择'))
    await waitFor(() => expect(screen.getByText('Binance')).toBeTruthy())
    fireEvent.click(screen.getByText('Binance'))
    fireEvent.click(screen.getByText('确认选择'))

    fireEvent.click(screen.getByText('保存策略'))
    await waitFor(() => expect(create).toHaveBeenCalled())

    const payload = create.mock.calls[0][0] as Record<string, unknown>
    const config = JSON.parse(payload.config_json as string) as Record<string, unknown>
    expect(config.selected_exchanges).toEqual(['binance'])
  })

  it('saves as default template when checkbox is checked', async () => {
    vi.mocked(strategyApi.createTemplate).mockResolvedValue({ id: 'tpl-1' } as unknown as never)
    const onSaved = vi.fn()
    render(<StrategyCreatePanel market="spot" onClose={vi.fn()} onSaved={onSaved} />, { wrapper })

    fireEvent.change(screen.getByPlaceholderText('输入策略名称'), { target: { value: 'Test Strategy' } })
    // 现货网格校验：补填区间（下限/上限），否则提交被即时校验拦截
    fireEvent.change(screen.getByPlaceholderText('40000'), { target: { value: '40000' } })
    fireEvent.change(screen.getByPlaceholderText('50000'), { target: { value: '50000' } })
    fireEvent.click(screen.getByText('点击选择'))
    await waitFor(() => expect(screen.getByText('Binance')).toBeTruthy())
    fireEvent.click(screen.getByText('Binance'))
    fireEvent.click(screen.getByText('确认选择'))

    fireEvent.click(screen.getByText('保存为默认策略模板'))
    fireEvent.click(screen.getByText('保存策略'))
    await waitFor(() => expect(create).toHaveBeenCalled())
    await waitFor(() => expect(strategyApi.createTemplate).toHaveBeenCalled())

    const templatePayload = (strategyApi.createTemplate as ReturnType<typeof vi.fn>).mock.calls[0][0]
    expect(templatePayload.name).toBe('Test Strategy')
    expect(templatePayload.category).toBe('spot')
    expect(templatePayload.default_config.strategy_type).toBe('cra_spot')
  })
})

describe('StrategyCreatePanel 类型派生（无类型下拉）', () => {
  const create = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(useStrategyDataModule.useStrategyData).mockReturnValue({
      create,
    } as unknown as ReturnType<typeof useStrategyDataModule.useStrategyData>)
    vi.mocked(configApi.exchangesConfigured).mockResolvedValue(mockConfigured)
  })

  it('按市场派生 strategy_type：spot→cra_spot，contract→cra_contract', async () => {
    const { unmount } = render(<StrategyCreatePanel market="spot" onClose={vi.fn()} onSaved={vi.fn()} />, { wrapper })
    fireEvent.change(screen.getByPlaceholderText('输入策略名称'), { target: { value: 'Spot策略' } })
    fireEvent.change(screen.getByPlaceholderText('40000'), { target: { value: '40000' } })
    fireEvent.change(screen.getByPlaceholderText('50000'), { target: { value: '50000' } })
    fireEvent.click(screen.getByText('点击选择'))
    await waitFor(() => expect(screen.getByText('Binance')).toBeTruthy())
    fireEvent.click(screen.getByText('Binance'))
    fireEvent.click(screen.getByText('确认选择'))
    fireEvent.click(screen.getByText('保存策略'))
    await waitFor(() => expect(create).toHaveBeenCalled())
    expect((create.mock.calls[0][0] as Record<string, unknown>).strategy_type).toBe('cra_spot')
    unmount()

    render(<StrategyCreatePanel market="contract" onClose={vi.fn()} onSaved={vi.fn()} />, { wrapper })
    fireEvent.change(screen.getByPlaceholderText('输入策略名称'), { target: { value: 'Contract策略' } })
    fireEvent.click(screen.getByText('点击选择'))
    await waitFor(() => expect(screen.getByText('Binance')).toBeTruthy())
    fireEvent.click(screen.getByText('Binance'))
    fireEvent.click(screen.getByText('确认选择'))
    fireEvent.click(screen.getByText('保存策略'))
    await waitFor(() => expect(create.mock.calls.length).toBe(2))
    expect((create.mock.calls[1][0] as Record<string, unknown>).strategy_type).toBe('cra_contract')
  })

  it('initialType 覆盖：编辑/旧快捷入口的非 cra 原类型保留不改', async () => {
    render(<StrategyCreatePanel market="spot" initialType="martin_trend" onClose={vi.fn()} onSaved={vi.fn()} />, {
      wrapper,
    })
    fireEvent.change(screen.getByPlaceholderText('输入策略名称'), { target: { value: '马丁趋势' } })
    fireEvent.click(screen.getByText('点击选择'))
    await waitFor(() => expect(screen.getByText('Binance')).toBeTruthy())
    fireEvent.click(screen.getByText('Binance'))
    fireEvent.click(screen.getByText('确认选择'))
    fireEvent.click(screen.getByText('保存策略'))
    await waitFor(() => expect(create).toHaveBeenCalled())
    expect((create.mock.calls[0][0] as Record<string, unknown>).strategy_type).toBe('martin_trend')
  })
})

describe('现货策略类型 → CRA 参数档案联动', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(configApi.exchangesConfigured).mockResolvedValue(mockConfigured)
  })

  it('档案常量：马丁趋势 7 档倍投 ladder / 华尔街斐波那契 / 网格等差', async () => {
    const { SPOT_TYPE_PROFILES, buildSpotGridLadder } = await import('../StrategyCreateForm')
    const martin = SPOT_TYPE_PROFILES.martin_trend
    if (martin.kind !== 'ladder') throw new Error('martin must be ladder profile')
    expect(martin.firstOrderAmount).toBe(20)
    expect(martin.addPositions.map((a) => a.multiplier)).toEqual([1, 2, 4, 8, 16, 32, 64])
    expect(martin.addPositions).toHaveLength(7)
    expect(martin.addPositions[6].spread).toBe(21) // 3% × 7
    expect(martin.addPositions[0].callback).toBe(0.5)
    const ws = SPOT_TYPE_PROFILES.wallstreet
    if (ws.kind !== 'ladder') throw new Error('wallstreet must be ladder profile')
    expect(ws.addPositions.map((a) => a.multiplier)).toEqual([1, 1, 2, 3, 5, 8, 13, 21])
    expect(ws.addPositions).toHaveLength(8)
    // 现货网格档案 = 网格语义（格数/每格金额）；ladder 由 buildSpotGridLadder 生成
    const grid = SPOT_TYPE_PROFILES.cra_spot
    expect(grid.kind).toBe('grid')
    if (grid.kind === 'grid') {
      expect(grid.gridCount).toBe(5)
      expect(grid.perGridAmount).toBe(50)
    }
    const agg = SPOT_TYPE_PROFILES.aggressive
    if (agg.kind !== 'ladder') throw new Error('aggressive must be ladder profile')
    expect(agg.addPositions).toHaveLength(10)
    // 网格 ladder：长度=格数-1，spread 等差
    const ladder = buildSpotGridLadder(40000, 50000, 5)
    expect(ladder).toHaveLength(4)
    // UI 百分比单位：每格 0.2/5=4%
    expect(ladder.map((a) => a.spread)).toEqual([4, 8, 12, 16])
    expect(ladder[0]).toMatchObject({ order: 1, multiplier: 1, callback: 0.3 })
  })

  it('spot 面板切「马丁趋势」→ 首单额度 20 / 补仓次数 7（CRA 参数跟随）', () => {
    render(<StrategyCreatePanel market="spot" onClose={vi.fn()} onSaved={vi.fn()} />, { wrapper })
    // 切换类型选择器
    fireEvent.change(screen.getByDisplayValue('现货网格'), { target: { value: 'martin_trend' } })
    // 联动提示可见
    expect(screen.getByText('选择类型将套用对应参数档案，可继续微调')).toBeTruthy()
    // CRA 参数跟随档案：首单额度 20、补仓次数 7
    expect((screen.getByDisplayValue('20') as HTMLInputElement).value).toBe('20')
    expect((screen.getByDisplayValue('7') as HTMLInputElement).value).toBe('7')
    // 未联动字段保持默认（杠杆仅合约不渲染）
    expect(screen.queryByText('杠杆（全仓）')).toBeFalsy()
  })

  it('合约市场切类型不联动档案（合约无类型选择器，仅验证不报错）', () => {
    render(<StrategyCreatePanel market="contract" onClose={vi.fn()} onSaved={vi.fn()} />, { wrapper })
    expect(screen.queryByText('策略类型')).toBeFalsy()
    // 合约默认首单额度（createDefaultCRAParams(contract)=5）保持，不被档案污染
    expect(screen.getAllByDisplayValue('5').length).toBeGreaterThan(0)
    expect(screen.queryByDisplayValue('20')).toBeFalsy()
  })
})

describe('现货网格模式（cra_spot → 网格参数组）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(useStrategyDataModule.useStrategyData).mockReturnValue({
      create: vi.fn().mockResolvedValue({ id: 'x' }),
    } as unknown as ReturnType<typeof useStrategyDataModule.useStrategyData>)
    vi.mocked(configApi.exchangesConfigured).mockResolvedValue(mockConfigured)
  })

  it('渲染网格四字段，隐藏 CRA 补仓区与开仓数字字段', () => {
    render(<StrategyCreatePanel market="spot" onClose={vi.fn()} onSaved={vi.fn()} />, { wrapper })
    expect(screen.getByText('网格参数')).toBeTruthy()
    expect(screen.getByText('区间下限')).toBeTruthy()
    expect(screen.getByText('区间上限')).toBeTruthy()
    expect(screen.getByText('格数 (2-200)')).toBeTruthy()
    expect(screen.getByText('每格金额 (USDT)')).toBeTruthy()
    // 隐藏：CRA 补仓区 + 开仓设置数字字段
    expect(screen.queryByText('补仓设置')).toBeFalsy()
    expect(screen.queryByText('首单额度 (USDT)')).toBeFalsy()
    expect(screen.queryByText('首单加倍')).toBeFalsy()
    // 开仓指标仍可见
    expect(screen.getByText('开仓指标（策略选择）')).toBeTruthy()
  })

  it('改格数→提交 config.add_positions 长度=格数-1 且等差；自定义键落库', async () => {
    render(<StrategyCreatePanel market="spot" onClose={vi.fn()} onSaved={vi.fn()} />, { wrapper })
    fireEvent.change(screen.getByPlaceholderText('输入策略名称'), { target: { value: '现货网格A' } })
    fireEvent.change(screen.getByPlaceholderText('40000'), { target: { value: '40000' } })
    fireEvent.change(screen.getByPlaceholderText('50000'), { target: { value: '50000' } })
    fireEvent.change(screen.getByPlaceholderText('5'), { target: { value: '7' } })
    fireEvent.change(screen.getByPlaceholderText('50'), { target: { value: '30' } })
    fireEvent.click(screen.getByText('点击选择'))
    await waitFor(() => expect(screen.getByText('Binance')).toBeTruthy())
    fireEvent.click(screen.getByText('Binance'))
    fireEvent.click(screen.getByText('确认选择'))
    fireEvent.click(screen.getByText('保存策略'))
    const create = vi.mocked(useStrategyDataModule.useStrategyData).mock.results[0].value.create as ReturnType<
      typeof vi.fn
    >
    await waitFor(() => expect(create).toHaveBeenCalled())
    const payload = create.mock.calls[0][0] as Record<string, unknown>
    expect(payload.strategy_type).toBe('cra_spot')
    const config = JSON.parse(payload.config_json as string) as Record<string, unknown>
    // 自定义键
    expect(config.price_lower).toBe(40000)
    expect(config.price_upper).toBe(50000)
    expect(config.grid_count).toBe(7)
    expect(config.per_grid_amount).toBe(30)
    // CRA 映射键：ladder 长度=格数-1，spread 等差，每格金额→first_order_amount
    expect(config.first_order_amount).toBe(30)
    expect(config.order_count).toBe(7)
    const ladder = config.add_positions as { order: number; spread: number; multiplier: number; callback: number }[]
    expect(ladder).toHaveLength(6)
    // 每格步长 = (50000-40000)/50000/7 ≈ 0.285714%（小数）
    const expected = [1, 2, 3, 4, 5, 6].map((i) => (10000 / 50000 / 7) * i)
    ladder.forEach((l, idx) => expect(l.spread).toBeCloseTo(expected[idx], 6))
    expect(ladder.every((l) => l.multiplier === 1)).toBe(true)
    expect(ladder[0].callback).toBeCloseTo(0.003, 6)
  })

  it('现货网格 → 切「马丁趋势」：恢复 CRA 补仓区，网格参数组隐藏', () => {
    render(<StrategyCreatePanel market="spot" onClose={vi.fn()} onSaved={vi.fn()} />, { wrapper })
    expect(screen.getByText('网格参数')).toBeTruthy()
    fireEvent.change(screen.getByDisplayValue('现货网格'), { target: { value: 'martin_trend' } })
    expect(screen.getByText('补仓设置')).toBeTruthy()
    expect(screen.getByText('首单额度 (USDT)')).toBeTruthy()
    expect(screen.queryByText('网格参数')).toBeFalsy()
    // 马丁档案联动：首单额度 20 / 补仓次数 7
    expect((screen.getByDisplayValue('20') as HTMLInputElement).value).toBe('20')
    expect((screen.getByDisplayValue('7') as HTMLInputElement).value).toBe('7')
  })

  it('现货网格区间非法时提交被拦截（即时校验）', async () => {
    render(<StrategyCreatePanel market="spot" onClose={vi.fn()} onSaved={vi.fn()} />, { wrapper })
    fireEvent.change(screen.getByPlaceholderText('输入策略名称'), { target: { value: '坏区间' } })
    fireEvent.change(screen.getByPlaceholderText('40000'), { target: { value: '50000' } })
    fireEvent.change(screen.getByPlaceholderText('50000'), { target: { value: '40000' } })
    fireEvent.click(screen.getByText('保存策略'))
    const create = vi.mocked(useStrategyDataModule.useStrategyData).mock.results[0].value.create as ReturnType<
      typeof vi.fn
    >
    await new Promise((r) => setTimeout(r, 50))
    expect(create).not.toHaveBeenCalled()
    expect(
      (useToastModule.toast as unknown as ReturnType<typeof vi.fn>).mock.calls.some((c) => c[0] === 'error')
    ).toBe(true)
  })
})
