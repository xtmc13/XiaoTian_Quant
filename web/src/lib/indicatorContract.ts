/**
 * Indicator IDE Contract types and utilities.
 * Mirrors the backend I/O contract for indicator scripts.
 */

export type ParamType = 'int' | 'float' | 'bool' | 'str'

export interface ParamRange {
  min: number
  max: number
  step: number
}

export interface ParamDecl {
  name: string
  type: ParamType
  default: unknown
  description: string
  range?: ParamRange
  values?: unknown[]
}

export interface StrategyConfig {
  stopLossPct?: number
  takeProfitPct?: number
  entryPct?: number
  trailingEnabled?: boolean
  trailingStopPct?: number
  trailingActivationPct?: number
  tradeDirection?: 'long' | 'short' | 'both'
}

export interface Plot {
  name: string
  data: unknown[]
  color?: string
  overlay?: boolean
  type?: string
}

export interface Signal {
  type: 'buy' | 'sell'
  text?: string
  data: unknown[]
  color?: string
}

export interface IndicatorOutput {
  name: string
  plots: Plot[]
  signals: Signal[]
  calculatedVars?: Record<string, unknown>
}

export interface ValidationHint {
  severity: 'error' | 'warn' | 'info'
  code: string
  params?: Record<string, unknown>
}

export interface ValidateResult {
  success: boolean
  msg: string
  errorType?: string
  details?: string
  plotsCount: number
  signalsCount: number
  hints: ValidationHint[]
}

export interface ParseResult {
  name: string
  description: string
  params: ParamDecl[]
  strategyConfig: StrategyConfig
}

export interface SavedIndicator {
  id: number
  userId: number
  name: string
  description: string
  code: string
  paramsJson: string
  strategyJson: string
  isEncrypted: number
  createdAt: number
  updatedAt: number
}

// --- Client-side regex parsers (mirror backend logic for instant feedback) ---

const paramRegex = /^\s*#\s*@param\s+(\S+)\s+(\S+)\s+(\S+)\s+(.+)$/
const strategyRegex = /^\s*#\s*@strategy\s+(\S+)\s+(.+)$/
const metaNameRegex = /^\s*my_indicator_name\s*=\s*(["'])(.*?)\1\s*$/m
const metaDescRegex = /^\s*my_indicator_description\s*=\s*(["'])(.*?)\1\s*$/m
const rangeRegex = /range\s*=\s*([\d.]+)\s*:\s*([\d.]+)\s*:\s*([\d.]+)/
const valuesRegex = /values\s*=\s*(.+)$/

function normalizeParamType(t: string): ParamType {
  const lowered = t.toLowerCase()
  switch (lowered) {
    case 'string':
    case 'str':
      return 'str'
    case 'integer':
    case 'int':
      return 'int'
    case 'boolean':
    case 'bool':
      return 'bool'
    case 'double':
    case 'number':
    case 'float':
      return 'float'
    default:
      return lowered as ParamType
  }
}

function parseDefaultValue(typ: ParamType, val: string): unknown {
  switch (typ) {
    case 'int':
      return parseInt(val, 10) || 0
    case 'float':
      return parseFloat(val) || 0
    case 'bool': {
      const v = val.toLowerCase()
      return v === 'true' || v === '1' || v === 'yes'
    }
    case 'str':
      return val.replace(/^["']|["']$/g, '')
    default:
      return val
  }
}

export function parseParamsFromCode(code: string): ParseResult {
  const result: ParseResult = {
    name: '',
    description: '',
    params: [],
    strategyConfig: {},
  }

  const nameMatch = metaNameRegex.exec(code)
  if (nameMatch) result.name = nameMatch[2].trim()

  const descMatch = metaDescRegex.exec(code)
  if (descMatch) result.description = descMatch[2].trim()

  const lines = code.split('\n')
  for (const line of lines) {
    const trimmed = line.trim()
    if (!trimmed.startsWith('#')) continue

    const paramMatch = paramRegex.exec(trimmed)
    if (paramMatch) {
      const name = paramMatch[1]
      const typ = normalizeParamType(paramMatch[2])
      const defaultVal = paramMatch[3]
      const rest = paramMatch[4]

      const param: ParamDecl = {
        name,
        type: typ,
        default: parseDefaultValue(typ, defaultVal),
        description: rest,
      }

      const rangeMatch = rangeRegex.exec(rest)
      if (rangeMatch) {
        param.range = {
          min: parseFloat(rangeMatch[1]),
          max: parseFloat(rangeMatch[2]),
          step: parseFloat(rangeMatch[3]),
        }
        param.description = rest.replace(rangeRegex, '').trim()
      }

      const valuesMatch = valuesRegex.exec(rest)
      if (valuesMatch) {
        param.values = valuesMatch[1].split(',').map(v => {
          const s = v.trim()
          const n = parseFloat(s)
          return isNaN(n) ? s : n
        })
        param.description = rest.replace(valuesRegex, '').trim()
      }

      result.params.push(param)
      continue
    }

    const strategyMatch = strategyRegex.exec(trimmed)
    if (strategyMatch) {
      const key = strategyMatch[1]
      const value = strategyMatch[2].trim()
      applyStrategyKey(result.strategyConfig, key, value)
    }
  }

  return result
}

function applyStrategyKey(cfg: StrategyConfig, key: string, value: string) {
  switch (key) {
    case 'stopLossPct':
      cfg.stopLossPct = parseFloat(value)
      break
    case 'takeProfitPct':
      cfg.takeProfitPct = parseFloat(value)
      break
    case 'entryPct':
      cfg.entryPct = parseFloat(value)
      break
    case 'trailingEnabled':
      cfg.trailingEnabled = value.toLowerCase() === 'true' || value === '1'
      break
    case 'trailingStopPct':
      cfg.trailingStopPct = parseFloat(value)
      break
    case 'trailingActivationPct':
      cfg.trailingActivationPct = parseFloat(value)
      break
    case 'tradeDirection':
      cfg.tradeDirection = value as StrategyConfig['tradeDirection']
      break
  }
}

/**
 * Extracts the indicator output dict from Python code as a JSON string.
 * This is a best-effort extraction for client-side preview.
 */
export function extractOutputFromCode(code: string): IndicatorOutput | null {
  const outputMatch = code.match(/output\s*=\s*(\{[\s\S]*?\n\})/)
  if (!outputMatch) return null
  try {
    // Replace Python None/null, True/true, False/false, single quotes
    const jsonLike = outputMatch[1]
      .replace(/\bNone\b/g, 'null')
      .replace(/\bTrue\b/g, 'true')
      .replace(/\bFalse\b/g, 'false')
      .replace(/'/g, '"')
      // Handle trailing commas
      .replace(/,\s*([}\]])/g, '$1')
    return JSON.parse(jsonLike)
  } catch {
    return null
  }
}

/**
 * Default indicator code template following the XiaoTianQuant contract.
 */
export const DEFAULT_INDICATOR_CODE = `my_indicator_name = "双均线交叉"
my_indicator_description = "短期均线上穿长期均线买入，下穿卖出"

# @param fast_period int 12 短期均线周期 range=5:50:1
# @param slow_period int 26 长期均线周期 range=10:100:1
# @strategy stopLossPct 0.03
# @strategy takeProfitPct 0.06
# @strategy tradeDirection both

import talib

fast_period = params.get('fast_period', 12)
slow_period = params.get('slow_period', 26)

df = df.copy()
close = df['close'].values
ma_fast = talib.SMA(close, timeperiod=fast_period)
ma_slow = talib.SMA(close, timeperiod=slow_period)

raw_buy = pd.Series(ma_fast > ma_slow, index=df.index) & pd.Series(pd.Series(ma_fast).shift(1) <= pd.Series(ma_slow).shift(1), index=df.index)
raw_sell = pd.Series(ma_fast < ma_slow, index=df.index) & pd.Series(pd.Series(ma_fast).shift(1) >= pd.Series(ma_slow).shift(1), index=df.index)

df['buy'] = raw_buy.fillna(False).astype(bool)
df['sell'] = raw_sell.fillna(False).astype(bool)

output = {
    'name': my_indicator_name,
    'plots': [
        {'name': f'MA({fast_period})', 'data': ma_fast.tolist(), 'color': '#00E676', 'overlay': True},
        {'name': f'MA({slow_period})', 'data': ma_slow.tolist(), 'color': '#FF5252', 'overlay': True}
    ],
    'signals': [
        {'type': 'buy', 'text': 'B', 'data': [df['low'].iloc[i]*0.995 if df['buy'].iloc[i] else None for i in range(len(df))], 'color': '#00E676'},
        {'type': 'sell', 'text': 'S', 'data': [df['high'].iloc[i]*1.005 if df['sell'].iloc[i] else None for i in range(len(df))], 'color': '#FF5252'}
    ]
}
`

export const RSI_TEMPLATE = `my_indicator_name = "RSI超买超卖"
my_indicator_description = "RSI低于超卖线买入，高于超买线卖出"

# @param period int 14 RSI周期 range=5:30:1
# @param oversold float 30 超卖阈值 range=10:40:1
# @param overbought float 70 超买阈值 range=60:90:1
# @strategy stopLossPct 0.03
# @strategy takeProfitPct 0.05
# @strategy tradeDirection both

import talib

period = params.get('period', 14)
oversold = params.get('oversold', 30)
overbought = params.get('overbought', 70)

df = df.copy()
close = df['close'].values
rsi = talib.RSI(close, timeperiod=period)

raw_buy = pd.Series(rsi < oversold, index=df.index) & pd.Series(rsi.shift(1) >= oversold, index=df.index)
raw_sell = pd.Series(rsi > overbought, index=df.index) & pd.Series(rsi.shift(1) <= overbought, index=df.index)

df['buy'] = raw_buy.fillna(False).astype(bool)
df['sell'] = raw_sell.fillna(False).astype(bool)

output = {
    'name': my_indicator_name,
    'plots': [
        {'name': f'RSI({period})', 'data': rsi.tolist(), 'color': '#faad14', 'overlay': False}
    ],
    'signals': [
        {'type': 'buy', 'text': 'B', 'data': [df['low'].iloc[i]*0.995 if df['buy'].iloc[i] else None for i in range(len(df))], 'color': '#00E676'},
        {'type': 'sell', 'text': 'S', 'data': [df['high'].iloc[i]*1.005 if df['sell'].iloc[i] else None for i in range(len(df))], 'color': '#FF5252'}
    ]
}
`

export const MACD_TEMPLATE = `my_indicator_name = "MACD金叉死叉"
my_indicator_description = "MACD金叉买入，死叉卖出"

# @param fast int 12 快线周期 range=5:50:1
# @param slow int 26 慢线周期 range=10:100:1
# @param signal int 9 信号线周期 range=5:30:1
# @strategy stopLossPct 0.03
# @strategy takeProfitPct 0.06
# @strategy tradeDirection both

import talib

fast = params.get('fast', 12)
slow = params.get('slow', 26)
signal_period = params.get('signal', 9)

df = df.copy()
close = df['close'].values
macd, macdsignal, macdhist = talib.MACD(close, fastperiod=fast, slowperiod=slow, signalperiod=signal_period)

raw_buy = pd.Series(macd > macdsignal, index=df.index) & pd.Series(pd.Series(macd).shift(1) <= pd.Series(macdsignal).shift(1), index=df.index)
raw_sell = pd.Series(macd < macdsignal, index=df.index) & pd.Series(pd.Series(macd).shift(1) >= pd.Series(macdsignal).shift(1), index=df.index)

df['buy'] = raw_buy.fillna(False).astype(bool)
df['sell'] = raw_sell.fillna(False).astype(bool)

output = {
    'name': my_indicator_name,
    'plots': [
        {'name': 'MACD', 'data': macd.tolist(), 'color': '#00E676', 'overlay': False},
        {'name': 'Signal', 'data': macdsignal.tolist(), 'color': '#FF5252', 'overlay': False},
        {'name': 'Hist', 'data': macdhist.tolist(), 'color': '#faad14', 'overlay': False}
    ],
    'signals': [
        {'type': 'buy', 'text': 'B', 'data': [df['low'].iloc[i]*0.995 if df['buy'].iloc[i] else None for i in range(len(df))], 'color': '#00E676'},
        {'type': 'sell', 'text': 'S', 'data': [df['high'].iloc[i]*1.005 if df['sell'].iloc[i] else None for i in range(len(df))], 'color': '#FF5252'}
    ]
}
`

export const BOLLINGER_TEMPLATE = `my_indicator_name = "布林带突破"
my_indicator_description = "价格突破下轨买入，突破上轨卖出"

# @param period int 20 周期 range=10:50:1
# @param mult float 2.0 标准差倍数 range=1:4:0.5
# @strategy stopLossPct 0.03
# @strategy takeProfitPct 0.06
# @strategy tradeDirection both

import talib

period = params.get('period', 20)
mult = params.get('mult', 2.0)

df = df.copy()
close = df['close'].values
upper, middle, lower = talib.BBANDS(close, timeperiod=period, nbdevup=mult, nbdevdn=mult)

raw_buy = pd.Series(df['close'] < lower, index=df.index) & pd.Series(df['close'].shift(1) >= pd.Series(lower).shift(1), index=df.index)
raw_sell = pd.Series(df['close'] > upper, index=df.index) & pd.Series(df['close'].shift(1) <= pd.Series(upper).shift(1), index=df.index)

df['buy'] = raw_buy.fillna(False).astype(bool)
df['sell'] = raw_sell.fillna(False).astype(bool)

output = {
    'name': my_indicator_name,
    'plots': [
        {'name': f'BB({period},{mult})', 'data': middle.tolist(), 'color': '#00E676', 'overlay': True},
        {'name': 'Upper', 'data': upper.tolist(), 'color': '#FF5252', 'overlay': True},
        {'name': 'Lower', 'data': lower.tolist(), 'color': '#FF5252', 'overlay': True}
    ],
    'signals': [
        {'type': 'buy', 'text': 'B', 'data': [df['low'].iloc[i]*0.995 if df['buy'].iloc[i] else None for i in range(len(df))], 'color': '#00E676'},
        {'type': 'sell', 'text': 'S', 'data': [df['high'].iloc[i]*1.005 if df['sell'].iloc[i] else None for i in range(len(df))], 'color': '#FF5252'}
    ]
}
`

export const INDICATOR_TEMPLATES = [
  { key: 'sma_cross', label: '双均线交叉', code: DEFAULT_INDICATOR_CODE },
  { key: 'rsi', label: 'RSI 超买超卖', code: RSI_TEMPLATE },
  { key: 'macd', label: 'MACD 金叉死叉', code: MACD_TEMPLATE },
  { key: 'bollinger', label: '布林带突破', code: BOLLINGER_TEMPLATE },
]

