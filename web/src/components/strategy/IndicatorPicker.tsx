import { useState } from 'react'
import { Settings2, Ban, FlaskConical } from 'lucide-react'
import { cn } from '@/lib/utils'
import {
  OPEN_INDICATORS,
  defaultIndicatorParams,
  type IndicatorParamValue,
  type OpenIndicatorKey,
} from './indicatorPresets'
import { IndicatorParamModal } from './IndicatorParamModal'
import { StrategyParamModal } from './StrategyParamModal'
import type { StrategyParamDef } from '@/types'

export interface IndicatorSelection {
  indicator: OpenIndicatorKey
  params: Record<string, IndicatorParamValue>
  custom: { code_id: number; name: string } | null
}

interface IndicatorPickerProps {
  indicator: OpenIndicatorKey
  params: Record<string, IndicatorParamValue>
  custom: { code_id: number; name: string } | null
  disabled?: boolean
  /** 策略 tile：经三级选项进入的自定义策略（如流动性热力扫反），作为一排指标
   *  卡中的一员展示；选中态 = indicator 为 'none'（策略自带信号，不设指标门槛）。
   *  参数走与指标一致的弹窗（点击 tile 或 ⚙ 打开，用户 2026-09-30 明确
   *  "原本是点击这个才弹窗"，不接受内嵌面板）。 */
  strategyTile?: {
    key: string
    label: string
    desc: string
    paramDefs: StrategyParamDef[]
    loading: boolean
    values: Record<string, unknown>
    onConfirm: (next: Record<string, unknown>) => void
  } | null
  onChange: (next: IndicatorSelection) => void
}

function summarizeParams(key: OpenIndicatorKey, params: Record<string, IndicatorParamValue>): string {
  const parts: string[] = []
  if (params.fast != null) parts.push(`F${params.fast}`)
  if (params.slow != null) parts.push(`S${params.slow}`)
  if (params.signal != null) parts.push(`Sig${params.signal}`)
  if (params.period != null && params.period !== 'close') parts.push(String(params.period))
  if (key === 'custom') return ''
  return parts.join('/')
}

/**
 * 开仓指标选择器：一排可点选的指标卡。选中高亮；每个指标带"参数"按钮
 * 弹出 IndicatorParamModal 编辑参数；"未设置"表示使用壳默认（无开仓门槛）。
 */
export function IndicatorPicker({ indicator, params, custom, disabled, strategyTile = null, onChange }: IndicatorPickerProps) {
  const [paramModalKey, setParamModalKey] = useState<OpenIndicatorKey | null>(null)
  const [strategyModalOpen, setStrategyModalOpen] = useState(false)

  const openStrategyModal = () => {
    if (!strategyTile) return
    setStrategyModalOpen(true)
  }

  const select = (key: OpenIndicatorKey) => {
    if (disabled) return
    if (key === 'none') {
      onChange({ indicator: 'none', params: {}, custom: null })
      return
    }
    // 首次选中：以默认参数初始化，并立即打开参数弹窗。
    const nextParams = indicator === key ? params : defaultIndicatorParams(key)
    onChange({ indicator: key, params: nextParams, custom: key === 'custom' ? custom : null })
    setParamModalKey(key)
  }

  const openParams = (key: OpenIndicatorKey) => {
    if (disabled) return
    setParamModalKey(key)
  }

  const activeDef = OPEN_INDICATORS.find((d) => d.key === indicator)

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-2">
        {/* 未设置（壳默认）：存在策略 tile 时隐藏——"未设置"语义由策略卡承担，
            避免双选中（2026-09-30 用户截图指出"未设置和策略同时选中"） */}
        {!strategyTile && (
          <button
            type="button"
            disabled={disabled}
            onClick={() => select('none')}
            title="未设置（将使用壳默认）"
            className={cn(
              'inline-flex items-center gap-1.5 px-3 py-2 rounded-lg border text-xs font-medium transition-colors',
              indicator === 'none'
                ? 'bg-quant-gold/10 border-quant-gold/30 text-quant-gold'
                : 'border-quant-border text-muted-foreground hover:text-foreground hover:border-quant-gold/20',
              disabled && 'opacity-40 cursor-not-allowed'
            )}
          >
            <Ban className="w-3 h-3" />
            未设置
          </button>
        )}

        {OPEN_INDICATORS.map((def) => {
          const active = indicator === def.key
          const summary = active ? summarizeParams(def.key, params) : ''
          return (
            <div
              key={def.key}
              className={cn(
                'inline-flex items-center rounded-lg border transition-colors overflow-hidden',
                active
                  ? 'border-quant-gold/40 bg-quant-gold/10'
                  : 'border-quant-border hover:border-quant-gold/20',
                disabled && 'opacity-40'
              )}
            >
              <button
                type="button"
                disabled={disabled}
                onClick={() => select(def.key)}
                title={def.desc}
                className="px-3 py-2 text-left"
              >
                <div className={cn('text-xs font-medium flex items-center gap-1', active ? 'text-quant-gold' : 'text-foreground')}>
                  {def.key === 'custom' && <FlaskConical className="w-3 h-3" />}
                  {def.label}
                </div>
                <div className="text-[9px] text-muted-foreground mt-0.5">
                  {active && def.key === 'custom'
                    ? (custom?.name ?? '未选择指标')
                    : active && summary
                      ? summary
                      : def.desc}
                </div>
              </button>
              {active && (
                <button
                  type="button"
                  disabled={disabled}
                  onClick={() => openParams(def.key)}
                  aria-label={`${def.label} 参数`}
                  className="px-2 py-2 text-muted-foreground hover:text-quant-gold transition-colors border-l border-quant-gold/20"
                >
                  <Settings2 className="w-3.5 h-3.5" />
                </button>
              )}
            </div>
          )
        })}

        {/* 策略 tile：与指标卡同排同外观；选中=使用策略自带信号（不设指标门槛）；
            点击或 ⚙ 弹策略参数窗（与指标点击行为一致） */}
        {strategyTile && (
          <div
            className={cn(
              'inline-flex items-center rounded-lg border transition-colors overflow-hidden',
              indicator === 'none'
                ? 'border-quant-gold/40 bg-quant-gold/10'
                : 'border-quant-border hover:border-quant-gold/20',
              disabled && 'opacity-40'
            )}
          >
            <button
              type="button"
              disabled={disabled}
              onClick={() => {
                select('none')
                setStrategyModalOpen(true)
              }}
              title={strategyTile.desc}
              className="px-3 py-2 text-left"
            >
              <div className={cn('text-xs font-medium flex items-center gap-1', indicator === 'none' ? 'text-quant-gold' : 'text-foreground')}>
                {strategyTile.label}
              </div>
              <div className="text-[9px] text-muted-foreground mt-0.5">
                {indicator === 'none' ? '已选 · 点击可调参数' : strategyTile.desc}
              </div>
            </button>
            {indicator === 'none' && (
              <button
                type="button"
                disabled={disabled}
                onClick={openStrategyModal}
                aria-label={`${strategyTile.label} 参数`}
                className="px-2 py-2 text-muted-foreground hover:text-quant-gold transition-colors border-l border-quant-gold/20"
              >
                <Settings2 className="w-3.5 h-3.5" />
              </button>
            )}
          </div>
        )}
      </div>

      {/* 说明行：策略在场时"未设置"语义由策略承担，不再显示壳默认文案 */}
      <div className="text-[10px] text-muted-foreground leading-relaxed">
        {indicator === 'none' ? (
          strategyTile ? (
          <>
            当前策略：<span className="text-foreground font-medium">{strategyTile.label}</span>
            <span className="ml-1">（策略自带入场信号，不设指标门槛；点卡片或 ⚙ 调节参数）</span>
          </>
          ) : (
            '未设置（将使用壳默认）：首单不受指标门槛限制。'
          )
        ) : (
          <>
            当前开仓指标：<span className="text-foreground font-medium">{activeDef?.label}</span>
            {indicator === 'custom' && <span className="ml-1 text-quant-gold/80">· 实验功能：执行依赖指标沙箱</span>}
          </>
        )}
      </div>

      {/* 参数弹窗 */}
      <IndicatorParamModal
        open={paramModalKey != null}
        indicatorKey={paramModalKey ?? 'none'}
        values={paramModalKey === indicator ? params : defaultIndicatorParams(paramModalKey ?? 'none')}
        custom={custom}
        onClose={() => setParamModalKey(null)}
        onConfirm={({ values: nextValues, custom: nextCustom }) => {
          if (paramModalKey == null) return
          onChange({ indicator: paramModalKey, params: nextValues, custom: nextCustom })
        }}
      />

      {/* 策略参数弹窗：与指标参数弹窗同款体验（StrategyParamModal，点击策略卡或 ⚙ 打开） */}
      {strategyTile && (
        <StrategyParamModal
          open={strategyModalOpen}
          title={`${strategyTile.label} · 参数`}
          desc={strategyTile.desc}
          paramDefs={strategyTile.paramDefs}
          loading={strategyTile.loading}
          values={strategyTile.values}
          onClose={() => setStrategyModalOpen(false)}
          onConfirm={(next) => {
            strategyTile.onConfirm(next)
            setStrategyModalOpen(false)
          }}
        />
      )}
    </div>
  )
}

export default IndicatorPicker
