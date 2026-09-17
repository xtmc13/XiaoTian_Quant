import { useMemo, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  LayoutGrid,
  Plus,
  Play,
  Square,
  Pencil,
  Trash2,
  Wallet,
  Activity,
  PauseCircle,
  TrendingUp,
  TrendingDown,
  Grid3x3,
  Layers,
  BarChart3,
  BrainCircuit,
  Bot,
  Settings2,
} from 'lucide-react'
import { cn, formatCurrency } from '@/lib/utils'
import { gridApi, strategyApi, strategyConfigApi } from '@/lib/api'
import type { GridBot, GridBotPayload } from '@/lib/api'
import type { StrategyItem } from '@/types'
import type { BotItem } from '@/hooks/useBotData'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import { SectionCard } from '@/components/ui/SectionCard'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { toast } from '@/lib/useToast'
import { StrategyConfigPanel } from '@/components/bots/StrategyConfigPanel'
import { BotCreateModal } from '@/components/bots/BotCreateModal'
import type { MartinConfig, WallStreetConfig } from '@/types'

/* ── 统一机器人类型 ── */
type BotKind = 'grid' | 'martin' | 'wallstreet' | 'add' | 'ai' | 'other'

interface UnifiedBot {
  id: string
  kind: BotKind
  name: string
  symbol: string
  status: string
  running: boolean
  /** 权益（有什么显示什么） */
  equity?: number
  /** 盈亏（有什么显示什么） */
  pnl?: number
  grid?: GridBot
  strategy?: StrategyItem
  botItem?: BotItem
}

const KIND_META: Record<BotKind, { label: string; variant: 'info' | 'success' | 'warning' | 'error' | 'neutral' }> = {
  grid: { label: '网格', variant: 'info' },
  martin: { label: '马丁', variant: 'error' },
  wallstreet: { label: '华尔街', variant: 'warning' },
  add: { label: '补仓', variant: 'success' },
  ai: { label: 'AI', variant: 'neutral' },
  other: { label: '机器人', variant: 'neutral' },
}

/** 从 kind=bot 列表项推断统一类型：bot_type 优先，其次 category/strategy_type。 */
function inferKind(s: StrategyItem): BotKind {
  const raw = (
    (s as StrategyItem & { bot_type?: string }).bot_type ||
    ((s.trading_config as Record<string, unknown> | undefined)?.bot_type as string) ||
    ''
  ).toLowerCase()
  const cat = (s.category || '').toLowerCase()
  const stype = (s.strategy_type || '').toLowerCase()
  if (raw === 'grid' || stype.includes('grid')) return 'grid'
  if (raw === 'martin_trend' || raw === 'martin' || cat === 'martin' || stype === 'martin') return 'martin'
  if (raw === 'wallstreet' || cat === 'wallstreet' || stype === 'wallstreet') return 'wallstreet'
  if (raw === 'ai' || stype.startsWith('ai_')) return 'ai'
  if (raw === 'dca' || raw === 'cra_contract' || raw === 'cra_spot' || stype.startsWith('cra')) return 'add'
  return 'other'
}

function isRunningStatus(status?: string, isRunning?: boolean): boolean {
  return status === 'running' || isRunning === true
}

/* ── 网格创建表单（沿用 BotsGrid 的 GridBotForm 逻辑） ── */
function GridCreateForm({ onDone }: { onDone: () => void }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [lowerPrice, setLowerPrice] = useState('')
  const [upperPrice, setUpperPrice] = useState('')
  const [gridCount, setGridCount] = useState('20')
  const [investment, setInvestment] = useState('')
  const [feeRate, setFeeRate] = useState('0.001')

  const createMutation = useMutation({
    mutationFn: (payload: GridBotPayload) => gridApi.create(payload),
    onSuccess: async (bot) => {
      await queryClient.invalidateQueries({ queryKey: ['grid-bots'] })
      toast('success', '网格机器人创建成功')
      onDone()
      setName('')
      setLowerPrice('')
      setUpperPrice('')
      setInvestment('')
      void bot
    },
    onError: (e) => toast('error', e instanceof Error ? e.message : '创建失败'),
  })

  const inputCls =
    'w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold'

  const handleSubmit = () => {
    const lower = parseFloat(lowerPrice)
    const upper = parseFloat(upperPrice)
    const grids = parseInt(gridCount, 10)
    const amount = parseFloat(investment)
    const fee = parseFloat(feeRate)
    if (!name.trim()) return toast('warning', '请填写机器人名称')
    if (!symbol.trim()) return toast('warning', '请填写交易对')
    if (!isFinite(lower) || !isFinite(upper) || lower <= 0 || upper <= 0 || lower >= upper)
      return toast('warning', '区间下限必须小于上限且均为正数')
    if (!isFinite(grids) || grids < 2 || grids > 200) return toast('warning', '网格数必须在 2-200 之间')
    if (!isFinite(amount) || amount <= 0) return toast('warning', '投入金额必须大于 0')
    if (!isFinite(fee) || fee < 0) return toast('warning', '费率不能为负数')
    createMutation.mutate({
      name: name.trim(),
      symbol: symbol.trim().toUpperCase(),
      lower_price: lower,
      upper_price: upper,
      grid_count: grids,
      investment: amount,
      fee_rate: fee,
    })
  }

  return (
    <div className="space-y-3">
      <div>
        <label className="text-[11px] text-muted-foreground mb-1 block">名称</label>
        <input value={name} onChange={(e) => setName(e.target.value)} className={inputCls} placeholder="例如：BTC 震荡网格" />
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <label className="text-[11px] text-muted-foreground mb-1 block">交易对</label>
          <input
            value={symbol}
            onChange={(e) => setSymbol(e.target.value.toUpperCase())}
            className={inputCls}
            placeholder="BTCUSDT"
          />
        </div>
        <div>
          <label className="text-[11px] text-muted-foreground mb-1 block">网格数 (2-200)</label>
          <input type="number" value={gridCount} onChange={(e) => setGridCount(e.target.value)} className={inputCls} />
        </div>
        <div>
          <label className="text-[11px] text-muted-foreground mb-1 block">区间下限</label>
          <input type="number" value={lowerPrice} onChange={(e) => setLowerPrice(e.target.value)} className={inputCls} />
        </div>
        <div>
          <label className="text-[11px] text-muted-foreground mb-1 block">区间上限</label>
          <input type="number" value={upperPrice} onChange={(e) => setUpperPrice(e.target.value)} className={inputCls} />
        </div>
        <div>
          <label className="text-[11px] text-muted-foreground mb-1 block">投入金额 (USDT)</label>
          <input type="number" value={investment} onChange={(e) => setInvestment(e.target.value)} className={inputCls} />
        </div>
        <div>
          <label className="text-[11px] text-muted-foreground mb-1 block">费率（默认 0.001）</label>
          <input type="number" value={feeRate} onChange={(e) => setFeeRate(e.target.value)} className={inputCls} />
        </div>
      </div>
      <div className="flex items-center gap-2 pt-1">
        <Button variant="primary" className="flex-1" isLoading={createMutation.isPending} onClick={handleSubmit}>
          创建网格机器人
        </Button>
      </div>
    </div>
  )
}

/* ── 马丁/华尔街表单（复用 StrategyConfigPanel + strategyConfigApi） ── */
function MartinWallstreetForm({
  strategyType,
  onDone,
}: {
  strategyType: 'martin' | 'wallstreet'
  onDone: () => void
}) {
  const queryClient = useQueryClient()
  const createMutation = useMutation({
    mutationFn: (config: MartinConfig | WallStreetConfig) =>
      config.strategy_type === 'martin' ? strategyConfigApi.createMartin(config) : strategyConfigApi.createWallStreet(config),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['strategies'] })
      toast('success', strategyType === 'martin' ? '马丁机器人创建成功' : '华尔街机器人创建成功')
      onDone()
    },
    onError: (e) => toast('error', e instanceof Error ? e.message : '创建失败'),
  })

  return (
    <StrategyConfigPanel
      initialData={{ strategy_type: strategyType }}
      onSubmit={(config) => createMutation.mutate(config)}
      onCancel={onDone}
      isLoading={createMutation.isPending}
    />
  )
}

/* ── 新建机器人：模板选择 ── */
const TEMPLATES: {
  key: 'grid' | 'martin' | 'wallstreet' | 'add' | 'ai'
  title: string
  desc: string
  icon: React.ReactNode
}[] = [
  { key: 'grid', title: '网格交易', desc: '价格区间内自动低买高卖，适合震荡行情', icon: <Grid3x3 className="w-5 h-5" /> },
  { key: 'martin', title: '马丁格尔', desc: '倍投补仓摊薄成本，循环止盈', icon: <TrendingUp className="w-5 h-5" /> },
  { key: 'wallstreet', title: '华尔街', desc: '等比数量补仓 + 趋势指标过滤', icon: <BarChart3 className="w-5 h-5" /> },
  { key: 'add', title: '智能补仓', desc: '补仓 ladder + 移动止盈的自定义机器人向导', icon: <Layers className="w-5 h-5" /> },
  { key: 'ai', title: 'AI 自定义机器人', desc: '用自然语言描述策略，AI 生成参数', icon: <BrainCircuit className="w-5 h-5" /> },
]

/* ── 统一卡片 ── */
function UnifiedBotCard({
  bot,
  actionLoadingId,
  onStart,
  onStop,
  onDelete,
  onEdit,
}: {
  bot: UnifiedBot
  actionLoadingId: string | null
  onStart: (bot: UnifiedBot) => void
  onStop: (bot: UnifiedBot) => void
  onDelete: (bot: UnifiedBot) => void
  onEdit?: (bot: UnifiedBot) => void
}) {
  const meta = KIND_META[bot.kind]
  const isLoading = actionLoadingId === bot.id
  return (
    <div className="rounded-xl border border-[#1c1c1c] bg-[#111] transition-all hover:border-[#333]">
      <div className="p-4">
        <div className="flex items-start justify-between mb-3">
          <div className="flex items-center gap-2 min-w-0">
            <span
              className={cn(
                'w-2 h-2 rounded-full flex-shrink-0',
                bot.running ? 'bg-[#52c41a] animate-pulse' : 'bg-[#555]'
              )}
            />
            <span className="text-sm font-medium text-[#e0e0e0] truncate">{bot.name || '未命名'}</span>
          </div>
          <div className="flex items-center gap-1.5 flex-shrink-0">
            <Badge variant={meta.variant} className="text-[10px]">
              {meta.label}
            </Badge>
            <Badge variant={bot.running ? 'success' : 'neutral'} dot>
              {bot.running ? '运行中' : '已停止'}
            </Badge>
          </div>
        </div>

        <div className="flex items-center justify-between mb-3">
          <Badge variant="info" className="text-[10px]">
            {bot.symbol || '-'}
          </Badge>
          {bot.pnl != null && (
            <span className={cn('text-sm font-semibold', bot.pnl >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]')}>
              {bot.pnl >= 0 ? '+' : ''}
              {formatCurrency(bot.pnl)}
            </span>
          )}
        </div>

        {bot.equity != null && (
          <div className="rounded-lg bg-[#0a0a0a] border border-[#1c1c1c] p-2.5 mb-3">
            <div className="text-[10px] text-[#888]">
              权益: <span className="text-[#ccc]">${formatCurrency(bot.equity)}</span>
            </div>
          </div>
        )}

        <div className="flex items-center gap-1.5">
          {bot.running ? (
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
          {onEdit && (bot.kind === 'martin' || bot.kind === 'wallstreet') && (
            <Button variant="ghost" size="sm" onClick={() => onEdit(bot)} leftIcon={<Pencil className="w-3 h-3" />}>
              编辑
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onDelete(bot)}
            leftIcon={<Trash2 className="w-3 h-3 text-[#f5222d]" />}
            className="text-[#f5222d] hover:bg-[#f5222d]/10"
          >
            删除
          </Button>
        </div>
      </div>
    </div>
  )
}

/* ── 编辑马丁/华尔街（StrategyConfigPanel + update api） ── */
function EditStrategyBotModal({ bot, onDone }: { bot: UnifiedBot; onDone: () => void }) {
  const queryClient = useQueryClient()
  const strategyType = bot.kind === 'wallstreet' ? 'wallstreet' : 'martin'
  const tc = (bot.botItem?.trading_config || {}) as Record<string, unknown>

  const updateMutation = useMutation({
    mutationFn: (config: MartinConfig | WallStreetConfig) =>
      config.strategy_type === 'martin'
        ? strategyConfigApi.updateMartin(bot.id, config)
        : strategyConfigApi.updateWallStreet(bot.id, config),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['strategies'] })
      toast('success', '机器人已更新')
      onDone()
    },
    onError: (e) => toast('error', e instanceof Error ? e.message : '更新失败'),
  })

  return (
    <StrategyConfigPanel
      initialData={{
        strategy_type: strategyType,
        name: bot.name,
        symbol: bot.symbol || (tc.symbol as string) || 'BTCUSDT',
        leverage: (tc.leverage as number) || 10,
        first_order_amount: (tc.first_order_amount as number) || 100,
        order_count: (tc.order_count as number) || 7,
        add_position_spread: (tc.add_position_spread as number) || 3.5,
        add_position_callback: (tc.add_position_callback as number) || 0.1,
        take_profit_ratio: (tc.take_profit_ratio as number) || 1.3,
        profit_callback: (tc.profit_callback as number) || 0.1,
      }}
      onSubmit={(config) => updateMutation.mutate(config)}
      onCancel={onDone}
      isLoading={updateMutation.isPending}
    />
  )
}

/* ════════════ 机器人中心 ════════════ */
type TypeFilter = 'all' | BotKind

export function BotsCenter() {
  const location = useLocation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [typeFilter, setTypeFilter] = useState<TypeFilter>(
    (location.state as { type?: TypeFilter } | null)?.type ?? 'all'
  )
  const [actionLoadingId, setActionLoadingId] = useState<string | null>(null)
  // 新建向导：'menu'=模板选择，其余为对应模板表单
  const [wizard, setWizard] = useState<'menu' | 'grid' | 'martin' | 'wallstreet' | 'add' | 'ai' | null>(null)
  const [editingBot, setEditingBot] = useState<UnifiedBot | null>(null)

  /* 数据两路合并：网格 + kind=bot 策略机器人 */
  const { data: gridBots = [], isLoading: gridLoading } = useQuery({
    queryKey: ['grid-bots'],
    queryFn: gridApi.list,
    refetchInterval: 8000,
  })
  const { data: strategyBots = [], isLoading: strategyLoading } = useQuery({
    queryKey: ['strategies', 'bot'],
    queryFn: () => strategyApi.list({ kind: 'bot' }),
    refetchInterval: 5000,
  })

  const unified: UnifiedBot[] = useMemo(() => {
    const fromGrid: UnifiedBot[] = gridBots.map((g: GridBot) => ({
      id: String(g.id),
      kind: 'grid',
      name: g.name,
      symbol: g.symbol,
      status: g.status,
      running: isRunningStatus(g.status, g.is_running),
      equity: g.initial_equity > 0 ? g.initial_equity + g.realized_pnl : g.investment,
      pnl: g.realized_pnl,
      grid: g,
    }))
    const fromStrategy: UnifiedBot[] = strategyBots.map((s: StrategyItem) => {
      const bi: BotItem = s as unknown as BotItem
      const tc = (s.trading_config || {}) as Record<string, unknown>
      return {
        id: String(s.id),
        kind: inferKind(s),
        name: s.strategy_name || s.name,
        symbol: s.symbol || (tc.symbol as string) || '',
        status: s.status,
        running: isRunningStatus(s.status),
        equity: (s.initial_capital as number) || (tc.initial_capital as number) || undefined,
        pnl: (s.total_pnl as number) ?? (bi.unrealized_pnl as number) ?? undefined,
        strategy: s,
        botItem: bi,
      }
    })
    return [...fromGrid, ...fromStrategy]
  }, [gridBots, strategyBots])

  const filtered = useMemo(
    () => (typeFilter === 'all' ? unified : unified.filter((b) => b.kind === typeFilter)),
    [unified, typeFilter]
  )

  const running = unified.filter((b) => b.running).length
  const totalEquity = unified.reduce((sum, b) => sum + (b.equity || 0), 0)
  const totalPnl = unified.reduce((sum, b) => sum + (b.pnl || 0), 0)

  const countOf = (k: TypeFilter) => (k === 'all' ? unified.length : unified.filter((b) => b.kind === k).length)

  const runAction = async (bot: UnifiedBot, action: () => Promise<unknown>, okMsg: string, errMsg: string) => {
    setActionLoadingId(bot.id)
    try {
      await action()
      toast('success', okMsg)
      if (bot.kind === 'grid') await queryClient.invalidateQueries({ queryKey: ['grid-bots'] })
      else await queryClient.invalidateQueries({ queryKey: ['strategies'] })
    } catch (e) {
      toast('error', `${errMsg}: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setActionLoadingId(null)
    }
  }

  const handleStart = (bot: UnifiedBot) =>
    runAction(
      bot,
      () => (bot.kind === 'grid' ? gridApi.start(bot.id) : strategyApi.start(bot.id)),
      '已启动',
      '启动失败'
    )
  const handleStop = (bot: UnifiedBot) =>
    runAction(
      bot,
      () => (bot.kind === 'grid' ? gridApi.stop(bot.id) : strategyApi.stop(bot.id)),
      '已停止',
      '停止失败'
    )
  const handleDelete = async (bot: UnifiedBot) => {
    if (!confirm(`确定删除机器人 "${bot.name}"？`)) return
    await runAction(
      bot,
      () => (bot.kind === 'grid' ? gridApi.remove(bot.id) : strategyApi.delete(bot.id)),
      '已删除',
      '删除失败'
    )
    if (editingBot?.id === bot.id) setEditingBot(null)
  }

  const isLoading = gridLoading || strategyLoading

  const FILTER_OPTIONS: { key: TypeFilter; label: string }[] = [
    { key: 'all', label: '全部' },
    { key: 'grid', label: '网格' },
    { key: 'martin', label: '马丁' },
    { key: 'wallstreet', label: '华尔街' },
    { key: 'add', label: '补仓' },
    { key: 'ai', label: 'AI' },
  ]

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader
          title="机器人中心"
          subtitle="网格、马丁、华尔街、智能补仓与 AI 机器人统一管理（模拟盘）"
          icon={<LayoutGrid className="w-5 h-5" />}
          actions={
            <Button variant="primary" size="sm" leftIcon={<Plus className="w-3 h-3" />} onClick={() => setWizard('menu')}>
              新建机器人
            </Button>
          }
        />

        {/* KPI 行 */}
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <KPICard icon={<Activity className="h-4 w-4 text-[#52c41a]" />} label="运行中" value={String(running)} subValue={`共 ${unified.length} 个`} />
          <KPICard icon={<PauseCircle className="h-4 w-4 text-[#faad14]" />} label="已停止" value={String(unified.length - running)} />
          <KPICard icon={<Wallet className="h-4 w-4 text-[#1890ff]" />} label="总权益" value={`$${formatCurrency(totalEquity)}`} />
          <KPICard
            icon={totalPnl >= 0 ? <TrendingUp className="h-4 w-4 text-[#52c41a]" /> : <TrendingDown className="h-4 w-4 text-[#f5222d]" />}
            label="累计盈亏"
            value={`${totalPnl >= 0 ? '+' : ''}${formatCurrency(totalPnl)}`}
            trend={totalPnl >= 0 ? 'up' : 'down'}
            variant={totalPnl >= 0 ? 'success' : 'error'}
            primary
          />
        </div>

        {/* 类型筛选 chips */}
        <div className="flex flex-wrap items-center gap-2">
          {FILTER_OPTIONS.map((f) => (
            <button
              key={f.key}
              onClick={() => setTypeFilter(f.key)}
              className={cn(
                'px-3 py-1.5 rounded-full text-xs font-medium border transition-colors',
                typeFilter === f.key
                  ? 'bg-quant-gold/10 text-quant-gold border-quant-gold/30'
                  : 'border-quant-border text-muted-foreground hover:text-foreground hover:border-quant-gold/20'
              )}
            >
              {f.label}
              <span className="ml-1.5 text-[10px] opacity-70">{countOf(f.key)}</span>
            </button>
          ))}
          <button
            onClick={() => navigate('/bots/ai')}
            className="ml-auto px-3 py-1.5 rounded-full text-xs text-muted-foreground border border-quant-border hover:text-foreground transition-colors"
          >
            AI 机器人市场 →
          </button>
        </div>

        {/* 卡片网格 */}
        <SectionCard title="机器人列表" headerAction={<span className="text-xs text-[#8a8a8a]">{filtered.length} 个</span>}>
          {isLoading ? (
            <div className="grid grid-cols-1 xl:grid-cols-2 gap-3">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-36 rounded-xl" />
              ))}
            </div>
          ) : filtered.length === 0 ? (
            <EmptyState
              icon={<Bot className="w-8 h-8" />}
              title={typeFilter === 'all' ? '暂无机器人' : `暂无${FILTER_OPTIONS.find((f) => f.key === typeFilter)?.label}类机器人`}
              description="点击右上角「新建机器人」选择模板创建"
              actionLabel="新建机器人"
              onAction={() => setWizard('menu')}
            />
          ) : (
            <div className="grid grid-cols-1 xl:grid-cols-2 gap-3">
              {filtered.map((bot) => (
                <UnifiedBotCard
                  key={`${bot.kind}-${bot.id}`}
                  bot={bot}
                  actionLoadingId={actionLoadingId}
                  onStart={handleStart}
                  onStop={handleStop}
                  onDelete={handleDelete}
                  onEdit={(b) => {
                    if (b.running) {
                      toast('info', '请先停止机器人再编辑')
                      return
                    }
                    setEditingBot(b)
                  }}
                />
              ))}
            </div>
          )}
        </SectionCard>
      </div>

      {/* 新建向导：第一步模板选择 */}
      {wizard === 'menu' && (
        <ModalShell title="新建机器人" subtitle="选择机器人类型模板" onClose={() => setWizard(null)}>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            {TEMPLATES.map((t) => (
              <button
                key={t.key}
                onClick={() => setWizard(t.key)}
                className="flex items-start gap-3 p-4 rounded-xl border border-quant-border text-left transition-all hover:border-quant-gold/30 hover:bg-quant-gold/5"
              >
                <div className="w-10 h-10 rounded-lg bg-quant-gold/10 text-quant-gold flex items-center justify-center shrink-0">
                  {t.icon}
                </div>
                <div className="min-w-0">
                  <div className="text-xs font-semibold text-foreground">{t.title}</div>
                  <div className="text-[10px] text-muted-foreground leading-relaxed mt-0.5">{t.desc}</div>
                </div>
              </button>
            ))}
          </div>
        </ModalShell>
      )}
      {/* 第二步：对应表单 */}
      {wizard === 'grid' && (
        <ModalShell title="新建网格机器人" subtitle="在价格区间内自动低买高卖" onClose={() => setWizard(null)}>
          <GridCreateForm onDone={() => setWizard(null)} />
        </ModalShell>
      )}
      {wizard === 'martin' && (
        <ModalShell title="新建马丁机器人" subtitle="倍投补仓类策略（模拟盘）" wide onClose={() => setWizard(null)}>
          <MartinWallstreetForm strategyType="martin" onDone={() => setWizard(null)} />
        </ModalShell>
      )}
      {wizard === 'wallstreet' && (
        <ModalShell title="新建华尔街机器人" subtitle="等比补仓类策略（模拟盘）" wide onClose={() => setWizard(null)}>
          <MartinWallstreetForm strategyType="wallstreet" onDone={() => setWizard(null)} />
        </ModalShell>
      )}
      {wizard === 'add' && (
        <BotCreateModal
          open
          botType="dca"
          aiPreset={null}
          editBot={null}
          onCancel={() => setWizard(null)}
          onCreated={() => {
            setWizard(null)
            void queryClient.invalidateQueries({ queryKey: ['strategies'] })
          }}
          onUpdated={() => setWizard(null)}
        />
      )}
      {wizard === 'ai' && (
        <BotCreateModal
          open
          botType="custom"
          aiPreset={null}
          editBot={null}
          onCancel={() => setWizard(null)}
          onCreated={() => {
            setWizard(null)
            void queryClient.invalidateQueries({ queryKey: ['strategies'] })
          }}
          onUpdated={() => setWizard(null)}
        />
      )}

      {/* 编辑马丁/华尔街 */}
      {editingBot && (
        <ModalShell
          title={`编辑: ${editingBot.name}`}
          subtitle="修改策略参数"
          wide
          onClose={() => setEditingBot(null)}
        >
          <EditStrategyBotModal bot={editingBot} onDone={() => setEditingBot(null)} />
        </ModalShell>
      )}
    </div>
  )
}

/* ── 通用弹层壳（对齐项目弹窗惯例） ── */
function ModalShell({
  title,
  subtitle,
  wide,
  onClose,
  children,
}: {
  title: string
  subtitle?: string
  wide?: boolean
  onClose: () => void
  children: React.ReactNode
}) {
  return (
    <div
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
      onClick={onClose}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onClose()
      }}
      tabIndex={-1}
    >
      <div
        role="document"
        className={cn(
          'w-full flex flex-col rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden',
          wide ? 'max-w-3xl max-h-[90vh]' : 'max-w-xl max-h-[85vh]'
        )}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
          <div className="flex items-center gap-2">
            <Settings2 className="w-4 h-4 text-quant-gold" />
            <div>
              <h3 className="text-sm font-bold">{title}</h3>
              {subtitle && <p className="text-[10px] text-muted-foreground mt-0.5">{subtitle}</p>}
            </div>
          </div>
          <button
            onClick={onClose}
            aria-label="关闭"
            className="w-8 h-8 rounded-lg border border-quant-border flex items-center justify-center text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors"
          >
            ✕
          </button>
        </div>
        <div className="flex-1 overflow-y-auto p-6">{children}</div>
      </div>
    </div>
  )
}

export default BotsCenter
