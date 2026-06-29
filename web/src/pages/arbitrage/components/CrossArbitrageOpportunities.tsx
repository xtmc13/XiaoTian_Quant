import { cn } from '@/lib/utils'
import { SectionCard } from '@/components/ui/SectionCard'
import { EmptyState } from '@/components/ui/EmptyState'
import { ArrowLeftRight, RefreshCw, Zap } from 'lucide-react'
import type { ArbitrageConfig, ArbitrageOpportunity } from '@/types'
import type { UseMutationResult } from '@tanstack/react-query'
import { DEFAULT_CONFIG } from './useCrossArbitrage'

interface CrossArbitrageOpportunitiesProps {
  opportunity: ArbitrageOpportunity | null
  editConfig: ArbitrageConfig | null
  isRunning: boolean
  executeMut: UseMutationResult<
    unknown,
    Error,
    {
      symbol: string
      buy_exchange: string
      sell_exchange: string
      buy_price: number
      sell_price: number
      quantity: number
    },
    unknown
  >
  onExecute: (opp: ArbitrageOpportunity) => void
}

function OpportunityRow({
  opportunity,
  editConfig,
  onExecute,
  executePending,
}: {
  opportunity: ArbitrageOpportunity
  editConfig: ArbitrageConfig | null
  onExecute: (opp: ArbitrageOpportunity) => void
  executePending: boolean
}) {
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
          <div className="text-[10px] text-muted-foreground">实 {opportunity.executable_buy_price.toFixed(2)}</div>
        ) : null}
      </td>
      <td className="py-3 px-3 text-right">
        <div>${opportunity.sell_price?.toFixed(2) ?? '-'}</div>
        {opportunity.executable_sell_price ? (
          <div className="text-[10px] text-muted-foreground">实 {opportunity.executable_sell_price.toFixed(2)}</div>
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
        {qtyChanged && <div className="text-[10px] text-quant-gold">→ {adjustedQty.toFixed(4)}</div>}
      </td>
      <td className="py-3 px-3 text-right text-xs">{opportunity.max_executable_qty?.toFixed(4) ?? '-'}</td>
      <td className="py-3 px-3 text-right">
        <div className={cn('font-medium', estimatedProfit >= 0 ? 'text-green-400' : 'text-red-400')}>
          ${estimatedProfit.toFixed(2)}
        </div>
        {!isViable && (
          <div className="text-[10px] text-yellow-400">{opportunity.viable === false ? '深度不足' : '未达阈值'}</div>
        )}
      </td>
      <td className="py-3 px-3 text-center">
        <button
          onClick={() => onExecute(opportunity)}
          disabled={executePending || !isViable}
          className={cn(
            'inline-flex items-center gap-1 px-2.5 py-1 rounded text-xs font-medium transition-colors',
            executePending || !isViable
              ? 'bg-muted text-muted-foreground cursor-not-allowed'
              : 'bg-quant-gold text-black hover:opacity-90'
          )}
        >
          {executePending ? <RefreshCw className="w-3 h-3 animate-spin" /> : <Zap className="w-3 h-3" />}
          执行
        </button>
      </td>
    </tr>
  )
}

export function CrossArbitrageOpportunities({
  opportunity,
  editConfig,
  isRunning,
  onExecute,
  executeMut,
}: CrossArbitrageOpportunitiesProps) {
  return (
    <SectionCard
      title="套利机会"
      headerAction={opportunity ? <span className="text-xs text-muted-foreground">最新扫描结果</span> : null}
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
              <OpportunityRow
                opportunity={opportunity}
                editConfig={editConfig}
                onExecute={onExecute}
                executePending={executeMut.isPending}
              />
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
  )
}
