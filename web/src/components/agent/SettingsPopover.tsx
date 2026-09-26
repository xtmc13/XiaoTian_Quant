import { useEffect, useState } from 'react'
import { X } from 'lucide-react'
import type { AgentSettings } from './types'

export interface SettingsPopoverProps {
  settings: AgentSettings
  onSave: (settings: AgentSettings) => void
  onClose: () => void
}

// ── 设置弹层：system_prompt 与 temperature，保存到 xt-agent-settings ──
export function SettingsPopover({ settings, onSave, onClose }: SettingsPopoverProps) {
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
        className="w-full max-w-[320px] rounded-xl border border-quant-border bg-quant-card p-3 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-2 flex items-center justify-between">
          <span className="text-[13px] font-semibold">助手设置</span>
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭设置"
            className="rounded p-1 text-[#888] hover:bg-quant-hover hover:text-foreground"
          >
            <X size={14} />
          </button>
        </div>
        <label className="mb-1 block text-[11px] text-muted-foreground" htmlFor="agent-system-prompt">
          系统提示词（system_prompt）
        </label>
        <textarea
          id="agent-system-prompt"
          value={systemPrompt}
          onChange={(e) => setSystemPrompt(e.target.value)}
          rows={3}
          placeholder="例如：你是一名严谨的量化交易助手…"
          className="w-full resize-none rounded-lg border border-quant-border bg-quant-bg-secondary px-2.5 py-2 text-[12px] text-foreground placeholder:text-[#555] focus:border-[#1890ff]/60 focus:outline-none focus:ring-1 focus:ring-[#1890ff]/30"
        />
        <label className="mb-1 mt-2 block text-[11px] text-muted-foreground" htmlFor="agent-temperature">
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
          className="w-24 rounded-lg border border-quant-border bg-quant-bg-secondary px-2.5 py-1.5 text-[12px] text-foreground focus:border-[#1890ff]/60 focus:outline-none"
        />
        <div className="mt-3 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded-md border border-quant-border px-2.5 py-1 text-[12px] text-[#aaa] hover:bg-quant-hover"
          >
            取消
          </button>
          <button
            type="button"
            onClick={handleSave}
            className="rounded-md bg-[#1890ff] px-2.5 py-1 text-[12px] text-white hover:bg-[#40a9ff]"
          >
            保存
          </button>
        </div>
      </div>
    </div>
  )
}

export default SettingsPopover
