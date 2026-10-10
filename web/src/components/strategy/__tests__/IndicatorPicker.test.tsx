import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { IndicatorPicker } from '../IndicatorPicker'

function wrapper({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}
import {
  buildOpenIndicatorConfig,
  detectOpenIndicator,
  validateIndicatorValues,
  defaultIndicatorParams,
  OPEN_INDICATORS,
} from '../indicatorPresets'

describe('indicatorPresets（纯函数）', () => {
  it('buildOpenIndicatorConfig: macd 写引擎兼容键 + 新键', () => {
    const cfg = buildOpenIndicatorConfig('macd', { fast: 12, slow: 26, signal: 9, period: '5m' }, null)
    expect(cfg.open_macd_enabled).toBe(true)
    expect(cfg.open_macd_period).toBe('5m')
    expect(cfg.open_trend_ema_enabled).toBe(false)
    expect(cfg.open_indicator).toBe('macd')
    expect(cfg.indicator_params).toEqual({ macd: { fast: 12, slow: 26, signal: 9, period: '5m' } })
  })

  it('buildOpenIndicatorConfig: rsi 落 open_indicator+indicator_params（引擎按新键执行真实门槛）', () => {
    const cfg = buildOpenIndicatorConfig('rsi', { period: 14, oversold: 30, overbought: 70 }, null)
    expect(cfg.open_macd_enabled).toBe(false)
    expect(cfg.open_trend_ema_enabled).toBe(false)
    expect(cfg.open_indicator).toBe('rsi')
    expect(cfg.indicator_params?.rsi).toMatchObject({ period: 14 })
  })

  it('buildOpenIndicatorConfig: custom 存 code_id/name', () => {
    const cfg = buildOpenIndicatorConfig('custom', {}, { code_id: 42, name: '我的指标' })
    expect(cfg.open_indicator).toBe('custom')
    expect(cfg.indicator_params?.custom).toEqual({ code_id: 42, name: '我的指标' })
  })

  it('buildOpenIndicatorConfig: none 全关', () => {
    const cfg = buildOpenIndicatorConfig('none', {}, null)
    expect(cfg.open_macd_enabled).toBe(false)
    expect(cfg.open_indicator).toBeUndefined()
    expect(cfg.indicator_params).toBeUndefined()
  })

  it('detectOpenIndicator: 新键优先，旧键回退', () => {
    // 存量顺势多/空已下线：回退显示 EMA交叉，并带方向提示
    const legacy = detectOpenIndicator({ open_indicator: 'trend_long', indicator_params: { trend_long: { fast: 10 } } })
    expect(legacy.indicator).toBe('ema_cross')
    expect(legacy.directionHint).toBe('long')
    expect(legacy.params.fast).toBe(10)
    expect(detectOpenIndicator({ open_indicator: 'trend_short' }).directionHint).toBe('short')
    // 合约/现货 picker 均不再含顺势多/顺势空预设
    expect(OPEN_INDICATORS.some((d) => (d.key as string) === 'trend_long' || (d.key as string) === 'trend_short')).toBe(false)
    expect(detectOpenIndicator({ open_macd_enabled: true }).indicator).toBe('macd')
    expect(detectOpenIndicator({ open_trend_ema_enabled: true }).indicator).toBe('trend')
    expect(detectOpenIndicator({}).indicator).toBe('none')
  })

  it('L16: counter 语义闭环——旧键回退 ema_counter，再保存仍是逆势门槛', () => {
    // 旧配置只有 enabled 键：回退必须显示为逆势指标（修复前显示 ema_cross，
    // 再保存经选择器派生写成 open_trend_ema_enabled，逆势→顺势静默漂移）。
    const det = detectOpenIndicator({ open_counter_ema_enabled: true, open_counter_ema_period: '15m' })
    expect(det.indicator).toBe('ema_counter')
    // 再保存（选择器派生）：counter 键保持 true，trend 键不置位。
    const cfg = buildOpenIndicatorConfig(det.indicator, det.params, det.custom)
    expect(cfg.open_counter_ema_enabled).toBe(true)
    expect(cfg.open_trend_ema_enabled).toBe(false)
    expect(cfg.open_indicator).toBe('ema_counter')
    // 新键直读：open_indicator='ema_counter' 回显同指标与自定义参数。
    const det2 = detectOpenIndicator({ open_indicator: 'ema_counter', indicator_params: { ema_counter: { fast: 8 } } })
    expect(det2.indicator).toBe('ema_counter')
    expect(det2.params.fast).toBe(8)
    // 完整往返：build→detect 语义不变。
    const round = detectOpenIndicator(buildOpenIndicatorConfig('ema_counter', { fast: 9, slow: 30, period: '1h' }, null) as unknown as Record<string, unknown>)
    expect(round.indicator).toBe('ema_counter')
    expect(round.params.fast).toBe(9)
    expect(round.params.period).toBe('1h')
  })

  it('validateIndicatorValues: 非正数报错，fast≥slow 仅提示', () => {
    expect(validateIndicatorValues('macd', { fast: -1, slow: 26, signal: 9 })).toContain('快线')
    expect(validateIndicatorValues('macd', { fast: 30, slow: 26, signal: 9 })).toContain('提示')
    expect(validateIndicatorValues('macd', { fast: 12, slow: 26, signal: 9 })).toBeNull()
  })

  it('defaultIndicatorParams 提供各指标默认值', () => {
    expect(defaultIndicatorParams('macd')).toMatchObject({ fast: 12, slow: 26, signal: 9 })
    expect(defaultIndicatorParams('none')).toEqual({})
  })
})

describe('IndicatorPicker（组件交互）', () => {
  const baseProps = {
    indicator: 'none' as const,
    params: {},
    custom: null,
    direction: 'long' as const,
    onChange: vi.fn(),
  }

  it('未设置状态显示壳默认文案', () => {
    render(<IndicatorPicker {...baseProps} />, { wrapper })
    expect(screen.getByText('未设置（将使用壳默认）：首单不受指标门槛限制。')).toBeTruthy()
  })

  it('点击指标卡 → 选中并携带默认参数回调', () => {
    const onChange = vi.fn()
    render(<IndicatorPicker {...baseProps} onChange={onChange} />, { wrapper })
    fireEvent.click(screen.getByText('MACD'))
    expect(onChange).toHaveBeenCalled()
    const sel = onChange.mock.calls[0][0] as { indicator: string; params: Record<string, number> }
    expect(sel.indicator).toBe('macd')
    expect(sel.params).toMatchObject({ fast: 12, slow: 26, signal: 9 })
    // 选中后自动打开参数弹窗
    expect(screen.getByRole('dialog')).toBeTruthy()
  })

  it('弹窗改参数 → 确认 → onChange 携带新值', async () => {
    const onChange = vi.fn()
    const { rerender } = render(<IndicatorPicker {...baseProps} indicator="macd" params={{ fast: 12, slow: 26, signal: 9, period: 'close' }} onChange={onChange} />, { wrapper })
    fireEvent.click(screen.getByLabelText('MACD 参数'))
    const dialog = screen.getByRole('dialog')
    expect(dialog).toBeTruthy()

    const fastInput = dialog.querySelector('#ind-param-fast') as HTMLInputElement
    fireEvent.change(fastInput, { target: { value: '8' } })
    fireEvent.click(screen.getByText('确认'))

    await waitFor(() => expect(onChange).toHaveBeenCalled())
    const sel = onChange.mock.calls[0][0] as { indicator: string; params: Record<string, number> }
    expect(sel.indicator).toBe('macd')
    expect(sel.params.fast).toBe(8)
    expect(sel.params.slow).toBe(26)
    rerender(<IndicatorPicker {...baseProps} onChange={onChange} />)
  })

  it('点击未设置 → 回调 indicator=none', () => {
    const onChange = vi.fn()
    render(<IndicatorPicker {...baseProps} indicator="macd" params={{ fast: 12 }} onChange={onChange} />, { wrapper })
    fireEvent.click(screen.getByText('未设置'))
    const sel = onChange.mock.calls[0][0] as { indicator: string }
    expect(sel.indicator).toBe('none')
  })
})
