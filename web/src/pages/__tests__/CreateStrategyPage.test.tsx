import { describe, it, expect, vi, beforeEach } from 'vitest'
import React from 'react'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { CreateStrategyPage } from '../CreateStrategyPage'
import * as useStrategyDataModule from '@/hooks/useStrategyData'
import { configApi, strategyApi } from '@/lib/api'

// 指标策略快捷卡（?indicator=xx）创建页预选测试：
// - 创建场景预选生效（picker 初始选中对应指标，派生引擎兼容键）；
// - 非法 indicator 忽略；
// - 编辑场景回填优先于预选。
// 仅打桩网络边界（api/hooks），表单与选择器为真实渲染。

vi.mock('@/hooks/useStrategyData', () => ({
  useStrategyData: vi.fn(),
}))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    configApi: { exchangesConfigured: vi.fn() },
    strategyApi: {
      ...(actual.strategyApi || {}),
      get: vi.fn(),
      defaults: vi.fn(),
      paramDefs: vi.fn(),
      createTemplate: vi.fn(),
    },
  }
})

// 历史版本（仅编辑模式渲染）不在本测试范围
vi.mock('@/components/strategy/StrategyVersionHistory', () => ({ StrategyVersionHistory: () => null }))

// jsdom 无 IntersectionObserver（页面左侧步骤导航用），打桩为 no-op
vi.stubGlobal(
  'IntersectionObserver',
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
)

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
