import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { I18nProvider } from '@/i18n'
import { BotsCombo } from '../bots/BotsCombo'
import { combosApi, strategyApi } from '@/lib/api'
import type { ComboConfig, ComboSignal } from '@/types'
import * as useToastModule from '@/lib/useToast'
// 测试环境不经 main.tsx，需显式注册词条（默认语言 zh-CN）
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/nav'
import '@/i18n/locales/combo'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    combosApi: {
      list: vi.fn(),
      get: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
      start: vi.fn(),
      stop: vi.fn(),
      signals: vi.fn(),
    },
    strategyApi: {
      ...actual.strategyApi,
      list: vi.fn(),
    },
  }
})

vi.mock('@/lib/useToast', async () => {
  const actual = await vi.importActual<typeof import('@/lib/useToast')>('@/lib/useToast')
  return { ...actual, toast: vi.fn() }
})

const COMBOS: ComboConfig[] = [
  {
    id: 'c1',
    user_id: 1,
    name: '趋势组合',
    symbol: 'BTCUSDT',
    members: [
      { strategy_name: 'ema_cross', weight: 0.5, enabled: true },
      { strategy_name: 'macd', weight: 0.5, enabled: true },
    ],
    aggregation_mode: 'vote',
    status: 'running',
    created_at: 1759600000000,
    updated_at: 1759600000000,
  },
  {
    id: 'c2',
    user_id: 1,
    name: '保守组合',
    symbol: 'ETHUSDT',
    members: [{ strategy_name: 'rsi', weight: 1, enabled: true }],
    aggregation_mode: 'weighted',
    status: 'stopped',
    created_at: 1759500000000,
    updated_at: 1759500000000,
  },
]

const SIGNALS: ComboSignal[] = [
  {
    symbol: 'BTCUSDT',
    direction: 'LONG',
    strength: 0.75,
    strategy: '趋势组合',
    reason: 'vote majority LONG (ema_cross:0.80, macd:0.70)',
    timestamp: 1759600000000,
  },
  {
    symbol: 'BTCUSDT',
    direction: 'SHORT',
    strength: 0.6,
    strategy: '趋势组合',
    reason: 'vote majority SHORT (rsi:0.60)',
    timestamp: 1759590000000,
  },
]

const STRATEGY_CONFIGS = [
  { id: 's1', name: '我的EMA', status: 'stopped' as const, strategy_type: 'ema_cross' },
  { id: 's2', name: 'MACD 策略', status: 'running' as const, strategy_type: 'macd' },
  { id: 's3', name: 'RSI 策略', status: 'stopped' as const, strategy_type: 'rsi' },
]

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <I18nProvider>{children}</I18nProvider>
    </QueryClientProvider>
  )
}

const toastMock = () => useToastModule.toast as unknown as ReturnType<typeof vi.fn>

function toastCalledWith(type: string, includes: string) {
  return toastMock().mock.calls.some((c) => c[0] === type && String(c[1]).includes(includes))
}

describe('BotsCombo 组合策略', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(combosApi.list).mockResolvedValue(COMBOS)
    vi.mocked(combosApi.create).mockResolvedValue({ status: 'ok', id: 'new-id' })
    vi.mocked(combosApi.update).mockResolvedValue({ status: 'ok' })
    vi.mocked(combosApi.delete).mockResolvedValue({ status: 'ok' })
    vi.mocked(combosApi.start).mockResolvedValue({ status: 'ok' })
    vi.mocked(combosApi.stop).mockResolvedValue({ status: 'ok' })
    vi.mocked(combosApi.signals).mockResolvedValue(SIGNALS)
    vi.mocked(strategyApi.list).mockResolvedValue(STRATEGY_CONFIGS)
  })

  it('列表渲染：名称/交易对/成员数/状态/聚合模式徽标', async () => {
    render(<BotsCombo />, { wrapper })
    await waitFor(() => expect(screen.getByText('趋势组合')).toBeTruthy())
    expect(screen.getByText('保守组合')).toBeTruthy()
    expect(screen.getByText('BTCUSDT · 2 个成员')).toBeTruthy()
    expect(screen.getByText('ETHUSDT · 1 个成员')).toBeTruthy()
    expect(screen.getByText(/运行中/)).toBeTruthy()
    expect(screen.getByText(/已停止/)).toBeTruthy()
    expect(screen.getByText('多数投票')).toBeTruthy()
    expect(screen.getByText('加权强度')).toBeTruthy()
  })

  it('创建校验：名称为空时 warning 拦截且不调用 create', async () => {
    render(<BotsCombo />, { wrapper })
    await waitFor(() => expect(screen.getByText('趋势组合')).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: /新建组合/ }))
    const dialog = await screen.findByRole('dialog')
    // 直接提交（未填名称）
    fireEvent.click(within(dialog).getAllByRole('button', { name: '新建组合' })[0])
    await waitFor(() => expect(toastCalledWith('warning', '请填写组合名称')).toBe(true))
    expect(combosApi.create).not.toHaveBeenCalled()
  })

  it('创建校验：无启用成员时 warning 拦截', async () => {
    render(<BotsCombo />, { wrapper })
    await waitFor(() => expect(screen.getByText('趋势组合')).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: /新建组合/ }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByPlaceholderText('例如：趋势+网格组合'), { target: { value: '新组合' } })
    fireEvent.click(within(dialog).getAllByRole('button', { name: '新建组合' })[0])
    await waitFor(() => expect(toastCalledWith('warning', '请至少添加一个启用的成员策略')).toBe(true))
    expect(combosApi.create).not.toHaveBeenCalled()
  })

  it('创建流：vote 模式两个成员等权提交（payload 对齐后端契约）', async () => {
    render(<BotsCombo />, { wrapper })
    await waitFor(() => expect(screen.getByText('趋势组合')).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: /新建组合/ }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByPlaceholderText('例如：趋势+网格组合'), { target: { value: '新组合' } })
    // 添加两个成员（策略类型选项来自策略配置接口 strategy_type 去重）
    const addBtn = await within(dialog).findByText('添加成员')
    fireEvent.click(addBtn)
    fireEvent.click(addBtn)
    const memberSelects = await within(dialog).findAllByLabelText('成员策略')
    expect(memberSelects).toHaveLength(2)
    fireEvent.change(memberSelects[1], { target: { value: 'macd' } })
    fireEvent.click(within(dialog).getAllByRole('button', { name: '新建组合' })[0])
    await waitFor(() =>
      expect(combosApi.create).toHaveBeenCalledWith({
        name: '新组合',
        symbol: 'BTCUSDT',
        aggregation_mode: 'vote',
        members: [
          { strategy_name: 'ema_cross', weight: 0.5, enabled: true },
          { strategy_name: 'macd', weight: 0.5, enabled: true },
        ],
      })
    )
    await waitFor(() => expect(toastCalledWith('success', '组合已创建')).toBe(true))
  })

  it('创建校验：weighted 模式权重和不为 1 时拦截并提示当前和', async () => {
    render(<BotsCombo />, { wrapper })
    await waitFor(() => expect(screen.getByText('趋势组合')).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: /新建组合/ }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByPlaceholderText('例如：趋势+网格组合'), { target: { value: '加权组合' } })
    fireEvent.change(within(dialog).getByLabelText('聚合模式'), { target: { value: 'weighted' } })
    const addBtn = await within(dialog).findByText('添加成员')
    fireEvent.click(addBtn)
    const weightInput = await within(dialog).findByLabelText('权重')
    fireEvent.change(weightInput, { target: { value: '0.3' } })
    fireEvent.click(within(dialog).getAllByRole('button', { name: '新建组合' })[0])
    await waitFor(() => expect(toastCalledWith('warning', '权重之和须为 1')).toBe(true))
    expect(toastCalledWith('warning', '0.300')).toBe(true)
    expect(combosApi.create).not.toHaveBeenCalled()
  })

  it('编辑流：打开预填并保存调用 update', async () => {
    render(<BotsCombo />, { wrapper })
    await waitFor(() => expect(screen.getByText('保守组合')).toBeTruthy())
    fireEvent.click(screen.getAllByText('编辑')[1])
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('编辑组合')).toBeTruthy()
    expect((within(dialog).getByPlaceholderText('例如：趋势+网格组合') as HTMLInputElement).value).toBe('保守组合')
    fireEvent.click(within(dialog).getByRole('button', { name: '保存' }))
    await waitFor(() =>
      expect(combosApi.update).toHaveBeenCalledWith('c2', {
        name: '保守组合',
        symbol: 'ETHUSDT',
        aggregation_mode: 'weighted',
        members: [{ strategy_name: 'rsi', weight: 1, enabled: true }],
      })
    )
  })

  it('启停流：停止中的组合点启动 → start 调用 + 成功 toast', async () => {
    render(<BotsCombo />, { wrapper })
    await waitFor(() => expect(screen.getByText('保守组合')).toBeTruthy())
    fireEvent.click(screen.getByText('启动'))
    await waitFor(() => expect(combosApi.start).toHaveBeenCalledWith('c2'))
    await waitFor(() => expect(toastCalledWith('success', '组合已启动')).toBe(true))
  })

  it('启停流：启动失败透出后端错误', async () => {
    vi.mocked(combosApi.start).mockRejectedValue(new Error('unknown strategy: foo'))
    render(<BotsCombo />, { wrapper })
    await waitFor(() => expect(screen.getByText('保守组合')).toBeTruthy())
    fireEvent.click(screen.getByText('启动'))
    await waitFor(() => expect(toastCalledWith('error', '启动失败: unknown strategy: foo')).toBe(true))
  })

  it('启停流：运行中的组合点停止 → stop 调用', async () => {
    render(<BotsCombo />, { wrapper })
    await waitFor(() => expect(screen.getByText('趋势组合')).toBeTruthy())
    fireEvent.click(screen.getByText('停止'))
    await waitFor(() => expect(combosApi.stop).toHaveBeenCalledWith('c1'))
    await waitFor(() => expect(toastCalledWith('success', '组合已停止')).toBe(true))
  })

  it('删除：二次确认后调用 delete', async () => {
    render(<BotsCombo />, { wrapper })
    await waitFor(() => expect(screen.getByText('趋势组合')).toBeTruthy())
    fireEvent.click(screen.getAllByText('删除')[0])
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('删除组合？')).toBeTruthy()
    expect(within(dialog).getByText(/趋势组合/)).toBeTruthy()
    fireEvent.click(within(dialog).getByText('确认删除'))
    await waitFor(() => expect(combosApi.delete).toHaveBeenCalledWith('c1'))
    await waitFor(() => expect(toastCalledWith('success', '组合已删除')).toBe(true))
  })

  it('删除：取消确认对话框则不调用 delete', async () => {
    render(<BotsCombo />, { wrapper })
    await waitFor(() => expect(screen.getByText('趋势组合')).toBeTruthy())
    fireEvent.click(screen.getAllByText('删除')[0])
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByText('取消'))
    await new Promise((r) => setTimeout(r, 50))
    expect(combosApi.delete).not.toHaveBeenCalled()
  })

  it('信号列表渲染：时间/方向/强度/成员/依据', async () => {
    render(<BotsCombo />, { wrapper })
    await waitFor(() => expect(screen.getByText('趋势组合')).toBeTruthy())
    fireEvent.click(screen.getAllByText('信号')[0])
    const dialog = await screen.findByRole('dialog')
    await waitFor(() =>
      expect(within(dialog).getByText('vote majority LONG (ema_cross:0.80, macd:0.70)')).toBeTruthy()
    )
    expect(within(dialog).getByText('vote majority SHORT (rsi:0.60)')).toBeTruthy()
    expect(within(dialog).getByText('LONG')).toBeTruthy()
    expect(within(dialog).getByText('SHORT')).toBeTruthy()
    expect(within(dialog).getByText('0.75')).toBeTruthy()
    expect(within(dialog).getByText('0.60')).toBeTruthy()
    expect(within(dialog).getByText(new Date(1759600000000).toLocaleString())).toBeTruthy()
    expect(combosApi.signals).toHaveBeenCalledWith('c1', 50)
  })

  it('信号空态 + 未运行提示如实呈现', async () => {
    vi.mocked(combosApi.signals).mockResolvedValue([])
    render(<BotsCombo />, { wrapper })
    await waitFor(() => expect(screen.getByText('保守组合')).toBeTruthy())
    // 第二张卡片（stopped）
    fireEvent.click(screen.getAllByText('信号')[1])
    const dialog = await screen.findByRole('dialog')
    await waitFor(() =>
      expect(within(dialog).getByText('暂无信号：组合运行中产生聚合信号后会显示在这里')).toBeTruthy()
    )
    expect(within(dialog).getByText('组合未运行，仅展示本次运行期间累计的信号')).toBeTruthy()
  })
})
