import React, { useCallback, useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowLeft, ChevronDown, CircleStop, Layers3, Loader2, MessageCircleQuestion, Shield, X } from 'lucide-react'
import { toast } from '@/lib/useToast'
import type { AgentApprovalRequest, AgentConversationSummary } from '@/lib/api'
import { agentFilesApi, agentPluginApi, agentSkillApi } from '@/lib/api'
import type { AgentChatMsg, AgentSettings } from '../types'
import { useWallpaper } from '../pet/WallpaperContext'
import { WallpaperMedia } from '../pet/WallpaperMedia'
import { cn } from '@/lib/utils'
import { AgentSidebar } from './Sidebar'
import { AgentThread, AgentEmptyState } from './Thread'
import { AgentComposer } from './Composer'
import { SettingsModal } from './SettingsModal'
import { CronPanel } from './CronPanel'
import { MemoryPanel } from './MemoryPanel'
import { SkillsPanel } from './SkillsPanel'
import { TelegramPanel } from './TelegramPanel'
import { FeishuPanel } from './FeishuPanel'
import { DingtalkPanel } from './DingtalkPanel'
import { QqPanel } from './QqPanel'
import { WecomPanel } from './WecomPanel'
import { WeixinPanel } from './WeixinPanel'
import { EvalsPanel } from './EvalsPanel'
import { FilesPanel } from './FilesPanel'
import { UsagePanel } from './UsagePanel'
import { InsightsPanel } from './InsightsPanel'
import { JourneyPanel } from './JourneyPanel'
import { KanbanPanel } from './KanbanPanel'
import { SubagentsPanel } from './SubagentsPanel'
import type { SlashCommand } from './slash'
import pkg from '../../../../package.json'
import './tokens.css'

const { version } = pkg

const ATTACH_MAX_BYTES = 100 * 1024
const ATTACH_EXTS = ['.txt', '.md', '.csv', '.json']

export interface HermesBtwState {
  question: string | null
  answer: string
  streaming: boolean
  error: string | null
}

export interface HermesChatProps {
  messages: AgentChatMsg[]
  isStreaming: boolean
  input: string
  setInput: (v: string) => void
  attachments: { name: string; content: string }[]
  setAttachments: React.Dispatch<React.SetStateAction<{ name: string; content: string }[]>>
  send: (text: string) => void
  /** 忙时点击发送键：打断当前回合后立刻以新输入重定向 */
  steer: (text: string) => void
  stop: () => void
  /** 撤销最后一轮（最后一条用户消息 + 其后的助手消息） */
  undo: () => void
  regenerate: () => void
  editMessage: (index: number, content: string) => void
  newChat: () => void
  conversations: AgentConversationSummary[]
  currentId: string | null
  selectConversation: (id: string) => void
  renameConversation: (id: string, title: string) => void
  removeConversation: (id: string) => void
  settings: AgentSettings
  updateSettings: (s: AgentSettings) => void
  providers: { key: string; label?: string; configured?: boolean; models?: string[] }[]
  textareaRef: React.RefObject<HTMLTextAreaElement | null>
  messagesEndRef: React.RefObject<HTMLDivElement | null>
  onClose: () => void
  /** 待审批的工具调用（回合暂停中） */
  pendingApproval: AgentApprovalRequest | null
  resolveApproval: (approve: boolean) => void
  /** 忙时 Enter 排队的消息（当前回合结束后自动接续） */
  queued: string[]
  cancelQueued: (index: number) => void
  /** 自动压缩提示（条数），一次性展示 */
  compressedNotice: number | null
  /** MoA 多模型综合：下一条消息启用（/moa 切换，发送后自动复位） */
  moaNext: boolean
  toggleMoa: () => void
  /** MoA 综合提示（SSE moa 事件），展示在流式状态条 */
  moaNotice: string | null
  /** 旁问（/btw）瞬态状态 */
  btw: HermesBtwState
  sendBtw: (text: string) => void
  dismissBtw: () => void
  /** 从会话分叉（缺省 messageIndex = 会话末尾） */
  branchConversation: (id: string, messageIndex?: number) => void
  compressCurrent: () => void
}

// ── 全屏助手壳：侧栏 + 消息流 + composer + 指标行（Hermes 桌面版布局） ──
export function HermesChat({
  messages,
  isStreaming,
  input,
  setInput,
  attachments,
  setAttachments,
  send,
  steer,
  stop,
  undo,
  regenerate,
  editMessage,
  newChat,
  conversations,
  currentId,
  selectConversation,
  renameConversation,
  removeConversation,
  settings,
  updateSettings,
  providers,
  textareaRef,
  messagesEndRef,
  onClose,
  pendingApproval,
  resolveApproval,
  queued,
  cancelQueued,
  compressedNotice,
  moaNext,
  toggleMoa,
  moaNotice,
  btw,
  sendBtw,
  dismissBtw,
  branchConversation,
  compressCurrent,
}: HermesChatProps) {
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [mainView, setMainView] = useState<
    | 'chat'
    | 'cron'
    | 'memory'
    | 'skills'
    | 'telegram'
    | 'feishu'
    | 'dingtalk'
    | 'qq'
    | 'weixin'
    | 'wecom'
    | 'files'
    | 'insights'
    | 'journey'
    | 'agents'
    | 'kanban'
    | 'evals'
  >('chat')
  const [btwOpen, setBtwOpen] = useState(false)
  const [btwInput, setBtwInput] = useState('')
  const wp = useWallpaper()
  const hasWallpaper = Boolean(wp?.active)
  const hasMessages = messages.length > 0
  // 状态栏统计：轮数（助手消息数）/ 步数（工具调用数）/ 输入输出字符（1 tok ≈ 2 字符）
  const rounds = messages.filter((m) => m.role === 'assistant').length
  const steps = messages.reduce((a, m) => a + (m.toolCalls?.length || 0), 0)
  const inChars = messages.reduce((a, m) => (m.role === 'user' ? a + (m.content?.length || 0) : a), 0)
  const outChars = messages.reduce((a, m) => (m.role === 'assistant' ? a + (m.content?.length || 0) : a), 0)
  // 最近一条带真实用量的助手消息（done 事件回传）；无则回退字符粗估
  let lastUsage: AgentChatMsg['usage'] | undefined
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].role === 'assistant' && messages[i].usage) {
      lastUsage = messages[i].usage
      break
    }
  }
  // 上下文占用：最近一轮 prompt_tokens（含全部历史）+ 本轮输出；上限按模型名推断
  const ctxUsed = lastUsage ? lastUsage.prompt_tokens + lastUsage.completion_tokens : 0
  const ctxLimit = (() => {
    const m = (settings.model || '').toLowerCase()
    if (m.includes('256k')) return 256_000
    if (m.includes('1m') || m.includes('k3') || m.includes('kimi-for-coding')) return 1_000_000
    return 256_000
  })()
  const ctxRatio = ctxLimit > 0 ? Math.min(ctxUsed / ctxLimit, 1) : 0
  const fmtTok = (n: number) => (n >= 1000 ? `${(n / 1000).toFixed(1)}K` : String(n))
  const ctxTone = ctxRatio > 0.9 ? 'var(--ag-red)' : ctxRatio > 0.7 ? 'var(--ag-amber)' : 'var(--ag-accent)'
  const [usageOpen, setUsageOpen] = useState(false)
  const [modelSignal, setModelSignal] = useState(0)
  const fileReaderRef = useRef<FileReader | null>(null)

  // 插件清单（万物皆可插件）：侧栏导航与斜杠命令由后端插件贡献
  const { data: pluginsData } = useQuery({
    queryKey: ['agent-plugins'],
    queryFn: () => agentPluginApi.list(),
    staleTime: 60 * 1000,
    retry: false,
  })
  const pluginNav = (pluginsData?.plugins || [])
    .map((p) => p.ui?.nav)
    .filter((n): n is NonNullable<typeof n> => Boolean(n))

  // 侧栏只留核心三项，其余收纳进 设置→工具（侧栏太挤）
  const SIDEBAR_CORE = ['cron', 'memory', 'skills']
  const sidebarNav = pluginNav.filter((n) => SIDEBAR_CORE.includes(n.id))
  const overflowNav = pluginNav.filter((n) => !SIDEBAR_CORE.includes(n.id))

  // 技能列表（调色板 + 面板共用）
  const { data: skillsData } = useQuery({
    queryKey: ['agent-skills'],
    queryFn: () => agentSkillApi.list(),
    staleTime: 30 * 1000,
    retry: false,
  })
  const paletteSkills = (skillsData?.skills || []).map((s) => ({
    name: s.name,
    description: s.description,
    body: s.body,
  }))

  // /rollback：确认后恢复最近一次文件检查点（管理员接口）
  const rollbackLatest = useCallback(async () => {
    try {
      const res = await agentFilesApi.checkpoints()
      const cps = res.checkpoints || []
      if (cps.length === 0) {
        toast('info', '暂无文件检查点')
        return
      }
      const latest = [...cps].sort((a, b) => b.created_at - a.created_at)[0]
      if (!window.confirm(`确认恢复最近的检查点？\n${latest.path}`)) return
      const r = await agentFilesApi.rollback(latest.id)
      toast('success', `已恢复 ${r.restored}`)
    } catch (e) {
      toast('error', (e as Error).message || '回滚失败')
    }
  }, [])

  const onSlash = useCallback(
    (cmd: SlashCommand) => {
      switch (cmd.name) {
        case 'new':
        case 'clear':
          newChat()
          break
        case 'stop':
          stop()
          break
        case 'retry':
          regenerate()
          break
        case 'undo':
          undo()
          break
        case 'approve':
          if (pendingApproval) resolveApproval(true)
          else toast('warning', '当前没有待批准的工具调用')
          break
        case 'deny':
          if (pendingApproval) resolveApproval(false)
          else toast('warning', '当前没有待拒绝的工具调用')
          break
        case 'btw':
          setBtwOpen(true)
          break
        case 'compress':
          compressCurrent()
          break
        case 'branch':
          if (currentId) branchConversation(currentId)
          else toast('warning', '当前会话尚未保存，无法分叉')
          break
        case 'model':
          setModelSignal((n) => n + 1)
          break
        case 'cron':
          setMainView('cron')
          break
        case 'memory':
          setMainView('memory')
          break
        case 'skills':
          setMainView('skills')
          break
        case 'telegram':
          setMainView('telegram')
          break
        case 'insights':
          setMainView('insights')
          break
        case 'journey':
          setMainView('journey')
          break
        case 'agents':
          setMainView('agents')
          break
        case 'kanban':
          setMainView('kanban')
          break
        case 'files':
          setMainView('files')
          break
        case 'feishu':
          setMainView('feishu')
          break
        case 'dingtalk':
          setMainView('dingtalk')
          break
        case 'qq':
          setMainView('qq')
          break
        case 'weixin':
          setMainView('weixin')
          break
        case 'wecom':
          setMainView('wecom')
          break
        case 'evals':
          setMainView('evals')
          break
        case 'moa':
          toggleMoa()
          break
        case 'rollback':
          rollbackLatest()
          break
        case 'usage':
          setUsageOpen(true)
          break
      }
    },
    [
      newChat,
      stop,
      regenerate,
      undo,
      pendingApproval,
      resolveApproval,
      compressCurrent,
      currentId,
      branchConversation,
      rollbackLatest,
      toggleMoa,
    ]
  )

  const onPickFile = useCallback(
    (file: File) => {
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
      fileReaderRef.current = reader
      reader.onload = () => {
        setAttachments((prev) => [...prev, { name: file.name, content: String(reader.result || '') }])
      }
      reader.onerror = () => toast('error', '附件读取失败')
      reader.readAsText(file)
    },
    [setAttachments]
  )

  // 打开全屏时聚焦输入框
  useEffect(() => {
    textareaRef.current?.focus()
  }, [textareaRef])

  const composer = (
    <AgentComposer
      input={input}
      onInputChange={setInput}
      onSubmit={send}
      onSteer={steer}
      onStop={stop}
      isStreaming={isStreaming}
      attachments={attachments}
      onRemoveAttachment={(i) => setAttachments((prev) => prev.filter((_, j) => j !== i))}
      onPickFile={onPickFile}
      textareaRef={textareaRef}
      settings={settings}
      updateSettings={updateSettings}
      providers={providers}
      onSlash={onSlash}
      onSuggestion={send}
      showSuggestions={messages.length > 0}
      openModelSignal={modelSignal}
      skills={paletteSkills}
      onUseSkill={(skill) => send(skill.body)}
      onShowUsage={() => setUsageOpen(true)}
      placeholder={hasMessages ? undefined : '描述你想要构建的内容，/ 调用指令'}
    />
  )

  // ── 旁问（/btw）：一行输入 → 瞬态回答卡片，不进入消息流 ──
  const btwCard = (btwOpen || btw.question) && (
    <div className="pointer-events-auto rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)]/95 px-3 py-2 shadow-[var(--ag-shadow-panel)] backdrop-blur-xl">
      <div className="flex items-center gap-1.5">
        <MessageCircleQuestion size={13} className="shrink-0 text-[var(--ag-accent)]" />
        <span className="shrink-0 text-[11px] font-medium text-[var(--ag-text2)]">旁问</span>
        {btw.question && (
          <span className="min-w-0 flex-1 truncate text-[11px] text-[var(--ag-text3)]">{btw.question}</span>
        )}
        <span className="min-w-0 flex-1" />
        <button
          type="button"
          aria-label="关闭旁问"
          title="关闭旁问"
          onClick={() => {
            dismissBtw()
            setBtwOpen(false)
            setBtwInput('')
          }}
          className="shrink-0 rounded p-0.5 text-[var(--ag-text4)] transition-colors hover:bg-black/5 hover:text-[var(--ag-text2)]"
        >
          <X size={12} />
        </button>
      </div>
      {btw.question ? (
        <div className="mt-1 max-h-36 overflow-y-auto text-[12.5px] leading-relaxed text-[var(--ag-text1)]">
          {btw.error ? (
            <span className="text-[var(--ag-red)]">{btw.error}</span>
          ) : btw.answer ? (
            <span className="whitespace-pre-wrap break-words">{btw.answer}</span>
          ) : (
            <span className="flex items-center gap-1 text-[var(--ag-text4)]">
              <Loader2 size={11} className="animate-spin" />
              思考中…
            </span>
          )}
        </div>
      ) : (
        <input
          value={btwInput}
          onChange={(e) => setBtwInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.nativeEvent.isComposing) return
            if (e.key === 'Enter' && btwInput.trim()) {
              e.preventDefault()
              sendBtw(btwInput)
              setBtwInput('')
            }
            if (e.key === 'Escape') {
              e.preventDefault()
              setBtwOpen(false)
              setBtwInput('')
            }
          }}
          placeholder="问一句题外话，不影响当前对话…"
          aria-label="旁问输入框"
          autoFocus
          className="mt-1 w-full rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2 py-1 text-[12px] text-[var(--ag-text1)] placeholder:text-[var(--ag-text4)] focus:border-[var(--ag-accent)]/50 focus:outline-none"
        />
      )}
    </div>
  )

  // ── MoA 模式 chip：/moa 切换，下一条消息走多模型综合（发送后自动复位） ──
  const moaChip = moaNext && (
    <div
      aria-label="MoA 模式"
      className="pointer-events-auto flex items-center gap-1.5 rounded-xl border border-[var(--ag-accent)]/40 bg-[var(--ag-accent)]/8 px-2.5 py-1.5 shadow-[var(--ag-shadow-panel)] backdrop-blur-xl"
    >
      <Layers3 size={12} className="shrink-0 text-[var(--ag-accent)]" />
      <span className="text-[11px] font-medium text-[var(--ag-accent)]">MoA 多模型</span>
      <span className="text-[10px] text-[var(--ag-text3)]">下一条消息生效</span>
      <span className="min-w-0 flex-1" />
      <button
        type="button"
        title="取消 MoA 模式"
        aria-label="取消 MoA 模式"
        onClick={toggleMoa}
        className="shrink-0 rounded p-0.5 text-[var(--ag-accent)]/70 transition-colors hover:bg-black/5 hover:text-[var(--ag-accent)]"
      >
        <X size={11} />
      </button>
    </div>
  )

  // ── 排队消息 chips：忙时 Enter 进入队列，回合结束后自动接续 ──
  const queueChips = queued.length > 0 && (
    <div
      aria-label="排队消息"
      className="pointer-events-auto flex flex-wrap items-center gap-1 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)]/90 px-2 py-1.5 shadow-[var(--ag-shadow-panel)] backdrop-blur-xl"
    >
      <span className="shrink-0 px-1 text-[10px] font-semibold uppercase tracking-[0.1em] text-[var(--ag-text4)]">
        排队中
      </span>
      {queued.map((q, i) => (
        <span
          key={`${i}-${q}`}
          className="flex max-w-[45%] items-center gap-1 rounded-full border border-[var(--ag-stroke3)] bg-[var(--ag-card)] px-2 py-0.5 text-[11px] text-[var(--ag-text2)]"
        >
          <span className="truncate">{q}</span>
          <button
            type="button"
            title="取消排队"
            aria-label={`取消排队 ${q}`}
            onClick={() => cancelQueued(i)}
            className="shrink-0 rounded p-0.5 text-[var(--ag-text4)] hover:text-[var(--ag-red)]"
          >
            <X size={10} />
          </button>
        </span>
      ))}
    </div>
  )

  return (
    <div className={cn('xt-hermes', hasWallpaper && 'has-wallpaper')} role="dialog" aria-label="小天助手">
      {/* 壁纸层（液态玻璃模式透出） */}
      {wp?.active && (
        <div aria-hidden className="pointer-events-none absolute inset-0 overflow-hidden">
          <WallpaperMedia entry={wp.active} fx={wp.fx} className="absolute inset-0 h-full w-full" />
        </div>
      )}
      <div className="relative flex min-h-0 flex-1">
        <AgentSidebar
          conversations={conversations}
          currentId={currentId}
          onSelect={selectConversation}
          onNew={newChat}
          onRename={renameConversation}
          onRemove={removeConversation}
          onBranch={(id) => branchConversation(id)}
          onOpenSettings={() => setSettingsOpen(true)}
          pluginNav={sidebarNav}
          onPluginNav={(id) => {
            if (
              [
                'cron',
                'memory',
                'skills',
                'telegram',
                'feishu',
                'dingtalk',
                'qq',
                'weixin',
                'wecom',
                'files',
                'insights',
                'journey',
                'agents',
                'kanban',
                'evals',
              ].includes(id)
            )
              setMainView(id as typeof mainView)
          }}
        />

        <div className="relative flex min-w-0 flex-1 flex-col">
          {mainView !== 'chat' ? (
            /* ── 插件整页（对标桌面版扩展页：标题 + 返回 + 内容区） ── */
            <>
              <div className="flex items-center gap-2 border-b border-[var(--ag-stroke3)] px-5 pb-2 pt-3">
                <button
                  type="button"
                  onClick={() => setMainView('chat')}
                  title="返回对话"
                  aria-label="返回对话"
                  className="shrink-0 rounded-md p-1 text-[var(--ag-text3)] transition-colors hover:bg-black/5 hover:text-[var(--ag-text1)]"
                >
                  <ArrowLeft size={15} />
                </button>
                <span className="text-[15px] font-semibold text-[var(--ag-text1)]">
                  {mainView === 'cron'
                    ? '定时任务'
                    : mainView === 'memory'
                      ? '记忆'
                      : mainView === 'skills'
                        ? '技能'
                        : mainView === 'insights'
                          ? '用量报告'
                          : mainView === 'journey'
                            ? '学习轨迹'
                            : mainView === 'agents'
                              ? '子代理'
                              : mainView === 'kanban'
                                ? '看板'
                                : mainView === 'files'
                                  ? '文件回滚'
                                  : mainView === 'feishu'
                                    ? '飞书'
                                    : mainView === 'dingtalk'
                                      ? '钉钉'
                                      : mainView === 'qq'
                                        ? 'QQ'
                                        : mainView === 'weixin'
                                          ? '微信'
                                          : mainView === 'wecom'
                                          ? '企业微信'
                                          : mainView === 'evals'
                                            ? '评测'
                                            : 'Telegram'}
                </span>
              </div>
              <div className="min-h-0 flex-1 p-4">
                {mainView === 'cron' && <CronPanel bare onClose={() => setMainView('chat')} />}
                {mainView === 'memory' && <MemoryPanel bare onClose={() => setMainView('chat')} />}
                {mainView === 'skills' && (
                  <SkillsPanel
                    bare
                    onUse={(skill) => {
                      setMainView('chat')
                      send(skill.body)
                    }}
                    onClose={() => setMainView('chat')}
                  />
                )}
                {mainView === 'telegram' && <TelegramPanel bare onClose={() => setMainView('chat')} />}
                {mainView === 'feishu' && <FeishuPanel bare onClose={() => setMainView('chat')} />}
                {mainView === 'dingtalk' && <DingtalkPanel bare onClose={() => setMainView('chat')} />}
                {mainView === 'qq' && <QqPanel bare onClose={() => setMainView('chat')} />}
                {mainView === 'weixin' && <WeixinPanel bare onClose={() => setMainView('chat')} />}
                {mainView === 'wecom' && <WecomPanel bare onClose={() => setMainView('chat')} />}
                {mainView === 'evals' && <EvalsPanel bare onClose={() => setMainView('chat')} />}
                {mainView === 'files' && <FilesPanel bare onClose={() => setMainView('chat')} />}
                {mainView === 'insights' && <InsightsPanel bare onClose={() => setMainView('chat')} />}
                {mainView === 'journey' && <JourneyPanel bare onClose={() => setMainView('chat')} />}
                {mainView === 'agents' && <SubagentsPanel bare onClose={() => setMainView('chat')} />}
                {mainView === 'kanban' && <KanbanPanel bare onClose={() => setMainView('chat')} />}
              </div>
            </>
          ) : hasMessages ? (
            <>
              {/* 标题行 + 页签（会话态，对标桌面版） */}
              <div className="flex items-center gap-2 px-5 pb-1 pt-3.5">
                <span className="min-w-0 flex-1 truncate text-[14px] font-semibold text-[var(--ag-text1)]">
                  {conversations.find((c) => c.id === currentId)?.title || '新对话'}
                </span>
                <span className="flex shrink-0 items-center gap-1 text-[11px] text-[var(--ag-text3)]">
                  <Shield size={11} />
                  标准模式
                </span>
                <button
                  type="button"
                  onClick={() => setUsageOpen(true)}
                  title="会话日志与用量"
                  aria-label="会话日志"
                  className="flex shrink-0 items-center gap-1 rounded-full border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2.5 py-1 text-[11px] text-[var(--ag-text2)] transition-colors hover:border-[var(--ag-accent)]/50 hover:text-[var(--ag-accent)]"
                >
                  会话日志
                  <ChevronDown size={11} />
                </button>
              </div>
              <div className="flex items-center gap-5 border-b border-[var(--ag-stroke3)] px-5">
                <span className="border-b-2 border-[var(--ag-accent)] py-2 text-[13px] font-medium text-[var(--ag-text1)]">
                  对话
                </span>
                <span className="cursor-not-allowed py-2 text-[13px] text-[var(--ag-text4)]" title="敬请期待">
                  轨迹
                </span>
              </div>

              {/* 消息流：flex-1 占满标题与停靠区之间的剩余空间，overflow-y-auto 滚动 */}
              <AgentThread
                messages={messages}
                isStreaming={isStreaming}
                onRegenerate={regenerate}
                onEdit={editMessage}
                onUndo={undo}
                messagesEndRef={messagesEndRef}
                pendingApproval={pendingApproval}
                onResolveApproval={resolveApproval}
                compressedNotice={compressedNotice}
                onBranch={(idx) => {
                  if (currentId) branchConversation(currentId, idx)
                  else toast('warning', '当前会话尚未保存，无法分叉')
                }}
              />

              {/* 流式状态条 + Composer + 指标行：文档流停靠底部（与交易系统页面
                  同一布局模式——输入框跟在内容后面占真实布局空间，不悬浮压消息流，
                  从结构上杜绝"输入框压住文字"这一类问题） */}
              <div className="shrink-0 px-5 pb-4">
                <div className="pointer-events-none mx-auto flex w-full max-w-[var(--ag-composer-max-w)] flex-col gap-1.5">
                  {btwCard}
                  {queueChips}
                  {moaChip}
                  {isStreaming && (
                    <div
                      aria-label="生成状态"
                      className="pointer-events-auto flex h-9 items-center gap-2 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)]/90 px-3 shadow-[var(--ag-shadow-panel)] backdrop-blur-xl"
                    >
                      <span className="xt-shimmer-text text-[12px] font-medium">正在生成…</span>
                      {moaNotice && (
                        <span className="min-w-0 truncate text-[11px] text-[var(--ag-accent)]">{moaNotice}</span>
                      )}
                      <span className="text-[11px] tabular-nums text-[var(--ag-text4)]">
                        {rounds} 轮 · {steps} 步
                      </span>
                      <span className="min-w-0 flex-1" />
                      <button
                        type="button"
                        onClick={stop}
                        title="停止生成（Esc）"
                        aria-label="停止生成"
                        className="flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] font-medium text-[var(--ag-text2)] transition-colors hover:bg-black/5 hover:text-[var(--ag-text1)]"
                      >
                        <CircleStop size={12} />
                        停止
                      </button>
                    </div>
                  )}
                  <div className="pointer-events-auto">{composer}</div>
                {/* 上下文用量条：最近一轮 prompt_tokens ≈ 当前上下文占用，>70% 变黄、>90% 变红 */}
                {hasMessages && ctxUsed > 0 && (
                  <div
                    className="pointer-events-auto flex items-center gap-1.5 px-1"
                    title={`本轮请求携带 ${lastUsage?.prompt_tokens} tok + 输出 ${lastUsage?.completion_tokens} tok；模型上下文上限约 ${fmtTok(ctxLimit)}`}
                    aria-label="上下文用量"
                  >
                    <div className="h-1 min-w-0 flex-1 overflow-hidden rounded-full bg-black/8">
                      <div
                        className="h-full rounded-full transition-[width] duration-300"
                        style={{ width: `${Math.max(ctxRatio * 100, 1.5)}%`, background: ctxTone }}
                      />
                    </div>
                    <span className="shrink-0 text-[10px] tabular-nums" style={{ color: ctxTone }}>
                      上下文 {fmtTok(ctxUsed)}/{fmtTok(ctxLimit)}
                    </span>
                  </div>
                )}
                  <div className="pointer-events-none text-center text-[11px] tabular-nums text-[var(--ag-text4)]">
                    {lastUsage
                      ? `${rounds} 轮 · ${steps} 步 | LLM ${(lastUsage.llm_ms / 1000).toFixed(1)}s · ${lastUsage.tok_per_s.toFixed(1)} tok/s | 输入 ${lastUsage.prompt_tokens} tok · 输出 ${lastUsage.completion_tokens} tok`
                      : `${rounds} 轮 · ${steps} 步 | 输入 ${(inChars / 2000).toFixed(1)}K tok · 输出 ${(outChars / 2000).toFixed(1)}K tok`}
                  </div>
                </div>
              </div>
            </>
          ) : (
            /* 空态：文案+药丸+Composer 整块垂直居中（对标桌面版预览0） */
            <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-5 px-6 pb-16">
              <AgentEmptyState />
              <div className="flex w-[min(100%-2rem,var(--ag-composer-max-w))] flex-col gap-1.5">
                {btwCard}
                {moaChip}
                {composer}
              </div>
            </div>
          )}

          {settingsOpen && (
            <SettingsModal
              settings={settings}
              onSave={updateSettings}
              onClose={() => setSettingsOpen(false)}
              providers={providers}
              version={version ? `web v${version}` : ''}
              tools={overflowNav.map((n) => ({ id: n.id, label: n.label }))}
            />
          )}
          {usageOpen && (
            <UsagePanel
              messages={messages}
              conversationId={currentId}
              model={settings.model}
              onClose={() => setUsageOpen(false)}
            />
          )}
        </div>
      </div>
    </div>
  )
}

export default HermesChat
