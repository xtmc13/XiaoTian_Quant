import React, { useMemo, useState, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { strategyApi, aiApi } from '@/lib/api'
import { cn, formatCurrency } from '@/lib/utils'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import { SectionCard } from '@/components/ui/SectionCard'
import { EmptyState } from '@/components/ui/EmptyState'
import { Skeleton } from '@/components/ui/Skeleton'
import {
  Bot,
  Play,
  Pause,
  Square,
  TrendingUp,
  TrendingDown,
  Wallet,
  Activity,
  PauseCircle,
  Grid3X3,
  Layers,
  ArrowLeftRight,
  LineChart,
  BrainCircuit,
  Plus,
  Sparkles,
  ArrowLeft,
  Eye,
  Pencil,
  Trash2,
  Copy,
  X,
  ChevronRight,
  Terminal,
  Search,
  SlidersHorizontal,
  RefreshCw,
} from 'lucide-react'

// ── Types ──────────────────────────────────────────────────────────

interface BotItem {
  id: string
  name: string
  strategy_name?: string
  status: 'running' | 'stopped' | 'paused' | 'error'
  bot_type: 'grid' | 'dca' | 'arbitrage' | 'market_making' | 'trend' | 'custom'
  coin?: string
  symbol?: string
  leverage?: number
  unrealized_pnl?: number
  realized_pnl?: number
  initial_capital?: number
  trading_config?: {
    initial_capital?: number
    bot_type?: string
    bot_params?: Record<string, unknown>
    symbol?: string
    timeframe?: string
  }
  created_at?: string
  updated_at?: string
  strategy_code?: string
  market_category?: string
  execution_mode?: 'live' | 'paper' | 'signal'
  notification_config?: { channels: string[]; targets: Record<string, unknown> }
}

interface BotTypeDef {
  key: BotItem['bot_type']
  label: string
  desc: string
  icon: React.ReactNode
  color: string
  bg: string
}

// ── Constants ──────────────────────────────────────────────────────

const BOT_TYPES: BotTypeDef[] = [
  {
    key: 'grid',
    label: '网格交易',
    desc: '在价格区间内自动低买高卖，适合震荡行情',
    icon: <Grid3X3 className="w-6 h-6" />,
    color: '#52c41a',
    bg: 'rgba(82,196,26,0.10)',
  },
  {
    key: 'dca',
    label: '定投策略',
    desc: '定时定额分批买入，平滑持仓成本',
    icon: <Layers className="w-6 h-6" />,
    color: '#1890ff',
    bg: 'rgba(24,144,255,0.10)',
  },
  {
    key: 'arbitrage',
    label: '套利策略',
    desc: '跨市场或跨品种价差套利，低风险收益',
    icon: <ArrowLeftRight className="w-6 h-6" />,
    color: '#722ed1',
    bg: 'rgba(114,46,209,0.10)',
  },
  {
    key: 'market_making',
    label: '做市策略',
    desc: '双边挂单赚取买卖价差，提供流动性',
    icon: <Activity className="w-6 h-6" />,
    color: '#fa8c16',
    bg: 'rgba(250,140,22,0.10)',
  },
  {
    key: 'trend',
    label: '趋势跟踪',
    desc: '跟随市场趋势方向交易，适合趋势行情',
    icon: <LineChart className="w-6 h-6" />,
    color: '#eb2f96',
    bg: 'rgba(235,47,150,0.10)',
  },
  {
    key: 'custom',
    label: '自定义',
    desc: '使用 Python 脚本编写完全自定义的策略逻辑',
    icon: <Terminal className="w-6 h-6" />,
    color: '#13c2c2',
    bg: 'rgba(19,194,194,0.10)',
  },
]

const STATUS_META: Record<string, { label: string; dot: string; border: string; text: string; bg: string }> = {
  running: {
    label: '运行中',
    dot: 'bg-emerald-400',
    border: 'border-emerald-500/20',
    text: 'text-emerald-400',
    bg: 'bg-emerald-500/10',
  },
  paused: {
    label: '已暂停',
    dot: 'bg-amber-400',
    border: 'border-amber-500/20',
    text: 'text-amber-400',
    bg: 'bg-amber-500/10',
  },
  stopped: {
    label: '已停止',
    dot: 'bg-[#555555]',
    border: 'border-[#333333]',
    text: 'text-[#888888]',
    bg: 'bg-[#1c1c1c]',
  },
  error: {
    label: '异常',
    dot: 'bg-red-400',
    border: 'border-red-500/20',
    text: 'text-red-400',
    bg: 'bg-red-500/10',
  },
}

// ── Sub-components ─────────────────────────────────────────────────

function StatusBadge({ status }: { status: string }) {
  const meta = STATUS_META[status] || STATUS_META.stopped
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-[11px] font-medium',
        meta.border,
        meta.text,
        meta.bg
      )}
    >
      <span className={cn('h-1.5 w-1.5 rounded-full', meta.dot)} />
      {meta.label}
    </span>
  )
}

function BotTypeCards({
  onSelect,
  onAiCreate,
}: {
  onSelect: (type: BotItem['bot_type']) => void
  onAiCreate: () => void
}) {
  return (
    <div className="space-y-4">
      {/* AI Smart Create banner */}
      <button
        onClick={onAiCreate}
        className="group relative flex w-full items-center gap-4 overflow-hidden rounded-xl border border-[#2a2a2a] bg-[#111111] p-5 text-left transition-all hover:border-[#667eea]/40 hover:shadow-[0_8px_32px_rgba(102,126,234,0.12)]"
      >
        <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-[#667eea]/10 text-[#667eea]">
          <BrainCircuit className="h-6 w-6" />
        </div>
        <div className="flex-1">
          <div className="flex items-center gap-2">
            <h3 className="text-sm font-semibold text-white">AI 智能创建</h3>
            <span className="rounded bg-[#667eea]/15 px-1.5 py-0.5 text-[10px] font-medium text-[#667eea]">
              Beta
            </span>
          </div>
          <p className="mt-0.5 text-xs text-[#666666]">
            用自然语言描述你的交易想法，AI 自动推荐并生成策略参数
          </p>
        </div>
        <ChevronRight className="h-5 w-5 text-[#444444] transition-colors group-hover:text-[#667eea]" />
      </button>

      {/* Grid of bot types */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {BOT_TYPES.map((bt) => (
          <div
            key={bt.key}
            className="group relative flex flex-col gap-3 rounded-xl border border-[#1c1c1c] bg-[#111111] p-4 transition-all hover:border-[#2a2a2a] hover:shadow-[0_4px_16px_rgba(0,0,0,0.3)]"
          >
            <div className="flex items-start justify-between">
              <div
                className="flex h-10 w-10 items-center justify-center rounded-lg"
                style={{ background: bt.bg, color: bt.color }}
              >
                {bt.icon}
              </div>
              <button
                onClick={() => onSelect(bt.key)}
                className="flex h-7 items-center gap-1 rounded-md bg-white px-2.5 text-xs font-medium text-[#0a0a0a] opacity-0 transition-opacity hover:opacity-90 group-hover:opacity-100"
              >
                <Plus className="h-3 w-3" />
                创建
              </button>
            </div>
            <div>
              <h4 className="text-sm font-semibold text-white">{bt.label}</h4>
              <p className="mt-1 text-xs leading-relaxed text-[#666666]">{bt.desc}</p>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function BotListTable({
  bots,
  loading,
  actionLoadingId,
  selectedId,
  onSelect,
  onStart,
  onStop,
  onEdit,
  onDelete,
  onViewDetail,
}: {
  bots: BotItem[]
  loading: boolean
  actionLoadingId: string | null
  selectedId: string | null
  onSelect: (bot: BotItem) => void
  onStart: (bot: BotItem) => void
  onStop: (bot: BotItem) => void
  onEdit: (bot: BotItem) => void
  onDelete: (bot: BotItem) => void
  onViewDetail: (bot: BotItem) => void
}) {
  if (loading) {
    return (
      <div className="space-y-3">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} variant="rect" height="72px" />
        ))}
      </div>
    )
  }

  if (bots.length === 0) {
    return (
      <EmptyState
        icon={<Bot className="h-6 w-6" />}
        title="暂无交易机器人"
        description="从上方选择一种策略类型创建你的第一个自动化交易机器人，或使用 AI 智能创建。"
      />
    )
  }

  return (
    <div className="space-y-2">
      {bots.map((bot) => {
        const isRunning = bot.status === 'running'
        const pnl = bot.unrealized_pnl ?? 0
        const pnlColor = pnl >= 0 ? 'text-emerald-400' : 'text-red-400'
        const isActionLoading = actionLoadingId === bot.id
        const isSelected = selectedId === bot.id

        return (
          <div
            key={bot.id}
            onClick={() => onSelect(bot)}
            className={cn(
              'group flex cursor-pointer flex-col gap-3 rounded-xl border bg-[#111111] p-4 transition-all sm:flex-row sm:items-center sm:justify-between',
              'border-[#1c1c1c] hover:border-[#2a2a2a] hover:shadow-[0_2px_8px_rgba(0,0,0,0.3)]',
              isSelected && 'border-[#667eea]/30 bg-[#667eea]/[0.06]'
            )}
          >
            {/* Left: icon + name + meta */}
            <div className="flex items-center gap-3">
              <div
                className={cn(
                  'flex h-10 w-10 shrink-0 items-center justify-center rounded-lg',
                  isRunning ? 'bg-emerald-500/10 text-emerald-400' : 'bg-[#1c1c1c] text-[#666666]'
                )}
              >
                <Bot className="h-5 w-5" />
              </div>
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-semibold text-white">
                    {bot.name || bot.strategy_name || '未命名机器人'}
                  </span>
                  <StatusBadge status={bot.status} />
                </div>
                <div className="mt-0.5 flex items-center gap-2 text-[11px] text-[#555555]">
                  <span>{bot.symbol || bot.coin || '-'}</span>
                  <span className="text-[#333333]">·</span>
                  <span className="capitalize">{bot.bot_type || 'custom'}</span>
                  {bot.leverage ? (
                    <>
                      <span className="text-[#333333]">·</span>
                      <span>{bot.leverage}x</span>
                    </>
                  ) : null}
                </div>
              </div>
            </div>

            {/* Middle: PnL + capital */}
            <div className="flex items-center gap-6 sm:justify-end">
              <div className="text-right">
                <div className={cn('font-mono text-sm font-semibold', pnlColor)}>
                  {pnl >= 0 ? '+' : ''}
                  {formatCurrency(pnl)}
                </div>
                <div className="text-[11px] text-[#555555]">未实现盈亏</div>
              </div>
              <div className="text-right">
                <div className="font-mono text-sm font-semibold text-white">
                  ${formatCurrency(bot.initial_capital || bot.trading_config?.initial_capital || 0)}
                </div>
                <div className="text-[11px] text-[#555555]">初始资金</div>
              </div>

              {/* Actions */}
              <div className="flex items-center gap-1">
                {isRunning ? (
                  <>
                    <ActionBtn
                      icon={<Pause className="h-3.5 w-3.5" />}
                      label="暂停"
                      onClick={(e) => {
                        e.stopPropagation()
                        onStop(bot)
                      }}
                      loading={isActionLoading}
                    />
                    <ActionBtn
                      icon={<Square className="h-3.5 w-3.5" />}
                      label="停止"
                      variant="danger"
                      onClick={(e) => {
                        e.stopPropagation()
                        onStop(bot)
                      }}
                      loading={isActionLoading}
                    />
                  </>
                ) : (
                  <ActionBtn
                    icon={<Play className="h-3.5 w-3.5" />}
                    label="启动"
                    variant="primary"
                    onClick={(e) => {
                      e.stopPropagation()
                      onStart(bot)
                    }}
                    loading={isActionLoading}
                  />
                )}
                <ActionBtn
                  icon={<Eye className="h-3.5 w-3.5" />}
                  label="详情"
                  onClick={(e) => {
                    e.stopPropagation()
                    onViewDetail(bot)
                  }}
                />
                <ActionBtn
                  icon={<Pencil className="h-3.5 w-3.5" />}
                  label="编辑"
                  onClick={(e) => {
                    e.stopPropagation()
                    onEdit(bot)
                  }}
                />
                <ActionBtn
                  icon={<Trash2 className="h-3.5 w-3.5" />}
                  label="删除"
                  variant="danger"
                  onClick={(e) => {
                    e.stopPropagation()
                    onDelete(bot)
                  }}
                />
              </div>
            </div>
          </div>
        )
      })}
    </div>
  )
}

function ActionBtn({
  icon,
  label,
  variant = 'default',
  onClick,
  loading,
}: {
  icon: React.ReactNode
  label: string
  variant?: 'default' | 'primary' | 'danger'
  onClick?: (e: React.MouseEvent) => void
  loading?: boolean
}) {
  const variants = {
    default:
      'border-[#1c1c1c] bg-[#141414] text-[#888888] hover:bg-[#1c1c1c] hover:text-white',
    primary: 'border-emerald-500/20 bg-emerald-500/10 text-emerald-400 hover:bg-emerald-500/20',
    danger: 'border-red-500/20 bg-red-500/10 text-red-400 hover:bg-red-500/20',
  }
  return (
    <button
      onClick={onClick}
      disabled={loading}
      title={label}
      className={cn(
        'flex h-8 w-8 items-center justify-center rounded-lg border transition-colors disabled:opacity-50',
        variants[variant]
      )}
    >
      {loading ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : icon}
    </button>
  )
}

function AiCreateDialog({
  open,
  onClose,
  onApply,
}: {
  open: boolean
  onClose: () => void
  onApply: (preset: { botType: BotItem['bot_type']; description: string; params?: Record<string, unknown> }) => void
}) {
  const [prompt, setPrompt] = useState('')
  const [isGenerating, setIsGenerating] = useState(false)

  const handleSubmit = useCallback(async () => {
    if (!prompt.trim()) return
    setIsGenerating(true)
    try {
      const res = await aiApi.chat(prompt)
      // Best-effort parse: if AI returns structured recommendation, use it
      const recommendation = {
        botType: (res?.botType as BotItem['bot_type']) || 'grid',
        description: prompt,
        params: res?.params || {},
      }
      onApply(recommendation)
    } catch {
      // Fallback to grid if AI fails
      onApply({ botType: 'grid', description: prompt })
    } finally {
      setIsGenerating(false)
      setPrompt('')
    }
  }, [prompt, onApply])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="w-full max-w-lg rounded-2xl border border-[#2a2a2a] bg-[#111111] p-6 shadow-2xl">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Sparkles className="h-5 w-5 text-[#667eea]" />
            <h3 className="text-base font-semibold text-white">AI 智能创建机器人</h3>
          </div>
          <button
            onClick={onClose}
            className="flex h-8 w-8 items-center justify-center rounded-lg text-[#666666] transition-colors hover:bg-[#1c1c1c] hover:text-white"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <p className="mt-2 text-sm text-[#666666]">
          描述你的交易目标、风险偏好和标的，AI 将为你推荐最优策略类型与参数。
        </p>

        <div className="mt-4">
          <textarea
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="例如：我想在 BTC/USDT 上做一个低风险的网格策略，投入 5000 USDT，价格区间 25000-35000"
            className="h-32 w-full resize-none rounded-xl border border-[#1c1c1c] bg-[#0a0a0a] p-3 text-sm text-white placeholder-[#444444] outline-none transition-colors focus:border-[#667eea]/40"
          />
        </div>

        <div className="mt-4 flex items-center justify-end gap-2">
          <button
            onClick={onClose}
            className="rounded-lg border border-[#1c1c1c] bg-[#141414] px-4 py-2 text-sm font-medium text-[#888888] transition-colors hover:bg-[#1c1c1c] hover:text-white"
          >
            取消
          </button>
          <button
            onClick={handleSubmit}
            disabled={!prompt.trim() || isGenerating}
            className="flex items-center gap-1.5 rounded-lg bg-[#667eea] px-4 py-2 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:opacity-40"
          >
            {isGenerating && <RefreshCw className="h-3.5 w-3.5 animate-spin" />}
            {isGenerating ? '生成中...' : '生成策略'}
          </button>
        </div>
      </div>
    </div>
  )
}

function CreateWizard({
  open,
  botType,
  aiPreset,
  editBot,
  onCancel,
  onCreated,
  onUpdated,
}: {
  open: boolean
  botType: BotItem['bot_type']
  aiPreset: { botType: BotItem['bot_type']; description: string; params?: Record<string, unknown> } | null
  editBot: BotItem | null
  onCancel: () => void
  onCreated: () => void
  onUpdated: () => void
}) {
  const queryClient = useQueryClient()
  const [step, setStep] = useState(0)
  const [form, setForm] = useState<Record<string, unknown>>({})

  const isEdit = !!editBot
  const effectiveType = aiPreset?.botType || botType || 'grid'

  const createMutation = useMutation({
    mutationFn: (data: any) => strategyApi.create(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
      onCreated()
      setStep(0)
      setForm({})
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: any }) => strategyApi.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
      onUpdated()
      setStep(0)
      setForm({})
    },
  })

  const steps = ['选择类型', '交易标的', '参数配置', '确认创建']

  const handleNext = () => {
    if (step < steps.length - 1) {
      setStep((s) => s + 1)
    } else {
      const payload = {
        strategy_name: (form.name as string) || '未命名机器人',
        strategy_mode: 'bot',
        bot_type: effectiveType,
        market_category: (form.market as string) || 'crypto',
        execution_mode: (form.execution as string) || 'paper',
        trading_config: {
          symbol: form.symbol,
          initial_capital: Number(form.capital) || 1000,
          timeframe: form.timeframe || '1h',
          bot_type: effectiveType,
          bot_params: { ...aiPreset?.params, ...form },
        },
      }
      if (isEdit && editBot) {
        updateMutation.mutate({ id: editBot.id, data: payload })
      } else {
        createMutation.mutate(payload)
      }
    }
  }

  const handleBack = () => {
    if (step > 0) setStep((s) => s - 1)
  }

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="w-full max-w-xl rounded-2xl border border-[#2a2a2a] bg-[#111111] shadow-2xl">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-[#1c1c1c] px-6 py-4">
          <div className="flex items-center gap-3">
            {step > 0 && (
              <button
                onClick={handleBack}
                className="flex h-8 w-8 items-center justify-center rounded-lg text-[#666666] transition-colors hover:bg-[#1c1c1c] hover:text-white"
              >
                <ArrowLeft className="h-4 w-4" />
              </button>
            )}
            <h3 className="text-base font-semibold text-white">
              {isEdit ? '编辑机器人' : '创建机器人'}
            </h3>
          </div>
          <button
            onClick={onCancel}
            className="flex h-8 w-8 items-center justify-center rounded-lg text-[#666666] transition-colors hover:bg-[#1c1c1c] hover:text-white"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Step indicator */}
        <div className="px-6 pt-5">
          <div className="flex items-center gap-2">
            {steps.map((s, i) => (
              <React.Fragment key={s}>
                <div
                  className={cn(
                    'flex h-7 items-center rounded-full px-2.5 text-[11px] font-medium transition-colors',
                    i === step
                      ? 'bg-white text-[#0a0a0a]'
                      : i < step
                        ? 'bg-[#1c1c1c] text-[#888888]'
                        : 'bg-[#141414] text-[#555555]'
                  )}
                >
                  {i + 1}. {s}
                </div>
                {i < steps.length - 1 && (
                  <div className={cn('h-px w-4', i < step ? 'bg-[#888888]' : 'bg-[#1c1c1c]')} />
                )}
              </React.Fragment>
            ))}
          </div>
        </div>

        {/* Body */}
        <div className="px-6 py-5">
          {step === 0 && (
            <div className="space-y-3">
              <p className="text-sm text-[#666666]">当前选择的策略类型：</p>
              <div className="flex items-center gap-3 rounded-xl border border-[#1c1c1c] bg-[#0a0a0a] p-4">
                <div
                  className="flex h-10 w-10 items-center justify-center rounded-lg"
                  style={{
                    background: BOT_TYPES.find((b) => b.key === effectiveType)?.bg,
                    color: BOT_TYPES.find((b) => b.key === effectiveType)?.color,
                  }}
                >
                  {BOT_TYPES.find((b) => b.key === effectiveType)?.icon}
                </div>
                <div>
                  <div className="text-sm font-semibold text-white">
                    {BOT_TYPES.find((b) => b.key === effectiveType)?.label}
                  </div>
                  <div className="text-xs text-[#666666]">
                    {BOT_TYPES.find((b) => b.key === effectiveType)?.desc}
                  </div>
                </div>
              </div>
              {aiPreset && (
                <div className="rounded-lg border border-[#667eea]/20 bg-[#667eea]/[0.06] p-3 text-xs text-[#888888]">
                  <span className="font-medium text-[#667eea]">AI 推荐：</span>
                  {aiPreset.description}
                </div>
              )}
            </div>
          )}

          {step === 1 && (
            <div className="space-y-4">
              <WizardField label="交易标的" hint="例如 BTC/USDT">
                <input
                  value={(form.symbol as string) || ''}
                  onChange={(e) => setForm((f) => ({ ...f, symbol: e.target.value }))}
                  placeholder="BTC/USDT"
                  className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white placeholder-[#444444] outline-none focus:border-[#667eea]/40"
                />
              </WizardField>
              <WizardField label="市场类型">
                <div className="flex gap-2">
                  {['spot', 'futures'].map((m) => (
                    <button
                      key={m}
                      onClick={() => setForm((f) => ({ ...f, market: m }))}
                      className={cn(
                        'rounded-lg border px-3 py-2 text-xs font-medium transition-colors',
                        (form.market as string) === m
                          ? 'border-white/20 bg-white/10 text-white'
                          : 'border-[#1c1c1c] bg-[#141414] text-[#666666] hover:text-[#888888]'
                      )}
                    >
                      {m === 'spot' ? '现货' : '合约'}
                    </button>
                  ))}
                </div>
              </WizardField>
            </div>
          )}

          {step === 2 && (
            <div className="space-y-4">
              <WizardField label="初始资金 (USDT)">
                <input
                  type="number"
                  value={(form.capital as number) || ''}
                  onChange={(e) => setForm((f) => ({ ...f, capital: e.target.value }))}
                  placeholder="5000"
                  className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white placeholder-[#444444] outline-none focus:border-[#667eea]/40"
                />
              </WizardField>
              <WizardField label="执行模式">
                <div className="flex gap-2">
                  {[
                    { key: 'paper', label: '模拟盘' },
                    { key: 'signal', label: '信号模式' },
                    { key: 'live', label: '实盘' },
                  ].map((m) => (
                    <button
                      key={m.key}
                      onClick={() => setForm((f) => ({ ...f, execution: m.key }))}
                      className={cn(
                        'rounded-lg border px-3 py-2 text-xs font-medium transition-colors',
                        (form.execution as string) === m.key
                          ? 'border-white/20 bg-white/10 text-white'
                          : 'border-[#1c1c1c] bg-[#141414] text-[#666666] hover:text-[#888888]'
                      )}
                    >
                      {m.label}
                    </button>
                  ))}
                </div>
              </WizardField>
              <WizardField label="名称">
                <input
                  value={(form.name as string) || ''}
                  onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                  placeholder="我的网格策略 #1"
                  className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white placeholder-[#444444] outline-none focus:border-[#667eea]/40"
                />
              </WizardField>
            </div>
          )}

          {step === 3 && (
            <div className="space-y-3">
              <h4 className="text-sm font-semibold text-white">配置确认</h4>
              <div className="rounded-xl border border-[#1c1c1c] bg-[#0a0a0a] p-4 space-y-2">
                <ConfirmRow label="策略类型" value={BOT_TYPES.find((b) => b.key === effectiveType)?.label} />
                <ConfirmRow label="交易标的" value={(form.symbol as string) || '-'} />
                <ConfirmRow label="初始资金" value={`$${formatCurrency(Number(form.capital) || 0)}`} />
                <ConfirmRow
                  label="执行模式"
                  value={
                    { paper: '模拟盘', signal: '信号模式', live: '实盘' }[(form.execution as string) || 'paper']
                  }
                />
                <ConfirmRow label="名称" value={(form.name as string) || '未命名'} />
              </div>
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 border-t border-[#1c1c1c] px-6 py-4">
          <button
            onClick={onCancel}
            className="rounded-lg border border-[#1c1c1c] bg-[#141414] px-4 py-2 text-sm font-medium text-[#888888] transition-colors hover:bg-[#1c1c1c] hover:text-white"
          >
            取消
          </button>
          <button
            onClick={handleNext}
            disabled={createMutation.isPending || updateMutation.isPending}
            className="flex items-center gap-1.5 rounded-lg bg-white px-4 py-2 text-sm font-medium text-[#0a0a0a] transition-opacity hover:opacity-90 disabled:opacity-40"
          >
            {(createMutation.isPending || updateMutation.isPending) && (
              <RefreshCw className="h-3.5 w-3.5 animate-spin" />
            )}
            {step === steps.length - 1 ? (isEdit ? '保存修改' : '创建机器人') : '下一步'}
          </button>
        </div>
      </div>
    </div>
  )
}

function WizardField({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: React.ReactNode
}) {
  return (
    <div>
      <div className="mb-1.5 flex items-center gap-2">
        <label className="text-xs font-medium text-[#aaaaaa]">{label}</label>
        {hint && <span className="text-[11px] text-[#555555]">{hint}</span>}
      </div>
      {children}
    </div>
  )
}

function ConfirmRow({ label, value }: { label: string; value?: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between text-sm">
      <span className="text-[#666666]">{label}</span>
      <span className="font-medium text-white">{value}</span>
    </div>
  )
}

function BotDetailView({
  bot,
  onBack,
  onStart,
  onStop,
  onEdit,
  onDelete,
  onClone,
}: {
  bot: BotItem
  onBack: () => void
  onStart: (bot: BotItem) => void
  onStop: (bot: BotItem) => void
  onEdit: (bot: BotItem) => void
  onDelete: (bot: BotItem) => void
  onClone: (bot: BotItem) => void
}) {
  const isRunning = bot.status === 'running'
  const totalPnl = (bot.unrealized_pnl || 0) + (bot.realized_pnl || 0)
  const pnlColor = totalPnl >= 0 ? 'text-emerald-400' : 'text-red-400'

  return (
    <div className="space-y-6">
      {/* Back */}
      <button
        onClick={onBack}
        className="flex items-center gap-1.5 text-sm text-[#666666] transition-colors hover:text-white"
      >
        <ArrowLeft className="h-4 w-4" />
        返回列表
      </button>

      {/* Header card */}
      <div className="rounded-xl border border-[#1c1c1c] bg-[#111111] p-5">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-3">
            <div
              className={cn(
                'flex h-12 w-12 items-center justify-center rounded-xl',
                isRunning ? 'bg-emerald-500/10 text-emerald-400' : 'bg-[#1c1c1c] text-[#666666]'
              )}
            >
              <Bot className="h-6 w-6" />
            </div>
            <div>
              <h2 className="text-lg font-semibold text-white">
                {bot.name || bot.strategy_name || '未命名机器人'}
              </h2>
              <div className="mt-1 flex items-center gap-2">
                <StatusBadge status={bot.status} />
                <span className="text-xs text-[#555555]">
                  {bot.symbol || bot.coin || '-'} · {bot.bot_type}
                </span>
              </div>
            </div>
          </div>

          <div className="flex items-center gap-2">
            {isRunning ? (
              <button
                onClick={() => onStop(bot)}
                className="flex items-center gap-1.5 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs font-medium text-red-400 transition-colors hover:bg-red-500/20"
              >
                <Square className="h-3.5 w-3.5" />
                停止
              </button>
            ) : (
              <button
                onClick={() => onStart(bot)}
                className="flex items-center gap-1.5 rounded-lg border border-emerald-500/20 bg-emerald-500/10 px-3 py-2 text-xs font-medium text-emerald-400 transition-colors hover:bg-emerald-500/20"
              >
                <Play className="h-3.5 w-3.5" />
                启动
              </button>
            )}
            <button
              onClick={() => onEdit(bot)}
              className="flex items-center gap-1.5 rounded-lg border border-[#1c1c1c] bg-[#141414] px-3 py-2 text-xs font-medium text-[#888888] transition-colors hover:bg-[#1c1c1c] hover:text-white"
            >
              <Pencil className="h-3.5 w-3.5" />
              编辑
            </button>
            <button
              onClick={() => onClone(bot)}
              className="flex items-center gap-1.5 rounded-lg border border-[#1c1c1c] bg-[#141414] px-3 py-2 text-xs font-medium text-[#888888] transition-colors hover:bg-[#1c1c1c] hover:text-white"
            >
              <Copy className="h-3.5 w-3.5" />
              克隆
            </button>
            <button
              onClick={() => onDelete(bot)}
              className="flex items-center gap-1.5 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs font-medium text-red-400 transition-colors hover:bg-red-500/20"
            >
              <Trash2 className="h-3.5 w-3.5" />
              删除
            </button>
          </div>
        </div>
      </div>

      {/* Stats grid */}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <KPICard
          icon={<Wallet className="h-4 w-4" />}
          label="初始资金"
          value={`$${formatCurrency(bot.initial_capital || bot.trading_config?.initial_capital || 0)}`}
        />
        <KPICard
          icon={totalPnl >= 0 ? <TrendingUp className="h-4 w-4" /> : <TrendingDown className="h-4 w-4" />}
          label="总盈亏"
          value={`${totalPnl >= 0 ? '+' : ''}${formatCurrency(totalPnl)}`}
          trend={totalPnl >= 0 ? 'up' : 'down'}
        />
        <KPICard
          icon={<Activity className="h-4 w-4" />}
          label="未实现盈亏"
          value={`${(bot.unrealized_pnl || 0) >= 0 ? '+' : ''}${formatCurrency(bot.unrealized_pnl || 0)}`}
          trend={(bot.unrealized_pnl || 0) >= 0 ? 'up' : 'down'}
        />
        <KPICard
          icon={<LineChart className="h-4 w-4" />}
          label="已实现盈亏"
          value={`${(bot.realized_pnl || 0) >= 0 ? '+' : ''}${formatCurrency(bot.realized_pnl || 0)}`}
          trend={(bot.realized_pnl || 0) >= 0 ? 'up' : 'down'}
        />
      </div>

      {/* Performance chart placeholder */}
      <SectionCard title="收益曲线">
        <div className="flex h-48 items-center justify-center rounded-lg border border-dashed border-[#1c1c1c] bg-[#0a0a0a]">
          <div className="text-center">
            <LineChart className="mx-auto h-8 w-8 text-[#333333]" />
            <p className="mt-2 text-xs text-[#555555]">收益曲线图表占位</p>
          </div>
        </div>
      </SectionCard>

      {/* Logs */}
      <SectionCard title="运行日志">
        <div className="flex h-40 items-center justify-center rounded-lg border border-dashed border-[#1c1c1c] bg-[#0a0a0a]">
          <div className="text-center">
            <Terminal className="mx-auto h-8 w-8 text-[#333333]" />
            <p className="mt-2 text-xs text-[#555555]">日志输出占位</p>
          </div>
        </div>
      </SectionCard>

      {/* Config summary */}
      <SectionCard title="配置详情">
        <div className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-3">
          <ConfigItem label="策略类型" value={bot.bot_type || '-'} />
          <ConfigItem label="标的" value={bot.symbol || bot.coin || '-'} />
          <ConfigItem label="杠杆" value={bot.leverage ? `${bot.leverage}x` : '-'} />
          <ConfigItem label="市场" value={bot.market_category || 'crypto'} />
          <ConfigItem label="执行模式" value={bot.execution_mode || 'paper'} />
          <ConfigItem
            label="创建时间"
            value={bot.created_at ? new Date(bot.created_at).toLocaleString() : '-'}
          />
        </div>
      </SectionCard>
    </div>
  )
}

function ConfigItem({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2">
      <div className="text-[11px] text-[#555555]">{label}</div>
      <div className="mt-0.5 text-sm font-medium text-white">{value}</div>
    </div>
  )
}

// ── Main Page ──────────────────────────────────────────────────────

export function Bots() {
  const queryClient = useQueryClient()
  const [viewMode, setViewMode] = useState<'list' | 'detail' | 'create' | 'edit'>('list')
  const [selectedBot, setSelectedBot] = useState<BotItem | null>(null)
  const [selectedBotType, setSelectedBotType] = useState<BotItem['bot_type']>('grid')
  const [aiPreset, setAiPreset] = useState<{
    botType: BotItem['bot_type']
    description: string
    params?: Record<string, unknown>
  } | null>(null)
  const [showAiDialog, setShowAiDialog] = useState(false)
  const [actionLoadingId, setActionLoadingId] = useState<string | null>(null)

  const { data: strategies, isLoading } = useQuery({
    queryKey: ['strategies'],
    queryFn: () => strategyApi.list(),
    refetchInterval: 5000,
  })

  const bots: BotItem[] = useMemo(() => {
    const all = Array.isArray(strategies) ? strategies : []
    return all
      .filter(
        (s: any) =>
          s.strategy_mode === 'bot' || s.bot_type || (s.trading_config && s.trading_config.bot_type)
      )
      .map((s: any) => ({
        ...s,
        id: String(s.id),
        bot_type: s.bot_type || s.trading_config?.bot_type || 'custom',
        name: s.strategy_name || s.name,
      }))
  }, [strategies])

  const kpi = useMemo(() => {
    const running = bots.filter((b) => b.status === 'running').length
    const stopped = bots.filter((b) => b.status === 'stopped').length
    const totalEquity = bots.reduce((sum, b) => sum + (b.initial_capital || b.trading_config?.initial_capital || 0), 0)
    const totalPnl = bots.reduce((sum, b) => sum + (b.unrealized_pnl || 0), 0)
    return { running, stopped, total: bots.length, totalEquity, totalPnl }
  }, [bots])

  const handleStartBot = useCallback(
    async (bot: BotItem) => {
      setActionLoadingId(bot.id)
      try {
        await strategyApi.start(bot.id)
        queryClient.invalidateQueries({ queryKey: ['strategies'] })
      } finally {
        setActionLoadingId(null)
      }
    },
    [queryClient]
  )

  const handleStopBot = useCallback(
    async (bot: BotItem) => {
      setActionLoadingId(bot.id)
      try {
        await strategyApi.stop(bot.id)
        queryClient.invalidateQueries({ queryKey: ['strategies'] })
      } finally {
        setActionLoadingId(null)
      }
    },
    [queryClient]
  )

  const handleDeleteBot = useCallback(
    async (bot: BotItem) => {
      if (bot.status === 'running') {
        alert('请先停止机器人再删除')
        return
      }
      if (!confirm(`确定删除机器人 "${bot.name || bot.strategy_name}" 吗？`)) return
      try {
        await strategyApi.delete(bot.id)
        if (selectedBot?.id === bot.id) {
          setSelectedBot(null)
          setViewMode('list')
        }
        queryClient.invalidateQueries({ queryKey: ['strategies'] })
      } catch {
        alert('删除失败')
      }
    },
    [queryClient, selectedBot]
  )

  const handleEditBot = useCallback((bot: BotItem) => {
    if (bot.status === 'running') {
      alert('请先停止机器人再编辑')
      return
    }
    setSelectedBot(bot)
    setAiPreset(null)
    setViewMode('edit')
  }, [])

  const handleViewDetail = useCallback((bot: BotItem) => {
    setSelectedBot(bot)
    setViewMode('detail')
  }, [])

  const handleSelectBotType = useCallback((type: BotItem['bot_type']) => {
    setSelectedBotType(type)
    setAiPreset(null)
    setSelectedBot(null)
    setViewMode('create')
  }, [])

  const handleAiApply = useCallback(
    (preset: { botType: BotItem['bot_type']; description: string; params?: Record<string, unknown> }) => {
      setShowAiDialog(false)
      setSelectedBotType(preset.botType)
      setAiPreset(preset)
      setSelectedBot(null)
      setViewMode('create')
    },
    []
  )

  const handleWizardCancel = useCallback(() => {
    setViewMode('list')
    setSelectedBot(null)
    setAiPreset(null)
  }, [])

  const handleBotCreated = useCallback(() => {
    setViewMode('list')
    setSelectedBot(null)
    setAiPreset(null)
    queryClient.invalidateQueries({ queryKey: ['strategies'] })
  }, [queryClient])

  const handleBotUpdated = useCallback(() => {
    setViewMode('list')
    setSelectedBot(null)
    setAiPreset(null)
    queryClient.invalidateQueries({ queryKey: ['strategies'] })
  }, [queryClient])

  const handleCloneBot = useCallback(
    async (bot: BotItem) => {
      if (!bot.strategy_code) {
        alert('该机器人没有可克隆的策略代码')
        return
      }
      if (!confirm(`克隆机器人 "${bot.name || bot.strategy_name}" 为脚本策略？`)) return
      try {
        const tc = bot.trading_config ? JSON.parse(JSON.stringify(bot.trading_config)) : {}
        delete tc.bot_type
        delete tc.bot_params
        await strategyApi.create({
          strategy_name: `${bot.name || bot.strategy_name} (克隆)`,
          strategy_type: 'ScriptStrategy',
          strategy_mode: 'script',
          strategy_code: bot.strategy_code,
          market_category: bot.market_category || tc.market_category || 'crypto',
          execution_mode: 'signal',
          notification_config: bot.notification_config || { channels: [], targets: {} },
          trading_config: tc,
        })
        queryClient.invalidateQueries({ queryKey: ['strategies'] })
      } catch {
        alert('克隆失败')
      }
    },
    [queryClient]
  )

  // ── Render ───────────────────────────────────────────────────────

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="space-y-6">
        {viewMode === 'detail' && selectedBot ? (
          <BotDetailView
            bot={selectedBot}
            onBack={() => {
              setViewMode('list')
              setSelectedBot(null)
            }}
            onStart={handleStartBot}
            onStop={handleStopBot}
            onEdit={handleEditBot}
            onDelete={handleDeleteBot}
            onClone={handleCloneBot}
          />
        ) : (
          <>
            {/* Page Header */}
            <PageHeader
              subtitle="管理和监控自动化交易策略"
              actions={
                <button
                  onClick={() => setShowAiDialog(true)}
                  className="flex items-center gap-1.5 rounded-lg bg-[#667eea] px-3 py-2 text-xs font-medium text-white transition-opacity hover:opacity-90"
                >
                  <Sparkles className="h-3.5 w-3.5" />
                  AI 智能创建
                </button>
              }
            />

            {/* KPI Row */}
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
              <KPICard
                icon={<Wallet className="h-4 w-4 text-[#1890ff]" />}
                label="总权益"
                value={`$${formatCurrency(kpi.totalEquity)}`}
                ringProgress={kpi.total > 0 ? (kpi.running / kpi.total) * 100 : 0}
              />
              <KPICard
                icon={kpi.totalPnl >= 0 ? <TrendingUp className="h-4 w-4 text-emerald-400" /> : <TrendingDown className="h-4 w-4 text-red-400" />}
                label="总盈亏"
                value={`${kpi.totalPnl >= 0 ? '+' : ''}${formatCurrency(kpi.totalPnl)}`}
                trend={kpi.totalPnl >= 0 ? 'up' : 'down'}
                primary
              />
              <KPICard
                icon={<Bot className="h-4 w-4 text-[#722ed1]" />}
                label="运行 / 停止"
                value={`${kpi.running} / ${kpi.stopped}`}
                subValue={`共 ${kpi.total} 个`}
              />
              <KPICard
                icon={<PauseCircle className="h-4 w-4 text-[#faad14]" />}
                label="已停止"
                value={String(kpi.stopped)}
              />
            </div>

            {/* Bot Type Cards */}
            <SectionCard title="选择策略类型" headerAction={null}>
              <BotTypeCards onSelect={handleSelectBotType} onAiCreate={() => setShowAiDialog(true)} />
            </SectionCard>

            {/* Bot List */}
            <SectionCard
              title="机器人列表"
              headerAction={
                <div className="flex items-center gap-2">
                  <span className="text-xs text-[#555555]">共 {bots.length} 个</span>
                </div>
              }
            >
              <BotListTable
                bots={bots}
                loading={isLoading}
                actionLoadingId={actionLoadingId}
                selectedId={selectedBot?.id || null}
                onSelect={handleViewDetail}
                onStart={handleStartBot}
                onStop={handleStopBot}
                onEdit={handleEditBot}
                onDelete={handleDeleteBot}
                onViewDetail={handleViewDetail}
              />
            </SectionCard>

            {/* Advanced script entry */}
            <div className="flex items-center justify-center gap-1.5 text-xs text-[#444444]">
              <Terminal className="h-3 w-3" />
              <span>需要完全自定义策略逻辑？</span>
              <a href="#/strategy-script" className="text-[#667eea] transition-colors hover:text-[#8898f3]">
                前往脚本策略
                <ChevronRight className="inline h-3 w-3" />
              </a>
            </div>
          </>
        )}
      </div>

      {/* AI Create Dialog */}
      <AiCreateDialog
        open={showAiDialog}
        onClose={() => setShowAiDialog(false)}
        onApply={handleAiApply}
      />

      {/* Create/Edit Wizard */}
      <CreateWizard
        open={viewMode === 'create' || viewMode === 'edit'}
        botType={selectedBotType}
        aiPreset={aiPreset}
        editBot={viewMode === 'edit' ? selectedBot : null}
        onCancel={handleWizardCancel}
        onCreated={handleBotCreated}
        onUpdated={handleBotUpdated}
      />
    </div>
  )
}
