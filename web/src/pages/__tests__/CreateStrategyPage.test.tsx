import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CreateStrategyPage } from '../CreateStrategyPage'
import * as useStrategyDataModule from '@/hooks/useStrategyData'
import { configApi, strategyApi, strategyVersionApi } from '@/lib/api'
// 词典按 import 副作用注册：L11 文案断言需要 zh-CN 词条就位。
import '@/i18n/locales/dashboard'

// 本文件为两线测试并集：
// - 编辑回填（L11/L12：交易所如实标注、margin_mode 回填）——renderPage 驱动；
// - 指标策略快捷卡（?indicator=xx）创建页预选：创建预选生效/非法 indicator 忽略/
//   编辑场景回填优先于预选——renderCreate 驱动；
// - 策略身份说明条：快捷卡进入显示"壳 + 指标"组合语义，编辑模式不显示。
// 仅打桩网络边界（api/hooks），表单与选择器为真实渲染。

vi.mock('@/hooks/useStrategyData', () => ({
  useStrategyData: vi.fn(),
}))

// jsdom 无 IntersectionObserver（步骤导航高亮用），打桩为 no-op。
vi.stubGlobal(
  'IntersectionObserver',
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
)

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    configApi: { exchangesConfigured: vi.fn() },
    strategyApi: {
      ...(actual.strategyApi || {}),
      get: vi.fn(),
      update: vi.fn(),
      defaults: vi.fn(),
      paramDefs: vi.fn(),
      createTemplate: vi.fn(),
    },
    strategyVersionApi: {
      ...(actual.strategyVersionApi || {}),
      list: vi.fn(),
    },
  }
})

// 历史版本（仅编辑模式渲染）不在本测试范围
vi.mock('@/components/strategy/StrategyVersionHistory', () => ({ StrategyVersionHistory: () => null }))

function renderPage(search: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`/create${search}`]}>
        <CreateStrategyPage />
      </MemoryRouter>
    </QueryClientProvider>
  )
}

function renderCreate(url: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[url]}>
        <CreateStrategyPage />
      </MemoryRouter>
    </QueryClientProvider>
  )
}

/** L12（2026-10-10）：保证金模式创建可选、编辑回填真实值（修复前创建硬编码
 * cross、回填不读——逐仓配置永远显示全仓）。 */

// 工作周期不再硬编码默认（2026-10-10）：CRA 类型无开仓指标时为空需显式选择，
// 测试统一在提交前补选；无下拉（非 CRA/编辑回填有值）时 no-op。
const pickTimeframeIfNeeded = () => {
  const sel = screen.queryByTestId('timeframe-select')
  if (sel && (sel as HTMLSelectElement).value === '') {
    fireEvent.change(sel, { target: { value: '15m' } })
  }
}
describe('CreateStrategyPage 编辑回填（L12 margin_mode）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(useStrategyDataModule.useStrategyData).mockReturnValue({
      create: vi.fn(),
    } as unknown as ReturnType<typeof useStrategyDataModule.useStrategyData>)
    vi.mocked(configApi.exchangesConfigured).mockResolvedValue({})
    vi.mocked(strategyApi.defaults).mockRejectedValue(new Error('skip'))
    vi.mocked(strategyApi.paramDefs).mockResolvedValue({ type: 'cra_contract', params: [] } as never)
    vi.mocked(strategyVersionApi.list).mockResolvedValue([] as never)
  })

  it('编辑逐仓策略：回填后「逐仓」为选中态', async () => {
    vi.mocked(strategyApi.get).mockResolvedValue({
      id: 'edit-mm-1',
      name: '逐仓策略',
      symbol: 'BTCUSDT',
      market_type: 'swap',
      margin_mode: 'cross', // 顶层列残值（历史创建硬编码）
      strategy_type: 'cra_contract',
      config_json: JSON.stringify({ market_type: 'swap', margin_mode: 'isolated', leverage: 5 }),
    } as never)

    renderPage('?market=contract&id=edit-mm-1')
    // 回填到达后逐仓按钮进入选中态（config_json 优先于顶层残值）。
    await waitFor(() => {
      const isolatedBtn = screen.getByText('逐仓')
      expect(isolatedBtn.className).toContain('text-quant-gold')
    })
    expect(screen.getByText('全仓').className).not.toContain('text-quant-gold')
  })

  it('编辑无 margin_mode 的旧记录：默认全仓选中', async () => {
    vi.mocked(strategyApi.get).mockResolvedValue({
      id: 'edit-mm-2',
      name: '旧策略',
      symbol: 'BTCUSDT',
      market_type: 'swap',
      strategy_type: 'cra_contract',
      config_json: JSON.stringify({ market_type: 'swap', leverage: 10 }),
    } as never)

    renderPage('?market=contract&id=edit-mm-2')
    await waitFor(() => {
      expect(screen.getByText('全仓').className).toContain('text-quant-gold')
    })
  })

  it('L11：交易所选择器下如实标注"仅记录偏好"', async () => {
    vi.mocked(strategyApi.get).mockResolvedValue({
      id: 'edit-mm-3',
      name: 'x',
      symbol: 'BTCUSDT',
      market_type: 'swap',
      strategy_type: 'cra_contract',
      config_json: '{}',
    } as never)
    renderPage('?market=contract&id=edit-mm-3')
    await waitFor(() => {
      expect(screen.getByText('仅记录偏好，实际下单交易所由账户凭证决定')).toBeTruthy()
    })
  })
})

describe('CreateStrategyPage ?indicator= 预选（指标策略快捷卡）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(useStrategyDataModule.useStrategyData).mockReturnValue({
      create: vi.fn(),
    } as unknown as ReturnType<typeof useStrategyDataModule.useStrategyData>)
    vi.mocked(configApi.exchangesConfigured).mockResolvedValue({})
    vi.mocked(strategyApi.defaults).mockRejectedValue(new Error('no defaults'))
    vi.mocked(strategyApi.paramDefs).mockResolvedValue({ params: [] } as never)
  })

  it('indicator=ema_cross：picker 初始选中 EMA交叉（提交 payload 带派生引擎键）', async () => {
    renderCreate('/create?market=contract&type=cra_contract&indicator=ema_cross')
    // 选中态的指标卡才有 ⚙ 参数按钮（aria-label="<label> 参数"）
    await waitFor(() => expect(screen.getByLabelText('EMA交叉 参数')).toBeTruthy())
    // 未选中其他指标
    expect(screen.queryByLabelText('MACD 参数')).toBeNull()
    // 说明行显示当前开仓指标
    expect(screen.getByText('当前开仓指标：')).toBeTruthy()
  })

  it('indicator=bollinger：picker 初始选中 布林带', async () => {
    renderCreate('/create?market=contract&type=cra_contract&indicator=bollinger')
    await waitFor(() => expect(screen.getByLabelText('布林带 参数')).toBeTruthy())
  })

  it('非法 indicator 忽略：picker 保持未设置（壳默认）', async () => {
    renderCreate('/create?market=contract&type=cra_contract&indicator=foo')
    await screen.findByText('未设置（将使用壳默认）：首单不受指标门槛限制。')
    for (const label of ['EMA交叉', 'MACD', 'RSI超卖', '布林带']) {
      expect(screen.queryByLabelText(`${label} 参数`)).toBeNull()
    }
  })

  it('编辑场景回填优先于预选：记录为 MACD 时忽略 ?indicator=ema_cross', async () => {
    vi.mocked(strategyApi.get).mockResolvedValue({
      id: 's1',
      name: '既有策略',
      symbol: 'BTCUSDT',
      timeframe: '15m',
      market_type: 'swap',
      strategy_type: 'cra_contract',
      status: 'stopped',
      config_json: JSON.stringify({
        open_indicator: 'macd',
        indicator_params: { macd: { fast: 8, slow: 21, signal: 5 } },
      }),
    } as never)
    renderCreate('/create?market=contract&id=s1&indicator=ema_cross')
    await waitFor(() => expect(screen.getByLabelText('MACD 参数')).toBeTruthy())
    expect(screen.queryByLabelText('EMA交叉 参数')).toBeNull()
    // 回填参数来自记录（F8/S21），不是默认值
    expect(screen.getByText('F8/S21/Sig5')).toBeTruthy()
  })
})

describe('CreateStrategyPage 策略身份说明条（壳 + 指标组合语义）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(useStrategyDataModule.useStrategyData).mockReturnValue({
      create: vi.fn(),
    } as unknown as ReturnType<typeof useStrategyDataModule.useStrategyData>)
    vi.mocked(configApi.exchangesConfigured).mockResolvedValue({})
    vi.mocked(strategyApi.defaults).mockRejectedValue(new Error('no defaults'))
    vi.mocked(strategyApi.paramDefs).mockResolvedValue({ params: [] } as never)
  })

  it('indicator=ema_cross：说明条显示「EMA 策略 = 智能补仓/移动止盈壳 + EMA 开仓监测」', async () => {
    renderCreate('/create?market=contract&type=cra_contract&indicator=ema_cross')
    await waitFor(() => expect(screen.getByTestId('strategy-identity-banner')).toBeTruthy())
    expect(screen.getByText('EMA 策略')).toBeTruthy()
    expect(screen.getByText(/= 智能补仓\/移动止盈壳 \+/)).toBeTruthy()
    expect(screen.getByText(/EMA 开仓监测/)).toBeTruthy()
  })

  it('indicator=bollinger：指标名随参数变（布林带 开仓监测）', async () => {
    renderCreate('/create?market=spot&type=cra_spot&indicator=bollinger')
    await waitFor(() => expect(screen.getByTestId('strategy-identity-banner')).toBeTruthy())
    expect(screen.getByText('布林带策略')).toBeTruthy()
    expect(screen.getByText(/布林带 开仓监测/)).toBeTruthy()
  })

  it('非法 indicator 不显示说明条（与预选忽略口径一致）', async () => {
    renderCreate('/create?market=contract&type=cra_contract&indicator=foo')
    await screen.findByText('未设置（将使用壳默认）：首单不受指标门槛限制。')
    expect(screen.queryByTestId('strategy-identity-banner')).toBeNull()
  })

  it('编辑模式不显示说明条（身份由已有配置承担）', async () => {
    vi.mocked(strategyApi.get).mockResolvedValue({
      id: 's1',
      name: '既有策略',
      symbol: 'BTCUSDT',
      timeframe: '15m',
      market_type: 'swap',
      strategy_type: 'cra_contract',
      status: 'stopped',
      config_json: JSON.stringify({ open_indicator: 'macd' }),
    } as never)
    renderCreate('/create?market=contract&id=s1&indicator=ema_cross')
    await waitFor(() => expect(screen.getByLabelText('MACD 参数')).toBeTruthy())
    expect(screen.queryByTestId('strategy-identity-banner')).toBeNull()
  })

  it('快捷卡创建提交：config_json 带 open_indicator + indicator_params（预选指标默认参数）', async () => {
    const create = vi.fn().mockResolvedValue({ id: 'new1' })
    vi.mocked(useStrategyDataModule.useStrategyData).mockReturnValue({
      create,
    } as unknown as ReturnType<typeof useStrategyDataModule.useStrategyData>)
    // 交易所按钮仅在已配置凭证时可点（ExchangeSelectModal ready 口径）
    vi.mocked(configApi.exchangesConfigured).mockResolvedValue({
      binance: { enabled: true, has_credentials: true, testnet: false, futures: true },
    })
    renderCreate('/create?market=contract&type=cra_contract&indicator=ema_cross')
    await waitFor(() => expect(screen.getByLabelText('EMA交叉 参数')).toBeTruthy())
    fireEvent.change(screen.getByPlaceholderText('输入策略名称'), { target: { value: 'EMA快捷策略' } })
    fireEvent.click(screen.getByText('点击选择'))
    await waitFor(() => expect(screen.getByText('Binance')).toBeTruthy())
    fireEvent.click(screen.getByText('Binance'))
    fireEvent.click(screen.getByText('确认选择'))
    pickTimeframeIfNeeded()
    fireEvent.click(screen.getAllByRole('button', { name: '保存策略' })[0])
    await waitFor(() => expect(create).toHaveBeenCalled())
    const payload = create.mock.calls[0][0] as Record<string, unknown>
    expect(payload.strategy_type).toBe('cra_contract')
    const config = JSON.parse(payload.config_json as string) as Record<string, unknown>
    expect(config.open_indicator).toBe('ema_cross')
    expect(config.indicator_params).toMatchObject({ ema_cross: { fast: 12, slow: 26, period: 'close' } })
    // 引擎兼容键同步（顺势 EMA 门槛承载双均线类指标）
    expect(config.open_trend_ema_enabled).toBe(true)
  })
})
