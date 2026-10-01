import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrategyCreateModal } from '../StrategyCreateModal'
import { configApi, strategyApi } from '@/lib/api'
import type { ExchangeConfiguredStatus } from '@/types'

const mockConfigured: Record<string, ExchangeConfiguredStatus> = {
  binance: { enabled: true, has_credentials: true, testnet: false, futures: true },
}

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    strategyApi: {
      ...(actual.strategyApi || {}),
      create: vi.fn(),
      update: vi.fn(),
      templates: vi.fn().mockResolvedValue([]),
    },
    configApi: { ...(actual.configApi || {}), exchangesConfigured: vi.fn() },
    backtestApi: { ...(actual.backtestApi || {}), run: vi.fn() },
  }
})

function wrapper({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}

describe('StrategyCreateModal 支撑回踩反弹（support_rebound）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(configApi.exchangesConfigured).mockResolvedValue(mockConfigured)
    vi.mocked(strategyApi.create).mockResolvedValue({ id: 'sr1' } as never)
    vi.mocked(strategyApi.update).mockResolvedValue({} as never)
  })

  it('选中 support_rebound → CRA参数区 + 策略指标卡弹窗编辑专属参数 → 提交 config_json 含参数', async () => {
    const create = vi.mocked(strategyApi.create)
    render(<StrategyCreateModal editing={null} defaultMarket="spot" inline onClose={vi.fn()} onSaved={vi.fn()} />, {
      wrapper,
    })

    // ── Step 0 基础配置：名称 + 类型 support_rebound + 交易所 ──
    fireEvent.change(screen.getByPlaceholderText('输入策略名称'), { target: { value: '支撑回踩机器人' } })
    fireEvent.change(screen.getByDisplayValue('请选择策略类型'), { target: { value: 'support_rebound' } })
    fireEvent.click(screen.getByText('点击选择交易所'))
    await waitFor(() => expect(screen.getByText('Binance')).toBeTruthy())
    fireEvent.click(screen.getByText('Binance'))
    fireEvent.click(screen.getByText('确认选择'))
    fireEvent.click(screen.getByText('下一步'))

    // ── Step 1 参数配置：CRA 量化参数区 + 开仓指标区的策略卡 ──
    // 2026-09-30 设计：support_rebound 属 CRA 兼容类型（仓位管理复用 CRA），
    // 14 个专属参数经开仓指标区的策略卡 → StrategyParamModal 弹窗编辑。
    await waitFor(() => expect(screen.getByText('CRA 量化参数')).toBeTruthy())
    await waitFor(() => expect(screen.getAllByText('支撑回踩反弹').length).toBeGreaterThan(0))

    // 打开策略参数弹窗（⚙ 按钮）。
    fireEvent.click(screen.getByLabelText('支撑回踩反弹 参数'))
    await waitFor(() => expect(screen.getByText('支撑回踩反弹 · 参数')).toBeTruthy())
    expect(screen.getByText('暴跌幅度')).toBeTruthy()
    expect(screen.getByText('支撑观察窗口（根）')).toBeTruthy()
    expect(screen.getByText('异常量倍数（暴跌）')).toBeTruthy()
    expect(screen.getByText('每单金额 (USDT)')).toBeTruthy()
    expect(screen.getByText('超时离场（根）')).toBeTruthy()

    // 修改暴跌幅度 0.12 → 0.20（弹窗内标签父级 label 的 number input）。
    const dropLabel = screen.getByText('暴跌幅度').closest('label')
    expect(dropLabel).toBeTruthy()
    fireEvent.change(dropLabel!.querySelector('input')!, { target: { value: '0.2' } })
    fireEvent.click(screen.getByRole('button', { name: '确认' }))
    await waitFor(() => expect(screen.queryByText('支撑回踩反弹 · 参数')).toBeNull())

    // ── 走到最后一步提交 ──
    fireEvent.click(screen.getByText('下一步')) // step 2 回测预览
    fireEvent.click(screen.getByText('下一步')) // step 3 执行设置
    fireEvent.click(screen.getByRole('button', { name: '创建策略' }))

    await waitFor(() => expect(create).toHaveBeenCalled())
    const payload = create.mock.calls[0][0] as Record<string, unknown>
    expect(payload.strategy_type).toBe('support_rebound')
    expect(payload.category).toBe('spot')
    expect(payload.market_type).toBe('spot')
    expect(payload.symbol).toBe('BTCUSDT')
    // 顶层 timeframe（K 线供给管按它订阅 4h K 线）
    expect(payload.timeframe).toBe('4h')

    const config = JSON.parse(payload.config_json as string) as Record<string, unknown>
    expect(config.crash_drop_pct).toBe(0.2)
    expect(config.lookback_bars).toBe(120)
    expect(config.support_touch_tolerance_pct).toBe(0.015)
    expect(config.min_support_touches).toBe(2)
    expect(config.crash_lookback_bars).toBe(6)
    expect(config.volume_sma_period).toBe(20)
    expect(config.volume_spike_mult).toBe(2)
    expect(config.rebound_pct).toBe(0.05)
    expect(config.pullback_bars).toBe(12)
    expect(config.entry_volume_mult).toBe(1.5)
    expect(config.stop_buffer_pct).toBe(0.02)
    expect(config.take_profit_pct).toBe(0.15)
    expect(config.position_size).toBe(500)
    expect(config.max_hold_bars).toBe(90)
    // 2026-09-30 起 support_rebound 为 CRA 兼容类型：CRA 仓位管理键随配置写入。
    expect(config.first_order_amount).toBeDefined()
    expect(config.add_positions).toBeDefined()
  })
})
