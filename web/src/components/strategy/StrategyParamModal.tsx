import { useEffect, useState } from 'react'
import { Settings2, X } from 'lucide-react'
import type { StrategyParamDef } from '@/types'
import { DynamicParamField } from './StrategyFormFields'

const inputCls =
  'w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold'

interface StrategyParamModalProps {
  open: boolean
  title: string
  desc: string
  paramDefs: StrategyParamDef[]
  loading: boolean
  values: Record<string, unknown>
  onClose: () => void
  /** 确认时带回完整草稿（取消则丢弃，与指标参数弹窗交互一致）。 */
  onConfirm: (next: Record<string, unknown>) => void
}

/**
 * 已选策略参数弹窗 —— 视觉与交互完全对齐 IndicatorParamModal
 * （同款遮罩、圆角卡片、字段竖排、底部 取消/确认）。字段由后端
 * param-defs 驱动（DynamicParamField 渲染）。
 */
export function StrategyParamModal({ open, title, desc, paramDefs, loading, values, onClose, onConfirm }: StrategyParamModalProps) {
  const [draft, setDraft] = useState<Record<string, unknown>>(values)
  useEffect(() => {
    if (open) setDraft(values)
  }, [open, values])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4" onClick={onClose}>
      <div
        className="w-full max-w-lg bg-quant-card border border-quant-border rounded-2xl flex flex-col max-h-[85vh]"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-start justify-between gap-3 p-5 border-b border-quant-border">
          <div className="flex items-start gap-2.5">
            <Settings2 className="w-4 h-4 text-quant-gold mt-0.5" />
            <div>
              <div className="text-sm font-bold text-foreground">{title}</div>
              <div className="text-[11px] text-muted-foreground mt-0.5">{desc}</div>
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

        {/* Body：字段竖排（与指标弹窗同款） */}
        <div className="flex-1 overflow-y-auto p-6 space-y-4">
          {loading ? (
            <div className="text-xs text-muted-foreground py-4 text-center">加载参数定义...</div>
          ) : paramDefs.length === 0 ? (
            <div className="text-xs text-muted-foreground py-4 text-center rounded-lg border border-dashed border-quant-border">
              该策略无可调参数
            </div>
          ) : (
            paramDefs.map((def) => (
              <DynamicParamField
                key={def.name}
                def={def}
                value={draft[def.name]}
                onChange={(v) => setDraft((prev) => ({ ...prev, [def.name]: v }))}
              />
            ))
          )}
        </div>

        {/* Footer */}
        <div className="flex justify-end gap-2 p-4 border-t border-quant-border">
          <button
            onClick={onClose}
            className="px-4 py-2 rounded-lg border border-quant-border text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            取消
          </button>
          <button
            onClick={() => onConfirm(draft)}
            className="px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity"
          >
            确认
          </button>
        </div>
      </div>
    </div>
  )
}

// inputCls 保留给后续扩展（与指标弹窗保持一致的可读性）。
void inputCls
