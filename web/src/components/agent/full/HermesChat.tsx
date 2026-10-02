import React, { useCallback, useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowLeft, ChevronDown, Shield } from 'lucide-react'
import { toast } from '@/lib/useToast'
import type { AgentConversationSummary } from '@/lib/api'
import { agentPluginApi, agentSkillApi } from '@/lib/api'
import type { AgentChatMsg, AgentSettings } from '../types'
import { SettingsPopover } from '../SettingsPopover'
import { useWallpaper } from '../pet/WallpaperContext'
import { WallpaperMedia } from '../pet/WallpaperMedia'
import { cn } from '@/lib/utils'
import { AgentSidebar } from './Sidebar'
import { AgentThread, AgentEmptyState } from './Thread'
import { AgentComposer } from './Composer'
import { AgentStatusBar } from './StatusBar'
import { CronPanel } from './CronPanel'
import { MemoryPanel } from './MemoryPanel'
import { SkillsPanel } from './SkillsPanel'
import { TelegramPanel } from './TelegramPanel'
import { UsagePanel } from './UsagePanel'
import type { SlashCommand } from './slash'
import pkg from '../../../../package.json'
import './tokens.css'

const { version } = pkg

const ATTACH_MAX_BYTES = 100 * 1024
const ATTACH_EXTS = ['.txt', '.md', '.csv', '.json']

export interface HermesChatProps {
  messages: AgentChatMsg[]
  isStreaming: boolean
  input: string
  setInput: (v: string) => void
  attachments: { name: string; content: string }[]
  setAttachments: React.Dispatch<React.SetStateAction<{ name: string; content: string }[]>>
  send: (text: string) => void
  /** 忙时发送：打断当前回合后立刻以新输入重定向 */
  steer: (text: string) => void
  stop: () => void
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
}

// ── 全屏助手壳：侧栏 + 消息流 + composer + 状态条（Hermes 桌面版布局） ──
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
}: HermesChatProps) {
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [mainView, setMainView] = useState<'chat' | 'cron' | 'memory' | 'skills' | 'telegram'>('chat')
  const wp = useWallpaper()
  const hasWallpaper = Boolean(wp?.active)
  const hasMessages = messages.length > 0
  // 状态栏统计：轮数（助手消息数）/ 步数（工具调用数）/ 输入输出字符（1 tok ≈ 2 字符）
  const rounds = messages.filter((m) => m.role === 'assistant').length
  const steps = messages.reduce((a, m) => a + (m.toolCalls?.length || 0), 0)
  const inChars = messages.reduce((a, m) => (m.role === 'user' ? a + (m.content?.length || 0) : a), 0)
  const outChars = messages.reduce((a, m) => (m.role === 'assistant' ? a + (m.content?.length || 0) : a), 0)
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
        case 'usage':
          setUsageOpen(true)
          break
      }
    },
    [newChat, stop, regenerate]
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
      onSubmit={(text) => (isStreaming ? steer(text) : send(text))}
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
    />
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
          onOpenSettings={() => setSettingsOpen(true)}
          pluginNav={pluginNav}
          onPluginNav={(id) => {
            if (id === 'cron' || id === 'memory' || id === 'skills' || id === 'telegram') setMainView(id)
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
              </div>
            </>
          ) : hasMessages ? (
            <>
              {/* 标题行 + 页签（会话态，对标桌面版） */}
              <div className="flex items-center gap-2 px-5 pb-1 pt-3">
                <span className="min-w-0 flex-1 truncate text-[15px] font-semibold text-[var(--ag-text1)]">
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
              <div className="flex items-center gap-4 border-b border-[var(--ag-stroke3)] px-5">
                <span className="border-b-2 border-[var(--ag-accent)] py-1.5 text-[12px] font-medium text-[var(--ag-text1)]">
                  对话
                </span>
                <span className="cursor-not-allowed py-1.5 text-[12px] text-[var(--ag-text4)]" title="敬请期待">
                  轨迹
                </span>
              </div>

              <AgentThread
                messages={messages}
                isStreaming={isStreaming}
                onRegenerate={regenerate}
                onEdit={editMessage}
                messagesEndRef={messagesEndRef}
              />

              {/* Composer 停靠底部 */}
              <div className="pointer-events-none absolute bottom-4 left-1/2 w-[calc(100%-2.5rem)] max-w-5xl -translate-x-1/2">
                <div className="pointer-events-auto">{composer}</div>
              </div>
            </>
          ) : (
            /* 空态：文案+药丸+Composer 整块垂直居中（对标桌面版预览0） */
            <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-5 px-6 pb-16">
              <AgentEmptyState />
              <div className="w-[min(100%-2rem,46rem)]">{composer}</div>
            </div>
          )}

          {settingsOpen && (
            <SettingsPopover light settings={settings} onSave={updateSettings} onClose={() => setSettingsOpen(false)} />
          )}
          {usageOpen && <UsagePanel messages={messages} onClose={() => setUsageOpen(false)} />}
        </div>
      </div>

      <div className="relative">
        <AgentStatusBar
          version={version ? `web v${version}` : ''}
          stats={
            messages.length > 0
              ? `${rounds} 轮 · ${steps} 步 | 📥 输入 ${(inChars / 2000).toFixed(1)}K tok · 输出 ${(outChars / 2000).toFixed(1)}K tok`
              : '就绪'
          }
        />
      </div>
    </div>
  )
}

export default HermesChat
