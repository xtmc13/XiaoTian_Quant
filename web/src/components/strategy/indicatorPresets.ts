/**
 * 开仓指标选择器（IndicatorPicker）的指标定义与 config 键映射。
 *
 * 新旧键兼容设计：
 * - 引擎已认识的键（open_macd_enabled/open_macd_period/open_trend_ema_enabled/
 *   open_trend_ema_period）按"选择器派生优先、旧字段回退"写入，引擎无需改动
 *   即可用默认参数跑 MACD/EMA 门槛；
 * - 新参数统一放 config.indicator_params[<key>]，后端 CRA 解析读新键、回退旧键；
 * - open_indicator 为新标记键（'custom' 等无引擎门槛的指标靠它识别）。
 */

export type OpenIndicatorKey =
  | 'none'
  | 'macd'
  | 'ema_cross'
  | 'rsi'
  | 'trend'
  | 'range'
  | 'custom'

export type IndicatorParamValue = number | string

export interface IndicatorFieldDef {
  key: string
  label: string
  type: 'int' | 'float' | 'select'
  default: IndicatorParamValue
  min?: number
  max?: number
  options?: { value: string; label: string }[]
}

export interface IndicatorDef {
  key: Exclude<OpenIndicatorKey, 'none'>
  label: string
  desc: string
  fields: IndicatorFieldDef[]
}

export const PERIOD_OPTIONS = [
  { value: 'close', label: '不额外监测（跟随主周期）' },
  { value: '5m', label: '5 分钟' },
  { value: '15m', label: '15 分钟' },
]

export const OPEN_INDICATORS: IndicatorDef[] = [
  {
    key: 'macd',
    label: 'MACD',
    desc: '快慢线金叉开仓',
    fields: [
      { key: 'fast', label: '快线周期', type: 'int', default: 12, min: 1, max: 500 },
      { key: 'slow', label: '慢线周期', type: 'int', default: 26, min: 1, max: 500 },
      { key: 'signal', label: '信号周期', type: 'int', default: 9, min: 1, max: 200 },
      { key: 'period', label: '监测周期', type: 'select', default: 'close', options: PERIOD_OPTIONS },
    ],
  },
  {
    key: 'ema_cross',
    label: 'EMA交叉',
    desc: '双均线金叉/死叉',
    fields: [
      { key: 'fast', label: '快线周期', type: 'int', default: 12, min: 1, max: 500 },
      { key: 'slow', label: '慢线周期', type: 'int', default: 26, min: 1, max: 500 },
      { key: 'period', label: '监测周期', type: 'select', default: 'close', options: PERIOD_OPTIONS },
    ],
  },
  {
    key: 'rsi',
    label: 'RSI超卖',
    desc: 'RSI 进入超卖区反弹开仓',
    fields: [
      { key: 'period', label: 'RSI 周期', type: 'int', default: 14, min: 2, max: 200 },
      { key: 'oversold', label: '超卖阈值', type: 'int', default: 30, min: 1, max: 100 },
      { key: 'overbought', label: '超买阈值', type: 'int', default: 70, min: 1, max: 100 },
    ],
  },
  {
    key: 'trend',
    label: '趋势确认',
    desc: '价格在均线之上确认趋势后开仓',
    fields: [
      { key: 'period', label: '均线周期', type: 'int', default: 20, min: 2, max: 500 },
      {
        key: 'ma_type',
        label: '均线类型',
        type: 'select',
        default: 'EMA',
        options: [
          { value: 'EMA', label: 'EMA（指数移动平均）' },
          { value: 'SMA', label: 'SMA（简单移动平均）' },
        ],
      },
    ],
  },
  {
    key: 'range',
    label: '震荡',
    desc: 'RSI 中性区往返（高抛低吸）',
    fields: [
      { key: 'period', label: 'RSI 周期', type: 'int', default: 14, min: 2, max: 200 },
      { key: 'neutral_band', label: '中性带宽（±）', type: 'int', default: 10, min: 1, max: 50 },
    ],
  },
  {
    key: 'custom',
    label: '自定义指标',
    desc: '使用指标实验室的自定义指标开仓',
    fields: [],
  },
]

export function getIndicatorDef(key: OpenIndicatorKey): IndicatorDef | undefined {
  return OPEN_INDICATORS.find((d) => d.key === key)
}

export function defaultIndicatorParams(key: OpenIndicatorKey): Record<string, IndicatorParamValue> {
  const def = getIndicatorDef(key)
  if (!def) return {}
  return Object.fromEntries(def.fields.map((f) => [f.key, f.default]))
}

/**
 * 数值校验（宽松）：fast<slow 只给提示不硬拦；范围/正整数违规返回错误文案，
 * 由调用方决定提示或拦截。
 */
export function validateIndicatorValues(
  key: OpenIndicatorKey,
  values: Record<string, IndicatorParamValue>
): string | null {
  const def = getIndicatorDef(key)
  if (!def) return null
  for (const f of def.fields) {
    const v = values[f.key]
    if (v == null || v === '') continue
    if (f.type === 'select') continue
    const n = Number(v)
    if (!Number.isFinite(n)) return `${f.label}必须是数字`
    if (f.type === 'int' && !Number.isInteger(n)) return `${f.label}必须是正整数`
    if (f.min != null && n < f.min) return `${f.label}不能小于 ${f.min}`
    if (f.max != null && n > f.max) return `${f.label}不能大于 ${f.max}`
  }
  const fast = Number(values.fast)
  const slow = Number(values.slow)
  if (Number.isFinite(fast) && Number.isFinite(slow) && fast >= slow) {
    return '提示：快线周期建议小于慢线周期（当前 fast ≥ slow）'
  }
  return null
}

/** 选择器值 → 引擎兼容键 + 新键（写入 config 的对象片段）。 */
export interface OpenIndicatorConfig {
  open_macd_enabled: boolean
  open_macd_period: string
  open_counter_ema_enabled: boolean
  open_counter_ema_period: string
  open_trend_ema_enabled: boolean
  open_trend_ema_period: string
  open_indicator?: string
  indicator_params?: Record<string, unknown>
}

export function buildOpenIndicatorConfig(
  key: OpenIndicatorKey,
  params: Record<string, IndicatorParamValue>,
  custom: { code_id: number; name: string } | null
): OpenIndicatorConfig {
  const cfg: OpenIndicatorConfig = {
    open_macd_enabled: false,
    open_macd_period: 'close',
    open_counter_ema_enabled: false,
    open_counter_ema_period: 'close',
    open_trend_ema_enabled: false,
    open_trend_ema_period: 'close',
  }
  if (key === 'none') return cfg
  cfg.open_indicator = key
  const period = typeof params.period === 'string' && params.period !== 'close' ? params.period : undefined
  switch (key) {
    case 'macd':
      cfg.open_macd_enabled = true
      cfg.open_macd_period = period ?? 'close'
      cfg.indicator_params = { macd: { ...params } }
      break
    case 'ema_cross':
      // 引擎侧以"顺势 EMA"门槛承载双均线类指标。
      cfg.open_trend_ema_enabled = true
      cfg.open_trend_ema_period = period ?? 'close'
      cfg.indicator_params = { ema_cross: { ...params } }
      break
    case 'trend':
      cfg.open_trend_ema_enabled = true
      cfg.open_trend_ema_period = 'close'
      cfg.indicator_params = { trend: { ...params } }
      break
    case 'rsi':
    case 'range':
      // 无对应引擎门槛键：引擎视为无开仓门槛，参数仅落 indicator_params。
      cfg.indicator_params = { [key]: { ...params } }
      break
    case 'custom':
      cfg.indicator_params = { custom: custom ?? {} }
      break
  }
  return cfg
}

/** 从旧 config（snake_case payload）反推选择器状态（编辑回显用）。 */
export function detectOpenIndicator(payload: Record<string, unknown>): {
  indicator: OpenIndicatorKey
  params: Record<string, IndicatorParamValue>
  custom: { code_id: number; name: string } | null
  /** 存量顺势多/空记录的方向提示（记录未存 direction 时回填用）。 */
  directionHint?: 'long' | 'short'
} {
  const indicatorParams = (payload.indicator_params as Record<string, unknown> | undefined) ?? {}
  const openIndicator = (payload.open_indicator as string) || ''
  if (openIndicator) {
    // 存量记录：顺势多/顺势空已下线，回退显示为 EMA交叉（方向沿记录原值）。
    if (openIndicator === 'trend_long' || openIndicator === 'trend_short') {
      const params = (indicatorParams[openIndicator] as Record<string, IndicatorParamValue> | undefined) ?? {}
      return {
        indicator: 'ema_cross',
        params: { ...defaultIndicatorParams('ema_cross'), ...params },
        custom: null,
        directionHint: openIndicator === 'trend_long' ? 'long' : 'short',
      }
    }
    const params = (indicatorParams[openIndicator] as Record<string, IndicatorParamValue> | undefined) ?? {}
    const custom =
      openIndicator === 'custom'
        ? (indicatorParams.custom as { code_id: number; name: string } | undefined) ?? null
        : null
    return {
      indicator: openIndicator as OpenIndicatorKey,
      params: { ...defaultIndicatorParams(openIndicator as OpenIndicatorKey), ...params },
      custom,
    }
  }
  // 回退：按旧 enabled 键推导
  if (payload.open_macd_enabled === true) {
    return { indicator: 'macd', params: defaultIndicatorParams('macd'), custom: null }
  }
  if (payload.open_trend_ema_enabled === true) {
    return { indicator: 'trend', params: defaultIndicatorParams('trend'), custom: null }
  }
  if (payload.open_counter_ema_enabled === true) {
    return { indicator: 'ema_cross', params: defaultIndicatorParams('ema_cross'), custom: null }
  }
  return { indicator: 'none', params: {}, custom: null }
}
