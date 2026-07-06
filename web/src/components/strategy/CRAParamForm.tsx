import { useMemo, useState } from 'react'
import { cn } from '@/lib/utils'
import { Settings2, TrendingUp } from 'lucide-react'
import type { AddPositionItem, MovingTPTier } from '@/types'
import { AddPositionModal } from './AddPositionModal'
import { MovingTPModal } from './MovingTPModal'

export type MarketType = 'spot' | 'contract'

export interface CRAParams {
  // ── 开仓设置 ──
  firstOrderPrice: number
  firstOrderAmount: number
  firstOrderMultiplier: number
  tradeCountMode: 'single' | 'cycle'
  loopCount: number

  // ── 补仓设置 ──
  enableAddPosition: boolean
  orderCount: number
  addPositions: AddPositionItem[]

  // ── 止盈设置 ──
  tpMethod: 'full' | 'tail' | 'head_tail'
  tpMode: 'static' | 'moving'
  tpRatio: number
  profitCallback: number
  movingTPTiers: MovingTPTier[]

  // ── 合约开仓指标 ──
  openMacdEnabled: boolean
  openMacdPeriod: 'close' | '5m' | '15m'
  openCounterEmaEnabled: boolean
  openCounterEmaPeriod: 'close' | '5m' | '15m'
  openTrendEmaEnabled: boolean
  openTrendEmaPeriod: 'close' | '5m' | '15m'

  // ── 合约补仓指标 ──
  addMacdEnabled: boolean
  addMacdPeriod: 'close' | '5m' | '15m'
  addEmaEnabled: boolean
  addEmaPeriod: 'close' | '5m' | '15m'

  // ── 风控 ──
  waterfallEnabled: boolean
  waterfall: number

  // ── 合约止损 ──
  stopLossEnabled: boolean
  stopLossType: 'ratio' | 'amount' | 'price'
  stopLossRatio: number
  stopLossAmount: number
  stopLossPrice: number

  // ── 合约反向止盈/止损 ──
  reverseTP: 'close' | '5m' | '15m'
  reverseSL: boolean

  // ── 合约燃烧 ──
  burnGlobalEnabled: boolean
  burnGlobalThreshold: number
  burnDualEnabled: boolean
  burnDualThreshold: number

  // ── 其他 ──
  openDouble: boolean
  followTrend: boolean
  onlineOrderLimit: number
  leverage: number
  direction: 'long' | 'short' | 'dual'
}

export const DEFAULT_CRA_PARAMS: CRAParams = {
  firstOrderPrice: 0,
  firstOrderAmount: 10,
  firstOrderMultiplier: 1,
  tradeCountMode: 'single',
  loopCount: 100,
  enableAddPosition: true,
  orderCount: 7,
  addPositions: [],
  tpMethod: 'full',
  tpMode: 'static',
  tpRatio: 1.3,
  profitCallback: 0.1,
  movingTPTiers: [
    { ratio: 2, drawback: 20 },
    { ratio: 3, drawback: 20 },
    { ratio: 4, drawback: 10 },
    { ratio: 5, drawback: 10 },
  ],
  openMacdEnabled: false,
  openMacdPeriod: 'close',
  openCounterEmaEnabled: false,
  openCounterEmaPeriod: '15m',
  openTrendEmaEnabled: false,
  openTrendEmaPeriod: '15m',
  addMacdEnabled: false,
  addMacdPeriod: 'close',
  addEmaEnabled: false,
  addEmaPeriod: '15m',
  waterfallEnabled: true,
  waterfall: 2,
  stopLossEnabled: false,
  stopLossType: 'ratio',
  stopLossRatio: 0,
  stopLossAmount: 0,
  stopLossPrice: 0,
  reverseTP: 'close',
  reverseSL: false,
  burnGlobalEnabled: false,
  burnGlobalThreshold: 5,
  burnDualEnabled: false,
  burnDualThreshold: 3,
  openDouble: false,
  followTrend: false,
  onlineOrderLimit: 10,
  leverage: 1,
  direction: 'long',
}

/** 为 Settings 页面提供的简化默认参数 */
export const DEFAULT_CRA_SETTINGS: CRAParams = { ...DEFAULT_CRA_PARAMS }

/* ─── useCRAConfig hook ─────────────────────────────────────────────── */
export function useCRAConfig() {
  const getStored = (): CRAParams | null => {
    try {
      const raw = localStorage.getItem('xt-cra-config')
      return raw ? JSON.parse(raw) : null
    } catch {
      return null
    }
  }

  const setStored = (next: CRAParams) => {
    localStorage.setItem('xt-cra-config', JSON.stringify(next))
  }

  return { getStored, setStored }
}

const inputCls =
  'w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold'

interface SectionProps {
  title: string
  children: React.ReactNode
}

function Section({ title, children }: SectionProps) {
  return (
    <div className="rounded-xl border border-quant-border bg-quant-card p-4 space-y-4">
      <div className="text-xs font-semibold text-quant-gold">{title}</div>
      {children}
    </div>
  )
}

interface PeriodSelectProps {
  label: string
  enabled: boolean
  period: 'close' | '5m' | '15m'
  onToggle: (v: boolean) => void
  onPeriodChange: (v: 'close' | '5m' | '15m') => void
}

function PeriodSelect({ label, enabled, period, onToggle, onPeriodChange }: PeriodSelectProps) {
  return (
    <div className="flex items-center gap-3">
      <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
        <input type="checkbox" checked={enabled} onChange={(e) => onToggle(e.target.checked)} className="rounded" />
        <span className={enabled ? 'text-foreground' : ''}>{label}</span>
      </label>
      {enabled && (
        <select
          value={period}
          onChange={(e) => onPeriodChange(e.target.value as 'close' | '5m' | '15m')}
          className="bg-quant-bg border border-quant-border rounded px-2 py-1 text-xs focus:outline-none focus:border-quant-gold"
        >
          <option value="close">关闭</option>
          <option value="5m">5 分钟</option>
          <option value="15m">15 分钟</option>
        </select>
      )}
    </div>
  )
}

interface CRAParamFormProps {
  value: CRAParams
  onChange: (next: CRAParams) => void
  market: MarketType
  className?: string
}

export function CRAParamForm({ value, onChange, market, className }: CRAParamFormProps) {
  const [showAddPositionModal, setShowAddPositionModal] = useState(false)
  const [showMovingTPModal, setShowMovingTPModal] = useState(false)

  const update = <K extends keyof CRAParams>(key: K, val: CRAParams[K]) => {
    onChange({ ...value, [key]: val })
  }

  const totalAddPosition = useMemo(() => {
    const first = value.firstOrderAmount * value.firstOrderMultiplier
    return value.addPositions.reduce((sum, pos) => sum + first * pos.multiplier, first)
  }, [value.firstOrderAmount, value.firstOrderMultiplier, value.addPositions])

  const isContract = market === 'contract'

  return (
    <div className={cn('space-y-4', className)}>
      {/* ── 开仓设置 ── */}
      <Section title="开仓设置">
        <div className="grid grid-cols-2 gap-4">
          <div>
            <label className="text-[11px] text-muted-foreground mb-1.5 block">首单额度 (USDT)</label>
            <input
              type="number"
              min={10}
              max={10000}
              step={10}
              value={value.firstOrderAmount}
              onChange={(e) => update('firstOrderAmount', Number(e.target.value))}
              className={inputCls}
            />
          </div>
          {isContract && (
            <div>
              <label className="text-[11px] text-muted-foreground mb-1.5 block">杠杆倍数</label>
              <input
                type="number"
                min={1}
                max={125}
                value={value.leverage}
                onChange={(e) => update('leverage', Number(e.target.value))}
                className={inputCls}
              />
            </div>
          )}
        </div>

        {isContract && (
          <div>
            <label className="text-[11px] text-muted-foreground mb-1.5 block">交易方向</label>
            <div className="flex gap-1 rounded-lg border border-quant-border overflow-hidden">
              {(['long', 'short', 'dual'] as const).map((d) => (
                <button
                  key={d}
                  type="button"
                  onClick={() => update('direction', d)}
                  className={cn(
                    'flex-1 py-2 text-xs font-medium transition-colors',
                    value.direction === d
                      ? 'bg-quant-gold/10 text-quant-gold'
                      : 'text-muted-foreground hover:text-foreground'
                  )}
                >
                  {d === 'long' ? '多' : d === 'short' ? '空' : '双向'}
                </button>
              ))}
            </div>
          </div>
        )}

        <div className="grid grid-cols-2 gap-4">
          <div>
            <label className="text-[11px] text-muted-foreground mb-1.5 block">首单加倍</label>
            <input
              type="number"
              min={1}
              max={10}
              step={0.1}
              value={value.firstOrderMultiplier}
              onChange={(e) => update('firstOrderMultiplier', Number(e.target.value))}
              className={inputCls}
            />
          </div>
          <div>
            <label className="text-[11px] text-muted-foreground mb-1.5 block">循环次数</label>
            <input
              type="number"
              min={1}
              value={value.loopCount}
              onChange={(e) => update('loopCount', Number(e.target.value))}
              className={inputCls}
            />
          </div>
        </div>

        <div>
          <label className="text-[11px] text-muted-foreground mb-1.5 block">循环类型</label>
          <div className="flex gap-2">
            {(
              [
                { key: 'single', label: '单次策略' },
                { key: 'cycle', label: '循环策略' },
              ] as const
            ).map((m) => (
              <button
                key={m.key}
                type="button"
                onClick={() => update('tradeCountMode', m.key)}
                className={cn(
                  'flex-1 py-2 rounded-lg text-xs border transition-colors',
                  value.tradeCountMode === m.key
                    ? 'bg-quant-gold/10 border-quant-gold/20 text-quant-gold'
                    : 'border-quant-border text-muted-foreground hover:text-foreground'
                )}
              >
                {m.label}
              </button>
            ))}
          </div>
        </div>

        {isContract && (
          <div className="space-y-2">
            <div className="text-[11px] text-muted-foreground">开仓指标</div>
            <div className="space-y-2">
              <PeriodSelect
                label="开仓 MACD 监测"
                enabled={value.openMacdEnabled}
                period={value.openMacdPeriod}
                onToggle={(v) => update('openMacdEnabled', v)}
                onPeriodChange={(v) => update('openMacdPeriod', v)}
              />
              <PeriodSelect
                label="逆势 EMA 监测"
                enabled={value.openCounterEmaEnabled}
                period={value.openCounterEmaPeriod}
                onToggle={(v) => update('openCounterEmaEnabled', v)}
                onPeriodChange={(v) => update('openCounterEmaPeriod', v)}
              />
              <PeriodSelect
                label="顺势 EMA 监测"
                enabled={value.openTrendEmaEnabled}
                period={value.openTrendEmaPeriod}
                onToggle={(v) => update('openTrendEmaEnabled', v)}
                onPeriodChange={(v) => update('openTrendEmaPeriod', v)}
              />
            </div>
          </div>
        )}

        <div className="flex flex-wrap gap-3">
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            <input
              type="checkbox"
              checked={value.openDouble}
              onChange={(e) => update('openDouble', e.target.checked)}
              className="rounded"
            />
            开仓加倍
          </label>
          {isContract && (
            <label className="flex items-center gap-2 text-xs text-muted-foreground">
              <input
                type="checkbox"
                checked={value.followTrend}
                onChange={(e) => update('followTrend', e.target.checked)}
                className="rounded"
              />
              顺势而为
            </label>
          )}
        </div>
      </Section>

      {/* ── 补仓设置 ── */}
      <Section title="补仓设置">
        <div className="flex items-center gap-4">
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            <input
              type="checkbox"
              checked={value.enableAddPosition}
              onChange={(e) => update('enableAddPosition', e.target.checked)}
              className="rounded"
            />
            开启补仓
          </label>
          <div className="flex-1 grid grid-cols-2 gap-4">
            <div>
              <label className="text-[11px] text-muted-foreground mb-1.5 block">补仓次数</label>
              <input
                type="number"
                min={0}
                max={20}
                value={value.orderCount}
                onChange={(e) => update('orderCount', Number(e.target.value))}
                disabled={!value.enableAddPosition}
                className={cn(inputCls, !value.enableAddPosition && 'opacity-40')}
              />
            </div>
            <div>
              <label className="text-[11px] text-muted-foreground mb-1.5 block">补仓总金额 (USDT)</label>
              <div
                className={cn(
                  'w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs',
                  !value.enableAddPosition && 'opacity-40'
                )}
              >
                {totalAddPosition.toFixed(2)}
              </div>
            </div>
          </div>
        </div>

        <div>
          <button
            type="button"
            disabled={!value.enableAddPosition}
            onClick={() => setShowAddPositionModal(true)}
            className={cn(
              'w-full flex items-center justify-center gap-2 py-2.5 rounded-lg border text-xs transition-colors',
              value.enableAddPosition
                ? 'border-quant-gold/30 text-quant-gold hover:bg-quant-gold/5'
                : 'border-quant-border text-muted-foreground opacity-40 cursor-not-allowed'
            )}
          >
            <Settings2 className="w-3.5 h-3.5" />
            配置补仓参数
          </button>
          <AddPositionModal
            open={showAddPositionModal}
            onClose={() => setShowAddPositionModal(false)}
            value={value.addPositions}
            onChange={(next) => update('addPositions', next)}
            showEma={isContract}
            disabled={!value.enableAddPosition}
          />
        </div>

        {isContract && (
          <div className="space-y-2">
            <div className="text-[11px] text-muted-foreground">补仓指标</div>
            <div className="space-y-2">
              <PeriodSelect
                label="补仓 MACD 监测"
                enabled={value.addMacdEnabled}
                period={value.addMacdPeriod}
                onToggle={(v) => update('addMacdEnabled', v)}
                onPeriodChange={(v) => update('addMacdPeriod', v)}
              />
              <PeriodSelect
                label="补仓 EMA 监测"
                enabled={value.addEmaEnabled}
                period={value.addEmaPeriod}
                onToggle={(v) => update('addEmaEnabled', v)}
                onPeriodChange={(v) => update('addEmaPeriod', v)}
              />
            </div>
          </div>
        )}
      </Section>

      {/* ── 风控设置 ── */}
      <Section title="风控设置">
        <div className="flex items-center gap-4">
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            <input
              type="checkbox"
              checked={value.waterfallEnabled}
              onChange={(e) => update('waterfallEnabled', e.target.checked)}
              className="rounded"
            />
            开启防瀑布
          </label>
          <div className="flex-1">
            <label className="text-[11px] text-muted-foreground mb-1.5 block">防瀑布比例 (%)</label>
            <input
              type="number"
              min={0.5}
              max={20}
              step={0.5}
              value={value.waterfall}
              onChange={(e) => update('waterfall', Number(e.target.value))}
              disabled={!value.waterfallEnabled}
              className={cn(inputCls, !value.waterfallEnabled && 'opacity-40')}
            />
          </div>
        </div>
      </Section>

      {/* ── 止盈设置 ── */}
      <Section title="止盈设置">
        <div>
          <label className="text-[11px] text-muted-foreground mb-1.5 block">止盈方式</label>
          <div className="flex gap-2">
            {(
              [
                { key: 'full', label: '全仓止盈' },
                { key: 'tail', label: '尾单止盈' },
                { key: 'head_tail', label: '首尾止盈' },
              ] as const
            ).map((m) => (
              <button
                key={m.key}
                type="button"
                onClick={() => update('tpMethod', m.key)}
                className={cn(
                  'flex-1 py-2 rounded-lg text-xs border transition-colors',
                  value.tpMethod === m.key
                    ? 'bg-quant-gold/10 border-quant-gold/20 text-quant-gold'
                    : 'border-quant-border text-muted-foreground hover:text-foreground'
                )}
              >
                {m.label}
              </button>
            ))}
          </div>
        </div>

        <div>
          <label className="text-[11px] text-muted-foreground mb-1.5 block">止盈模式</label>
          <div className="flex gap-2">
            {(
              [
                { key: 'static', label: '静态止盈' },
                { key: 'moving', label: '移动止盈' },
              ] as const
            ).map((m) => (
              <button
                key={m.key}
                type="button"
                onClick={() => update('tpMode', m.key)}
                className={cn(
                  'flex-1 py-2 rounded-lg text-xs border transition-colors',
                  value.tpMode === m.key
                    ? 'bg-quant-gold/10 border-quant-gold/20 text-quant-gold'
                    : 'border-quant-border text-muted-foreground hover:text-foreground'
                )}
              >
                {m.label}
              </button>
            ))}
          </div>
        </div>

        {value.tpMode === 'static' && (
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="text-[11px] text-muted-foreground mb-1.5 block">止盈比例 (%)</label>
              <input
                type="number"
                min={0.1}
                max={50}
                step={0.1}
                value={value.tpRatio}
                onChange={(e) => update('tpRatio', Number(e.target.value))}
                className={inputCls}
              />
            </div>
            <div>
              <label className="text-[11px] text-muted-foreground mb-1.5 block">盈利回调 (%)</label>
              <input
                type="number"
                min={0.01}
                max={0.5}
                step={0.01}
                value={value.profitCallback}
                onChange={(e) => update('profitCallback', Number(e.target.value))}
                className={inputCls}
              />
            </div>
          </div>
        )}

        {value.tpMode === 'moving' && (
          <div>
            <button
              type="button"
              onClick={() => setShowMovingTPModal(true)}
              className="w-full flex items-center justify-center gap-2 py-2.5 rounded-lg border border-quant-gold/30 text-quant-gold text-xs hover:bg-quant-gold/5 transition-colors"
            >
              <TrendingUp className="w-3.5 h-3.5" />
              配置移动止盈止损参数
            </button>
            <MovingTPModal
              open={showMovingTPModal}
              onClose={() => setShowMovingTPModal(false)}
              value={value.movingTPTiers}
              onChange={(next) => update('movingTPTiers', next)}
            />
          </div>
        )}

        {isContract && (
          <div className="space-y-3">
            <div className="flex items-center gap-3">
              <label className="text-[11px] text-muted-foreground">反向止盈</label>
              <select
                value={value.reverseTP}
                onChange={(e) => update('reverseTP', e.target.value as 'close' | '5m' | '15m')}
                className="bg-quant-bg border border-quant-border rounded px-2 py-1 text-xs focus:outline-none focus:border-quant-gold"
              >
                <option value="close">关闭</option>
                <option value="5m">5 分钟</option>
                <option value="15m">15 分钟</option>
              </select>
            </div>
            <label className="flex items-center gap-2 text-xs text-muted-foreground">
              <input
                type="checkbox"
                checked={value.reverseSL}
                onChange={(e) => update('reverseSL', e.target.checked)}
                className="rounded"
              />
              反向止损
            </label>
          </div>
        )}
      </Section>

      {/* ── 止损设置 (合约) ── */}
      {isContract && (
        <Section title="止损设置">
          <label className="flex items-center gap-2 text-xs text-muted-foreground mb-3">
            <input
              type="checkbox"
              checked={value.stopLossEnabled}
              onChange={(e) => update('stopLossEnabled', e.target.checked)}
              className="rounded"
            />
            开启止损
          </label>

          <div className="space-y-4">
            <div>
              <label className="text-[11px] text-muted-foreground mb-1.5 block">止损类型</label>
              <div className="flex gap-1 rounded-lg border border-quant-border overflow-hidden">
                {(['ratio', 'amount', 'price'] as const).map((t) => (
                  <button
                    key={t}
                    type="button"
                    onClick={() => update('stopLossType', t)}
                    disabled={!value.stopLossEnabled}
                    className={cn(
                      'flex-1 py-2 text-xs font-medium transition-colors',
                      value.stopLossType === t
                        ? 'bg-quant-gold/10 text-quant-gold'
                        : 'text-muted-foreground hover:text-foreground',
                      !value.stopLossEnabled && 'opacity-40'
                    )}
                  >
                    {t === 'ratio' ? '比例止损' : t === 'amount' ? '金额止损' : '价格止损'}
                  </button>
                ))}
              </div>
            </div>

            <div className="grid grid-cols-2 gap-4">
              {value.stopLossType === 'ratio' && (
                <div>
                  <label className="text-[11px] text-muted-foreground mb-1.5 block">止损比例 (%)</label>
                  <input
                    type="number"
                    min={0}
                    max={100}
                    step={0.1}
                    value={value.stopLossRatio}
                    onChange={(e) => update('stopLossRatio', Number(e.target.value))}
                    disabled={!value.stopLossEnabled}
                    className={cn(inputCls, !value.stopLossEnabled && 'opacity-40')}
                  />
                </div>
              )}
              {value.stopLossType === 'amount' && (
                <div>
                  <label className="text-[11px] text-muted-foreground mb-1.5 block">止损金额 (USDT)</label>
                  <input
                    type="number"
                    min={0}
                    value={value.stopLossAmount}
                    onChange={(e) => update('stopLossAmount', Number(e.target.value))}
                    disabled={!value.stopLossEnabled}
                    className={cn(inputCls, !value.stopLossEnabled && 'opacity-40')}
                  />
                </div>
              )}
              {value.stopLossType === 'price' && (
                <div>
                  <label className="text-[11px] text-muted-foreground mb-1.5 block">止损价格 (USDT)</label>
                  <input
                    type="number"
                    min={0}
                    value={value.stopLossPrice}
                    onChange={(e) => update('stopLossPrice', Number(e.target.value))}
                    disabled={!value.stopLossEnabled}
                    className={cn(inputCls, !value.stopLossEnabled && 'opacity-40')}
                  />
                </div>
              )}
            </div>
          </div>
        </Section>
      )}

      {/* ── 燃烧设置 (合约) ── */}
      {isContract && (
        <Section title="燃烧设置">
          <div className="space-y-4">
            <div className="flex items-center gap-4">
              <label className="flex items-center gap-2 text-xs text-muted-foreground">
                <input
                  type="checkbox"
                  checked={value.burnGlobalEnabled}
                  onChange={(e) => update('burnGlobalEnabled', e.target.checked)}
                  className="rounded"
                />
                全局燃烧
              </label>
              <div className="flex-1">
                <label className="text-[11px] text-muted-foreground mb-1.5 block">触发补仓次数</label>
                <input
                  type="number"
                  min={1}
                  max={20}
                  value={value.burnGlobalThreshold}
                  onChange={(e) => update('burnGlobalThreshold', Number(e.target.value))}
                  disabled={!value.burnGlobalEnabled}
                  className={cn(inputCls, !value.burnGlobalEnabled && 'opacity-40')}
                />
              </div>
            </div>

            <div className="flex items-center gap-4">
              <label className="flex items-center gap-2 text-xs text-muted-foreground">
                <input
                  type="checkbox"
                  checked={value.burnDualEnabled}
                  onChange={(e) => update('burnDualEnabled', e.target.checked)}
                  className="rounded"
                />
                对向燃烧
              </label>
              <div className="flex-1">
                <label className="text-[11px] text-muted-foreground mb-1.5 block">触发补仓次数</label>
                <input
                  type="number"
                  min={1}
                  max={20}
                  value={value.burnDualThreshold}
                  onChange={(e) => update('burnDualThreshold', Number(e.target.value))}
                  disabled={!value.burnDualEnabled}
                  className={cn(inputCls, !value.burnDualEnabled && 'opacity-40')}
                />
              </div>
            </div>
          </div>
        </Section>
      )}
    </div>
  )
}

interface ApiPayload {
  first_order_price: number
  first_order_amount: number
  first_order_multiplier: number
  trade_count_mode: 'single' | 'cycle'
  loop_count: number
  enable_add_position: boolean
  order_count: number
  add_positions: AddPositionItem[]
  take_profit_method: 'full' | 'tail' | 'head_tail'
  tp_mode: 'static' | 'moving'
  take_profit_ratio: number
  profit_callback: number
  moving_take_profit_tiers: MovingTPTier[]
  open_macd_enabled: boolean
  open_macd_period: 'close' | '5m' | '15m'
  open_counter_ema_enabled: boolean
  open_counter_ema_period: 'close' | '5m' | '15m'
  open_trend_ema_enabled: boolean
  open_trend_ema_period: 'close' | '5m' | '15m'
  add_macd_enabled: boolean
  add_macd_period: 'close' | '5m' | '15m'
  add_ema_enabled: boolean
  add_ema_period: 'close' | '5m' | '15m'
  waterfall_enabled: boolean
  waterfall_protection: number
  stop_loss_enabled: boolean
  stop_loss_type: 'ratio' | 'amount' | 'price'
  stop_loss_ratio: number
  stop_loss_amount: number
  stop_loss_price: number
  reverse_take_profit_period: 'close' | '5m' | '15m'
  reverse_stop_loss: boolean
  burn_global_enabled: boolean
  burn_global_threshold: number
  burn_dual_enabled: boolean
  burn_dual_threshold: number
  open_double: boolean
  follow_trend: boolean
  online_order_limit: number
  leverage: number
  direction: 'long' | 'short' | 'dual'
}

/** 将 CRAParams 转换为后端 API 所需的 snake_case 参数对象 */
export function craParamsToApiPayload(p: CRAParams): ApiPayload {
  return {
    first_order_price: p.firstOrderPrice,
    first_order_amount: p.firstOrderAmount,
    first_order_multiplier: p.firstOrderMultiplier,
    trade_count_mode: p.tradeCountMode,
    loop_count: p.loopCount,
    enable_add_position: p.enableAddPosition,
    order_count: p.orderCount,
    add_positions: p.addPositions,
    take_profit_method: p.tpMethod,
    tp_mode: p.tpMode,
    take_profit_ratio: p.tpRatio,
    profit_callback: p.profitCallback,
    moving_take_profit_tiers: p.movingTPTiers,
    open_macd_enabled: p.openMacdEnabled,
    open_macd_period: p.openMacdPeriod,
    open_counter_ema_enabled: p.openCounterEmaEnabled,
    open_counter_ema_period: p.openCounterEmaPeriod,
    open_trend_ema_enabled: p.openTrendEmaEnabled,
    open_trend_ema_period: p.openTrendEmaPeriod,
    add_macd_enabled: p.addMacdEnabled,
    add_macd_period: p.addMacdPeriod,
    add_ema_enabled: p.addEmaEnabled,
    add_ema_period: p.addEmaPeriod,
    waterfall_enabled: p.waterfallEnabled,
    waterfall_protection: p.waterfall,
    stop_loss_enabled: p.stopLossEnabled,
    stop_loss_type: p.stopLossType,
    stop_loss_ratio: p.stopLossRatio,
    stop_loss_amount: p.stopLossAmount,
    stop_loss_price: p.stopLossPrice,
    reverse_take_profit_period: p.reverseTP,
    reverse_stop_loss: p.reverseSL,
    burn_global_enabled: p.burnGlobalEnabled,
    burn_global_threshold: p.burnGlobalThreshold,
    burn_dual_enabled: p.burnDualEnabled,
    burn_dual_threshold: p.burnDualThreshold,
    open_double: p.openDouble,
    follow_trend: p.followTrend,
    online_order_limit: p.onlineOrderLimit,
    leverage: p.leverage,
    direction: p.direction,
  }
}
