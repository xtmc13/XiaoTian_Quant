import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RuntimePanel } from '../RuntimePanel'
import { strategyApi } from '@/lib/api'
import type { StrategyItem, StrategyRuntimeResponse } from '@/types'

vi.mock('@/lib/api', () => ({
  strategyApi: {
    runtime: vi.fn(),
    manualAction: vi.fn(() => Promise.resolve({ status: 'ok', detail: 'ok' })),
  },
}))

const strategy = {
  id: 's1',
  name: '运行实例',
  symbol: 'BTCUSDT',
  timeframe: '15m',
  status: 'running',
  market_type: 'swap',
} as unknown as StrategyItem

function runtimeResp(status: Record<string, unknown> | null): StrategyRuntimeResponse {
  return { status: status as StrategyRuntimeResponse['status'], price: 100, config: {} }
}

function renderPanel() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <RuntimePanel strategy={strategy} />
    </QueryClientProvider>
  )
}

describe('RuntimePanel 手动操控区挂载口径', () => {
  beforeEach(() => vi.clearAllMocks())

  it('CRA 引擎实例（RuntimeStatus 透出 add_position_enabled）→ 显示手动操控区', async () => {
    vi.mocked(strategyApi.runtime).mockResolvedValue(
      runtimeResp({ running: true, in_position: false, add_position_enabled: true, entry_paused: false })
    )
    renderPanel()
    await waitFor(() => expect(screen.getByTestId('manual-action-panel')).toBeTruthy())
  })

  it('非 CRA 策略（无 add_position_enabled 键）→ 不显示手动操控区', async () => {
    vi.mocked(strategyApi.runtime).mockResolvedValue(
      runtimeResp({ running: true, in_position: false, bars_collected: 30 })
    )
    renderPanel()
    await waitFor(() => expect(screen.getByText(/已收集 K 线/)).toBeTruthy())
    expect(screen.queryByTestId('manual-action-panel')).toBeNull()
  })

  it('引擎无运行详情（status=null）→ 空态且不显示操控区', async () => {
    vi.mocked(strategyApi.runtime).mockResolvedValue(runtimeResp(null))
    renderPanel()
    await waitFor(() => expect(screen.getByText('暂无运行详情')).toBeTruthy())
    expect(screen.queryByTestId('manual-action-panel')).toBeNull()
  })
})
