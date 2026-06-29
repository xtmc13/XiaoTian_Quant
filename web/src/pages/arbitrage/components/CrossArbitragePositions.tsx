import { cn } from '@/lib/utils'
import { SectionCard } from '@/components/ui/SectionCard'
import { ArrowLeftRight } from 'lucide-react'
import type { ArbitragePosition } from '@/types'
import type { UseMutationResult } from '@tanstack/react-query'

interface CrossArbitragePositionsProps {
  positions: ArbitragePosition[] | undefined
  isPositionActive: (status: string) => boolean
  onClosePosition: (pos: ArbitragePosition) => void
  onFailPosition: (pos: ArbitragePosition) => void
  closePositionMut: UseMutationResult<unknown, Error, { id: string; sell_price: number }, unknown>
  failPositionMut: UseMutationResult<unknown, Error, string, unknown>
}

export function CrossArbitragePositions({
  positions,
  isPositionActive,
  onClosePosition,
  onFailPosition,
  closePositionMut,
  failPositionMut,
}: CrossArbitragePositionsProps) {
  return (
    <SectionCard title="活跃持仓">
      <div className="space-y-2">
        {!positions || positions.length === 0 ? (
          <div className="text-sm text-muted-foreground text-center py-4">无活跃持仓</div>
        ) : (
          positions.map((pos: ArbitragePosition, i: number) => (
            <div key={pos.id || i} className="flex items-center justify-between p-3 rounded-md bg-quant-bg-secondary">
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
                <div className={cn('text-sm font-semibold', pos.net_profit > 0 ? 'text-green-400' : 'text-red-400')}>
                  {pos.net_profit > 0 ? '+' : ''}${pos.net_profit.toFixed(2)}
                </div>
                {isPositionActive(pos.status) && (
                  <div className="flex items-center justify-end gap-2">
                    <button
                      onClick={() => onClosePosition(pos)}
                      disabled={closePositionMut.isPending}
                      className="inline-flex items-center px-2 py-1 rounded text-[10px] font-medium bg-quant-gold text-black hover:opacity-90 disabled:opacity-50"
                    >
                      平仓
                    </button>
                    <button
                      onClick={() => onFailPosition(pos)}
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
  )
}
