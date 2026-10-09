import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RuntimePanel } from '../RuntimePanel'
import { strategyApi } from '@/lib/api'
import { I18nProvider } from '@/i18n'
import '@/i18n/locales/runtime'
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
    <I18nProvider>
      <QueryClientProvider client={qc}>
        <RuntimePanel strategy={strategy} />
      </QueryClientProvider>
    </I18nProvider>
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

// H4：pending_close_kind 在途平仓徽标——仅在途时显示（键存在性语义），
// 标签按 kind 走 i18n 词条，未知 kind 回退原始字符串。
describe('RuntimePanel 平仓在途徽标（H4）', () => {
  beforeEach(() => vi.clearAllMocks())

  const inPosStatus = {
    running: true,
    in_position: true,
    direction: 'long',
    position_qty: 1,
    entry_price: 100,
    avg_entry_price: 100,
    add_position_enabled: true,
    entry_paused: false,
  }

  it('无 pending_close_kind 键 → 不显示徽标（零行为变化）', async () => {
    vi.mocked(strategyApi.runtime).mockResolvedValue(runtimeResp({ ...inPosStatus }))
    renderPanel()
    await waitFor(() => expect(screen.getByText(/已收集 K 线/)).toBeTruthy())
    expect(screen.queryByTestId('pending-close-badge')).toBeNull()
  })

  it('pending_close_kind=tail → 显示「平仓在途：尾单止盈」', async () => {
    vi.mocked(strategyApi.runtime).mockResolvedValue(
      runtimeResp({ ...inPosStatus, pending_close_kind: 'tail' })
    )
    renderPanel()
    await waitFor(() => expect(screen.getByTestId('pending-close-badge')).toBeTruthy())
    expect(screen.getByTestId('pending-close-badge').textContent).toContain('平仓在途：尾单止盈')
  })

  it('各在途形态标签：reverse_sl/burn_global/manual_close 等', async () => {
    const cases: Array<[string, string]> = [
      ['head_tail', '首尾止盈'],
      ['reverse_tp', '反向止盈'],
      ['reverse_sl', '反向止损'],
      ['burn_dual', '对向燃烧斩仓'],
      ['burn_global', '全局燃烧斩仓'],
      ['manual_reduce', '手动减仓'],
      ['manual_close', '手动清仓'],
    ]
    for (const [kind, label] of cases) {
      vi.mocked(strategyApi.runtime).mockResolvedValue(
        runtimeResp({ ...inPosStatus, pending_close_kind: kind })
      )
      const { unmount } = renderPanel()
      await waitFor(() => expect(screen.getByTestId('pending-close-badge')).toBeTruthy())
      expect(screen.getByTestId('pending-close-badge').textContent, `kind=${kind}`).toContain(label)
      unmount()
    }
  })

  it('未知 kind → 回退原始字符串（不空白不报错）', async () => {
    vi.mocked(strategyApi.runtime).mockResolvedValue(
      runtimeResp({ ...inPosStatus, pending_close_kind: 'future_kind_x' })
    )
    renderPanel()
    await waitFor(() => expect(screen.getByTestId('pending-close-badge')).toBeTruthy())
    expect(screen.getByTestId('pending-close-badge').textContent).toContain('future_kind_x')
  })
})
