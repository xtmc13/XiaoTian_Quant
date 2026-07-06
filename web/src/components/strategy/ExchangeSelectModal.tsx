import { useState, useEffect } from 'react'
import { cn } from '@/lib/utils'
import { Globe, CheckCircle2, X } from 'lucide-react'
import type { ExchangeConfiguredStatus } from '@/types'

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

interface ExchangeSelectModalProps {
  open: boolean
  onClose: () => void
  value: string[]
  onChange: (next: string[]) => void
  configuredExchanges?: Record<string, ExchangeConfiguredStatus>
}

export function ExchangeSelectModal({ open, onClose, value, onChange, configuredExchanges }: ExchangeSelectModalProps) {
  const [draft, setDraft] = useState<string[]>(value)

  useEffect(() => {
    if (open) setDraft(value)
  }, [open, value])

  if (!open) return null

  const toggleExchange = (key: string) => {
    setDraft((prev) => (prev.includes(key) ? prev.filter((k) => k !== key) : [...prev, key]))
  }

  const handleConfirm = () => {
    const hasChanged = draft.length !== value.length || draft.some((k, i) => k !== value[i])
    if (hasChanged) {
      onChange(draft)
    }
    onClose()
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
        className="w-full max-w-xl max-h-[90vh] flex flex-col rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
          <div>
            <h3 className="text-sm font-bold">选择交易所</h3>
            <p className="text-[10px] text-muted-foreground mt-0.5">请选择策略运行的交易所账号</p>
          </div>
          <button
            onClick={onClose}
            aria-label="关闭"
            className="w-8 h-8 rounded-lg border border-quant-border flex items-center justify-center text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors"
          >
            <X className="w-3.5 h-3.5" />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-6">
          {configuredExchanges ? (
            <div className="space-y-2">
              {SUPPORTED_EXCHANGES.map((ex) => {
                const cfg = configuredExchanges[ex.key]
                const selected = draft.includes(ex.key)
                const ready = cfg?.enabled && cfg?.has_credentials
                return (
                  <button
                    key={ex.key}
                    type="button"
                    disabled={!ready}
                    aria-pressed={selected}
                    onClick={() => ready && toggleExchange(ex.key)}
                    className={cn(
                      'w-full flex items-center justify-between rounded-lg border px-3 py-3 transition-colors text-left',
                      selected ? 'border-quant-gold bg-quant-gold/5' : 'border-quant-border hover:border-quant-gold/30',
                      !ready && 'opacity-50 cursor-not-allowed'
                    )}
                  >
                    <div className="flex items-center gap-3">
                      <Globe className="h-4 w-4 text-muted-foreground" />
                      <div>
                        <div className="text-xs font-medium">{ex.label}</div>
                        <div className="text-[10px] text-muted-foreground">
                          {cfg?.enabled
                            ? cfg?.has_credentials
                              ? `已配置${cfg.testnet ? ' · 测试网' : ''}`
                              : '缺少凭证'
                            : '未启用'}
                        </div>
                      </div>
                    </div>
                    {selected && (
                      <span className="inline-flex items-center gap-1 text-xs text-quant-gold">
                        <CheckCircle2 className="h-3.5 w-3.5" />
                        已选择
                      </span>
                    )}
                  </button>
                )
              })}
            </div>
          ) : (
            <div className="text-sm text-muted-foreground text-center py-4">加载交易所配置中...</div>
          )}
          {configuredExchanges && !Object.values(configuredExchanges).some((c) => c.enabled && c.has_credentials) && (
            <div className="mt-3 text-xs text-yellow-400">
              系统中没有可用的交易所配置。请先在 Settings / 交易所账号 中配置 API Key。
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between px-6 py-4 border-t border-quant-border shrink-0">
          <div className="text-[11px] text-muted-foreground">
            已选择 <span className="text-foreground font-mono">{draft.length}</span> 个交易所
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 rounded-lg border border-quant-border text-xs hover:bg-quant-hover transition-colors"
            >
              取消
            </button>
            <button
              type="button"
              onClick={handleConfirm}
              className="px-4 py-2 rounded-lg text-xs font-medium bg-quant-gold text-white hover:opacity-90 transition-opacity"
            >
              确认选择
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
