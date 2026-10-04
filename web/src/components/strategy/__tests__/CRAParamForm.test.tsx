import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CRAParamForm, DEFAULT_CRA_PARAMS, craParamsToApiPayload, apiPayloadToCraParams } from '../CRAParamForm'
import type { CRAParams } from '../CRAParamForm'
import { PERIOD_OPTIONS } from '../indicatorPresets'

function wrapper({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}

describe('CRAParamForm', () => {
  const baseProps = {
    value: DEFAULT_CRA_PARAMS,
    onChange: vi.fn(),
    market: 'spot' as const,
  }

  it('renders default first order amount from DEFAULT_CRA_PARAMS', () => {
    render(<CRAParamForm {...baseProps} />, { wrapper })
    const inputs = screen.getAllByDisplayValue(String(DEFAULT_CRA_PARAMS.firstOrderAmount))
    expect(inputs.length).toBeGreaterThan(0)
  })

  it('shows static take profit inputs by default', () => {
    render(<CRAParamForm {...baseProps} />, { wrapper })
    expect(screen.getByText('止盈比例 (%)')).toBeTruthy()
    expect(screen.getByText('盈利回调 (%)')).toBeTruthy()
  })

  it('switches take profit mode to moving when clicked', () => {
    const onChange = vi.fn()
    render(<CRAParamForm {...baseProps} onChange={onChange} />, { wrapper })
    fireEvent.click(screen.getByText('移动止盈'))
    expect(onChange).toHaveBeenCalled()
    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0] as CRAParams
    expect(lastCall.tpMode).toBe('moving')
  })

  it('renders open double checkbox (contract only)', () => {
    render(<CRAParamForm {...baseProps} market="contract" />, { wrapper })
    expect(screen.getByLabelText('开仓加倍')).toBeTruthy()
  })

  it('hides open double checkbox on spot (币富该功能在合约页)', () => {
    render(<CRAParamForm {...baseProps} />, { wrapper })
    expect(screen.queryByLabelText('开仓加倍')).toBeNull()
  })

  it('toggles open double checkbox', () => {
    const onChange = vi.fn()
    render(<CRAParamForm {...baseProps} market="contract" onChange={onChange} />, { wrapper })
    const checkbox = screen.getByLabelText('开仓加倍') as HTMLInputElement
    fireEvent.click(checkbox)
    expect(onChange).toHaveBeenCalled()
    const hasOpenDouble = onChange.mock.calls.some((call) => (call[0] as CRAParams).openDouble === true)
    expect(hasOpenDouble).toBe(true)
  })

  // ── E 片：顺势而为口径说明（合约区，dual 生效语义如实标注）──

  it('shows follow trend caption only when enabled on contract', () => {
    const { unmount } = render(
      <CRAParamForm {...baseProps} market="contract" value={{ ...DEFAULT_CRA_PARAMS, followTrend: true }} />,
      { wrapper }
    )
    expect(screen.getByText(/仅双向（dual）模式生效/)).toBeTruthy()
    expect(screen.getByText(/min\(N\+1, 5\) 倍/)).toBeTruthy()
    unmount()

    // 未开启时不渲染说明。
    render(<CRAParamForm {...baseProps} market="contract" />, { wrapper })
    expect(screen.queryByText(/仅双向（dual）模式生效/)).toBeNull()
  })

  it('hides follow trend caption on spot (币富该功能在合约页)', () => {
    render(<CRAParamForm {...baseProps} value={{ ...DEFAULT_CRA_PARAMS, followTrend: true }} />, { wrapper })
    expect(screen.queryByLabelText('顺势而为')).toBeNull()
    expect(screen.queryByText(/仅双向（dual）模式生效/)).toBeNull()
  })

  it('round-trips followTrend as follow_trend in the api payload', () => {
    const payload = craParamsToApiPayload({ ...DEFAULT_CRA_PARAMS, followTrend: true })
    expect(payload.follow_trend).toBe(true)
    const restored = apiPayloadToCraParams({ follow_trend: true })
    expect(restored.followTrend).toBe(true)
    // 缺省回填默认 false（与后端 ParseCRAParams 一致）。
    expect(apiPayloadToCraParams({}).followTrend).toBe(false)
  })

  // ── D2：在线单量限制输入框（合约区，币富 #32 跨实例总量闸口径）──

  it('renders online order limit input with default 10 (contract only)', () => {
    const { unmount } = render(<CRAParamForm {...baseProps} market="contract" />, { wrapper })
    expect(screen.getByText('在线单量限制')).toBeTruthy()
    const input = screen.getByText('在线单量限制').parentElement!.querySelector('input') as HTMLInputElement
    expect(input).toBeTruthy()
    expect(input.value).toBe('10')
    unmount()

    // 现货不渲染（币富该功能在合约页）。
    render(<CRAParamForm {...baseProps} />, { wrapper })
    expect(screen.queryByText('在线单量限制')).toBeNull()
  })

  it('updates onlineOrderLimit via the input, clamped to min 1', () => {
    const onChange = vi.fn()
    render(<CRAParamForm {...baseProps} market="contract" onChange={onChange} />, { wrapper })
    const input = screen.getByText('在线单量限制').parentElement!.querySelector('input') as HTMLInputElement
    fireEvent.change(input, { target: { value: '3' } })
    let lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0] as CRAParams
    expect(lastCall.onlineOrderLimit).toBe(3)
    // 0/空输入钳制为 1（与后端 craOnlineOrderLimit 的最严口径一致）。
    fireEvent.change(input, { target: { value: '0' } })
    lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0] as CRAParams
    expect(lastCall.onlineOrderLimit).toBe(1)
  })

  it('round-trips onlineOrderLimit as online_order_limit in the api payload', () => {
    const payload = craParamsToApiPayload({ ...DEFAULT_CRA_PARAMS, onlineOrderLimit: 3 })
    expect(payload.online_order_limit).toBe(3)
    const restored = apiPayloadToCraParams({ online_order_limit: 5 })
    expect(restored.onlineOrderLimit).toBe(5)
    // 缺省回填默认 10（与后端 ParseCRAParams 一致）。
    expect(apiPayloadToCraParams({}).onlineOrderLimit).toBe(10)
  })

  // ── A1：首单挂单价格输入框 ──

  it('renders first order pending price input with default 0 (market price)', () => {
    render(<CRAParamForm {...baseProps} />, { wrapper })
    expect(screen.getByText('首单挂单价格 (USDT)')).toBeTruthy()
    const input = screen.getByDisplayValue('0') as HTMLInputElement
    expect(input).toBeTruthy()
  })

  it('updates firstOrderPrice when editing the pending price input', () => {
    const onChange = vi.fn()
    render(<CRAParamForm {...baseProps} onChange={onChange} />, { wrapper })
    const input = screen.getByDisplayValue('0') as HTMLInputElement
    fireEvent.change(input, { target: { value: '1234.5' } })
    expect(onChange).toHaveBeenCalled()
    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0] as CRAParams
    expect(lastCall.firstOrderPrice).toBe(1234.5)
  })

  it('round-trips firstOrderPrice as first_order_price in the api payload (USDT, no conversion)', () => {
    const payload = craParamsToApiPayload({ ...DEFAULT_CRA_PARAMS, firstOrderPrice: 1234.5 })
    expect(payload.first_order_price).toBe(1234.5)
    const restored = apiPayloadToCraParams({ first_order_price: 2345 })
    expect(restored.firstOrderPrice).toBe(2345)
    // 编辑回填默认路径：缺省 0 = 市价。
    const def = apiPayloadToCraParams({})
    expect(def.firstOrderPrice).toBe(0)
  })

  // ── A2：指标周期扩档（币富 5m/15m/30m/1h/4h/8h）──

  it('extends indicator period options with 30m/1h/4h/8h', () => {
    expect(PERIOD_OPTIONS.map((o) => o.value)).toEqual(['close', '5m', '15m', '30m', '1h', '4h', '8h'])
  })

  it('add-position MACD period select offers 30m/1h/4h/8h (contract)', () => {
    const value: CRAParams = { ...DEFAULT_CRA_PARAMS, addMacdEnabled: true }
    render(<CRAParamForm value={value} onChange={vi.fn()} market="contract" />, { wrapper })
    const labelText = screen.getByText('补仓 MACD 监测')
    const row = labelText.closest('label')!.parentElement!
    const select = row.querySelector('select') as HTMLSelectElement
    expect(select).toBeTruthy()
    expect(Array.from(select.options).map((o) => o.value)).toEqual([
      'close',
      '5m',
      '15m',
      '30m',
      '1h',
      '4h',
      '8h',
    ])
  })

  it('round-trips extended indicator periods in the api payload', () => {
    const payload = craParamsToApiPayload({
      ...DEFAULT_CRA_PARAMS,
      openMacdEnabled: true,
      openMacdPeriod: '30m',
      addEmaEnabled: true,
      addEmaPeriod: '8h',
    })
    expect(payload.open_macd_period).toBe('30m')
    expect(payload.add_ema_period).toBe('8h')
    const restored = apiPayloadToCraParams({ open_macd_enabled: true, open_macd_period: '4h' })
    expect(restored.openMacdPeriod).toBe('4h')
  })

  // ── B 片：止盈方式三态说明文案 + 移动止盈不生效提示（币富名词解释 #29）──

  it('shows per-method description matching engine semantics', () => {
    const { unmount } = render(
      <CRAParamForm {...baseProps} value={{ ...DEFAULT_CRA_PARAMS, tpMethod: 'full' }} />,
      { wrapper }
    )
    expect(screen.getByText(/卖出全部仓位/)).toBeTruthy()
    unmount()

    render(<CRAParamForm {...baseProps} value={{ ...DEFAULT_CRA_PARAMS, tpMethod: 'tail' }} />, { wrapper })
    expect(screen.getByText(/只卖出最后一档减仓/)).toBeTruthy()
    unmount()

    render(<CRAParamForm {...baseProps} value={{ ...DEFAULT_CRA_PARAMS, tpMethod: 'head_tail' }} />, { wrapper })
    expect(screen.getByText(/卖出首档\+尾档/)).toBeTruthy()
  })

  it('warns that tail/head_tail take profit is inert under moving mode', () => {
    // 移动止盈 + 尾单：提示不生效。
    const { unmount } = render(
      <CRAParamForm {...baseProps} value={{ ...DEFAULT_CRA_PARAMS, tpMethod: 'tail', tpMode: 'moving' }} />,
      { wrapper }
    )
    expect(screen.getByText(/尾单止盈不生效/)).toBeTruthy()
    unmount()

    // 移动止盈 + 首尾：提示不生效。
    render(
      <CRAParamForm {...baseProps} value={{ ...DEFAULT_CRA_PARAMS, tpMethod: 'head_tail', tpMode: 'moving' }} />,
      { wrapper }
    )
    expect(screen.getByText(/首尾止盈不生效/)).toBeTruthy()
  })

  it('does not show the inert hint for full method or static mode', () => {
    const { unmount } = render(
      <CRAParamForm {...baseProps} value={{ ...DEFAULT_CRA_PARAMS, tpMethod: 'full', tpMode: 'moving' }} />,
      { wrapper }
    )
    expect(screen.queryByText(/不生效/)).toBeNull()
    unmount()

    render(<CRAParamForm {...baseProps} value={{ ...DEFAULT_CRA_PARAMS, tpMethod: 'tail', tpMode: 'static' }} />, {
      wrapper,
    })
    expect(screen.queryByText(/不生效/)).toBeNull()
  })
})
