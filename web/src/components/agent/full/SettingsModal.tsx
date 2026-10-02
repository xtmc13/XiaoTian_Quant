import { useEffect, useState } from 'react'
import { Info, SlidersHorizontal, Sparkles, X } from 'lucide-react'
import type { AgentSettings } from '../types'
import { cn } from '@/lib/utils'

interface ModelProvider {
  key: string
  label?: string
  configured?: boolean
  models?: string[]
}

export interface SettingsModalProps {
  settings: AgentSettings
  onSave: (settings: AgentSettings) => void
  onClose: () => void
  providers: ModelProvider[]
  version?: string
}

type Section = 'general' | 'model' | 'about'

const SECTIONS: { id: Section; label: string; icon: typeof Sparkles }[] = [
  { id: 'general', label: '通用', icon: SlidersHorizontal },
  { id: 'model', label: '模型', icon: Sparkles },
  { id: 'about', label: '关于', icon: Info },
]

// ── 大号居中设置窗（对标 dsh 桌面版"配置"弹窗：左侧图标导航 + 右侧内容区） ──
export function SettingsModal({ settings, onSave, onClose, providers, version }: SettingsModalProps) {
  const [section, setSection] = useState<Section>('general')
  const [systemPrompt, setSystemPrompt] = useState(settings.system_prompt)
  const [temperature, setTemperature] = useState(String(settings.temperature))
  const [model, setModel] = useState(settings.model)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const selectedProvider = model.split(':')[0] || ''
  const selectedModel = model.split(':')[1] || ''
  const providerObj = providers.find((p) => p.key === selectedProvider)
  const modelLabel = model
    ? `${providerObj?.label || providerObj?.key || selectedProvider}${selectedModel ? ` · ${selectedModel}` : ''}`
    : '默认模型'

  const handleSave = () => {
    const t = Number(temperature)
    onSave({
      ...settings,
      system_prompt: systemPrompt.trim(),
      temperature: Number.isFinite(t) ? Math.min(2, Math.max(0, t)) : 0.7,
      model,
    })
    onClose()
  }

  const inputCls =
    'w-full rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-sidebar)] px-2.5 py-2 text-[12px] text-[var(--ag-text1)] placeholder:text-[var(--ag-text4)] focus:border-[var(--ag-accent)]/60 focus:outline-none'

  return (
    <div
      className="absolute inset-0 z-30 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
      role="dialog"
      aria-label="助手设置"
    >
      <div
        className="flex h-[min(86%,560px)] w-[min(92%,880px)] flex-col overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] text-[var(--ag-text1)] shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b border-[var(--ag-stroke3)] px-4 py-2.5">
          <span className="text-[14px] font-semibold">设置</span>
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭设置"
            className="rounded-md p-1 text-[var(--ag-text3)] transition-colors hover:bg-black/5 hover:text-[var(--ag-text1)]"
          >
            <X size={15} />
          </button>
        </div>

        <div className="flex min-h-0 flex-1">
          <nav aria-label="设置导航" className="flex w-40 shrink-0 flex-col gap-0.5 border-r border-[var(--ag-stroke3)] p-2">
            {SECTIONS.map(({ id, label, icon: Icon }) => (
              <button
                key={id}
                type="button"
                aria-current={section === id ? 'true' : undefined}
                onClick={() => setSection(id)}
                className={cn(
                  'flex h-8 items-center gap-2 rounded-lg px-2.5 text-[13px] font-medium transition-colors',
                  section === id
                    ? 'bg-black/5 text-[var(--ag-text1)]'
                    : 'text-[var(--ag-text2)] hover:bg-black/4 hover:text-[var(--ag-text1)]'
                )}
              >
                <Icon size={14} className={section === id ? 'text-[var(--ag-accent)]' : 'text-[var(--ag-text3)]'} />
                {label}
              </button>
            ))}
          </nav>

          <div className="min-w-0 flex-1 overflow-y-auto p-5">
            {section === 'general' && (
              <div className="max-w-lg">
                <h3 className="mb-1 text-[15px] font-semibold">通用</h3>
                <p className="mb-4 text-[12px] text-[var(--ag-text3)]">助手的人格与生成风格，保存到本地设置。</p>
                <label className="mb-1 block text-[11px] text-[var(--ag-text3)]" htmlFor="agent-system-prompt">
                  系统提示词（system_prompt）
                </label>
                <textarea
                  id="agent-system-prompt"
                  value={systemPrompt}
                  onChange={(e) => setSystemPrompt(e.target.value)}
                  rows={5}
                  placeholder="例如：你是一名严谨的量化交易助手…"
                  className={cn(inputCls, 'resize-none')}
                />
                <label className="mb-1 mt-3 block text-[11px] text-[var(--ag-text3)]" htmlFor="agent-temperature">
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
                  className={cn(inputCls, 'w-24')}
                />
              </div>
            )}

            {section === 'model' && (
              <div className="max-w-lg">
                <h3 className="mb-1 text-[15px] font-semibold">模型</h3>
                <p className="mb-4 text-[12px] text-[var(--ag-text3)]">选择对话使用的厂商与模型，留空使用默认。</p>
                <label className="mb-1 block text-[11px] text-[var(--ag-text3)]" htmlFor="agent-provider">
                  厂商
                </label>
                <select
                  id="agent-provider"
                  value={selectedProvider}
                  onChange={(e) => setModel(e.target.value)}
                  className={inputCls}
                >
                  <option value="">默认</option>
                  {providers.map((p) => (
                    <option key={p.key} value={p.key}>
                      {p.label || p.key}
                    </option>
                  ))}
                </select>
                <label className="mb-1 mt-3 block text-[11px] text-[var(--ag-text3)]" htmlFor="agent-model">
                  模型
                </label>
                <select
                  id="agent-model"
                  value={selectedModel}
                  onChange={(e) =>
                    setModel(e.target.value ? `${selectedProvider}:${e.target.value}` : selectedProvider)
                  }
                  disabled={!selectedProvider}
                  className={cn(inputCls, 'disabled:opacity-50')}
                >
                  <option value="">{selectedProvider ? '厂商默认' : '默认模型'}</option>
                  {(providerObj?.models || []).map((m) => (
                    <option key={m} value={m}>
                      {m}
                    </option>
                  ))}
                </select>
              </div>
            )}

            {section === 'about' && (
              <div className="max-w-lg">
                <h3 className="mb-1 text-[15px] font-semibold">关于</h3>
                <p className="mb-4 text-[12px] text-[var(--ag-text3)]">小天量化助手 · 全屏模式</p>
                <dl className="space-y-2 text-[12px]">
                  <div className="flex items-center justify-between border-b border-[var(--ag-stroke3)] pb-2">
                    <dt className="text-[var(--ag-text3)]">界面版本</dt>
                    <dd className="font-mono text-[var(--ag-text1)]">{version || '—'}</dd>
                  </div>
                  <div className="flex items-center justify-between border-b border-[var(--ag-stroke3)] pb-2">
                    <dt className="text-[var(--ag-text3)]">当前模型</dt>
                    <dd className="text-[var(--ag-text1)]">{modelLabel}</dd>
                  </div>
                </dl>
              </div>
            )}
          </div>
        </div>

        <div className="flex justify-end gap-2 border-t border-[var(--ag-stroke3)] px-4 py-2.5">
          <button
            type="button"
            onClick={onClose}
            className="rounded-md border border-[var(--ag-stroke2)] px-2.5 py-1 text-[12px] text-[var(--ag-text3)] hover:bg-black/5"
          >
            取消
          </button>
          <button
            type="button"
            onClick={handleSave}
            className="rounded-md bg-[var(--ag-accent)] px-2.5 py-1 text-[12px] text-white hover:opacity-90"
          >
            保存
          </button>
        </div>
      </div>
    </div>
  )
}

export default SettingsModal
