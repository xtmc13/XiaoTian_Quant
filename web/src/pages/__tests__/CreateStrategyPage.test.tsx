import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CreateStrategyPage } from '../CreateStrategyPage'
import * as useStrategyDataModule from '@/hooks/useStrategyData'
import { configApi, strategyApi, strategyVersionApi } from '@/lib/api'
// 词典按 import 副作用注册：L11 文案断言需要 zh-CN 词条就位。
import '@/i18n/locales/dashboard'

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

/** L12（2026-10-10）：保证金模式创建可选、编辑回填真实值（修复前创建硬编码
 * cross、回填不读——逐仓配置永远显示全仓）。 */
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
