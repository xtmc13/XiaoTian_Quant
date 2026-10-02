import { useEffect, useState } from 'react'
import { X } from 'lucide-react'
import type { AgentSettings } from './types'
import { cn } from '@/lib/utils'

export interface SettingsPopoverProps {
  settings: AgentSettings
  onSave: (settings: AgentSettings) => void
  onClose: () => void
  /** 浅色（桌面版）皮肤：用于全屏助手；默认深色供悬浮面板使用 */
  light?: boolean
}

// ── 设置弹层：system_prompt 与 temperature，保存到 xt-agent-settings ──
export function SettingsPopover({ settings, onSave, onClose, light }: SettingsPopoverProps) {
  const [systemPrompt, setSystemPrompt] = useState(settings.system_prompt)
  const [temperature, setTemperature] = useState(String(settings.temperature))

  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const handleSave = () => {
    const t = Number(temperature)
    onSave({
      ...settings,
      system_prompt: systemPrompt.trim(),
      temperature: Number.isFinite(t) ? Math.min(2, Math.max(0, t)) : 0.7,
    })
    onClose()
  }

  return (
    <div
      className="absolute inset-0 z-20 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
      role="dialog"
      aria-label="助手设置"
    >
      <div
        className={cn(
          'w-full max-w-[320px] rounded-2xl border p-4 shadow-2xl',
          light
            ? 'border-[var(--ag-stroke3)] bg-[var(--ag-card)] text-[var(--ag-text1)]'
            : 'border-quant-border bg-quant-card'
        )}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-2 flex items-center justify-between">
          <span className="text-[14px] font-semibold">助手设置</span>
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭设置"
            className={cn(
              'rounded p-1',
              light
                ? 'text-[var(--ag-text3)] hover:bg-black/5 hover:text-[var(--ag-text1)]'
                : 'text-[#888] hover:bg-quant-hover hover:text-foreground'
            )}
          >
            <X size={14} />
          </button>
        </div>
        <label
          className={cn('mb-1 block text-[11px]', light ? 'text-[var(--ag-text3)]' : 'text-muted-foreground')}
          htmlFor="agent-system-prompt"
        >
          系统提示词（system_prompt）
        </label>
        <textarea
          id="agent-system-prompt"
          value={systemPrompt}
          onChange={(e) => setSystemPrompt(e.target.value)}
          rows={3}
          placeholder="例如：你是一名严谨的量化交易助手…"
          className={cn(
            'w-full resize-none rounded-lg border px-2.5 py-2 text-[12px] focus:outline-none',
            light
              ? 'border-[var(--ag-stroke2)] bg-[var(--ag-sidebar)] text-[var(--ag-text1)] placeholder:text-[var(--ag-text4)] focus:border-[var(--ag-accent)]/60'
              : 'border-quant-border bg-quant-bg-secondary text-foreground placeholder:text-[#555] focus:border-[#1890ff]/60 focus:ring-1 focus:ring-[#1890ff]/30'
          )}
        />
        <label
          className={cn('mb-1 mt-2 block text-[11px]', light ? 'text-[var(--ag-text3)]' : 'text-muted-foreground')}
          htmlFor="agent-temperature"
        >
          温度（temperature，0-2）
        </label>
        <input
          id="agent-temperature"
          type="number"
          min={0}
          max={2}
          step={0.1}
          value={temperature}
          onChange={(e) => setTemperature(e.target.value)}
          className={cn(
            'w-24 rounded-lg border px-2.5 py-1.5 text-[12px] focus:outline-none',
            light
              ? 'border-[var(--ag-stroke2)] bg-[var(--ag-sidebar)] text-[var(--ag-text1)] focus:border-[var(--ag-accent)]/60'
              : 'border-quant-border bg-quant-bg-secondary text-foreground focus:border-[#1890ff]/60'
          )}
        />
        <div className="mt-3 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className={cn(
              'rounded-md border px-2.5 py-1 text-[12px]',
              light
                ? 'border-[var(--ag-stroke2)] text-[var(--ag-text3)] hover:bg-black/5'
                : 'border-quant-border text-[#aaa] hover:bg-quant-hover'
            )}
          >
            取消
          </button>
          <button
            type="button"
            onClick={handleSave}
            className={cn(
              'rounded-md px-2.5 py-1 text-[12px] text-white',
              light ? 'bg-[var(--ag-accent)] hover:opacity-90' : 'bg-[#1890ff] hover:bg-[#40a9ff]'
            )}
          >
            保存
          </button>
        </div>
      </div>
    </div>
  )
}

export default SettingsPopover
