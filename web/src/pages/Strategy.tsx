import { useState, useEffect, useMemo, useRef, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { strategyApi, aiApi, mlApi } from '@/lib/api'
import { cn, formatCurrency, formatPercent } from '@/lib/utils'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import {
  Play, Pause, Save, FlaskConical, Code, Bot, MessageSquare,
  LayoutDashboard, FolderOpen, Search, Plus, ChevronRight, ChevronDown,
  MoreVertical, Trash2, Edit3, Activity, Wallet, TrendingUp, TrendingDown,
  Send, Cpu, Sparkles, BarChart3, X, CheckCircle2, AlertTriangle,
  Clock, DollarSign, Zap, ArrowRight, Settings2, Layers, GripVertical,
  FileCode2, Terminal, Copy, RotateCcw, Eye, EyeOff, SlidersHorizontal,
  BrainCircuit, Vote, Gauge
} from 'lucide-react'

/* ─── Types ─── */
interface StrategyItem {
  id: string
  name: string
  symbol?: string
  status: 'running' | 'stopped' | 'error'
  mode?: 'signal' | 'script' | 'bot'
  type?: string
  group_id?: string
  group_name?: string
  initial_capital?: number
  current_equity?: number
  total_pnl?: number
  total_pnl_percent?: number
  leverage?: number
  timeframe?: string
  trade_direction?: 'long' | 'short' | 'both'
  market_type?: 'swap' | 'spot'
  indicator_name?: string
  exchange_id?: string
  created_at?: string
  updated_at?: string
  strategy_code?: string
  ai_generated?: boolean
  // ── CRA 策略参数 ──
  order_count?: number              // 做单数量 (5-7单)
  first_order_amount?: number       // 首单仓位 (10-10000U)
  add_position_spread?: number      // 补仓价差 (0.5-50%)
  add_position_callback?: number    // 补仓回调 (0.01-0.5%)
  take_profit_ratio?: number        // 止盈比例 (默认1.3%)
  profit_callback?: number          // 盈利回调 (0.01-0.5%)
  trade_count_mode?: 'single' | 'cycle'  // 交易次数: 单次循环/策略循环
  open_indicator?: 'macd_golden' | 'macd_death' | 'ema' | 'close'  // 开仓指标
  add_position_indicator?: 'macd' | 'ema' | 'close'  // 补仓指标
  add_position_multiple?: number    // 补仓倍数
  waterfall_protection?: number     // 防瀑布 (默认2%)
  open_double?: boolean             // 开仓加倍
  trend_indicator?: boolean        // 是否开启趋势指标(EMA4)
  trend_timeframe?: '5m' | '15m' | '30m' | '60m'  // EMA4时间周期
  take_profit_method?: 'full' | 'tail' | 'head_tail' | 'moving'  // 止盈方式
  moving_take_profit?: {            // 移动止盈
    enabled: boolean
    tier1_ratio: number             // 第一档比例 (默认1.5%)
    tier1_drawback: number          // 第一档回撤 (默认30%)
    tier2_drawback: number          // 第二档回撤 (默认20%)
  }
  reverse_take_profit?: boolean     // 反向止盈
  reverse_stop_loss?: boolean       // 反向止损
  amplitude?: {                     // 振幅
    '5m': number; '15m': number; '30m': number; '1h': number
  }
  burn_cut?: {                      // 斩仓和燃烧
    enabled: boolean
    dual_burn_start: number         // 双向燃烧起始仓 (默认3)
    global_burn_start: number       // 全局燃烧起始仓 (默认5)
  }
  custom_reduce?: boolean           // 自定义减仓
  online_order_limit?: number       // 限制在线单量
  profit_protection?: boolean       // 盈利保护
  follow_trend?: boolean            // 顺势而为
  follow_trend_max?: number         // 顺势最大倍数 (最高5倍)
  stop_loss_ratio?: number          // 止损比例
  stop_loss_amount?: number         // 止损金额
  stop_loss_price?: number          // 止损价格
  first_order_price?: number        // 首单挂单价格 (0=市价)
  close_add_position?: boolean      // 关闭补仓
}

interface StrategyGroup {
  id: string
  baseName: string
  strategies: StrategyItem[]
  runningCount: number
  stoppedCount: number
}

interface ChatMessage {
  role: 'user' | 'bot'
  content: string
  meta?: { model?: string; confidence?: number; latency?: number }
}

/* ─── Constants ─── */
const TABS = [
  { key: 'overview', label: '概览', icon: LayoutDashboard },
  { key: 'strategy', label: '策略管理', icon: FolderOpen },
  { key: 'code', label: '代码编辑器', icon: Code },
  { key: 'freqtrade', label: 'ML 策略', icon: FlaskConical },
  { key: 'dinger', label: 'QuantDinger AI', icon: MessageSquare },
] as const

const STRAT_TYPES = {
  contract: [
    { value: 'trend_long', label: '顺势做多（EMA金叉）' },
    { value: 'trend_short', label: '顺势做空（EMA死叉）' },
    { value: 'counter_stable', label: '逆势稳健（EMA60振幅）' },
    { value: 'counter_safe', label: '逆势保守' },
    { value: 'high_flat', label: '高平策略' },
    { value: 'head_tail_arb', label: '首尾套利' },
    { value: 'macd_golden_long', label: 'MACD金叉开多' },
    { value: 'macd_death_short', label: 'MACD死叉开空' },
    { value: 'ema_follow_trend', label: 'EMA顺势（拐点开仓）' },
    { value: 'ema_counter_trend', label: 'EMA逆势（振幅开仓）' },
    { value: 'dual_burn', label: '双向燃烧斩仓' },
    { value: 'global_burn', label: '超级全局燃烧斩仓' },
  ],
  spot: [
    { value: 'martin_trend', label: '马丁趋势策略（倍投2,4,8,16,32,64）' },
    { value: 'wallstreet', label: '华尔街策略（等比1,2,3,5,8,13,21,34,55）' },
    { value: 'aggressive', label: '激进策略' },
    { value: 'conservative', label: '保守策略' },
    { value: 'high_flat', label: '高平策略' },
    { value: 'macd_spot_long', label: 'MACD金叉开多' },
    { value: 'ema_spot', label: 'EMA拐点策略' },
  ],
}

const MODELS = [
  { id: 'gpt-4o', name: 'GPT-4o', color: 'text-green-400' },
  { id: 'claude-3.5', name: 'Claude-3.5', color: 'text-orange-400' },
  { id: 'deepseek', name: 'DeepSeek', color: 'text-blue-400' },
  { id: 'gemini', name: 'Gemini', color: 'text-purple-400' },
  { id: 'grok', name: 'Grok', color: 'text-red-400' },
]

const TIMEFRAMES = ['1m', '5m', '15m', '30m', '1h', '4h', '8h', '1D']

const DEFAULT_CODE = `from freqtrade.strategy import IStrategy
import talib.abstract as ta

class MyStrategy(IStrategy):
    timeframe = '15m'
    minimal_roi = {"0": 0.01, "60": 0.005}
    stoploss = -0.40

    def populate_indicators(self, dataframe, metadata):
        dataframe['ema_short'] = ta.EMA(dataframe, timeperiod=12)
        dataframe['ema_long'] = ta.EMA(dataframe, timeperiod=26)
        return dataframe

    def populate_entry_trend(self, dataframe, metadata):
        dataframe.loc[dataframe['ema_short'] > dataframe['ema_long'], 'enter_long'] = 1
        return dataframe

    def populate_exit_trend(self, dataframe, metadata):
        dataframe.loc[dataframe['ema_short'] < dataframe['ema_long'], 'exit_long'] = 1
        return dataframe`

/* ─── Helpers ─── */
function useLocalStorage<T>(key: string, initial: T): [T, (v: T) => void] {
  const [val, setVal] = useState<T>(() => {
    try { return JSON.parse(localStorage.getItem(key) || 'null') ?? initial } catch { return initial }
  })
  useEffect(() => { localStorage.setItem(key, JSON.stringify(val)) }, [key, val])
  return [val, setVal]
}

function getStatusColor(status: string) {
  switch (status) {
    case 'running': return 'bg-quant-green/10 text-quant-green border-quant-green/20'
    case 'error': return 'bg-quant-red/10 text-quant-red border-quant-red/20'
    default: return 'bg-quant-bg-tertiary text-muted-foreground border-quant-border'
  }
}

function getStatusDot(status: string) {
  switch (status) {
    case 'running': return 'bg-quant-green'
    case 'error': return 'bg-quant-red'
    default: return 'bg-muted-foreground'
  }
}

/* ─── Main Component ─── */
export function Strategy() {
  const [tab, setTab] = useState<typeof TABS[number]['key']>('overview')
  const [guideDismissed, setGuideDismissed] = useLocalStorage('strategy-guide-dismissed', false)

  return (
    <div className="h-full flex flex-col min-w-0">
      {/* Top Guide Bar */}
      {!guideDismissed && <GuideBar onDismiss={() => setGuideDismissed(true)} onCreate={() => setTab('strategy')} />}

      {/* Tabs */}
      <div className="flex border-b border-quant-border px-2 shrink-0">
        {TABS.map((t) => (
          <button
            key={t.key}
            onClick={() => setTab(t.key)}
            className={cn(
              'flex items-center gap-1.5 px-4 py-2.5 text-xs font-medium transition-colors relative',
              tab === t.key ? 'text-quant-gold' : 'text-muted-foreground hover:text-foreground'
            )}
          >
            <t.icon className="w-3.5 h-3.5" />
            {t.label}
            {tab === t.key && <span className="absolute bottom-0 left-0 right-0 h-0.5 bg-quant-gold" />}
          </button>
        ))}
      </div>

      {/* Content */}
      <div className="flex-1 overflow-hidden">
        {tab === 'overview' && <OverviewTab />}
        {tab === 'strategy' && <StrategyManagementTab />}
        {tab === 'code' && <CodeEditorTab />}
        {tab === 'freqtrade' && <MLStrategyTab />}
        {tab === 'dinger' && <QuantDingerAITab />}
      </div>
    </div>
  )
}

/* ═══════════════════════════════════════════════════════════════
   Guide Bar
   ═══════════════════════════════════════════════════════════════ */
function GuideBar({ onDismiss, onCreate }: { onDismiss: () => void; onCreate: () => void }) {
  const steps = [
    { idx: 1, title: '选择策略类型', desc: '从指标信号、脚本代码或AI生成中选择适合的模式' },
    { idx: 2, title: '配置交易参数', desc: '设置交易对、杠杆、风控及通知渠道' },
    { idx: 3, title: '启动实盘或信号', desc: '连接交易所API，开启自动交易或仅接收信号' },
  ]
  return (
    <div className="shrink-0 mx-4 mt-3 mb-2 rounded-2xl border border-quant-gold/10 bg-gradient-to-r from-quant-gold/5 to-purple-500/5 px-5 py-4 flex flex-wrap gap-4 items-center">
      <div className="flex-1 min-w-[200px]">
        <div className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-quant-gold/10 text-quant-gold text-[11px] font-semibold mb-2">
          <Sparkles className="w-3 h-3" /> 快速入门
        </div>
        <div className="text-sm font-bold text-foreground">三步创建你的第一个量化策略</div>
        <div className="text-xs text-muted-foreground mt-1">跟随向导完成策略配置，支持指标信号、代码脚本和AI生成三种模式</div>
      </div>
      <div className="flex gap-3 flex-1 min-w-[300px]">
        {steps.map((s) => (
          <div key={s.idx} className="flex-1 flex items-start gap-2.5 rounded-xl bg-quant-bg/60 border border-quant-border/40 p-3">
            <div className="w-6 h-6 rounded-full bg-gradient-to-br from-quant-gold to-purple-500 flex items-center justify-center text-[10px] font-bold text-white shrink-0">
              {s.idx}
            </div>
            <div className="min-w-0">
              <div className="text-xs font-semibold text-foreground">{s.title}</div>
              <div className="text-[10px] text-muted-foreground leading-relaxed mt-0.5">{s.desc}</div>
            </div>
          </div>
        ))}
      </div>
      <div className="flex items-center gap-2 shrink-0">
        <button onClick={onCreate} className="px-3 py-2 rounded-lg bg-quant-gold/10 text-quant-gold border border-quant-gold/20 text-xs font-medium hover:bg-quant-gold/20 transition-colors flex items-center gap-1.5">
          <Plus className="w-3.5 h-3.5" /> 创建策略
        </button>
        <button onClick={onDismiss} className="w-8 h-8 rounded-lg border border-quant-border flex items-center justify-center text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors">
          <X className="w-3.5 h-3.5" />
        </button>
      </div>
    </div>
  )
}

/* ═══════════════════════════════════════════════════════════════
   Overview Tab
   ═══════════════════════════════════════════════════════════════ */
function OverviewTab() {
  const { data: strategies } = useQuery({ queryKey: ['strategies'], queryFn: () => strategyApi.list() })
  const list: StrategyItem[] = strategies || []
  const running = list.filter((s) => s.status === 'running').length
  const stopped = list.filter((s) => s.status === 'stopped').length
  const totalPnl = list.reduce((sum, s) => sum + (s.total_pnl || 0), 0)

  return (
    <div className="h-full overflow-y-auto p-6 space-y-5">
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <StatCard icon={Layers} label="总策略数" value={String(list.length)} />
        <StatCard icon={Activity} label="运行中" value={String(running)} color="text-quant-green" />
        <StatCard icon={Pause} label="已停止" value={String(stopped)} color="text-muted-foreground" />
        <StatCard icon={TrendingUp} label="总盈亏" value={totalPnl >= 0 ? `+$${totalPnl.toFixed(2)}` : `-$${Math.abs(totalPnl).toFixed(2)}`} color={totalPnl >= 0 ? 'text-quant-green' : 'text-quant-red'} />
      </div>
      <SectionCard title="最近活跃策略">
        {list.length === 0 ? (
          <EmptyState title="暂无策略" description="前往策略管理页创建你的第一个策略" />
        ) : (
          <div className="space-y-2">
            {list.slice(0, 5).map((s) => (
              <div key={s.id} className="flex items-center justify-between px-3 py-2.5 rounded-lg bg-quant-bg-tertiary border border-quant-border hover:border-quant-gold/20 transition-colors">
                <div className="flex items-center gap-3 min-w-0">
                  <span className={cn('w-2 h-2 rounded-full', getStatusDot(s.status))} />
                  <span className="text-xs font-medium truncate">{s.name}</span>
                  <span className="text-[10px] text-muted-foreground">{s.symbol}</span>
                </div>
                <div className="flex items-center gap-3 text-xs">
                  <span className={cn(s.total_pnl && s.total_pnl >= 0 ? 'text-quant-green' : 'text-quant-red')}>{s.total_pnl != null ? formatCurrency(s.total_pnl) : '-'}</span>
                  <StatusBadge status={s.status} />
                </div>
              </div>
            ))}
          </div>
        )}
      </SectionCard>
    </div>
  )
}

function StatCard({ icon: Icon, label, value, color }: { icon: any; label: string; value: string; color?: string }) {
  return (
    <div className="rounded-xl border border-quant-border bg-quant-card p-4 flex items-center gap-3">
      <div className="w-10 h-10 rounded-lg bg-quant-bg-tertiary flex items-center justify-center text-quant-gold">
        <Icon className="w-5 h-5" />
      </div>
      <div>
        <div className={cn('text-lg font-bold', color || 'text-foreground')}>{value}</div>
        <div className="text-[11px] text-muted-foreground">{label}</div>
      </div>
    </div>
  )
}

/* ═══════════════════════════════════════════════════════════════
   Strategy Management Tab
   ═══════════════════════════════════════════════════════════════ */
function StrategyManagementTab() {
  const queryClient = useQueryClient()
  const { data: strategies, isLoading } = useQuery({ queryKey: ['strategies'], queryFn: () => strategyApi.list() })
  const [groupBy, setGroupBy] = useState<'strategy' | 'symbol'>('strategy')
  const [search, setSearch] = useState('')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const [showCreate, setShowCreate] = useState(false)
  const [editingStrategy, setEditingStrategy] = useState<StrategyItem | null>(null)

  const list: StrategyItem[] = strategies || []
  const filtered = useMemo(() => {
    if (!search.trim()) return list
    const q = search.toLowerCase()
    return list.filter((s) => s.name.toLowerCase().includes(q) || (s.symbol || '').toLowerCase().includes(q))
  }, [list, search])

  const grouped = useMemo(() => {
    if (groupBy === 'symbol') {
      const map = new Map<string, StrategyGroup>()
      const ungrouped: StrategyItem[] = []
      filtered.forEach((s) => {
        const sym = s.symbol || '未指定标的'
        if (!map.has(sym)) map.set(sym, { id: `sym_${sym}`, baseName: sym, strategies: [], runningCount: 0, stoppedCount: 0 })
        const g = map.get(sym)!
        g.strategies.push(s)
        s.status === 'running' ? g.runningCount++ : g.stoppedCount++
      })
      return { groups: Array.from(map.values()).sort((a, b) => a.baseName.localeCompare(b.baseName)), ungrouped }
    }
    const map = new Map<string, StrategyGroup>()
    filtered.forEach((s) => {
      const gid = s.group_id || ''
      if (gid) {
        if (!map.has(gid)) map.set(gid, { id: gid, baseName: s.group_name || s.name.split('-')[0] || '默认分组', strategies: [], runningCount: 0, stoppedCount: 0 })
        const g = map.get(gid)!
        g.strategies.push(s)
        s.status === 'running' ? g.runningCount++ : g.stoppedCount++
      }
    })
    const groupedIds = new Set(Array.from(map.keys()))
    const ungrouped = filtered.filter((s) => !s.group_id || !groupedIds.has(s.group_id))
    return { groups: Array.from(map.values()), ungrouped }
  }, [filtered, groupBy])

  const selected = list.find((s) => s.id === selectedId) || null

  const startMut = useMutation({ mutationFn: (id: string) => strategyApi.start(id), onSuccess: () => queryClient.invalidateQueries({ queryKey: ['strategies'] }) })
  const stopMut = useMutation({ mutationFn: (id: string) => strategyApi.stop(id), onSuccess: () => queryClient.invalidateQueries({ queryKey: ['strategies'] }) })
  const deleteMut = useMutation({ mutationFn: (id: string) => strategyApi.delete(id), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['strategies'] }); setSelectedId(null) } })

  const toggleGroup = (id: string) => setCollapsed((p) => ({ ...p, [id]: !p[id] }))

  const handleGroupAction = (action: 'startAll' | 'stopAll' | 'deleteAll', group: StrategyGroup) => {
    const ids = group.strategies.map((s) => s.id)
    if (action === 'startAll') ids.forEach((id) => startMut.mutate(id))
    if (action === 'stopAll') ids.forEach((id) => stopMut.mutate(id))
    if (action === 'deleteAll') {
      if (confirm(`确定删除分组 "${group.baseName}" 下的 ${ids.length} 个策略？`)) ids.forEach((id) => deleteMut.mutate(id))
    }
  }

  return (
    <div className="h-full flex">
      {/* Left: Strategy List */}
      <div className="hidden md:flex w-80 shrink-0 border-r border-quant-border bg-quant-bg-secondary flex-col">
        <div className="p-3 border-b border-quant-border flex items-center justify-between gap-2">
          <div className="relative flex-1">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="搜索策略..."
              className="w-full bg-quant-bg border border-quant-border rounded-lg pl-8 pr-3 py-2 text-xs focus:outline-none focus:border-quant-gold"
            />
          </div>
          <button onClick={() => { setEditingStrategy(null); setShowCreate(true) }} className="shrink-0 px-2.5 py-2 rounded-lg bg-quant-gold/10 text-quant-gold border border-quant-gold/20 hover:bg-quant-gold/20 transition-colors">
            <Plus className="w-4 h-4" />
          </button>
        </div>

        {/* Group toggle */}
        <div className="px-3 py-2 border-b border-quant-border flex items-center gap-2">
          <span className="text-[11px] text-muted-foreground">分组:</span>
          <div className="flex rounded-md border border-quant-border overflow-hidden">
            <button onClick={() => setGroupBy('strategy')} className={cn('px-2.5 py-1 text-[11px] transition-colors', groupBy === 'strategy' ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground hover:text-foreground')}>
              <FolderOpen className="w-3 h-3 inline mr-1" />策略
            </button>
            <button onClick={() => setGroupBy('symbol')} className={cn('px-2.5 py-1 text-[11px] transition-colors border-l border-quant-border', groupBy === 'symbol' ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground hover:text-foreground')}>
              <DollarSign className="w-3 h-3 inline mr-1" />标的
            </button>
          </div>
        </div>

        {/* List */}
        <div className="flex-1 overflow-y-auto p-2 space-y-2">
          {isLoading && <div className="text-center text-xs text-muted-foreground py-8">加载中...</div>}
          {!isLoading && list.length === 0 && (
            <div className="px-2 py-6 text-center">
              <EmptyState title="暂无策略" description="点击右上角 + 创建策略" actionLabel="创建策略" onAction={() => { setEditingStrategy(null); setShowCreate(true) }} />
            </div>
          )}

          {grouped.groups.map((g) => (
            <div key={g.id} className="rounded-lg border border-quant-border overflow-hidden">
              <div onClick={() => toggleGroup(g.id)} className="flex items-center justify-between px-3 py-2 bg-quant-bg-tertiary cursor-pointer hover:bg-quant-hover transition-colors">
                <div className="flex items-center gap-2 min-w-0">
                  {collapsed[g.id] ? <ChevronRight className="w-3.5 h-3.5 text-muted-foreground shrink-0" /> : <ChevronDown className="w-3.5 h-3.5 text-muted-foreground shrink-0" />}
                  <span className="text-xs font-semibold truncate">{g.baseName}</span>
                  <span className="text-[10px] px-1.5 py-0.5 rounded bg-quant-bg border border-quant-border text-muted-foreground">{g.strategies.length}</span>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  {g.runningCount > 0 && <span className="text-[10px] text-quant-green">{g.runningCount} 运行</span>}
                  {g.stoppedCount > 0 && <span className="text-[10px] text-quant-red">{g.stoppedCount} 停止</span>}
                  <GroupDropdown onAction={(a) => handleGroupAction(a, g)} />
                </div>
              </div>
              {!collapsed[g.id] && (
                <div className="p-1.5 space-y-1">
                  {g.strategies.map((s) => (
                    <StrategyListItem key={s.id} strategy={s} selected={selectedId === s.id} onSelect={() => setSelectedId(s.id)} onStart={() => startMut.mutate(s.id)} onStop={() => stopMut.mutate(s.id)} onEdit={() => { setEditingStrategy(s); setShowCreate(true) }} onDelete={() => { if (confirm(`删除策略 "${s.name}"？`)) deleteMut.mutate(s.id) }} />
                  ))}
                </div>
              )}
            </div>
          ))}

          {grouped.ungrouped.length > 0 && (
            <div className="space-y-1">
              {grouped.ungrouped.map((s) => (
                <StrategyListItem key={s.id} strategy={s} selected={selectedId === s.id} onSelect={() => setSelectedId(s.id)} onStart={() => startMut.mutate(s.id)} onStop={() => stopMut.mutate(s.id)} onEdit={() => { setEditingStrategy(s); setShowCreate(true) }} onDelete={() => { if (confirm(`删除策略 "${s.name}"？`)) deleteMut.mutate(s.id) }} />
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Right: Detail / Empty */}
      <div className="flex-1 overflow-y-auto p-6">
        {!selected ? (
          <div className="h-full flex items-center justify-center">
            <EmptyState
              icon={<Bot className="w-6 h-6" />}
              title="选择或创建一个策略"
              description="从左侧列表选择策略查看详情，或点击创建按钮新建策略"
              actionLabel="创建策略"
              onAction={() => { setEditingStrategy(null); setShowCreate(true) }}
            />
          </div>
        ) : (
          <StrategyDetailPanel strategy={selected} onStart={() => startMut.mutate(selected.id)} onStop={() => stopMut.mutate(selected.id)} onEdit={() => { setEditingStrategy(selected); setShowCreate(true) }} onDelete={() => { if (confirm(`删除策略 "${selected.name}"？`)) deleteMut.mutate(selected.id) }} />
        )}
      </div>

      {/* Create / Edit Modal */}
      {showCreate && (
        <StrategyFormModal
          editing={editingStrategy}
          onClose={() => { setShowCreate(false); setEditingStrategy(null) }}
          onSaved={() => { setShowCreate(false); setEditingStrategy(null); queryClient.invalidateQueries({ queryKey: ['strategies'] }) }}
        />
      )}
    </div>
  )
}

function StrategyListItem({ strategy, selected, onSelect, onStart, onStop, onEdit, onDelete }: {
  strategy: StrategyItem; selected: boolean; onSelect: () => void; onStart: () => void; onStop: () => void; onEdit: () => void; onDelete: () => void
}) {
  const [menuOpen, setMenuOpen] = useState(false)
  return (
    <div onClick={onSelect} className={cn(
      'flex items-center justify-between gap-2 px-3 py-2.5 rounded-md cursor-pointer transition-all border',
      selected
        ? 'bg-quant-gold/5 border-quant-gold/30 border-l-2 border-l-quant-gold'
        : 'bg-quant-bg border-transparent hover:bg-quant-bg-tertiary hover:border-quant-border'
    )}>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="text-xs font-medium truncate">{strategy.name}</span>
          {strategy.ai_generated && <span className="text-[10px] px-1 rounded bg-purple-500/10 text-purple-400 border border-purple-500/20">AI</span>}
          {strategy.mode === 'script' && <span className="text-[10px] px-1 rounded bg-green-500/10 text-green-400 border border-green-500/20">脚本</span>}
        </div>
        <div className="flex items-center gap-2 mt-1">
          <span className="text-[10px] text-muted-foreground flex items-center gap-1"><DollarSign className="w-3 h-3" />{strategy.symbol || '-'}</span>
          <span className="text-[10px] text-muted-foreground flex items-center gap-1"><Clock className="w-3 h-3" />{strategy.timeframe || '-'}</span>
          <StatusBadge status={strategy.status} />
        </div>
      </div>
      <div className="relative shrink-0" onClick={(e) => e.stopPropagation()}>
        <button onClick={() => setMenuOpen((v) => !v)} className="p-1 rounded hover:bg-quant-hover text-muted-foreground hover:text-foreground">
          <MoreVertical className="w-3.5 h-3.5" />
        </button>
        {menuOpen && (
          <>
            <div className="fixed inset-0 z-10" onClick={() => setMenuOpen(false)} />
            <div className="absolute right-0 top-full mt-1 w-32 rounded-lg border border-quant-border bg-quant-card shadow-lg z-20 py-1">
              {strategy.status === 'stopped' && (
                <button onClick={() => { onStart(); setMenuOpen(false) }} className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-green">
                  <Play className="w-3 h-3" /> 启动
                </button>
              )}
              {strategy.status === 'running' && (
                <button onClick={() => { onStop(); setMenuOpen(false) }} className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-orange">
                  <Pause className="w-3 h-3" /> 停止
                </button>
              )}
              <button onClick={() => { onEdit(); setMenuOpen(false) }} className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2">
                <Edit3 className="w-3 h-3" /> 编辑
              </button>
              <div className="border-t border-quant-border my-1" />
              <button onClick={() => { onDelete(); setMenuOpen(false) }} className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-red">
                <Trash2 className="w-3 h-3" /> 删除
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

function GroupDropdown({ onAction }: { onAction: (action: 'startAll' | 'stopAll' | 'deleteAll') => void }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="relative" onClick={(e) => e.stopPropagation()}>
      <button onClick={() => setOpen((v) => !v)} className="p-1 rounded hover:bg-quant-hover text-muted-foreground hover:text-foreground">
        <MoreVertical className="w-3.5 h-3.5" />
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-10" onClick={() => setOpen(false)} />
          <div className="absolute right-0 top-full mt-1 w-32 rounded-lg border border-quant-border bg-quant-card shadow-lg z-20 py-1">
            <button onClick={() => { onAction('startAll'); setOpen(false) }} className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-green">
              <Play className="w-3 h-3" /> 全部启动
            </button>
            <button onClick={() => { onAction('stopAll'); setOpen(false) }} className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-orange">
              <Pause className="w-3 h-3" /> 全部停止
            </button>
            <div className="border-t border-quant-border my-1" />
            <button onClick={() => { onAction('deleteAll'); setOpen(false) }} className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-red">
              <Trash2 className="w-3 h-3" /> 全部删除
            </button>
          </div>
        </>
      )}
    </div>
  )
}

function StatusBadge({ status }: { status: string }) {
  return (
    <span className={cn('inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded border', getStatusColor(status))}>
      <span className={cn('w-1.5 h-1.5 rounded-full', getStatusDot(status))} />
      {status === 'running' ? '运行中' : status === 'error' ? '异常' : '已停止'}
    </span>
  )
}

/* ═══════════════════════════════════════════════════════════════
   Strategy Detail Panel
   ═══════════════════════════════════════════════════════════════ */
function StrategyDetailPanel({ strategy, onStart, onStop, onEdit, onDelete }: {
  strategy: StrategyItem; onStart: () => void; onStop: () => void; onEdit: () => void; onDelete: () => void
}) {
  const pnl = strategy.total_pnl ?? 0
  const pnlPct = strategy.total_pnl_percent ?? 0
  return (
    <div className="space-y-4 max-w-4xl mx-auto">
      {/* Header */}
      <div className="rounded-xl border border-quant-border bg-quant-card p-5">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="flex items-center gap-3 flex-wrap">
              <h2 className="text-lg font-bold text-foreground">{strategy.name}</h2>
              <StatusBadge status={strategy.status} />
              {strategy.ai_generated && <span className="text-[10px] px-1.5 py-0.5 rounded bg-purple-500/10 text-purple-400 border border-purple-500/20">AI 生成</span>}
              {strategy.mode === 'script' && <span className="text-[10px] px-1.5 py-0.5 rounded bg-green-500/10 text-green-400 border border-green-500/20">脚本</span>}
            </div>
            <div className="flex flex-wrap gap-2 mt-3">
              <Tag icon={DollarSign} label={strategy.symbol || '-'} />
              <Tag icon={Zap} label={`${strategy.leverage || 1}x`} />
              <Tag icon={ArrowRight} label={strategy.trade_direction === 'long' ? '做多' : strategy.trade_direction === 'short' ? '做空' : '双向'} />
              <Tag icon={Clock} label={strategy.timeframe || '-'} />
              {strategy.indicator_name && <Tag icon={BarChart3} label={strategy.indicator_name} />}
              {strategy.exchange_id && <Tag icon={Wallet} label={strategy.exchange_id} />}
            </div>
          </div>
          <div className="flex items-center gap-2 shrink-0">
            {strategy.status === 'stopped' ? (
              <button onClick={onStart} className="px-4 py-2 rounded-lg bg-quant-green text-white text-xs font-semibold hover:opacity-90 transition-opacity flex items-center gap-1.5">
                <Play className="w-3.5 h-3.5" /> 启动
              </button>
            ) : (
              <button onClick={onStop} className="px-4 py-2 rounded-lg bg-quant-red text-white text-xs font-semibold hover:opacity-90 transition-opacity flex items-center gap-1.5">
                <Pause className="w-3.5 h-3.5" /> 停止
              </button>
            )}
            <button onClick={onEdit} className="px-3 py-2 rounded-lg bg-quant-bg-tertiary border border-quant-border text-xs hover:bg-quant-hover transition-colors">
              <Edit3 className="w-3.5 h-3.5" />
            </button>
            <button onClick={onDelete} className="px-3 py-2 rounded-lg bg-quant-bg-tertiary border border-quant-border text-xs hover:bg-quant-red/10 hover:text-quant-red hover:border-quant-red/20 transition-colors">
              <Trash2 className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>

        {/* Stats */}
        <div className="grid grid-cols-3 gap-3 mt-5">
          <StatBox icon={Wallet} label="投入资金" value={strategy.initial_capital != null ? `$${formatCurrency(strategy.initial_capital)}` : '-'} />
          <StatBox icon={Activity} label="当前净值" value={strategy.current_equity != null ? `$${formatCurrency(strategy.current_equity)}` : '-'} />
          <StatBox icon={pnl >= 0 ? TrendingUp : TrendingDown} label="累计盈亏" value={pnl !== 0 ? `${pnl >= 0 ? '+' : ''}$${formatCurrency(pnl)} (${formatPercent(pnlPct)})` : '-'} valueColor={pnl >= 0 ? 'text-quant-green' : pnl < 0 ? 'text-quant-red' : undefined} />
        </div>
      </div>

      {/* Tabs inside detail */}
      <SectionCard title="策略详情">
        <div className="grid grid-cols-2 gap-4 text-xs">
          <DetailRow label="策略ID" value={strategy.id} />
          <DetailRow label="状态" value={strategy.status === 'running' ? '运行中' : strategy.status === 'error' ? '异常' : '已停止'} />
          <DetailRow label="交易对" value={strategy.symbol || '-'} />
          <DetailRow label="K线周期" value={strategy.timeframe || '-'} />
          <DetailRow label="杠杆" value={`${strategy.leverage || 1}x`} />
          <DetailRow label="方向" value={strategy.trade_direction === 'long' ? '做多' : strategy.trade_direction === 'short' ? '做空' : '双向'} />
          <DetailRow label="市场类型" value={strategy.market_type === 'spot' ? '现货' : '合约'} />
          <DetailRow label="创建时间" value={strategy.created_at ? new Date(strategy.created_at).toLocaleString() : '-'} />
        </div>
      </SectionCard>

      {/* ── CRA 参数详情 ── */}
      <SectionCard title="CRA 量化参数">
        <div className="grid grid-cols-2 gap-4 text-xs">
          <DetailRow label="做单数量" value={`${strategy.order_count || '-'} 单`} />
          <DetailRow label="首单仓位" value={strategy.first_order_amount ? `${strategy.first_order_amount} USDT` : '-'} />
          <DetailRow label="补仓价差" value={strategy.add_position_spread ? `${strategy.add_position_spread}%` : '-'} />
          <DetailRow label="补仓回调" value={strategy.add_position_callback ? `${strategy.add_position_callback}%` : '-'} />
          <DetailRow label="止盈比例" value={strategy.take_profit_ratio ? `${strategy.take_profit_ratio}%` : '-'} />
          <DetailRow label="盈利回调" value={strategy.profit_callback ? `${strategy.profit_callback}%` : '-'} />
          <DetailRow label="止盈方式" value={strategy.take_profit_method === 'full' ? '全仓止盈' : strategy.take_profit_method === 'tail' ? '尾单止盈' : strategy.take_profit_method === 'head_tail' ? '首尾止盈' : strategy.take_profit_method === 'moving' ? '移动止盈' : '-'} />
          <DetailRow label="开仓指标" value={strategy.open_indicator === 'macd_golden' ? 'MACD金叉开多' : strategy.open_indicator === 'macd_death' ? 'MACD死叉开空' : strategy.open_indicator === 'ema' ? 'EMA拐点' : strategy.open_indicator === 'close' ? '无脑买入' : '-'} />
          <DetailRow label="补仓指标" value={strategy.add_position_indicator === 'macd' ? 'MACD' : strategy.add_position_indicator === 'ema' ? 'EMA4' : '仅跌幅'} />
          <DetailRow label="防瀑布" value={strategy.waterfall_protection ? `${strategy.waterfall_protection}%` : '-'} />
          <DetailRow label="开仓加倍" value={strategy.open_double ? '已开启' : '未开启'} />
          <DetailRow label="趋势指标" value={strategy.trend_indicator ? `EMA4 (${strategy.trend_timeframe || '-'})` : '未开启'} />
          <DetailRow label="交易次数" value={strategy.trade_count_mode === 'single' ? '单次循环' : strategy.trade_count_mode === 'cycle' ? '策略循环' : '-'} />
          <DetailRow label="顺势而为" value={strategy.follow_trend ? `已开启 (最高${strategy.follow_trend_max || 5}倍)` : '未开启'} />
          <DetailRow label="斩仓燃烧" value={strategy.burn_cut?.enabled ? `双向${strategy.burn_cut.dual_burn_start}仓/全局${strategy.burn_cut.global_burn_start}仓` : '未开启'} />
          <DetailRow label="在线单量限制" value={strategy.online_order_limit ? `${strategy.online_order_limit} 单` : '-'} />
          <DetailRow label="盈利保护" value={strategy.profit_protection ? '已开启' : '未开启'} />
          <DetailRow label="自定义减仓" value={strategy.custom_reduce ? '已开启' : '未开启'} />
          <DetailRow label="反向止盈" value={strategy.reverse_take_profit ? '已开启' : '未开启'} />
          <DetailRow label="反向止损" value={strategy.reverse_stop_loss ? '已开启' : '未开启'} />
          <DetailRow label="关闭补仓" value={strategy.close_add_position ? '是（仅止盈）' : '否'} />
          <DetailRow label="首单挂单" value={strategy.first_order_price ? `${strategy.first_order_price} USDT` : '市价'} />
        </div>
      </SectionCard>
    </div>
  )
}

function Tag({ icon: Icon, label }: { icon: any; label: string }) {
  return (
    <span className="inline-flex items-center gap-1 text-[11px] px-2 py-1 rounded-md bg-quant-bg-tertiary border border-quant-border text-muted-foreground">
      <Icon className="w-3 h-3 text-quant-gold" /> {label}
    </span>
  )
}

function StatBox({ icon: Icon, label, value, valueColor }: { icon: any; label: string; value: string; valueColor?: string }) {
  return (
    <div className="flex items-center gap-3 p-3 rounded-lg bg-quant-bg border border-quant-border">
      <div className="w-9 h-9 rounded-lg bg-quant-bg-tertiary flex items-center justify-center text-quant-gold">
        <Icon className="w-4 h-4" />
      </div>
      <div>
        <div className={cn('text-sm font-bold', valueColor || 'text-foreground')}>{value}</div>
        <div className="text-[10px] text-muted-foreground">{label}</div>
      </div>
    </div>
  )
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between py-1.5 border-b border-quant-border/50">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-medium text-foreground">{value}</span>
    </div>
  )
}

/* ═══════════════════════════════════════════════════════════════
   Strategy Form Modal (Create / Edit)
   ═══════════════════════════════════════════════════════════════ */
function StrategyFormModal({ editing, onClose, onSaved }: { editing: StrategyItem | null; onClose: () => void; onSaved: () => void }) {
  const queryClient = useQueryClient()
  const [step, setStep] = useState(0)
  const [mode, setMode] = useState<'signal' | 'script'>('signal')
  const [market, setMarket] = useState<'contract' | 'spot'>('contract')
  const [name, setName] = useState(editing?.name || '')
  const [symbol, setSymbol] = useState(editing?.symbol || 'BTCUSDT')
  const [strategyType, setStrategyType] = useState('trend_long')
  const [timeframe, setTimeframe] = useState(editing?.timeframe || '15m')
  const [leverage, setLeverage] = useState(editing?.leverage || 5)
  const [direction, setDirection] = useState<'long' | 'short' | 'dual'>('long')
  const [initialCapital, setInitialCapital] = useState(editing?.initial_capital || 1000)
  const [executionMode, setExecutionMode] = useState<'live' | 'signal'>('signal')
  const [notifyChannels, setNotifyChannels] = useState<string[]>(['browser'])
  // ── CRA 核心参数 ──
  const [orderCount, setOrderCount] = useState(editing?.order_count || 7)
  const [firstOrderAmount, setFirstOrderAmount] = useState(editing?.first_order_amount || 100)
  const [addPosSpread, setAddPosSpread] = useState(editing?.add_position_spread || 3)
  const [addPosCallback, setAddPosCallback] = useState(editing?.add_position_callback || 0.1)
  const [takeProfitRatio, setTakeProfitRatio] = useState(editing?.take_profit_ratio || 1.3)
  const [profitCallback, setProfitCallback] = useState(editing?.profit_callback || 0.1)
  const [tradeCountMode, setTradeCountMode] = useState<'single' | 'cycle'>(editing?.trade_count_mode || 'cycle')
  const [openIndicator, setOpenIndicator] = useState(editing?.open_indicator || 'macd_golden')
  const [addPosIndicator, setAddPosIndicator] = useState(editing?.add_position_indicator || 'macd')
  const [addPosMultiple, setAddPosMultiple] = useState(editing?.add_position_multiple || 1)
  const [waterfallProtection, setWaterfallProtection] = useState(editing?.waterfall_protection ?? 2)
  const [openDouble, setOpenDouble] = useState(editing?.open_double || false)
  const [trendIndicator, setTrendIndicator] = useState(editing?.trend_indicator ?? false)
  const [trendTimeframe, setTrendTimeframe] = useState(editing?.trend_timeframe || '15m')
  const [takeProfitMethod, setTakeProfitMethod] = useState(editing?.take_profit_method || 'full')
  const [movingTP, setMovingTP] = useState(editing?.moving_take_profit || { enabled: false, tier1_ratio: 1.5, tier1_drawback: 30, tier2_drawback: 20 })
  const [reverseTP, setReverseTP] = useState(editing?.reverse_take_profit ?? false)
  const [reverseSL, setReverseSL] = useState(editing?.reverse_stop_loss ?? false)
  const [amplitude, setAmplitude] = useState(editing?.amplitude || { '5m': 2, '15m': 4, '30m': 7, '1h': 10 })
  const [burnCut, setBurnCut] = useState(editing?.burn_cut || { enabled: false, dual_burn_start: 3, global_burn_start: 5 })
  const [customReduce, setCustomReduce] = useState(editing?.custom_reduce ?? false)
  const [onlineOrderLimit, setOnlineOrderLimit] = useState(editing?.online_order_limit || 10)
  const [profitProtection, setProfitProtection] = useState(editing?.profit_protection ?? false)
  const [followTrend, setFollowTrend] = useState(editing?.follow_trend ?? false)
  const [followTrendMax, setFollowTrendMax] = useState(editing?.follow_trend_max || 5)
  const [stopLossRatio, setStopLossRatio] = useState(editing?.stop_loss_ratio || 0)
  const [stopLossAmount, setStopLossAmount] = useState(editing?.stop_loss_amount || 0)
  const [stopLossPrice, setStopLossPrice] = useState(editing?.stop_loss_price || 0)
  const [firstOrderPrice, setFirstOrderPrice] = useState(editing?.first_order_price || 0)
  const [closeAddPosition, setCloseAddPosition] = useState(editing?.close_add_position ?? false)

  const createMut = useMutation({ mutationFn: strategyApi.create, onSuccess: onSaved })
  const updateMut = useMutation({ mutationFn: ({ id, data }: { id: string; data: any }) => strategyApi.update(id, data), onSuccess: onSaved })

  const handleSubmit = () => {
    const payload = {
      name: name || '未命名策略',
      symbol,
      timeframe,
      leverage: market === 'spot' ? 1 : leverage,
      trade_direction: market === 'spot' ? 'long' : direction,
      market_type: market === 'spot' ? 'spot' : 'swap',
      initial_capital: initialCapital,
      execution_mode: executionMode,
      notification_config: { channels: notifyChannels },
      strategy_type: strategyType,
      status: 'stopped',
      // ── CRA 核心参数 ──
      order_count: orderCount,
      first_order_amount: firstOrderAmount,
      add_position_spread: addPosSpread,
      add_position_callback: addPosCallback,
      take_profit_ratio: takeProfitRatio,
      profit_callback: profitCallback,
      trade_count_mode: tradeCountMode,
      open_indicator: openIndicator,
      add_position_indicator: addPosIndicator,
      add_position_multiple: addPosMultiple,
      waterfall_protection: waterfallProtection,
      open_double: openDouble,
      trend_indicator: trendIndicator,
      trend_timeframe: trendTimeframe,
      take_profit_method: takeProfitMethod,
      moving_take_profit: movingTP,
      reverse_take_profit: reverseTP,
      reverse_stop_loss: reverseSL,
      amplitude,
      burn_cut: burnCut,
      custom_reduce: customReduce,
      online_order_limit: onlineOrderLimit,
      profit_protection: profitProtection,
      follow_trend: followTrend,
      follow_trend_max: followTrendMax,
      stop_loss_ratio: stopLossRatio,
      stop_loss_amount: stopLossAmount,
      stop_loss_price: stopLossPrice,
      first_order_price: firstOrderPrice,
      close_add_position: closeAddPosition,
    }
    if (editing) {
      updateMut.mutate({ id: editing.id, data: payload })
    } else {
      createMut.mutate(payload)
    }
  }

  const steps = mode === 'script'
    ? ['基础配置', '代码编辑', '执行设置']
    : ['基础配置', '执行设置']

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
      <div className="w-full max-w-2xl max-h-[85vh] flex flex-col rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
          <h3 className="text-sm font-bold">{editing ? '编辑策略' : '创建策略'}{mode === 'script' ? ' - 脚本' : ''}</h3>
          <button onClick={onClose} className="text-muted-foreground hover:text-foreground"><X className="w-4 h-4" /></button>
        </div>

        {/* Steps */}
        <div className="px-6 py-4 border-b border-quant-border shrink-0">
          <div className="flex items-center gap-2">
            {steps.map((s, i) => (
              <div key={s} className="flex items-center gap-2">
                <span className={cn('w-6 h-6 rounded-full flex items-center justify-center text-[10px] font-bold', i === step ? 'bg-quant-gold text-white' : i < step ? 'bg-quant-green text-white' : 'bg-quant-bg-tertiary text-muted-foreground border border-quant-border')}>
                  {i < step ? <CheckCircle2 className="w-3.5 h-3.5" /> : i + 1}
                </span>
                <span className={cn('text-xs', i === step ? 'text-foreground font-medium' : 'text-muted-foreground')}>{s}</span>
                {i < steps.length - 1 && <ChevronRight className="w-3 h-3 text-muted-foreground" />}
              </div>
            ))}
          </div>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-6 space-y-5">
          {step === 0 && (
            <>
              {/* Mode selector */}
              {!editing && (
                <div className="flex rounded-lg border border-quant-border overflow-hidden">
                  <button onClick={() => setMode('signal')} className={cn('flex-1 py-2 text-xs font-medium transition-colors', mode === 'signal' ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground hover:text-foreground')}>
                    <Activity className="w-3.5 h-3.5 inline mr-1" />指标信号
                  </button>
                  <button onClick={() => setMode('script')} className={cn('flex-1 py-2 text-xs font-medium transition-colors border-l border-quant-border', mode === 'script' ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground hover:text-foreground')}>
                    <FileCode2 className="w-3.5 h-3.5 inline mr-1" />脚本代码
                  </button>
                </div>
              )}

              <FormField label="策略名称">
                <input value={name} onChange={(e) => setName(e.target.value)} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" placeholder="输入策略名称" />
              </FormField>

              <div className="grid grid-cols-2 gap-4">
                <FormField label="市场类型">
                  <div className="flex rounded-lg border border-quant-border overflow-hidden">
                    <button onClick={() => setMarket('contract')} className={cn('flex-1 py-2 text-xs transition-colors', market === 'contract' ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground')}>合约</button>
                    <button onClick={() => setMarket('spot')} className={cn('flex-1 py-2 text-xs transition-colors border-l border-quant-border', market === 'spot' ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground')}>现货</button>
                  </div>
                </FormField>
                <FormField label="交易对">
                  <input value={symbol} onChange={(e) => setSymbol(e.target.value.toUpperCase())} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                </FormField>
              </div>

              <FormField label="策略类型">
                <select value={strategyType} onChange={(e) => setStrategyType(e.target.value)} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold">
                  {STRAT_TYPES[market].map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}
                </select>
              </FormField>

              <div className="grid grid-cols-3 gap-4">
                <FormField label="K线周期">
                  <select value={timeframe} onChange={(e) => setTimeframe(e.target.value)} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold">
                    {TIMEFRAMES.map((tf) => <option key={tf} value={tf}>{tf}</option>)}
                  </select>
                </FormField>
                <FormField label="杠杆">
                  <input type="number" min={1} max={125} value={leverage} onChange={(e) => setLeverage(Number(e.target.value))} disabled={market === 'spot'} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold disabled:opacity-40" />
                </FormField>
                <FormField label="首单额度 (USDT)">
                  <input type="number" value={initialCapital} onChange={(e) => setInitialCapital(Number(e.target.value))} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                </FormField>
              </div>

              {market !== 'spot' && (
                <FormField label="交易方向">
                  <div className="flex gap-2">
                    {(['long', 'short', 'dual'] as const).map((d) => (
                      <button key={d} onClick={() => setDirection(d)} className={cn('flex-1 py-2 rounded-lg text-xs border transition-colors', direction === d ? 'bg-quant-green/10 border-quant-green/20 text-quant-green' : 'border-quant-border text-muted-foreground hover:text-foreground')}>
                        {d === 'long' ? '做多' : d === 'short' ? '做空' : '双向'}
                      </button>
                    ))}
                  </div>
                </FormField>
              )}

              {/* ── CRA 核心参数面板 ── */}
              <div className="rounded-xl border border-quant-border bg-quant-bg-tertiary p-4 space-y-4">
                <div className="flex items-center gap-2 text-xs font-semibold text-quant-gold">
                  <SlidersHorizontal className="w-3.5 h-3.5" />
                  CRA 量化参数配置
                </div>

                {/* 做单数量 + 首单仓位 */}
                <div className="grid grid-cols-2 gap-4">
                  <FormField label="做单数量 (5-7单)">
                    <input type="number" min={1} max={20} value={orderCount} onChange={(e) => setOrderCount(Number(e.target.value))} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                  </FormField>
                  <FormField label="首单仓位 (10-10000 USDT)">
                    <input type="number" min={10} max={10000} step={10} value={firstOrderAmount} onChange={(e) => setFirstOrderAmount(Number(e.target.value))} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                  </FormField>
                </div>

                {/* 开仓加倍 + 关闭补仓 */}
                <div className="grid grid-cols-2 gap-4">
                  <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-3">
                    <span className="text-xs text-muted-foreground">开仓加倍（首单x2）</span>
                    <Toggle value={openDouble} onChange={setOpenDouble} />
                  </label>
                  <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-3">
                    <span className="text-xs text-muted-foreground">关闭补仓（仅止盈）</span>
                    <Toggle value={closeAddPosition} onChange={setCloseAddPosition} />
                  </label>
                </div>

                {/* 补仓价差 + 补仓回调 */}
                <div className="grid grid-cols-2 gap-4">
                  <FormField label="补仓价差 (0.5-50%)">
                    <input type="number" min={0.5} max={50} step={0.5} value={addPosSpread} onChange={(e) => setAddPosSpread(Number(e.target.value))} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                  </FormField>
                  <FormField label="补仓回调 (0.01-0.5%)">
                    <input type="number" min={0.01} max={0.5} step={0.01} value={addPosCallback} onChange={(e) => setAddPosCallback(Number(e.target.value))} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                  </FormField>
                </div>

                {/* 止盈比例 + 盈利回调 */}
                <div className="grid grid-cols-2 gap-4">
                  <FormField label="止盈比例 (%)">
                    <input type="number" min={0.1} max={50} step={0.1} value={takeProfitRatio} onChange={(e) => setTakeProfitRatio(Number(e.target.value))} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                  </FormField>
                  <FormField label="盈利回调 (0.01-0.5%)">
                    <input type="number" min={0.01} max={0.5} step={0.01} value={profitCallback} onChange={(e) => setProfitCallback(Number(e.target.value))} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                  </FormField>
                </div>

                {/* 止盈方式 */}
                <FormField label="止盈方式">
                  <div className="grid grid-cols-2 gap-2">
                    {[
                      { key: 'full', label: '全仓止盈', desc: '全仓盈利后卖出' },
                      { key: 'tail', label: '尾单止盈', desc: '最后一单盈利后卖出' },
                      { key: 'head_tail', label: '首尾止盈', desc: '首单+尾单盈利后卖出' },
                      { key: 'moving', label: '移动止盈', desc: '动态分档止盈' },
                    ].map((m) => (
                      <button key={m.key} onClick={() => setTakeProfitMethod(m.key as any)} className={cn('p-3 rounded-lg border text-left transition-colors', takeProfitMethod === m.key ? 'bg-quant-gold/10 border-quant-gold/30' : 'border-quant-border bg-quant-bg hover:border-quant-gold/20')}>
                        <div className="text-xs font-medium">{m.label}</div>
                        <div className="text-[10px] text-muted-foreground mt-0.5">{m.desc}</div>
                      </button>
                    ))}
                  </div>
                </FormField>

                {/* 移动止盈配置 */}
                {takeProfitMethod === 'moving' && (
                  <div className="grid grid-cols-3 gap-3">
                    <FormField label="止盈比例 (%)">
                      <input type="number" min={0.1} max={10} step={0.1} value={movingTP.tier1_ratio} onChange={(e) => setMovingTP({ ...movingTP, tier1_ratio: Number(e.target.value) })} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                    </FormField>
                    <FormField label="第一档回撤 (%)">
                      <input type="number" min={5} max={100} value={movingTP.tier1_drawback} onChange={(e) => setMovingTP({ ...movingTP, tier1_drawback: Number(e.target.value) })} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                    </FormField>
                    <FormField label="第二档回撤 (%)">
                      <input type="number" min={5} max={100} value={movingTP.tier2_drawback} onChange={(e) => setMovingTP({ ...movingTP, tier2_drawback: Number(e.target.value) })} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                    </FormField>
                    <div className="col-span-3 text-[10px] text-muted-foreground">
                      计算公式: {movingTP.tier1_ratio}% ± ({movingTP.tier1_ratio}% × {movingTP.tier1_drawback}%)，移动止盈开启后分仓/首尾止盈失效
                    </div>
                  </div>
                )}

                {/* 开仓指标 */}
                <FormField label="开仓指标策略">
                  <select value={openIndicator} onChange={(e) => setOpenIndicator(e.target.value as any)} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold">
                    <option value="macd_golden">MACD金叉开多</option>
                    <option value="macd_death">MACD死叉开空</option>
                    <option value="ema">EMA拐点开仓</option>
                    <option value="close">关闭（无脑买入）</option>
                  </select>
                </FormField>

                {/* 补仓指标 */}
                <FormField label="补仓指标（EMA和MACD补仓）">
                  <select value={addPosIndicator} onChange={(e) => setAddPosIndicator(e.target.value as any)} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold">
                    <option value="macd">MACD金叉/死叉补仓</option>
                    <option value="ema">EMA4上下拐点补仓</option>
                    <option value="close">关闭（仅按跌幅补仓）</option>
                  </select>
                  <p className="text-[10px] text-muted-foreground mt-1">开启后需同时满足跌幅条件和指标条件才补仓，大行情时非常抗跌</p>
                </FormField>

                {/* 趋势指标 EMA4 */}
                <div className="space-y-3">
                  <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-3">
                    <div>
                      <div className="text-xs font-medium">趋势指标 (EMA4)</div>
                      <div className="text-[10px] text-muted-foreground">监控EMA指数平滑移动平均线</div>
                    </div>
                    <Toggle value={trendIndicator} onChange={setTrendIndicator} />
                  </label>
                  {trendIndicator && (
                    <FormField label="EMA4 时间周期">
                      <div className="flex gap-2">
                        {(['5m', '15m', '30m', '60m'] as const).map((tf) => (
                          <button key={tf} onClick={() => setTrendTimeframe(tf)} className={cn('flex-1 py-2 rounded-lg text-xs border transition-colors', trendTimeframe === tf ? 'bg-quant-gold/10 border-quant-gold/20 text-quant-gold' : 'border-quant-border text-muted-foreground hover:text-foreground')}>
                            {tf}
                          </button>
                        ))}
                      </div>
                      <p className="text-[10px] text-muted-foreground mt-1">时间越长准确性越高，但也越容易错过行情</p>
                    </FormField>
                  )}
                </div>

                {/* 防瀑布 */}
                <FormField label="防瀑布保护 (分钟内最大涨跌%)">
                  <input type="number" min={0.5} max={20} step={0.5} value={waterfallProtection} onChange={(e) => setWaterfallProtection(Number(e.target.value))} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                  <p className="text-[10px] text-muted-foreground mt-1">1分钟内单一币种涨跌超过设定值自动暂停补仓，默认2%</p>
                </FormField>

                {/* 反向止盈/止损 */}
                <div className="grid grid-cols-2 gap-4">
                  <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-3">
                    <div>
                      <div className="text-xs font-medium">反向止盈</div>
                      <div className="text-[10px] text-muted-foreground">MACD反向信号清仓</div>
                    </div>
                    <Toggle value={reverseTP} onChange={setReverseTP} />
                  </label>
                  <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-3">
                    <div>
                      <div className="text-xs font-medium">反向止损</div>
                      <div className="text-[10px] text-muted-foreground">MACD判断错误直接止损</div>
                    </div>
                    <Toggle value={reverseSL} onChange={setReverseSL} />
                  </label>
                </div>

                {/* 顺势而为 */}
                <div className="space-y-3">
                  <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-3">
                    <div>
                      <div className="text-xs font-medium">顺势而为</div>
                      <div className="text-[10px] text-muted-foreground">逆势补仓后顺势单倍投，最高5倍</div>
                    </div>
                    <Toggle value={followTrend} onChange={setFollowTrend} />
                  </label>
                  {followTrend && (
                    <FormField label="顺势最大倍数 (逆势单补仓次数+首单，最高5倍)">
                      <input type="number" min={1} max={5} value={followTrendMax} onChange={(e) => setFollowTrendMax(Number(e.target.value))} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                    </FormField>
                  )}
                </div>

                {/* 斩仓和燃烧 */}
                <div className="space-y-3">
                  <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-3">
                    <div>
                      <div className="text-xs font-medium">斩仓和燃烧</div>
                      <div className="text-[10px] text-muted-foreground">用顺势单盈利消耗逆势单浮亏</div>
                    </div>
                    <Toggle value={burnCut.enabled} onChange={(v) => setBurnCut({ ...burnCut, enabled: v })} />
                  </label>
                  {burnCut.enabled && (
                    <div className="grid grid-cols-2 gap-4">
                      <FormField label="双向燃烧起始仓">
                        <input type="number" min={1} max={10} value={burnCut.dual_burn_start} onChange={(e) => setBurnCut({ ...burnCut, dual_burn_start: Number(e.target.value) })} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                        <p className="text-[10px] text-muted-foreground">默认第3仓启动</p>
                      </FormField>
                      <FormField label="全局燃烧起始仓">
                        <input type="number" min={1} max={10} value={burnCut.global_burn_start} onChange={(e) => setBurnCut({ ...burnCut, global_burn_start: Number(e.target.value) })} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus-quant-gold" />
                        <p className="text-[10px] text-muted-foreground">默认第5仓启动跨币种燃烧</p>
                      </FormField>
                    </div>
                  )}
                </div>

                {/* 止损设置 */}
                <div className="space-y-3">
                  <div className="text-xs font-medium text-muted-foreground">止损设置（三选一）</div>
                  <div className="grid grid-cols-3 gap-3">
                    <FormField label="止损比例 (%)">
                      <input type="number" min={0} max={100} step={0.1} value={stopLossRatio} onChange={(e) => setStopLossRatio(Number(e.target.value))} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                    </FormField>
                    <FormField label="止损金额 (USDT)">
                      <input type="number" min={0} value={stopLossAmount} onChange={(e) => setStopLossAmount(Number(e.target.value))} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                    </FormField>
                    <FormField label="止损价格">
                      <input type="number" min={0} value={stopLossPrice} onChange={(e) => setStopLossPrice(Number(e.target.value))} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                    </FormField>
                  </div>
                </div>

                {/* 首单挂单价格 */}
                <FormField label="首单挂单价格 (0=实时市价)">
                  <input type="number" min={0} value={firstOrderPrice} onChange={(e) => setFirstOrderPrice(Number(e.target.value))} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                  <p className="text-[10px] text-muted-foreground mt-1">输入固定价格后，只有价格低于设定值系统才会买入</p>
                </FormField>

                {/* 限制在线单量 + 盈利保护 + 自定义减仓 */}
                <div className="grid grid-cols-3 gap-3">
                  <FormField label="限制在线单量">
                    <input type="number" min={1} max={50} value={onlineOrderLimit} onChange={(e) => setOnlineOrderLimit(Number(e.target.value))} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                  </FormField>
                  <label className="flex flex-col justify-center rounded-lg border border-quant-border bg-quant-bg p-3">
                    <span className="text-xs text-muted-foreground mb-1">盈利保护</span>
                    <Toggle value={profitProtection} onChange={setProfitProtection} />
                  </label>
                  <label className="flex flex-col justify-center rounded-lg border border-quant-border bg-quant-bg p-3">
                    <span className="text-xs text-muted-foreground mb-1">自定义减仓</span>
                    <Toggle value={customReduce} onChange={setCustomReduce} />
                  </label>
                </div>

                {/* 振幅设置 */}
                <div className="space-y-2">
                  <div className="text-xs font-medium text-muted-foreground">振幅设置（各周期建议值）</div>
                  <div className="grid grid-cols-4 gap-3">
                    {([
                      { key: '5m', label: '5分钟', suggest: 2 },
                      { key: '15m', label: '15分钟', suggest: 4 },
                      { key: '30m', label: '30分钟', suggest: 7 },
                      { key: '1h', label: '1小时', suggest: 10 },
                    ] as const).map((a) => (
                      <FormField key={a.key} label={a.label}>
                        <input type="number" min={0.1} max={50} step={0.1} value={amplitude[a.key]} onChange={(e) => setAmplitude({ ...amplitude, [a.key]: Number(e.target.value) })} className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                        <p className="text-[10px] text-muted-foreground">建议{a.suggest}%</p>
                      </FormField>
                    ))}
                  </div>
                </div>

                {/* 交易次数 */}
                <FormField label="交易次数">
                  <div className="flex gap-2">
                    {([
                      { key: 'single', label: '单次循环', desc: '止盈后不再买入，补仓继续' },
                      { key: 'cycle', label: '策略循环', desc: '卖出后持续买入直到次数用尽' },
                    ] as const).map((m) => (
                      <button key={m.key} onClick={() => setTradeCountMode(m.key)} className={cn('flex-1 p-3 rounded-lg border text-left transition-colors', tradeCountMode === m.key ? 'bg-quant-gold/10 border-quant-gold/30' : 'border-quant-border bg-quant-bg hover:border-quant-gold/20')}>
                        <div className="text-xs font-medium">{m.label}</div>
                        <div className="text-[10px] text-muted-foreground mt-0.5">{m.desc}</div>
                      </button>
                    ))}
                  </div>
                </FormField>
              </div>
            </>
          )}

          {mode === 'script' && step === 1 && (
            <FormField label="策略代码 (Python)">
              <textarea
                defaultValue={editing?.strategy_code || DEFAULT_CODE}
                className="w-full h-64 bg-quant-bg border border-quant-border rounded-lg p-3 font-mono text-[11px] leading-relaxed resize-none focus:outline-none focus:border-quant-gold"
                spellCheck={false}
              />
            </FormField>
          )}

          {step === (mode === 'script' ? 2 : 1) && (
            <>
              <div className="rounded-xl border border-quant-border bg-quant-bg-tertiary p-4">
                <div className="text-xs font-semibold mb-3">执行模式</div>
                <div className="grid grid-cols-2 gap-3">
                  <button onClick={() => setExecutionMode('live')} className={cn('flex items-start gap-3 p-4 rounded-xl border transition-all text-left', executionMode === 'live' ? 'border-quant-gold bg-quant-gold/5' : 'border-quant-border hover:border-quant-gold/30')}>
                    <div className={cn('w-10 h-10 rounded-lg flex items-center justify-center shrink-0', executionMode === 'live' ? 'bg-quant-gold/10 text-quant-gold' : 'bg-quant-bg text-muted-foreground')}><Zap className="w-5 h-5" /></div>
                    <div>
                      <div className="text-xs font-semibold">实盘交易</div>
                      <div className="text-[10px] text-muted-foreground mt-1">连接交易所API自动执行买卖</div>
                    </div>
                    {executionMode === 'live' && <CheckCircle2 className="w-4 h-4 text-quant-gold ml-auto shrink-0" />}
                  </button>
                  <button onClick={() => setExecutionMode('signal')} className={cn('flex items-start gap-3 p-4 rounded-xl border transition-all text-left', executionMode === 'signal' ? 'border-quant-gold bg-quant-gold/5' : 'border-quant-border hover:border-quant-gold/30')}>
                    <div className={cn('w-10 h-10 rounded-lg flex items-center justify-center shrink-0', executionMode === 'signal' ? 'bg-quant-gold/10 text-quant-gold' : 'bg-quant-bg text-muted-foreground')}><Activity className="w-5 h-5" /></div>
                    <div>
                      <div className="text-xs font-semibold">信号通知</div>
                      <div className="text-[10px] text-muted-foreground mt-1">仅发送交易信号，不自动下单</div>
                    </div>
                    {executionMode === 'signal' && <CheckCircle2 className="w-4 h-4 text-quant-gold ml-auto shrink-0" />}
                  </button>
                </div>
              </div>

              <div className="rounded-xl border border-quant-border bg-quant-bg-tertiary p-4">
                <div className="text-xs font-semibold mb-3">通知渠道</div>
                <div className="grid grid-cols-3 gap-2">
                  {[
                    { key: 'browser', label: '浏览器' },
                    { key: 'email', label: '邮件' },
                    { key: 'telegram', label: 'Telegram' },
                    { key: 'discord', label: 'Discord' },
                    { key: 'webhook', label: 'Webhook' },
                    { key: 'phone', label: '短信' },
                  ].map((ch) => (
                    <label key={ch.key} className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer hover:text-foreground transition-colors">
                      <input
                        type="checkbox"
                        checked={notifyChannels.includes(ch.key)}
                        onChange={(e) => setNotifyChannels((prev) => e.target.checked ? [...prev, ch.key] : prev.filter((c) => c !== ch.key))}
                        className="rounded border-quant-border"
                      />
                      {ch.label}
                    </label>
                  ))}
                </div>
              </div>
            </>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 px-6 py-4 border-t border-quant-border shrink-0">
          <button onClick={onClose} className="px-4 py-2 rounded-lg border border-quant-border text-xs hover:bg-quant-hover transition-colors">取消</button>
          {step > 0 && <button onClick={() => setStep(step - 1)} className="px-4 py-2 rounded-lg border border-quant-border text-xs hover:bg-quant-hover transition-colors">上一步</button>}
          {step < steps.length - 1 ? (
            <button onClick={() => setStep(step + 1)} className="px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity">下一步</button>
          ) : (
            <button onClick={handleSubmit} className="px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity">
              {editing ? '保存修改' : '创建策略'}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

function FormField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="text-[11px] text-muted-foreground mb-1.5 block">{label}</label>
      {children}
    </div>
  )
}

/* ═══════════════════════════════════════════════════════════════
   Code Editor Tab
   ═══════════════════════════════════════════════════════════════ */
function CodeEditorTab() {
  const [code, setCode] = useState(DEFAULT_CODE)
  const [aiPrompt, setAiPrompt] = useState('')
  const [aiResponse, setAiResponse] = useState('')
  const [generating, setGenerating] = useState(false)

  const handleGenerate = async () => {
    if (!aiPrompt.trim()) return
    setGenerating(true)
    try {
      const res = await aiApi.generate({ prompt: aiPrompt, type: 'strategy' })
      setAiResponse(res?.code || res?.data?.code || 'AI 建议将显示在这里...')
    } catch {
      setAiResponse('生成失败，请稍后重试')
    } finally {
      setGenerating(false)
    }
  }

  return (
    <div className="h-full flex">
      {/* Editor */}
      <div className="flex-1 flex flex-col min-w-0">
        <div className="flex items-center justify-between px-4 py-2 border-b border-quant-border bg-quant-bg-secondary">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <FileCode2 className="w-3.5 h-3.5" />
            Python 策略代码
          </div>
          <div className="flex gap-2">
            <button onClick={() => setCode(DEFAULT_CODE)} className="px-2.5 py-1.5 rounded-md bg-quant-bg border border-quant-border text-[10px] hover:bg-quant-hover transition-colors flex items-center gap-1">
              <RotateCcw className="w-3 h-3" /> 重置
            </button>
            <button className="px-2.5 py-1.5 rounded-md bg-quant-gold/10 border border-quant-gold/20 text-quant-gold text-[10px] hover:bg-quant-gold/20 transition-colors flex items-center gap-1">
              <Save className="w-3 h-3" /> 保存
            </button>
          </div>
        </div>
        <textarea
          value={code}
          onChange={(e) => setCode(e.target.value)}
          className="flex-1 bg-quant-bg p-4 font-mono text-[11px] leading-relaxed resize-none focus:outline-none border-none"
          spellCheck={false}
        />
      </div>

      {/* AI Sidebar */}
      <div className="hidden md:flex w-80 shrink-0 border-l border-quant-border bg-quant-bg-secondary flex-col">
        <div className="px-4 py-3 border-b border-quant-border text-xs font-semibold flex items-center gap-2">
          <BrainCircuit className="w-4 h-4 text-quant-gold" /> AI 策略助手
        </div>
        <div className="p-3 space-y-3">
          <textarea
            value={aiPrompt}
            onChange={(e) => setAiPrompt(e.target.value)}
            placeholder="描述你的交易思路，AI 将生成策略代码..."
            className="w-full h-24 bg-quant-bg border border-quant-border rounded-lg p-3 text-xs resize-none focus:outline-none focus:border-quant-gold"
          />
          <button onClick={handleGenerate} disabled={generating} className="w-full py-2 bg-quant-gold text-white rounded-lg text-xs font-medium hover:opacity-90 disabled:opacity-50 transition-opacity">
            {generating ? '生成中...' : '生成策略'}
          </button>
        </div>
        <div className="flex-1 overflow-y-auto px-3 pb-3">
          <div className="rounded-lg border border-quant-border bg-quant-card p-3 text-xs text-muted-foreground whitespace-pre-wrap">
            {aiResponse || 'AI 建议将显示在这里...'}
          </div>
        </div>
      </div>
    </div>
  )
}

/* ═══════════════════════════════════════════════════════════════
   ML Strategy Tab
   ═══════════════════════════════════════════════════════════════ */
function MLStrategyTab() {
  const [strategyName, setStrategyName] = useState('ML_Strategy')
  const [mlModels, setMlModels] = useState<any[]>([])
  const [selectedMlModel, setSelectedMlModel] = useState('')
  const [mlSymbol, setMlSymbol] = useState('BTCUSDT')
  const [mlMinConfidence, setMlMinConfidence] = useState(0.3)
  const [mlDeploying, setMlDeploying] = useState(false)

  useEffect(() => {
    mlApi.list().then(d => {
      setMlModels(d?.models || [])
      if (d?.models?.length > 0 && !selectedMlModel) setSelectedMlModel(d.models[0].model_id)
    }).catch(() => {})
  }, [])

  const handleDeployMl = async () => {
    if (!selectedMlModel) return
    setMlDeploying(true)
    try {
      await mlApi.deploy({ model_id: selectedMlModel, symbol: mlSymbol, min_confidence: mlMinConfidence })
      alert(`ML 策略已部署: ${selectedMlModel} → ${mlSymbol}`)
    } catch (e: any) { alert('部署失败: ' + (e?.message || e)) }
    finally { setMlDeploying(false) }
  }

  return (
    <div className="h-full overflow-y-auto p-6 space-y-5 max-w-4xl mx-auto">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <input value={strategyName} onChange={(e) => setStrategyName(e.target.value)} className="bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-sm w-56 focus:outline-none focus:border-quant-gold" />
          <span className="px-2 py-1 bg-blue-500/10 text-blue-400 rounded text-[10px] font-medium border border-blue-500/20">ML 策略</span>
        </div>
        <button onClick={handleDeployMl} disabled={mlDeploying || !selectedMlModel}
          className={cn('px-4 py-2 rounded-lg text-xs font-medium transition-opacity',
            !selectedMlModel ? 'bg-quant-bg-tertiary text-muted-foreground cursor-not-allowed' :
            mlDeploying ? 'bg-quant-gold/50 text-white cursor-wait' : 'bg-quant-green text-white hover:opacity-90')}>
          {mlDeploying ? '部署中...' : '部署 ML 策略'}
        </button>
      </div>

      <SectionCard title="模型配置">
        <div className="space-y-4">
          <FormField label="选择已训练模型">
            {mlModels.length > 0 ? (
              <select value={selectedMlModel} onChange={e => setSelectedMlModel(e.target.value)}
                className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold">
                {mlModels.map((m: any) => (
                  <option key={m.model_id} value={m.model_id}>{m.model_id} ({m.model_type} · {m.task_type})</option>
                ))}
              </select>
            ) : (
              <div className="text-xs text-muted-foreground p-3 bg-quant-bg-secondary rounded-lg">
                暂无已训练模型。请到 <b>AI 研究 → ML 预测 → 训练</b> 先训练一个模型。
              </div>
            )}
          </FormField>
          <div className="grid grid-cols-2 gap-4">
            <FormField label="交易对">
              <input value={mlSymbol} onChange={e => setMlSymbol(e.target.value.toUpperCase())}
                className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
            </FormField>
            <FormField label="最小置信度 (0~1)">
              <input type="number" step={0.05} min={0} max={1} value={mlMinConfidence}
                onChange={e => setMlMinConfidence(Number(e.target.value))}
                className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
            </FormField>
          </div>
        </div>
      </SectionCard>

      <SectionCard title="策略说明">
        <div className="space-y-3 text-xs text-muted-foreground">
          <div className="flex items-start gap-2">
            <BrainCircuit className="h-4 w-4 text-blue-400 mt-0.5 shrink-0" />
            <div>
              <p className="font-medium text-foreground mb-1">自动预测交易</p>
              <p>模型在每个 K 线上预测价格方向，超过置信度阈值时自动开仓，信号反转时平仓，自带 5% 止损。</p>
            </div>
          </div>
          <div className="flex items-start gap-2">
            <RotateCcw className="h-4 w-4 text-quant-gold mt-0.5 shrink-0" />
            <div>
              <p className="font-medium text-foreground mb-1">在线学习</p>
              <p>策略每 24 小时自动用最新数据重训练模型，持续适应市场变化。</p>
            </div>
          </div>
          <div className="flex items-start gap-2">
            <Zap className="h-4 w-4 text-quant-green mt-0.5 shrink-0" />
            <div>
              <p className="font-medium text-foreground mb-1">即插即用</p>
              <p>无需编写代码。在 AI 研究页面训练模型后，在此一键部署为实盘策略。</p>
            </div>
          </div>
        </div>
      </SectionCard>
    </div>
  )
}

/* ═══════════════════════════════════════════════════════════════
   QuantDinger AI Tab
   ═══════════════════════════════════════════════════════════════ */
function QuantDingerAITab() {
  const [model, setModel] = useState('gpt-4o')
  const [voting, setVoting] = useState(false)
  const [confidence, setConfidence] = useState(78)
  const [messages, setMessages] = useState<ChatMessage[]>([
    { role: 'bot', content: '你好！描述你的交易思路，我来帮你生成量化丁格策略。\n\n例如："做一个BTC的网格策略，震荡区间30000-40000"' },
  ])
  const [input, setInput] = useState('')
  const [codeWorkspace, setCodeWorkspace] = useState(`class GridStrategy:\n    def on_init(self, ctx):\n        ctx.param('upper', 40000)\n        ctx.param('lower', 30000)\n        ctx.param('grids', 20)\n\n    def on_bar(self, ctx, bar):\n        price = bar.close\n        step = (self.upper - self.lower) / (self.grids - 1)\n        level = int((price - self.lower) / step)\n        # ...`)
  const [generating, setGenerating] = useState(false)
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => { scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' }) }, [messages])

  const handleSend = async () => {
    if (!input.trim()) return
    const userMsg: ChatMessage = { role: 'user', content: input }
    setMessages((prev) => [...prev, userMsg])
    setInput('')
    setGenerating(true)
    try {
      const res = await aiApi.chat(input)
      const botMsg: ChatMessage = { role: 'bot', content: res?.reply || res?.data?.reply || '正在分析您的交易思路...', meta: { model, confidence } }
      setMessages((prev) => [...prev, botMsg])
    } catch {
      setMessages((prev) => [...prev, { role: 'bot', content: '服务暂时不可用，请稍后重试。' }])
    } finally {
      setGenerating(false)
    }
  }

  return (
    <div className="h-full flex flex-col">
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-3 border-b border-quant-border bg-quant-bg-secondary shrink-0 flex-wrap gap-3">
        <div className="flex items-center gap-2">
          <BrainCircuit className="w-4 h-4 text-quant-gold" />
          <span className="font-semibold text-sm">QuantDinger AI · 策略生成器</span>
        </div>
        <div className="flex items-center gap-3">
          <select value={model} onChange={(e) => setModel(e.target.value)} className="bg-quant-bg border border-quant-border rounded-lg px-2 py-1.5 text-xs focus:outline-none focus:border-quant-gold">
            {MODELS.map((m) => <option key={m.id} value={m.id}>{m.name}</option>)}
          </select>
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer">
            <input type="checkbox" checked={voting} onChange={(e) => setVoting(e.target.checked)} className="rounded border-quant-border" />
            <Vote className="w-3 h-3" /> 多模型投票
          </label>
          <div className="flex items-center gap-2">
            <Gauge className="w-3.5 h-3.5 text-muted-foreground" />
            <span className="text-xs text-muted-foreground">置信度</span>
            <input type="range" min={50} max={100} value={confidence} onChange={(e) => setConfidence(Number(e.target.value))} className="w-20 accent-quant-gold" />
            <span className="text-xs font-mono w-8">{confidence}%</span>
          </div>
        </div>
      </div>

      {/* Main */}
      <div className="flex-1 flex min-h-0">
        {/* Chat */}
        <div className="flex-1 flex flex-col min-w-0 border-r border-quant-border">
          <div ref={scrollRef} className="flex-1 overflow-y-auto p-6 space-y-4">
            {messages.map((msg, i) => (
              <div key={i} className={cn('flex gap-3', msg.role === 'user' ? 'justify-end' : '')}>
                {msg.role === 'bot' && (
                  <div className="w-8 h-8 rounded-full bg-quant-gold/20 flex items-center justify-center text-sm shrink-0">🤖</div>
                )}
                <div className={cn('max-w-[75%] rounded-xl px-4 py-3 text-xs leading-relaxed whitespace-pre-wrap border', msg.role === 'user' ? 'bg-quant-gold/10 border-quant-gold/20 text-foreground' : 'bg-quant-card border-quant-border')}>
                  {msg.content}
                  {msg.meta && (
                    <div className="mt-2 pt-2 border-t border-quant-border/50 flex items-center gap-2 text-[10px] text-muted-foreground">
                      <Cpu className="w-3 h-3" /> {msg.meta.model} · 置信度 {msg.meta.confidence}%
                    </div>
                  )}
                </div>
              </div>
            ))}
            {generating && (
              <div className="flex gap-3">
                <div className="w-8 h-8 rounded-full bg-quant-gold/20 flex items-center justify-center text-sm shrink-0">🤖</div>
                <div className="bg-quant-card border border-quant-border rounded-xl px-4 py-3 text-xs text-muted-foreground">思考中...</div>
              </div>
            )}
          </div>
          <div className="p-4 border-t border-quant-border">
            <div className="flex gap-2">
              <input
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && handleSend()}
                placeholder="描述你的交易思路..."
                className="flex-1 bg-quant-bg border border-quant-border rounded-lg px-4 py-2.5 text-sm focus:outline-none focus:border-quant-gold"
              />
              <button onClick={handleSend} disabled={generating} className="px-4 py-2.5 bg-quant-gold text-white rounded-lg hover:opacity-90 disabled:opacity-50 transition-opacity">
                <Send className="w-4 h-4" />
              </button>
            </div>
          </div>
        </div>

        {/* Workspace */}
        <div className="w-96 shrink-0 bg-quant-bg-secondary flex flex-col gap-4 overflow-y-auto p-4">
          <SectionCard title="策略概览" className="shrink-0">
            <div className="space-y-2 text-xs text-muted-foreground">
              <div className="flex justify-between"><span>策略类型</span><span className="text-foreground">网格策略</span></div>
              <div className="flex justify-between"><span>预期胜率</span><span className="text-quant-green">58.2%</span></div>
              <div className="flex justify-between"><span>最大回撤</span><span className="text-quant-red">-12.4%</span></div>
              <div className="flex justify-between"><span>盈亏比</span><span className="text-foreground">2.1:1</span></div>
            </div>
          </SectionCard>

          <SectionCard title="代码工作区" className="flex-1 flex flex-col min-h-0">
            <div className="flex items-center justify-between mb-2">
              <span className="text-[10px] text-muted-foreground">Python</span>
              <div className="flex gap-1">
                <button onClick={() => navigator.clipboard.writeText(codeWorkspace)} className="p-1 rounded hover:bg-quant-hover text-muted-foreground" title="复制"><Copy className="w-3 h-3" /></button>
                <button onClick={() => setCodeWorkspace('')} className="p-1 rounded hover:bg-quant-hover text-muted-foreground" title="清空"><Trash2 className="w-3 h-3" /></button>
              </div>
            </div>
            <textarea
              value={codeWorkspace}
              onChange={(e) => setCodeWorkspace(e.target.value)}
              className="flex-1 min-h-[200px] bg-quant-bg border border-quant-border rounded-lg p-3 font-mono text-[11px] leading-relaxed resize-none focus:outline-none focus:border-quant-gold"
              spellCheck={false}
            />
          </SectionCard>

          <div className="flex gap-2 shrink-0">
            <button className="flex-1 py-2.5 bg-quant-gold/10 text-quant-gold border border-quant-gold/20 rounded-lg text-xs font-medium hover:bg-quant-gold/20 flex items-center justify-center gap-1.5 transition-colors">
              <BarChart3 className="w-3.5 h-3.5" /> 回测
            </button>
            <button className="flex-1 py-2.5 bg-quant-green text-white rounded-lg text-xs font-medium hover:opacity-90 flex items-center justify-center gap-1.5 transition-opacity">
              <Play className="w-3.5 h-3.5" /> 部署
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

/* ═══════════════════════════════════════════════════════════════
   Shared UI Components
   ═══════════════════════════════════════════════════════════════ */
function Toggle({ value, onChange }: { value: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      onClick={() => onChange(!value)}
      className={cn('w-10 h-5 rounded-full relative transition-colors', value ? 'bg-quant-gold' : 'bg-quant-border')}
    >
      <span className={cn('absolute top-0.5 w-4 h-4 rounded-full bg-white transition-transform', value ? 'left-5' : 'left-0.5')} />
    </button>
  )
}
