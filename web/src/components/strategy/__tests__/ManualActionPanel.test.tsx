import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ManualActionPanel } from '../ManualActionPanel'
import { strategyApi } from '@/lib/api'
import { useToastStore } from '@/stores/toastStore'
import type { StrategyItem, StrategyRuntimeStatus } from '@/types'

vi.mock('@/lib/api', () => ({
  strategyApi: { manualAction: vi.fn(() => Promise.resolve({ status: 'ok', detail: '后端口径文案' })) },
}))

const strategy = {
  id: 's-cra',
  name: 'CRA 测试实例',
  symbol: 'BTCUSDT',
  timeframe: '15m',
  status: 'running',
  market_type: 'spot',
} as unknown as StrategyItem

const inPosStatus: StrategyRuntimeStatus = {
  running: true,
  in_position: true,
  direction: 'long',
  avg_entry_price: 100,
  entry_price: 100,
  position_qty: 1.5,
  add_position_enabled: true,
  entry_paused: false,
  manual_add_count: 0,
}

function renderPanel(status: StrategyRuntimeStatus = inPosStatus, price = 102) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <ManualActionPanel strategy={strategy} status={status} price={price} />
    </QueryClientProvider>
  )
}

describe('G1 手动操控区（币富 #23/#24/#25/#28）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useToastStore.getState().clearAll()
  })

  it('渲染四件套操控：关闭补仓开关/一键补仓/自定义减仓/清仓卖出', () => {
    renderPanel()
    expect(screen.getByTestId('manual-action-panel')).toBeTruthy()
    expect(screen.getByRole('button', { name: /关闭补仓/ })).toBeTruthy()
    expect(screen.getByRole('button', { name: /一键补仓/ })).toBeTruthy()
    expect(screen.getByRole('button', { name: /减仓/ })).toBeTruthy()
    expect(screen.getByRole('button', { name: /清仓卖出/ })).toBeTruthy()
    // 开关态如实显示当前状态
    expect(screen.getByText('开启中')).toBeTruthy()
  })

  it('关闭补仓开关：提交 toggle_add_position 并 toast 透出后端 detail', async () => {
    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: /关闭补仓/ }))
    await waitFor(() =>
      expect(vi.mocked(strategyApi.manualAction)).toHaveBeenCalledWith('s-cra', {
        action: 'toggle_add_position',
        enabled: false,
      })
    )
    await waitFor(() =>
      expect(useToastStore.getState().toasts.some((t) => t.message === '后端口径文案')).toBe(true)
    )
  })

  it('补仓已关闭时开关如实显示并提供开启入口', async () => {
    renderPanel({ ...inPosStatus, add_position_enabled: false })
    expect(screen.getByText('已关闭')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /开启补仓/ }))
    await waitFor(() =>
      expect(vi.mocked(strategyApi.manualAction)).toHaveBeenCalledWith('s-cra', {
        action: 'toggle_add_position',
        enabled: true,
      })
    )
  })

  it('一键补仓：金额输入 + 两步确认后提交 add_position', async () => {
    renderPanel()
    fireEvent.change(screen.getByLabelText('补仓金额'), { target: { value: '50' } })
    fireEvent.click(screen.getByRole('button', { name: /一键补仓/ }))
    // 第一步：确认区出现（尚未提交）
    expect(screen.getByText('确认一键补仓？')).toBeTruthy()
    expect(vi.mocked(strategyApi.manualAction)).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: /确认执行/ }))
    await waitFor(() =>
      expect(vi.mocked(strategyApi.manualAction)).toHaveBeenCalledWith('s-cra', {
        action: 'add_position',
        amount: 50,
      })
    )
  })

  it('清仓卖出：danger 两步确认带持仓摘要后提交 close_all', async () => {
    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: /清仓卖出/ }))
    expect(screen.getByText('确认清仓卖出？')).toBeTruthy()
    // 持仓摘要：方向/数量/均价/浮动盈亏
    expect(screen.getByText(/持仓摘要/)).toBeTruthy()
    expect(screen.getByText(/1\.5/)).toBeTruthy()
    expect(screen.getByText(/\+\$3\.00/)).toBeTruthy() // (102-100)×1.5
    expect(vi.mocked(strategyApi.manualAction)).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: /确认执行/ }))
    await waitFor(() =>
      expect(vi.mocked(strategyApi.manualAction)).toHaveBeenCalledWith('s-cra', { action: 'close_all' })
    )
  })

  it('自定义减仓：比例模式换算提交 ratio；数量模式提交 qty', async () => {
    renderPanel()
    // 比例模式（默认）：输入 50 = 减半 → ratio 0.5
    fireEvent.change(screen.getByLabelText('减仓数值'), { target: { value: '50' } })
    fireEvent.click(screen.getByRole('button', { name: /减仓/ }))
    expect(screen.getByText('确认自定义减仓？')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /确认执行/ }))
    await waitFor(() =>
      expect(vi.mocked(strategyApi.manualAction)).toHaveBeenCalledWith('s-cra', {
        action: 'reduce_position',
        qty: undefined,
        ratio: 0.5,
      })
    )

    // 数量模式：切换后输入 0.5 → qty 0.5
    vi.mocked(strategyApi.manualAction).mockClear()
    fireEvent.change(screen.getByLabelText('减仓方式'), { target: { value: 'qty' } })
    fireEvent.change(screen.getByLabelText('减仓数值'), { target: { value: '0.5' } })
    fireEvent.click(screen.getByRole('button', { name: /减仓/ }))
    fireEvent.click(screen.getByRole('button', { name: /确认执行/ }))
    await waitFor(() =>
      expect(vi.mocked(strategyApi.manualAction)).toHaveBeenCalledWith('s-cra', {
        action: 'reduce_position',
        qty: 0.5,
        ratio: undefined,
      })
    )
  })

  it('空仓时 补仓/减仓/清仓 全部禁用', () => {
    renderPanel({ ...inPosStatus, in_position: false })
    expect(screen.getByRole('button', { name: /一键补仓/ }).hasAttribute('disabled')).toBe(true)
    expect(screen.getByRole('button', { name: /减仓/ }).hasAttribute('disabled')).toBe(true)
    expect(screen.getByRole('button', { name: /清仓卖出/ }).hasAttribute('disabled')).toBe(true)
    // 关闭补仓开关不依赖持仓，仍可用
    expect(screen.getByRole('button', { name: /关闭补仓/ }).hasAttribute('disabled')).toBe(false)
  })

  it('entry_paused 时如实显示"已暂停新开仓"徽标', () => {
    renderPanel({ ...inPosStatus, entry_paused: true })
    expect(screen.getByText('已暂停新开仓（重启策略恢复）')).toBeTruthy()
  })

  it('减仓比例 ≥100% 不允许提交（全平请用清仓）', () => {
    renderPanel()
    fireEvent.change(screen.getByLabelText('减仓数值'), { target: { value: '100' } })
    expect(screen.getByRole('button', { name: /减仓/ }).hasAttribute('disabled')).toBe(true)
  })
})
