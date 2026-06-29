import { cn } from '@/lib/utils'
import { SectionCard } from '@/components/ui/SectionCard'
import { RefreshCw, Save, Globe, CheckCircle2, Plus } from 'lucide-react'
import { TextInput, NumberInput, Toggle } from './ArbitrageUI'
import type { ArbitrageConfig, ExchangeConfiguredStatus } from '@/types'
import type { UseMutationResult } from '@tanstack/react-query'

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

interface CrossArbitrageConfigProps {
  editConfig: ArbitrageConfig | null
  setEditConfig: (cfg: ArbitrageConfig | null) => void
  symbolsInput: string
  setSymbolsInput: (v: string) => void
  configuredExchanges: Record<string, ExchangeConfiguredStatus> | undefined
  exchangesMeta: { registered_count?: number; exchanges?: string[] } | undefined
  onSave: () => void
  onRegister: (name: string) => void
  isSaving: boolean
  isRegistering: boolean
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
  editConfig,
  setEditConfig,
  symbolsInput,
  setSymbolsInput,
  configuredExchanges,
  exchangesMeta,
  onSave,
  onRegister,
  isSaving,
  isRegistering,
}: CrossArbitrageConfigProps) {
  if (!editConfig) {
    return (
      <SectionCard title="引擎配置">
        <div className="text-sm text-muted-foreground text-center py-4">加载配置中...</div>
      </SectionCard>
    )
  }

  const updateField = <K extends keyof ArbitrageConfig>(field: K, value: ArbitrageConfig[K]) => {
    setEditConfig(editConfig ? { ...editConfig, [field]: value } : null)
  }

  return (
    <SectionCard
      title="引擎配置"
      headerAction={
        <button
          onClick={onSave}
          disabled={isSaving}
          className={cn(
            'flex items-center gap-2 px-3 py-1.5 rounded-md text-xs font-medium transition-colors',
            isSaving ? 'bg-muted text-muted-foreground cursor-not-allowed' : 'bg-quant-gold text-black hover:opacity-90'
          )}
        >
          {isSaving ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
          保存配置
        </button>
      }
    >
      <div className="space-y-6">
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          <ConfigField
            label="交易对（逗号分隔）"
            input={<TextInput value={symbolsInput} onChange={setSymbolsInput} placeholder="BTCUSDT,ETHUSDT" />}
            fieldKey="symbol"
          />
          <ConfigField
            label="最小价差 (%)"
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
            label="订单数量"
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
            label="最大持仓数"
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
            label="买入所手续费 (小数)"
            input={
              <NumberInput value={editConfig.fee_a} onChange={(v) => updateField('fee_a', v)} min={0} step={0.0001} />
            }
            fieldKey="fee_a"
          />
          <ConfigField
            label="卖出所手续费 (小数)"
            input={
              <NumberInput value={editConfig.fee_b} onChange={(v) => updateField('fee_b', v)} min={0} step={0.0001} />
            }
            fieldKey="fee_b"
          />
          <ConfigField
            label="轮询间隔 (秒)"
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
            label="最大滑点 (%)"
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
              自动执行
            </label>
            <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
              <Toggle value={editConfig.dry_run} onChange={(v) => updateField('dry_run', v)} />
              模拟运行
            </label>
            <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
              <Toggle
                value={editConfig.adaptive_qty_enabled}
                onChange={(v) => updateField('adaptive_qty_enabled', v)}
              />
              自适应数量
            </label>
          </div>
          {editConfig.adaptive_qty_enabled && (
            <>
              <ConfigField
                label="最小订单数量"
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
                label="最小订单金额 (USD)"
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
          <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-4">交易所选择</h3>
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
                        onClick={() => onRegister(ex.key)}
                        disabled={isRegistering}
                        className={cn(
                          'inline-flex items-center gap-1 px-2.5 py-1 rounded text-xs font-medium transition-colors',
                          isRegistering
                            ? 'bg-muted text-muted-foreground cursor-not-allowed'
                            : 'bg-quant-gold text-black hover:opacity-90'
                        )}
                      >
                        {isRegistering ? <RefreshCw className="h-3 w-3 animate-spin" /> : <Plus className="h-3 w-3" />}
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
            <div className="text-sm text-muted-foreground">加载交易所配置中...</div>
          )}
          {configuredExchanges && !Object.values(configuredExchanges).some((c) => c.enabled && c.has_credentials) && (
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
    </SectionCard>
  )
}
