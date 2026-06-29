import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { SectionCard } from '@/components/ui/SectionCard'
import { Badge } from '@/components/ui/Badge'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { DataTable } from '@/components/DataTable'
import { rustEngineApi, marketApi } from '@/lib/api'
import { Cpu, Activity } from 'lucide-react'
import { cn } from '@/lib/utils'

const DEFAULT_SYMBOL = 'BTCUSDT'

export function RustEnginePanel() {
  const [symbol] = useState(DEFAULT_SYMBOL)

  const { data: snapshot, isLoading: snapshotLoading, error: snapshotError } = useQuery({
    queryKey: ['rust-engine-snapshot', symbol],
    queryFn: () => rustEngineApi.snapshot(symbol, 10),
    refetchInterval: 2000,
  })

  const { data: stats, isLoading: statsLoading } = useQuery({
    queryKey: ['rust-engine-stats'],
    queryFn: () => rustEngineApi.stats(),
    refetchInterval: 2000,
  })

  const { data: marketTrades } = useQuery({
    queryKey: ['market-trades', symbol],
    queryFn: () => marketApi.trades(symbol),
    refetchInterval: 2000,
  })

  const isLoading = snapshotLoading || statsLoading

  if (isLoading) {
    return (
      <SectionCard title="Rust 撮合引擎">
        <Skeleton variant="text" lines={6} />
      </SectionCard>
    )
  }

  if (snapshotError) {
    return (
      <SectionCard title="Rust 撮合引擎">
        <EmptyState
          icon={<Cpu className="w-6 h-6" />}
          title="引擎接口未就绪"
          description="后端 /engine/* 接口可能需要补充实现。当前展示市场成交数据占位。"
        />
        {marketTrades && marketTrades.length > 0 && (
          <div className="mt-4">
            <div className="text-xs text-muted-foreground mb-2">最新成交</div>
            <DataTable
              data={marketTrades.slice(0, 5)}
              keyExtractor={(_, i) => String(i)}
              columns={[
                { key: 'price', title: '价格', render: (item) => <span className="text-sm font-mono">{item.price}</span> },
                { key: 'quantity', title: '数量', render: (item) => <span className="text-sm font-mono">{item.quantity}</span> },
                { key: 'side', title: '方向', render: (item) => <Badge variant={item.side === 'BUY' ? 'success' : 'error'}>{item.side}</Badge> },
              ]}
            />
          </div>
        )}
      </SectionCard>
    )
  }

  return (
    <SectionCard
      title="Rust 撮合引擎"
      headerAction={
        <div className="flex items-center gap-2">
          <Badge variant={stats?.tps ? 'success' : 'neutral'} className="font-mono">{stats?.tps || 0} TPS</Badge>
          <Badge variant="info" className="font-mono">{symbol}</Badge>
        </div>
      }
    >
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mb-4">
        <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-2.5 text-center">
          <div className="text-[10px] text-muted-foreground">总订单</div>
          <div className="text-sm font-semibold text-white font-mono">{stats?.total_orders || 0}</div>
        </div>
        <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-2.5 text-center">
          <div className="text-[10px] text-muted-foreground">总成交</div>
          <div className="text-sm font-semibold text-white font-mono">{stats?.total_trades || 0}</div>
        </div>
        <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-2.5 text-center">
          <div className="text-[10px] text-muted-foreground">延迟</div>
          <div className="text-sm font-semibold text-white font-mono">{stats?.latency_ms || 0} ms</div>
        </div>
        <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-2.5 text-center">
          <div className="text-[10px] text-muted-foreground">引擎实例</div>
          <div className="text-sm font-semibold text-white font-mono">{stats?.engine_count || 0}</div>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-3">
          <div className="text-xs text-muted-foreground mb-2">买十档</div>
          <div className="space-y-1">
            {snapshot?.bids.map((level, i) => (
              <div key={`bid-${i}`} className="flex justify-between text-xs">
                <span className="text-quant-green font-mono">{level.price}</span>
                <span className="text-muted-foreground font-mono">{level.quantity}</span>
              </div>
            ))}
          </div>
        </div>
        <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-3">
          <div className="text-xs text-muted-foreground mb-2">卖十档</div>
          <div className="space-y-1">
            {snapshot?.asks.map((level, i) => (
              <div key={`ask-${i}`} className="flex justify-between text-xs">
                <span className="text-quant-red font-mono">{level.price}</span>
                <span className="text-muted-foreground font-mono">{level.quantity}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </SectionCard>
  )
}
