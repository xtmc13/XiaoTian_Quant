import React, { useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowUp,
  Brain,
  ChevronDown,
  CircleStop,
  Clock,
  Gauge,
  Layers3,
  Paperclip,
  Plus,
  Send,
  Shield,
  Sparkles,
  SquareTerminal,
  SquarePen,
  RotateCcw,
  Undo2,
  X,
  Zap,
} from 'lucide-react'
import type { AgentSettings } from '../types'
import { QUICK_COMMANDS } from '../types'
import { completePalette, exactMatch, type PaletteItem, type SkillItem, type SlashCommand } from './slash'
import { cn } from '@/lib/utils'

// ── 提示词片段（+ 菜单 → Prompt snippets） ──
const PROMPT_SNIPPETS = [
  { label: '复盘今日交易', text: '帮我复盘今天的交易情况，找出问题和改进点。' },
  { label: '分析行情走势', text: '分析一下当前大盘走势，给出关键位和操作建议。' },
  { label: '检查机器人状态', text: '检查一下我所有运行中机器人的状态和盈亏。' },
]

const SLASH_ICONS: Record<SlashCommand['icon'], React.ReactNode> = {
  new: <SquarePen size={13} />,
  stop: <CircleStop size={13} />,
  retry: <RotateCcw size={13} />,
  model: <Sparkles size={13} />,
  clear: <X size={13} />,
  clock: <Clock size={13} />,
  brain: <Brain size={13} />,
  zap: <Zap size={13} />,
  send: <Send size={13} />,
  gauge: <Gauge size={13} />,
  undo: <Undo2 size={13} />,
}

interface ModelProvider {
  key: string
  label?: string
  configured?: boolean
  models?: string[]
}

export interface AgentComposerProps {
  input: string
  onInputChange: (v: string) => void
  onSubmit: (text: string) => void
  onStop: () => void
  isStreaming: boolean
  attachments: { name: string; content: string }[]
  onRemoveAttachment: (index: number) => void
  onPickFile: (file: File) => void
  textareaRef: React.RefObject<HTMLTextAreaElement | null>
  settings: AgentSettings
  updateSettings: (s: AgentSettings) => void
  providers: ModelProvider[]
  /** 执行斜杠命令；返回 false 表示未知命令（不应发生） */
  onSlash: (cmd: SlashCommand) => void
  /** 会话已有消息时展示的快捷指令（SuggestionPills） */
  onSuggestion: (text: string) => void
  showSuggestions: boolean
  /** 递增时打开模型选择弹层（/model 命令） */
  openModelSignal?: number
  /** 技能调色板数据（skills 插件），与命令合并展示 */
  skills?: SkillItem[]
  onUseSkill: (skill: SkillItem) => void
  /** 点击"完全权限"药丸（打开用量/权限概览） */
  onShowUsage?: () => void
  /** 占位符（空态/会话态文案不同，对标桌面版） */
  placeholder?: string
}

// ── 底部 Composer：多行自动增高 + 斜杠面板 + 附件 + 模型药丸 + 发送/停止/重定向 ──
export function AgentComposer({
  input,
  onInputChange,
  onSubmit,
  onStop,
  isStreaming,
  attachments,
  onRemoveAttachment,
  onPickFile,
  textareaRef,
  settings,
  updateSettings,
  providers,
  onSlash,
  onSuggestion,
  showSuggestions,
  openModelSignal,
  skills,
  onUseSkill,
  onShowUsage,
  placeholder = '给智能体发消息，/ 调用指令',
}: AgentComposerProps) {
  const [slashIndex, setSlashIndex] = useState(0)
  const [plusOpen, setPlusOpen] = useState(false)
  const [modelOpen, setModelOpen] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const rootRef = useRef<HTMLDivElement>(null)

  const candidates = useMemo(() => completePalette(input, skills || []), [input, skills])
  const paletteOpen = candidates !== null && candidates.length > 0

  // 候选变化时重置高亮
  useEffect(() => {
    setSlashIndex(0)
  }, [input])

  // /model 命令 → 打开模型弹层
  useEffect(() => {
    if (openModelSignal) setModelOpen(true)
  }, [openModelSignal])

  // 输入框自动增高
  useEffect(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = '0px'
    el.style.height = `${Math.min(el.scrollHeight, 150)}px`
  }, [input, textareaRef])

  const selectedProvider = settings.model.split(':')[0] || ''
  const selectedModel = settings.model.split(':')[1] || ''
  const providerObj = providers.find((p) => p.key === selectedProvider)
  const modelLabel = settings.model
    ? `${providerObj?.label || providerObj?.key || selectedProvider}${selectedModel ? ` · ${selectedModel}` : ''}`
    : '默认模型'

  const submit = () => {
    const body = input.trim()
    if (!body) return
    // 隐式接受：输入恰好是一条无参命令 → 执行命令而不是发送文本
    const exact = exactMatch(input)
    if (exact) {
      onSlash(exact)
      onInputChange('')
      return
    }
    onSubmit(body)
  }

  const acceptItem = (item: PaletteItem) => {
    if (item.kind === 'command' && item.command) {
      onSlash(item.command)
    } else if (item.skill) {
      onUseSkill(item.skill)
    }
    onInputChange('')
  }

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // IME 组合中不拦截
    if (e.nativeEvent.isComposing) return

    if (paletteOpen) {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSlashIndex((i) => (i + 1) % candidates.length)
        return
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSlashIndex((i) => (i - 1 + candidates.length) % candidates.length)
        return
      }
      if (e.key === 'Enter' || e.key === 'Tab') {
        e.preventDefault()
        const item = candidates[Math.min(slashIndex, candidates.length - 1)]
        if (item) acceptItem(item)
        return
      }
      if (e.key === 'Escape') {
        e.preventDefault()
        onInputChange('')
        return
      }
    }

    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      submit()
      return
    }
    if (e.key === 'Escape' && isStreaming) {
      e.preventDefault()
      onStop()
    }
  }

  const canSubmit = isStreaming || input.trim().length > 0

  return (
    <div ref={rootRef} className="relative w-full">
      {/* SuggestionPills：会话中且空闲时的快捷指令 */}
      {showSuggestions && !isStreaming && (
        <div className="mb-1.5 flex gap-1.5 overflow-x-auto px-1 pb-0.5 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
          {QUICK_COMMANDS.map((cmd) => (
            <button
              key={cmd}
              type="button"
              onClick={() => onSuggestion(cmd)}
              className="shrink-0 rounded-full border border-[var(--ag-stroke3)] bg-[var(--ag-card)]/80 px-2.5 py-1 text-[11px] text-[var(--ag-text3)] transition-colors hover:border-[var(--ag-accent)]/40 hover:text-[var(--ag-accent)]"
            >
              {cmd}
            </button>
          ))}
        </div>
      )}

      {/* 附件 chips */}
      {attachments.length > 0 && (
        <div className="mb-1.5 flex flex-wrap gap-1 px-1">
          {attachments.map((a, i) => (
            <span
              key={`${a.name}-${i}`}
              className="flex items-center gap-1 rounded-md border border-[var(--ag-stroke3)] bg-[var(--ag-card)] px-1.5 py-0.5 text-[11px] text-[var(--ag-text2)]"
            >
              <Paperclip size={10} />
              <span className="max-w-[140px] truncate">{a.name}</span>
              <button
                type="button"
                title="移除附件"
                aria-label={`移除附件 ${a.name}`}
                onClick={() => onRemoveAttachment(i)}
                className="rounded p-0.5 hover:text-[var(--ag-red)]"
              >
                <X size={10} />
              </button>
            </span>
          ))}
        </div>
      )}

      {/* 斜杠面板：命令 + 技能 */}
      {paletteOpen && (
        <div
          role="listbox"
          aria-label="命令面板"
          className="absolute bottom-full left-0 right-0 z-30 mb-2 overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)]/95 shadow-[var(--ag-shadow-panel)] backdrop-blur-xl"
        >
          <div className="max-h-64 overflow-y-auto p-1">
            {candidates.map((item, i) => (
              <div key={`${item.kind}-${item.name}`}>
                {/* 分组头：首个命令前 / 首个技能前 */}
                {(i === 0 || (item.kind === 'skill' && candidates[i - 1].kind === 'command')) && (
                  <div className="px-2.5 pb-0.5 pt-1.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-[var(--ag-text4)]">
                    {i === 0 && item.kind === 'command' ? '命令' : '技能'}
                  </div>
                )}
                <button
                  type="button"
                  role="option"
                  aria-selected={i === slashIndex}
                  onMouseEnter={() => setSlashIndex(i)}
                  onClick={() => acceptItem(item)}
                  className={cn(
                    'flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left',
                    i === slashIndex ? 'bg-[var(--ag-accent)]/8' : ''
                  )}
                >
                  <span className="shrink-0 text-[var(--ag-text3)]">{SLASH_ICONS[item.icon]}</span>
                  <span className="shrink-0 font-mono text-[12px] text-[var(--ag-text1)]">/{item.name}</span>
                  <span className="min-w-0 flex-1 truncate text-[11px] text-[var(--ag-text3)]">{item.description}</span>
                </button>
              </div>
            ))}
          </div>
          <div className="flex items-center gap-2 border-t border-[var(--ag-stroke3)] px-2.5 py-1 text-[10px] text-[var(--ag-text4)]">
            <span>↑↓ 选择</span>
            <span>Enter 执行</span>
            <span>Esc 关闭</span>
          </div>
        </div>
      )}

      {/* + 菜单 */}
      {plusOpen && (
        <>
          <div className="fixed inset-0 z-20" onClick={() => setPlusOpen(false)} />
          <div
            role="menu"
            className="absolute bottom-full left-0 z-30 mb-2 w-60 overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)]/95 py-1 shadow-[var(--ag-shadow-panel)] backdrop-blur-xl"
          >
            <div className="px-2.5 pb-0.5 pt-1 text-[10px] font-semibold uppercase tracking-[0.12em] text-[var(--ag-text4)]">
              附件
            </div>
            <button
              type="button"
              role="menuitem"
              onClick={() => {
                setPlusOpen(false)
                fileInputRef.current?.click()
              }}
              className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-[12px] text-[var(--ag-text2)] hover:bg-black/5"
            >
              <Paperclip size={13} />
              添加文件（.txt/.md/.csv/.json ≤100KB）
            </button>
            <div className="mt-1 px-2.5 pb-0.5 pt-1 text-[10px] font-semibold uppercase tracking-[0.12em] text-[var(--ag-text4)]">
              提示词片段
            </div>
            {PROMPT_SNIPPETS.map((s) => (
              <button
                key={s.label}
                type="button"
                role="menuitem"
                onClick={() => {
                  setPlusOpen(false)
                  onInputChange(input ? `${input}\n${s.text}` : s.text)
                  textareaRef.current?.focus()
                }}
                className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-[12px] text-[var(--ag-text2)] hover:bg-black/5"
              >
                <Sparkles size={13} />
                {s.label}
              </button>
            ))}
            <div className="border-t border-[var(--ag-stroke3)] px-2.5 py-1.5 text-[10px] text-[var(--ag-text4)]">
              输入 / 打开命令面板
            </div>
          </div>
        </>
      )}

      {/* 模型药丸弹层 */}
      {modelOpen && (
        <>
          <div className="fixed inset-0 z-20" onClick={() => setModelOpen(false)} />
          <div
            role="dialog"
            aria-label="选择模型"
            className="absolute bottom-full right-0 z-30 mb-2 w-72 rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)]/95 p-2.5 shadow-[var(--ag-shadow-panel)] backdrop-blur-xl"
          >
            <div className="mb-1.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-[var(--ag-text4)]">
              厂商
            </div>
            <select
              value={selectedProvider}
              onChange={(e) => updateSettings({ ...settings, model: e.target.value })}
              aria-label="选择厂商"
              className="mb-2 w-full rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2 py-1.5 text-[12px] text-[var(--ag-text1)] focus:border-[var(--ag-accent)]/60 focus:outline-none"
            >
              <option value="">默认</option>
              {providers.map((p) => (
                <option key={p.key} value={p.key}>
                  {p.label || p.key}
                </option>
              ))}
            </select>
            <div className="mb-1.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-[var(--ag-text4)]">
              模型
            </div>
            <select
              value={selectedModel}
              onChange={(e) =>
                updateSettings({
                  ...settings,
                  model: e.target.value ? `${selectedProvider}:${e.target.value}` : selectedProvider,
                })
              }
              disabled={!selectedProvider}
              aria-label="选择模型"
              className="w-full rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2 py-1.5 text-[12px] text-[var(--ag-text1)] focus:border-[var(--ag-accent)]/60 focus:outline-none disabled:opacity-50"
            >
              <option value="">{selectedProvider ? '厂商默认' : '默认模型'}</option>
              {(providerObj?.models || []).map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
          </div>
        </>
      )}

      {/* Composer 本体：大卡片，占位符置顶，控制行沉底（对标桌面版） */}
      <div className="xt-composer-card rounded-2xl border border-[var(--ag-stroke2)] bg-[var(--ag-card)]/90 shadow-[var(--ag-shadow-panel)] backdrop-blur-xl transition-colors focus-within:border-[var(--ag-accent)]">
        <textarea
          ref={textareaRef}
          value={input}
          onChange={(e) => onInputChange(e.target.value)}
          onKeyDown={onKeyDown}
          rows={1}
          placeholder={placeholder}
          aria-label="消息输入框"
          className="min-h-[var(--ag-composer-min-h)] max-h-[var(--ag-composer-max-h)] w-full resize-none border-0 bg-transparent px-3.5 pt-3 text-[13.5px] leading-6 text-[var(--ag-text1)] placeholder:text-[var(--ag-text4)] focus:outline-none focus:ring-0"
        />
        {/* 控制行 */}
        <div className="flex items-center gap-1 px-2 pb-1.5 pt-0.5">
          <input
            ref={fileInputRef}
            type="file"
            accept=".txt,.md,.csv,.json"
            className="hidden"
            aria-label="选择附件"
            onChange={(e) => {
              const f = e.target.files?.[0]
              e.target.value = ''
              if (f) onPickFile(f)
            }}
          />
          <button
            type="button"
            title="添加附件或提示词"
            aria-label="打开附件菜单"
            aria-expanded={plusOpen}
            onClick={() => setPlusOpen((v) => !v)}
            className="flex size-6 items-center justify-center rounded-full text-[var(--ag-text3)] transition-colors hover:bg-black/5 hover:text-[var(--ag-text1)]"
          >
            <Plus size={15} />
          </button>

          {/* 完全权限药丸（点击看用量概览） */}
          <button
            type="button"
            title="权限与用量"
            aria-label="权限与用量"
            onClick={() => onShowUsage?.()}
            className="flex h-6 items-center gap-1 rounded-full border border-transparent px-2 text-[11px] text-[var(--ag-text3)] transition-colors hover:border-[var(--ag-stroke3)] hover:text-[var(--ag-text1)]"
          >
            <Shield size={11} className="shrink-0" />
            完全权限
            <ChevronDown size={11} className="shrink-0" />
          </button>

          <span className="min-w-0 flex-1" />

          {/* 模型药丸（靠右贴发送键，对标 dsh 桌面版） */}
          <button
            type="button"
            title="切换模型"
            aria-label="模型选择"
            aria-expanded={modelOpen}
            onClick={() => setModelOpen((v) => !v)}
            className="flex h-6 max-w-[35%] items-center gap-1 rounded-full border border-transparent px-2 text-[11px] text-[var(--ag-text3)] transition-colors hover:border-[var(--ag-stroke3)] hover:text-[var(--ag-text1)]"
          >
            <span className="truncate">{modelLabel}</span>
            <ChevronDown size={11} className="shrink-0" />
          </button>

          {/* 发送键状态机：忙+空 → Stop；其余可提交态 → 发送（忙时为重定向）；浅蓝圆形（对标 dsh） */}
          {isStreaming && !input.trim() ? (
            <button
              type="button"
              onClick={onStop}
              title="停止生成（Esc）"
              aria-label="停止生成"
              className="flex size-7 items-center justify-center rounded-full bg-[var(--ag-accent)] text-[var(--ag-accent-fg)] transition-opacity hover:opacity-85"
            >
              <SquareTerminal size={13} />
            </button>
          ) : (
            <button
              type="button"
              onClick={submit}
              disabled={!canSubmit}
              title={isStreaming ? '发送并重定向当前回复' : '发送'}
              aria-label="发送消息"
              className="flex size-7 items-center justify-center rounded-full bg-[var(--ag-send-bg)] text-[var(--ag-send-fg)] transition-colors hover:bg-[var(--ag-send-bg-hover)] disabled:opacity-40"
            >
              {isStreaming ? <Layers3 size={13} /> : <ArrowUp size={14} />}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

export default AgentComposer
