import { useStrategyCreateForm, StrategyCreateFormSections, strategyTypeLabel } from './StrategyCreateForm'
import { cn } from '@/lib/utils'
import { X } from 'lucide-react'

interface StrategyCreatePanelProps {
  /** 市场（表单模式）：合约多出杠杆/逐全仓字段。策略类型由 hook 按市场自动派生。 */
  market: 'spot' | 'contract'
  /** 兼容覆盖：编辑回写或旧快捷入口传入非 cra 原类型时保留该值（创建场景不传）。 */
  initialType?: string
  onClose: () => void
  onSaved: () => void
}

/**
 * 创建策略面板（弹窗/侧栏壳）。表单状态与四个区块全部复用
 * StrategyCreateForm（与 /create 独立页同一份实现），本组件只保留
 * 头部/底部操作栏。
 */
export function StrategyCreatePanel({ market, initialType, onClose, onSaved }: StrategyCreatePanelProps) {
  const form = useStrategyCreateForm(market, onSaved, initialType ? { initialType } : undefined)

  return (
    <div className="h-full flex flex-col bg-quant-card">
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
        <div>
          <h3 className="text-sm font-bold">新建 {strategyTypeLabel(form.strategyType, form.market)} 策略</h3>
          <p className="text-[10px] text-muted-foreground mt-0.5">
            {form.market === 'spot' ? '现货策略' : '合约策略'} · 直接配置 CRA 参数
          </p>
        </div>
        <button
          onClick={onClose}
          aria-label="关闭"
          className="w-8 h-8 rounded-lg border border-quant-border flex items-center justify-center text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors"
        >
          <X className="w-3.5 h-3.5" />
        </button>
      </div>

      {/* Body：四个锚点区块（与 /create 独立页共用） */}
      <div className="flex-1 overflow-y-auto p-6 space-y-5">
        <StrategyCreateFormSections form={form} />
      </div>

      {/* Footer */}
      <div className="flex items-center justify-between px-6 py-4 border-t border-quant-border shrink-0">
        <div className="flex items-center gap-4">
          <div className="text-[11px] text-muted-foreground">
            预估总投入: <span className="text-foreground font-mono">${form.totalAddPosition.toFixed(2)}</span>
          </div>
          <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer hover:text-foreground">
            <input
              type="checkbox"
              checked={form.saveAsDefault}
              onChange={(e) => form.setSaveAsDefault(e.target.checked)}
              className="rounded border-quant-border"
            />
            保存为默认策略模板
          </label>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={onClose}
            disabled={form.isSubmitting}
            className="px-4 py-2 rounded-lg border border-quant-border text-xs hover:bg-quant-hover transition-colors disabled:opacity-50"
          >
            取消
          </button>
          <button
            onClick={() => void form.handleSubmit()}
            disabled={form.isSubmitting}
            className={cn(
              'px-4 py-2 rounded-lg text-xs font-medium transition-opacity',
              form.isSubmitting ? 'bg-quant-gold/50 text-white cursor-wait' : 'bg-quant-gold text-white hover:opacity-90'
            )}
          >
            {form.isSubmitting ? '保存中...' : '保存策略'}
          </button>
        </div>
      </div>
    </div>
  )
}

export default StrategyCreatePanel
