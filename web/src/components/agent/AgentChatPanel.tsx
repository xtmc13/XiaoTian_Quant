import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Bot,
  Check,
  ChevronDown,
  CircleAlert,
  Eraser,
  Loader2,
  SendHorizonal,
  Square,
  Wrench,
} from 'lucide-react'
import { agentApi, agentChatApi, type AgentToolCall } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { cn } from '@/lib/utils'

// ── 消息模型 ──
export interface AgentChatMsg {
  role: 'user' | 'assistant' | 'system'
  content: string
  toolCalls?: AgentToolCall[]
  streaming?: boolean
  error?: boolean
}

const HISTORY_KEY = 'agent_chat_history'
const HISTORY_LIMIT = 50

const QUICK_COMMANDS = [
  '分析 BTCUSDT',
  '我的持仓和盈亏',
  '今日 AI 信号',
  '创建一个 BTC 网格机器人（模拟盘）',
]

// ── 最小 markdown 渲染：```代码块``` / `行内代码` / **加粗** / 换行 ──
export function renderMarkdown(content: string): React.ReactNode[] {
  const nodes: React.ReactNode[] = []
  // 先按 ``` 切成代码块与普通文本
  const segments = content.split('```')
  segments.forEach((seg, i) => {
    if (i % 2 === 1) {
      const nl = seg.indexOf('\n')
      const lang = nl === -1 ? '' : seg.slice(0, nl).trim()
      const code = nl === -1 ? seg : seg.slice(nl + 1)
      nodes.push(
        <pre
          key={`code-${i}`}
          className="my-1.5 overflow-x-auto rounded-md bg-black/30 dark:bg-black/40 p-2 text-[11px] leading-relaxed text-[#7dd3fc]"
        >
          <code className={cn(lang && `language-${lang}`)}>{code.replace(/\n$/, '')}</code>
        </pre>
      )
      return
    }
    // 普通文本：按行处理，行内支持 **bold** 与 `code`
    seg.split('\n').forEach((line, j) => {
      if (line === '' && j === seg.split('\n').length - 1) return
      const parts = line
        .split(/(\*\*[^*]+\*\*|`[^`]+`)/g)
        .filter(Boolean)
        .map((part, k) => {
          if (part.startsWith('**') && part.endsWith('**')) {
            return (
              <strong key={k} className="font-semibold">
                {part.slice(2, -2)}
              </strong>
            )
          }
          if (part.startsWith('`') && part.endsWith('`')) {
            return (
              <code key={k} className="rounded bg-black/20 dark:bg-white/10 px-1 py-0.5 text-[11px]">
                {part.slice(1, -1)}
              </code>
            )
          }
          return part
        })
      nodes.push(
        <span key={`line-${i}-${j}`}>
          {parts}
          {j < seg.split('\n').length - 1 && <br />}
        </span>
      )
    })
  })
  return nodes
}

// ── 工具调用中文标签 ──
const TOOL_LABELS: Record<string, string> = {
  get_market: '查询行情',
  get_ticker: '查询行情',
  get_klines: '查询K线',
  get_portfolio: '查询持仓',
  get_positions: '查询持仓',
  get_pnl: '查询盈亏',
  get_signals: '查询AI信号',
  create_grid_bot: '创建网格机器人',
  create_bot: '创建机器人',
  list_bots: '查询机器人列表',
}

function toolLabel(name: string): string {
  return TOOL_LABELS[name] || name
}

function loadHistory(): AgentChatMsg[] {
  try {
    const raw = localStorage.getItem(HISTORY_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed
      .filter((m) => m && typeof m.content === 'string' && (m.role === 'user' || m.role === 'assistant'))
      .map((m) => ({
        role: m.role,
        content: m.content,
        toolCalls: Array.isArray(m.toolCalls) ? m.toolCalls : undefined,
      }))
      .slice(-HISTORY_LIMIT)
  } catch {
    return []
  }
}

export interface AgentChatPanelProps {
  open: boolean
  onClose: () => void
  /** 面板关闭期间收到助手消息时回调（用于 FAB 未读小红点） */
  onUnread?: () => void
}

export function AgentChatPanel({ open, onClose, onUnread }: AgentChatPanelProps) {
  const [messages, setMessages] = useState<AgentChatMsg[]>(loadHistory)
  const [input, setInput] = useState('')
  const [isStreaming, setIsStreaming] = useState(false)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const abortRef = useRef<(() => void) | null>(null)
  const openRef = useRef(open)
  openRef.current = open

  // 头部展示的模型名（读 agent AI 配置，失败则兜底文案）
  const { data: aiConfig } = useQuery({
    queryKey: ['agent', 'ai-config'],
    queryFn: () => agentApi.aiConfig(),
    staleTime: 5 * 60 * 1000,
    retry: false,
  })
  const modelName = aiConfig?.model || '默认模型'

  // 会话历史持久化（最多 50 条）
  useEffect(() => {
    try {
      const toSave = messages
        .filter((m) => m.role === 'user' || m.role === 'assistant')
        .map((m) => ({ role: m.role, content: m.content, toolCalls: m.toolCalls }))
        .slice(-HISTORY_LIMIT)
      localStorage.setItem(HISTORY_KEY, JSON.stringify(toSave))
    } catch {
      /* 存储满等情况静默失败 */
    }
  }, [messages])

  // 展开时滚动到底部并聚焦输入框
  useEffect(() => {
    if (open) {
      messagesEndRef.current?.scrollIntoView?.({ block: 'end' })
      textareaRef.current?.focus()
    }
  }, [open])

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView?.({ block: 'end', behavior: 'smooth' })
  }, [messages, isStreaming])

  // 卸载时若仍在生成则停止
  useEffect(() => {
    return () => abortRef.current?.()
  }, [])

  const patchLastAssistant = useCallback((patch: (m: AgentChatMsg) => AgentChatMsg) => {
    setMessages((prev) => {
      const next = [...prev]
      for (let i = next.length - 1; i >= 0; i--) {
        if (next[i].role === 'assistant') {
          next[i] = patch(next[i])
          break
        }
      }
      return next
    })
  }, [])

  const send = useCallback(
    (text: string) => {
      const content = text.trim()
      if (!content || isStreaming) return

      const history: AgentChatMsg[] = [...messages, { role: 'user', content }]
      setMessages([...history, { role: 'assistant', content: '', streaming: true }])
      setInput('')
      setIsStreaming(true)

      const apiMsgs = history
        .filter((m) => m.role === 'user' || m.role === 'assistant')
        .map((m) => ({ role: m.role, content: m.content }))

      const { abort, promise } = agentChatApi.chat(apiMsgs, {
        onDelta: (delta) => {
          patchLastAssistant((m) => ({ ...m, content: m.content + delta }))
        },
        onToolCall: (toolCall) => {
          patchLastAssistant((m) => {
            const toolCalls = [...(m.toolCalls || [])]
            const idx = toolCalls.findIndex((t) => t.name === toolCall.name && t.status === 'running')
            if (idx >= 0) toolCalls[idx] = { ...toolCalls[idx], ...toolCall }
            else toolCalls.push(toolCall)
            return { ...m, toolCalls }
          })
        },
        onDone: (done) => {
          patchLastAssistant((m) => ({
            ...m,
            content: done.content || m.content,
            toolCalls: done.tool_calls || m.toolCalls,
          }))
        },
        onError: (msg) => {
          const isAuth = msg.includes('401')
          if (!isAuth) toast('error', msg)
          patchLastAssistant((m) => ({ ...m, error: true }))
        },
      })
      abortRef.current = abort

      promise
        .catch(() => {
          // 具体错误已由 onError 处理（或 abort 静默）；此处兜底中断提示
          patchLastAssistant((m) => ({ ...m, error: true }))
        })
        .finally(() => {
          setIsStreaming(false)
          abortRef.current = null
          patchLastAssistant((m) => ({ ...m, streaming: false }))
          // 面板关闭期间收到回复 → 未读提示
          if (!openRef.current) onUnread?.()
        })
    },
    [messages, isStreaming, patchLastAssistant, onUnread]
  )

  const stop = useCallback(() => {
    abortRef.current?.()
  }, [])

  const clearSession = useCallback(() => {
    if (isStreaming) stop()
    setMessages([])
    try {
      localStorage.removeItem(HISTORY_KEY)
    } catch {
      /* ignore */
    }
    toast('info', '会话已清空')
  }, [isStreaming, stop])

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      send(input)
    }
  }

  const quickChips = useMemo(() => QUICK_COMMANDS, [])

  if (!open) return null

  return (
    <div
      role="dialog"
      aria-label="小天助手"
      className={cn(
        'fixed bottom-36 right-5 z-50 flex flex-col md:bottom-24 md:right-6',
        'w-[380px] max-w-[calc(100vw-2.5rem)] h-[560px] max-h-[75vh]',
        'rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden',
        'text-foreground duration-200'
      )}
    >
      {/* ── 头部 ── */}
      <div className="flex items-center gap-2 border-b border-quant-border px-4 py-3 shrink-0">
        <div className="flex h-8 w-8 items-center justify-center rounded-full bg-gradient-to-br from-[#1890ff] to-[#36cfc9]">
          <Bot className="text-white" size={18} />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <span className="text-sm font-semibold">小天助手</span>
            <span className="relative flex h-2 w-2" title="在线">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-[#52c41a] opacity-60" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-[#52c41a]" />
            </span>
          </div>
          <div className="truncate text-[11px] text-muted-foreground">{modelName}</div>
        </div>
        <button
          type="button"
          onClick={clearSession}
          title="清空会话"
          aria-label="清空会话"
          className="rounded-md p-1.5 text-[#888] transition-colors hover:bg-quant-hover hover:text-foreground"
        >
          <Eraser size={15} />
        </button>
        <button
          type="button"
          onClick={onClose}
          title="收起"
          aria-label="收起聊天面板"
          className="rounded-md p-1.5 text-[#888] transition-colors hover:bg-quant-hover hover:text-foreground"
        >
          <ChevronDown size={16} />
        </button>
      </div>

      {/* ── 消息流 ── */}
      <div className="flex-1 min-h-0 overflow-y-auto px-3 py-3 space-y-3">
        {messages.length === 0 && (
          <div className="flex h-full flex-col items-center justify-center gap-2 text-center text-[#666]">
            <Bot size={32} className="opacity-40" />
            <p className="text-xs">你好，我是小天助手。可以问我行情、持仓、信号，</p>
            <p className="text-xs">或直接吩咐我创建交易机器人。</p>
          </div>
        )}
        {messages.map((m, i) =>
          m.role === 'user' ? (
            <div key={i} className="flex justify-end">
              <div className="max-w-[85%] whitespace-pre-wrap break-words rounded-2xl rounded-br-sm bg-[#1890ff] px-3 py-2 text-[13px] leading-relaxed text-white">
                {m.content}
              </div>
            </div>
          ) : (
            <div key={i} className="flex justify-start">
              <div className="max-w-[92%] rounded-2xl rounded-bl-sm border border-quant-border bg-quant-bg-secondary px-3 py-2 text-[13px] leading-relaxed">
                {/* 工具调用卡片 */}
                {m.toolCalls && m.toolCalls.length > 0 && (
                  <div className="mb-1.5 space-y-1">
                    {m.toolCalls.map((t, ti) => (
                      <div
                        key={ti}
                        className="flex items-center gap-1.5 rounded-md bg-black/10 dark:bg-white/5 px-2 py-1 text-[11px] text-[#999]"
                      >
                        {t.status === 'running' ? (
                          <Loader2 size={12} className="shrink-0 animate-spin text-[#1890ff]" />
                        ) : (
                          <Check size={12} className="shrink-0 text-[#52c41a]" />
                        )}
                        <Wrench size={11} className="shrink-0 opacity-60" />
                        <span className="shrink-0 text-foreground/80">{toolLabel(t.name)}</span>
                        {t.args_summary && <span className="truncate opacity-70">{t.args_summary}</span>}
                        {t.status === 'running' ? (
                          <span className="ml-auto shrink-0 animate-pulse">执行中…</span>
                        ) : (
                          <span className="ml-auto shrink-0 text-[#52c41a]">
                            {t.result_summary || '完成'}
                          </span>
                        )}
                      </div>
                    ))}
                  </div>
                )}
                {m.content ? (
                  <div className="break-words">{renderMarkdown(m.content)}</div>
                ) : (
                  m.streaming && <Loader2 size={14} className="animate-spin text-[#1890ff]" />
                )}
                {m.error && (
                  <div className="mt-1 flex items-center gap-1 text-[11px] text-[#f5222d]">
                    <CircleAlert size={12} />
                    连接中断，请重试
                  </div>
                )}
              </div>
            </div>
          )
        )}
        <div ref={messagesEndRef} />
      </div>

      {/* ── 快捷指令 ── */}
      <div className="shrink-0 flex gap-1.5 overflow-x-auto px-3 pb-2 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
        {quickChips.map((cmd) => (
          <button
            key={cmd}
            type="button"
            disabled={isStreaming}
            onClick={() => send(cmd)}
            className="shrink-0 rounded-full border border-quant-border bg-quant-bg-secondary px-2.5 py-1 text-[11px] text-[#aaa] transition-colors hover:border-[#1890ff]/50 hover:text-[#1890ff] disabled:opacity-50"
          >
            {cmd}
          </button>
        ))}
      </div>

      {/* ── 输入区 ── */}
      <div className="shrink-0 border-t border-quant-border p-3">
        <div className="flex items-end gap-2">
          <textarea
            ref={textareaRef}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={onKeyDown}
            rows={1}
            placeholder="输入消息，Enter 发送，Shift+Enter 换行"
            aria-label="消息输入框"
            className="max-h-24 min-h-[36px] flex-1 resize-none rounded-xl border border-quant-border bg-quant-bg-secondary px-3 py-2 text-[13px] text-foreground placeholder:text-[#555] focus:border-[#1890ff]/60 focus:outline-none focus:ring-1 focus:ring-[#1890ff]/30"
          />
          {isStreaming ? (
            <button
              type="button"
              onClick={stop}
              title="停止生成"
              aria-label="停止生成"
              className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-[#f5222d]/10 text-[#f5222d] transition-colors hover:bg-[#f5222d]/20"
            >
              <Square size={14} fill="currentColor" />
            </button>
          ) : (
            <button
              type="button"
              onClick={() => send(input)}
              disabled={!input.trim()}
              title="发送"
              aria-label="发送消息"
              className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-[#1890ff] text-white transition-colors hover:bg-[#40a9ff] disabled:opacity-40"
            >
              <SendHorizonal size={15} />
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

export default AgentChatPanel
