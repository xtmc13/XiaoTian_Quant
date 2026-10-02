import React, { useCallback, useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { toast } from '@/lib/useToast'
import type { AgentConversationSummary } from '@/lib/api'
import { agentPluginApi, agentSkillApi } from '@/lib/api'
import type { AgentChatMsg, AgentSettings } from '../types'
import { SettingsPopover } from '../SettingsPopover'
import { AgentSidebar } from './Sidebar'
import { AgentThread } from './Thread'
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
  const [cronOpen, setCronOpen] = useState(false)
  const [memoryOpen, setMemoryOpen] = useState(false)
  const [skillsOpen, setSkillsOpen] = useState(false)
  const [telegramOpen, setTelegramOpen] = useState(false)
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
          setCronOpen(true)
          break
        case 'memory':
          setMemoryOpen(true)
          break
        case 'skills':
          setSkillsOpen(true)
          break
        case 'telegram':
          setTelegramOpen(true)
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

  return (
    <div className="xt-hermes" role="dialog" aria-label="小天助手">
      <div className="flex min-h-0 flex-1">
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
            if (id === 'cron') setCronOpen(true)
            if (id === 'memory') setMemoryOpen(true)
            if (id === 'skills') setSkillsOpen(true)
            if (id === 'telegram') setTelegramOpen(true)
          }}
        />

        <div className="relative flex min-w-0 flex-1 flex-col">
          <AgentThread
            messages={messages}
            isStreaming={isStreaming}
            onRegenerate={regenerate}
            onEdit={editMessage}
            messagesEndRef={messagesEndRef}
          />

          {/* Composer 停靠底部居中 */}
          <div className="pointer-events-none absolute bottom-4 left-1/2 w-[calc(100%-2rem)] max-w-3xl -translate-x-1/2">
            <div className="pointer-events-auto">
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
              />
            </div>
          </div>

          {settingsOpen && (
            <SettingsPopover settings={settings} onSave={updateSettings} onClose={() => setSettingsOpen(false)} />
          )}
          {cronOpen && <CronPanel onClose={() => setCronOpen(false)} />}
          {memoryOpen && <MemoryPanel onClose={() => setMemoryOpen(false)} />}
          {skillsOpen && <SkillsPanel onUse={(skill) => send(skill.body)} onClose={() => setSkillsOpen(false)} />}
          {telegramOpen && <TelegramPanel onClose={() => setTelegramOpen(false)} />}
          {usageOpen && <UsagePanel messages={messages} onClose={() => setUsageOpen(false)} />}
        </div>
      </div>

      <AgentStatusBar model={settings.model} version={version ? `web v${version}` : ''} />
    </div>
  )
}

export default HermesChat
