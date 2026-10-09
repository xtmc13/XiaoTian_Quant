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

  // ── 在线单量限制已迁往风控中心风控参数（2026-10-10）：策略表单不再出现，
  // payload 不再携带该键；存量 config_json 里的旧键由后端忽略。 ──

  it('no longer renders the online order limit input (moved to 风控中心风控参数)', () => {
    render(<CRAParamForm {...baseProps} market="contract" />, { wrapper })
    expect(screen.queryByText('在线单量限制')).toBeNull()
  })

  it('api payload no longer carries online_order_limit', () => {
    const payload = craParamsToApiPayload(DEFAULT_CRA_PARAMS)
    expect('online_order_limit' in payload).toBe(false)
    // 存量配置的旧键回填时被忽略（不抛错、不产生策略级字段）。
    const restored = apiPayloadToCraParams({
      online_order_limit: 3,
    } as unknown as Parameters<typeof apiPayloadToCraParams>[0])
    expect('onlineOrderLimit' in restored).toBe(false)
  })

  // ── 补仓 EMA 监测行（币富"补仓MACD监测+补仓EMA监测"两行补齐）──

  it('renders add-position EMA monitor row under MACD (contract only)', () => {
    const { unmount } = render(<CRAParamForm {...baseProps} market="contract" />, { wrapper })
    expect(screen.getByLabelText('补仓 MACD 监测')).toBeTruthy()
    expect(screen.getByLabelText('补仓 EMA 监测')).toBeTruthy()
    unmount()

    // 现货不渲染补仓指标区（币富该功能在合约页）。
    render(<CRAParamForm {...baseProps} />, { wrapper })
    expect(screen.queryByLabelText('补仓 EMA 监测')).toBeNull()
  })

  it('add-position EMA monitor defaults to off / close（与引擎 ParseCRAParams 默认一致）', () => {
    expect(DEFAULT_CRA_PARAMS.addEmaEnabled).toBe(false)
    expect(DEFAULT_CRA_PARAMS.addEmaPeriod).toBe('close')
    // 默认关闭时周期下拉不渲染（与 MACD 行同一 PeriodSelect 行为）。
    render(<CRAParamForm {...baseProps} market="contract" />, { wrapper })
    const labelText = screen.getByText('补仓 EMA 监测')
    const row = labelText.closest('label')!.parentElement!
    expect(row.querySelector('select')).toBeNull()
  })

  it('toggles add-position EMA monitor and changes its period', () => {
    const onChange = vi.fn()
    const value: CRAParams = { ...DEFAULT_CRA_PARAMS, addEmaEnabled: true }
    render(<CRAParamForm {...baseProps} market="contract" value={value} onChange={onChange} />, { wrapper })

    // 周期下拉复用统一档位（含 close/30m/1h/4h/8h）。
    const labelText = screen.getByText('补仓 EMA 监测')
    const row = labelText.closest('label')!.parentElement!
    const select = row.querySelector('select') as HTMLSelectElement
    expect(select).toBeTruthy()
    expect(Array.from(select.options).map((o) => o.value)).toEqual(['close', '5m', '15m', '30m', '1h', '4h', '8h'])
    fireEvent.change(select, { target: { value: '1h' } })
    let lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0] as CRAParams
    expect(lastCall.addEmaPeriod).toBe('1h')

    // 开关切换。
    const checkbox = screen.getByLabelText('补仓 EMA 监测') as HTMLInputElement
    fireEvent.click(checkbox)
    lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0] as CRAParams
    expect(lastCall.addEmaEnabled).toBe(false)
  })

  it('round-trips add_ema_enabled/add_ema_period in the api payload（编辑回填可用）', () => {
    const payload = craParamsToApiPayload({ ...DEFAULT_CRA_PARAMS, addEmaEnabled: true, addEmaPeriod: '4h' })
    expect(payload.add_ema_enabled).toBe(true)
    expect(payload.add_ema_period).toBe('4h')
    const restored = apiPayloadToCraParams({ add_ema_enabled: true, add_ema_period: '30m' })
    expect(restored.addEmaEnabled).toBe(true)
    expect(restored.addEmaPeriod).toBe('30m')
    // 缺省回填默认关闭/close（与后端 ParseCRAParams 一致）。
    const def = apiPayloadToCraParams({})
    expect(def.addEmaEnabled).toBe(false)
    expect(def.addEmaPeriod).toBe('close')
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
