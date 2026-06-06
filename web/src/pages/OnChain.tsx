import { useState, memo, useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Link2, Activity, ArrowUpRight, ArrowDownRight, Minus, Flame,
  Wallet, TrendingUp, TrendingDown, Loader2, AlertTriangle,
  BarChart3, Globe, Clock,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { onchainApi } from '@/lib/api'

interface ETHMetrics {
  gas_price_gwei: number
  active_addresses: number
  tx_count_24h: number
  avg_tx_fee_usd: number
  staking_apr_pct: number
  eth_burned_24h: number
  exchange_inflow_eth: number
  exchange_outflow_eth: number
  net_exchange_flow_eth: number
  mvrv_ratio: number
  nupl: number
}

interface BTCMetrics {
  hash_rate_eh: number
  active_addresses: number
  tx_count_24h: number
  avg_tx_fee_usd: number
  exchange_inflow_btc: number
  exchange_outflow_btc: number
  net_exchange_flow_btc: number
  sopr: number
  mvrv_ratio: number
  nupl: number
  puell_multiple: number
  stock_to_flow: number
}

interface OnChainSignal {
  symbol: string
  direction: string
  strength: number
  indicators: string[]
  timestamp: number
}

/**
 * MetricCard — memoized metric display card.
 */
const MetricCard = memo(function MetricCard({
  label, value, unit, color = 'text-foreground',
}: {
  label: string
  value: string | number
  unit?: string
  color?: string
}) {
  return (
    <div className="bg-quant-card border border-quant-border rounded-xl p-3 shadow-sm">
      <div className="text-[10px] text-muted-foreground mb-1">{label}</div>
      <div className={cn('text-sm font-bold', color)}>
        {typeof value === 'number' ? value.toLocaleString(undefined, { maximumFractionDigits: 2 }) : value}
        {unit && <span className="text-[10px] ml-0.5 text-muted-foreground">{unit}</span>}
      </div>
    </div>
  )
})

/**
 * SignalCard — memoized on-chain signal card.
 */
const SignalCard = memo(function SignalCard({ signal }: { signal: OnChainSignal | null }) {
  if (!signal) return null
  return (
    <div className={cn(
      'bg-quant-card border rounded-xl p-4 shadow-sm',
      signal.direction === 'bullish' ? 'border-quant-green/30' :
      signal.direction === 'bearish' ? 'border-quant-red/30' : 'border-quant-border'
    )}>
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2">
          <span className="text-sm font-bold text-foreground">{signal.symbol}</span>
          <span className={cn(
            'text-[10px] px-2 py-0.5 rounded font-medium',
            signal.direction === 'bullish' ? 'bg-quant-green/10 text-quant-green' :
            signal.direction === 'bearish' ? 'bg-quant-red/10 text-quant-red' :
            'bg-quant-gold/10 text-quant-gold'
          )}>
            {signal.direction === 'bullish' ? '看涨' : signal.direction === 'bearish' ? '看跌' : '中性'}
          </span>
        </div>
        <div className="text-xs font-bold text-foreground">{(signal.strength ?? 0).toFixed(0)}%</div>
      </div>
      <div className="w-full h-1.5 bg-quant-bg-secondary rounded-full overflow-hidden mb-2">
        <div className={cn('h-full rounded-full',
          signal.direction === 'bullish' ? 'bg-quant-green' :
          signal.direction === 'bearish' ? 'bg-quant-red' : 'bg-quant-gold'
        )} style={{ width: `${signal.strength}%` }} />
      </div>
      <div className="flex flex-wrap gap-1">
        {(signal.indicators ?? []).map((ind: string) => (
          <span key={ind} className="text-[9px] px-1.5 py-0.5 rounded bg-quant-bg-secondary text-muted-foreground">
            {ind}
          </span>
        ))}
      </div>
    </div>
  )
})

export function OnChain() {
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useState<'btc' | 'eth'>('btc')

  /* ── Queries ── */
  const { data: ethMetrics, isLoading: ethLoading } = useQuery<ETHMetrics>({
    queryKey: ['onchain-eth-metrics'],
    queryFn: () => onchainApi.ethMetrics(),
    staleTime: 60_000,
    retry: 2,
    refetchInterval: 60_000,
  })

  const { data: btcMetrics, isLoading: btcLoading } = useQuery<BTCMetrics>({
    queryKey: ['onchain-btc-metrics'],
    queryFn: () => onchainApi.btcMetrics(),
    staleTime: 60_000,
    retry: 2,
    refetchInterval: 60_000,
  })

  const { data: btcSignal } = useQuery<OnChainSignal>({
    queryKey: ['onchain-btc-signal'],
    queryFn: () => onchainApi.btcSignal(),
    staleTime: 60_000,
    retry: 2,
    refetchInterval: 60_000,
  })

  const { data: ethSignal } = useQuery<OnChainSignal>({
    queryKey: ['onchain-eth-signal'],
    queryFn: () => onchainApi.ethSignal(),
    staleTime: 60_000,
    retry: 2,
    refetchInterval: 60_000,
  })

  const loading = ethLoading || btcLoading

  const handleRefresh = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['onchain'] })
  }, [queryClient])

  return (
    <div className="h-full flex flex-col p-4 gap-4 overflow-auto">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Link2 className="w-5 h-5 text-quant-gold" />
          <h1 className="text-lg font-bold text-foreground">链上数据</h1>
        </div>
        <button
          onClick={handleRefresh}
          disabled={loading}
          className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-quant-card border border-quant-border text-foreground text-xs font-medium hover:border-quant-gold/40 transition-colors disabled:opacity-50"
        >
          {loading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Clock className="w-3.5 h-3.5" />}
          刷新
        </button>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 bg-quant-bg-secondary rounded-lg p-0.5 w-fit">
        {[
          { k: 'btc' as const, label: 'Bitcoin', icon: TrendingUp },
          { k: 'eth' as const, label: 'Ethereum', icon: Flame },
        ].map(t => (
          <button key={t.k} onClick={() => setActiveTab(t.k)}
            className={cn('flex items-center gap-1 px-3 py-1.5 rounded text-xs font-medium transition-colors',
              activeTab === t.k ? 'bg-quant-gold text-white' : 'text-muted-foreground hover:text-foreground')}>
            <t.icon className="h-3 w-3" />{t.label}
          </button>
        ))}
      </div>

      {/* BTC Tab */}
      {activeTab === 'btc' && btcMetrics && (
        <div className="space-y-4">
          <SignalCard signal={btcSignal ?? null} />

          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <MetricCard label="算力" value={btcMetrics.hash_rate_eh} unit="EH/s" />
            <MetricCard label="活跃地址" value={btcMetrics.active_addresses} />
            <MetricCard label="24h 交易数" value={btcMetrics.tx_count_24h} />
            <MetricCard label="平均手续费" value={btcMetrics.avg_tx_fee_usd} unit="$" />
          </div>

          <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
            <MetricCard label="交易所流入" value={btcMetrics.exchange_inflow_btc} unit="BTC" color="text-quant-red" />
            <MetricCard label="交易所流出" value={btcMetrics.exchange_outflow_btc} unit="BTC" color="text-quant-green" />
            <MetricCard
              label="净流入"
              value={btcMetrics.net_exchange_flow_btc > 0 ? '+' + btcMetrics.net_exchange_flow_btc : btcMetrics.net_exchange_flow_btc}
              unit="BTC"
              color={btcMetrics.net_exchange_flow_btc > 0 ? 'text-quant-red' : 'text-quant-green'}
            />
          </div>

          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <MetricCard label="SOPR" value={btcMetrics.sopr} color={btcMetrics.sopr > 1 ? 'text-quant-red' : 'text-quant-green'} />
            <MetricCard label="MVRV" value={btcMetrics.mvrv_ratio} color={btcMetrics.mvrv_ratio > 3.5 ? 'text-quant-red' : btcMetrics.mvrv_ratio < 1 ? 'text-quant-green' : 'text-foreground'} />
            <MetricCard label="NUPL" value={btcMetrics.nupl} color={btcMetrics.nupl > 0.5 ? 'text-quant-red' : btcMetrics.nupl < 0 ? 'text-quant-green' : 'text-foreground'} />
            <MetricCard label="Puell" value={btcMetrics.puell_multiple} />
          </div>

          {/* Exchange Flow Interpretation */}
          <div className="bg-quant-card border border-quant-border rounded-xl p-4 shadow-sm">
            <div className="flex items-center gap-1.5 text-xs font-bold text-foreground mb-2">
              <Globe className="w-3.5 h-3.5 text-quant-gold" />
              交易所流向解读
            </div>
            <div className="text-xs text-muted-foreground leading-relaxed">
              {btcMetrics.net_exchange_flow_btc < -500
                ? '大量 BTC 流出交易所，表明持有者倾向于长期持有（HODL），通常被视为看涨信号。'
                : btcMetrics.net_exchange_flow_btc > 500
                ? '大量 BTC 流入交易所，可能预示抛售压力增加，通常被视为看跌信号。'
                : '交易所流向相对平衡，市场处于观望状态。'}
            </div>
          </div>
        </div>
      )}

      {/* ETH Tab */}
      {activeTab === 'eth' && ethMetrics && (
        <div className="space-y-4">
          <SignalCard signal={ethSignal ?? null} />

          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <MetricCard label="Gas 价格" value={ethMetrics.gas_price_gwei} unit="Gwei" color={ethMetrics.gas_price_gwei > 100 ? 'text-quant-red' : 'text-quant-green'} />
            <MetricCard label="活跃地址" value={ethMetrics.active_addresses} />
            <MetricCard label="24h 交易数" value={ethMetrics.tx_count_24h} />
            <MetricCard label="平均手续费" value={ethMetrics.avg_tx_fee_usd} unit="$" />
          </div>

          <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
            <MetricCard label="交易所流入" value={ethMetrics.exchange_inflow_eth} unit="ETH" color="text-quant-red" />
            <MetricCard label="交易所流出" value={ethMetrics.exchange_outflow_eth} unit="ETH" color="text-quant-green" />
            <MetricCard
              label="净流入"
              value={ethMetrics.net_exchange_flow_eth > 0 ? '+' + ethMetrics.net_exchange_flow_eth : ethMetrics.net_exchange_flow_eth}
              unit="ETH"
              color={ethMetrics.net_exchange_flow_eth > 0 ? 'text-quant-red' : 'text-quant-green'}
            />
          </div>

          <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
            <MetricCard label="质押 APR" value={ethMetrics.staking_apr_pct} unit="%" color="text-quant-blue" />
            <MetricCard label="24h 销毁" value={ethMetrics.eth_burned_24h} unit="ETH" color="text-quant-gold" />
            <MetricCard label="MVRV" value={ethMetrics.mvrv_ratio} />
          </div>

          {/* Gas Analysis */}
          <div className="bg-quant-card border border-quant-border rounded-xl p-4 shadow-sm">
            <div className="flex items-center gap-1.5 text-xs font-bold text-foreground mb-2">
              <Flame className="w-3.5 h-3.5 text-quant-gold" />
              Gas 价格分析
            </div>
            <div className="text-xs text-muted-foreground leading-relaxed">
              {ethMetrics.gas_price_gwei > 100
                ? 'Gas 价格处于高位，网络拥堵严重。这通常发生在市场剧烈波动或热门 NFT 项目发售期间。'
                : ethMetrics.gas_price_gwei > 50
                ? 'Gas 价格中等偏高，网络活跃度良好。'
                : 'Gas 价格较低，网络畅通，适合进行常规交易。'}
            </div>
          </div>
        </div>
      )}

      {loading && (!btcMetrics && !ethMetrics) && (
        <div className="flex items-center justify-center py-20 gap-2 text-muted-foreground">
          <Loader2 className="w-5 h-5 animate-spin" /> 加载链上数据中...
        </div>
      )}
    </div>
  )
}
