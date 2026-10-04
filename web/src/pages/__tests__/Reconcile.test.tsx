import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { I18nProvider } from '@/i18n'
import { Reconcile } from '../Reconcile'
import { reconcileApi, type ReconcileConfig } from '@/lib/api'
import { useAuthStore } from '@/stores/authStore'
import * as useToastModule from '@/lib/useToast'
// 测试环境不经 main.tsx，需显式注册词条（默认语言 zh-CN）
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/nav'
import '@/i18n/locales/reconcile'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    reconcileApi: {
      diffs: vi.fn(),
      resolveDiff: vi.fn(),
      deviations: vi.fn(),
      resolveDeviation: vi.fn(),
      status: vi.fn(),
      getConfig: vi.fn(),
      putConfig: vi.fn(),
      reportedPnl: vi.fn(),
      runReportedPnl: vi.fn(),
    },
  }
})

vi.mock('@/lib/useToast', async () => {
  const actual = await vi.importActual<typeof import('@/lib/useToast')>('@/lib/useToast')
  return { ...actual, toast: vi.fn() }
})

// 默认 admin（配置可写 + 手动触发可见）；单测内可 mockImplementation 切角色。
vi.mock('@/stores/authStore', () => ({
  useAuthStore: vi.fn((selector: (s: unknown) => unknown) => selector({ user: { role: 'admin' } })),
}))

const CONFIG: ReconcileConfig = {
  interval_sec: 60,
  slippage_pct: 0.5,
  stuck_timeout_sec: 900,
  auto_fix: false,
  min_drift: 1e-9,
  funding_lookback_h: 24,
  fill_recovery_max: 50,
  reported_pnl_window_h: 24,
  reported_pnl_pct: 0.5,
  enabled: true,
}

const DIFFS = [
  {
    id: 1, user_id: 1, exchange: 'binance', symbol: 'BTCUSDT', diff_type: 'position_quantity',
    local_qty: 0.5, exchange_qty: 0.3, local_entry_price: 67000, exchange_entry_price: 67100,
    asset: '', amount: 0, detail: '本地持仓数量与交易所不一致', status: 'open',
    resolution: '', resolved_by: '', created_at: 1759600000000, resolved_at: 0,
  },
  {
    id: 2, user_id: 1, exchange: 'bybit', symbol: 'ETHUSDT', diff_type: 'funding',
    local_qty: 0, exchange_qty: 0, local_entry_price: 0, exchange_entry_price: 0,
    asset: 'USDT', amount: -1.23, detail: '资金费支出 -1.23000000 USDT', status: 'open',
    resolution: '', resolved_by: '', created_at: 1759600000000, resolved_at: 0,
  },
  {
    id: 3, user_id: 1, exchange: 'binance', symbol: 'SOLUSDT', diff_type: 'position_missing_local',
    local_qty: 0, exchange_qty: 10, local_entry_price: 0, exchange_entry_price: 200,
    asset: '', amount: 0, detail: '交易所持仓本地未记录', status: 'resolved',
    resolution: 'accept_exchange', resolved_by: 'admin', created_at: 1759500000000, resolved_at: 1759500001000,
  },
]

const DEVIATIONS = [
  {
    id: 11, order_id: 'ord-123', user_id: 1, symbol: 'BTCUSDT', exchange: 'binance',
    kind: 'slippage', expected_price: 67000, avg_price: 67050, slippage_pct: 0.0746,
    detail: '', status: 'open', resolved_by: '', created_at: 1759600000000, resolved_at: 0,
  },
]

const PNL_CHECKS = [
  {
    id: 21, user_id: 0, credential_id: 'cred-1', exchange: 'binance', symbol: '',
    window_start: 1759500000000, window_end: 1759600000000,
    local_pnl: 120.5, reported_pnl: 118.2, diff: 2.3, diff_pct: 0.0191,
    status: 'mismatch', detail: '', checked_at: 1759600001000,
  },
]

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <I18nProvider>{children}</I18nProvider>
    </QueryClientProvider>
  )
}

function setRole(role: string) {
  ;(useAuthStore as unknown as ReturnType<typeof vi.fn>).mockImplementation(
    (selector: (s: unknown) => unknown) => selector({ user: { role } })
  )
}

describe('Reconcile 对账中心', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setRole('admin')
    vi.mocked(reconcileApi.status).mockResolvedValue({
      running: true,
      last_runs: {
        positions: { name: 'positions', ok: true, message: 'drift=0', timestamp: 1759600000000 },
        reported_pnl: { name: 'reported_pnl', ok: false, message: 'exchange error', timestamp: 1759600000000 },
      },
      open_diffs: 2,
      open_deviations: 1,
      config: CONFIG,
      audit: [
        { id: 9, action: 'auto_fix_position', exchange: 'binance', symbol: 'BTCUSDT', user_id: 0, detail: 'qty=0.3', created_at: 1759600000000 },
      ],
    })
    vi.mocked(reconcileApi.diffs).mockResolvedValue(DIFFS)
    vi.mocked(reconcileApi.deviations).mockResolvedValue(DEVIATIONS)
    vi.mocked(reconcileApi.reportedPnl).mockResolvedValue(PNL_CHECKS)
    vi.mocked(reconcileApi.getConfig).mockResolvedValue(CONFIG)
    vi.mocked(reconcileApi.putConfig).mockResolvedValue(CONFIG)
    vi.mocked(reconcileApi.resolveDiff).mockResolvedValue(DIFFS[0])
    vi.mocked(reconcileApi.resolveDeviation).mockResolvedValue(DEVIATIONS[0])
    vi.mocked(reconcileApi.runReportedPnl).mockResolvedValue({ ok: true, message: 'checked=1 mismatch=0 error=0' })
  })

  it('状态总览渲染：运行标记/未解决数/任务运行/审计行', async () => {
    render(<Reconcile />, { wrapper })
    await waitFor(() => expect(screen.getByText('运行中')).toBeTruthy())
    expect(screen.getByText('未解决差异')).toBeTruthy()
    expect(screen.getByText('未确认偏差')).toBeTruthy()
    // 任务行：中文任务名 + 成功/失败徽标
    expect(screen.getByText('持仓对账')).toBeTruthy()
    expect(screen.getByText('回报 PnL')).toBeTruthy()
    expect(screen.getByText('exchange error')).toBeTruthy()
    // 审计行
    expect(screen.getByText('auto_fix_position')).toBeTruthy()
  })

  it('差异列表默认 open 过滤渲染，切换"已解决"带 status 参数重新请求', async () => {
    render(<Reconcile />, { wrapper })
    await waitFor(() =>
      expect(reconcileApi.diffs).toHaveBeenCalledWith(
        expect.objectContaining({ status: 'open', limit: 20, offset: 0 })
      )
    )
    // 本地 vs 交易所数值如实呈现
    await waitFor(() => expect(screen.getAllByText('BTCUSDT').length).toBeGreaterThan(0))
    expect(screen.getByText('0.5')).toBeTruthy()
    expect(screen.getByText('0.3')).toBeTruthy()
    // 资金费差异呈现金额+币种
    expect(screen.getByText('金额 -1.23 USDT')).toBeTruthy()

    const statusSelect = screen.getAllByLabelText('状态')[0]
    fireEvent.change(statusSelect, { target: { value: 'resolved' } })
    await waitFor(() =>
      expect(reconcileApi.diffs).toHaveBeenCalledWith(expect.objectContaining({ status: 'resolved', offset: 0 }))
    )
  })

  it('单条差异"以交易所为准"二次确认后调用 resolveDiff', async () => {
    render(<Reconcile />, { wrapper })
    await waitFor(() => expect(screen.getAllByText('以交易所为准').length).toBeGreaterThan(0))
    fireEvent.click(screen.getAllByText('以交易所为准')[0])
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('以交易所为准？')).toBeTruthy()
    fireEvent.click(within(dialog).getByText('确认'))
    await waitFor(() => expect(reconcileApi.resolveDiff).toHaveBeenCalledWith(1, 'accept_exchange'))
    await waitFor(() =>
      expect(
        (useToastModule.toast as unknown as ReturnType<typeof vi.fn>).mock.calls.some(
          (c) => c[0] === 'success' && String(c[1]).includes('差异已处理')
        )
      ).toBe(true)
    )
  })

  it('差异确认对话框取消则不调用 resolveDiff', async () => {
    render(<Reconcile />, { wrapper })
    await waitFor(() => expect(screen.getAllByText('忽略').length).toBeGreaterThan(0))
    fireEvent.click(screen.getAllByText('忽略')[0])
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByText('取消'))
    await new Promise((r) => setTimeout(r, 50))
    expect(reconcileApi.resolveDiff).not.toHaveBeenCalled()
  })

  it('批量忽略：勾选 → 二次确认 → 逐条 resolveDiff 并 toast 汇总', async () => {
    render(<Reconcile />, { wrapper })
    await waitFor(() => expect(screen.getAllByLabelText('选择该差异').length).toBe(2))
    for (const box of screen.getAllByLabelText('选择该差异')) fireEvent.click(box)
    await waitFor(() => expect(screen.getByText('已选 2 条')).toBeTruthy())
    fireEvent.click(screen.getByText('批量忽略'))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('批量忽略差异？')).toBeTruthy()
    fireEvent.click(within(dialog).getByText('确认'))
    await waitFor(() => expect(reconcileApi.resolveDiff).toHaveBeenCalledTimes(2))
    expect(reconcileApi.resolveDiff).toHaveBeenCalledWith(1, 'ignore')
    expect(reconcileApi.resolveDiff).toHaveBeenCalledWith(2, 'ignore')
    await waitFor(() =>
      expect(
        (useToastModule.toast as unknown as ReturnType<typeof vi.fn>).mock.calls.some(
          (c) => String(c[1]).includes('成功 2 条')
        )
      ).toBe(true)
    )
  })

  it('偏差单条确认流：确认对话框 → resolveDeviation', async () => {
    render(<Reconcile />, { wrapper })
    await waitFor(() => expect(screen.getByText('ord-123')).toBeTruthy())
    // 滑点/类型徽标如实呈现（下拉选项与徽标各一处）
    expect(screen.getAllByText('滑点超阈值').length).toBeGreaterThan(0)
    fireEvent.click(screen.getByText('确认'))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByText('确认'))
    await waitFor(() => expect(reconcileApi.resolveDeviation).toHaveBeenCalledWith(11))
  })

  it('回报 PnL 区块渲染（全账户/不一致徽标）+ admin 手动触发一轮', async () => {
    render(<Reconcile />, { wrapper })
    await waitFor(() => expect(screen.getByText('全账户')).toBeTruthy())
    expect(screen.getAllByText('不一致').length).toBeGreaterThan(0)
    expect(screen.getByText('+120.5')).toBeTruthy()
    expect(screen.getByText('+118.2')).toBeTruthy()

    fireEvent.click(screen.getByText('手动跑一轮'))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('手动触发回报 PnL 对账？')).toBeTruthy()
    fireEvent.click(within(dialog).getByText('确认'))
    await waitFor(() => expect(reconcileApi.runReportedPnl).toHaveBeenCalledWith(undefined))
    await waitFor(() =>
      expect(
        (useToastModule.toast as unknown as ReturnType<typeof vi.fn>).mock.calls.some(
          (c) => c[0] === 'success' && String(c[1]).includes('checked=1')
        )
      ).toBe(true)
    )
  })

  it('配置：admin 修改周期保存 → PUT 八键白名单 payload + 成功 toast', async () => {
    render(<Reconcile />, { wrapper })
    await waitFor(() => expect(screen.getByDisplayValue('60')).toBeTruthy())
    fireEvent.change(screen.getByDisplayValue('60'), { target: { value: '120' } })
    fireEvent.click(screen.getByText('保存配置'))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByText('确认'))
    await waitFor(() =>
      expect(reconcileApi.putConfig).toHaveBeenCalledWith({
        enabled: true,
        auto_fix: false,
        interval_sec: 120,
        slippage_pct: 0.5,
        stuck_timeout_sec: 900,
        min_drift: 1e-9,
        reported_pnl_window_h: 24,
        reported_pnl_pct: 0.5,
      })
    )
    await waitFor(() =>
      expect(
        (useToastModule.toast as unknown as ReturnType<typeof vi.fn>).mock.calls.some(
          (c) => c[0] === 'success' && String(c[1]).includes('对账配置已保存')
        )
      ).toBe(true)
    )
  })

  it('非 admin：配置只读（无保存按钮/无手动触发，提示只读）', async () => {
    setRole('user')
    render(<Reconcile />, { wrapper })
    await waitFor(() => expect(screen.getByText('配置修改仅管理员可用，当前为只读视图')).toBeTruthy())
    expect(screen.queryByText('保存配置')).toBeNull()
    expect(screen.queryByText('手动跑一轮')).toBeNull()
  })
})
