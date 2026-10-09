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

  // ── H3：口径说明同步——多/空分别计（币富 #32 完整语义）──
  it('H3: 在线单量限制口径说明注明多单/空单分别计数', () => {
    render(<CRAParamForm {...baseProps} market="contract" />, { wrapper })
    const desc = screen.getByText('在线单量限制').parentElement!.querySelector('.text-\\[10px\\]')
    expect(desc?.textContent).toContain('多/空分别计')
    expect(desc?.textContent).toContain('dual 两侧各占一席')
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

  // ── F 片：燃烧斩仓口径说明（合约区，实现语义与币富差异如实标注）──

  it('shows burn mechanism captions on contract (real semantics, not 币富 original)', () => {
    render(<CRAParamForm {...baseProps} market="contract" />, { wrapper })
    // 对向燃烧：斩首单档 + 单侧模型无法并行对向仓的差异标注。
    expect(screen.getByText(/市价斩掉首单档/)).toBeTruthy()
    expect(screen.getByText(/无法并行持有对向仓/)).toBeTruthy()
    // 全局燃烧：斩设定比例（默认 50%）+ 无跨实例盈利数据源的保守版标注。
    expect(screen.getByText(/斩掉当前持仓的设定比例/)).toBeTruthy()
    expect(screen.getByText(/拿不到其它实例的盈利数据/)).toBeTruthy()
    // 拒单重试不静默假成功的备注。
    expect(screen.getByText(/自动重试并如实记日志/)).toBeTruthy()
  })

  it('hides burn section on spot (币富该功能在合约页)', () => {
    render(<CRAParamForm {...baseProps} />, { wrapper })
    expect(screen.queryByLabelText('对向燃烧')).toBeNull()
    expect(screen.queryByLabelText('全局燃烧')).toBeNull()
    expect(screen.queryByText(/市价斩掉首单档/)).toBeNull()
  })

  it('round-trips burn params in the api payload', () => {
    const payload = craParamsToApiPayload({
      ...DEFAULT_CRA_PARAMS,
      burnDualEnabled: true,
      burnDualThreshold: 2,
      burnGlobalEnabled: true,
      burnGlobalThreshold: 4,
    })
    expect(payload.burn_dual_enabled).toBe(true)
    expect(payload.burn_dual_threshold).toBe(2)
    expect(payload.burn_global_enabled).toBe(true)
    expect(payload.burn_global_threshold).toBe(4)
    const restored = apiPayloadToCraParams({
      burn_dual_enabled: true,
      burn_dual_threshold: 2,
      burn_global_enabled: true,
      burn_global_threshold: 4,
    })
    expect(restored.burnDualEnabled).toBe(true)
    expect(restored.burnDualThreshold).toBe(2)
    expect(restored.burnGlobalEnabled).toBe(true)
    expect(restored.burnGlobalThreshold).toBe(4)
    // 缺省回填默认 false/3/5（与后端 ParseCRAParams 一致）。
    const def = apiPayloadToCraParams({})
    expect(def.burnDualEnabled).toBe(false)
    expect(def.burnDualThreshold).toBe(3)
    expect(def.burnGlobalEnabled).toBe(false)
    expect(def.burnGlobalThreshold).toBe(5)
  })

  // ── G2-1：反向止盈判定周期扩档（币富反向止盈适合大周期：4h 金叉开多 1h 死叉卖）──

  it('reverse take profit period select offers 30m/1h/4h/8h (contract)', () => {
    render(<CRAParamForm {...baseProps} market="contract" />, { wrapper })
    const labelText = screen.getByText('反向止盈')
    const select = labelText.parentElement!.querySelector('select') as HTMLSelectElement
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

  it('round-trips reverse take profit period 4h in the api payload', () => {
    const payload = craParamsToApiPayload({ ...DEFAULT_CRA_PARAMS, reverseTP: '4h' })
    expect(payload.reverse_take_profit_period).toBe('4h')
    const restored = apiPayloadToCraParams({ reverse_take_profit_period: '4h' })
    expect(restored.reverseTP).toBe('4h')
    // 缺省回填默认 close（与后端 ParseCRAParams 一致）。
    expect(apiPayloadToCraParams({}).reverseTP).toBe('close')
  })

  // ── G2-2：全局燃烧斩仓比例输入框（跟随全局燃烧开关显示）──

  it('shows burn global close ratio input only when global burn enabled (contract)', () => {
    const { unmount } = render(<CRAParamForm {...baseProps} market="contract" />, { wrapper })
    expect(screen.queryByText('斩仓比例 (%)')).toBeNull()
    unmount()

    render(
      <CRAParamForm {...baseProps} market="contract" value={{ ...DEFAULT_CRA_PARAMS, burnGlobalEnabled: true }} />,
      { wrapper }
    )
    const input = screen.getByText('斩仓比例 (%)').parentElement!.querySelector('input') as HTMLInputElement
    expect(input).toBeTruthy()
    expect(input.value).toBe('50')
  })

  it('updates burnGlobalCloseRatio via the input, clamped to 10-90', () => {
    const onChange = vi.fn()
    render(
      <CRAParamForm
        {...baseProps}
        market="contract"
        value={{ ...DEFAULT_CRA_PARAMS, burnGlobalEnabled: true }}
        onChange={onChange}
      />,
      { wrapper }
    )
    const input = screen.getByText('斩仓比例 (%)').parentElement!.querySelector('input') as HTMLInputElement
    fireEvent.change(input, { target: { value: '30' } })
    let lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0] as CRAParams
    expect(lastCall.burnGlobalCloseRatio).toBe(30)
    // 越界钳制到 10-90（与后端 0.1-0.9 校验口径一致）。
    fireEvent.change(input, { target: { value: '95' } })
    lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0] as CRAParams
    expect(lastCall.burnGlobalCloseRatio).toBe(90)
  })

  it('round-trips burnGlobalCloseRatio as burn_global_close_ratio in the api payload', () => {
    const payload = craParamsToApiPayload({ ...DEFAULT_CRA_PARAMS, burnGlobalCloseRatio: 30 })
    expect(payload.burn_global_close_ratio).toBe(0.3)
    const restored = apiPayloadToCraParams({ burn_global_close_ratio: 0.25 })
    expect(restored.burnGlobalCloseRatio).toBe(25)
    // 缺省回填默认 50%（与后端 ParseCRAParams 默认 0.5 一致）。
    expect(apiPayloadToCraParams({}).burnGlobalCloseRatio).toBe(50)
  })
})
