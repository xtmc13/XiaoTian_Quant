import { cn } from '@/lib/utils'
import type { StrategyItem, StrategyParamDef } from '@/types'

export type StrategyRow = StrategyItem & {
  add_position_multiple?: number
  moving_take_profit?: { enabled: boolean; tier1_ratio: number; tier1_drawback: number; tier2_drawback: number }
  reverse_take_profit?: boolean
  reverse_stop_loss?: boolean
  amplitude?: { '5m': number; '15m': number; '30m': number; '1h': number }
  custom_reduce?: boolean
  online_order_limit?: number
  profit_protection?: boolean
  follow_trend_max?: number
  stop_loss_ratio?: number
  stop_loss_amount?: number
  stop_loss_price?: number
  first_order_price?: number
}

export const STRAT_TYPES: Record<string, { value: string; label: string }[]> = {
  spot: [
    { value: 'martin_trend', label: '马丁趋势' },
    { value: 'wallstreet', label: '华尔街' },
    { value: 'aggressive', label: '激进' },
    { value: 'conservative', label: '保守' },
    { value: 'high_frequency', label: '高频' },
  ],
  contract: [
    { value: 'trend_long', label: '顺势多' },
    { value: 'trend_short', label: '顺势空' },
    { value: 'counter_stable', label: '逆势稳健' },
    { value: 'counter_safe', label: '逆势保守' },
    { value: 'high_frequency', label: '高频策略' },
    { value: 'head_tail_arbitrage', label: '首尾套利' },
  ],
}

export const TIMEFRAMES = ['1m', '5m', '15m', '30m', '1h', '4h', '8h', '1D']

const BASE_CODE = `from freqtrade.strategy import IStrategy
import talib.abstract as ta

class MyStrategy(IStrategy):
    timeframe = '15m'
    minimal_roi = {"0": 0.01, "60": 0.005}
    stoploss = -0.40

    def populate_indicators(self, dataframe, metadata):
        {{indicators}}
        return dataframe

    def populate_entry_trend(self, dataframe, metadata):
        {{entry}}
        return dataframe

    def populate_exit_trend(self, dataframe, metadata):
        {{exit}}
        return dataframe`

const INDICATOR_TEMPLATES: Record<string, { indicators: string; entry: string; exit: string }> = {
  ema: {
    indicators: `dataframe['ema_short'] = ta.EMA(dataframe, timeperiod=12)
        dataframe['ema_long'] = ta.EMA(dataframe, timeperiod=26)`,
    entry: `dataframe.loc[dataframe['ema_short'] > dataframe['ema_long'], 'enter_long'] = 1`,
    exit: `dataframe.loc[dataframe['ema_short'] < dataframe['ema_long'], 'exit_long'] = 1`,
  },
  macd: {
    indicators: `macd = ta.MACD(dataframe, fastperiod=12, slowperiod=26, signalperiod=9)
        dataframe['macd'] = macd['macd']
        dataframe['macdsignal'] = macd['macdsignal']`,
    entry: `dataframe.loc[dataframe['macd'] > dataframe['macdsignal'], 'enter_long'] = 1`,
    exit: `dataframe.loc[dataframe['macd'] < dataframe['macdsignal'], 'exit_long'] = 1`,
  },
  rsi: {
    indicators: `dataframe['rsi'] = ta.RSI(dataframe, timeperiod=14)`,
    entry: `dataframe.loc[dataframe['rsi'] < 30, 'enter_long'] = 1`,
    exit: `dataframe.loc[dataframe['rsi'] > 70, 'exit_long'] = 1`,
  },
  bollinger: {
    indicators: `bbands = ta.BBANDS(dataframe, timeperiod=20)
        dataframe['bb_upper'] = bbands['upperband']
        dataframe['bb_lower'] = bbands['lowerband']`,
    entry: `dataframe.loc[dataframe['close'] < dataframe['bb_lower'], 'enter_long'] = 1`,
    exit: `dataframe.loc[dataframe['close'] > dataframe['bb_upper'], 'exit_long'] = 1`,
  },
  breakout: {
    indicators: `dataframe['high_20'] = dataframe['high'].rolling(window=20).max()
        dataframe['low_20'] = dataframe['low'].rolling(window=20).min()`,
    entry: `dataframe.loc[dataframe['close'] > dataframe['high_20'].shift(1), 'enter_long'] = 1`,
    exit: `dataframe.loc[dataframe['close'] < dataframe['low_20'].shift(1), 'exit_long'] = 1`,
  },
}

function strategyTypeToTemplateKey(strategyType: string): keyof typeof INDICATOR_TEMPLATES | 'generic' {
  if (strategyType.startsWith('ema')) return 'ema'
  if (strategyType.startsWith('macd')) return 'macd'
  if (strategyType.startsWith('rsi')) return 'rsi'
  if (strategyType.startsWith('bollinger')) return 'bollinger'
  if (['breakout', 'trend', 'custom'].includes(strategyType)) return 'breakout'
  return 'generic'
}

/** Return a starter Python strategy template for the selected strategy type. */
export function getDefaultStrategyCode(strategyType: string): string {
  const key = strategyTypeToTemplateKey(strategyType)
  if (key === 'generic') {
    return BASE_CODE.replace(
      '{{indicators}}',
      `# 在这里添加你的指标，例如：
        # dataframe['rsi'] = ta.RSI(dataframe, timeperiod=14)`
    )
      .replace('{{entry}}', `# 在这里添加入场逻辑`)
      .replace('{{exit}}', `# 在这里添加出场逻辑`)
  }
  const t = INDICATOR_TEMPLATES[key]
  return BASE_CODE.replace('{{indicators}}', t.indicators).replace('{{entry}}', t.entry).replace('{{exit}}', t.exit)
}

export function FormField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="text-[11px] text-muted-foreground mb-1.5 block">{label}</span>
      {children}
    </label>
  )
}

export function Toggle({ value, onChange }: { value: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      onClick={() => onChange(!value)}
      className={cn('w-10 h-5 rounded-full relative transition-colors', value ? 'bg-quant-gold' : 'bg-quant-border')}
    >
      <span
        className={cn(
          'absolute top-0.5 w-4 h-4 rounded-full bg-white transition-transform',
          value ? 'left-5' : 'left-0.5'
        )}
      />
    </button>
  )
}

export function DynamicParamField({
  def,
  value,
  onChange,
}: {
  def: StrategyParamDef
  value: unknown
  onChange: (val: unknown) => void
}) {
  const label = def.label || def.name
  const desc = def.description
  const inputCls =
    'w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold'
  switch (def.type) {
    case 'int':
      return (
        <FormField label={label}>
          <input
            type="number"
            min={def.min}
            max={def.max}
            step={def.step || 1}
            value={Number(value ?? (def.default as number) ?? 0)}
            onChange={(e) => onChange(Number(e.target.value))}
            className={inputCls}
          />
          {desc && <p className="text-[10px] text-muted-foreground mt-1">{desc}</p>}
        </FormField>
      )
    case 'float':
      return (
        <FormField label={label}>
          <input
            type="number"
            min={def.min}
            max={def.max}
            step={def.step || 0.01}
            value={Number(value ?? (def.default as number) ?? 0)}
            onChange={(e) => onChange(Number(e.target.value))}
            className={inputCls}
          />
          {desc && <p className="text-[10px] text-muted-foreground mt-1">{desc}</p>}
        </FormField>
      )
    case 'string':
      return (
        <FormField label={label}>
          <input
            type="text"
            value={String(value ?? (def.default as string) ?? '')}
            onChange={(e) => onChange(e.target.value)}
            className={inputCls}
          />
          {desc && <p className="text-[10px] text-muted-foreground mt-1">{desc}</p>}
        </FormField>
      )
    case 'bool':
      return (
        <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-3">
          <div>
            <div className="text-xs font-medium">{label}</div>
            {desc && <div className="text-[10px] text-muted-foreground">{desc}</div>}
          </div>
          <Toggle value={Boolean(value ?? (def.default as boolean) ?? false)} onChange={onChange} />
        </label>
      )
    case 'enum':
      return (
        <FormField label={label}>
          <select
            value={String(value ?? (def.default as string) ?? '')}
            onChange={(e) => onChange(e.target.value)}
            className={inputCls}
          >
            {(def.options || []).map((opt) => (
              <option key={opt} value={opt}>
                {opt}
              </option>
            ))}
          </select>
          {desc && <p className="text-[10px] text-muted-foreground mt-1">{desc}</p>}
        </FormField>
      )
    default:
      return (
        <FormField label={label}>
          <input
            type="text"
            value={String(value ?? def.default ?? '')}
            onChange={(e) => onChange(e.target.value)}
            className={inputCls}
          />
        </FormField>
      )
  }
}
