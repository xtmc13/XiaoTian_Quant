import { useMemo, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  LayoutGrid,
  Plus,
  Search,
  TrendingUp,
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
import { RuntimePanel } from '@/components/strategy/RuntimePanel'
import type { BotItem } from '@/hooks/useBotData'
import { SectionCard } from '@/components/ui/SectionCard'
import { Button } from '@/components/ui/Button'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { toast } from '@/lib/useToast'
import { StrategyConfigPanel } from '@/components/bots/StrategyConfigPanel'
import { BotCreateModal } from '@/components/bots/BotCreateModal'
import type { MartinConfig, WallStreetConfig } from '@/types'

/* ── 统一机器人类型 ── */
type BotKind = 'grid' | 'martin' | 'wallstreet' | 'add' | 'ai' | 'spot-strategy' | 'contract-strategy' | 'other'

interface UnifiedBot {
  id: string
  /** 数据来源：grid=网格 / bot=策略机器人 / strategy=策略实例（kind=strategy）。 */
  source: 'grid' | 'bot' | 'strategy'
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

// 类型 pill 配色对齐效果图：网格紫 / 补仓蓝 / 马丁绿 / 华尔街青 / AI 青。
const KIND_META: Record<BotKind, { label: string; pill: string }> = {
  grid: { label: '网格', pill: 'bg-purple-500/15 text-purple-400' },
  martin: { label: '马丁', pill: 'bg-emerald-500/15 text-emerald-400' },
  wallstreet: { label: '华尔街', pill: 'bg-teal-500/15 text-teal-400' },
  add: { label: '补仓', pill: 'bg-blue-500/15 text-blue-400' },
  ai: { label: 'AI', pill: 'bg-cyan-500/15 text-cyan-400' },
  'spot-strategy': { label: '现货策略', pill: 'bg-sky-500/15 text-sky-400' },
  'contract-strategy': { label: '合约策略', pill: 'bg-orange-500/15 text-orange-400' },
  other: { label: '机器人', pill: 'bg-quant-gold/15 text-quant-gold' },
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

function canDetailKind(kind: BotKind): boolean {
  return kind === 'martin' || kind === 'wallstreet'
}

/** kind=strategy 来源（策略实例）的类型映射：现货/合约。 */
function inferStrategyKind(s: StrategyItem): BotKind {
  const stype = (s.strategy_type || '').toLowerCase()
  if (stype === 'cra_spot' || s.market_type === 'spot') return 'spot-strategy'
  return 'contract-strategy'
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

/* ── 新建机器人：模板选择（与右上角「新建机器人」一致） ── */
const TEMPLATES: {
  key: 'spot' | 'contract' | 'ai' | 'custom'
  title: string
  desc: string
  icon: React.ReactNode
}[] = [
  { key: 'spot', title: '现货策略机器人', desc: '现货网格/补仓策略，开仓指标可选（MACD/顺势多等）', icon: <TrendingUp className="w-5 h-5" /> },
  { key: 'contract', title: '合约策略机器人', desc: '支持杠杆/逐全仓、开仓指标选择器与补仓壳', icon: <BarChart3 className="w-5 h-5" /> },
  { key: 'ai', title: 'AI 机器人', desc: 'AI 生成的策略机器人实例库', icon: <BrainCircuit className="w-5 h-5" /> },
  { key: 'custom', title: 'AI 自定义机器人', desc: '用自然语言描述策略，AI 生成参数', icon: <Bot className="w-5 h-5" /> },
]

/* ── 统一卡片（紧凑四行小卡，对齐效果图屏幕1） ── */
function UnifiedBotCard({
  bot,
  actionLoadingId,
  onStart,
  onStop,
  onDelete,
  onEdit,
  onDetail,
}: {
  bot: UnifiedBot
  actionLoadingId: string | null
  onStart: (bot: UnifiedBot) => void
  onStop: (bot: UnifiedBot) => void
  onDelete: (bot: UnifiedBot) => void
  onEdit?: (bot: UnifiedBot) => void
  /** 策略实例「详情」：RuntimePanel 弹层。 */
  onDetail?: (bot: UnifiedBot) => void
}) {
  const meta = KIND_META[bot.kind]
  const isLoading = actionLoadingId === bot.id
  const isLive = bot.strategy?.execution_mode === 'live' || bot.strategy?.mode === 'live'
  // 策略实例：详情（RuntimePanel）始终可用；马丁/华尔街详情=编辑（既有行为）。
  const showDetail = bot.source === 'strategy' || canDetailKind(bot.kind)
  // 行2：类型 pill + 标的 · 周期 · 杠杆（网格显示格数）。
  const timeframe = bot.strategy?.timeframe
  const leverage = bot.strategy?.leverage
  const metaLine = [
    bot.symbol || '-',
    bot.kind === 'grid' && bot.grid ? `${bot.grid.grid_count}格` : (timeframe || null),
    leverage && leverage > 1 ? `${leverage}x` : null,
  ]
    .filter(Boolean)
    .join(' · ')
  const canDetail = !!onEdit && canDetailKind(bot.kind)

  const btnCls =
    'flex-1 py-1 rounded bg-quant-bg border border-quant-border text-[11px] text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors disabled:opacity-50'

  return (
    <div
      className={cn(
        'bg-quant-card border border-quant-border rounded-xl p-3 transition-all hover:border-quant-gold/20',
        !bot.running && 'opacity-80'
      )}
    >
      {/* 行1：名称 + 实盘/模拟盘徽标 */}
      <div className="flex items-center justify-between gap-2">
        <span className="font-bold text-xs text-foreground truncate">{bot.name || '未命名'}</span>
        {isLive ? (
          <span className="px-1.5 py-0.5 rounded bg-quant-red/15 text-quant-red border border-quant-red/40 text-[10px] font-semibold shrink-0">
            实盘
          </span>
        ) : (
          <span className="px-1.5 py-0.5 rounded bg-quant-gold/15 text-quant-gold border border-quant-gold/40 text-[10px] shrink-0">
            模拟盘
          </span>
        )}
      </div>

      {/* 行2：类型 pill + 标的 · 周期 · 杠杆 */}
      <div className="text-muted-foreground mt-1 flex items-center gap-1.5 text-[10px] min-w-0">
        <span className={cn('px-1 py-0.5 rounded shrink-0', meta.pill)}>{meta.label}</span>
        <span className="truncate">{metaLine}</span>
      </div>

      {/* 行3：盈亏大字 + 状态 */}
      <div className="flex items-center justify-between mt-2">
        <span className={cn('text-sm font-bold', bot.pnl == null ? 'text-muted-foreground' : bot.pnl >= 0 ? 'text-quant-green' : 'text-quant-red')}>
          {bot.pnl == null ? '--' : `${bot.pnl >= 0 ? '+' : ''}${formatCurrency(bot.pnl)}`}
        </span>
        <span className={cn('text-[10px]', bot.running ? 'text-quant-green' : 'text-muted-foreground')}>
          {bot.running ? '● 运行中' : '○ 已停止'}
        </span>
      </div>

      {/* 行4：操作按钮组（运行中=停止/详情，已停止=启动/删除） */}
      <div className="flex gap-1 mt-2">
        {bot.running ? (
          <>
            <button className={btnCls} disabled={isLoading} onClick={() => onStop(bot)}>
              停止
            </button>
            {showDetail ? (
              <button
                className={btnCls}
                onClick={() => (bot.source === 'strategy' ? onDetail?.(bot) : onEdit?.(bot))}
              >
                详情
              </button>
            ) : (
              <button className={btnCls} onClick={() => onDelete(bot)}>
                删除
              </button>
            )}
            {bot.source === 'strategy' && (
              <button className={btnCls} onClick={() => onDelete(bot)}>
                删除
              </button>
            )}
          </>
        ) : (
          <>
            <button className={btnCls} disabled={isLoading} onClick={() => onStart(bot)}>
              启动
            </button>
            {bot.source === 'strategy' && (
              <button className={btnCls} onClick={() => onDetail?.(bot)}>
                详情
              </button>
            )}
            <button className={btnCls} onClick={() => onDelete(bot)}>
              删除
            </button>
          </>
        )}
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
  // 内联统计 pill 筛选/排序（可点击）。机器人中心 = 唯一管理页：网格/马丁/华尔街/AI/现货/合约策略统一卡片。
  const [botStatusFilter, setBotStatusFilter] = useState<'all' | 'running' | 'stopped'>('all')
  const [botSort, setBotSort] = useState<{ key: 'equity' | 'pnl'; dir: 'desc' | 'asc' } | null>(null)
  const [actionLoadingId, setActionLoadingId] = useState<string | null>(null)
  // 工具条搜索（名称/标的）。
  const [search, setSearch] = useState('')
  // [+ 新建] 三卡选择层：现货/合约策略 → /create；机器人 → 模板向导。
  // 新建向导：'menu'=模板选择，其余为对应模板表单（智能补仓 CRA 归策略侧，不在此）
  const [wizard, setWizard] = useState<'menu' | 'spot' | 'contract' | 'ai' | 'custom' | null>(null)
  const [editingBot, setEditingBot] = useState<UnifiedBot | null>(null)
  // 策略实例「详情」：RuntimePanel 弹层（5s 轮询，复用策略管理页运行面板）。
  const [detailBot, setDetailBot] = useState<UnifiedBot | null>(null)

  /* 数据三源合并：网格 + kind=bot 机器人 + kind=strategy 策略实例 */
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
  // 策略实例（kind=strategy：现货/合约策略统一在此管理，创建出来即实例）。
  const { data: strategyItems = [], isLoading: strategyItemsLoading } = useQuery({
    queryKey: ['strategies', 'strategy'],
    queryFn: () => strategyApi.list({ kind: 'strategy' }),
    refetchInterval: 5000,
  })

  const unified: UnifiedBot[] = useMemo(() => {
    const fromGrid: UnifiedBot[] = gridBots.map((g: GridBot) => ({
      id: String(g.id),
      source: 'grid',
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
        source: 'bot',
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
    const fromStrategyItems: UnifiedBot[] = strategyItems.map((s: StrategyItem) => {
      const tc = (s.trading_config || {}) as Record<string, unknown>
      return {
        id: String(s.id),
        source: 'strategy',
        kind: inferStrategyKind(s),
        name: s.name,
        symbol: s.symbol || (tc.symbol as string) || '',
        status: s.status,
        running: isRunningStatus(s.status),
        equity: (s.initial_capital as number) || (tc.initial_capital as number) || undefined,
        pnl: (s.total_pnl as number) ?? undefined,
        strategy: s,
      }
    })
    return [...fromGrid, ...fromStrategy, ...fromStrategyItems]
  }, [gridBots, strategyBots, strategyItems])

  const filtered = useMemo(() => {
    let list = typeFilter === 'all' ? unified : unified.filter((b) => b.kind === typeFilter)
    const q = search.trim().toLowerCase()
    if (q) {
      list = list.filter((b) => b.name.toLowerCase().includes(q) || (b.symbol || '').toLowerCase().includes(q))
    }
    if (botStatusFilter !== 'all') {
      list = list.filter((b) => (botStatusFilter === 'running' ? b.running : !b.running))
    }
    if (botSort) {
      list = [...list].sort((a, b) => {
        const av = botSort.key === 'equity' ? a.equity || 0 : a.pnl || 0
        const bv = botSort.key === 'equity' ? b.equity || 0 : b.pnl || 0
        const diff = av - bv
        return botSort.dir === 'desc' ? -diff : diff
      })
    }
    return list
  }, [unified, typeFilter, botStatusFilter, botSort, search])

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

  const isLoading = gridLoading || strategyLoading || strategyItemsLoading

  const FILTER_OPTIONS: { key: TypeFilter; label: string }[] = [
    { key: 'all', label: '全部' },
    { key: 'grid', label: '网格' },
    { key: 'martin', label: '马丁' },
    { key: 'wallstreet', label: '华尔街' },
    { key: 'add', label: '补仓' },
    { key: 'ai', label: 'AI' },
    { key: 'spot-strategy', label: '现货策略' },
    { key: 'contract-strategy', label: '合约策略' },
  ]

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        {/* 工具条：标题 + 搜索 + 内联统计 pill（可点筛选/排序）+ 新建 */}
        <div className="flex items-center gap-3 bg-quant-card border border-quant-border rounded-xl px-4 py-2 flex-wrap">
          <span className="font-bold text-sm flex items-center gap-1.5">
            <LayoutGrid className="w-4 h-4 text-quant-gold" /> 机器人中心
          </span>
          <div className="relative flex-1 min-w-[140px] max-w-xs">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="搜索名称 / 标的"
              className="w-full bg-quant-bg border border-quant-border rounded-lg pl-8 pr-3 py-1.5 text-xs focus:outline-none focus:border-quant-gold"
            />
          </div>
          {/* 内联统计（可点，但不占整行） */}
          <div className="flex items-center gap-1 text-[11px] flex-wrap">
            <button
              onClick={() => setBotStatusFilter((prev) => (prev === 'running' ? 'all' : 'running'))}
              className={cn(
                'px-2 py-1 rounded transition-colors',
                botStatusFilter === 'running'
                  ? 'bg-quant-green/10 text-quant-green border border-quant-green/30 font-semibold'
                  : 'text-muted-foreground hover:text-foreground'
              )}
            >
              运行 {running}
            </button>
            <button
              onClick={() => setBotStatusFilter((prev) => (prev === 'stopped' ? 'all' : 'stopped'))}
              className={cn(
                'px-2 py-1 rounded transition-colors',
                botStatusFilter === 'stopped'
                  ? 'bg-quant-gold/10 text-quant-gold border border-quant-gold/30 font-semibold'
                  : 'text-muted-foreground hover:text-foreground'
              )}
            >
              停止 {unified.length - running}
            </button>
            <span className="text-muted-foreground">·</span>
            <button
              onClick={() =>
                setBotSort((prev) =>
                  prev == null || prev.key !== 'equity'
                    ? { key: 'equity', dir: 'desc' }
                    : prev.dir === 'desc'
                      ? { key: 'equity', dir: 'asc' }
                      : null
                )
              }
              className={cn(
                'px-2 py-1 rounded transition-colors',
                botSort?.key === 'equity' ? 'text-quant-gold font-semibold underline underline-offset-4' : 'text-quant-gold hover:opacity-80'
              )}
            >
              投入 ${formatCurrency(totalEquity)}
              {botSort?.key === 'equity' ? (botSort.dir === 'desc' ? ' ▼' : ' ▲') : ''}
            </button>
            <button
              onClick={() =>
                setBotSort((prev) =>
                  prev == null || prev.key !== 'pnl'
                    ? { key: 'pnl', dir: 'desc' }
                    : prev.dir === 'desc'
                      ? { key: 'pnl', dir: 'asc' }
                      : null
                )
              }
              className={cn(
                'px-2 py-1 rounded transition-colors',
                botSort?.key === 'pnl' ? 'font-semibold underline underline-offset-4' : 'hover:opacity-80',
                totalPnl >= 0 ? 'text-quant-green' : 'text-quant-red'
              )}
            >
              盈亏 {totalPnl >= 0 ? '+' : ''}${formatCurrency(totalPnl)}
              {botSort?.key === 'pnl' ? (botSort.dir === 'desc' ? ' ▼' : ' ▲') : ''}
            </button>
          </div>
          <span className="flex-1" />
          <Button variant="primary" size="sm" leftIcon={<Plus className="w-3 h-3" />} onClick={() => setWizard('menu')}>
            新建机器人
          </Button>
        </div>

        {/* 统一管理中心：网格/马丁/华尔街/补仓/AI/现货策略/合约策略 */}
        <>
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
          {/* AI 相关入口（AI 研究导航组已精简，入口迁入机器人中心） */}
          <span className="ml-auto flex items-center gap-2">
            <button
              onClick={() => navigate('/bots/ai')}
              className="px-3 py-1.5 rounded-full text-xs text-muted-foreground border border-quant-border hover:text-foreground transition-colors"
            >
              AI 机器人市场 →
            </button>
            <button
              onClick={() => navigate('/ai/freqai')}
              className="px-3 py-1.5 rounded-full text-xs text-muted-foreground border border-quant-border hover:text-foreground transition-colors"
            >
              FreqAI →
            </button>
            <button
              onClick={() => navigate('/model-management')}
              className="px-3 py-1.5 rounded-full text-xs text-muted-foreground border border-quant-border hover:text-foreground transition-colors"
            >
              模型管理 →
            </button>
          </span>
        </div>

        {/* 卡片网格 */}
        <SectionCard title="机器人列表" headerAction={<span className="text-xs text-[#8a8a8a]">{filtered.length} 个</span>}>
          {isLoading ? (
            <div className="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-4 gap-3">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-32 rounded-xl" />
              ))}
            </div>
          ) : filtered.length === 0 ? (
            <EmptyState
              icon={<Bot className="w-8 h-8" />}
              title={typeFilter === 'all' ? '暂无机器人' : `暂无${FILTER_OPTIONS.find((f) => f.key === typeFilter)?.label}类机器人`}
              description="点击右上角「新建机器人」选择类型创建"
              actionLabel="新建机器人"
              onAction={() => setWizard('menu')}
            />
          ) : (
            <div className="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-4 gap-3">
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
                  onDetail={(b) => setDetailBot(b)}
                />
              ))}
            </div>
          )}
        </SectionCard>
        </>
      </div>

      {/* 新建入口：现货/合约策略 → /create；AI → 机器人页/参数向导 */}
      {wizard === 'menu' && (
        <ModalShell title="新建机器人" subtitle="选择机器人类型模板" onClose={() => setWizard(null)}>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            {TEMPLATES.map((t) => (
              <button
                key={t.key}
                onClick={() => {
                  setWizard(null)
                  if (t.key === 'spot') navigate('/create?market=spot')
                  else if (t.key === 'contract') navigate('/create?market=contract')
                  else if (t.key === 'ai') navigate('/bots/ai')
                  else setWizard('custom')
                }}
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
      {/* AI 自定义机器人参数向导 */}
      {wizard === 'custom' && (
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

      {/* 策略实例详情：RuntimePanel + 资金三件套（策略管理页价值保留） */}
      {detailBot?.strategy && (
        <ModalShell title={detailBot.name} subtitle="实时运行状态" onClose={() => setDetailBot(null)}>
          <div className="space-y-4">
            <StrategyRuntimeSummary strategy={detailBot.strategy} />
            <RuntimePanel strategy={detailBot.strategy} />
            <div className="flex items-center justify-end gap-2 pt-2 border-t border-quant-border">
              <button
                className="px-4 py-2 rounded-lg border border-quant-border text-xs text-muted-foreground hover:text-foreground transition-colors"
                onClick={() => setDetailBot(null)}
              >
                关闭
              </button>
              <button
                className="px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity"
                onClick={() => {
                  const m = detailBot.strategy?.market_type === 'spot' ? 'spot' : 'contract'
                  navigate(`/create?market=${m}&id=${detailBot.id}`)
                }}
              >
                编辑
              </button>
            </div>
          </div>
        </ModalShell>
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

/* ── 策略实例详情头部：资金三件套（初始资金/当前权益/收益率）+ 累计盈亏 ── */
function StrategyRuntimeSummary({ strategy }: { strategy: StrategyItem }) {
  const initial = strategy.initial_capital ?? 0
  const equity = strategy.current_equity
  const returnPct = initial > 0 && equity != null ? ((equity - initial) / initial) * 100 : null
  const pnl = strategy.total_pnl ?? 0
  return (
    <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
      {[
        { label: '初始资金', value: strategy.initial_capital != null ? `$${formatCurrency(strategy.initial_capital)}` : '-' },
        { label: '当前权益', value: equity != null ? `$${formatCurrency(equity)}` : '-' },
        {
          label: '收益率',
          value: returnPct != null ? `${returnPct >= 0 ? '+' : ''}${returnPct.toFixed(2)}%` : '—',
          color: returnPct == null ? undefined : returnPct >= 0 ? 'text-quant-green' : 'text-quant-red',
        },
        {
          label: '累计盈亏',
          value: pnl !== 0 ? `${pnl >= 0 ? '+' : ''}$${formatCurrency(pnl)}` : '-',
          color: pnl >= 0 ? 'text-quant-green' : 'text-quant-red',
        },
      ].map((k) => (
        <div key={k.label} className="p-3 rounded-lg bg-quant-bg border border-quant-border">
          <div className="text-[10px] text-muted-foreground">{k.label}</div>
          <div className={cn('text-sm font-bold font-mono', k.color || 'text-foreground')}>{k.value}</div>
        </div>
      ))}
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
