import { useState } from 'react'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { toast } from '@/lib/useToast'
import { strategyApi } from '@/lib/api'
import { createDefaultAddPositions, strategyTypeToMultiplierPreset } from '@/lib/strategyUtils'

interface StrategyBatchEditModalProps {
  ids: string[]
  market: 'spot' | 'contract'
  onClose: () => void
  onSaved: () => void
}

const inputCls =
  'w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold'

export function StrategyBatchEditModal({ ids, market, onClose, onSaved }: StrategyBatchEditModalProps) {
  const [loading, setLoading] = useState(false)
  const [fields, setFields] = useState({
    leverage: { enabled: false, value: market === 'contract' ? 10 : 1 },
    firstOrderAmount: { enabled: false, value: market === 'contract' ? 5 : 10 },
    orderCount: { enabled: false, value: market === 'contract' ? 9 : 7 },
    takeProfitRatio: { enabled: false, value: 1.3 },
    stopLossRatio: { enabled: false, value: market === 'contract' ? 40 : 0 },
    waterfallProtection: { enabled: false, value: 2 },
  })

  const toggleField = (key: keyof typeof fields) => {
    setFields((prev) => ({ ...prev, [key]: { ...prev[key], enabled: !prev[key].enabled } }))
  }

  const updateValue = (key: keyof typeof fields, value: number) => {
    setFields((prev) => ({ ...prev, [key]: { ...prev[key], value } }))
  }

  const handleSubmit = async () => {
    if (ids.length === 0) return
    const enabledEntries = Object.entries(fields).filter(([, v]) => v.enabled)
    if (enabledEntries.length === 0) {
      toast('info', '请至少选择一个要修改的字段')
      return
    }
    setLoading(true)
    const updates: Record<string, unknown> = {}
    enabledEntries.forEach(([k, v]) => {
      switch (k) {
        case 'leverage':
          updates.leverage = v.value
          break
        case 'firstOrderAmount':
          updates.first_order_amount = v.value
          break
        case 'orderCount':
          updates.order_count = v.value
          break
        case 'takeProfitRatio':
          updates.take_profit_ratio = v.value
          break
        case 'stopLossRatio':
          updates.stop_loss_ratio = v.value
          break
        case 'waterfallProtection':
          updates.waterfall_protection = v.value
          break
      }
    })

    const results = await Promise.allSettled(
      ids.map(async (id) => {
        const current = await strategyApi.get(id)
        const parsed = current.config_json ? JSON.parse(current.config_json) : {}
        const nextConfig = { ...parsed, ...updates }
        if (updates.order_count != null) {
          const strategyType = (current.strategy_type || parsed.strategy_type || 'martin_trend') as string
          const isSpot = current.market_type === 'spot' || current.category === 'spot' || parsed.market_type === 'spot'
          const preset = strategyTypeToMultiplierPreset(strategyType, isSpot ? 'spot' : 'contract')
          nextConfig.add_positions = createDefaultAddPositions(preset, updates.order_count as number)
        }
        await strategyApi.update(id, { config_json: JSON.stringify(nextConfig) })
      })
    )

    const succeeded = results.filter((r) => r.status === 'fulfilled').length
    const failed = results.filter((r) => r.status === 'rejected').length
    setLoading(false)

    if (failed === 0) {
      toast('success', `批量修改成功: ${succeeded} 个策略`)
    } else {
      toast('warning', `批量修改完成: ${succeeded} 成功, ${failed} 失败`)
    }
    onSaved()
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
    >
      <div className="w-full max-w-md rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden">
        <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border">
          <h3 className="text-sm font-bold">批量修改 ({ids.length} 个策略)</h3>
          <button onClick={onClose} aria-label="关闭" className="text-muted-foreground hover:text-foreground">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="p-6 space-y-4">
          <p className="text-xs text-muted-foreground">勾选要修改的字段，未勾选的字段将保持原值。</p>

          <div className="space-y-3">
            {market === 'contract' && (
              <FieldRow label="杠杆" enabled={fields.leverage.enabled} onToggle={() => toggleField('leverage')}>
                <input
                  type="number"
                  min={1}
                  max={100}
                  value={fields.leverage.value}
                  disabled={!fields.leverage.enabled}
                  onChange={(e) => updateValue('leverage', Number(e.target.value))}
                  className={cn(inputCls, !fields.leverage.enabled && 'opacity-40')}
                />
              </FieldRow>
            )}

            <FieldRow
              label="首单额度 (USDT)"
              enabled={fields.firstOrderAmount.enabled}
              onToggle={() => toggleField('firstOrderAmount')}
            >
              <input
                type="number"
                min={1}
                value={fields.firstOrderAmount.value}
                disabled={!fields.firstOrderAmount.enabled}
                onChange={(e) => updateValue('firstOrderAmount', Number(e.target.value))}
                className={cn(inputCls, !fields.firstOrderAmount.enabled && 'opacity-40')}
              />
            </FieldRow>

            <FieldRow label="做单数量" enabled={fields.orderCount.enabled} onToggle={() => toggleField('orderCount')}>
              <input
                type="number"
                min={1}
                max={20}
                value={fields.orderCount.value}
                disabled={!fields.orderCount.enabled}
                onChange={(e) => updateValue('orderCount', Number(e.target.value))}
                className={cn(inputCls, !fields.orderCount.enabled && 'opacity-40')}
              />
            </FieldRow>

            <FieldRow
              label="止盈比例 (%)"
              enabled={fields.takeProfitRatio.enabled}
              onToggle={() => toggleField('takeProfitRatio')}
            >
              <input
                type="number"
                min={0.1}
                max={50}
                step={0.1}
                value={fields.takeProfitRatio.value}
                disabled={!fields.takeProfitRatio.enabled}
                onChange={(e) => updateValue('takeProfitRatio', Number(e.target.value))}
                className={cn(inputCls, !fields.takeProfitRatio.enabled && 'opacity-40')}
              />
            </FieldRow>

            <FieldRow
              label="止损比例 (%)"
              enabled={fields.stopLossRatio.enabled}
              onToggle={() => toggleField('stopLossRatio')}
            >
              <input
                type="number"
                min={0}
                max={100}
                step={0.1}
                value={fields.stopLossRatio.value}
                disabled={!fields.stopLossRatio.enabled}
                onChange={(e) => updateValue('stopLossRatio', Number(e.target.value))}
                className={cn(inputCls, !fields.stopLossRatio.enabled && 'opacity-40')}
              />
            </FieldRow>

            <FieldRow
              label="防瀑布 (%)"
              enabled={fields.waterfallProtection.enabled}
              onToggle={() => toggleField('waterfallProtection')}
            >
              <input
                type="number"
                min={0.5}
                max={20}
                step={0.5}
                value={fields.waterfallProtection.value}
                disabled={!fields.waterfallProtection.enabled}
                onChange={(e) => updateValue('waterfallProtection', Number(e.target.value))}
                className={cn(inputCls, !fields.waterfallProtection.enabled && 'opacity-40')}
              />
            </FieldRow>
          </div>
        </div>

        <div className="flex justify-end gap-2 px-6 py-4 border-t border-quant-border">
          <button
            onClick={onClose}
            disabled={loading}
            className="px-4 py-2 rounded-lg text-xs border border-quant-border text-muted-foreground hover:text-foreground transition-colors"
          >
            取消
          </button>
          <button
            onClick={handleSubmit}
            disabled={loading}
            className={cn(
              'px-4 py-2 rounded-lg text-xs font-medium transition-colors',
              loading ? 'bg-quant-gold/30 text-quant-gold cursor-wait' : 'bg-quant-gold text-black hover:opacity-90'
            )}
          >
            {loading ? '保存中...' : '确认修改'}
          </button>
        </div>
      </div>
    </div>
  )
}

function FieldRow({
  label,
  enabled,
  onToggle,
  children,
}: {
  label: string
  enabled: boolean
  onToggle: () => void
  children: React.ReactNode
}) {
  return (
    <div className="flex items-center gap-3">
      <label className="flex items-center gap-2 shrink-0">
        <input type="checkbox" checked={enabled} onChange={onToggle} className="rounded" />
        <span className={cn('text-xs', enabled ? 'text-foreground' : 'text-muted-foreground')}>{label}</span>
      </label>
      <div className="flex-1">{children}</div>
    </div>
  )
}
