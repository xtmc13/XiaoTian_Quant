import { TrendingUp, X } from 'lucide-react'
import type { MovingTPTier } from '@/types'
import { MovingTPTiersTable } from './MovingTPTiersTable'

interface MovingTPModalProps {
  open: boolean
  onClose: () => void
  value: MovingTPTier[]
  onChange: (next: MovingTPTier[]) => void
  disabled?: boolean
}

export function MovingTPModal({ open, onClose, value, onChange, disabled = false }: MovingTPModalProps) {
  if (!open) return null

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
            <TrendingUp className="w-4 h-4 text-quant-gold" />
            <div>
              <h3 className="text-sm font-bold">移动止盈止损参数</h3>
              <p className="text-[10px] text-muted-foreground mt-0.5">配置不同涨幅档位对应的止盈回撤比例</p>
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

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-6">
          <MovingTPTiersTable value={value} onChange={onChange} disabled={disabled} />
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end px-6 py-4 border-t border-quant-border shrink-0">
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 rounded-lg text-xs font-medium bg-quant-gold text-white hover:opacity-90 transition-opacity"
          >
            完成
          </button>
        </div>
      </div>
    </div>
  )
}
