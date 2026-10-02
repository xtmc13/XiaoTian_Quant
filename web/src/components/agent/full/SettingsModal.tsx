import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronRight, Info, KeyRound, Layers, Plus, SlidersHorizontal, Sparkles, Trash2, Users, Wrench, X } from 'lucide-react'
import { agentProfilesApi, agentUserAiConfigApi, type AgentProfile } from '@/lib/api'
import { useAuthStore } from '@/stores/authStore'
import { toast } from '@/lib/useToast'
import type { AgentSettings } from '../types'
import { useTts } from '../useTts'
import { cn } from '@/lib/utils'

interface ModelProvider {
  key: string
  label?: string
  configured?: boolean
  models?: string[]
}

// ── 我的 API 覆盖：个人 key 优先于全局配置（key 只写不读，永不回显） ──
function UserAiKeyCard({ providers, inputCls }: { providers: ModelProvider[]; inputCls: string }) {
  const queryClient = useQueryClient()
  const { data } = useQuery({
    queryKey: ['agent-user-ai-config'],
    queryFn: agentUserAiConfigApi.get,
    retry: false,
    staleTime: 30_000,
  })
  const [provider, setProvider] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [model, setModel] = useState('')
  const [saving, setSaving] = useState(false)
  // 服务端配置到达后回填（api_key 永不回传，只回填 provider/model）
  useEffect(() => {
    if (!data) return
    setProvider(data.provider || '')
    setModel(data.model || '')
  }, [data])

  const hasKey = Boolean(data?.has_key)

  const save = async () => {
    if (!provider || !apiKey.trim()) {
      toast('warning', '请选择厂商并填写 API Key')
      return
    }
    setSaving(true)
    try {
      await agentUserAiConfigApi.put({ provider, api_key: apiKey.trim(), model: model.trim() })
      setApiKey('')
      toast('success', '个人 API 覆盖已保存')
      queryClient.invalidateQueries({ queryKey: ['agent-user-ai-config'] })
    } catch {
      toast('error', '保存失败')
    } finally {
      setSaving(false)
    }
  }

  const clear = async () => {
    setSaving(true)
    try {
      await agentUserAiConfigApi.remove()
      setApiKey('')
      toast('success', '已清除个人 API 覆盖')
      queryClient.invalidateQueries({ queryKey: ['agent-user-ai-config'] })
    } catch {
      toast('error', '清除失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="mt-5 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-sidebar)]/60 p-3.5">
      <div className="flex items-center gap-1.5">
        <KeyRound size={13} className="text-[var(--ag-accent)]" />
        <span className="text-[13px] font-semibold text-[var(--ag-text1)]">我的 API 覆盖</span>
        <span className="min-w-0 flex-1" />
        <span
          aria-label="个人 key 状态"
          className={cn(
            'rounded-full border px-1.5 py-px text-[10px] font-medium',
            hasKey
              ? 'border-[var(--ag-green)]/40 bg-[var(--ag-green)]/10 text-[var(--ag-green)]'
              : 'border-[var(--ag-stroke3)] text-[var(--ag-text4)]'
          )}
        >
          {hasKey ? '已配置' : '未配置'}
        </span>
      </div>
      <p className="mt-1 text-[11px] leading-relaxed text-[var(--ag-text4)]">
        配置后你的对话优先使用个人 key，不影响全局设置；key 只保存不回显。
      </p>
      <label className="mb-1 mt-3 block text-[11px] text-[var(--ag-text3)]" htmlFor="agent-user-provider">
        厂商
      </label>
      <select
        id="agent-user-provider"
        value={provider}
        onChange={(e) => setProvider(e.target.value)}
        className={inputCls}
      >
        <option value="">选择厂商</option>
        {providers.map((p) => (
          <option key={p.key} value={p.key}>
            {p.label || p.key}
          </option>
        ))}
      </select>
      <label className="mb-1 mt-3 block text-[11px] text-[var(--ag-text3)]" htmlFor="agent-user-api-key">
        API Key
      </label>
      <input
        id="agent-user-api-key"
        type="password"
        value={apiKey}
        onChange={(e) => setApiKey(e.target.value)}
        placeholder={hasKey ? '已保存，输入新 key 可覆盖' : 'sk-…'}
        autoComplete="new-password"
        className={inputCls}
      />
      <label className="mb-1 mt-3 block text-[11px] text-[var(--ag-text3)]" htmlFor="agent-user-model">
        模型（可留空用厂商默认）
      </label>
      <input
        id="agent-user-model"
        value={model}
        onChange={(e) => setModel(e.target.value)}
        placeholder="例如 deepseek-chat"
        className={inputCls}
      />
      <div className="mt-3 flex gap-2">
        <button
          type="button"
          onClick={save}
          disabled={saving}
          className="rounded-md bg-[var(--ag-accent)] px-2.5 py-1 text-[12px] text-white hover:opacity-90 disabled:opacity-50"
        >
          保存覆盖
        </button>
        <button
          type="button"
          onClick={clear}
          disabled={saving || !hasKey}
          className="rounded-md border border-[var(--ag-stroke2)] px-2.5 py-1 text-[12px] text-[var(--ag-text3)] hover:bg-black/5 disabled:opacity-50"
        >
          清除
        </button>
      </div>
    </div>
  )
}

export interface SettingsModalProps {
  settings: AgentSettings
  onSave: (settings: AgentSettings) => void
  onClose: () => void
  providers: ModelProvider[]
  version?: string
  /** 侧栏放不下的扩展工具（收纳进"工具"页签） */
  tools?: { id: string; label: string }[]
  onTool?: (id: string) => void
}

type Section = 'general' | 'tools' | 'model' | 'profiles' | 'about'

const SECTIONS: { id: Section; label: string; icon: typeof Sparkles }[] = [
  { id: 'general', label: '通用', icon: SlidersHorizontal },
  { id: 'tools', label: '工具', icon: Wrench },
  { id: 'model', label: '模型', icon: Sparkles },
  { id: 'profiles', label: '档案', icon: Layers },
  { id: 'about', label: '关于', icon: Info },
]

function profileDate(ts: number): string {
  if (!ts) return ''
  const d = new Date(ts < 1e12 ? ts * 1000 : ts)
  return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()}`
}

// ── 档案管理：记忆/技能等上下文按档案隔离；激活即切换作用域 ──
function ProfilesSection() {
  const queryClient = useQueryClient()
  const isAdmin = useAuthStore((s) => s.user?.role === 'admin')
  const [newName, setNewName] = useState('')
  const [confirmDelete, setConfirmDelete] = useState<number | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['agent-profiles'],
    queryFn: () => agentProfilesApi.list(),
    retry: false,
  })
  const profiles = data?.profiles || []
  const adminAll = isAdmin ? data?.admin_all || [] : []

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['agent-profiles'] })

  const createMut = useMutation({
    mutationFn: (name: string) => agentProfilesApi.create(name),
    onSuccess: () => {
      setNewName('')
      toast('success', '档案已创建')
      invalidate()
    },
    onError: (e: Error) => toast('error', e.message || '创建失败'),
  })

  const activateMut = useMutation({
    mutationFn: (id: number) => agentProfilesApi.activate(id),
    onSuccess: () => {
      toast('success', '已切换档案')
      invalidate()
    },
    onError: (e: Error) => toast('error', e.message || '切换失败'),
  })

  const removeMut = useMutation({
    mutationFn: (id: number) => agentProfilesApi.remove(id),
    // 删除当前激活档案后由后端回退到默认档案，重新拉取即可看到
    onSuccess: () => {
      setConfirmDelete(null)
      toast('success', '档案已删除')
      invalidate()
    },
    onError: (e: Error) => {
      setConfirmDelete(null)
      toast('error', e.message || '删除失败')
    },
  })

  const submitNew = () => {
    const name = newName.trim()
    if (!name || createMut.isPending) return
    createMut.mutate(name)
  }

  const renderRow = (p: AgentProfile, deletable: boolean) => (
    <div
      key={p.id}
      className="flex items-center gap-2 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] px-3 py-2"
    >
      <input
        type="radio"
        name="agent-active-profile"
        checked={p.is_active}
        onChange={() => !p.is_active && activateMut.mutate(p.id)}
        aria-label={`激活档案 ${p.name}`}
        className="size-3.5 shrink-0 accent-[var(--ag-accent)]"
      />
      <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-[var(--ag-text1)]">
        {p.name}
        {p.is_default && (
          <span className="ml-1.5 rounded-full bg-[var(--ag-accent)]/10 px-1.5 py-px text-[10px] font-medium text-[var(--ag-accent)]">
            默认
          </span>
        )}
      </span>
      <span className="shrink-0 text-[10px] tabular-nums text-[var(--ag-text4)]">{profileDate(p.created_at)}</span>
      {!p.is_active && (
        <button
          type="button"
          onClick={() => activateMut.mutate(p.id)}
          disabled={activateMut.isPending}
          aria-label={`切换激活 ${p.name}`}
          className="shrink-0 rounded-md border border-[var(--ag-stroke2)] px-2 py-0.5 text-[11px] text-[var(--ag-text3)] hover:text-[var(--ag-text1)] disabled:opacity-50"
        >
          激活
        </button>
      )}
      {deletable && renderDelete(p)}
    </div>
  )

  const renderDelete = (p: AgentProfile) =>
    confirmDelete === p.id ? (
      <button
        type="button"
        onClick={() => removeMut.mutate(p.id)}
        disabled={removeMut.isPending}
        aria-label={`确认删除档案 ${p.name}`}
        className="shrink-0 rounded-md border border-[var(--ag-red)]/40 px-2 py-0.5 text-[11px] font-medium text-[var(--ag-red)] hover:bg-[var(--ag-red)]/8 disabled:opacity-50"
      >
        确认删除？
      </button>
    ) : (
      <button
        type="button"
        onClick={() => setConfirmDelete(p.id)}
        aria-label={`删除档案 ${p.name}`}
        className="shrink-0 rounded-md border border-[var(--ag-stroke2)] px-2 py-0.5 text-[11px] text-[var(--ag-text3)] hover:text-[var(--ag-red)]"
      >
        <span className="flex items-center gap-1">
          <Trash2 size={11} />
          删除
        </span>
      </button>
    )

  return (
    <div className="max-w-lg">
      <h3 className="mb-1 text-[15px] font-semibold">档案</h3>
      <p className="mb-4 text-[12px] text-[var(--ag-text3)]">
        记忆、技能等上下文按档案隔离；切换档案即可切换助手的作用域。
      </p>

      <div className="space-y-1.5" aria-label="档案列表">
        {isLoading && <p className="py-3 text-center text-[11px] text-[var(--ag-text4)]">加载中…</p>}
        {!isLoading && profiles.length === 0 && (
          <p className="py-3 text-center text-[11px] text-[var(--ag-text4)]">暂无档案</p>
        )}
        {profiles.map((p) => renderRow(p, !p.is_default))}
      </div>

      <div className="mt-3 flex items-center gap-2">
        <input
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          onKeyDown={(e) => {
            if (e.nativeEvent.isComposing) return
            if (e.key === 'Enter') {
              e.preventDefault()
              submitNew()
            }
          }}
          placeholder="新档案名称，如：工作"
          aria-label="新档案名称"
          className="min-w-0 flex-1 rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-sidebar)] px-2.5 py-1.5 text-[12px] text-[var(--ag-text1)] placeholder:text-[var(--ag-text4)] focus:border-[var(--ag-accent)]/60 focus:outline-none"
        />
        <button
          type="button"
          onClick={submitNew}
          disabled={!newName.trim() || createMut.isPending}
          className="flex shrink-0 items-center gap-1 rounded-lg bg-[var(--ag-accent)] px-2.5 py-1.5 text-[12px] font-medium text-white hover:opacity-90 disabled:opacity-40"
        >
          <Plus size={12} />
          新建
        </button>
      </div>

      {isAdmin && adminAll.length > 0 && (
        <div className="mt-5 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-sidebar)]/60 p-3.5">
          <div className="flex items-center gap-1.5">
            <Users size={13} className="text-[var(--ag-accent)]" />
            <span className="text-[13px] font-semibold text-[var(--ag-text1)]">全部用户档案</span>
            <span className="rounded-full border border-[var(--ag-amber)]/40 bg-[var(--ag-amber)]/10 px-1.5 py-px text-[10px] font-medium text-[var(--ag-amber)]">
              管理员
            </span>
          </div>
          <div className="mt-2.5 space-y-3">
            {adminAll.map((u) => (
              <div key={u.user_id}>
                <div className="mb-1 flex items-center gap-1.5">
                  <span className="text-[12px] font-medium text-[var(--ag-text1)]">{u.username}</span>
                  <span className="text-[10px] tabular-nums text-[var(--ag-text4)]">#{u.user_id}</span>
                </div>
                <div className="space-y-1.5">
                  {u.profiles.map((p) => (
                    <div
                      key={p.id}
                      className="flex items-center gap-2 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] px-3 py-1.5"
                    >
                      <span className="min-w-0 flex-1 truncate text-[12px] text-[var(--ag-text1)]">{p.name}</span>
                      {p.is_default && (
                        <span className="shrink-0 rounded-full bg-[var(--ag-accent)]/10 px-1.5 py-px text-[10px] font-medium text-[var(--ag-accent)]">
                          默认
                        </span>
                      )}
                      {p.is_active && (
                        <span className="shrink-0 rounded-full bg-[var(--ag-green)]/10 px-1.5 py-px text-[10px] font-medium text-[var(--ag-green)]">
                          激活中
                        </span>
                      )}
                      {!p.is_default && renderDelete(p)}
                    </div>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

// ── 大号居中设置窗（对标 dsh 桌面版"配置"弹窗：左侧图标导航 + 右侧内容区） ──
export function SettingsModal({ settings, onSave, onClose, providers, version, tools, onTool }: SettingsModalProps) {
  const [section, setSection] = useState<Section>('general')
  const [systemPrompt, setSystemPrompt] = useState(settings.system_prompt)
  const [temperature, setTemperature] = useState(String(settings.temperature))
  const [model, setModel] = useState(settings.model)
  const tts = useTts()

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
          <nav
            aria-label="设置导航"
            className="flex w-40 shrink-0 flex-col gap-0.5 border-r border-[var(--ag-stroke3)] p-2"
          >
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
                {/* 语音输出：开启后回复完成时自动朗读（浏览器 speechSynthesis，零后端） */}
                <div className="mt-4 flex items-center justify-between gap-3 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-sidebar)]/60 px-3 py-2.5">
                  <div className="min-w-0">
                    <div className="text-[12px] font-medium text-[var(--ag-text1)]">朗读助手回复</div>
                    <div className="mt-0.5 text-[11px] text-[var(--ag-text4)]">
                      {tts.supported ? '开启后，助手回复完成时自动朗读（中文语音）' : '当前浏览器不支持语音朗读'}
                    </div>
                  </div>
                  <button
                    type="button"
                    role="switch"
                    aria-checked={tts.enabled}
                    aria-label="朗读助手回复"
                    disabled={!tts.supported}
                    onClick={() => tts.setEnabled(!tts.enabled)}
                    className={cn(
                      'relative h-4 w-7 shrink-0 rounded-full transition-colors disabled:opacity-40',
                      tts.enabled ? 'bg-[var(--ag-green)]' : 'bg-[var(--ag-stroke1)]'
                    )}
                  >
                    <span
                      className={cn(
                        'absolute top-0.5 size-3 rounded-full bg-white transition-all',
                        tts.enabled ? 'left-3.5' : 'left-0.5'
                      )}
                    />
                  </button>
                </div>
              </div>
            )}

            {section === 'tools' && (
              <div className="max-w-lg">
                <h3 className="mb-1 text-[15px] font-semibold">工具</h3>
                <p className="mb-4 text-[12px] text-[var(--ag-text3)]">
                  侧栏收纳的扩展功能，点击打开对应面板。
                </p>
                {(tools || []).length === 0 ? (
                  <p className="text-[12px] text-[var(--ag-text4)]">暂无可用工具</p>
                ) : (
                  <div className="flex flex-col gap-1">
                    {(tools || []).map((t) => (
                      <button
                        key={t.id}
                        type="button"
                        onClick={() => {
                          onTool?.(t.id)
                          onClose()
                        }}
                        className="flex h-9 items-center justify-between rounded-lg border border-[var(--ag-stroke3)] bg-[var(--ag-card)]/60 px-3 text-[13px] font-medium text-[var(--ag-text1)] transition-colors hover:border-[var(--ag-accent)]/40 hover:bg-[var(--ag-active-hover)]"
                      >
                        {t.label}
                        <ChevronRight size={14} className="text-[var(--ag-text4)]" />
                      </button>
                    ))}
                  </div>
                )}
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
                <UserAiKeyCard providers={providers} inputCls={inputCls} />
              </div>
            )}

            {section === 'profiles' && <ProfilesSection />}

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
