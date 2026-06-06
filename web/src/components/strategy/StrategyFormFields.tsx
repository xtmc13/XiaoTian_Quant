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
  contract: [
    { value: 'trend_long', label: '顺势做多（EMA金叉）' },
    { value: 'trend_short', label: '顺势做空（EMA死叉）' },
    { value: 'counter_stable', label: '逆势稳健（EMA60振幅）' },
    { value: 'counter_safe', label: '逆势保守' },
    { value: 'high_flat', label: '高平策略' },
    { value: 'head_tail_arb', label: '首尾套利' },
    { value: 'macd_golden_long', label: 'MACD金叉开多' },
    { value: 'macd_death_short', label: 'MACD死叉开空' },
    { value: 'ema_follow_trend', label: 'EMA顺势（拐点开仓）' },
    { value: 'ema_counter_trend', label: 'EMA逆势（振幅开仓）' },
    { value: 'dual_burn', label: '双向燃烧斩仓' },
    { value: 'global_burn', label: '超级全局燃烧斩仓' },
  ],
  spot: [
    { value: 'martin_trend', label: '马丁趋势策略（倍投2,4,8,16,32,64）' },
    { value: 'wallstreet', label: '华尔街策略（等比1,2,3,5,8,13,21,34,55）' },
    { value: 'aggressive', label: '激进策略' },
    { value: 'conservative', label: '保守策略' },
    { value: 'high_flat', label: '高平策略' },
    { value: 'macd_spot_long', label: 'MACD金叉开多' },
    { value: 'ema_spot', label: 'EMA拐点策略' },
  ],
}

export const TIMEFRAMES = ['1m', '5m', '15m', '30m', '1h', '4h', '8h', '1D']

export const DEFAULT_CODE = `from freqtrade.strategy import IStrategy
import talib.abstract as ta

class MyStrategy(IStrategy):
    timeframe = '15m'
    minimal_roi = {"0": 0.01, "60": 0.005}
    stoploss = -0.40

    def populate_indicators(self, dataframe, metadata):
        dataframe['ema_short'] = ta.EMA(dataframe, timeperiod=12)
        dataframe['ema_long'] = ta.EMA(dataframe, timeperiod=26)
        return dataframe

    def populate_entry_trend(self, dataframe, metadata):
        dataframe.loc[dataframe['ema_short'] > dataframe['ema_long'], 'enter_long'] = 1
        return dataframe

    def populate_exit_trend(self, dataframe, metadata):
        dataframe.loc[dataframe['ema_short'] < dataframe['ema_long'], 'exit_long'] = 1
        return dataframe`

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
      <span className={cn('absolute top-0.5 w-4 h-4 rounded-full bg-white transition-transform', value ? 'left-5' : 'left-0.5')} />
    </button>
  )
}

export function DynamicParamField({ def, value, onChange }: { def: StrategyParamDef; value: unknown; onChange: (val: unknown) => void }) {
  const label = def.label || def.name
  const desc = def.description
  const inputCls = "w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold"
  switch (def.type) {
    case 'int':
      return (
        <FormField label={label}>
          <input type="number" min={def.min} max={def.max} step={def.step || 1} value={Number(value ?? (def.default as number) ?? 0)} onChange={(e) => onChange(Number(e.target.value))} className={inputCls} />
          {desc && <p className="text-[10px] text-muted-foreground mt-1">{desc}</p>}
        </FormField>
      )
    case 'float':
      return (
        <FormField label={label}>
          <input type="number" min={def.min} max={def.max} step={def.step || 0.01} value={Number(value ?? (def.default as number) ?? 0)} onChange={(e) => onChange(Number(e.target.value))} className={inputCls} />
          {desc && <p className="text-[10px] text-muted-foreground mt-1">{desc}</p>}
        </FormField>
      )
    case 'string':
      return (
        <FormField label={label}>
          <input type="text" value={String(value ?? (def.default as string) ?? '')} onChange={(e) => onChange(e.target.value)} className={inputCls} />
          {desc && <p className="text-[10px] text-muted-foreground mt-1">{desc}</p>}
        </FormField>
      )
    case 'bool':
      return (
        <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-3">
          <div><div className="text-xs font-medium">{label}</div>{desc && <div className="text-[10px] text-muted-foreground">{desc}</div>}</div>
          <Toggle value={Boolean(value ?? (def.default as boolean) ?? false)} onChange={onChange} />
        </label>
      )
    case 'enum':
      return (
        <FormField label={label}>
          <select value={String(value ?? (def.default as string) ?? '')} onChange={(e) => onChange(e.target.value)} className={inputCls}>
            {(def.options || []).map((opt) => <option key={opt} value={opt}>{opt}</option>)}
          </select>
          {desc && <p className="text-[10px] text-muted-foreground mt-1">{desc}</p>}
        </FormField>
      )
    default:
      return (
        <FormField label={label}>
          <input type="text" value={String(value ?? def.default ?? '')} onChange={(e) => onChange(e.target.value)} className={inputCls} />
        </FormField>
      )
  }
}
