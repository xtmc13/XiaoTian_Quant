import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { arbitrageApi, configApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useToastStore } from '@/stores/toastStore'
import { Select } from '@/components/ui/Select'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import type {
  ArbitrageConfig,
  ArbitrageOpportunity,
  ArbitragePosition,
  ArbitrageHistoryItem,
} from '@/types'
import { TriangularArbitragePanel } from './TriangularArbitragePanel'
import {
  ArrowLeftRight,
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
  Eye,
  EyeOff,
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

function PasswordInput({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  const [visible, setVisible] = useState(false)
  return (
    <div className="relative">
      <input
        type={visible ? 'text' : 'password'}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 pr-10 text-sm text-white placeholder-muted-foreground outline-none transition-colors focus:border-quant-gold"
      />
      <button
        type="button"
        onClick={() => setVisible(!visible)}
        className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
      >
        {visible ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
      </button>
    </div>
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
  { key: 'binance', label: 'Binance', needsPassphrase: false, supportsTestnet: true },
  { key: 'okx', label: 'OKX', needsPassphrase: true, supportsTestnet: true },
  { key: 'mexc', label: 'MEXC', needsPassphrase: false, supportsTestnet: false },
  { key: 'gateio', label: 'Gate.io', needsPassphrase: false, supportsTestnet: false },
  { key: 'bybit', label: 'Bybit', needsPassphrase: false, supportsTestnet: true },
  { key: 'coinbase', label: 'Coinbase', needsPassphrase: false, supportsTestnet: false },
  { key: 'kraken', label: 'Kraken', needsPassphrase: false, supportsTestnet: false },
  { key: 'bitget', label: 'Bitget', needsPassphrase: true, supportsTestnet: false },
] as const

const DEFAULT_CONFIG: ArbitrageConfig = {
  symbol: 'BTCUSDT',
  min_spread_pct: 0.3,
  order_size: 500,
  max_positions: 3,
  fee_a: 0.001,
  fee_b: 0.001,
  poll_interval: 2,
  auto_execute: false,
  dry_run: true,
  adaptive_qty_enabled: false,
  max_slippage_pct: 0.5,
  min_order_qty: 0.001,
  min_order_value: 10.0,
}

/* ── Page ── */

export function ArbitrageMonitor() {
  const [searchParams, setSearchParams] = useSearchParams()
  const activeTab = searchParams.get('tab') || 'cross'
  const setActiveTab = (tab: string) => {
    setSearchParams({ tab })
  }

  return (
    <div className="h-full overflow-y-auto">
      <div className="p-4 md:p-6 space-y-6 max-w-7xl mx-auto">
        <PageHeader
          title="套利中心"
          subtitle="跨所套利与三角套利实时监控"
          actions={<ArrowLeftRight className="w-6 h-6 text-quant-gold" />}
        />

        {/* Tabs */}
        <div className="flex items-center gap-1 border-b border-quant-border">
          <button
            onClick={() => setActiveTab('cross')}
            className={cn(
              'px-4 py-2 text-sm font-medium transition-colors border-b-2',
              activeTab === 'cross'
                ? 'text-quant-gold border-quant-gold'
                : 'text-muted-foreground border-transparent hover:text-foreground'
            )}
          >
            跨所套利
          </button>
          <button
            onClick={() => setActiveTab('triangular')}
            className={cn(
              'px-4 py-2 text-sm font-medium transition-colors border-b-2',
              activeTab === 'triangular'
                ? 'text-quant-gold border-quant-gold'
                : 'text-muted-foreground border-transparent hover:text-foreground'
            )}
          >
            三角套利
          </button>
        </div>

        {activeTab === 'cross' && <CrossArbitragePanel />}
        {activeTab === 'triangular' && <TriangularArbitragePanel />}
      </div>
    </div>
  )
}

function CrossArbitragePanel() {
  const queryClient = useQueryClient()
  const [showHistory, setShowHistory] = useState(false)
  const [showConfig, setShowConfig] = useState(false)

  /* ── Queries ── */
  const { data: status } = useQuery({
    queryKey: ['arbitrage-status'],
    queryFn: () => arbitrageApi.status(),
    refetchInterval: 5000,
  })

  const { data: configData } = useQuery({
    queryKey: ['arbitrage-config'],
    queryFn: () => arbitrageApi.config(),
    staleTime: 30000,
  })

  const { data: opportunities } = useQuery({
    queryKey: ['arbitrage-opportunity'],
    queryFn: () => arbitrageApi.opportunity(),
    refetchInterval: 3000,
  })

  const { data: positions } = useQuery({
    queryKey: ['arbitrage-positions'],
    queryFn: () => arbitrageApi.positions(),
    refetchInterval: 5000,
  })

  const { data: history } = useQuery({
    queryKey: ['arbitrage-history'],
    queryFn: () => arbitrageApi.history(50),
    enabled: showHistory,
  })

  const { data: exchangesMeta } = useQuery({
    queryKey: ['arbitrage-exchanges'],
    queryFn: () => arbitrageApi.exchanges(),
    enabled: showConfig,
  })

  const { data: configuredExchanges } = useQuery({
    queryKey: ['configured-exchanges'],
    queryFn: () => configApi.exchangesConfigured(),
    enabled: showConfig,
    staleTime: 30000,
  })

  /* ── Local state ── */
  const [editConfig, setEditConfig] = useState<ArbitrageConfig | null>(null)
  const [symbolsInput, setSymbolsInput] = useState<string>('BTCUSDT')

  useEffect(() => {
    if (configData) {
      setEditConfig({ ...DEFAULT_CONFIG, ...configData })
      const symbols = configData.symbols?.length
        ? configData.symbols
        : [configData.symbol || 'BTCUSDT']
      setSymbolsInput(symbols.join(', '))
    }
  }, [configData])

  /* ── Mutations ── */
  const startMutation = useMutation({
    mutationFn: arbitrageApi.start,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] }),
  })

  const stopMutation = useMutation({
    mutationFn: arbitrageApi.stop,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] }),
  })

  const updateConfigMut = useMutation({
    mutationFn: (data: ArbitrageConfig) => arbitrageApi.updateConfig(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-config'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      useToastStore.getState().addToast({ type: 'success', message: '配置已保存', duration: 3000 })
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: 'error', message: err.message || '保存失败', duration: 5000 })
    },
  })

  const registerExchangeMut = useMutation({
    mutationFn: (data: { name: string }) => arbitrageApi.registerExchange(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-exchanges'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      useToastStore.getState().addToast({ type: 'success', message: '交易所已加入套利', duration: 3000 })
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: 'error', message: err.message || '加入失败', duration: 5000 })
    },
  })

  const executeMut = useMutation({
    mutationFn: (data: {
      symbol: string
      buy_exchange: string
      sell_exchange: string
      buy_price: number
      sell_price: number
      quantity: number
    }) => arbitrageApi.execute(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-positions'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-history'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      useToastStore.getState().addToast({ type: 'success', message: '套利执行已提交', duration: 3000 })
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: 'error', message: err.message || '执行失败', duration: 5000 })
    },
  })

  const closePositionMut = useMutation({
    mutationFn: ({ id, sell_price }: { id: string; sell_price: number }) =>
      arbitrageApi.closePosition(id, sell_price),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-positions'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-history'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      useToastStore.getState().addToast({ type: 'success', message: '持仓已平仓', duration: 3000 })
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: 'error', message: err.message || '平仓失败', duration: 5000 })
    },
  })

  const failPositionMut = useMutation({
    mutationFn: (id: string) => arbitrageApi.failPosition(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-positions'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-history'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      useToastStore.getState().addToast({ type: 'success', message: '持仓已标记为失败', duration: 3000 })
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: 'error', message: err.message || '标记失败', duration: 5000 })
    },
  })

  /* ── Derived ── */
  const isRunning = status?.running ?? false
  const stats = (status?.stats ?? {}) as Record<string, string | number | undefined>
  const opportunity: ArbitrageOpportunity | null = opportunities?.[0] ?? null

  const handleSaveConfig = () => {
    if (!editConfig) return
    const symbols = symbolsInput
      .split(',')
      .map((s) => s.trim().toUpperCase())
      .filter(Boolean)
    const payload: ArbitrageConfig = {
      ...editConfig,
      symbol: symbols[0] || editConfig.symbol,
      symbols: symbols.length > 0 ? symbols : undefined,
    }
    updateConfigMut.mutate(payload)
  }

  const handleRegisterExchange = (name: string) => {
    registerExchangeMut.mutate({ name })
  }

  const handleExecute = (opp: ArbitrageOpportunity) => {
    if (!editConfig) return
    if (!editConfig.dry_run && !window.confirm('确认执行真实套利交易？')) return
    const targetQty = editConfig.order_size / opp.buy_price
    const quantity = opp.adjusted_qty ?? Math.floor(targetQty * 1e6) / 1e6
    executeMut.mutate({
      symbol: opp.symbol,
      buy_exchange: opp.buy_exchange,
      sell_exchange: opp.sell_exchange,
      buy_price: opp.buy_price,
      sell_price: opp.sell_price,
      quantity,
    })
  }

  const isPositionActive = (status: string) => ['pending', 'open_buy', 'open', 'open_sell'].includes(status)

  const handleClosePosition = (pos: ArbitragePosition) => {
    const input = window.prompt('请输入实际卖出价（USD）', pos.sell_price?.toFixed(2) ?? '')
    if (input === null) return
    const sellPrice = Number(input)
    if (Number.isNaN(sellPrice) || sellPrice <= 0) {
      useToastStore.getState().addToast({ type: 'error', message: '请输入有效的卖出价', duration: 3000 })
      return
    }
    closePositionMut.mutate({ id: pos.id, sell_price: sellPrice })
  }

  const handleFailPosition = (pos: ArbitragePosition) => {
    if (!window.confirm(`确认将持仓 ${pos.symbol} 标记为失败？`)) return
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
    <div className="h-full overflow-y-auto">
      <div className="p-4 md:p-6 space-y-6 max-w-7xl mx-auto">
        <PageHeader
          title="套利监控"
          subtitle="跨交易所价差套利实时监控"
          actions={<ArrowLeftRight className="w-6 h-6 text-quant-gold" />}
        />

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
            label="检测次数"
            value={stats.checks ?? 0}
            icon={<Target className="w-4 h-4 text-quant-gold" />}
            subValue="总扫描"
            trend="neutral"
          />
          <KPICard
            label="执行次数"
            value={stats.executions ?? 0}
            icon={<Zap className="w-4 h-4 text-quant-gold" />}
            subValue="已执行"
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

        {/* Config & Exchange Registration */}
        {showConfig && (
          <SectionCard
            title="引擎配置"
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
                    '交易对（逗号分隔）',
                    <TextInput
                      value={symbolsInput}
                      onChange={(v) => setSymbolsInput(v)}
                      placeholder="BTCUSDT,ETHUSDT"
                    />,
                    'symbol'
                  )}
                  {renderConfigField(
                    '最小价差 (%)',
                    <NumberInput
                      value={editConfig.min_spread_pct}
                      onChange={(v) => setEditConfig((p) => (p ? { ...p, min_spread_pct: v } : p))}
                      min={0}
                      step={0.01}
                    />,
                    'min_spread_pct'
                  )}
                  {renderConfigField(
                    '订单数量',
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
                    '买入所手续费 (小数)',
                    <NumberInput
                      value={editConfig.fee_a}
                      onChange={(v) => setEditConfig((p) => (p ? { ...p, fee_a: v } : p))}
                      min={0}
                      step={0.0001}
                    />,
                    'fee_a'
                  )}
                  {renderConfigField(
                    '卖出所手续费 (小数)',
                    <NumberInput
                      value={editConfig.fee_b}
                      onChange={(v) => setEditConfig((p) => (p ? { ...p, fee_b: v } : p))}
                      min={0}
                      step={0.0001}
                    />,
                    'fee_b'
                  )}
                  {renderConfigField(
                    '轮询间隔 (秒)',
                    <NumberInput
                      value={editConfig.poll_interval}
                      onChange={(v) => setEditConfig((p) => (p ? { ...p, poll_interval: Math.floor(v) } : p))}
                      min={1}
                      step={1}
                    />,
                    'poll_interval'
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
                  {editConfig.adaptive_qty_enabled && (
                    <>
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
                        '最小订单金额 (USD)',
                        <NumberInput
                          value={editConfig.min_order_value}
                          onChange={(v) => setEditConfig((p) => (p ? { ...p, min_order_value: v } : p))}
                          min={0}
                          step={1}
                        />,
                        'min_order_value'
                      )}
                    </>
                  )}
                </div>

                {/* Exchange selection */}
                <div className="border-t border-quant-border pt-6">
                  <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-4">
                    交易所选择
                  </h3>
                  {configuredExchanges ? (
                    <div className="space-y-2">
                      {SUPPORTED_EXCHANGES.map((ex) => {
                        const cfg = configuredExchanges[ex.key]
                        const registered = exchangesMeta?.exchanges?.includes(ex.key) ?? false
                        const canRegister = cfg?.enabled && cfg?.has_credentials
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
                            {registered ? (
                              <span className="inline-flex items-center gap-1 text-xs text-green-400">
                                <CheckCircle2 className="h-3.5 w-3.5" />
                                已加入套利
                              </span>
                            ) : canRegister ? (
                              <button
                                onClick={() => handleRegisterExchange(ex.key)}
                                disabled={registerExchangeMut.isPending}
                                className={cn(
                                  'inline-flex items-center gap-1 px-2.5 py-1 rounded text-xs font-medium transition-colors',
                                  registerExchangeMut.isPending
                                    ? 'bg-muted text-muted-foreground cursor-not-allowed'
                                    : 'bg-quant-gold text-black hover:opacity-90'
                                )}
                              >
                                {registerExchangeMut.isPending ? (
                                  <RefreshCw className="h-3 w-3 animate-spin" />
                                ) : (
                                  <Plus className="h-3 w-3" />
                                )}
                                加入套利
                              </button>
                            ) : (
                              <span className="text-[10px] text-muted-foreground">未就绪</span>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  ) : (
                    <div className="text-sm text-muted-foreground">
                      加载交易所配置中...
                    </div>
                  )}
                  {configuredExchanges &&
                    !Object.values(configuredExchanges).some((c) => c.enabled && c.has_credentials) && (
                      <div className="mt-3 text-xs text-yellow-400">
                        系统中没有可用的交易所配置。请先在 Settings / 交易所账号 中配置 API Key。
                      </div>
                    )}
                  {exchangesMeta && (
                    <div className="mt-3 text-xs text-muted-foreground">
                      已加入套利交易所: {exchangesMeta.registered_count ?? 0} 个
                    </div>
                  )}
                </div>
              </div>
            )}
          </SectionCard>
        )}

        {/* Opportunities Table */}
        <SectionCard
          title="套利机会"
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
                    <th className="py-2 px-3 font-medium">交易对</th>
                    <th className="py-2 px-3 font-medium">买入所</th>
                    <th className="py-2 px-3 font-medium">卖出所</th>
                    <th className="py-2 px-3 font-medium text-right">买价/可执行</th>
                    <th className="py-2 px-3 font-medium text-right">卖价/可执行</th>
                    <th className="py-2 px-3 font-medium text-right">净价差 %</th>
                    <th className="py-2 px-3 font-medium text-right">滑点(买/卖)</th>
                    <th className="py-2 px-3 font-medium text-right">目标/调整数量</th>
                    <th className="py-2 px-3 font-medium text-right">最大可成交</th>
                    <th className="py-2 px-3 font-medium text-right">预估净利润</th>
                    <th className="py-2 px-3 font-medium text-center">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {(() => {
                    const feeA = editConfig?.fee_a ?? DEFAULT_CONFIG.fee_a
                    const feeB = editConfig?.fee_b ?? DEFAULT_CONFIG.fee_b
                    const orderSize = editConfig?.order_size ?? DEFAULT_CONFIG.order_size
                    const minSpread = editConfig?.min_spread_pct ?? DEFAULT_CONFIG.min_spread_pct

                    const buyPrice = opportunity.executable_buy_price ?? opportunity.buy_price ?? 0
                    const sellPrice = opportunity.executable_sell_price ?? opportunity.sell_price ?? 0
                    const spreadPct = buyPrice > 0 ? ((sellPrice - buyPrice) / buyPrice) * 100 : 0
                    const netSpreadPct = spreadPct - (feeA + feeB) * 100
                    const targetQty = opportunity.buy_price > 0 ? orderSize / opportunity.buy_price : 0
                    const adjustedQty = opportunity.adjusted_qty ?? targetQty
                    const actualValue = adjustedQty * buyPrice
                    const estimatedProfit = actualValue * (netSpreadPct / 100)
                    const isViable = opportunity.viable !== false && netSpreadPct >= minSpread && buyPrice > 0 && sellPrice > 0

                    const slipBuy = opportunity.slippage_buy_pct ?? 0
                    const slipSell = opportunity.slippage_sell_pct ?? 0
                    const qtyChanged = opportunity.adjusted_qty !== undefined && Math.abs(opportunity.adjusted_qty - targetQty) > 1e-9
                    return (
                      <tr
                        className={cn(
                          'border-b border-quant-border transition-colors',
                          isViable ? 'bg-green-500/5' : 'hover:bg-quant-bg-secondary/50'
                        )}
                      >
                        <td className="py-3 px-3 font-medium">{opportunity.symbol}</td>
                        <td className="py-3 px-3 text-green-400">{opportunity.buy_exchange}</td>
                        <td className="py-3 px-3 text-red-400">{opportunity.sell_exchange}</td>
                        <td className="py-3 px-3 text-right">
                          <div>${opportunity.buy_price?.toFixed(2) ?? '-'}</div>
                          {opportunity.executable_buy_price ? (
                            <div className="text-[10px] text-muted-foreground">
                              实 {opportunity.executable_buy_price.toFixed(2)}
                            </div>
                          ) : null}
                        </td>
                        <td className="py-3 px-3 text-right">
                          <div>${opportunity.sell_price?.toFixed(2) ?? '-'}</div>
                          {opportunity.executable_sell_price ? (
                            <div className="text-[10px] text-muted-foreground">
                              实 {opportunity.executable_sell_price.toFixed(2)}
                            </div>
                          ) : null}
                        </td>
                        <td className="py-3 px-3 text-right">
                          <span className={cn('font-medium', netSpreadPct >= 0 ? 'text-green-400' : 'text-red-400')}>
                            {netSpreadPct.toFixed(4)}%
                          </span>
                          <div className="text-[10px] text-muted-foreground">毛 {spreadPct.toFixed(4)}%</div>
                        </td>
                        <td className="py-3 px-3 text-right text-[10px] text-muted-foreground">
                          <div className="text-red-400">+{slipBuy.toFixed(4)}%</div>
                          <div className="text-red-400">+{slipSell.toFixed(4)}%</div>
                        </td>
                        <td className="py-3 px-3 text-right text-xs">
                          <div>{targetQty.toFixed(4)}</div>
                          {qtyChanged && (
                            <div className="text-[10px] text-quant-gold">→ {adjustedQty.toFixed(4)}</div>
                          )}
                        </td>
                        <td className="py-3 px-3 text-right text-xs">
                          {opportunity.max_executable_qty?.toFixed(4) ?? '-'}
                        </td>
                        <td className="py-3 px-3 text-right">
                          <div className={cn('font-medium', estimatedProfit >= 0 ? 'text-green-400' : 'text-red-400')}>
                            ${estimatedProfit.toFixed(2)}
                          </div>
                          {!isViable && (
                            <div className="text-[10px] text-yellow-400">
                              {opportunity.viable === false ? '深度不足' : '未达阈值'}
                            </div>
                          )}
                        </td>
                        <td className="py-3 px-3 text-center">
                          <button
                            onClick={() => handleExecute(opportunity)}
                            disabled={executeMut.isPending || !isViable}
                            className={cn(
                              'inline-flex items-center gap-1 px-2.5 py-1 rounded text-xs font-medium transition-colors',
                              executeMut.isPending || !isViable
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
                    )
                  })()}
                </tbody>
              </table>
            </div>
          ) : (
            <EmptyState
              icon={<ArrowLeftRight className="w-10 h-10 text-muted-foreground" />}
              title="暂无套利机会"
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
              positions.map((pos: ArbitragePosition, i: number) => (
                <div
                  key={pos.id || i}
                  className="flex items-center justify-between p-3 rounded-md bg-quant-bg-secondary"
                >
                  <div className="flex items-center gap-3">
                    <ArrowLeftRight className="w-4 h-4 text-quant-gold" />
                    <div>
                      <div className="text-sm font-medium">{pos.symbol}</div>
                      <div className="text-xs text-muted-foreground">
                        {pos.buy_exchange} → {pos.sell_exchange}
                      </div>
                      <div className="mt-1 inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-quant-bg text-muted-foreground border border-quant-border">
                        {pos.status}
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
                history.map((trade: ArbitrageHistoryItem, i: number) => (
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
                        <div className="text-sm font-medium">{trade.symbol}</div>
                        <div className="text-xs text-muted-foreground">
                          {trade.buy_exchange} → {trade.sell_exchange}
                        </div>
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
    </div>
  )
}
