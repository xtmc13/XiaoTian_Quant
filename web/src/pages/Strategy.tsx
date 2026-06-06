import { useState, useEffect, useRef, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { strategyApi, aiApi, mlApi } from '@/lib/api'
import { cn, formatCurrency, formatPercent } from '@/lib/utils'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import type { MLModelInfo } from '@/types'
import {
  Play, Pause, Save, FlaskConical, Code, Bot, MessageSquare,
  LayoutDashboard, FolderOpen, Search, Plus, ChevronRight, ChevronDown,
  MoreVertical, Trash2, Edit3, Activity, Wallet, TrendingUp, TrendingDown,
  Send, Cpu, Sparkles, BarChart3, X, CheckCircle2, AlertTriangle,
  Clock, DollarSign, Zap, ArrowRight, Settings2, Layers, GripVertical,
  FileCode2, Terminal, Copy, RotateCcw, Eye, EyeOff, SlidersHorizontal,
  BrainCircuit, Vote, Gauge
} from 'lucide-react'
import { useStrategyData } from '@/hooks/useStrategyData'
import { StrategyList, StatusBadge, getStatusDot } from '@/components/strategy/StrategyList'
import { StrategyDetailPanel } from '@/components/strategy/StrategyDetailPanel'
import { StrategyCreateModal } from '@/components/strategy/StrategyCreateModal'
import { StrategyEditor } from '@/components/strategy/StrategyEditor'
import { FormField } from '@/components/strategy/StrategyFormFields'
import type { StrategyItem } from '@/types'

/* ─── Types ─── */
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

const MODELS = [
  { id: 'gpt-4o', name: 'GPT-4o', color: 'text-green-400' },
  { id: 'claude-3.5', name: 'Claude-3.5', color: 'text-orange-400' },
  { id: 'deepseek', name: 'DeepSeek', color: 'text-blue-400' },
  { id: 'gemini', name: 'Gemini', color: 'text-purple-400' },
  { id: 'grok', name: 'Grok', color: 'text-red-400' },
]

/* ─── Helpers ─── */
function useLocalStorage<T>(key: string, initial: T): [T, (v: T) => void] {
  const [val, setVal] = useState<T>(() => {
    try { return JSON.parse(localStorage.getItem(key) || 'null') ?? initial } catch { return initial }
  })
  useEffect(() => { localStorage.setItem(key, JSON.stringify(val)) }, [key, val])
  return [val, setVal]
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
        {tab === 'code' && <StrategyEditor />}
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
        <button onClick={onDismiss} aria-label="关闭提示" className="w-8 h-8 rounded-lg border border-quant-border flex items-center justify-center text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors">
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
  const list = (strategies || []) as StrategyItem[]
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

function StatCard({ icon: Icon, label, value, color }: { icon: React.ComponentType<{ className?: string }>; label: string; value: string; color?: string }) {
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
  const { strategies, isLoading, start, stop, delete: del } = useStrategyData()
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [editingStrategy, setEditingStrategy] = useState<StrategyItem | null>(null)

  const selected = strategies.find((s) => s.id === selectedId) || null

  return (
    <div className="h-full flex">
      <StrategyList
        strategies={strategies}
        isLoading={isLoading}
        selectedId={selectedId}
        onSelect={setSelectedId}
        onStart={start}
        onStop={stop}
        onEdit={(s) => { setEditingStrategy(s); setShowCreate(true) }}
        onDelete={del}
        onCreate={() => { setEditingStrategy(null); setShowCreate(true) }}
      />

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
          <StrategyDetailPanel
            strategy={selected}
            onStart={() => start(selected.id)}
            onStop={() => stop(selected.id)}
            onEdit={() => { setEditingStrategy(selected); setShowCreate(true) }}
            onDelete={() => { if (confirm(`删除策略 "${selected.name}"？`)) del(selected.id) }}
          />
        )}
      </div>

      {showCreate && (
        <StrategyCreateModal
          editing={editingStrategy}
          onClose={() => { setShowCreate(false); setEditingStrategy(null) }}
          onSaved={() => { setShowCreate(false); setEditingStrategy(null) }}
        />
      )}
    </div>
  )
}

/* ═══════════════════════════════════════════════════════════════
   ML Strategy Tab
   ═══════════════════════════════════════════════════════════════ */
function MLStrategyTab() {
  const [strategyName, setStrategyName] = useState('ML_Strategy')
  const [mlModels, setMlModels] = useState<MLModelInfo[]>([])
  const [selectedMlModel, setSelectedMlModel] = useState('')
  const [mlSymbol, setMlSymbol] = useState('BTCUSDT')
  const [mlMinConfidence, setMlMinConfidence] = useState(0.3)
  const [mlDeploying, setMlDeploying] = useState(false)

  useEffect(() => {
    mlApi.list().then(models => {
      setMlModels(models || [])
      if (models?.length > 0 && !selectedMlModel) setSelectedMlModel(models[0].model_id)
    }).catch(() => {})
  }, [])

  const handleDeployMl = async () => {
    if (!selectedMlModel) return
    setMlDeploying(true)
    try {
      await mlApi.deploy({ model_id: selectedMlModel, symbol: mlSymbol, min_confidence: mlMinConfidence })
      alert(`ML 策略已部署: ${selectedMlModel} → ${mlSymbol}`)
    } catch (e: unknown) { alert('部署失败: ' + (e instanceof Error ? e.message : String(e))) }
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
                {mlModels.map((m: MLModelInfo) => (
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
      const botMsg: ChatMessage = { role: 'bot', content: res?.reply || '正在分析您的交易思路...', meta: { model, confidence } }
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
