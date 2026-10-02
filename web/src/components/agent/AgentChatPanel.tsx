import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Bot,
  ChevronDown,
  CircleAlert,
  History,
  Paperclip,
  SendHorizonal,
  Settings,
  Square,
  SquarePen,
  X,
} from 'lucide-react'
import {
  agentChatApi,
  agentConversationApi,
  configApi,
  type AgentApprovalRequest,
  type AgentChatMessage,
  type AgentConversationDetail,
  type AgentConversationSummary,
} from '@/lib/api'
import { toast } from '@/lib/useToast'
import { cn } from '@/lib/utils'
import { HistorySidebar } from './HistorySidebar'
import { MessageItem } from './MessageItem'
import { SettingsPopover } from './SettingsPopover'
import { QUICK_COMMANDS, loadSettings, saveSettings, type AgentChatMsg, type AgentSettings } from './types'
import { loadTtsEnabled, speakText } from './useTts'
import { HermesChat } from './full/HermesChat'

// ── 附件限制 ──
const ATTACH_MAX_BYTES = 100 * 1024
const ATTACH_EXTS = ['.txt', '.md', '.csv', '.json']

interface Attachment {
  name: string
  content: string
}

function toApiMsgs(msgs: AgentChatMsg[]): AgentChatMessage[] {
  return msgs
    .filter((m) => m.role === 'user' || m.role === 'assistant')
    .map((m) => ({ role: m.role, content: m.content }))
}

function mapConversation(detail: AgentConversationDetail): AgentChatMsg[] {
  return (detail.messages || [])
    .filter((m) => m.role === 'user' || m.role === 'assistant')
    .map((m) => ({
      id: m.id,
      role: m.role as AgentChatMsg['role'],
      content: m.content,
      reasoning: m.reasoning || undefined,
      toolCalls: m.tool_calls,
    }))
}

export interface AgentChatPanelProps {
  open: boolean
  onClose: () => void
  /** 面板关闭期间收到助手消息时回调（用于 FAB 未读小红点） */
  onUnread?: () => void
  /** float=右下角悬浮小面板（默认）；full=填满外层容器（FAB 全屏展开时用） */
  variant?: 'float' | 'full'
}

export function AgentChatPanel({ open, onClose, onUnread, variant = 'float' }: AgentChatPanelProps) {
  const queryClient = useQueryClient()
  const [messages, setMessages] = useState<AgentChatMsg[]>([])
  const [currentId, setCurrentId] = useState<string | null>(null)
  const [input, setInput] = useState('')
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const [isStreaming, setIsStreaming] = useState(false)
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [settings, setSettings] = useState<AgentSettings>(loadSettings)
  // 工具调用审批：SSE approval_request 到达后回合暂停，等待用户决定
  const [pendingApproval, setPendingApproval] = useState<AgentApprovalRequest | null>(null)
  // 自动压缩提示（SSE compressed）：一次性灰色提示，下一回合开始时清除
  const [compressedNotice, setCompressedNotice] = useState<number | null>(null)
  // MoA 多模型综合（/moa）：仅作用于下一条发送的消息，发送后自动清除
  const [moaNext, setMoaNext] = useState(false)
  // MoA 提示（SSE moa）：流式状态条内的一次性提示，下一回合开始时清除
  const [moaNotice, setMoaNotice] = useState<string | null>(null)
  // 排队消息：忙时 Enter 发送进入队列，当前回合结束后自动接续
  const [queued, setQueuedState] = useState<string[]>([])
  // 旁问（/btw）：独立流式，不写入 messages、不落库
  const [btwQuestion, setBtwQuestion] = useState<string | null>(null)
  const [btwAnswer, setBtwAnswer] = useState('')
  const [btwStreaming, setBtwStreaming] = useState(false)
  const [btwError, setBtwError] = useState<string | null>(null)

  const messagesEndRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const abortRef = useRef<(() => void) | null>(null)
  const openRef = useRef(open)
  openRef.current = open
  // 流式期间禁止用（可能滞后的）服务端会话数据覆盖本地消息
  const skipSyncRef = useRef(false)
  const streamingIndexRef = useRef(-1)
  // 打断重定向：忙时发送的新输入，当前回合中断后在 finally 里接续
  const steerTextRef = useRef<string | null>(null)
  // 队列以 ref 为真源（finally 里读取最新值），state 仅用于渲染
  const queueRef = useRef<string[]>([])
  const btwAbortRef = useRef<(() => void) | null>(null)
  // 回合结束后后端异步用 LLM 升级会话标题：8s 后单次刷新会话列表（防叠堆定时器）
  const titleRefreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  // messages 的实时镜像（供 finally 里安全读取最新历史）
  const messagesRef = useRef<AgentChatMsg[]>(messages)
  useEffect(() => {
    messagesRef.current = messages
  }, [messages])

  const setQueued = useCallback((next: string[]) => {
    queueRef.current = next
    setQueuedState(next)
  }, [])

  // 切换/新建会话时清理审批、压缩提示、队列与旁问
  const clearTransient = useCallback(() => {
    setPendingApproval(null)
    setCompressedNotice(null)
    setMoaNext(false)
    setMoaNotice(null)
    setQueued([])
    btwAbortRef.current?.()
    btwAbortRef.current = null
    setBtwQuestion(null)
    setBtwAnswer('')
    setBtwStreaming(false)
    setBtwError(null)
  }, [setQueued])

  // ── 会话列表 ──
  const { data: conversations = [] } = useQuery({
    queryKey: ['agent-conversations'],
    queryFn: async () => {
      const res = await agentConversationApi.list(50, 0)
      return res.conversations || []
    },
    staleTime: 30 * 1000,
    retry: false,
  })

  // ── 当前会话详情（切会话时加载消息） ──
  const conversationQuery = useQuery({
    queryKey: ['agent-conversation', currentId],
    queryFn: () => agentConversationApi.get(currentId as string),
    enabled: !!currentId,
    staleTime: 60 * 1000,
    retry: false,
  })

  // 服务端会话数据 → 本地消息（流式或刚本地建会话时跳过）
  useEffect(() => {
    const detail = conversationQuery.data
    if (!detail || isStreaming || skipSyncRef.current) return
    setMessages(mapConversation(detail))
  }, [conversationQuery.data, isStreaming])

  // ── AI 模型目录（两级选择器） ──
  const { data: aiModels } = useQuery({
    queryKey: ['agent-ai-models'],
    queryFn: configApi.getAIModels,
    staleTime: 5 * 60 * 1000,
    retry: false,
  })
  // configured=true 的厂商排在前面
  const providers = useMemo(() => {
    const list = aiModels?.providers || []
    return [...list].sort((a, b) => Number(b.configured) - Number(a.configured))
  }, [aiModels])
  const selectedProvider = settings.model.split(':')[0] || ''
  const selectedModel = settings.model.split(':')[1] || ''
  const providerObj = providers.find((p) => p.key === selectedProvider)

  const updateSettings = useCallback((next: AgentSettings) => {
    setSettings(next)
    saveSettings(next)
  }, [])

  // 展开时滚动到底部并聚焦输入框
  useEffect(() => {
    if (open) {
      messagesEndRef.current?.scrollIntoView?.({ block: 'end' })
      textareaRef.current?.focus()
    }
  }, [open])

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView?.({ block: 'end', behavior: 'smooth' })
  }, [messages, isStreaming, open])

  // 卸载时若仍在生成则停止
  useEffect(() => {
    return () => {
      abortRef.current?.()
      btwAbortRef.current?.()
      if (titleRefreshTimerRef.current) clearTimeout(titleRefreshTimerRef.current)
    }
  }, [])

  // ── 流式 patch：定位到占位消息 ──
  const patchPlaceholder = useCallback((patch: (m: AgentChatMsg) => AgentChatMsg) => {
    setMessages((prev) => {
      const idx = streamingIndexRef.current
      if (idx < 0 || idx >= prev.length) return prev
      const next = [...prev]
      next[idx] = patch(next[idx])
      return next
    })
  }, [])

  const upsertConversationCache = useCallback(
    (conv: { id: string; title: string }) => {
      queryClient.setQueryData<AgentConversationSummary[]>(['agent-conversations'], (old) => {
        const list = old || []
        const idx = list.findIndex((c) => c.id === conv.id)
        if (idx >= 0) {
          const next = [...list]
          next[idx] = { ...next[idx], title: conv.title, updated_at: Date.now() }
          return next
        }
        return [{ id: conv.id, title: conv.title, updated_at: Date.now() }, ...list]
      })
    },
    [queryClient]
  )

  // ── 统一的发起流式请求入口 ──
  const startStream = useCallback(
    (
      localMsgs: AgentChatMsg[],
      apiMsgs: AgentChatMessage[],
      opts: { regenerate?: boolean; replace_history?: boolean; moa?: boolean }
    ) => {
      setMessages(localMsgs)
      setInput('')
      setAttachments([])
      setIsStreaming(true)
      // 新回合开始：上一回合的压缩提示、MoA 提示与未决审批不再适用
      setCompressedNotice(null)
      setMoaNotice(null)
      setPendingApproval(null)

      const { abort, promise } = agentChatApi.chat(
        {
          messages: apiMsgs,
          conversation_id: currentId ?? undefined,
          model: settings.model || undefined,
          regenerate: opts.regenerate || undefined,
          replace_history: opts.replace_history || undefined,
          moa: opts.moa || undefined,
          system_prompt: settings.system_prompt || undefined,
          temperature: settings.temperature,
          stream: true,
        },
        {
          onDelta: (delta) => {
            patchPlaceholder((m) => ({ ...m, content: m.content + delta }))
          },
          onReasoning: (delta) => {
            patchPlaceholder((m) => ({ ...m, reasoning: (m.reasoning || '') + delta }))
          },
          onToolCall: (toolCall) => {
            patchPlaceholder((m) => {
              const toolCalls = [...(m.toolCalls || [])]
              const idx = toolCalls.findIndex((t) => t.name === toolCall.name && t.status === 'running')
              if (idx >= 0) toolCalls[idx] = { ...toolCalls[idx], ...toolCall }
              else toolCalls.push(toolCall)
              return { ...m, toolCalls }
            })
          },
          onApprovalRequest: (req) => {
            // 回合暂停等待决定；流式状态保持，卡片在消息流尾部展示
            setPendingApproval(req)
          },
          onCompressed: (evt) => {
            setCompressedNotice(evt.compressed_count)
          },
          onMoa: (evt) => {
            // note 始终 toast（含 proposers 为空 = 回退普通回复的说明）
            if (evt.note) toast('info', evt.note)
            // 仅 ≥2 家提案模型才算真正的 MoA 综合，显示在流式状态条
            if (evt.proposers.length >= 2) {
              setMoaNotice(`综合 ${evt.proposers.length} 家模型：${evt.proposers.join('、')}`)
            }
          },
          onConversation: (conv) => {
            // 新会话由后端在 done 前下发 id/title，以此为准
            skipSyncRef.current = true
            setCurrentId(conv.id)
            upsertConversationCache(conv)
          },
          onDone: (done) => {
            setPendingApproval(null)
            if (done.conversation_id) {
              skipSyncRef.current = true
              setCurrentId(done.conversation_id)
            }
            patchPlaceholder((m) => ({
              ...m,
              content: done.content || m.content,
              toolCalls: done.tool_calls || m.toolCalls,
              reasoning: done.reasoning || m.reasoning,
              usage: done.usage || m.usage,
              // done 即本条消息完结：立即清除流式标志（reasoning 随 done 到达时
              // ThinkingDisclosure 此刻才挂载，若等 promise.finally 会残留"思考中"计时）
              streaming: false,
            }))
            // 朗读助手回复（设置开启时；浏览器不支持 speechSynthesis 则静默跳过）
            if (loadTtsEnabled()) speakText(done.content || '')
            // 标题可能由后端异步用 LLM 升级：8s 后单次刷新会话列表（清除旧定时器防叠堆）
            if (titleRefreshTimerRef.current) clearTimeout(titleRefreshTimerRef.current)
            titleRefreshTimerRef.current = setTimeout(() => {
              titleRefreshTimerRef.current = null
              queryClient.invalidateQueries({ queryKey: ['agent-conversations'] })
            }, 8000)
          },
          onError: (msg) => {
            setPendingApproval(null)
            if (!msg.includes('401')) toast('error', msg)
            patchPlaceholder((m) => ({ ...m, error: true }))
          },
        }
      )
      abortRef.current = abort

      promise
        .catch(() => {
          patchPlaceholder((m) => ({ ...m, error: true }))
        })
        .finally(() => {
          setIsStreaming(false)
          abortRef.current = null
          setPendingApproval(null)
          // 重定向/排队待发：本回合的占位清理不得重置流式游标（后续 patch 还要用它）
          const willFollowUp = Boolean(steerTextRef.current) || queueRef.current.length > 0
          setMessages((prev) => {
            const idx = streamingIndexRef.current
            if (!willFollowUp) streamingIndexRef.current = -1
            if (idx < 0 || idx >= prev.length) return prev
            const next = [...prev]
            const m = { ...next[idx], streaming: false }
            // 中断且没有任何内容时移除空占位，避免留下气泡
            if (!m.content && !m.reasoning && !m.toolCalls?.length && !m.error) {
              next.splice(idx, 1)
            } else {
              next[idx] = m
            }
            return next
          })
          // 后端负责落库，前端刷新列表（title/时间）
          queryClient.invalidateQueries({ queryKey: ['agent-conversations'] })
          if (!openRef.current) onUnread?.()

          // 打断重定向优先，其次排队消息：当前回合收尾后立即以新输入开启新回合
          let followUp: string | null = null
          if (steerTextRef.current) {
            followUp = steerTextRef.current
            steerTextRef.current = null
          } else if (queueRef.current.length > 0) {
            followUp = queueRef.current[0]
            setQueued(queueRef.current.slice(1))
          }
          if (followUp) {
            const base = messagesRef.current
            const last = base[base.length - 1]
            const withoutEmptyTail =
              last && last.streaming && !last.content && !last.reasoning && !last.toolCalls?.length
                ? base.slice(0, -1)
                : base
            const cleanedBase = withoutEmptyTail.map((m) => (m.streaming ? { ...m, streaming: false } : m))
            const history: AgentChatMsg[] = [...cleanedBase, { role: 'user', content: followUp }]
            streamingIndexRef.current = history.length
            startStreamRef.current?.(
              [...history, { role: 'assistant', content: '', streaming: true }],
              toApiMsgs(history),
              {}
            )
          }
        })
    },
    [currentId, settings, patchPlaceholder, upsertConversationCache, queryClient, onUnread, setQueued]
  )

  // startStream 自引用（finally 里做打断重定向）
  const startStreamRef = useRef(startStream)
  useEffect(() => {
    startStreamRef.current = startStream
  }, [startStream])

  // ── 发送（含附件拼接到消息文本前）；忙时 Enter 发送进入排队 ──
  const send = useCallback(
    (text: string) => {
      const body = text.trim()
      if (!body) return
      if (isStreaming) {
        setQueued([...queueRef.current, body])
        setInput('')
        setAttachments([])
        return
      }
      const content =
        attachments.length > 0
          ? `${attachments.map((a) => `\`\`\`${a.name}\n${a.content}\n\`\`\``).join('\n')}\n${body}`
          : body
      const history: AgentChatMsg[] = [...messages, { role: 'user', content }]
      streamingIndexRef.current = history.length
      // MoA 只作用于本条消息：消费后立即复位 chip
      const moa = moaNext
      if (moa) setMoaNext(false)
      startStream([...history, { role: 'assistant', content: '', streaming: true }], toApiMsgs(history), { moa })
    },
    [messages, attachments, isStreaming, startStream, setQueued, moaNext]
  )

  // ── MoA 模式（/moa）：切换下一条消息是否走多模型综合 ──
  const toggleMoa = useCallback(() => setMoaNext((v) => !v), [])

  // ── 取消排队中的消息 ──
  const cancelQueued = useCallback(
    (index: number) => {
      setQueued(queueRef.current.filter((_, i) => i !== index))
    },
    [setQueued]
  )

  // ── 工具调用审批：批准/拒绝后清除卡片（回合继续流式） ──
  const resolveApproval = useCallback(
    (approve: boolean) => {
      const pending = pendingApproval
      if (!pending) return
      setPendingApproval(null)
      agentChatApi
        .approve(pending.id, approve)
        .catch(() => toast('error', approve ? '批准失败，请重试' : '拒绝失败，请重试'))
    },
    [pendingApproval]
  )

  // ── 旁问（/btw）：ephemeral 流式，不写入 messages、不落库 ──
  const sendBtw = useCallback(
    (text: string) => {
      const body = text.trim()
      if (!body || btwStreaming) return
      btwAbortRef.current?.()
      setBtwQuestion(body)
      setBtwAnswer('')
      setBtwError(null)
      setBtwStreaming(true)
      const apiMsgs: AgentChatMessage[] = [...toApiMsgs(messagesRef.current), { role: 'user', content: body }]
      const { abort, promise } = agentChatApi.chat(
        {
          messages: apiMsgs,
          model: settings.model || undefined,
          system_prompt: settings.system_prompt || undefined,
          temperature: settings.temperature,
          stream: true,
          ephemeral: true,
        },
        {
          onDelta: (delta) => setBtwAnswer((prev) => prev + delta),
          onDone: (done) => {
            if (done.content) setBtwAnswer(done.content)
          },
          onError: (msg) => setBtwError(msg),
        }
      )
      btwAbortRef.current = abort
      promise
        .catch(() => setBtwError('旁问中断，请重试'))
        .finally(() => {
          setBtwStreaming(false)
          btwAbortRef.current = null
        })
    },
    [btwStreaming, settings]
  )

  const dismissBtw = useCallback(() => {
    btwAbortRef.current?.()
    btwAbortRef.current = null
    setBtwQuestion(null)
    setBtwAnswer('')
    setBtwStreaming(false)
    setBtwError(null)
  }, [])

  // ── 重新生成：对最后一条助手消息 ──
  const regenerate = useCallback(() => {
    if (isStreaming) return
    let lastAssistantIdx = -1
    for (let i = messages.length - 1; i >= 0; i--) {
      if (messages[i].role === 'assistant') {
        lastAssistantIdx = i
        break
      }
    }
    if (lastAssistantIdx < 0) return
    const history = messages.slice(0, lastAssistantIdx)
    streamingIndexRef.current = history.length
    startStream([...history, { role: 'assistant', content: '', streaming: true }], toApiMsgs(history), {
      regenerate: true,
    })
  }, [messages, isStreaming, startStream])

  // ── 编辑用户消息：截断到该条并以 replace_history 重发 ──
  const editMessage = useCallback(
    (index: number, content: string) => {
      if (isStreaming || !content) return
      const history: AgentChatMsg[] = [...messages.slice(0, index), { role: 'user', content }]
      streamingIndexRef.current = history.length
      startStream([...history, { role: 'assistant', content: '', streaming: true }], toApiMsgs(history), {
        replace_history: true,
      })
    },
    [messages, isStreaming, startStream]
  )

  const stop = useCallback(() => abortRef.current?.(), [])

  // ── 撤销最后一轮：删除最后一条用户消息及其后的助手消息 ──
  const undo = useCallback(() => {
    if (isStreaming) return
    const trimLocal = () =>
      setMessages((prev) => {
        let idx = -1
        for (let i = prev.length - 1; i >= 0; i--) {
          if (prev[i].role === 'user') {
            idx = i
            break
          }
        }
        return idx >= 0 ? prev.slice(0, idx) : prev
      })
    if (!currentId) {
      trimLocal()
      return
    }
    agentConversationApi
      .undo(currentId)
      .then(() => {
        trimLocal()
        queryClient.invalidateQueries({ queryKey: ['agent-conversation', currentId] })
        queryClient.invalidateQueries({ queryKey: ['agent-conversations'] })
      })
      .catch(() => toast('error', '撤销失败'))
  }, [currentId, isStreaming, queryClient])

  // ── 打断重定向：忙时发送 → 中断当前回合，结束后立即以新输入续聊 ──
  const steer = useCallback(
    (text: string) => {
      const body = text.trim()
      if (!body) return
      if (!isStreaming) {
        send(body)
        return
      }
      steerTextRef.current = body
      setInput('')
      setAttachments([])
      abortRef.current?.()
    },
    [isStreaming, send]
  )

  // ── 新对话（本地重置；首个消息发出后由 conversation 事件建会话） ──
  const newChat = useCallback(() => {
    if (isStreaming) stop()
    skipSyncRef.current = false
    clearTransient()
    setCurrentId(null)
    setMessages([])
    // 清空输入草稿与附件，否则点击"新会话"看起来毫无反应
    setInput('')
    setAttachments([])
  }, [isStreaming, stop, clearTransient])

  // ── 切换会话：中止进行中的生成 ──
  const selectConversation = useCallback(
    (id: string) => {
      if (id === currentId) return
      if (isStreaming) stop()
      skipSyncRef.current = false
      clearTransient()
      setCurrentId(id)
      const cached = queryClient.getQueryData<AgentConversationDetail>(['agent-conversation', id])
      if (cached) setMessages(mapConversation(cached))
    },
    [currentId, isStreaming, stop, queryClient, clearTransient]
  )

  // ── 分叉：从指定会话的某条消息（user+assistant 下标）分叉并切换过去 ──
  const branchConversation = useCallback(
    async (id: string, messageIndex?: number) => {
      try {
        let idx = messageIndex
        if (idx === undefined) {
          // 未指定位置（侧栏菜单 / /branch）：从会话末尾分叉，先取消息数
          const detail = await agentConversationApi.get(id)
          idx = (detail.messages || []).filter((m) => m.role === 'user' || m.role === 'assistant').length
        }
        const res = await agentConversationApi.branch(id, idx)
        queryClient.removeQueries({ queryKey: ['agent-conversation', res.id] })
        queryClient.invalidateQueries({ queryKey: ['agent-conversations'] })
        toast('success', `已分叉为新会话「${res.title || '未命名会话'}」`)
        selectConversation(res.id)
      } catch {
        toast('error', '分叉失败')
      }
    },
    [queryClient, selectConversation]
  )

  // ── 手动压缩当前会话 ──
  const compressCurrent = useCallback(async () => {
    if (!currentId) {
      toast('warning', '当前会话尚未保存，无法压缩')
      return
    }
    try {
      const res = await agentConversationApi.compress(currentId)
      toast('success', `已压缩 ${res.compressed_count} 条早期消息`)
      queryClient.invalidateQueries({ queryKey: ['agent-conversation', currentId] })
    } catch {
      toast('error', '压缩失败')
    }
  }, [currentId, queryClient])

  // ── 重命名 / 删除 ──
  const renameConversation = useCallback(
    (id: string, title: string) => {
      agentConversationApi
        .rename(id, title)
        .then(() => queryClient.invalidateQueries({ queryKey: ['agent-conversations'] }))
        .catch(() => toast('error', '重命名失败'))
    },
    [queryClient]
  )

  const removeConversation = useCallback(
    (id: string) => {
      agentConversationApi
        .remove(id)
        .then(() => {
          queryClient.invalidateQueries({ queryKey: ['agent-conversations'] })
          queryClient.removeQueries({ queryKey: ['agent-conversation', id] })
          if (id === currentId) newChat()
        })
        .catch(() => toast('error', '删除失败'))
    },
    [queryClient, currentId, newChat]
  )

  // ── 附件选择 ──
  const onPickFile = useCallback(() => fileInputRef.current?.click(), [])

  const onFileChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (!file) return
    const ext = file.name.slice(file.name.lastIndexOf('.')).toLowerCase()
    if (!ATTACH_EXTS.includes(ext)) {
      toast('warning', '仅支持 .txt/.md/.csv/.json 附件')
      return
    }
    if (file.size > ATTACH_MAX_BYTES) {
      toast('warning', '附件不能超过 100KB')
      return
    }
    const reader = new FileReader()
    reader.onload = () => {
      setAttachments((prev) => [...prev, { name: file.name, content: String(reader.result || '') }])
    }
    reader.onerror = () => toast('error', '附件读取失败')
    reader.readAsText(file)
  }, [])

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      send(input)
    }
  }

  if (variant === 'full') {
    return (
      <HermesChat
        messages={messages}
        isStreaming={isStreaming}
        input={input}
        setInput={setInput}
        attachments={attachments}
        setAttachments={setAttachments}
        send={send}
        steer={steer}
        stop={stop}
        undo={undo}
        regenerate={regenerate}
        editMessage={editMessage}
        newChat={newChat}
        conversations={conversations}
        currentId={currentId}
        selectConversation={selectConversation}
        renameConversation={renameConversation}
        removeConversation={removeConversation}
        settings={settings}
        updateSettings={updateSettings}
        providers={providers}
        textareaRef={textareaRef}
        messagesEndRef={messagesEndRef}
        onClose={onClose}
        pendingApproval={pendingApproval}
        resolveApproval={resolveApproval}
        queued={queued}
        cancelQueued={cancelQueued}
        compressedNotice={compressedNotice}
        moaNext={moaNext}
        toggleMoa={toggleMoa}
        moaNotice={moaNotice}
        btw={{ question: btwQuestion, answer: btwAnswer, streaming: btwStreaming, error: btwError }}
        sendBtw={sendBtw}
        dismissBtw={dismissBtw}
        branchConversation={branchConversation}
        compressCurrent={compressCurrent}
      />
    )
  }

  if (!open) return null

  // 以下为 float 悬浮面板专属渲染（full 全屏已在上方提前返回）
  return (
    <div
      role="dialog"
      aria-label="小天助手"
      className={cn(
        'fixed bottom-36 right-5 z-50 flex md:bottom-24 md:right-6 w-[400px] max-w-[calc(100vw-2.5rem)] h-[600px] max-h-[80vh]',
        'rounded-2xl border border-quant-border shadow-2xl',
        'bg-quant-card overflow-hidden text-foreground duration-200'
      )}
    >
      {sidebarOpen && (
        <HistorySidebar
          conversations={conversations}
          currentId={currentId}
          onSelect={selectConversation}
          onNew={newChat}
          onRename={renameConversation}
          onRemove={removeConversation}
        />
      )}

      <div className="relative flex min-w-0 flex-1 flex-col">
        {settingsOpen && (
          <SettingsPopover settings={settings} onSave={updateSettings} onClose={() => setSettingsOpen(false)} />
        )}

        {/* ── 头部 ── */}
        <div className="flex items-center gap-1.5 border-b border-quant-border px-3 py-2.5 shrink-0">
          <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-gradient-to-br from-[#1890ff] to-[#36cfc9]">
            <Bot className="text-white" size={15} />
          </div>
          <span className="shrink-0 text-[13px] font-semibold">小天助手</span>
          {/* 模型选择器：厂商 + 模型 两级 */}
          <div className="flex min-w-0 flex-1 items-center gap-1">
            <select
              value={selectedProvider}
              onChange={(e) => updateSettings({ ...settings, model: e.target.value })}
              title="选择厂商"
              aria-label="选择厂商"
              className="min-w-0 flex-1 truncate rounded-md border border-quant-border bg-quant-bg-secondary px-1 py-1 text-[11px] text-foreground focus:border-[#1890ff]/60 focus:outline-none"
            >
              <option value="">默认</option>
              {providers.map((p) => (
                <option key={p.key} value={p.key}>
                  {p.label || p.key}
                </option>
              ))}
            </select>
            <select
              value={selectedModel}
              onChange={(e) =>
                updateSettings({
                  ...settings,
                  model: e.target.value ? `${selectedProvider}:${e.target.value}` : selectedProvider,
                })
              }
              disabled={!selectedProvider}
              title="选择模型"
              aria-label="选择模型"
              className="min-w-0 flex-1 truncate rounded-md border border-quant-border bg-quant-bg-secondary px-1 py-1 text-[11px] text-foreground focus:border-[#1890ff]/60 focus:outline-none disabled:opacity-50"
            >
              <option value="">{selectedProvider ? '厂商默认' : '默认模型'}</option>
              {(providerObj?.models || []).map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
          </div>
          <button
            type="button"
            onClick={newChat}
            title="新对话"
            aria-label="新对话"
            className="shrink-0 rounded-md p-1.5 text-[#888] transition-colors hover:bg-quant-hover hover:text-foreground"
          >
            <SquarePen size={14} />
          </button>
          <button
            type="button"
            onClick={() => setSidebarOpen((v) => !v)}
            title="历史会话"
            aria-label="历史会话"
            aria-pressed={sidebarOpen}
            className={cn(
              'shrink-0 rounded-md p-1.5 transition-colors hover:bg-quant-hover hover:text-foreground',
              sidebarOpen ? 'text-[#1890ff]' : 'text-[#888]'
            )}
          >
            <History size={14} />
          </button>
          <button
            type="button"
            onClick={() => setSettingsOpen(true)}
            title="设置"
            aria-label="助手设置"
            className="shrink-0 rounded-md p-1.5 text-[#888] transition-colors hover:bg-quant-hover hover:text-foreground"
          >
            <Settings size={14} />
          </button>
          <button
            type="button"
            onClick={onClose}
            title="收起"
            aria-label="收起聊天面板"
            className="shrink-0 rounded-md p-1.5 text-[#888] transition-colors hover:bg-quant-hover hover:text-foreground"
          >
            <ChevronDown size={15} />
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
          {messages.map((m, i) => (
            <MessageItem
              key={m.id || i}
              msg={m}
              index={i}
              isStreaming={isStreaming}
              onRegenerate={regenerate}
              onEdit={editMessage}
            />
          ))}
          {conversationQuery.isError && currentId && messages.length === 0 && (
            <div className="flex items-center justify-center gap-1 text-[11px] text-[#f5222d]">
              <CircleAlert size={12} />
              会话加载失败
            </div>
          )}
          <div ref={messagesEndRef} />
        </div>

        {/* ── 快捷指令 ── */}
        <div className="shrink-0 flex gap-1.5 overflow-x-auto px-3 pb-2 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
          {QUICK_COMMANDS.map((cmd) => (
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
          {/* 附件芯片 */}
          {attachments.length > 0 && (
            <div className="mb-1.5 flex flex-wrap gap-1">
              {attachments.map((a, i) => (
                <span
                  key={`${a.name}-${i}`}
                  className="flex items-center gap-1 rounded-md bg-quant-bg-secondary border border-quant-border px-1.5 py-0.5 text-[11px] text-[#aaa]"
                >
                  <Paperclip size={10} />
                  <span className="max-w-[120px] truncate">{a.name}</span>
                  <button
                    type="button"
                    title="移除附件"
                    aria-label={`移除附件 ${a.name}`}
                    onClick={() => setAttachments((prev) => prev.filter((_, j) => j !== i))}
                    className="rounded p-0.5 hover:text-[#f5222d]"
                  >
                    <X size={10} />
                  </button>
                </span>
              ))}
            </div>
          )}
          <div className="flex items-end gap-2">
            <input
              ref={fileInputRef}
              type="file"
              accept=".txt,.md,.csv,.json"
              className="hidden"
              aria-label="选择附件"
              onChange={onFileChange}
            />
            <button
              type="button"
              onClick={onPickFile}
              disabled={isStreaming}
              title="添加附件（.txt/.md/.csv/.json ≤100KB）"
              aria-label="添加附件"
              className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-quant-border text-[#888] transition-colors hover:bg-quant-hover hover:text-foreground disabled:opacity-50"
            >
              <Paperclip size={15} />
            </button>
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
    </div>
  )
}

export default AgentChatPanel
