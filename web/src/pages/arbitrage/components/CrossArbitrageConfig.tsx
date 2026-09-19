import { useEffect, useState } from 'react'
import { cn } from '@/lib/utils'
import { RefreshCw, Save, Globe, CheckCircle2, X } from 'lucide-react'
import { TextInput, NumberInput, Toggle } from './ArbitrageUI'
import type { ArbitrageConfig, ExchangeConfiguredStatus } from '@/types'
import { useI18n } from '@/i18n'

const SUPPORTED_EXCHANGES = [
  { key: 'binance', label: 'Binance', needsPassphrase: false, supportsTestnet: true },
  { key: 'okx', label: 'OKX', needsPassphrase: true, supportsTestnet: true },
  { key: 'mexc', label: 'MEXC', needsPassphrase: false, supportsTestnet: false },
  { key: 'gate', label: 'Gate.io', needsPassphrase: false, supportsTestnet: false },
  { key: 'bybit', label: 'Bybit', needsPassphrase: false, supportsTestnet: true },
  { key: 'coinbase', label: 'Coinbase', needsPassphrase: false, supportsTestnet: false },
  { key: 'kraken', label: 'Kraken', needsPassphrase: false, supportsTestnet: false },
  { key: 'bitget', label: 'Bitget', needsPassphrase: true, supportsTestnet: false },
] as const

interface CrossArbitrageConfigProps {
  open: boolean
  onClose: () => void
  editConfig: ArbitrageConfig | null
  setEditConfig: (cfg: ArbitrageConfig | null) => void
  symbolsInput: string
  setSymbolsInput: (v: string) => void
  configuredExchanges: Record<string, ExchangeConfiguredStatus> | undefined
  exchangesMeta: { registered_count?: number; exchanges?: string[] } | undefined
  onSave: (selectedExchanges: string[]) => void
  isSaving: boolean
}

function ConfigField({ label, input, fieldKey }: { label: string; input: React.ReactNode; fieldKey?: string }) {
  return (
    <div key={fieldKey}>
      <label className="mb-1.5 block text-xs text-muted-foreground">{label}</label>
      {input}
    </div>
  )
}

export function CrossArbitrageConfig({
  open,
  onClose,
  editConfig,
  setEditConfig,
  symbolsInput,
  setSymbolsInput,
  configuredExchanges,
  exchangesMeta,
  onSave,
  isSaving,
}: CrossArbitrageConfigProps) {
  const { t } = useI18n()
  // null = 用户未改动，跟随引擎已注册列表；改动后跟随用户选择
  const [touchedSelection, setTouchedSelection] = useState<string[] | null>(null)
  useEffect(() => {
    if (!open) setTouchedSelection(null)
  }, [open])

  const selected = touchedSelection ?? exchangesMeta?.exchanges ?? []
  const toggleExchange = (key: string, checked: boolean) => {
    setTouchedSelection(checked ? [...selected, key] : selected.filter((k) => k !== key))
  }

  if (!open) return null

  if (!editConfig) {
    return (
      <div
        role="dialog"
        aria-modal="true"
        className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
        onClick={onClose}
      >
        <div
          role="document"
          className="w-full max-w-2xl rounded-2xl border border-quant-border bg-quant-card p-6 shadow-2xl"
          onClick={(e) => e.stopPropagation()}
        >
          <div className="text-sm text-muted-foreground text-center py-4">{t('arb.ui.loading-config')}</div>
        </div>
      </div>
    )
  }

  const updateField = <K extends keyof ArbitrageConfig>(field: K, value: ArbitrageConfig[K]) => {
    setEditConfig(editConfig ? { ...editConfig, [field]: value } : null)
  }

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
        className="w-full max-w-4xl max-h-[90vh] flex flex-col rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
          <h3 className="text-sm font-bold">{t('arb.cross.config-title')}</h3>
          <button onClick={onClose} aria-label={t('arb.ui.close')} className="text-muted-foreground hover:text-foreground">
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-6">
          <div className="space-y-6">
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              <ConfigField
                label={t('arb.ui.symbols')}
                input={<TextInput value={symbolsInput} onChange={setSymbolsInput} placeholder="BTCUSDT,ETHUSDT" />}
                fieldKey="symbol"
              />
              <ConfigField
                label={t('arb.cross.min-spread')}
                input={
                  <NumberInput
                    value={editConfig.min_spread_pct}
                    onChange={(v) => updateField('min_spread_pct', v)}
                    min={0}
                    step={0.01}
                  />
                }
                fieldKey="min_spread_pct"
              />
              <ConfigField
                label={t('arb.cross.order-size')}
                input={
                  <NumberInput
                    value={editConfig.order_size}
                    onChange={(v) => updateField('order_size', v)}
                    min={0}
                    step={0.001}
                  />
                }
                fieldKey="order_size"
              />
              <ConfigField
                label={t('arb.ui.max-positions')}
                input={
                  <NumberInput
                    value={editConfig.max_positions}
                    onChange={(v) => updateField('max_positions', Math.floor(v))}
                    min={1}
                    step={1}
                  />
                }
                fieldKey="max_positions"
              />
              <ConfigField
                label={t('arb.cross.fee-a')}
                input={
                  <NumberInput
                    value={editConfig.fee_a}
                    onChange={(v) => updateField('fee_a', v)}
                    min={0}
                    step={0.0001}
                  />
                }
                fieldKey="fee_a"
              />
              <ConfigField
                label={t('arb.cross.fee-b')}
                input={
                  <NumberInput
                    value={editConfig.fee_b}
                    onChange={(v) => updateField('fee_b', v)}
                    min={0}
                    step={0.0001}
                  />
                }
                fieldKey="fee_b"
              />
              <ConfigField
                label={t('arb.cross.poll-interval')}
                input={
                  <NumberInput
                    value={editConfig.poll_interval}
                    onChange={(v) => updateField('poll_interval', Math.floor(v))}
                    min={1}
                    step={1}
                  />
                }
                fieldKey="poll_interval"
              />
              <ConfigField
                label={t('arb.ui.max-slippage')}
                input={
                  <NumberInput
                    value={editConfig.max_slippage_pct}
                    onChange={(v) => updateField('max_slippage_pct', v)}
                    min={0}
                    step={0.01}
                  />
                }
                fieldKey="max_slippage_pct"
              />
              <div className="flex items-center gap-6 md:col-span-2 flex-wrap">
                <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                  <Toggle value={editConfig.auto_execute} onChange={(v) => updateField('auto_execute', v)} />
                  {t('arb.ui.auto-execute')}
                </label>
                <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                  <Toggle value={editConfig.dry_run} onChange={(v) => updateField('dry_run', v)} />
                  {t('arb.ui.dry-run')}
                </label>
                <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                  <Toggle
                    value={editConfig.adaptive_qty_enabled}
                    onChange={(v) => updateField('adaptive_qty_enabled', v)}
                  />
                  {t('arb.ui.adaptive-qty')}
                </label>
              </div>
              {editConfig.adaptive_qty_enabled && (
                <>
                  <ConfigField
                    label={t('arb.ui.min-order-qty')}
                    input={
                      <NumberInput
                        value={editConfig.min_order_qty}
                        onChange={(v) => updateField('min_order_qty', v)}
                        min={0}
                        step={0.0001}
                      />
                    }
                    fieldKey="min_order_qty"
                  />
                  <ConfigField
                    label={t('arb.cross.min-order-value')}
                    input={
                      <NumberInput
                        value={editConfig.min_order_value}
                        onChange={(v) => updateField('min_order_value', v)}
                        min={0}
                        step={1}
                      />
                    }
                    fieldKey="min_order_value"
                  />
                </>
              )}
            </div>

            {/* Exchange selection */}
            <div className="border-t border-quant-border pt-6">
              <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-4">{t('arb.ui.exchange-selection')}</h3>
              {configuredExchanges ? (
                <div className="space-y-2">
                  {SUPPORTED_EXCHANGES.map((ex) => {
                    const cfg = configuredExchanges[ex.key]
                    const isSelected = selected.includes(ex.key)
                    const canRegister = cfg?.enabled && cfg?.has_credentials
                    const disabled = !isSelected && !canRegister
                    return (
                      <label
                        key={ex.key}
                        className={cn(
                          'flex items-center justify-between rounded-md border px-3 py-2 transition-colors',
                          disabled
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
                            disabled={disabled}
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
                        {isSelected && (
                          <span className="inline-flex items-center gap-1 text-xs text-green-400">
                            <CheckCircle2 className="h-3.5 w-3.5" />
                            {t('arb.ui.added-arb')}
                          </span>
                        )}
                      </label>
                    )
                  })}
                </div>
              ) : (
                <div className="text-sm text-muted-foreground">{t('arb.ui.loading-exchanges')}</div>
              )}
              {configuredExchanges &&
                !Object.values(configuredExchanges).some((c) => c.enabled && c.has_credentials) && (
                  <div className="mt-3 text-xs text-yellow-400">{t('arb.cross.no-exchange-warning')}</div>
                )}
              {exchangesMeta && (
                <div className="mt-3 text-xs text-muted-foreground">
                  {t('arb.cross.save-hint').replace('{count}', String(selected.length))}
                </div>
              )}
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 px-6 py-4 border-t border-quant-border shrink-0">
          <button
            onClick={onClose}
            className="px-4 py-2 rounded-lg border border-quant-border text-xs hover:bg-quant-hover transition-colors"
          >
            {t('arb.ui.close')}
          </button>
          <button
            onClick={() => onSave(selected)}
            disabled={isSaving}
            className={cn(
              'flex items-center gap-2 px-4 py-2 rounded-lg text-xs font-medium transition-colors',
              isSaving
                ? 'bg-muted text-muted-foreground cursor-not-allowed'
                : 'bg-quant-gold text-black hover:opacity-90'
            )}
          >
            {isSaving ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
            {t('arb.ui.save-config')}
          </button>
        </div>
      </div>
    </div>
  )
}
