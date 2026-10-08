import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Hand, AlertTriangle, TrendingDown, PlusCircle, Scissors, Power } from 'lucide-react'
import { strategyApi } from '@/lib/api'
import type { StrategyManualActionRequest } from '@/types'
import type { StrategyItem, StrategyRuntimeStatus } from '@/types'
import { useToastStore } from '@/stores/toastStore'
import { cn } from '@/lib/utils'

type ConfirmTarget =
  | { kind: 'close_all' }
  | { kind: 'add_position'; amount: number }
  | { kind: 'reduce_position'; qty?: number; ratio?: number }

/**
 * G1：CRA 运行时手动操控区（币富名词解释 #23/#24/#25/#28）——
 * 清仓卖出 / 一键补仓 / 关闭·开启补仓 / 自定义减仓。
 * 仅当后端 RuntimeStatus 透出 add_position_enabled（=CRA 引擎实例）时由
 * RuntimePanel 挂载；所有危险动作两步确认，结果 toast 透出后端 detail。
 */
export function ManualActionPanel({
  strategy,
  status,
  price,
}: {
  strategy: StrategyItem
  status: StrategyRuntimeStatus
  price?: number
}) {
  const qc = useQueryClient()
  const addToast = useToastStore((s) => s.addToast)
  const [confirming, setConfirming] = useState<ConfirmTarget | null>(null)
  const [addAmount, setAddAmount] = useState('')
  const [reduceMode, setReduceMode] = useState<'ratio' | 'qty'>('ratio')
  const [reduceInput, setReduceInput] = useState('')

  const inPos = !!status.in_position
  const qty = status.position_qty ?? status.quantity ?? 0
  const avg = status.avg_entry_price || status.entry_price
  const isShort = status.direction === 'short' || status.direction === 'SHORT'
  const addEnabled = status.add_position_enabled !== false
  const entryPaused = !!status.entry_paused

  // 浮动盈亏（与 RuntimePanel KPI 同口径）。
  let pnlAmt: number | null = null
  if (inPos && price && avg && avg > 0) {
    pnlAmt = (price - avg) * qty
    if (isShort) pnlAmt = -pnlAmt
  }

  const mut = useMutation({
    mutationFn: (req: StrategyManualActionRequest) => strategyApi.manualAction(strategy.id, req),
    onSuccess: (resp) => {
      addToast({ type: 'success', message: resp?.detail || '操控指令已发出', duration: 5000, }, { bypassCooldown: true })
      setConfirming(null)
      setAddAmount('')
      setReduceInput('')
      void qc.invalidateQueries({ queryKey: ['strategy-runtime', strategy.id] })
      void qc.invalidateQueries({ queryKey: ['strategies'] })
    },
    // 错误 toast 由 api 拦截器统一弹出（detail 透出后端口径），这里不重复。
  })
  const busy = mut.isPending

  const fire = (target: ConfirmTarget) => {
    switch (target.kind) {
      case 'close_all':
        mut.mutate({ action: 'close_all' })
        break
      case 'add_position':
        mut.mutate({ action: 'add_position', amount: target.amount })
        break
      case 'reduce_position':
        mut.mutate({ action: 'reduce_position', qty: target.qty, ratio: target.ratio })
        break
    }
  }

  const amount = parseFloat(addAmount)
  const amountOk = isFinite(amount) && amount > 0
  const reduceVal = parseFloat(reduceInput)
  const reduceOk =
    isFinite(reduceVal) &&
    reduceVal > 0 &&
    (reduceMode === 'ratio' ? reduceVal < 100 : qty > 0 && reduceVal < qty)
  const reducePreviewQty =
    reduceOk && qty > 0 ? (reduceMode === 'ratio' ? (qty * reduceVal) / 100 : reduceVal) : null

  const dirLabel = !inPos ? '空仓' : isShort ? '做空' : '做多/买入'

  return (
    <div className="rounded-lg bg-quant-bg border border-quant-border p-3 space-y-2" data-testid="manual-action-panel">
      <div className="flex items-center justify-between text-[11px]">
        <span className="text-muted-foreground flex items-center gap-1.5">
          <Hand className="w-3 h-3 text-quant-gold" />
          手动操控
        </span>
        {entryPaused && (
          <span className="text-[10px] px-1.5 py-0.5 rounded bg-quant-red/10 border border-quant-red/30 text-quant-red">
            已暂停新开仓（重启策略恢复）
          </span>
        )}
      </div>

      {/* 关闭/开启补仓（币富 #25）：开关态如实显示当前状态 */}
      <div className="flex items-center justify-between gap-2 text-[11px]">
        <span className="text-muted-foreground">
          自动补仓
          <span className={cn('ml-1.5 font-mono', addEnabled ? 'text-quant-green' : 'text-quant-red')}>
            {addEnabled ? '开启中' : '已关闭'}
          </span>
          {status.manual_add_count != null && status.manual_add_count > 0 && (
            <span className="ml-1.5 text-[10px]">· 本循环手动补 {status.manual_add_count} 笔</span>
          )}
        </span>
        <button
          className="px-2.5 py-1 rounded border border-quant-border text-[11px] text-foreground hover:border-quant-gold/40 transition-colors disabled:opacity-40"
          disabled={busy}
          onClick={() => mut.mutate({ action: 'toggle_add_position', enabled: !addEnabled })}
        >
          <Power className="w-3 h-3 inline mr-1" />
          {addEnabled ? '关闭补仓' : '开启补仓'}
        </button>
      </div>

      {/* 一键补仓（币富 #24）+ 自定义减仓（币富 #28）输入行 */}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
        <div className="flex items-center gap-1.5">
          <input
            type="number"
            min="0"
            step="any"
            value={addAmount}
            onChange={(e) => setAddAmount(e.target.value)}
            placeholder="补仓金额 USDT"
            aria-label="补仓金额"
            className="flex-1 min-w-0 px-2 py-1.5 rounded bg-quant-bg-tertiary border border-quant-border text-[11px] font-mono text-foreground placeholder:text-muted-foreground/60 focus:outline-none focus:border-quant-gold/50"
          />
          <button
            className="px-2.5 py-1.5 rounded border border-quant-gold/40 text-quant-gold text-[11px] hover:bg-quant-gold/10 transition-colors disabled:opacity-40"
            disabled={busy || !inPos || !amountOk}
            title={!inPos ? '当前无持仓' : undefined}
            onClick={() => setConfirming({ kind: 'add_position', amount })}
          >
            <PlusCircle className="w-3 h-3 inline mr-1" />
            一键补仓
          </button>
        </div>
        <div className="flex items-center gap-1.5">
          <select
            value={reduceMode}
            onChange={(e) => setReduceMode(e.target.value as 'ratio' | 'qty')}
            aria-label="减仓方式"
            className="px-1.5 py-1.5 rounded bg-quant-bg-tertiary border border-quant-border text-[11px] text-foreground focus:outline-none"
          >
            <option value="ratio">比例%</option>
            <option value="qty">数量</option>
          </select>
          <input
            type="number"
            min="0"
            step="any"
            value={reduceInput}
            onChange={(e) => setReduceInput(e.target.value)}
            placeholder={reduceMode === 'ratio' ? '如 50 = 减半' : '减仓数量'}
            aria-label="减仓数值"
            className="flex-1 min-w-0 px-2 py-1.5 rounded bg-quant-bg-tertiary border border-quant-border text-[11px] font-mono text-foreground placeholder:text-muted-foreground/60 focus:outline-none focus:border-quant-gold/50"
          />
          <button
            className="px-2.5 py-1.5 rounded border border-quant-border text-foreground text-[11px] hover:border-quant-red/50 hover:text-quant-red transition-colors disabled:opacity-40"
            disabled={busy || !inPos || !reduceOk}
            title={!inPos ? '当前无持仓' : undefined}
            onClick={() =>
              setConfirming(
                reduceMode === 'ratio'
                  ? { kind: 'reduce_position', ratio: reduceVal / 100 }
                  : { kind: 'reduce_position', qty: reduceVal }
              )
            }
          >
            <Scissors className="w-3 h-3 inline mr-1" />
            减仓
          </button>
        </div>
      </div>

      {/* 清仓卖出（币富 #23，danger） */}
      <button
        className="w-full px-2.5 py-1.5 rounded border border-quant-red/40 text-quant-red text-[11px] font-medium hover:bg-quant-red/10 transition-colors disabled:opacity-40"
        disabled={busy || !inPos}
        title={!inPos ? '当前无持仓' : undefined}
        onClick={() => setConfirming({ kind: 'close_all' })}
      >
        <TrendingDown className="w-3 h-3 inline mr-1" />
        清仓卖出（市价全平并暂停新开仓）
      </button>

      {/* 两步确认区 */}
      {confirming && (
        <div className="rounded border border-quant-red/30 bg-quant-red/5 p-2.5 space-y-2" role="alertdialog" aria-label="确认手动操控">
          <div className="flex items-start gap-1.5 text-[11px] text-foreground">
            <AlertTriangle className="w-3.5 h-3.5 text-quant-red shrink-0 mt-0.5" />
            <div className="space-y-0.5">
              {confirming.kind === 'close_all' && (
                <>
                  <div className="font-semibold text-quant-red">确认清仓卖出？</div>
                  <div className="text-muted-foreground">
                    持仓摘要：{dirLabel} {qty.toLocaleString('en-US', { maximumFractionDigits: 6 })} @ 均价{' '}
                    {avg ? avg.toLocaleString('en-US', { maximumFractionDigits: 6 }) : '-'}
                    {price ? ` · 现价 ${price.toLocaleString('en-US', { maximumFractionDigits: 6 })}` : ''}
                    {pnlAmt != null && (
                      <span className={pnlAmt >= 0 ? ' text-quant-green' : ' text-quant-red'}>
                        {' '}
                        · 浮动盈亏 {pnlAmt >= 0 ? '+' : '-'}${Math.abs(pnlAmt).toFixed(2)}
                      </span>
                    )}
                  </div>
                  <div className="text-muted-foreground">将按市价全平当前持仓，此后策略保持运行但暂停新开仓（重启策略恢复）。</div>
                </>
              )}
              {confirming.kind === 'add_position' && (
                <>
                  <div className="font-semibold">确认一键补仓？</div>
                  <div className="text-muted-foreground">
                    将立即按市价买入 {confirming.amount.toLocaleString('en-US', { maximumFractionDigits: 2 })} USDT
                    保证金仓位（计入持仓与均价，不占自动补仓梯档）。
                  </div>
                </>
              )}
              {confirming.kind === 'reduce_position' && (
                <>
                  <div className="font-semibold">确认自定义减仓？</div>
                  <div className="text-muted-foreground">
                    将按市价卖出约{' '}
                    {(reducePreviewQty ?? 0).toLocaleString('en-US', { maximumFractionDigits: 6 })}（当前持仓{' '}
                    {qty.toLocaleString('en-US', { maximumFractionDigits: 6 })}），从首档起核销。
                  </div>
                </>
              )}
            </div>
          </div>
          <div className="flex items-center justify-end gap-2">
            <button
              className="px-3 py-1 rounded border border-quant-border text-[11px] text-muted-foreground hover:text-foreground transition-colors"
              disabled={busy}
              onClick={() => setConfirming(null)}
            >
              取消
            </button>
            <button
              className={cn(
                'px-3 py-1 rounded text-[11px] font-medium transition-colors',
                confirming.kind === 'close_all'
                  ? 'bg-quant-red text-white hover:opacity-90'
                  : 'bg-quant-gold text-white hover:opacity-90'
              )}
              disabled={busy}
              onClick={() => fire(confirming)}
            >
              {busy ? '提交中...' : '确认执行'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

export default ManualActionPanel
