import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { triangularApi, configApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useToastStore } from '@/stores/toastStore'
import { SectionCard } from '@/components/ui/SectionCard'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import { EmptyState } from '@/components/ui/EmptyState'
import type {
  TriangularConfig,
  TriangularOpportunity,
  TriangularTrade,
} from '@/types'
import {
  Triangle,
  Play,
  Square,
  RefreshCw,
  DollarSign,
  Activity,
  Globe,
  Zap,
  CheckCircle2,
  AlertCircle,
  Clock,
  Target,
  Layers,
  ChevronUp,
  Save,
  Plus,
} from 'lucide-react'

/* ── Local UI primitives ── */

function Toggle({ value, onChange, disabled }: { value: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <button
      type="button"
      onClick={() => !disabled && onChange(!value)}
      disabled={disabled}
      className={cn(
        'relative h-5 w-10 rounded-full transition-colors',
        value ? 'bg-quant-gold' : 'bg-quant-border',
        disabled && 'opacity-50'
      )}
      role="switch"
      aria-checked={value}
    >
      <span
        className={cn(
          'absolute top-0.5 h-4 w-4 rounded-full bg-white transition-transform',
          value ? 'left-5' : 'left-0.5'
        )}
      />
    </button>
  )
}

function TextInput({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  return (
    <input
      type="text"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white placeholder-muted-foreground outline-none transition-colors focus:border-quant-gold"
    />
  )
}

function NumberInput({
  value,
  onChange,
  placeholder,
  min,
  max,
  step,
}: {
  value: number
  onChange: (v: number) => void
  placeholder?: string
  min?: number
  max?: number
  step?: number
}) {
  return (
    <input
      type="number"
      min={min}
      max={max}
      step={step}
      value={value}
      onChange={(e) => onChange(Number(e.target.value))}
      placeholder={placeholder}
      className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white placeholder-muted-foreground outline-none transition-colors focus:border-quant-gold"
    />
  )
}

/* ── Constants ── */

const SUPPORTED_EXCHANGES = [
  { key: 'binance', label: 'Binance' },
  { key: 'okx', label: 'OKX' },
  { key: 'mexc', label: 'MEXC' },
  { key: 'gateio', label: 'Gate.io' },
  { key: 'bybit', label: 'Bybit' },
  { key: 'coinbase', label: 'Coinbase' },
  { key: 'kraken', label: 'Kraken' },
  { key: 'bitget', label: 'Bitget' },
] as const

const DEFAULT_TRIANGULAR_CONFIG: TriangularConfig = {
  exchange: 'binance',
  symbols: ['BTCUSDT', 'ETHUSDT', 'ETHBTC'],
  quote_asset: 'USDT',
  min_profit_pct: 0.3,
  order_size: 500,
  max_positions: 2,
  fee_rate: 0.001,
  auto_execute: false,
  dry_run: true,
  adaptive_qty_enabled: false,
  max_slippage_pct: 0.5,
  min_order_qty: 0.001,
  execution_mode: 'sequential',
  max_execution_ms: 5000,
}

/* ── Panel ── */

export function TriangularArbitragePanel() {
  const queryClient = useQueryClient()
  const [showHistory, setShowHistory] = useState(false)
  const [showConfig, setShowConfig] = useState(false)

  /* ── Queries ── */
  const { data: status } = useQuery({
    queryKey: ['triangular-status'],
    queryFn: () => triangularApi.status(),
    refetchInterval: 5000,
  })

  const { data: configData } = useQuery({
    queryKey: ['triangular-config'],
    queryFn: () => triangularApi.config(),
    staleTime: 30000,
  })

  const { data: opportunities } = useQuery({
    queryKey: ['triangular-opportunity'],
    queryFn: () => triangularApi.opportunity(),
    refetchInterval: 3000,
  })

  const { data: positions } = useQuery({
    queryKey: ['triangular-positions'],
    queryFn: () => triangularApi.positions(),
    refetchInterval: 5000,
  })

  const { data: history } = useQuery({
    queryKey: ['triangular-history'],
    queryFn: () => triangularApi.history(50),
    enabled: showHistory,
  })

  const { data: configuredExchanges } = useQuery({
    queryKey: ['configured-exchanges'],
    queryFn: () => configApi.exchangesConfigured(),
    enabled: showConfig,
    staleTime: 30000,
  })

  /* ── Local state ── */
  const [editConfig, setEditConfig] = useState<TriangularConfig | null>(null)
  const [symbolsInput, setSymbolsInput] = useState<string>('BTCUSDT,ETHUSDT,ETHBTC')

  useEffect(() => {
    if (configData) {
      setEditConfig({ ...DEFAULT_TRIANGULAR_CONFIG, ...configData })
      setSymbolsInput((configData.symbols ?? DEFAULT_TRIANGULAR_CONFIG.symbols).join(', '))
    }
  }, [configData])

  /* ── Mutations ── */
  const startMutation = useMutation({
    mutationFn: triangularApi.start,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['triangular-status'] }),
  })

  const stopMutation = useMutation({
    mutationFn: triangularApi.stop,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['triangular-status'] }),
  })

  const updateConfigMut = useMutation({
    mutationFn: (data: TriangularConfig) => triangularApi.updateConfig(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['triangular-config'] })
      queryClient.invalidateQueries({ queryKey: ['triangular-status'] })
      useToastStore.getState().addToast({ type: 'success', message: '配置已保存', duration: 3000 })
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: 'error', message: err.message || '保存失败', duration: 5000 })
    },
  })

  const executeMut = useMutation({
    mutationFn: (data: { exchange: string; cycle: string[]; start_qty: number }) =>
      triangularApi.execute(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['triangular-positions'] })
      queryClient.invalidateQueries({ queryKey: ['triangular-history'] })
      queryClient.invalidateQueries({ queryKey: ['triangular-status'] })
      useToastStore.getState().addToast({ type: 'success', message: '三角套利执行已提交', duration: 3000 })
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: 'error', message: err.message || '执行失败', duration: 5000 })
    },
  })

  const closePositionMut = useMutation({
    mutationFn: (id: string) => triangularApi.closePosition(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['triangular-positions'] })
      queryClient.invalidateQueries({ queryKey: ['triangular-history'] })
      queryClient.invalidateQueries({ queryKey: ['triangular-status'] })
      useToastStore.getState().addToast({ type: 'success', message: '持仓已平仓', duration: 3000 })
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: 'error', message: err.message || '平仓失败', duration: 5000 })
    },
  })

  const failPositionMut = useMutation({
    mutationFn: (id: string) => triangularApi.failPosition(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['triangular-positions'] })
      queryClient.invalidateQueries({ queryKey: ['triangular-history'] })
      queryClient.invalidateQueries({ queryKey: ['triangular-status'] })
      useToastStore.getState().addToast({ type: 'success', message: '持仓已标记为失败', duration: 3000 })
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: 'error', message: err.message || '标记失败', duration: 5000 })
    },
  })

  /* ── Derived ── */
  const isRunning = status?.running ?? false
  const stats = (status?.stats ?? {}) as Record<string, string | number | undefined>
  const opportunity: TriangularOpportunity | null = opportunities?.[0] ?? null

  const handleSaveConfig = () => {
    if (!editConfig) return
    const symbols = symbolsInput
      .split(',')
      .map((s) => s.trim().toUpperCase())
      .filter(Boolean)
    const payload: TriangularConfig = {
      ...editConfig,
      symbols: symbols.length > 0 ? symbols : editConfig.symbols,
    }
    updateConfigMut.mutate(payload)
  }

  const handleExecute = (opp: TriangularOpportunity) => {
    if (!editConfig) return
    if (!editConfig.dry_run && !window.confirm('确认执行真实三角套利交易？')) return
    executeMut.mutate({
      exchange: opp.exchange,
      cycle: opp.cycle,
      start_qty: opp.start_qty,
    })
  }

  const isPositionActive = (status: string) => ['pending', 'executing'].includes(status)

  const handleClosePosition = (pos: TriangularTrade) => {
    if (!window.confirm(`确认将持仓 ${pos.cycle.join(' → ')} 平仓？`)) return
    closePositionMut.mutate(pos.id)
  }

  const handleFailPosition = (pos: TriangularTrade) => {
    if (!window.confirm(`确认将持仓 ${pos.cycle.join(' → ')} 标记为失败？`)) return
    failPositionMut.mutate(pos.id)
  }

  /* ── Render helpers ── */
  const renderConfigField = (
    label: string,
    input: React.ReactNode,
    key?: string
  ) => (
    <div key={key}>
      <label className="mb-1.5 block text-xs text-muted-foreground">{label}</label>
      {input}
    </div>
  )

  return (
    <div className="space-y-6">
      {/* Status & Controls */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <KPICard
          label="引擎状态"
          value={isRunning ? '运行中' : '已停止'}
          icon={
            isRunning ? (
              <Activity className="w-4 h-4 text-green-400" />
            ) : (
              <AlertCircle className="w-4 h-4 text-red-400" />
            )
          }
          subValue={isRunning ? '监控中' : '点击启动'}
          trend={isRunning ? 'up' : 'down'}
        />
        <KPICard
          label="扫描次数"
          value={stats.checks ?? 0}
          icon={<Target className="w-4 h-4 text-quant-gold" />}
          subValue="总扫描"
          trend="neutral"
        />
        <KPICard
          label="循环数"
          value={stats.cycles ?? 0}
          icon={<Triangle className="w-4 h-4 text-quant-gold" />}
          subValue="已检测"
          trend="up"
        />
        <KPICard
          label="总利润"
          value={stats.total_profit ? `$${(stats.total_profit as number).toFixed(2)}` : '$0.00'}
          icon={<DollarSign className="w-4 h-4 text-quant-gold" />}
          subValue="累计"
          trend="up"
        />
      </div>

      {/* Controls */}
      <div className="flex flex-wrap items-center gap-2">
        {!isRunning ? (
          <button
            onClick={() => startMutation.mutate()}
            disabled={startMutation.isPending}
            className={cn(
              'flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors',
              startMutation.isPending
                ? 'bg-muted text-muted-foreground cursor-not-allowed'
                : 'bg-green-500/20 text-green-400 hover:bg-green-500/30'
            )}
          >
            {startMutation.isPending ? (
              <RefreshCw className="w-4 h-4 animate-spin" />
            ) : (
              <Play className="w-4 h-4" />
            )}
            启动引擎
          </button>
        ) : (
          <button
            onClick={() => stopMutation.mutate()}
            disabled={stopMutation.isPending}
            className={cn(
              'flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors',
              stopMutation.isPending
                ? 'bg-muted text-muted-foreground cursor-not-allowed'
                : 'bg-red-500/20 text-red-400 hover:bg-red-500/30'
            )}
          >
            {stopMutation.isPending ? (
              <RefreshCw className="w-4 h-4 animate-spin" />
            ) : (
              <Square className="w-4 h-4" />
            )}
            停止引擎
          </button>
        )}
        <button
          onClick={() => setShowConfig(!showConfig)}
          className={cn(
            'flex items-center gap-2 px-3 py-2 rounded-md text-xs font-medium transition-colors',
            showConfig ? 'bg-quant-gold/10 text-quant-gold' : 'bg-quant-bg-secondary text-muted-foreground hover:text-foreground'
          )}
        >
          <Layers className="w-3.5 h-3.5" />
          配置
        </button>
        <button
          onClick={() => setShowHistory(!showHistory)}
          className={cn(
            'flex items-center gap-2 px-3 py-2 rounded-md text-xs font-medium transition-colors',
            showHistory ? 'bg-quant-gold/10 text-quant-gold' : 'bg-quant-bg-secondary text-muted-foreground hover:text-foreground'
          )}
        >
          <Clock className="w-3.5 h-3.5" />
          历史
        </button>
      </div>

      {/* Config */}
      {showConfig && (
        <SectionCard
          title="三角套利配置"
          headerAction={
            editConfig ? (
              <button
                onClick={handleSaveConfig}
                disabled={updateConfigMut.isPending}
                className={cn(
                  'flex items-center gap-2 px-3 py-1.5 rounded-md text-xs font-medium transition-colors',
                  updateConfigMut.isPending
                    ? 'bg-muted text-muted-foreground cursor-not-allowed'
                    : 'bg-quant-gold text-black hover:opacity-90'
                )}
              >
                {updateConfigMut.isPending ? (
                  <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <Save className="w-3.5 h-3.5" />
                )}
                保存配置
              </button>
            ) : null
          }
        >
          {!editConfig ? (
            <div className="text-sm text-muted-foreground text-center py-4">加载配置中...</div>
          ) : (
            <div className="space-y-6">
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {renderConfigField(
                  '交易所',
                  <select
                    value={editConfig.exchange}
                    onChange={(e) => setEditConfig((p) => (p ? { ...p, exchange: e.target.value } : p))}
                    className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold"
                  >
                    {SUPPORTED_EXCHANGES.map((ex) => (
                      <option key={ex.key} value={ex.key}>
                        {ex.label}
                      </option>
                    ))}
                  </select>,
                  'exchange'
                )}
                {renderConfigField(
                  '交易对（逗号分隔）',
                  <TextInput
                    value={symbolsInput}
                    onChange={(v) => setSymbolsInput(v)}
                    placeholder="BTCUSDT,ETHUSDT,ETHBTC"
                  />,
                  'symbols'
                )}
                {renderConfigField(
                  '计价资产',
                  <TextInput
                    value={editConfig.quote_asset}
                    onChange={(v) => setEditConfig((p) => (p ? { ...p, quote_asset: v.toUpperCase() } : p))}
                    placeholder="USDT"
                  />,
                  'quote_asset'
                )}
                {renderConfigField(
                  '最小净利润 (%)',
                  <NumberInput
                    value={editConfig.min_profit_pct}
                    onChange={(v) => setEditConfig((p) => (p ? { ...p, min_profit_pct: v } : p))}
                    min={0}
                    step={0.01}
                  />,
                  'min_profit_pct'
                )}
                {renderConfigField(
                  '订单金额',
                  <NumberInput
                    value={editConfig.order_size}
                    onChange={(v) => setEditConfig((p) => (p ? { ...p, order_size: v } : p))}
                    min={0}
                    step={0.001}
                  />,
                  'order_size'
                )}
                {renderConfigField(
                  '最大持仓数',
                  <NumberInput
                    value={editConfig.max_positions}
                    onChange={(v) => setEditConfig((p) => (p ? { ...p, max_positions: Math.floor(v) } : p))}
                    min={1}
                    step={1}
                  />,
                  'max_positions'
                )}
                {renderConfigField(
                  '手续费率 (小数)',
                  <NumberInput
                    value={editConfig.fee_rate}
                    onChange={(v) => setEditConfig((p) => (p ? { ...p, fee_rate: v } : p))}
                    min={0}
                    step={0.0001}
                  />,
                  'fee_rate'
                )}
                {renderConfigField(
                  '最大滑点 (%)',
                  <NumberInput
                    value={editConfig.max_slippage_pct}
                    onChange={(v) => setEditConfig((p) => (p ? { ...p, max_slippage_pct: v } : p))}
                    min={0}
                    step={0.01}
                  />,
                  'max_slippage_pct'
                )}
                {renderConfigField(
                  '最小订单数量',
                  <NumberInput
                    value={editConfig.min_order_qty}
                    onChange={(v) => setEditConfig((p) => (p ? { ...p, min_order_qty: v } : p))}
                    min={0}
                    step={0.0001}
                  />,
                  'min_order_qty'
                )}
                {renderConfigField(
                  '执行超时 (ms)',
                  <NumberInput
                    value={editConfig.max_execution_ms}
                    onChange={(v) => setEditConfig((p) => (p ? { ...p, max_execution_ms: Math.floor(v) } : p))}
                    min={1000}
                    step={100}
                  />,
                  'max_execution_ms'
                )}
                <div className="flex items-center gap-6 md:col-span-2 flex-wrap">
                  <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                    <Toggle
                      value={editConfig.auto_execute}
                      onChange={(v) => setEditConfig((p) => (p ? { ...p, auto_execute: v } : p))}
                    />
                    自动执行
                  </label>
                  <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                    <Toggle
                      value={editConfig.dry_run}
                      onChange={(v) => setEditConfig((p) => (p ? { ...p, dry_run: v } : p))}
                    />
                    模拟运行
                  </label>
                  <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                    <Toggle
                      value={editConfig.adaptive_qty_enabled}
                      onChange={(v) => setEditConfig((p) => (p ? { ...p, adaptive_qty_enabled: v } : p))}
                    />
                    自适应数量
                  </label>
                </div>
              </div>

              {/* Exchange readiness */}
              <div className="border-t border-quant-border pt-6">
                <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-4">
                  交易所状态
                </h3>
                {configuredExchanges ? (
                  <div className="space-y-2">
                    {SUPPORTED_EXCHANGES.map((ex) => {
                      const cfg = configuredExchanges[ex.key]
                      const ready = cfg?.enabled && cfg?.has_credentials
                      return (
                        <div
                          key={ex.key}
                          className="flex items-center justify-between rounded-md border border-quant-border px-3 py-2"
                        >
                          <div className="flex items-center gap-3">
                            <Globe className="h-4 w-4 text-muted-foreground" />
                            <div>
                              <div className="text-sm font-medium">{ex.label}</div>
                              <div className="text-[10px] text-muted-foreground">
                                {cfg?.enabled
                                  ? cfg?.has_credentials
                                    ? `已配置${cfg.testnet ? ' · 测试网' : ''}`
                                    : '缺少凭证'
                                  : '未启用'}
                              </div>
                            </div>
                          </div>
                          {ready ? (
                            <span className="inline-flex items-center gap-1 text-xs text-green-400">
                              <CheckCircle2 className="h-3.5 w-3.5" />
                              可用
                            </span>
                          ) : (
                            <span className="text-[10px] text-muted-foreground">未就绪</span>
                          )}
                        </div>
                      )
                    })}
                  </div>
                ) : (
                  <div className="text-sm text-muted-foreground">加载交易所配置中...</div>
                )}
              </div>
            </div>
          )}
        </SectionCard>
      )}

      {/* Opportunities Table */}
      <SectionCard
        title="三角套利机会"
        headerAction={
          opportunity ? (
            <span className="text-xs text-muted-foreground">最新扫描结果</span>
          ) : null
        }
      >
        {opportunity ? (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-quant-border text-left text-xs text-muted-foreground">
                  <th className="py-2 px-3 font-medium">循环路径</th>
                  <th className="py-2 px-3 font-medium">交易所</th>
                  <th className="py-2 px-3 font-medium text-right">起点数量</th>
                  <th className="py-2 px-3 font-medium text-right">终点数量</th>
                  <th className="py-2 px-3 font-medium text-right">净利润 %</th>
                  <th className="py-2 px-3 font-medium text-right">总手续费</th>
                  <th className="py-2 px-3 font-medium text-right">三腿滑点</th>
                  <th className="py-2 px-3 font-medium text-center">操作</th>
                </tr>
              </thead>
              <tbody>
                <tr className={cn('border-b border-quant-border transition-colors', opportunity.viable ? 'bg-green-500/5' : 'hover:bg-quant-bg-secondary/50')}>
                  <td className="py-3 px-3 font-medium">
                    {opportunity.cycle.join(' → ')}
                  </td>
                  <td className="py-3 px-3">{opportunity.exchange}</td>
                  <td className="py-3 px-3 text-right">
                    {opportunity.start_qty.toFixed(4)} {opportunity.start_asset}
                  </td>
                  <td className="py-3 px-3 text-right">
                    {opportunity.end_qty.toFixed(4)} {opportunity.start_asset}
                  </td>
                  <td className="py-3 px-3 text-right">
                    <span className={cn('font-medium', opportunity.net_profit_pct >= 0 ? 'text-green-400' : 'text-red-400')}>
                      {opportunity.net_profit_pct.toFixed(4)}%
                    </span>
                  </td>
                  <td className="py-3 px-3 text-right text-xs text-muted-foreground">
                    ${opportunity.total_fees.toFixed(4)}
                  </td>
                  <td className="py-3 px-3 text-right text-xs text-muted-foreground">
                    {opportunity.legs.map((leg, i) => (
                      <div key={i} className={leg.slippage_pct > (editConfig?.max_slippage_pct ?? 0.5) ? 'text-red-400' : ''}>
                        {leg.symbol} {leg.slippage_pct.toFixed(4)}%
                      </div>
                    ))}
                  </td>
                  <td className="py-3 px-3 text-center">
                    <button
                      onClick={() => handleExecute(opportunity)}
                      disabled={executeMut.isPending || !opportunity.viable}
                      className={cn(
                        'inline-flex items-center gap-1 px-2.5 py-1 rounded text-xs font-medium transition-colors',
                        executeMut.isPending || !opportunity.viable
                          ? 'bg-muted text-muted-foreground cursor-not-allowed'
                          : 'bg-quant-gold text-black hover:opacity-90'
                      )}
                    >
                      {executeMut.isPending ? (
                        <RefreshCw className="w-3 h-3 animate-spin" />
                      ) : (
                        <Zap className="w-3 h-3" />
                      )}
                      执行
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyState
            icon={<Triangle className="w-10 h-10 text-muted-foreground" />}
            title="暂无三角套利机会"
            description={isRunning ? '引擎正在扫描中...' : '启动引擎后开始扫描'}
          />
        )}
      </SectionCard>

      {/* Active Positions */}
      <SectionCard title="活跃持仓">
        <div className="space-y-2">
          {!positions || positions.length === 0 ? (
            <div className="text-sm text-muted-foreground text-center py-4">无活跃持仓</div>
          ) : (
            positions.map((pos: TriangularTrade, i: number) => (
              <div
                key={pos.id || i}
                className="flex items-center justify-between p-3 rounded-md bg-quant-bg-secondary"
              >
                <div className="flex items-center gap-3">
                  <Triangle className="w-4 h-4 text-quant-gold" />
                  <div>
                    <div className="text-sm font-medium">{pos.cycle.join(' → ')}</div>
                    <div className="text-xs text-muted-foreground">{pos.exchange}</div>
                    <div className="mt-1 flex items-center gap-1">
                      {pos.legs.map((leg, idx) => (
                        <span
                          key={idx}
                          className={cn(
                            'inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border',
                            leg.status === 'filled'
                              ? 'bg-green-500/10 text-green-400 border-green-500/20'
                              : leg.status === 'failed'
                              ? 'bg-red-500/10 text-red-400 border-red-500/20'
                              : 'bg-quant-bg text-muted-foreground border-quant-border'
                          )}
                        >
                          {leg.symbol} {leg.side}
                        </span>
                      ))}
                    </div>
                  </div>
                </div>
                <div className="text-right space-y-1">
                  <div
                    className={cn(
                      'text-sm font-semibold',
                      pos.net_profit > 0 ? 'text-green-400' : 'text-red-400'
                    )}
                  >
                    {pos.net_profit > 0 ? '+' : ''}${pos.net_profit.toFixed(2)}
                  </div>
                  {isPositionActive(pos.status) && (
                    <div className="flex items-center justify-end gap-2">
                      <button
                        onClick={() => handleClosePosition(pos)}
                        disabled={closePositionMut.isPending}
                        className="inline-flex items-center px-2 py-1 rounded text-[10px] font-medium bg-quant-gold text-black hover:opacity-90 disabled:opacity-50"
                      >
                        平仓
                      </button>
                      <button
                        onClick={() => handleFailPosition(pos)}
                        disabled={failPositionMut.isPending}
                        className="inline-flex items-center px-2 py-1 rounded text-[10px] font-medium bg-red-500/20 text-red-400 hover:bg-red-500/30 disabled:opacity-50"
                      >
                        失败
                      </button>
                    </div>
                  )}
                </div>
              </div>
            ))
          )}
        </div>
      </SectionCard>

      {/* History */}
      {showHistory && (
        <SectionCard
          title={
            <button onClick={() => setShowHistory(false)} className="flex items-center gap-2">
              历史记录 <ChevronUp className="w-4 h-4" />
            </button>
          }
        >
          <div className="space-y-2 max-h-80 overflow-y-auto">
            {!history || history.length === 0 ? (
              <div className="text-sm text-muted-foreground text-center py-4">无历史记录</div>
            ) : (
              history.map((trade: TriangularTrade, i: number) => (
                <div
                  key={trade.id || i}
                  className="flex items-center justify-between p-3 rounded-md bg-quant-bg-secondary"
                >
                  <div className="flex items-center gap-3">
                    <CheckCircle2
                      className={cn(
                        'w-4 h-4',
                        trade.net_profit > 0 ? 'text-green-400' : 'text-red-400'
                      )}
                    />
                    <div>
                      <div className="text-sm font-medium">{trade.cycle.join(' → ')}</div>
                      <div className="text-xs text-muted-foreground">{trade.exchange} · {trade.status}</div>
                    </div>
                  </div>
                  <div className="text-right">
                    <div
                      className={cn(
                        'text-sm font-semibold',
                        trade.net_profit > 0 ? 'text-green-400' : 'text-red-400'
                      )}
                    >
                      {trade.net_profit > 0 ? '+' : ''}${trade.net_profit.toFixed(2)}
                    </div>
                    <div className="text-xs text-muted-foreground">
                      {trade.closed_at ? new Date(trade.closed_at).toLocaleString() : '-'}
                    </div>
                  </div>
                </div>
              ))
            )}
          </div>
        </SectionCard>
      )}
    </div>
  )
}
