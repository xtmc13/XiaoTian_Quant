import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BotCreateModal } from '../BotCreateModal'
import { strategyApi } from '@/lib/api'
import * as useToastModule from '@/lib/useToast'
import type { BotItem } from '@/hooks/useBotData'

vi.mock('@/lib/useToast', async () => {
  const actual = await vi.importActual<typeof import('@/lib/useToast')>('@/lib/useToast')
  return { ...actual, toast: vi.fn() }
})

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    strategyApi: { ...(actual.strategyApi || {}), create: vi.fn(), update: vi.fn() },
    aiApi: { ...(actual.aiApi || {}), chat: vi.fn() },
  }
})

function wrapper({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}

function renderModal(props: Partial<React.ComponentProps<typeof BotCreateModal>> = {}) {
  return render(
    <BotCreateModal
      open
      botType="custom"
      aiPreset={null}
      editBot={null}
      onCancel={vi.fn()}
      onCreated={vi.fn()}
      onUpdated={vi.fn()}
      {...props}
    />,
    { wrapper }
  )
}

function walkToParams() {
  fireEvent.click(screen.getByRole('button', { name: '下一步' }))
  fireEvent.change(screen.getByLabelText('交易标的'), { target: { value: 'BTC/USDT' } })
  fireEvent.click(screen.getByRole('button', { name: '下一步' }))
}

async function walkToConfirmAndCreate() {
  fireEvent.click(screen.getByRole('button', { name: '下一步' }))
  fireEvent.click(screen.getByRole('button', { name: '创建机器人' }))
  await waitFor(() => expect(vi.mocked(strategyApi.create).mock.calls.length).toBeGreaterThan(0))
}

function createdPayload(): Record<string, unknown> {
  return vi.mocked(strategyApi.create).mock.calls[0][0] as Record<string, unknown>
}

function createdConfig(): Record<string, unknown> {
  return JSON.parse(createdPayload().config_json as string) as Record<string, unknown>
}

describe('BotCreateModal 向导提交（CRA 类型参数并入 config_json）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(strategyApi.create).mockResolvedValue({ id: 'b1' } as never)
    vi.mocked(strategyApi.update).mockResolvedValue({} as never)
  })

  it('cra_contract（AI 预设类型，默认 CRA 参数）：config_json 含 add_positions/order_count，过类型-参数防呆', async () => {
    renderModal({ aiPreset: { botType: 'cra_contract' as BotItem['bot_type'], description: 'test' } })
    walkToParams()
    fireEvent.change(screen.getByLabelText('K线周期'), { target: { value: '1h' } })
    await walkToConfirmAndCreate()

    const payload = createdPayload()
    expect(payload.strategy_type).toBe('cra_contract')
    expect(payload.name).toBe('未命名机器人')
    expect(payload.timeframe).toBe('1h')

    const config = createdConfig()
    expect(config.enable_add_position).toBe(true)
    expect(config.order_count).toBe(9)
    expect(config.first_order_amount).toBeGreaterThanOrEqual(1)
    expect(Array.isArray(config.add_positions)).toBe(true)
    const ladder = config.add_positions as unknown[]
    expect(ladder.length).toBe(9)
    // 与后端 checkStrategyTypeConfigMatch 同口径：首单金额≥1 且带补仓梯子（或显式关闭补仓）
    const guardPasses =
      (config.first_order_amount as number) >= 1 && (config.enable_add_position === false || ladder.length > 0)
    expect(guardPasses).toBe(true)

    const tradingConfig = payload.trading_config as { bot_params: Record<string, unknown>; timeframe: string }
    expect(tradingConfig.timeframe).toBe('1h')
    expect(Array.isArray(tradingConfig.bot_params.add_positions)).toBe(true)
  })

  it('wallstreet：CRAParamForm 改首单额度后提交，序列化值进 config_json', async () => {
    renderModal({ botType: 'wallstreet' })
    walkToParams()
    fireEvent.change(screen.getByDisplayValue('10'), { target: { value: '50' } })
    fireEvent.change(screen.getByLabelText('K线周期'), { target: { value: '5m' } })
    await walkToConfirmAndCreate()

    const payload = createdPayload()
    expect(payload.strategy_type).toBe('wallstreet')
    expect(payload.timeframe).toBe('5m')
    const config = createdConfig()
    expect(config.first_order_amount).toBe(50)
    expect((config.add_positions as unknown[]).length).toBe(7)
    expect(config.order_count).toBe(7)
  })

  it('CRA 类型工作周期推导优先：开仓指标周期 15m 时无需下拉选择', async () => {
    renderModal({
      aiPreset: {
        botType: 'wallstreet',
        description: 'AI',
        params: {
          first_order_amount: 20,
          enable_add_position: true,
          order_count: 1,
          add_positions: [{ order: 1, multiplier: 2, spread: 0.03, callback: 0.003, ema_enabled: false }],
          open_macd_enabled: true,
          open_macd_period: '15m',
        },
      },
    })
    walkToParams()
    expect((screen.getByLabelText('K线周期') as HTMLSelectElement).value).toBe('')
    await walkToConfirmAndCreate()

    const payload = createdPayload()
    expect(payload.strategy_type).toBe('wallstreet')
    expect(payload.timeframe).toBe('15m')
    const config = createdConfig()
    expect(config.first_order_amount).toBe(20)
    expect((config.add_positions as unknown[]).length).toBe(1)
  })

  it('非 CRA 类型（grid）：不带 config_json，周期用下拉选择', async () => {
    renderModal({ botType: 'grid' })
    walkToParams()
    expect((screen.getByLabelText('K线周期') as HTMLSelectElement).value).toBe('')
    fireEvent.change(screen.getByLabelText('K线周期'), { target: { value: '30m' } })
    await walkToConfirmAndCreate()

    const payload = createdPayload()
    expect(payload.strategy_type).toBe('grid_trading')
    expect('config_json' in payload).toBe(false)
    expect(payload.timeframe).toBe('30m')
    expect((payload.trading_config as { timeframe: string }).timeframe).toBe('30m')
  })

  it('周期推导与下拉均为空：如实拦截，不提交', async () => {
    renderModal({ botType: 'grid' })
    walkToParams()
    fireEvent.click(screen.getByRole('button', { name: '下一步' }))
    fireEvent.click(screen.getByRole('button', { name: '创建机器人' }))

    expect(vi.mocked(strategyApi.create)).not.toHaveBeenCalled()
    expect(
      vi.mocked(useToastModule.toast).mock.calls.some((c) => c[0] === 'error' && c[1] === '请选择工作周期')
    ).toBe(true)
  })
})
