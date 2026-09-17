import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Settings2, X, FlaskConical } from 'lucide-react'
import { indicatorApi } from '@/lib/api'
import type { IndicatorItem } from '@/types'
import { cn } from '@/lib/utils'
import {
  getIndicatorDef,
  validateIndicatorValues,
  type IndicatorParamValue,
  type OpenIndicatorKey,
} from './indicatorPresets'

const inputCls =
  'w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold'

interface IndicatorParamModalProps {
  open: boolean
  indicatorKey: OpenIndicatorKey
  values: Record<string, IndicatorParamValue>
  custom: { code_id: number; name: string } | null
  direction: 'long' | 'short' | 'dual'
  onClose: () => void
  onConfirm: (next: {
    values: Record<string, IndicatorParamValue>
    custom: { code_id: number; name: string } | null
  }) => void
}

/**
 * 开仓指标参数弹窗 —— 视觉与交互对齐 AddPositionModal/MovingTPModal
 * （同款遮罩、圆角卡片、底部 取消/确认）。按指标定义动态渲染字段；
 * "提示"级校验（如 fast≥slow）不硬拦，确认时原样带回由父级展示。
 */
export function IndicatorParamModal({
  open,
  indicatorKey,
  values,
  custom,
  direction,
  onClose,
  onConfirm,
}: IndicatorParamModalProps) {
  const [draft, setDraft] = useState<Record<string, IndicatorParamValue>>(values)
  const [draftCustom, setDraftCustom] = useState<{ code_id: number; name: string } | null>(custom)
  useEffect(() => {
    if (open) {
      setDraft(values)
      setDraftCustom(custom)
    }
  }, [open, values, custom])

  const def = getIndicatorDef(indicatorKey)
  const isCustom = indicatorKey === 'custom'

  // 自定义指标：拉取指标实验室列表。
  const { data: indicatorItems = [], isLoading: indicatorsLoading } = useQuery<IndicatorItem[]>({
    queryKey: ['indicator-picker-list'],
    queryFn: () => indicatorApi.list(),
    enabled: open && isCustom,
    staleTime: 60_000,
  })

  if (!open || !def) return null

  const hint = validateIndicatorValues(indicatorKey, draft)
  const lockedDirLabel = def.lockedDirection === 'long' ? '做多（锁定）' : '做空（锁定）'

  const handleConfirm = () => {
    onConfirm({ values: draft, custom: isCustom ? draftCustom : custom })
    onClose()
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
      onClick={onClose}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onClose()
      }}
      tabIndex={-1}
    >
      <div
        role="document"
        className="w-full max-w-2xl max-h-[90vh] flex flex-col rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
          <div className="flex items-center gap-2">
            <Settings2 className="w-4 h-4 text-quant-gold" />
            <div>
              <h3 className="text-sm font-bold">{def.label} · 参数</h3>
              <p className="text-[10px] text-muted-foreground mt-0.5">
                {def.desc}
                {def.lockedDirection && ` · 方向锁定${def.lockedDirection === 'long' ? '做多' : '做空'}`}
              </p>
            </div>
          </div>
          <button
            onClick={onClose}
            aria-label="关闭"
            className="w-8 h-8 rounded-lg border border-quant-border flex items-center justify-center text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors"
          >
            <X className="w-3.5 h-3.5" />
          </button>
        </div>

        {/* Body：字段竖排 */}
        <div className="flex-1 overflow-y-auto p-6 space-y-4">
          {def.lockedDirection && (
            <div>
              <label className="text-[11px] text-muted-foreground mb-1.5 block">交易方向</label>
              <div className={cn(inputCls, 'flex items-center justify-between text-muted-foreground')}>
                <span>
                  {direction === 'long' ? '做多' : direction === 'short' ? '做空' : '双向'} →{' '}
                  <span className="text-foreground font-medium">{lockedDirLabel}</span>
                </span>
                <span className="text-[10px]">不可修改</span>
              </div>
            </div>
          )}

          {def.fields.map((f) => (
            <div key={f.key}>
              <label htmlFor={`ind-param-${f.key}`} className="text-[11px] text-muted-foreground mb-1.5 block">
                {f.label}
              </label>
              {f.type === 'select' ? (
                <select
                  id={`ind-param-${f.key}`}
                  value={String(draft[f.key] ?? f.default)}
                  onChange={(e) => setDraft((prev) => ({ ...prev, [f.key]: e.target.value }))}
                  className={inputCls}
                >
                  {(f.options ?? []).map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  id={`ind-param-${f.key}`}
                  type="number"
                  min={f.min}
                  max={f.max}
                  step={f.type === 'int' ? 1 : 'any'}
                  value={Number(draft[f.key] ?? f.default)}
                  onChange={(e) =>
                    setDraft((prev) => ({ ...prev, [f.key]: f.type === 'int' ? parseInt(e.target.value, 10) || 0 : Number(e.target.value) }))
                  }
                  className={inputCls}
                />
              )}
            </div>
          ))}

          {isCustom && (
            <div className="space-y-2">
              <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                <FlaskConical className="w-3.5 h-3.5 text-quant-gold" />
                选择指标实验室的指标
              </div>
              {indicatorsLoading ? (
                <div className="text-xs text-muted-foreground py-4 text-center">加载指标列表...</div>
              ) : indicatorItems.length === 0 ? (
                <div className="text-xs text-muted-foreground py-4 text-center rounded-lg border border-dashed border-quant-border">
                  暂无可用指标，请先在「指标 IDE」创建
                </div>
              ) : (
                <div className="max-h-56 overflow-y-auto rounded-lg border border-quant-border divide-y divide-quant-border/50">
                  {indicatorItems.map((it) => (
                    <button
                      key={it.id}
                      type="button"
                      onClick={() => setDraftCustom({ code_id: it.id, name: it.name })}
                      className={cn(
                        'w-full px-3 py-2.5 text-left transition-colors',
                        draftCustom?.code_id === it.id ? 'bg-quant-gold/10' : 'hover:bg-quant-hover'
                      )}
                    >
                      <div className="flex items-center justify-between">
                        <span className={cn('text-xs font-medium', draftCustom?.code_id === it.id ? 'text-quant-gold' : 'text-foreground')}>
                          {it.name}
                        </span>
                        {draftCustom?.code_id === it.id && <span className="text-[10px] text-quant-gold">已选</span>}
                      </div>
                      {it.description && <div className="text-[10px] text-muted-foreground mt-0.5 line-clamp-1">{it.description}</div>}
                    </button>
                  ))}
                </div>
              )}
              <p className="text-[10px] text-quant-gold/80 leading-relaxed">
                实验功能：自定义指标执行依赖指标沙箱（本期仅保存配置，开仓执行将视为无门槛）。
              </p>
            </div>
          )}

          {hint && (
            <div
              className={cn(
                'px-3 py-2 rounded-lg text-[11px] border',
                hint.startsWith('提示') ? 'bg-quant-gold/5 border-quant-gold/20 text-quant-gold' : 'bg-quant-red/10 border-quant-red/30 text-quant-red'
              )}
            >
              {hint}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 px-6 py-4 border-t border-quant-border shrink-0">
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 rounded-lg border border-quant-border text-xs text-muted-foreground hover:text-foreground hover:bg-quant-hover transition-colors"
          >
            取消
          </button>
          <button
            type="button"
            onClick={handleConfirm}
            disabled={isCustom && !draftCustom}
            className="px-4 py-2 rounded-lg text-xs font-medium bg-quant-gold text-white hover:opacity-90 transition-opacity disabled:opacity-40 disabled:cursor-not-allowed"
          >
            确认
          </button>
        </div>
      </div>
    </div>
  )
}

export default IndicatorParamModal
