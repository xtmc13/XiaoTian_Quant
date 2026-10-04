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

  it('renders open double checkbox', () => {
    render(<CRAParamForm {...baseProps} />, { wrapper })
    expect(screen.getByLabelText('开仓加倍')).toBeTruthy()
  })

  it('toggles open double checkbox', () => {
    const onChange = vi.fn()
    render(<CRAParamForm {...baseProps} onChange={onChange} />, { wrapper })
    const checkbox = screen.getByLabelText('开仓加倍') as HTMLInputElement
    fireEvent.click(checkbox)
    expect(onChange).toHaveBeenCalled()
    const hasOpenDouble = onChange.mock.calls.some((call) => (call[0] as CRAParams).openDouble === true)
    expect(hasOpenDouble).toBe(true)
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
})
