import { useState, useCallback } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import {
  Bot, TrendingUp, TrendingDown, Wallet, PauseCircle,
  Terminal, ChevronRight, Activity
} from 'lucide-react'
import { formatCurrency } from '@/lib/utils'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import { SectionCard } from '@/components/ui/SectionCard'
import type { BotItem } from '@/hooks/useBotData'
import { useBotData } from '@/hooks/useBotData'
import { toast } from '@/lib/useToast'
import { BotList } from '@/components/bots/BotList'
import { BotDetailView } from '@/components/bots/BotCard'

export function Bots() {
  const navigate = useNavigate()
  const { bots, isLoading, kpi, actionLoadingId, startBot, stopBot, deleteBot, cloneBot } = useBotData()

  const [viewMode, setViewMode] = useState<'list' | 'detail'>('list')
  const [selectedBot, setSelectedBot] = useState<BotItem | null>(null)

  const handleViewDetail = useCallback((bot: BotItem) => {
    setSelectedBot(bot)
    setViewMode('detail')
  }, [])

  const handleEditBot = useCallback((bot: BotItem) => {
    if (bot.status === 'running') {
      toast('info', '请先停止再编辑')
      return
    }
    navigate(`/strategy?tab=strategy&edit=${bot.id}`)
  }, [navigate])

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

  const handleBack = useCallback(() => {
    setViewMode('list')
    setSelectedBot(null)
  }, [])

  // Running strategies for quick status view
  const runningBots = bots.filter(b => b.status === 'running')
  const runningPnl = runningBots.reduce((s, b) => s + (b.unrealized_pnl || 0), 0)

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="space-y-6">
        {viewMode === 'detail' && selectedBot ? (
          <BotDetailView
            bot={selectedBot}
            onBack={handleBack}
            onStart={startBot}
            onStop={stopBot}
            onEdit={handleEditBot}
            onDelete={handleDeleteWithCleanup}
            onClone={cloneBot}
          />
        ) : (
          <>
            <PageHeader subtitle="实时监控所有策略的运行状态" />

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
                icon={<Activity className="h-4 w-4 text-[#722ed1]" />}
                label="运行中"
                value={String(kpi.running)}
                subValue={`共 ${kpi.total} 个`}
              />
              <KPICard
                icon={<PauseCircle className="h-4 w-4 text-[#faad14]" />}
                label="已停止"
                value={String(kpi.stopped)}
              />
            </div>

            {/* Running strategies mini-summary */}
            {runningBots.length > 0 && (
              <div className="rounded-xl border border-quant-border bg-quant-bg-secondary/50 px-4 py-3">
                <div className="flex items-center justify-between text-xs">
                  <span className="text-muted-foreground">
                    运行中策略 <strong className="text-foreground">{runningBots.length}</strong> 个
                  </span>
                  <span className={runningPnl >= 0 ? 'text-quant-green' : 'text-quant-red'}>
                    实时盈亏 {runningPnl >= 0 ? '+' : ''}${formatCurrency(runningPnl)}
                  </span>
                </div>
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {runningBots.slice(0, 10).map(b => (
                    <span key={b.id} className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-quant-green/10 text-[10px] text-quant-green border border-quant-green/20">
                      <span className="w-1.5 h-1.5 rounded-full bg-quant-green animate-pulse" />
                      {b.name || b.strategy_name}
                    </span>
                  ))}
                </div>
              </div>
            )}

            {/* Bot List */}
            <SectionCard
              title="策略总览"
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
              <span>创建或编辑策略请前往</span>
              <Link to="/strategy" className="text-[#4f6ed1] transition-colors hover:text-[#8898f3]">
                策略管理
                <ChevronRight className="inline h-3 w-3" />
              </Link>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
