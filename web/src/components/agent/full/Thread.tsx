import React, { useEffect, useState } from 'react'
import {
  Check,
  ChevronDown,
  ChevronRight,
  CircleAlert,
  Clipboard,
  Folder,
  GitBranch,
  Loader2,
  Pencil,
  RotateCcw,
  Shield,
  ShieldAlert,
  Wrench,
  X,
  ThumbsDown,
  ThumbsUp,
  Sprout,
  Undo2,
  Volume2,
  VolumeX,
} from 'lucide-react'
import type { AgentApprovalRequest } from '@/lib/api'
import type { AgentChatMsg } from '../types'
import { copyText, toolLabel } from '../types'
import { speakText, stopSpeak, ttsSupported } from '../useTts'
import { MarkdownView } from '../MarkdownView'
import { DefaultPet } from '../pet/DefaultPet'
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

export function AgentEmptyState() {
  const [tagline] = useState(() => TAGLINES[Math.floor(Math.random() * TAGLINES.length)])
  return (
    <div className="flex flex-col items-center gap-2 px-4 py-6">
      {/* 鲸鱼 + 标语（与登录页标语一致） */}
      <div className="flex items-center gap-2.5">
        <DefaultPet size={40} />
        <span className="text-[22px] font-bold tracking-tight text-[var(--ag-text1)]">AI 驱动的量化交易平台</span>
        <span className="rounded-full bg-[var(--ag-accent)]/10 px-1.5 py-0.5 text-[10px] font-medium text-[var(--ag-accent)]">
          预览版
        </span>
      </div>
      <p className="max-w-md text-center text-[12px] text-[var(--ag-text3)]">{tagline}</p>
      {/* 面包屑药丸：工作区 / 模式 / 工作树（对标桌面版） */}
      <div className="mt-4 flex items-center gap-2">
        <span className="flex items-center gap-1 rounded-lg border border-[var(--ag-stroke3)] bg-[var(--ag-card)]/70 px-2.5 py-1.5 text-[12px] text-[var(--ag-text2)]">
          <Folder size={13} className="text-[var(--ag-accent)]" />
          小天量化
          <ChevronDown size={12} className="text-[var(--ag-text4)]" />
        </span>
        <span className="flex items-center gap-1 rounded-lg border border-[var(--ag-stroke3)] bg-[var(--ag-card)]/70 px-2.5 py-1.5 text-[12px] text-[var(--ag-text2)]">
          <Shield size={13} className="text-[var(--ag-accent)]" />
          标准模式
          <ChevronDown size={12} className="text-[var(--ag-text4)]" />
        </span>
        <span className="flex items-center gap-1 rounded-lg border border-[var(--ag-stroke3)] bg-[var(--ag-card)]/70 px-2.5 py-1.5 text-[12px] text-[var(--ag-text2)]">
          <Sprout size={13} className="text-[var(--ag-accent)]" />
          新建工作树
          <ChevronDown size={12} className="text-[var(--ag-text4)]" />
        </span>
      </div>
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
    <div className="mb-1.5 transition-opacity [opacity:0.67] hover:[opacity:1] focus-within:[opacity:1]">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex items-center gap-1 rounded px-1 py-0.5 text-[11px] text-[var(--ag-text3)] transition-colors hover:bg-black/5"
      >
        <DefaultPet size={14} />
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
    <div className="mb-1 transition-opacity [opacity:0.67] hover:[opacity:1] focus-within:[opacity:1]">
      <div className="flex items-center gap-1.5 rounded-md px-1 py-0.5 text-[11.5px] text-[var(--ag-text3)]">
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
            className="shrink-0 rounded p-0.5 text-[var(--ag-text4)] hover:bg-black/5 hover:text-[var(--ag-text2)]"
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

// ── 工具调用审批卡：回合暂停等待用户决定（批准/拒绝） ──
function ApprovalCard({
  approval,
  onResolve,
}: {
  approval: AgentApprovalRequest
  onResolve: (approve: boolean) => void
}) {
  return (
    <div
      role="alertdialog"
      aria-label="工具调用审批"
      className="mt-3 rounded-xl border border-[var(--ag-amber)]/40 bg-[var(--ag-amber)]/8 px-3.5 py-3"
    >
      <div className="flex items-center gap-1.5 text-[12.5px] font-medium text-[var(--ag-text1)]">
        <ShieldAlert size={14} className="shrink-0 text-[var(--ag-amber)]" />
        等待批准：{toolLabel(approval.tool)}
      </div>
      {approval.args_summary && (
        <div className="mt-1 break-words pl-5 text-[11.5px] leading-relaxed text-[var(--ag-text3)]">
          {approval.args_summary}
        </div>
      )}
      <div className="mt-2.5 flex gap-1.5 pl-5">
        <button
          type="button"
          aria-label="批准工具调用"
          onClick={() => onResolve(true)}
          className="flex items-center gap-1 rounded-lg bg-[var(--ag-accent)] px-3 py-1 text-[12px] font-medium text-[var(--ag-accent-fg)] transition-opacity hover:opacity-85"
        >
          <Check size={12} />
          批准
        </button>
        <button
          type="button"
          aria-label="拒绝工具调用"
          onClick={() => onResolve(false)}
          className="flex items-center gap-1 rounded-lg border border-[var(--ag-stroke2)] px-3 py-1 text-[12px] text-[var(--ag-text2)] transition-colors hover:bg-black/5"
        >
          <X size={12} />
          拒绝
        </button>
      </div>
    </div>
  )
}

// ── 用户消息：全宽 sticky 气泡（>2 行 clamp + 渐隐，hover 展开） ──
function UserMessage({
  msg,
  isStreaming,
  onEdit,
  onBranch,
}: {
  msg: AgentChatMsg
  isStreaming: boolean
  onEdit: (content: string) => void
  /** 从此条用户消息分叉新会话（user+assistant 列表下标已由外层换算） */
  onBranch?: () => void
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
            className="flex items-center gap-1 rounded-md border border-[var(--ag-stroke2)] px-2 py-1 text-[11px] text-[var(--ag-text3)] hover:bg-black/5"
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

  // 桌面版：用户消息为右对齐浅蓝气泡，气泡下方右侧小复制/编辑图标
  return (
    <div className="group flex flex-col items-end pt-3">
      <div className="xt-human-clamp max-w-[85%]">
         <div className="xt-human-bubble rounded-[1.125rem] bg-[var(--ag-user-bubble)] px-3.5 py-2 transition-colors group-hover:bg-[var(--ag-user-bubble-hover)]">
          <div className="xt-human-bubble-text whitespace-pre-wrap break-words text-[13px] leading-relaxed text-[var(--ag-text1)]">
            {msg.content}
          </div>
        </div>
      </div>
      <span className="mt-0.5 flex items-center gap-0.5 pr-1 opacity-0 transition-opacity group-hover:opacity-100">
        <button
          type="button"
          title="复制"
          aria-label="复制用户消息"
          onClick={() => void copyText(msg.content)}
          className="rounded p-0.5 text-[var(--ag-text4)] hover:text-[var(--ag-text2)]"
        >
          <Clipboard size={11} />
        </button>
        <button
          type="button"
          title="编辑并重新发送"
          aria-label="编辑并重新发送"
          disabled={isStreaming}
          onClick={() => {
            setEditValue(msg.content)
            setEditing(true)
          }}
          className="rounded p-0.5 text-[var(--ag-text4)] hover:text-[var(--ag-text2)] disabled:opacity-0"
        >
          <Pencil size={11} />
        </button>
        {onBranch && (
          <button
            type="button"
            title="从此处分叉"
            aria-label="从此处分叉"
            disabled={isStreaming}
            onClick={onBranch}
            className="rounded p-0.5 text-[var(--ag-text4)] hover:text-[var(--ag-text2)] disabled:opacity-0"
          >
            <GitBranch size={11} />
          </button>
        )}
      </span>
    </div>
  )
}

// ── 助手消息：全宽裸文本 + scaffold + hover 操作 ──
function AssistantMessage({
  msg,
  isLast,
  isStreaming,
  onRegenerate,
  showUndo,
  onUndo,
}: {
  msg: AgentChatMsg
  isLast: boolean
  isStreaming: boolean
  onRegenerate: () => void
  showUndo?: boolean
  onUndo?: () => void
}) {
  const [copied, setCopied] = useState(false)
  const [feedback, setFeedback] = useState<'up' | 'down' | null>(null)
  const [speaking, setSpeaking] = useState(false)

  const doCopy = async () => {
    if (await copyText(msg.content)) {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    }
  }

  // 手动朗读本条回复；再次点击停止（cancel 会触发 onend 复位状态）
  const toggleSpeak = () => {
    if (speaking) {
      stopSpeak()
      setSpeaking(false)
      return
    }
    if (speakText(msg.content, { onend: () => setSpeaking(false) })) setSpeaking(true)
  }

  const thinking = msg.streaming ? msg.reasoning || '' : msg.reasoning || ''
  const bareActivity =
    isLast && isStreaming && !msg.content && !msg.reasoning && !(msg.toolCalls && msg.toolCalls.length > 0)

  return (
    <div className="group pt-3">
      {thinking && <ThinkingDisclosure reasoning={thinking} streaming={Boolean(msg.streaming)} />}
      {msg.toolCalls && msg.toolCalls.length > 0 && (
        <div className="mb-1.5 space-y-1">
          {msg.toolCalls.map((t, ti) => (
            <ToolScaffoldRow key={`${t.name}-${ti}`} tool={t} />
          ))}
        </div>
      )}
      {msg.content ? (
        <div className="text-[13.5px] leading-7 text-[var(--ag-text1)] [&_a]:text-[var(--ag-accent)] [&_code]:rounded [&_code]:bg-[var(--ag-inline-code-bg)] [&_code]:px-1 [&_code]:py-0.5 [&_code]:text-[12px] [&_code]:text-[var(--ag-inline-code-fg)]">
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
      {/* 操作条：左下常显，弱化至 hover 提亮（对标桌面版复制/👍/👎/↻） */}
      {(msg.content || msg.toolCalls?.length) && !msg.streaming && !msg.error && (
        <div className="mt-1 flex gap-0.5 opacity-45 transition-opacity hover:opacity-100">
          <button
            type="button"
            title={copied ? '已复制' : '复制'}
            aria-label={copied ? '已复制' : '复制回复'}
            onClick={doCopy}
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-black/5 hover:text-[var(--ag-text1)]"
          >
            {copied ? <Check size={12} className="text-[var(--ag-green)]" /> : <Clipboard size={12} />}
          </button>
          {ttsSupported() && (
            <button
              type="button"
              title={speaking ? '停止朗读' : '朗读'}
              aria-label={speaking ? '停止朗读' : '朗读回复'}
              aria-pressed={speaking}
              onClick={toggleSpeak}
              className={cn(
                'rounded p-1 hover:bg-black/5',
                speaking ? 'text-[var(--ag-accent)]' : 'text-[var(--ag-text3)] hover:text-[var(--ag-text1)]'
              )}
            >
              {speaking ? <VolumeX size={12} /> : <Volume2 size={12} />}
            </button>
          )}
          <button
            type="button"
            title="有帮助"
            aria-label="点赞回复"
            aria-pressed={feedback === 'up'}
            onClick={() => setFeedback(feedback === 'up' ? null : 'up')}
            className={cn(
              'rounded p-1 hover:bg-black/5',
              feedback === 'up' ? 'text-[var(--ag-accent)]' : 'text-[var(--ag-text3)] hover:text-[var(--ag-text1)]'
            )}
          >
            <ThumbsUp size={12} fill={feedback === 'up' ? 'currentColor' : 'none'} />
          </button>
          <button
            type="button"
            title="没帮助"
            aria-label="点踩回复"
            aria-pressed={feedback === 'down'}
            onClick={() => setFeedback(feedback === 'down' ? null : 'down')}
            className={cn(
              'rounded p-1 hover:bg-black/5',
              feedback === 'down' ? 'text-[var(--ag-red)]' : 'text-[var(--ag-text3)] hover:text-[var(--ag-text1)]'
            )}
          >
            <ThumbsDown size={12} fill={feedback === 'down' ? 'currentColor' : 'none'} />
          </button>
          <button
            type="button"
            title="重新生成"
            aria-label="重新生成回复"
            disabled={isStreaming}
            onClick={onRegenerate}
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-black/5 hover:text-[var(--ag-text1)] disabled:opacity-40"
          >
            <RotateCcw size={12} />
          </button>
          {showUndo && onUndo && (
            <button
              type="button"
              title="撤销这一轮"
              aria-label="撤销这一轮"
              disabled={isStreaming}
              onClick={onUndo}
              className="rounded p-1 text-[var(--ag-text3)] hover:bg-black/5 hover:text-[var(--ag-text1)] disabled:opacity-40"
            >
              <Undo2 size={12} />
            </button>
          )}
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
  /** 撤销最后一轮（仅最后一条助手消息上显示入口） */
  onUndo?: () => void
  messagesEndRef: React.RefObject<HTMLDivElement | null>
  /** 待审批的工具调用（展示在消息流尾部，回合暂停中） */
  pendingApproval?: AgentApprovalRequest | null
  onResolveApproval?: (approve: boolean) => void
  /** 自动压缩提示（条数）：一次性灰色提示，插在在途消息之前 */
  compressedNotice?: number | null
  /** 从某条用户消息分叉（参数为 user+assistant 列表下标） */
  onBranch?: (uaIndex: number) => void
}

// ── 消息流：与 composer 同宽居中 ──
export function AgentThread({
  messages,
  isStreaming,
  onRegenerate,
  onEdit,
  onUndo,
  messagesEndRef,
  pendingApproval,
  onResolveApproval,
  compressedNotice,
  onBranch,
}: AgentThreadProps) {
  let lastAssistantIdx = -1
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].role === 'assistant') {
      lastAssistantIdx = i
      break
    }
  }
  // 压缩提示插入点：流式中插在在途消息（最后一条）之前，否则追加到末尾
  const noticeIdx = isStreaming ? Math.max(messages.length - 1, 0) : messages.length
  const notice =
    compressedNotice != null ? (
      <div className="pt-2 text-center text-[11px] text-[var(--ag-text4)]">
        （已自动压缩 {compressedNotice} 条早期消息）
      </div>
    ) : null
  // 每条消息在 user+assistant 列表中的下标（分叉用）
  const uaIndices: number[] = []
  {
    let n = 0
    for (const m of messages) {
      if (m.role === 'user' || m.role === 'assistant') uaIndices.push(n++)
      else uaIndices.push(-1)
    }
  }
  return (
    <div className="xt-thread-scroll min-h-0 flex-1 overflow-y-auto" aria-label="消息流">
      <div className="mx-auto flex h-full w-full max-w-[var(--ag-content-max-w)] flex-col px-6 pb-44 pt-2">
        {messages.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center">
            <AgentEmptyState />
          </div>
        ) : (
          <>
            {messages.map((m, i) => (
              <React.Fragment key={m.id || i}>
                {notice && i === noticeIdx && notice}
                {m.role === 'user' ? (
                  <UserMessage
                    msg={m}
                    isStreaming={isStreaming}
                    onEdit={(content) => onEdit(i, content)}
                    onBranch={onBranch && uaIndices[i] >= 0 ? () => onBranch(uaIndices[i]) : undefined}
                  />
                ) : (
                  <AssistantMessage
                    msg={m}
                    isLast={i === messages.length - 1}
                    isStreaming={isStreaming}
                    onRegenerate={onRegenerate}
                    showUndo={i === lastAssistantIdx && !m.streaming && !m.error}
                    onUndo={onUndo}
                  />
                )}
              </React.Fragment>
            ))}
            {notice && noticeIdx >= messages.length && notice}
            {pendingApproval && onResolveApproval && (
              <ApprovalCard approval={pendingApproval} onResolve={onResolveApproval} />
            )}
          </>
        )}
        <div ref={messagesEndRef} />
      </div>
    </div>
  )
}

export default AgentThread
