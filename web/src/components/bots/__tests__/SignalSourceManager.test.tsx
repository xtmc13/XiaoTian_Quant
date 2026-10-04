import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { SignalSourceManager } from '../SignalSourceManager'
import { SignalExecutorPanel } from '../SignalExecutorPanel'
import { executorApi } from '@/lib/api'
import { useAuthStore } from '@/stores/authStore'

vi.mock('@/lib/api', () => ({
  executorApi: {
    getStatus: vi.fn(),
    getActivePositions: vi.fn(),
    getExecutionRecords: vi.fn(),
    getSignalSources: vi.fn(),
    createSignalSource: vi.fn(),
    updateSignalSource: vi.fn(),
    deleteSignalSource: vi.fn(),
    subscribeSignalSource: vi.fn(),
    getSignalSourceSubscribers: vi.fn(),
    getStats: vi.fn(),
  },
}))

const MY_SOURCE = {
  id: 'src-mine',
  name: '我的 Webhook',
  type: 'webhook',
  enabled: true,
  owner_user_id: 2,
  signal_count_today: 1,
  signal_count_total: 5,
  fee_model: 'free',
}

const OTHER_SOURCE = {
  id: 'src-other',
  name: '别人的分成源',
  type: 'api',
  enabled: true,
  owner_user_id: 99,
  signal_count_today: 0,
  signal_count_total: 3,
  fee_model: 'profit_share',
  fee_percent: 20,
}

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      {children}
    </QueryClientProvider>
  )
}

function mockSources(sources: unknown[]) {
  vi.mocked(executorApi.getSignalSources).mockResolvedValue({ data: { sources } } as never)
}

describe('SignalSourceManager 信号源管理', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useAuthStore.setState({
      user: { id: 2, username: 'user', role: 'user' },
      token: 't',
      isAuthenticated: true,
    } as never)
    mockSources([MY_SOURCE, OTHER_SOURCE])
    vi.mocked(executorApi.createSignalSource).mockResolvedValue({ data: { success: true, source_id: 'new-1' } } as never)
    vi.mocked(executorApi.updateSignalSource).mockResolvedValue({ data: { success: true } } as never)
    vi.mocked(executorApi.deleteSignalSource).mockResolvedValue({
      data: { success: true, cancelled_subscriptions: 0 },
    } as never)
    vi.mocked(executorApi.subscribeSignalSource).mockResolvedValue({
      data: { success: true, subscription_id: 1, fee_model: 'profit_share', next_billing_at: 0 },
    } as never)
    vi.mocked(executorApi.getSignalSourceSubscribers).mockResolvedValue({
      data: {
        success: true,
        subscribers: [
          {
            id: 1,
            source_id: 'src-mine',
            user_id: 7,
            fee_model: 'free',
            fee_percent: 0,
            monthly_fee: 0,
            next_billing_at: 0,
            pending_share: 0,
            settled_share: 0,
            status: 'active',
            created_at: 1791000000000,
          },
        ],
      },
    } as never)
  })

  it('属主可见编辑/删除/订阅者，他人源只见订阅按钮', async () => {
    render(<SignalSourceManager open onClose={() => {}} />, { wrapper })
    await waitFor(() => expect(screen.getByText('我的 Webhook')).toBeTruthy())
    expect(screen.getByText('别人的分成源')).toBeTruthy()

    // 自己的源：编辑/删除/订阅者
    expect(screen.getByRole('button', { name: /编辑/ })).toBeTruthy()
    expect(screen.getByRole('button', { name: /删除/ })).toBeTruthy()
    expect(screen.getByRole('button', { name: /订阅者/ })).toBeTruthy()
    // 他人的源：仅一个订阅按钮，且无第二组编辑按钮
    const subscribeButtons = screen.getAllByRole('button', { name: /^订阅$/ })
    expect(subscribeButtons.length).toBe(1)
    expect(screen.getAllByRole('button', { name: /编辑/ }).length).toBe(1)
  })

  it('admin 对任何信号源都有编辑权', async () => {
    useAuthStore.setState({
      user: { id: 1, username: 'admin', role: 'admin' },
      token: 't',
      isAuthenticated: true,
    } as never)
    render(<SignalSourceManager open onClose={() => {}} />, { wrapper })
    await waitFor(() => expect(screen.getByText('别人的分成源')).toBeTruthy())
    // 两个源都可编辑，且不出现订阅按钮
    expect(screen.getAllByRole('button', { name: /编辑/ }).length).toBe(2)
    expect(screen.queryByRole('button', { name: /^订阅$/ })).toBeNull()
  })

  it('新建信号源：提交调用 createSignalSource 并携带定价/TP-SL 字段', async () => {
    render(<SignalSourceManager open onClose={() => {}} />, { wrapper })
    await waitFor(() => expect(screen.getByText('我的 Webhook')).toBeTruthy())

    fireEvent.click(screen.getByRole('button', { name: /新建信号源/ }))
    fireEvent.change(screen.getByPlaceholderText('例如：TradingView Webhook'), {
      target: { value: 'TV 信号' },
    })
    fireEvent.change(screen.getByLabelText(/定价模型/), { target: { value: 'profit_share' } })
    fireEvent.change(screen.getByPlaceholderText('例如：20'), { target: { value: '15' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() =>
      expect(executorApi.createSignalSource).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'TV 信号',
          type: 'webhook',
          enabled: true,
          fee_model: 'profit_share',
          fee_percent: 15,
        })
      )
    )
  })

  it('盈利分成未填比例时前端拦截，不调接口', async () => {
    render(<SignalSourceManager open onClose={() => {}} />, { wrapper })
    await waitFor(() => expect(screen.getByText('我的 Webhook')).toBeTruthy())

    fireEvent.click(screen.getByRole('button', { name: /新建信号源/ }))
    fireEvent.change(screen.getByPlaceholderText('例如：TradingView Webhook'), {
      target: { value: 'X' },
    })
    fireEvent.change(screen.getByLabelText(/定价模型/), { target: { value: 'profit_share' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => expect(screen.getByText(/分成比例 > 0/)).toBeTruthy())
    expect(executorApi.createSignalSource).not.toHaveBeenCalled()
  })

  it('编辑信号源：预填表单并调用 updateSignalSource', async () => {
    render(<SignalSourceManager open onClose={() => {}} />, { wrapper })
    await waitFor(() => expect(screen.getByText('我的 Webhook')).toBeTruthy())

    fireEvent.click(screen.getByRole('button', { name: /编辑/ }))
    const nameInput = screen.getByDisplayValue('我的 Webhook')
    fireEvent.change(nameInput, { target: { value: '改名后' } })
    fireEvent.click(screen.getByRole('button', { name: '保存修改' }))

    await waitFor(() =>
      expect(executorApi.updateSignalSource).toHaveBeenCalledWith(
        'src-mine',
        expect.objectContaining({ name: '改名后', fee_model: 'free' })
      )
    )
  })

  it('删除信号源：确认弹窗后调用 deleteSignalSource', async () => {
    render(<SignalSourceManager open onClose={() => {}} />, { wrapper })
    await waitFor(() => expect(screen.getByText('我的 Webhook')).toBeTruthy())

    fireEvent.click(screen.getByRole('button', { name: /删除/ }))
    await waitFor(() => expect(screen.getByText(/删除信号源「我的 Webhook」/)).toBeTruthy())
    // 确认弹窗里的删除按钮（列表行按钮在先，弹窗按钮在后）
    const deleteButtons = screen.getAllByRole('button', { name: '删除' })
    fireEvent.click(deleteButtons[deleteButtons.length - 1])

    await waitFor(() => expect(executorApi.deleteSignalSource).toHaveBeenCalledWith('src-mine'))
  })

  it('普通用户订阅他人信号源', async () => {
    render(<SignalSourceManager open onClose={() => {}} />, { wrapper })
    await waitFor(() => expect(screen.getByText('别人的分成源')).toBeTruthy())

    fireEvent.click(screen.getByRole('button', { name: /^订阅$/ }))
    await waitFor(() => expect(executorApi.subscribeSignalSource).toHaveBeenCalledWith('src-other'))
  })

  it('属主查看订阅者列表', async () => {
    render(<SignalSourceManager open onClose={() => {}} />, { wrapper })
    await waitFor(() => expect(screen.getByText('我的 Webhook')).toBeTruthy())

    fireEvent.click(screen.getByRole('button', { name: /订阅者/ }))
    await waitFor(() =>
      expect(executorApi.getSignalSourceSubscribers).toHaveBeenCalledWith('src-mine')
    )
    await waitFor(() => expect(screen.getByText('用户 #7')).toBeTruthy())
  })
})

describe('SignalExecutorPanel 信号源入口', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useAuthStore.setState({
      user: { id: 2, username: 'user', role: 'user' },
      token: 't',
      isAuthenticated: true,
    } as never)
    vi.mocked(executorApi.getStatus).mockResolvedValue({
      data: {
        active_positions: 0,
        pending_signals: 0,
        today_executed: 0,
        today_pnl: 0,
        tp1_executed: 0,
        tp2_executed: 0,
        tp3_executed: 0,
        sl_triggered: 0,
        status: 'running',
      },
    } as never)
    vi.mocked(executorApi.getActivePositions).mockResolvedValue({ data: { positions: [] } } as never)
    vi.mocked(executorApi.getExecutionRecords).mockResolvedValue({ data: { records: [] } } as never)
    mockSources([])
  })

  it('空态按钮打开信号源管理弹窗（不再是死路提示）', async () => {
    render(<SignalExecutorPanel />, { wrapper })
    await waitFor(() => expect(screen.getByText('暂无信号来源配置')).toBeTruthy())

    fireEvent.click(screen.getByRole('button', { name: '配置信号源' }))
    await waitFor(() => expect(screen.getByText('信号源管理')).toBeTruthy())
  })

  it('卡片头部提供「管理信号源」入口', async () => {
    render(<SignalExecutorPanel />, { wrapper })
    await waitFor(() => expect(screen.getByRole('button', { name: '管理信号源' })).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: '管理信号源' }))
    await waitFor(() => expect(screen.getByText('信号源管理')).toBeTruthy())
  })
})
