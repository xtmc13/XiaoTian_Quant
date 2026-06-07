import React from 'react'
import { cn } from '@/lib/utils'
import { CRAParamForm, DEFAULT_CRA_PARAMS, type CRAParams } from '@/components/strategy/CRAParamForm'

export function WizardField({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: React.ReactNode
}) {
  return (
    <div>
      <div className="mb-1.5 flex items-center gap-2">
        <label className="text-xs font-medium text-[#aaaaaa]">{label}</label>
        {hint && <span className="text-[11px] text-[#8a8a8a]">{hint}</span>}
      </div>
      {children}
    </div>
  )
}

interface BotParamFormProps {
  form: Record<string, unknown>
  setForm: React.Dispatch<React.SetStateAction<Record<string, unknown>>>
  effectiveType: string
}

/* ─── CRA↔ snake_case form conversion ─────────────────────────────── */
function formToCra(f: Record<string, unknown>): CRAParams {
  return {
    orderCount: (f.order_count as number) ?? 7,
    firstOrderAmount: (f.first_order_amount as number) ?? 100,
    addPosSpread: (f.add_position_spread as number) ?? 3,
    addPosCallback: (f.add_position_callback as number) ?? 0.1,
    tpRatio: (f.take_profit_ratio as number) ?? 1.3,
    profitCallback: (f.profit_callback as number) ?? 0.1,
    tpMethod: ((f.take_profit_method as string) ?? 'full') as CRAParams['tpMethod'],
    openInd: (f.open_indicator as string) ?? 'macd_golden',
    addInd: (f.add_position_indicator as string) ?? 'macd',
    waterfall: (f.waterfall_protection as number) ?? 2,
    openDouble: !!f.open_double,
    trendInd: !!f.trend_indicator,
    trendTf: (f.trend_timeframe as string) ?? '15m',
    followTrend: !!f.follow_trend,
    burnCut: !!f.burn_cut,
    closeAddPos: !!f.close_add_position,
    leverage: (f.leverage as number) ?? 5,
    direction: ((f.direction as string) ?? 'long') as CRAParams['direction'],
    tradeCountMode: ((f.trade_count_mode as string) ?? 'cycle') as CRAParams['tradeCountMode'],
    reverseTP: !!f.reverse_take_profit,
    reverseSL: !!f.reverse_stop_loss,
    customReduce: !!f.custom_reduce,
    onlineOrderLimit: (f.online_order_limit as number) ?? 10,
    profitProtection: !!f.profit_protection,
    stopLossRatio: (f.stop_loss_ratio as number) ?? 0,
    stopLossAmount: (f.stop_loss_amount as number) ?? 0,
    stopLossPrice: (f.stop_loss_price as number) ?? 0,
    firstOrderPrice: (f.first_order_price as number) ?? 0,
    addPosMultiple: (f.add_position_multiple as number) ?? 1,
    movingTP: f.moving_take_profit as CRAParams['movingTP'],
    amplitude: f.amplitude as CRAParams['amplitude'],
    burnCutExtra: f.burn_cut as CRAParams['burnCutExtra'],
    followTrendMax: (f.follow_trend_max as number) ?? 5,
  }
}

function craToForm(cra: CRAParams, f: Record<string, unknown>): Record<string, unknown> {
  return {
    ...f,
    order_count: cra.orderCount,
    first_order_amount: cra.firstOrderAmount,
    add_position_spread: cra.addPosSpread,
    add_position_callback: cra.addPosCallback,
    take_profit_ratio: cra.tpRatio,
    profit_callback: cra.profitCallback,
    take_profit_method: cra.tpMethod,
    open_indicator: cra.openInd,
    add_position_indicator: cra.addInd,
    waterfall_protection: cra.waterfall,
    open_double: cra.openDouble,
    trend_indicator: cra.trendInd,
    trend_timeframe: cra.trendTf,
    follow_trend: cra.followTrend,
    burn_cut: cra.burnCut,
    close_add_position: cra.closeAddPos,
    leverage: cra.leverage,
    direction: cra.direction,
    trade_count_mode: cra.tradeCountMode,
    reverse_take_profit: cra.reverseTP ?? false,
    reverse_stop_loss: cra.reverseSL ?? false,
    custom_reduce: cra.customReduce ?? false,
    online_order_limit: cra.onlineOrderLimit ?? 10,
    profit_protection: cra.profitProtection ?? false,
    stop_loss_ratio: cra.stopLossRatio ?? 0,
    stop_loss_amount: cra.stopLossAmount ?? 0,
    stop_loss_price: cra.stopLossPrice ?? 0,
    first_order_price: cra.firstOrderPrice ?? 0,
    add_position_multiple: cra.addPosMultiple ?? 1,
  }
}

export function BotParamForm({ form, setForm, effectiveType }: BotParamFormProps) {
  const isCraType = ['trend', 'martin_trend', 'wallstreet', 'macd_golden', 'macd_death', 'dual_burn', 'ema_follow', 'ema_counter'].includes(effectiveType)

  const handleCraChange = (cra: CRAParams) => {
    setForm(craToForm(cra, form))
  }

  return (
    <div className="space-y-4">
      {/* -- 基础策略参数 -- */}
      <div className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-4 space-y-4">
        <div className="text-xs font-semibold text-white">基础策略参数</div>
        <div className="grid grid-cols-2 gap-3">
          <WizardField label="K线周期">
            <select
              value={(form.timeframe as string) || '1h'}
              onChange={(e) => setForm((f) => ({ ...f, timeframe: e.target.value }))}
              className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#4f6ed1]/40"
            >
              <option value="1m">1分钟</option>
              <option value="5m">5分钟</option>
              <option value="15m">15分钟</option>
              <option value="30m">30分钟</option>
              <option value="1h">1小时</option>
              <option value="4h">4小时</option>
              <option value="1d">1天</option>
            </select>
          </WizardField>
          <WizardField label="杠杆">
            <input
              type="number"
              min={1}
              max={125}
              value={(form.leverage as number) || 10}
              onChange={(e) => setForm((f) => ({ ...f, leverage: Number(e.target.value) }))}
              className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#4f6ed1]/40"
            />
          </WizardField>
        </div>
        <WizardField label="方向">
          <div className="flex gap-2">
            {[
              { key: 'long', label: '做多' },
              { key: 'short', label: '做空' },
              { key: 'dual', label: '双向' },
            ].map((d) => (
              <button
                key={d.key}
                onClick={() => setForm((f) => ({ ...f, direction: d.key }))}
                className={cn(
                  'flex-1 rounded-lg border px-3 py-2 text-xs font-medium transition-colors',
                  (form.direction as string) === d.key
                    ? 'border-white/20 bg-white/10 text-white'
                    : 'border-[#1c1c1c] bg-[#141414] text-[#999999] hover:text-[#888888]'
                )}
              >
                {d.label}
              </button>
            ))}
          </div>
        </WizardField>
      </div>

      {/* -- CRA 参数配置 (shared component) -- */}
      {isCraType && (
        <div className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-4 space-y-4">
          <div className="text-xs font-semibold text-[#4f6ed1]">CRA 量化参数</div>
          <CRAParamForm
            value={formToCra(form)}
            onChange={handleCraChange}
            showTradeCountMode
          />
        </div>
      )}
    </div>
  )
}
