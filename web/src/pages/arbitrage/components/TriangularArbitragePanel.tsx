import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { triangularApi, configApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useToastStore } from '@/stores/toastStore'
import { SectionCard } from '@/components/ui/SectionCard'
import { useConfirmDialog } from '@/components/ui/ConfirmDialog'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import { EmptyState } from '@/components/ui/EmptyState'
import { useI18n } from '@/i18n'
import type { TriangularConfig, TriangularOpportunity, TriangularTrade } from '@/types'
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
  X,
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

function TextInput({
  value,
  onChange,
  placeholder,
}: {
  value: string
  onChange: (v: string) => void
  placeholder?: string
}) {
  return (
    <input
      type="text"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-foreground placeholder-muted-foreground outline-none transition-colors focus:border-quant-gold"
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
      className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-foreground placeholder-muted-foreground outline-none transition-colors focus:border-quant-gold"
    />
  )
}

/* ── Constants ── */

const SUPPORTED_EXCHANGES = [
  { key: 'binance', label: 'Binance' },
  { key: 'okx', label: 'OKX' },
  { key: 'mexc', label: 'MEXC' },
  { key: 'gate', label: 'Gate.io' },
  { key: 'bybit', label: 'Bybit' },
  { key: 'coinbase', label: 'Coinbase' },
  { key: 'kraken', label: 'Kraken' },
  { key: 'bitget', label: 'Bitget' },
] as const

const DEFAULT_TRIANGULAR_CONFIG: TriangularConfig = {
  exchange: 'binance',
  exchanges: ['binance'],
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
  const { t } = useI18n()
  const { confirm, Dialog } = useConfirmDialog()
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
  // 交易所多选：null=未触碰，跟随已配置列表；触碰后跟随用户选择
  const [selectedExchanges, setSelectedExchanges] = useState<string[] | null>(null)
  useEffect(() => {
    if (!showConfig) setSelectedExchanges(null)
  }, [showConfig])
  const effectiveExchanges =
    selectedExchanges ??
    (editConfig ? (editConfig.exchanges?.length ? editConfig.exchanges : [editConfig.exchange]) : [])
  const toggleExchange = (key: string, checked: boolean) => {
    setSelectedExchanges(checked ? [...effectiveExchanges, key] : effectiveExchanges.filter((k) => k !== key))
  }

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
      useToastStore.getState().addToast({ type: 'success', message: t('arb.ui.toast-config-saved'), duration: 3000 })
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: 'error', message: err.message || t('arb.ui.toast-save-failed'), duration: 5000 })
    },
  })

  const executeMut = useMutation({
    mutationFn: (data: { exchange: string; cycle: string[]; start_qty: number }) => triangularApi.execute(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['triangular-positions'] })
      queryClient.invalidateQueries({ queryKey: ['triangular-history'] })
      queryClient.invalidateQueries({ queryKey: ['triangular-status'] })
      useToastStore.getState().addToast({ type: 'success', message: t('arb.tri.toast-execute-submitted'), duration: 3000 })
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: 'error', message: err.message || t('arb.ui.toast-execute-failed'), duration: 5000 })
    },
  })

  const closePositionMut = useMutation({
    mutationFn: (id: string) => triangularApi.closePosition(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['triangular-positions'] })
      queryClient.invalidateQueries({ queryKey: ['triangular-history'] })
      queryClient.invalidateQueries({ queryKey: ['triangular-status'] })
      useToastStore.getState().addToast({ type: 'success', message: t('arb.ui.toast-position-closed'), duration: 3000 })
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: 'error', message: err.message || t('arb.ui.toast-close-failed'), duration: 5000 })
    },
  })

  const failPositionMut = useMutation({
    mutationFn: (id: string) => triangularApi.failPosition(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['triangular-positions'] })
      queryClient.invalidateQueries({ queryKey: ['triangular-history'] })
      queryClient.invalidateQueries({ queryKey: ['triangular-status'] })
      useToastStore.getState().addToast({ type: 'success', message: t('arb.ui.toast-position-failed'), duration: 3000 })
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: 'error', message: err.message || t('arb.ui.toast-mark-failed'), duration: 5000 })
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
      exchanges: effectiveExchanges,
      exchange: effectiveExchanges[0] || editConfig.exchange,
    }
    updateConfigMut.mutate(payload)
  }

  const handleExecute = async (opp: TriangularOpportunity) => {
    if (!editConfig) return
    if (!editConfig.dry_run) {
      const ok = await confirm({
        title: t('arb.tri.confirm-execute-title'),
        message: t('arb.tri.confirm-execute-msg')
          .replace('{cycle}', opp.cycle.join(' → '))
          .replace('{profit}', opp.net_profit_pct.toFixed(4)),
        confirmText: t('arb.ui.execute'),
        cancelText: t('arb.ui.cancel'),
      })
      if (!ok) return
    }
    executeMut.mutate({
      exchange: opp.exchange,
      cycle: opp.cycle,
      start_qty: opp.start_qty,
    })
  }

  const isPositionActive = (status: string) => ['pending', 'executing'].includes(status)

  const handleClosePosition = async (pos: TriangularTrade) => {
    const ok = await confirm({
      title: t('arb.ui.close-position'),
      message: t('arb.ui.confirm-close-msg').replace('{target}', pos.cycle.join(' → ')),
      confirmText: t('arb.ui.close-position'),
      cancelText: t('arb.ui.cancel'),
    })
    if (!ok) return
    closePositionMut.mutate(pos.id)
  }

  const handleFailPosition = async (pos: TriangularTrade) => {
    const ok = await confirm({
      title: t('arb.ui.mark-as-failed'),
      message: t('arb.ui.confirm-fail-msg').replace('{target}', pos.cycle.join(' → ')),
      variant: 'danger',
      confirmText: t('arb.ui.mark-fail'),
      cancelText: t('arb.ui.cancel'),
    })
    if (!ok) return
    failPositionMut.mutate(pos.id)
  }

  /* ── Render helpers ── */
  const renderConfigField = (label: string, input: React.ReactNode, key?: string) => (
    <div key={key}>
      <label className="mb-1.5 block text-xs text-muted-foreground">{label}</label>
      {input}
    </div>
  )

  return (
    <div className="space-y-6">
      <Dialog />
      {/* Status & Controls */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <KPICard
          label={t('arb.ui.engine-status')}
          value={isRunning ? t('arb.ui.running') : t('arb.ui.stopped')}
          icon={
            isRunning ? (
              <Activity className="w-4 h-4 text-green-400" />
            ) : (
              <AlertCircle className="w-4 h-4 text-red-400" />
            )
          }
          subValue={isRunning ? t('arb.ui.monitoring') : t('arb.ui.click-to-start')}
          trend={isRunning ? 'up' : 'down'}
        />
        <KPICard
          label={t('arb.tri.scans')}
          value={stats.checks ?? 0}
          icon={<Target className="w-4 h-4 text-quant-gold" />}
          subValue={t('arb.ui.sub-total-scan')}
          trend="neutral"
        />
        <KPICard
          label={t('arb.tri.cycles')}
          value={stats.cycles ?? 0}
          icon={<Triangle className="w-4 h-4 text-quant-gold" />}
          subValue={t('arb.tri.sub-detected')}
          trend="up"
        />
        <KPICard
          label={t('arb.ui.total-profit')}
          value={stats.total_profit ? `$${(stats.total_profit as number).toFixed(2)}` : '$0.00'}
          icon={<DollarSign className="w-4 h-4 text-quant-gold" />}
          subValue={t('arb.ui.cumulative')}
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
            {startMutation.isPending ? <RefreshCw className="w-4 h-4 animate-spin" /> : <Play className="w-4 h-4" />}
            {t('arb.ui.start-engine')}
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
            {stopMutation.isPending ? <RefreshCw className="w-4 h-4 animate-spin" /> : <Square className="w-4 h-4" />}
            {t('arb.ui.stop-engine')}
          </button>
        )}
        <button
          onClick={() => setShowConfig(!showConfig)}
          className={cn(
            'flex items-center gap-2 px-3 py-2 rounded-md text-xs font-medium transition-colors',
            showConfig
              ? 'bg-quant-gold/10 text-quant-gold'
              : 'bg-quant-bg-secondary text-muted-foreground hover:text-foreground'
          )}
        >
          <Layers className="w-3.5 h-3.5" />
          {t('arb.ui.config')}
        </button>
        <button
          onClick={() => setShowHistory(!showHistory)}
          className={cn(
            'flex items-center gap-2 px-3 py-2 rounded-md text-xs font-medium transition-colors',
            showHistory
              ? 'bg-quant-gold/10 text-quant-gold'
              : 'bg-quant-bg-secondary text-muted-foreground hover:text-foreground'
          )}
        >
          <Clock className="w-3.5 h-3.5" />
          {t('arb.ui.history')}
        </button>
      </div>

      {/* Config modal */}
      {showConfig && (
        <div
          role="dialog"
          aria-modal="true"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
          onClick={() => setShowConfig(false)}
          onKeyDown={(e) => {
            if (e.key === 'Escape') setShowConfig(false)
          }}
          tabIndex={-1}
        >
          <div
            role="document"
            className="w-full max-w-4xl max-h-[90vh] flex flex-col rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
              <h3 className="text-sm font-bold">{t('arb.tri.config-title')}</h3>
              <button
                onClick={() => setShowConfig(false)}
                aria-label={t('arb.ui.close')}
                className="text-muted-foreground hover:text-foreground"
              >
                <X className="w-4 h-4" />
              </button>
            </div>
            <div className="flex-1 overflow-y-auto p-6">
          {!editConfig ? (
            <div className="text-sm text-muted-foreground text-center py-4">{t('arb.ui.loading-config')}</div>
          ) : (
            <div className="space-y-6">
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {renderConfigField(
                  t('arb.ui.symbols'),
                  <TextInput
                    value={symbolsInput}
                    onChange={(v) => setSymbolsInput(v)}
                    placeholder="BTCUSDT,ETHUSDT,ETHBTC"
                  />,
                  'symbols'
                )}
                {renderConfigField(
                  t('arb.tri.quote-asset'),
                  <TextInput
                    value={editConfig.quote_asset}
                    onChange={(v) => setEditConfig((p) => (p ? { ...p, quote_asset: v.toUpperCase() } : p))}
                    placeholder="USDT"
                  />,
                  'quote_asset'
                )}
                {renderConfigField(
                  t('arb.tri.min-profit'),
                  <NumberInput
                    value={editConfig.min_profit_pct}
                    onChange={(v) => setEditConfig((p) => (p ? { ...p, min_profit_pct: v } : p))}
                    min={0}
                    step={0.01}
                  />,
                  'min_profit_pct'
                )}
                {renderConfigField(
                  t('arb.tri.order-size'),
                  <NumberInput
                    value={editConfig.order_size}
                    onChange={(v) => setEditConfig((p) => (p ? { ...p, order_size: v } : p))}
                    min={0}
                    step={0.001}
                  />,
                  'order_size'
                )}
                {renderConfigField(
                  t('arb.ui.max-positions'),
                  <NumberInput
                    value={editConfig.max_positions}
                    onChange={(v) => setEditConfig((p) => (p ? { ...p, max_positions: Math.floor(v) } : p))}
                    min={1}
                    step={1}
                  />,
                  'max_positions'
                )}
                {renderConfigField(
                  t('arb.tri.fee-rate'),
                  <NumberInput
                    value={editConfig.fee_rate}
                    onChange={(v) => setEditConfig((p) => (p ? { ...p, fee_rate: v } : p))}
                    min={0}
                    step={0.0001}
                  />,
                  'fee_rate'
                )}
                {renderConfigField(
                  t('arb.ui.max-slippage'),
                  <NumberInput
                    value={editConfig.max_slippage_pct}
                    onChange={(v) => setEditConfig((p) => (p ? { ...p, max_slippage_pct: v } : p))}
                    min={0}
                    step={0.01}
                  />,
                  'max_slippage_pct'
                )}
                {renderConfigField(
                  t('arb.ui.min-order-qty'),
                  <NumberInput
                    value={editConfig.min_order_qty}
                    onChange={(v) => setEditConfig((p) => (p ? { ...p, min_order_qty: v } : p))}
                    min={0}
                    step={0.0001}
                  />,
                  'min_order_qty'
                )}
                {renderConfigField(
                  t('arb.tri.exec-timeout'),
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
                    {t('arb.ui.auto-execute')}
                  </label>
                  <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                    <Toggle
                      value={editConfig.dry_run}
                      onChange={(v) => setEditConfig((p) => (p ? { ...p, dry_run: v } : p))}
                    />
                    {t('arb.ui.dry-run')}
                  </label>
                  <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                    <Toggle
                      value={editConfig.adaptive_qty_enabled}
                      onChange={(v) => setEditConfig((p) => (p ? { ...p, adaptive_qty_enabled: v } : p))}
                    />
                    {t('arb.ui.adaptive-qty')}
                  </label>
                </div>
              </div>

              {/* Exchange selection */}
              <div className="border-t border-quant-border pt-6">
                <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-4">
                  {t('arb.ui.exchange-selection')}
                </h3>
                {configuredExchanges ? (
                  <div className="space-y-2">
                    {SUPPORTED_EXCHANGES.map((ex) => {
                      const cfg = configuredExchanges[ex.key]
                      const ready = cfg?.enabled && cfg?.has_credentials
                      const isSelected = effectiveExchanges.includes(ex.key)
                      return (
                        <label
                          key={ex.key}
                          className={cn(
                            'flex items-center justify-between rounded-md border px-3 py-2 transition-colors',
                            !ready && !isSelected
                              ? 'border-quant-border opacity-50 cursor-not-allowed'
                              : isSelected
                                ? 'border-quant-gold/60 bg-quant-gold/5 cursor-pointer'
                                : 'border-quant-border hover:border-quant-gold/40 cursor-pointer'
                          )}
                        >
                          <div className="flex items-center gap-3">
                            <input
                              type="checkbox"
                              checked={isSelected}
                              disabled={!ready && !isSelected}
                              onChange={(e) => toggleExchange(ex.key, e.target.checked)}
                              className="h-4 w-4 accent-quant-gold"
                            />
                            <Globe className="h-4 w-4 text-muted-foreground" />
                            <div>
                              <div className="text-sm font-medium">{ex.label}</div>
                              <div className="text-[10px] text-muted-foreground">
                                {cfg?.enabled
                                  ? cfg?.has_credentials
                                    ? cfg.testnet
                                      ? t('arb.ui.configured-testnet')
                                      : t('arb.ui.configured')
                                    : t('arb.ui.missing-credentials')
                                  : t('arb.ui.not-enabled')}
                              </div>
                            </div>
                          </div>
                          {isSelected ? (
                            <span className="inline-flex items-center gap-1 text-xs text-green-400">
                              <CheckCircle2 className="h-3.5 w-3.5" />
                              {t('arb.ui.added')}
                            </span>
                          ) : (
                            !ready && <span className="text-[10px] text-muted-foreground">{t('arb.ui.not-ready')}</span>
                          )}
                        </label>
                      )
                    })}
                  </div>
                ) : (
                  <div className="text-sm text-muted-foreground">{t('arb.ui.loading-exchanges')}</div>
                )}
                <div className="mt-3 text-xs text-muted-foreground">
                  {t('arb.tri.save-hint').replace('{count}', String(effectiveExchanges.length))}
                </div>
              </div>
            </div>
          )}
            </div>
            {/* Footer */}
            <div className="flex items-center justify-end gap-2 px-6 py-4 border-t border-quant-border shrink-0">
              <button
                onClick={() => setShowConfig(false)}
                className="px-4 py-2 rounded-lg border border-quant-border text-xs hover:bg-quant-hover transition-colors"
              >
                {t('arb.ui.close')}
              </button>
              <button
                onClick={handleSaveConfig}
                disabled={updateConfigMut.isPending || !editConfig}
                className={cn(
                  'flex items-center gap-2 px-4 py-2 rounded-lg text-xs font-medium transition-colors',
                  updateConfigMut.isPending || !editConfig
                    ? 'bg-muted text-muted-foreground cursor-not-allowed'
                    : 'bg-quant-gold text-black hover:opacity-90'
                )}
              >
                {updateConfigMut.isPending ? (
                  <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <Save className="w-3.5 h-3.5" />
                )}
                {t('arb.ui.save-config')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Opportunities Table */}
      <SectionCard
        title={t('arb.tri.opportunities-title')}
        headerAction={opportunity ? <span className="text-xs text-muted-foreground">{t('arb.ui.latest-scan')}</span> : null}
      >
        {opportunity ? (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-quant-border text-left text-xs text-muted-foreground">
                  <th className="py-2 px-3 font-medium">{t('arb.tri.hdr-cycle')}</th>
                  <th className="py-2 px-3 font-medium">{t('arb.tri.hdr-exchange')}</th>
                  <th className="py-2 px-3 font-medium text-right">{t('arb.tri.hdr-start-qty')}</th>
                  <th className="py-2 px-3 font-medium text-right">{t('arb.tri.hdr-end-qty')}</th>
                  <th className="py-2 px-3 font-medium text-right">{t('arb.tri.hdr-net-profit')}</th>
                  <th className="py-2 px-3 font-medium text-right">{t('arb.tri.hdr-total-fees')}</th>
                  <th className="py-2 px-3 font-medium text-right">{t('arb.tri.hdr-leg-slippage')}</th>
                  <th className="py-2 px-3 font-medium text-center">{t('arb.ui.hdr-action')}</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  className={cn(
                    'border-b border-quant-border transition-colors',
                    opportunity.viable ? 'bg-green-500/5' : 'hover:bg-quant-bg-secondary/50'
                  )}
                >
                  <td className="py-3 px-3 font-medium">{opportunity.cycle.join(' → ')}</td>
                  <td className="py-3 px-3">{opportunity.exchange}</td>
                  <td className="py-3 px-3 text-right">
                    {opportunity.start_qty.toFixed(4)} {opportunity.start_asset}
                  </td>
                  <td className="py-3 px-3 text-right">
                    {opportunity.end_qty.toFixed(4)} {opportunity.start_asset}
                  </td>
                  <td className="py-3 px-3 text-right">
                    <span
                      className={cn('font-medium', opportunity.net_profit_pct >= 0 ? 'text-green-400' : 'text-red-400')}
                    >
                      {opportunity.net_profit_pct.toFixed(4)}%
                    </span>
                  </td>
                  <td className="py-3 px-3 text-right text-xs text-muted-foreground">
                    ${opportunity.total_fees.toFixed(4)}
                  </td>
                  <td className="py-3 px-3 text-right text-xs text-muted-foreground">
                    {opportunity.legs.map((leg, i) => (
                      <div
                        key={i}
                        className={leg.slippage_pct > (editConfig?.max_slippage_pct ?? 0.5) ? 'text-red-400' : ''}
                      >
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
                      {t('arb.ui.execute')}
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyState
            icon={<Triangle className="w-10 h-10 text-muted-foreground" />}
            title={t('arb.tri.no-opportunity')}
            description={isRunning ? t('arb.ui.scanning') : t('arb.ui.start-to-scan')}
          />
        )}
      </SectionCard>

      {/* Active Positions */}
      <SectionCard title={t('arb.ui.active-positions')}>
        <div className="space-y-2">
          {!positions || positions.length === 0 ? (
            <div className="text-sm text-muted-foreground text-center py-4">{t('arb.ui.no-active-positions')}</div>
          ) : (
            positions.map((pos: TriangularTrade, i: number) => (
              <div key={pos.id || i} className="flex items-center justify-between p-3 rounded-md bg-quant-bg-secondary">
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
                  <div className={cn('text-sm font-semibold', pos.net_profit > 0 ? 'text-green-400' : 'text-red-400')}>
                    {pos.net_profit > 0 ? '+' : ''}${pos.net_profit.toFixed(2)}
                  </div>
                  {isPositionActive(pos.status) && (
                    <div className="flex items-center justify-end gap-2">
                      <button
                        onClick={() => handleClosePosition(pos)}
                        disabled={closePositionMut.isPending}
                        className="inline-flex items-center px-2 py-1 rounded text-[10px] font-medium bg-quant-gold text-black hover:opacity-90 disabled:opacity-50"
                      >
                        {t('arb.ui.close-position')}
                      </button>
                      <button
                        onClick={() => handleFailPosition(pos)}
                        disabled={failPositionMut.isPending}
                        className="inline-flex items-center px-2 py-1 rounded text-[10px] font-medium bg-red-500/20 text-red-400 hover:bg-red-500/30 disabled:opacity-50"
                      >
                        {t('arb.ui.fail')}
                      </button>
                    </div>
                  )}
                </div>
              </div>
            ))
          )}
        </div>
      </SectionCard>

      {/* History modal */}
      {showHistory && (
        <div
          role="dialog"
          aria-modal="true"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
          onClick={() => setShowHistory(false)}
          onKeyDown={(e) => {
            if (e.key === 'Escape') setShowHistory(false)
          }}
          tabIndex={-1}
        >
          <div
            role="document"
            className="w-full max-w-2xl max-h-[80vh] flex flex-col rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
              <h3 className="text-sm font-bold">{t('arb.ui.history-record')}</h3>
              <button
                onClick={() => setShowHistory(false)}
                aria-label={t('arb.ui.close')}
                className="text-muted-foreground hover:text-foreground"
              >
                <X className="w-4 h-4" />
              </button>
            </div>
            <div className="flex-1 overflow-y-auto p-6">
              <div className="space-y-2">
                {!history || history.length === 0 ? (
                  <div className="text-sm text-muted-foreground text-center py-4">{t('arb.ui.no-history')}</div>
                ) : (
                  history.map((trade: TriangularTrade, i: number) => (
                    <div
                      key={trade.id || i}
                      className="flex items-center justify-between p-3 rounded-md bg-quant-bg-secondary"
                    >
                      <div className="flex items-center gap-3">
                        <CheckCircle2 className={cn('w-4 h-4', trade.net_profit > 0 ? 'text-green-400' : 'text-red-400')} />
                        <div>
                          <div className="text-sm font-medium">{trade.cycle.join(' → ')}</div>
                          <div className="text-xs text-muted-foreground">
                            {trade.exchange} · {trade.status}
                          </div>
                        </div>
                      </div>
                      <div className="text-right">
                        <div
                          className={cn('text-sm font-semibold', trade.net_profit > 0 ? 'text-green-400' : 'text-red-400')}
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
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
