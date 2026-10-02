import React, { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Check, ChevronRight, CircleAlert, Clipboard, Loader2, Pencil, RotateCcw, Wrench, X } from 'lucide-react'
import type { AgentChatMsg } from '../types'
import { copyText, toolLabel } from '../types'
import { MarkdownView } from '../MarkdownView'
import { cn } from '@/lib/utils'

// ── 空态标语（按挂载随机轮换，交易场景文案） ──
const TAGLINES = [
  '丢给我一段行情、一个目标，或整个持仓，我来帮你理清。',
  '可以问我行情、持仓、信号，或直接吩咐我创建交易机器人。',
  '盯盘累了？让我替你看着。',
  '把你的交易想法说出来，我帮你落成策略。',
  '数据、信号、执行——从我这里开始。',
  '一键复盘今日得失，让下一单更干净。',
]

// ── 大字标：fit-text，自动撑满容器宽 ──
function Wordmark({ text }: { text: string }) {
  const boxRef = useRef<HTMLDivElement>(null)
  const textRef = useRef<HTMLSpanElement>(null)
  const [fontSize, setFontSize] = useState(44)

  useLayoutEffect(() => {
    const measure = () => {
      const box = boxRef.current
      const el = textRef.current
      if (!box || !el) return
      const base = 44
      el.style.fontSize = `${base}px`
      const w = el.scrollWidth || 1
      const ratio = (box.clientWidth - 8) / w
      setFontSize(Math.max(44, Math.min(base * ratio, 200)))
    }
    measure()
    window.addEventListener('resize', measure)
    return () => window.removeEventListener('resize', measure)
  }, [text])

  return (
    <div ref={boxRef} className="w-full px-0.5">
      <span
        ref={textRef}
        aria-hidden
        className="block select-none whitespace-nowrap font-bold uppercase leading-[0.9] tracking-[0.08em] text-[var(--ag-accent)]"
        style={{ fontSize, mixBlendMode: 'plus-lighter' }}
      >
        {text}
      </span>
    </div>
  )
}

function EmptyState() {
  const [tagline] = useState(() => TAGLINES[Math.floor(Math.random() * TAGLINES.length)])
  return (
    <div className="flex h-full flex-col items-center justify-center gap-1 px-4 py-6">
      <Wordmark text="小天助手" />
      <p className="mt-2 max-w-md text-center text-[13px] text-[var(--ag-text3)]">{tagline}</p>
    </div>
  )
}

// ── 思考过程（scaffold 行）：pending shimmer + 计时，完成 "思考了 N 秒" ──
function ThinkingDisclosure({ reasoning, streaming }: { reasoning: string; streaming: boolean }) {
  const [open, setOpen] = useState(streaming)
  const [elapsed, setElapsed] = useState(0)

  useEffect(() => {
    if (streaming) setOpen(true)
  }, [streaming])

  useEffect(() => {
    if (!streaming) return
    const start = Date.now()
    const t = setInterval(() => setElapsed(Math.floor((Date.now() - start) / 1000)), 500)
    return () => clearInterval(t)
  }, [streaming])

  const label = streaming
    ? elapsed < 1
      ? '思考中…'
      : `思考中… ${elapsed}s`
    : elapsed < 1 && reasoning.length < 200
      ? '稍作思考'
      : `思考了 ${Math.max(elapsed, 1)} 秒`

  return (
    <div className="mb-1 transition-opacity [opacity:0.67] hover:[opacity:1] focus-within:[opacity:1]">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex items-center gap-1 rounded px-1 py-0.5 text-[11px] text-[var(--ag-text3)] transition-colors hover:bg-white/6"
      >
        <ChevronRight size={12} className={cn('shrink-0 transition-transform duration-150', open && 'rotate-90')} />
        <span className={cn(streaming && 'xt-shimmer-text')}>{label}</span>
      </button>
      {open && reasoning && (
        <div className="mt-0.5 whitespace-pre-wrap break-words pl-6 pr-1 text-[11.5px] leading-relaxed text-[var(--ag-text3)]">
          {reasoning}
        </div>
      )}
    </div>
  )
}

// ── 工具调用（scaffold 行）：运行中 spinner + shimmer，成功静默，可展开 ──
function ToolScaffoldRow({ tool }: { tool: NonNullable<AgentChatMsg['toolCalls']>[number] }) {
  const [open, setOpen] = useState(false)
  const expandable = Boolean(tool.args_summary || tool.result_summary)
  return (
    <div className="mb-0.5 transition-opacity [opacity:0.67] hover:[opacity:1] focus-within:[opacity:1]">
      <div className="flex items-center gap-1.5 rounded-md px-1 py-0.5 text-[11px] text-[var(--ag-text3)]">
        {tool.status === 'running' ? (
          <Loader2 size={12} className="shrink-0 animate-spin text-[var(--ag-accent)]" />
        ) : (
          <Wrench size={12} className="shrink-0 text-[var(--ag-text4)]" />
        )}
        <span className="shrink-0 text-[var(--ag-text2)]">{toolLabel(tool.name)}</span>
        {tool.args_summary && <span className="truncate text-[var(--ag-text4)]">{tool.args_summary}</span>}
        <span className="ml-auto shrink-0 tabular-nums">
          {tool.status === 'running' ? (
            <span className="xt-shimmer-text">执行中…</span>
          ) : (
            tool.result_summary && <span>{tool.result_summary}</span>
          )}
        </span>
        {expandable && (
          <button
            type="button"
            title={open ? '收起详情' : '展开详情'}
            aria-label={open ? `收起 ${toolLabel(tool.name)} 详情` : `展开 ${toolLabel(tool.name)} 详情`}
            aria-expanded={open}
            onClick={() => setOpen((v) => !v)}
            className="shrink-0 rounded p-0.5 text-[var(--ag-text4)] hover:bg-white/6 hover:text-[var(--ag-text2)]"
          >
            <ChevronRight size={11} className={cn('transition-transform duration-150', open && 'rotate-90')} />
          </button>
        )}
      </div>
      {open && (
        <div className="ml-5 mt-0.5 max-h-32 space-y-1 overflow-y-auto rounded-md border border-[var(--ag-stroke3)] bg-[var(--ag-card)] p-2 font-mono text-[10.5px] leading-relaxed">
          {tool.args_summary && (
            <div>
              <span className="text-[var(--ag-text4)]">参数：</span>
              <span className="text-[var(--ag-text2)]">{tool.args_summary}</span>
            </div>
          )}
          {tool.result_summary && (
            <div>
              <span className="text-[var(--ag-text4)]">结果：</span>
              <span className="text-[var(--ag-text2)]">{tool.result_summary}</span>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ── 用户消息：全宽 sticky 气泡（>2 行 clamp + 渐隐，hover 展开） ──
function UserMessage({
  msg,
  isStreaming,
  onEdit,
}: {
  msg: AgentChatMsg
  isStreaming: boolean
  onEdit: (content: string) => void
}) {
  const [editing, setEditing] = useState(false)
  const [editValue, setEditValue] = useState(msg.content)

  if (editing) {
    return (
      <div className="pt-1">
        <textarea
          value={editValue}
          onChange={(e) => setEditValue(e.target.value)}
          rows={3}
          aria-label="编辑消息"
          className="w-full resize-none rounded-xl border border-[var(--ag-accent)]/50 bg-[var(--ag-card)] px-3 py-2 text-[13px] text-[var(--ag-text1)] focus:outline-none"
        />
        <div className="mt-1 flex justify-end gap-1.5">
          <button
            type="button"
            onClick={() => {
              setEditing(false)
              setEditValue(msg.content)
            }}
            className="flex items-center gap-1 rounded-md border border-[var(--ag-stroke2)] px-2 py-1 text-[11px] text-[var(--ag-text3)] hover:bg-white/6"
          >
            <X size={11} />
            取消
          </button>
          <button
            type="button"
            disabled={!editValue.trim() || isStreaming}
            onClick={() => {
              setEditing(false)
              onEdit(editValue.trim())
            }}
            className="rounded-md bg-[var(--ag-text1)] px-2.5 py-1 text-[11px] text-[var(--ag-bg)] hover:opacity-85 disabled:opacity-40"
          >
            发送
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="xt-sticky-human xt-human-clamp group relative pt-1">
      <div className="xt-human-bubble w-full rounded-xl border border-[var(--ag-stroke2)] bg-[var(--ag-user-bubble)] px-3 py-2 transition-colors group-hover:border-[var(--ag-stroke1)]">
        <div className="xt-human-bubble-text whitespace-pre-wrap break-words text-[13px] leading-relaxed text-[var(--ag-text1)]">
          {msg.content}
        </div>
      </div>
      <button
        type="button"
        title="编辑并重新发送"
        aria-label="编辑并重新发送"
        disabled={isStreaming}
        onClick={() => {
          setEditValue(msg.content)
          setEditing(true)
        }}
        className="absolute bottom-1.5 right-2 rounded bg-[var(--ag-card)]/90 p-1 text-[var(--ag-text3)] opacity-0 shadow-sm transition-opacity group-hover:opacity-100 hover:text-[var(--ag-text1)] disabled:opacity-0"
      >
        <Pencil size={11} />
      </button>
    </div>
  )
}

// ── 助手消息：全宽裸文本 + scaffold + hover 操作 ──
function AssistantMessage({
  msg,
  isLast,
  isStreaming,
  onRegenerate,
}: {
  msg: AgentChatMsg
  isLast: boolean
  isStreaming: boolean
  onRegenerate: () => void
}) {
  const [copied, setCopied] = useState(false)

  const doCopy = async () => {
    if (await copyText(msg.content)) {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    }
  }

  const thinking = msg.streaming ? msg.reasoning || '' : msg.reasoning || ''
  const bareActivity =
    isLast && isStreaming && !msg.content && !msg.reasoning && !(msg.toolCalls && msg.toolCalls.length > 0)

  return (
    <div className="group pt-2">
      {thinking && <ThinkingDisclosure reasoning={thinking} streaming={Boolean(msg.streaming)} />}
      {msg.toolCalls && msg.toolCalls.length > 0 && (
        <div className="mb-1 space-y-0.5">
          {msg.toolCalls.map((t, ti) => (
            <ToolScaffoldRow key={`${t.name}-${ti}`} tool={t} />
          ))}
        </div>
      )}
      {msg.content ? (
        <div className="text-[13px] leading-relaxed text-[var(--ag-text1)] [&_a]:text-[var(--ag-accent)] [&_code]:rounded [&_code]:bg-[var(--ag-inline-code-bg)] [&_code]:px-1 [&_code]:py-0.5 [&_code]:text-[12px] [&_code]:text-[var(--ag-inline-code-fg)]">
          <MarkdownView content={msg.content} />
        </div>
      ) : (
        bareActivity && (
          <div className="flex items-center gap-1 py-1" aria-label="正在生成">
            {[0, 1, 2].map((i) => (
              <span
                key={i}
                className="xt-activity-dot size-1.5 rounded-full bg-[var(--ag-text3)]"
                style={{ animationDelay: `${i * 0.2}s` }}
              />
            ))}
          </div>
        )
      )}
      {msg.error && (
        <div className="mt-1 flex items-center gap-1.5 rounded-md border border-[var(--ag-red)]/25 bg-[var(--ag-red)]/6 px-2 py-1.5 text-[11px] text-[var(--ag-red)]">
          <CircleAlert size={12} />
          连接中断，请重试
          <button
            type="button"
            aria-label="重新生成回复"
            onClick={onRegenerate}
            className="ml-auto flex items-center gap-1 rounded px-1.5 py-0.5 font-medium hover:bg-[var(--ag-red)]/10"
          >
            <RotateCcw size={11} />
            重试
          </button>
        </div>
      )}
      {/* hover 操作条（右对齐） */}
      {(msg.content || msg.toolCalls?.length) && !msg.streaming && !msg.error && (
        <div className="mt-0.5 flex justify-end gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
          <button
            type="button"
            title={copied ? '已复制' : '复制'}
            aria-label={copied ? '已复制' : '复制回复'}
            onClick={doCopy}
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-white/6 hover:text-[var(--ag-text1)]"
          >
            {copied ? <Check size={12} className="text-[var(--ag-green)]" /> : <Clipboard size={12} />}
          </button>
          <button
            type="button"
            title="重新生成"
            aria-label="重新生成回复"
            disabled={isStreaming}
            onClick={onRegenerate}
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-white/6 hover:text-[var(--ag-text1)] disabled:opacity-40"
          >
            <RotateCcw size={12} />
          </button>
        </div>
      )}
    </div>
  )
}

export interface AgentThreadProps {
  messages: AgentChatMsg[]
  isStreaming: boolean
  onRegenerate: () => void
  onEdit: (index: number, content: string) => void
  messagesEndRef: React.RefObject<HTMLDivElement | null>
}

// ── 消息流：与 composer 同宽居中 ──
export function AgentThread({ messages, isStreaming, onRegenerate, onEdit, messagesEndRef }: AgentThreadProps) {
  return (
    <div className="xt-thread-scroll min-h-0 flex-1 overflow-y-auto" aria-label="消息流">
      <div className="mx-auto flex min-h-full w-full max-w-3xl flex-col px-6 pb-40 pt-2">
        {messages.length === 0 ? (
          <EmptyState />
        ) : (
          messages.map((m, i) =>
            m.role === 'user' ? (
              <UserMessage key={m.id || i} msg={m} isStreaming={isStreaming} onEdit={(content) => onEdit(i, content)} />
            ) : (
              <AssistantMessage
                key={m.id || i}
                msg={m}
                isLast={i === messages.length - 1}
                isStreaming={isStreaming}
                onRegenerate={onRegenerate}
              />
            )
          )
        )}
        <div ref={messagesEndRef} />
      </div>
    </div>
  )
}

export default AgentThread
