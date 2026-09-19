import { cn } from '@/lib/utils'
import { SectionCard } from '@/components/ui/SectionCard'
import { EmptyState } from '@/components/ui/EmptyState'
import { ArrowLeftRight, RefreshCw, Zap } from 'lucide-react'
import type { ArbitrageConfig, ArbitrageOpportunity } from '@/types'
import type { UseMutationResult } from '@tanstack/react-query'
import { DEFAULT_CONFIG } from './useCrossArbitrage'
import { useI18n } from '@/i18n'

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
  const { t } = useI18n()
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
            {t('arb.cross.exec-prefix')} {opportunity.executable_buy_price.toFixed(2)}
          </div>
        ) : null}
      </td>
      <td className="py-3 px-3 text-right">
        <div>${opportunity.sell_price?.toFixed(2) ?? '-'}</div>
        {opportunity.executable_sell_price ? (
          <div className="text-[10px] text-muted-foreground">
            {t('arb.cross.exec-prefix')} {opportunity.executable_sell_price.toFixed(2)}
          </div>
        ) : null}
      </td>
      <td className="py-3 px-3 text-right">
        <span className={cn('font-medium', netSpreadPct >= 0 ? 'text-green-400' : 'text-red-400')}>
          {netSpreadPct.toFixed(4)}%
        </span>
        <div className="text-[10px] text-muted-foreground">
          {t('arb.cross.gross-prefix')} {spreadPct.toFixed(4)}%
        </div>
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
          <div className="text-[10px] text-yellow-400">
            {opportunity.viable === false ? t('arb.cross.insufficient-depth') : t('arb.cross.below-threshold')}
          </div>
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
          {t('arb.ui.execute')}
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
  const { t } = useI18n()
  return (
    <SectionCard
      title={t('arb.cross.opportunities-title')}
      headerAction={opportunity ? <span className="text-xs text-muted-foreground">{t('arb.ui.latest-scan')}</span> : null}
    >
      {opportunity ? (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-quant-border text-left text-xs text-muted-foreground">
                <th className="py-2 px-3 font-medium">{t('arb.cross.hdr-symbol')}</th>
                <th className="py-2 px-3 font-medium">{t('arb.cross.hdr-buy-ex')}</th>
                <th className="py-2 px-3 font-medium">{t('arb.cross.hdr-sell-ex')}</th>
                <th className="py-2 px-3 font-medium text-right">{t('arb.cross.hdr-buy-price')}</th>
                <th className="py-2 px-3 font-medium text-right">{t('arb.cross.hdr-sell-price')}</th>
                <th className="py-2 px-3 font-medium text-right">{t('arb.cross.hdr-net-spread')}</th>
                <th className="py-2 px-3 font-medium text-right">{t('arb.cross.hdr-slippage')}</th>
                <th className="py-2 px-3 font-medium text-right">{t('arb.cross.hdr-qty')}</th>
                <th className="py-2 px-3 font-medium text-right">{t('arb.cross.hdr-max-exec')}</th>
                <th className="py-2 px-3 font-medium text-right">{t('arb.cross.hdr-est-profit')}</th>
                <th className="py-2 px-3 font-medium text-center">{t('arb.ui.hdr-action')}</th>
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
          title={t('arb.cross.no-opportunity')}
          description={isRunning ? t('arb.ui.scanning') : t('arb.ui.start-to-scan')}
        />
      )}
    </SectionCard>
  )
}
