import React from 'react'
import { CRAParamForm, DEFAULT_CRA_PARAMS, type CRAParams } from '@/components/strategy/CRAParamForm'
import { apiPayloadToCraParams, craParamsToApiPayload } from '@/components/strategy/CRAParamForm'

export function WizardField({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
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

const CONTRACT_TYPES = new Set([
  'trend_long',
  'trend_short',
  'counter_stable',
  'counter_safe',
  'high_frequency',
  'head_tail_arbitrage',
  'macd_golden_long',
  'macd_death_short',
  'ema_follow_trend',
  'ema_counter_trend',
  'dual_burn',
  'global_burn',
])

function marketFromType(type: string): 'spot' | 'contract' {
  return CONTRACT_TYPES.has(type) ? 'contract' : 'spot'
}

/* ─── CRA↔ snake_case form conversion ─────────────────────────────── */
function formToCra(f: Record<string, unknown>): CRAParams {
  return apiPayloadToCraParams(f as Partial<Parameters<typeof apiPayloadToCraParams>[0]>)
}

function craToForm(cra: CRAParams, f: Record<string, unknown>): Record<string, unknown> {
  return {
    ...f,
    ...craParamsToApiPayload(cra),
  }
}

export function BotParamForm({ form, setForm, effectiveType }: BotParamFormProps) {
  const CRA_TYPES = new Set([
    'martin_trend',
    'wallstreet',
    'aggressive',
    'conservative',
    'high_frequency',
    'high_flat',
    'trend_long',
    'trend_short',
    'counter_stable',
    'counter_safe',
    'head_tail_arbitrage',
    'dual_burn',
    'global_burn',
    'macd_golden',
    'macd_death',
    'macd_golden_long',
    'macd_death_short',
    'ema_follow',
    'ema_counter',
    'ema_follow_trend',
    'ema_counter_trend',
  ])
  const isCraType = CRA_TYPES.has(effectiveType)

  const market = marketFromType(effectiveType)

  const handleCraChange = (cra: CRAParams) => {
    setForm(craToForm(cra, form))
  }

  return (
    <div className="space-y-4">
      {/* -- 基础策略参数 -- */}
      <div className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-4 space-y-4">
        <div className="text-xs font-semibold text-white">基础策略参数</div>
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
      </div>

      {/* -- CRA 参数配置 (shared component) -- */}
      {isCraType && (
        <div className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-4 space-y-4">
          <div className="text-xs font-semibold text-[#4f6ed1]">CRA 量化参数</div>
          <CRAParamForm value={formToCra(form)} onChange={handleCraChange} market={market} />
        </div>
      )}
    </div>
  )
}
