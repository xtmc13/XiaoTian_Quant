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

export interface IndicatorSelection {
  indicator: OpenIndicatorKey
  params: Record<string, IndicatorParamValue>
  custom: { code_id: number; name: string } | null
}

interface IndicatorPickerProps {
  indicator: OpenIndicatorKey
  params: Record<string, IndicatorParamValue>
  custom: { code_id: number; name: string } | null
  /** 当前表单方向（弹窗内展示锁定关系用） */
  direction: 'long' | 'short' | 'dual'
  disabled?: boolean
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
export function IndicatorPicker({ indicator, params, custom, direction, disabled, onChange }: IndicatorPickerProps) {
  const [paramModalKey, setParamModalKey] = useState<OpenIndicatorKey | null>(null)

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
        {/* 未设置（壳默认） */}
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
                  {def.lockedDirection && (
                    <span className="text-[9px] px-1 rounded bg-quant-bg-tertiary text-muted-foreground">
                      {def.lockedDirection === 'long' ? '多' : '空'}
                    </span>
                  )}
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
      </div>

      {/* 说明行 */}
      <div className="text-[10px] text-muted-foreground leading-relaxed">
        {indicator === 'none' ? (
          '未设置（将使用壳默认）：首单不受指标门槛限制。'
        ) : (
          <>
            当前开仓指标：<span className="text-foreground font-medium">{activeDef?.label}</span>
            {activeDef?.lockedDirection && (
              <span className="ml-1 text-quant-gold">（方向已锁定{activeDef.lockedDirection === 'long' ? '做多' : '做空'}）</span>
            )}
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
        direction={direction}
        onClose={() => setParamModalKey(null)}
        onConfirm={({ values: nextValues, custom: nextCustom }) => {
          if (paramModalKey == null) return
          onChange({ indicator: paramModalKey, params: nextValues, custom: nextCustom })
        }}
      />
    </div>
  )
}

export default IndicatorPicker
