import { useState, useCallback } from 'react'
import { useNavigate, Link, useLocation } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Bot, TrendingUp, TrendingDown, Wallet, PauseCircle,
  Terminal, ChevronRight, Activity, Radio, BrainCircuit,
  BarChart3, Plus, Play, Square, Pencil, Trash2, Copy,
  Settings2,
} from 'lucide-react'
import { formatCurrency, cn } from '@/lib/utils'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import { SectionCard } from '@/components/ui/SectionCard'
import { Badge } from '@/components/ui/Badge'
import { Skeleton } from '@/components/ui/Skeleton'
import { Button } from '@/components/ui/Button'
import type { BotItem } from '@/hooks/useBotData'
import { useBotData, STATUS_META, BOT_TYPES } from '@/hooks/useBotData'
import { BotList } from '@/components/bots/BotList'
import { BotDetailView } from '@/components/bots/BotCard'
import { SignalExecutorPanel } from '@/components/bots/SignalExecutorPanel'
import { AIRobotPanel } from '@/components/bots/AIRobotPanel'
import { StrategyConfigPanel } from '@/components/bots/StrategyConfigPanel'
import { strategyConfigApi, strategyApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import type { MartinConfig, WallStreetConfig } from '@/types'

// 三机器类型的元数据
type BotType = 'strategy' | 'signal' | 'ai'

const BOT_TYPE_META: Record<BotType, { title: string; subtitle: string; icon: React.ReactNode; emptyTitle: string; emptyDesc: string }> = {
  strategy: {
    title: '策略机器人',
    subtitle: '自主扫描市场、计算指标、生成信号并自动执行',
    icon: <Bot className="w-5 h-5" />,
    emptyTitle: '暂无策略机器人',
    emptyDesc: '策略机器人会自主扫描K线数据，根据内置策略自动交易。创建你的第一个策略机器人吧。',
  },
  signal: {
    title: '信号机器人',
    subtitle: '接收外部信号并按阶梯止盈/止损自动执行',
    icon: <Radio className="w-5 h-5" />,
    emptyTitle: '暂无信号机器人',
    emptyDesc: '信号机器人监听外部信号（Webhook/API/IDE），自动跟单执行并管理TP/SL。创建你的第一个信号机器人吧。',
  },
  ai: {
    title: 'AI 机器人',
    subtitle: 'AI 驱动决策，智能过滤波动市场，带置信度评估',
    icon: <BrainCircuit className="w-5 h-5" />,
    emptyTitle: '暂无 AI 机器人',
    emptyDesc: 'AI 机器人使用大语言模型分析市场，在波动市场中自动过滤信号。创建你的第一个 AI 机器人吧。',
  },
}

const TAB_LIST: { key: BotType; label: string; icon: React.ReactNode }[] = [
  { key: 'strategy', label: '策略', icon: <Bot className="w-4 h-4" /> },
  { key: 'signal', label: '信号', icon: <Radio className="w-4 h-4" /> },
  { key: 'ai', label: 'AI', icon: <BrainCircuit className="w-4 h-4" /> },
]

/* ── 策略卡片扩展：显示马丁/华尔街参数 ── */
interface StrategyCardProps {
  bot: BotItem
  actionLoadingId: string | null
  onStart: (bot: BotItem) => void
  onStop: (bot: BotItem) => void
  onEdit: (bot: BotItem) => void
  onDelete: (bot: BotItem) => void
  onClone: (bot: BotItem) => void
  onViewDetail: (bot: BotItem) => void
}

const StrategyCard: React.FC<StrategyCardProps> = ({
  bot,
  actionLoadingId,
  onStart,
  onStop,
  onEdit,
  onDelete,
  onClone,
  onViewDetail,
}) => {
  const meta = STATUS_META[bot.status]
  const isRunning = bot.status === 'running'
  const isLoading = actionLoadingId === bot.id

  const isMartin = bot.bot_type === 'martin_trend'
  const isWallStreet = bot.bot_type === 'wallstreet'
  const showParams = isMartin || isWallStreet

  const tc = bot.trading_config

  return (
    <div
      className={cn(
        'rounded-xl border bg-[#111] transition-all hover:border-[#333]',
        meta?.border || 'border-[#1c1c1c]'
      )}
    >
      <div className="p-4">
        {/* Header */}
        <div className="flex items-start justify-between mb-3">
          <div className="flex items-center gap-2 min-w-0">
            <span className={cn('w-2 h-2 rounded-full flex-shrink-0', meta?.dot || 'bg-[#555]')} />
            <span className="text-sm font-medium text-[#e0e0e0] truncate">
              {bot.name || bot.strategy_name}
            </span>
          </div>
          <div className="flex items-center gap-1 flex-shrink-0">
            {isMartin && (
              <Badge variant="error" className="text-[10px]">马丁</Badge>
            )}
            {isWallStreet && (
              <Badge variant="warning" className="text-[10px]">华尔街</Badge>
            )}
            <span className={cn('text-[10px] px-1.5 py-0.5 rounded-full', meta?.bg, meta?.text)}>
              {meta?.label}
            </span>
          </div>
        </div>

        {/* PnL */}
        <div className="flex items-center justify-between mb-3">
          <span className="text-xs text-[#888]">
            {bot.symbol || tc?.symbol || 'BTCUSDT'}
          </span>
          <span className={cn(
            'text-sm font-semibold',
            (bot.unrealized_pnl || 0) >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]'
          )}>
            {(bot.unrealized_pnl || 0) >= 0 ? '+' : ''}
            {formatCurrency(bot.unrealized_pnl || 0)}
          </span>
        </div>

        {/* Strategy Params */}
        {showParams && tc && (
          <div className="rounded-lg bg-[#0a0a0a] border border-[#1c1c1c] p-2.5 mb-3">
            <div className="grid grid-cols-2 gap-x-3 gap-y-1 text-[10px]">
              {tc.first_order_amount !== undefined && (
                <div className="text-[#888]">
                  首单: <span className="text-[#ccc]">${tc.first_order_amount}</span>
                </div>
              )}
              {tc.order_count !== undefined && (
                <div className="text-[#888]">
                  单数: <span className="text-[#ccc]">{tc.order_count}</span>
                </div>
              )}
              {tc.add_position_spread !== undefined && (
                <div className="text-[#888]">
                  价差: <span className="text-[#ccc]">{tc.add_position_spread}%</span>
                </div>
              )}
              {tc.take_profit_ratio !== undefined && (
                <div className="text-[#888]">
                  止盈: <span className="text-[#ccc]">{tc.take_profit_ratio}%</span>
                </div>
              )}
              {tc.add_position_callback !== undefined && (
                <div className="text-[#888]">
                  回调: <span className="text-[#ccc]">{tc.add_position_callback}%</span>
                </div>
              )}
              {tc.trade_count_mode && (
                <div className="text-[#888]">
                  循环: <span className="text-[#ccc]">{tc.trade_count_mode === 'cycle' ? '循环' : '单次'}</span>
                </div>
              )}
              {tc.open_double !== undefined && tc.open_double && (
                <div className="text-[#faad14]">首单加倍 ✓</div>
              )}
              {tc.waterfall_protection !== undefined && tc.waterfall_protection > 0 && (
                <div className="text-[#1890ff]">
                  防瀑布: {tc.waterfall_protection}%
                </div>
              )}
            </div>
          </div>
        )}

        {/* Actions */}
        <div className="flex items-center gap-1.5">
          {isRunning ? (
            <Button
              variant="ghost"
              size="sm"
              isLoading={isLoading}
              onClick={() => onStop(bot)}
              leftIcon={<Square className="w-3 h-3 text-[#f5222d]" />}
              className="text-[#f5222d] hover:bg-[#f5222d]/10"
            >
              停止
            </Button>
          ) : (
            <Button
              variant="ghost"
              size="sm"
              isLoading={isLoading}
              onClick={() => onStart(bot)}
              leftIcon={<Play className="w-3 h-3 text-[#52c41a]" />}
              className="text-[#52c41a] hover:bg-[#52c41a]/10"
            >
              启动
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onEdit(bot)}
            leftIcon={<Pencil className="w-3 h-3" />}
          >
            编辑
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onClone(bot)}
            leftIcon={<Copy className="w-3 h-3" />}
          >
            克隆
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onDelete(bot)}
            leftIcon={<Trash2 className="w-3 h-3 text-[#f5222d]" />}
            className="text-[#f5222d] hover:bg-[#f5222d]/10"
          >
            删除
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onViewDetail(bot)}
            className="ml-auto"
          >
            <ChevronRight className="w-3 h-3" />
          </Button>
        </div>
      </div>
    </div>
  )
}

/* ── 策略机器人标签页内容 ── */
function StrategyBotTab() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { bots, isLoading, kpi, actionLoadingId, startBot, stopBot, deleteBot, cloneBot } = useBotData('strategy')

  const [viewMode, setViewMode] = useState<'list' | 'detail' | 'create'>('list')
  const [selectedBot, setSelectedBot] = useState<BotItem | null>(null)
  const [editingBot, setEditingBot] = useState<BotItem | null>(null)

  const runningBots = bots.filter(b => b.status === 'running')
  const runningPnl = runningBots.reduce((s, b) => s + (b.unrealized_pnl || 0), 0)

  const handleViewDetail = useCallback((bot: BotItem) => {
    setSelectedBot(bot)
    setViewMode('detail')
  }, [])

  const handleEditBot = useCallback((bot: BotItem) => {
    if (bot.status === 'running') {
      toast('info', '请先停止再编辑')
      return
    }
    setEditingBot(bot)
    setViewMode('create')
  }, [])

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
    setEditingBot(null)
  }, [])

  const createMutation = useMutation({
    mutationFn: (config: MartinConfig | WallStreetConfig) => {
      if (config.strategy_type === 'martin') {
        return strategyConfigApi.createMartin(config)
      }
      return strategyConfigApi.createWallStreet(config)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
      toast('success', '策略创建成功')
      setViewMode('list')
      setEditingBot(null)
    },
    onError: () => {
      toast('error', '策略创建失败')
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, config }: { id: string; config: MartinConfig | WallStreetConfig }) => {
      if (config.strategy_type === 'martin') {
        return strategyConfigApi.updateMartin(id, config)
      }
      return strategyConfigApi.updateWallStreet(id, config)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
      toast('success', '策略更新成功')
      setViewMode('list')
      setEditingBot(null)
    },
    onError: () => {
      toast('error', '策略更新失败')
    },
  })

  const handleConfigSubmit = (config: MartinConfig | WallStreetConfig) => {
    if (editingBot?.id && editingBot.id !== 'new') {
      updateMutation.mutate({ id: editingBot.id, config })
    } else {
      createMutation.mutate(config)
    }
  }

  const getEditInitialData = (): Partial<MartinConfig | WallStreetConfig> | undefined => {
    if (!editingBot) return undefined
    const tc = editingBot.trading_config
    return {
      name: editingBot.name || editingBot.strategy_name || '',
      strategy_type: (editingBot.bot_type === 'wallstreet' ? 'wallstreet' : 'martin') as 'martin' | 'wallstreet',
      symbol: editingBot.symbol || tc?.symbol || 'BTCUSDT',
      leverage: editingBot.leverage || tc?.leverage || 10,
      direction: (tc?.trade_direction as 'long' | 'short' | 'dual') || 'long',
      first_order_amount: tc?.first_order_amount || 100,
      order_count: tc?.order_count || 7,
      add_position_spread: tc?.add_position_spread || 3.5,
      add_position_callback: tc?.add_position_callback || 0.1,
      take_profit_ratio: tc?.take_profit_ratio || 1.3,
      profit_callback: tc?.profit_callback || 0.1,
      double_first_order: tc?.open_double || false,
      loop_type: tc?.trade_count_mode || 'cycle',
      loop_count: 999,
      enable_add_position: tc?.close_add_position !== true,
      flash_crash_protection: tc?.waterfall_protection || 2,
    }
  }

  // Detail view
  if (viewMode === 'detail' && selectedBot) {
    return (
      <BotDetailView
        bot={selectedBot}
        onBack={handleBack}
        onStart={startBot}
        onStop={stopBot}
        onEdit={handleEditBot}
        onDelete={handleDeleteWithCleanup}
        onClone={cloneBot}
      />
    )
  }

  // Create / Edit view
  if (viewMode === 'create') {
    return (
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={handleBack}>
            ← 返回列表
          </Button>
        </div>
        <PageHeader
          title={editingBot ? `编辑: ${editingBot.name || editingBot.strategy_name}` : '新建策略'}
          subtitle={editingBot ? '修改策略参数' : '配置马丁或华尔街策略参数'}
          icon={<Settings2 className="w-5 h-5" />}
        />
        <StrategyConfigPanel
          initialData={getEditInitialData()}
          onSubmit={handleConfigSubmit}
          onCancel={handleBack}
          isLoading={createMutation.isPending || updateMutation.isPending}
        />
      </div>
    )
  }

  // List view
  return (
    <div className="space-y-5">
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
        <div className="rounded-xl border border-[#1c1c1c] bg-[#111]/50 px-4 py-3">
          <div className="flex items-center justify-between text-xs">
            <span className="text-[#888]">
              运行中策略 <strong className="text-[#e0e0e0]">{runningBots.length}</strong> 个
            </span>
            <span className={runningPnl >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]'}>
              实时盈亏 {runningPnl >= 0 ? '+' : ''}${formatCurrency(runningPnl)}
            </span>
          </div>
          <div className="mt-2 flex flex-wrap gap-1.5">
            {runningBots.slice(0, 10).map(b => (
              <span key={b.id} className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-[#52c41a]/10 text-[10px] text-[#52c41a] border border-[#52c41a]/20">
                <span className="w-1.5 h-1.5 rounded-full bg-[#52c41a] animate-pulse" />
                {b.name || b.strategy_name}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* Create button */}
      <div className="flex items-center justify-between">
        <SectionCard
          title="策略列表"
          headerAction={
            <div className="flex items-center gap-2">
              <span className="text-xs text-[#8a8a8a]">共 {bots.length} 个</span>
              <Button
                variant="primary"
                size="sm"
                leftIcon={<Plus className="w-3 h-3" />}
                onClick={() => {
                  setEditingBot(null)
                  setViewMode('create')
                }}
              >
                新建策略
              </Button>
            </div>
          }
          className="w-full"
        >
          {isLoading ? (
            <div className="space-y-3">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-32 rounded-xl" />
              ))}
            </div>
          ) : bots.length === 0 ? (
            <div className="text-center py-10">
              <BarChart3 className="w-10 h-10 mx-auto mb-3 text-[#333]" />
              <p className="text-sm text-[#888] font-medium mb-1">暂无策略机器人</p>
              <p className="text-xs text-[#555] mb-4">创建你的第一个策略机器人吧</p>
              <Button
                variant="primary"
                size="sm"
                leftIcon={<Plus className="w-3 h-3" />}
                onClick={() => {
                  setEditingBot(null)
                  setViewMode('create')
                }}
              >
                新建策略
              </Button>
            </div>
          ) : (
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
              {bots.map((bot) => (
                <StrategyCard
                  key={bot.id}
                  bot={bot}
                  actionLoadingId={actionLoadingId}
                  onStart={startBot}
                  onStop={stopBot}
                  onEdit={handleEditBot}
                  onDelete={handleDeleteWithCleanup}
                  onClone={cloneBot}
                  onViewDetail={handleViewDetail}
                />
              ))}
            </div>
          )}
        </SectionCard>
      </div>

      {/* Advanced script entry */}
      <div className="flex items-center justify-center gap-1.5 text-xs text-[#757575]">
        <Terminal className="h-3 w-3" />
        <span>创建或编辑策略请前往</span>
        <Link to="/strategy" className="text-[#4f6ed1] transition-colors hover:text-[#8898f3]">
          策略管理
          <ChevronRight className="inline h-3 w-3" />
        </Link>
      </div>
    </div>
  )
}

/* ── 信号机器人标签页内容 ── */
function SignalBotTab() {
  return <SignalExecutorPanel />
}

/* ── AI机器人标签页内容 ── */
function AIBotTab() {
  return <AIRobotPanel />
}

/* ── 主页面 ── */
export function Bots() {
  const location = useLocation()
  const searchParams = new URLSearchParams(location.search)
  const botType = (searchParams.get('type') || 'strategy') as BotType

  const meta = BOT_TYPE_META[botType]

  const handleTabChange = (type: BotType) => {
    const url = new URL(window.location.href)
    url.searchParams.set('type', type)
    window.history.replaceState({}, '', url.toString())
  }

  const renderTabContent = () => {
    switch (botType) {
      case 'signal':
        return <SignalBotTab />
      case 'ai':
        return <AIBotTab />
      case 'strategy':
      default:
        return <StrategyBotTab />
    }
  }

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="space-y-5">
        {/* Page Header */}
        <PageHeader
          title={meta.title}
          subtitle={meta.subtitle}
          icon={meta.icon}
        />

        {/* Tab Navigation */}
        <div className="flex gap-1 p-1 rounded-xl bg-[#111] border border-[#1c1c1c]">
          {TAB_LIST.map((tab) => (
            <button
              key={tab.key}
              onClick={() => handleTabChange(tab.key)}
              className={cn(
                'flex-1 flex items-center justify-center gap-2 py-2.5 rounded-lg text-sm font-medium transition-all',
                botType === tab.key
                  ? 'bg-[#2a2a2a] text-[#e0e0e0] shadow-sm'
                  : 'text-[#888] hover:text-[#ccc] hover:bg-[#1a1a1a]'
              )}
            >
              {tab.icon}
              {tab.label}
            </button>
          ))}
        </div>

        {/* Tab Content */}
        {renderTabContent()}
      </div>
    </div>
  )
}
