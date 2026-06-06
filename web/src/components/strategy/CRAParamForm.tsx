import { useState, useCallback } from 'react'
import { cn } from '@/lib/utils'
import { SectionCard } from '@/components/ui/SectionCard'

/* ── Helpers ── */
function loadLocal<T>(key: string, defaultValue: T): T {
  try {
    const raw = localStorage.getItem(key)
    if (raw === null) return defaultValue
    if (typeof defaultValue === 'boolean') return (raw === 'true') as unknown as T
    if (typeof defaultValue === 'number') return Number(raw) as unknown as T
    return raw as unknown as T
  } catch {
    return defaultValue
  }
}

function saveLocal(key: string, value: unknown) {
  try {
    localStorage.setItem(key, String(value))
  } catch { /* ignore */ }
}

/* ── Sub-components ── */
function NumberInput({
  value,
  onChange,
  min,
  max,
  step = 1,
}: {
  value: number
  onChange: (v: number) => void
  min?: number
  max?: number
  step?: number
}) {
  return (
    <input
      type="number"
      value={value}
      onChange={(e) => onChange(Number(e.target.value))}
      min={min}
      max={max}
      step={step}
      className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none transition-colors focus:border-quant-gold"
    />
  )
}

function Toggle({ value, onChange }: { value: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      onClick={() => onChange(!value)}
      className={cn(
        'relative h-5 w-9 rounded-full transition-colors',
        value ? 'bg-quant-gold' : 'bg-quant-border'
      )}
    >
      <span
        className={cn(
          'absolute top-0.5 h-4 w-4 rounded-full bg-white transition-transform',
          value ? 'translate-x-4.5' : 'translate-x-0.5'
        )}
        style={{ transform: value ? 'translateX(18px)' : 'translateX(2px)' }}
      />
    </button>
  )
}

/* ── Field Definitions ── */
const TP_METHODS = [
  { key: 'full', label: '全仓止盈', desc: '全仓盈利后卖出' },
  { key: 'tail', label: '尾单止盈', desc: '最后一单盈利后卖出减仓' },
  { key: 'head_tail', label: '首尾止盈', desc: '首单+尾单盈利后先行出仓' },
  { key: 'moving', label: '移动止盈', desc: '动态分档止盈' },
]

const OPEN_INDICATORS = [
  { value: 'macd_golden', label: 'MACD金叉开多' },
  { value: 'macd_death', label: 'MACD死叉开空' },
  { value: 'ema', label: 'EMA拐点开仓' },
  { value: 'close', label: '关闭（执行无脑买入）' },
]

const ADD_INDICATORS = [
  { value: 'macd', label: 'MACD金叉/死叉补仓' },
  { value: 'ema', label: 'EMA4上下拐点补仓' },
  { value: 'close', label: '关闭（仅按跌幅补仓）' },
]

const TREND_TFS = [
  { value: '5m', label: '5分钟' },
  { value: '15m', label: '15分钟' },
  { value: '30m', label: '30分钟' },
  { value: '60m', label: '60分钟' },
]

const AMP_FIELDS = [
  { key: 'cra-amp-5m', label: '5分钟', suggest: 2 },
  { key: 'cra-amp-15m', label: '15分钟', suggest: 4 },
  { key: 'cra-amp-30m', label: '30分钟', suggest: 7 },
  { key: 'cra-amp-1h', label: '1小时', suggest: 10 },
]

const KLINE_FIELDS = [
  { key: 'cra-kline-5m', label: '5分钟', desc: '短线' },
  { key: 'cra-kline-15m', label: '15分钟', desc: '中短线' },
  { key: 'cra-kline-30m', label: '30分钟', desc: '中线' },
  { key: 'cra-kline-1h', label: '1小时', desc: '中长线' },
  { key: 'cra-kline-4h', label: '4小时', desc: '长线' },
  { key: 'cra-kline-8h', label: '8小时', desc: '超长线' },
]

const TRADE_COUNT_MODES = [
  { key: 'single', label: '单次循环', desc: '止盈后不再买入，但补仓还会正常进行' },
  { key: 'cycle', label: '策略循环', desc: '卖出后持续买入，直到循环次数用尽' },
]

/* ── Main Component ── */
export interface CRAConfig {
  'cra-order-count': number
  'cra-first-amount': number
  'cra-spread': number
  'cra-callback': number
  'cra-open-double': boolean
  'cra-close-add': boolean
  'cra-tp-ratio': number
  'cra-profit-cb': number
  'cra-tp-method': string
  'cra-mv-tp1': number
  'cra-mv-dbk1': number
  'cra-mv-dbk2': number
  'cra-reverse-tp': boolean
  'cra-reverse-sl': boolean
  'cra-open-ind': string
  'cra-add-ind': string
  'cra-trend-ind': boolean
  'cra-trend-tf': string
  'cra-waterfall': number
  'cra-amp-5m': number
  'cra-amp-15m': number
  'cra-amp-30m': number
  'cra-amp-1h': number
  'cra-burn': boolean
  'cra-burn-dual': number
  'cra-burn-global': number
  'cra-follow': boolean
  'cra-follow-max': number
  'cra-reduce': boolean
  'cra-trade-count': string
  'cra-online-limit': number
  'cra-first-price': number
  'cra-kline-5m': boolean
  'cra-kline-15m': boolean
  'cra-kline-30m': boolean
  'cra-kline-1h': boolean
  'cra-kline-4h': boolean
  'cra-kline-8h': boolean
}

export const CRA_DEFAULTS: CRAConfig = {
  'cra-order-count': 7,
  'cra-first-amount': 100,
  'cra-spread': 3,
  'cra-callback': 0.1,
  'cra-open-double': false,
  'cra-close-add': false,
  'cra-tp-ratio': 1.3,
  'cra-profit-cb': 0.1,
  'cra-tp-method': 'full',
  'cra-mv-tp1': 1.5,
  'cra-mv-dbk1': 30,
  'cra-mv-dbk2': 20,
  'cra-reverse-tp': false,
  'cra-reverse-sl': false,
  'cra-open-ind': 'macd_golden',
  'cra-add-ind': 'macd',
  'cra-trend-ind': false,
  'cra-trend-tf': '15m',
  'cra-waterfall': 2,
  'cra-amp-5m': 2,
  'cra-amp-15m': 4,
  'cra-amp-30m': 7,
  'cra-amp-1h': 10,
  'cra-burn': false,
  'cra-burn-dual': 3,
  'cra-burn-global': 5,
  'cra-follow': false,
  'cra-follow-max': 5,
  'cra-reduce': false,
  'cra-trade-count': 'cycle',
  'cra-online-limit': 10,
  'cra-first-price': 0,
  'cra-kline-5m': false,
  'cra-kline-15m': false,
  'cra-kline-30m': false,
  'cra-kline-1h': false,
  'cra-kline-4h': false,
  'cra-kline-8h': false,
}

export function useCRAConfig() {
  const [config, setConfig] = useState<CRAConfig>(() => {
    const c = { ...CRA_DEFAULTS }
    for (const k of Object.keys(CRA_DEFAULTS) as Array<keyof CRAConfig>) {
      ;(c as Record<keyof CRAConfig, unknown>)[k] = loadLocal(k, CRA_DEFAULTS[k])
    }
    return c
  })

  const update = useCallback(<K extends keyof CRAConfig>(key: K, value: CRAConfig[K]) => {
    setConfig((prev) => {
      const next = { ...prev, [key]: value }
      saveLocal(key, value)
      return next
    })
  }, [])

  return { config, update }
}

export function CRAParamForm({
  config,
  onChange,
}: {
  config: CRAConfig
  onChange: <K extends keyof CRAConfig>(key: K, value: CRAConfig[K]) => void
}) {
  const set = onChange

  return (
    <div className="space-y-5">
      {/* 首单与补仓参数 */}
      <SectionCard title="首单与补仓参数" bodyClassName="space-y-5">
        <div className="grid grid-cols-2 gap-4">
          <div>
            <label className="mb-1.5 block text-xs text-muted-foreground">做单数量（一般5-7单）</label>
            <NumberInput value={config['cra-order-count']} onChange={(v) => set('cra-order-count', v)} min={1} max={20} />
          </div>
          <div>
            <label className="mb-1.5 block text-xs text-muted-foreground">首单仓位 (10-10000 USDT)</label>
            <NumberInput value={config['cra-first-amount']} onChange={(v) => set('cra-first-amount', v)} min={10} max={10000} />
          </div>
        </div>
        <div className="grid grid-cols-2 gap-4">
          <div>
            <label className="mb-1.5 block text-xs text-muted-foreground">补仓价差 (0.5-50%)</label>
            <NumberInput value={config['cra-spread']} onChange={(v) => set('cra-spread', v)} min={0.5} max={50} />
            <p className="text-[10px] text-muted-foreground mt-1">每下跌达到设定百分比自动买入下一单</p>
          </div>
          <div>
            <label className="mb-1.5 block text-xs text-muted-foreground">补仓回调 (0.01-0.5%)</label>
            <NumberInput value={config['cra-callback']} onChange={(v) => set('cra-callback', v)} min={0.01} max={0.5} />
            <p className="text-[10px] text-muted-foreground mt-1">下跌到低点又上涨达到设定值才买入</p>
          </div>
        </div>
        <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
          <div>
            <div className="text-sm font-medium text-foreground">开仓加倍</div>
            <div className="mt-0.5 text-xs text-muted-foreground">首单金额x2，补仓倍数仍按首单金额倍投或等比</div>
          </div>
          <Toggle value={config['cra-open-double']} onChange={(v) => set('cra-open-double', v)} />
        </div>
        <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
          <div>
            <div className="text-sm font-medium text-foreground">关闭补仓</div>
            <div className="mt-0.5 text-xs text-muted-foreground">不执行补仓策略，但会正常止盈</div>
          </div>
          <Toggle value={config['cra-close-add']} onChange={(v) => set('cra-close-add', v)} />
        </div>
      </SectionCard>

      {/* 止盈与止损参数 */}
      <SectionCard title="止盈与止损参数" bodyClassName="space-y-5">
        <div className="grid grid-cols-2 gap-4">
          <div>
            <label className="mb-1.5 block text-xs text-muted-foreground">止盈比例 (%)</label>
            <NumberInput value={config['cra-tp-ratio']} onChange={(v) => set('cra-tp-ratio', v)} min={0.1} max={50} />
          </div>
          <div>
            <label className="mb-1.5 block text-xs text-muted-foreground">盈利回调 (0.01-0.5%)</label>
            <NumberInput value={config['cra-profit-cb']} onChange={(v) => set('cra-profit-cb', v)} min={0.01} max={0.5} />
          </div>
        </div>
        <div>
          <label className="mb-1.5 block text-xs text-muted-foreground">止盈方式</label>
          <div className="grid grid-cols-2 gap-3">
            {TP_METHODS.map((m) => (
              <button
                key={m.key}
                onClick={() => set('cra-tp-method', m.key)}
                className={cn(
                  'p-3 rounded-lg border text-left transition-colors',
                  config['cra-tp-method'] === m.key ? 'bg-quant-gold/10 border-quant-gold/30' : 'border-quant-border bg-quant-bg hover:border-quant-gold/20'
                )}
              >
                <div className="text-xs font-medium">{m.label}</div>
                <div className="text-[10px] text-muted-foreground mt-0.5">{m.desc}</div>
              </button>
            ))}
          </div>
        </div>
        <div className="rounded-lg border border-quant-border bg-quant-bg p-4 space-y-3">
          <div className="text-xs font-semibold">移动止盈档位配置</div>
          <div className="grid grid-cols-3 gap-3">
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">第一档止盈比例 (%)</label>
              <NumberInput value={config['cra-mv-tp1']} onChange={(v) => set('cra-mv-tp1', v)} min={0.1} max={10} />
            </div>
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">第一档回撤 (%)</label>
              <NumberInput value={config['cra-mv-dbk1']} onChange={(v) => set('cra-mv-dbk1', v)} min={5} max={100} />
            </div>
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">第二档回撤 (%)</label>
              <NumberInput value={config['cra-mv-dbk2']} onChange={(v) => set('cra-mv-dbk2', v)} min={5} max={100} />
            </div>
          </div>
          <p className="text-[10px] text-muted-foreground">计算公式: 止盈比例 ± (止盈比例 × 回撤比例)。移动止盈开启后分仓/首尾止盈失效</p>
        </div>
        <div className="grid grid-cols-2 gap-4">
          <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
            <div>
              <div className="text-sm font-medium text-foreground">反向止盈</div>
              <div className="mt-0.5 text-xs text-muted-foreground">MACD反向信号时清仓（适合大周期订单）</div>
            </div>
            <Toggle value={config['cra-reverse-tp']} onChange={(v) => set('cra-reverse-tp', v)} />
          </div>
          <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
            <div>
              <div className="text-sm font-medium text-foreground">反向止损</div>
              <div className="mt-0.5 text-xs text-muted-foreground">MACD判断错误直接止损</div>
            </div>
            <Toggle value={config['cra-reverse-sl']} onChange={(v) => set('cra-reverse-sl', v)} />
          </div>
        </div>
      </SectionCard>

      {/* 开仓与补仓指标 */}
      <SectionCard title="开仓与补仓指标" bodyClassName="space-y-5">
        <div>
          <label className="mb-1.5 block text-xs text-muted-foreground">开仓指标策略</label>
          <select
            value={config['cra-open-ind']}
            onChange={(e) => set('cra-open-ind', e.target.value)}
            className="w-full appearance-none rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none transition-colors focus:border-quant-gold"
          >
            {OPEN_INDICATORS.map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </select>
        </div>
        <div>
          <label className="mb-1.5 block text-xs text-muted-foreground">补仓指标（EMA和MACD补仓）</label>
          <select
            value={config['cra-add-ind']}
            onChange={(e) => set('cra-add-ind', e.target.value)}
            className="w-full appearance-none rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none transition-colors focus:border-quant-gold"
          >
            {ADD_INDICATORS.map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </select>
          <p className="text-[10px] text-muted-foreground mt-1">开启后需同时满足跌幅条件和指标条件才补仓，大行情时非常抗跌</p>
        </div>
        <div className="space-y-3">
          <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
            <div>
              <div className="text-sm font-medium text-foreground">趋势指标 (EMA4)</div>
              <div className="mt-0.5 text-xs text-muted-foreground">监控EMA指数平滑移动平均线，可选5/15/30/60分钟</div>
            </div>
            <Toggle value={config['cra-trend-ind']} onChange={(v) => set('cra-trend-ind', v)} />
          </div>
          {config['cra-trend-ind'] && (
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">EMA4 时间周期</label>
              <select
                value={config['cra-trend-tf']}
                onChange={(e) => set('cra-trend-tf', e.target.value)}
                className="w-full appearance-none rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none transition-colors focus:border-quant-gold"
              >
                {TREND_TFS.map((o) => (
                  <option key={o.value} value={o.value}>{o.label}</option>
                ))}
              </select>
              <p className="text-[10px] text-muted-foreground mt-1">时间越长准确性越高，但也越容易错过行情</p>
            </div>
          )}
        </div>
      </SectionCard>

      {/* 防瀑布与振幅 */}
      <SectionCard title="防瀑布与振幅" bodyClassName="space-y-5">
        <div>
          <label className="mb-1.5 block text-xs text-muted-foreground">防瀑布设定 (%)</label>
          <NumberInput value={config['cra-waterfall']} onChange={(v) => set('cra-waterfall', v)} min={0.5} max={20} />
          <p className="text-[10px] text-muted-foreground mt-1">1分钟内单一币种涨跌超过设定值自动暂停补仓，默认2%</p>
        </div>
        <div className="text-xs font-semibold">振幅建议设置</div>
        <div className="grid grid-cols-4 gap-3">
          {AMP_FIELDS.map((a) => (
            <div key={a.key}>
              <label className="mb-1.5 block text-xs text-muted-foreground">{a.label} (建议{a.suggest}%)</label>
              <NumberInput value={config[a.key as keyof CRAConfig] as number} onChange={(v) => set(a.key as keyof CRAConfig, v as CRAConfig[keyof CRAConfig])} min={0.1} max={50} />
            </div>
          ))}
        </div>
        <p className="text-[10px] text-muted-foreground">连续几根或一根K线连续上涨/下跌产生的价差幅度</p>
      </SectionCard>

      {/* 斩仓燃烧与顺势而为 */}
      <SectionCard title="斩仓燃烧与顺势而为" bodyClassName="space-y-5">
        <div className="space-y-3">
          <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
            <div>
              <div className="text-sm font-medium text-foreground">斩仓和燃烧</div>
              <div className="mt-0.5 text-xs text-muted-foreground">用顺势单盈利消耗逆势单浮亏，顺势单不占用在线单数</div>
            </div>
            <Toggle value={config['cra-burn']} onChange={(v) => set('cra-burn', v)} />
          </div>
          {config['cra-burn'] && (
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">双向燃烧起始仓（默认第3仓）</label>
                <NumberInput value={config['cra-burn-dual']} onChange={(v) => set('cra-burn-dual', v)} min={1} max={10} />
              </div>
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">全局燃烧起始仓（默认第5仓）</label>
                <NumberInput value={config['cra-burn-global']} onChange={(v) => set('cra-burn-global', v)} min={1} max={10} />
              </div>
            </div>
          )}
        </div>
        <div className="space-y-3">
          <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
            <div>
              <div className="text-sm font-medium text-foreground">顺势而为</div>
              <div className="mt-0.5 text-xs text-muted-foreground">逆势单补仓后顺势单倍投开仓，最高放大5倍</div>
            </div>
            <Toggle value={config['cra-follow']} onChange={(v) => set('cra-follow', v)} />
          </div>
          {config['cra-follow'] && (
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">顺势最大倍数（逆势补仓次数+首单，最高5倍）</label>
              <NumberInput value={config['cra-follow-max']} onChange={(v) => set('cra-follow-max', v)} min={1} max={5} />
              <p className="text-[10px] text-muted-foreground mt-1">顺势首单金额 = 逆势单补仓次数 + 首单倍率 × 首单金额</p>
            </div>
          )}
        </div>
        <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
          <div>
            <div className="text-sm font-medium text-foreground">自定义减仓</div>
            <div className="mt-0.5 text-xs text-muted-foreground">极端行情下手动止损部分仓位，最后一仓占比50%，倒数第二25%，依次类推</div>
          </div>
          <Toggle value={config['cra-reduce']} onChange={(v) => set('cra-reduce', v)} />
        </div>
      </SectionCard>

      {/* 交易次数与在线限制 */}
      <SectionCard title="交易次数与在线限制" bodyClassName="space-y-5">
        <div>
          <label className="mb-1.5 block text-xs text-muted-foreground">交易次数模式</label>
          <div className="flex gap-3">
            {TRADE_COUNT_MODES.map((m) => (
              <button
                key={m.key}
                onClick={() => set('cra-trade-count', m.key)}
                className={cn(
                  'flex-1 p-3 rounded-lg border text-left transition-colors',
                  config['cra-trade-count'] === m.key ? 'bg-quant-gold/10 border-quant-gold/30' : 'border-quant-border bg-quant-bg hover:border-quant-gold/20'
                )}
              >
                <div className="text-xs font-medium">{m.label}</div>
                <div className="text-[10px] text-muted-foreground mt-0.5">{m.desc}</div>
              </button>
            ))}
          </div>
        </div>
        <div>
          <label className="mb-1.5 block text-xs text-muted-foreground">限制在线单量</label>
          <NumberInput value={config['cra-online-limit']} onChange={(v) => set('cra-online-limit', v)} min={1} max={50} />
          <p className="text-[10px] text-muted-foreground mt-1">控制趋势开仓后进场的交易对过多，包括多单和空单数量</p>
        </div>
        <div>
          <label className="mb-1.5 block text-xs text-muted-foreground">首单挂单价格 (0=实时市价)</label>
          <NumberInput value={config['cra-first-price']} onChange={(v) => set('cra-first-price', v)} min={0} max={1000000} />
          <p className="text-[10px] text-muted-foreground mt-1">输入固定价格后，只有最新价格达到设定值系统才会市价买入</p>
        </div>
      </SectionCard>

      {/* 监控K线配置 */}
      <SectionCard title="监控K线配置" bodyClassName="space-y-5">
        <div className="grid grid-cols-3 gap-3">
          {KLINE_FIELDS.map((k) => (
            <button
              key={k.key}
              onClick={() => set(k.key as keyof CRAConfig, !config[k.key as keyof CRAConfig] as CRAConfig[keyof CRAConfig])}
              className={cn(
                'p-3 rounded-lg border text-center transition-colors',
                config[k.key as keyof CRAConfig] ? 'bg-quant-gold/10 border-quant-gold/30' : 'border-quant-border bg-quant-bg hover:border-quant-gold/20'
              )}
            >
              <div className="text-xs font-medium">{k.label}</div>
              <div className="text-[10px] text-muted-foreground">{k.desc}</div>
            </button>
          ))}
        </div>
        <p className="text-[10px] text-muted-foreground">监控币安交易所MACD和EMA指标，建议开启适合交易周期的K线</p>
      </SectionCard>
    </div>
  )
}
