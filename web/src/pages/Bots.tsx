import React, { useState, useCallback } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  Bot,
  TrendingUp,
  TrendingDown,
  Wallet,
  Activity,
  PauseCircle,
  Sparkles,
  Terminal,
  ChevronRight,
  BrainCircuit,
  Plus,
  Grid3X3,
  Layers,
  ArrowLeftRight,
  BarChart3,
  Zap,
} from 'lucide-react'
import { cn, formatCurrency } from '@/lib/utils'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import { SectionCard } from '@/components/ui/SectionCard'
import type { BotItem } from '@/hooks/useBotData'
import { useBotData, BOT_TYPES } from '@/hooks/useBotData'
import { BotList } from '@/components/bots/BotList'
import { BotCreateModal, AiCreateDialog } from '@/components/bots/BotCreateModal'
import { StatusBadge, BotDetailView } from '@/components/bots/BotCard'

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
        className="group relative flex w-full items-center gap-4 overflow-hidden rounded-xl border border-[#2a2a2a] bg-[#111111] p-5 text-left transition-all hover:border-[#4f6ed1]/40 hover:shadow-[0_8px_32px_rgba(79,110,209,0.12)]"
      >
        <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-[#4f6ed1]/10 text-[#4f6ed1]">
          <BrainCircuit className="h-6 w-6" />
        </div>
        <div className="flex-1">
          <div className="flex items-center gap-2">
            <h3 className="text-sm font-semibold text-white">AI 智能创建</h3>
            <span className="rounded bg-[#3d5bb5]/15 px-1.5 py-0.5 text-[10px] font-medium text-white">
              Beta
            </span>
          </div>
          <p className="mt-0.5 text-xs text-[#999999]">
            用自然语言描述你的交易想法，AI 自动推荐并生成策略参数
          </p>
        </div>
        <ChevronRight className="h-5 w-5 text-[#757575] transition-colors group-hover:text-[#4f6ed1]" />
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
              <p className="mt-1 text-xs leading-relaxed text-[#999999]">{bt.desc}</p>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

export function Bots() {
  const queryClient = useQueryClient()
  const { bots, isLoading, kpi, actionLoadingId, startBot, stopBot, deleteBot, cloneBot } = useBotData()

  const [viewMode, setViewMode] = useState<'list' | 'detail' | 'create' | 'edit'>('list')
  const [selectedBot, setSelectedBot] = useState<BotItem | null>(null)
  const [selectedBotType, setSelectedBotType] = useState<BotItem['bot_type']>('grid')
  const [aiPreset, setAiPreset] = useState<{
    botType: BotItem['bot_type']
    description: string
    params?: Record<string, unknown>
  } | null>(null)
  const [showAiDialog, setShowAiDialog] = useState(false)

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

  const handleDeleteWithCleanup = useCallback(
    async (bot: BotItem) => {
      await deleteBot(bot)
      if (selectedBot?.id === bot.id) {
        setSelectedBot(null)
        setViewMode('list')
      }
    },
    [deleteBot, selectedBot]
  )

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
            onStart={startBot}
            onStop={stopBot}
            onEdit={handleEditBot}
            onDelete={handleDeleteWithCleanup}
            onClone={cloneBot}
          />
        ) : (
          <>
            {/* Page Header */}
            <PageHeader
              subtitle="管理和监控自动化交易策略"
              actions={
                <button
                  onClick={() => setShowAiDialog(true)}
                  className="flex items-center gap-1.5 rounded-lg bg-[#4f6ed1] px-3 py-2 text-xs font-medium text-white transition-opacity hover:opacity-90"
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
                  <span className="text-xs text-[#8a8a8a]">共 {bots.length} 个</span>
                </div>
              }
            >
              <BotList
                bots={bots}
                loading={isLoading}
                actionLoadingId={actionLoadingId}
                selectedId={selectedBot?.id || null}
                onSelect={handleViewDetail}
                onStart={startBot}
                onStop={stopBot}
                onEdit={handleEditBot}
                onDelete={handleDeleteWithCleanup}
                onViewDetail={handleViewDetail}
              />
            </SectionCard>

            {/* Advanced script entry */}
            <div className="flex items-center justify-center gap-1.5 text-xs text-[#757575]">
              <Terminal className="h-3 w-3" />
              <span>需要完全自定义策略逻辑？</span>
              <a href="#/strategy-script" className="text-[#4f6ed1] transition-colors hover:text-[#8898f3]">
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
      <BotCreateModal
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
