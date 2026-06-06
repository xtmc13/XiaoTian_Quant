import { useState, useEffect, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useWebSocket } from '@/hooks/useWebSocket'
import { accountApi, orderApi, portfolioApi } from '@/lib/api'
import { cn, formatCurrency, formatPercent } from '@/lib/utils'
import { DataTable } from '@/components/DataTable'
import { KPICard } from '@/components/ui/KPICard'
import { SectionCard } from '@/components/ui/SectionCard'
import { EmptyState } from '@/components/ui/EmptyState'
import { Skeleton } from '@/components/ui/Skeleton'
import { ExchangeSignupModal } from '@/components/ExchangeSignupModal'
import {
  Wallet,
  TrendingUp,
  TrendingDown,
  Activity,
  Clock,
  Zap,
  Wifi,
  WifiOff,
  Layers,
  List,
  CheckCircle2,
  XCircle,
  AlertCircle,
  ArrowUpRight,
  ArrowDownRight,
  BarChart3,
} from 'lucide-react'

/* ── Types ───────────────────────────────────────────────────────── */

interface BalanceItem {
  asset: string
  free: number
  total: number
}

interface PositionItem {
  symbol: string
  quantity: number
  avg_entry_price: number
  current_price?: number
  unrealized_pnl: number
  realized_pnl?: number
}

interface OrderItem {
  id: string
  symbol: string
  side: 'BUY' | 'SELL'
  type: string
  price: number
  quantity: number
  filled_quantity?: number
  status: string
  created_at?: string
}

/* ── Status Tag ──────────────────────────────────────────────────── */

function StatusTag({ status }: { status: string }) {
  const config: Record<string, { cls: string; label: string }> = {
    PENDING: { cls: 'bg-yellow-500/10 text-yellow-500', label: '待成交' },
    OPEN: { cls: 'bg-quant-gold/10 text-quant-gold', label: '委托中' },
    PARTIALLY_FILLED: { cls: 'bg-quant-orange/10 text-quant-orange', label: '部分成交' },
    FILLED: { cls: 'bg-quant-green/10 text-quant-green', label: '已成交' },
    CANCELLED: { cls: 'bg-quant-border/40 text-muted-foreground', label: '已取消' },
    REJECTED: { cls: 'bg-quant-red/10 text-quant-red', label: '已拒绝' },
  }
  const c = config[status] || { cls: 'bg-quant-border/40 text-muted-foreground', label: status }
  return (
    <span className={cn('inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-medium', c.cls)}>
      {c.label}
    </span>
  )
}

/* ── Live Price Hook ─────────────────────────────────────────────── */

function useLivePrices() {
  const [prices, setPrices] = useState<Record<string, { price: number; change24h?: number }>>({})
  const { on, isConnected } = useWebSocket('/ws')

  useEffect(() => {
    const unsub = on('price', (data: unknown) => {
      const d = data as Record<string, unknown>
      if (d?.symbol && d?.price != null) {
        setPrices((prev) => ({
          ...prev,
          [d.symbol as string]: { price: d.price as number, change24h: d.change_24h as number | undefined },
        }))
      }
    })
    return unsub
  }, [on])

  return { prices, isConnected }
}

/* ── Main Page ───────────────────────────────────────────────────── */

export function ExchangeAccount() {
  const [activeTab, setActiveTab] = useState<'positions' | 'orders' | 'history'>('positions')
  const [showSignupModal, setShowSignupModal] = useState(false)
  const { prices, isConnected } = useLivePrices()

  /* ── Data Queries ── */
  const { data: balanceData, isLoading: balanceLoading } = useQuery({
    queryKey: ['account-balance'],
    queryFn: () => accountApi.balance(),
    refetchInterval: 10000,
  })

  const { data: portfolio, isLoading: portfolioLoading } = useQuery({
    queryKey: ['portfolio-summary'],
    queryFn: () => portfolioApi.summary(),
    refetchInterval: 10000,
  })

  const { data: positionsData, isLoading: posLoading } = useQuery({
    queryKey: ['portfolio-positions'],
    queryFn: () => portfolioApi.positions(),
    refetchInterval: 5000,
  })

  const { data: ordersData, isLoading: ordersLoading } = useQuery({
    queryKey: ['orders'],
    queryFn: () => orderApi.list(),
    refetchInterval: 5000,
  })

  const { data: historyData, isLoading: historyLoading } = useQuery({
    queryKey: ['orders-history'],
    queryFn: () => orderApi.history({ status: 'filled' }),
    refetchInterval: 10000,
  })

  const balances: BalanceItem[] = useMemo(() => {
    const raw = balanceData?.balances || balanceData?.currencies || []
    return (raw || []).map((b: Record<string, unknown>) => ({
      asset: (b.asset || b.currency || '') as string,
      free: Number(b.free ?? b.available ?? 0),
      total: Number(b.total ?? 0),
    }))
  }, [balanceData])

  const positions: PositionItem[] = useMemo(() => {
    const raw = positionsData?.positions || positionsData || []
    return (Array.isArray(raw) ? raw : []).map((p: Record<string, unknown>) => ({
      symbol: (p.symbol || '') as string,
      quantity: Number(p.quantity ?? 0),
      avg_entry_price: Number(p.avg_entry_price ?? p.entry_price ?? 0),
      current_price: p.current_price != null ? Number(p.current_price) : prices[(p.symbol as string) || '']?.price,
      unrealized_pnl: Number(p.unrealized_pnl ?? 0),
      realized_pnl: Number(p.realized_pnl ?? 0),
    } as PositionItem))
  }, [positionsData, prices])

  const orders: OrderItem[] = useMemo(() => {
    const raw = ordersData || []
    return (Array.isArray(raw) ? raw : []).map((o: Record<string, unknown>) => ({
      id: (o.id || o.order_id || '') as string,
      symbol: (o.symbol || '') as string,
      side: (o.side || 'BUY') as 'BUY' | 'SELL',
      type: (o.type || o.order_type || 'LIMIT') as string,
      price: Number(o.price ?? 0),
      quantity: Number(o.quantity ?? 0),
      filled_quantity: Number(o.filled_quantity ?? 0),
      status: (o.status || 'PENDING') as string,
      created_at: o.created_at as string | undefined,
    } as OrderItem))
  }, [ordersData])

  const history: OrderItem[] = useMemo(() => {
    const raw = historyData || []
    return (Array.isArray(raw) ? raw : []).map((o: Record<string, unknown>) => ({
      id: (o.id || o.order_id || '') as string,
      symbol: (o.symbol || '') as string,
      side: (o.side || 'BUY') as 'BUY' | 'SELL',
      type: (o.type || o.order_type || 'LIMIT') as string,
      price: Number(o.avg_price ?? o.price ?? 0),
      quantity: Number(o.filled_quantity ?? o.quantity ?? 0),
      filled_quantity: Number(o.filled_quantity ?? 0),
      status: (o.status || 'FILLED') as string,
      created_at: (o.created_at || o.updated_at) as string | undefined,
    } as OrderItem))
  }, [historyData])

  /* ── Computed KPIs ── */
  const totalEquity = portfolio?.total_equity ?? balances.reduce((sum, b) => sum + b.total, 0)
  const availableBalance = portfolio?.available_balance ?? balances.find((b) => b.asset === 'USDT')?.free ?? 0
  const totalUnrealizedPnl = positions.reduce((sum, p) => sum + (p.unrealized_pnl || 0), 0)
  const totalRealizedPnl = positions.reduce((sum, p) => sum + (p.realized_pnl || 0), 0)
  const isLoading = balanceLoading || portfolioLoading

  /* ── Render ── */
  return (
    <div className="h-full overflow-y-auto bg-quant-bg p-5">
      <div className="space-y-5">
        {/* Header */}
        <div className="flex items-center justify-between">
          <p className="text-xs text-muted-foreground">虚拟币资产、持仓与订单管理</p>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setShowSignupModal(true)}
              className="px-3 py-1.5 rounded-lg bg-quant-gold/10 text-quant-gold border border-quant-gold/20 text-xs hover:bg-quant-gold/20 transition-colors"
            >注册交易所</button>
            {isConnected ? (
              <span className="flex items-center gap-1.5 rounded-full border border-quant-green/20 bg-quant-green/10 px-2.5 py-1 text-[10px] font-medium text-quant-green">
                <Wifi className="h-3 w-3" />
                实时连接
              </span>
            ) : (
              <span className="flex items-center gap-1.5 rounded-full border border-quant-red/20 bg-quant-red/10 px-2.5 py-1 text-[10px] font-medium text-quant-red">
                <WifiOff className="h-3 w-3" />
                断开
              </span>
            )}
          </div>
        </div>

        {/* KPI Row */}
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          {isLoading ? (
            Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} variant="card" height="108px" />)
          ) : (
            <>
              <KPICard
                icon={<Wallet className="h-4 w-4 text-amber-400" />}
                label="总资产估值"
                value={`$${formatCurrency(totalEquity)}`}
                trend="neutral"
                primary
              />
              <KPICard
                icon={<BarChart3 className="h-4 w-4 text-[#888888]" />}
                label="可用余额"
                value={`$${formatCurrency(availableBalance)}`}
                subValue={`${((availableBalance / Math.max(totalEquity, 1)) * 100).toFixed(1)}%`}
                subLabel="占比"
                trend="neutral"
              />
              <KPICard
                icon={totalUnrealizedPnl >= 0 ? <TrendingUp className="h-4 w-4 text-emerald-400" /> : <TrendingDown className="h-4 w-4 text-red-400" />}
                label="未实现盈亏"
                value={`${totalUnrealizedPnl >= 0 ? '+' : ''}$${formatCurrency(totalUnrealizedPnl)}`}
                trend={totalUnrealizedPnl >= 0 ? 'up' : 'down'}
                primary
              />
              <KPICard
                icon={<Activity className="h-4 w-4 text-[#888888]" />}
                label="持仓数量"
                value={String(positions.length)}
                subValue={String(orders.length)}
                subLabel="活跃订单"
                trend="neutral"
              />
            </>
          )}
        </div>

        {/* Main content grid */}
        <div className="grid grid-cols-1 gap-5 lg:grid-cols-3">
          {/* Left: Asset distribution */}
          <SectionCard title="资产分布" className="lg:col-span-2">
            {balanceLoading ? (
              <div className="space-y-2">
                {Array.from({ length: 4 }).map((_, i) => (
                  <Skeleton key={i} variant="rect" height={40} />
                ))}
              </div>
            ) : balances.length > 0 ? (
              <div className="space-y-2">
                <div className="flex items-center gap-2 text-[10px] text-muted-foreground px-2">
                  <span className="flex-1">资产</span>
                  <span className="w-24 text-right">可用</span>
                  <span className="w-24 text-right">总量</span>
                  <span className="w-24 text-right">估值占比</span>
                </div>
                {balances.map((b) => {
                  const pct = totalEquity > 0 ? (b.total / totalEquity) * 100 : 0
                  const livePrice = prices[b.asset + 'USDT']?.price
                  return (
                    <div
                      key={b.asset}
                      className="flex items-center gap-2 rounded-lg border border-transparent px-2 py-2 hover:border-quant-border hover:bg-white/[0.02] transition-colors"
                    >
                      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-quant-bg-secondary text-[10px] font-bold text-quant-gold">
                        {b.asset.slice(0, 2)}
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="text-sm font-medium text-white">{b.asset}</div>
                        <div className="text-[10px] text-muted-foreground">
                          {livePrice ? `$${formatCurrency(livePrice)}` : '—'}
                        </div>
                      </div>
                      <span className="w-24 text-right text-xs font-mono text-muted-foreground">
                        {b.free.toLocaleString(undefined, { maximumFractionDigits: 4 })}
                      </span>
                      <span className="w-24 text-right text-xs font-mono text-white">
                        {b.total.toLocaleString(undefined, { maximumFractionDigits: 4 })}
                      </span>
                      <div className="w-24 text-right">
                        <span className="text-xs font-mono text-white">{pct.toFixed(1)}%</span>
                        <div className="mt-1 h-1 w-full overflow-hidden rounded-full bg-quant-border">
                          <div
                            className="h-full rounded-full bg-quant-gold"
                            style={{ width: `${Math.min(pct, 100)}%` }}
                          />
                        </div>
                      </div>
                    </div>
                  )
                })}
              </div>
            ) : (
              <EmptyState title="暂无资产数据" description="当前没有获取到交易所资产信息" />
            )}
          </SectionCard>

          {/* Right: Live prices */}
          <SectionCard
            title="实时行情"
            headerAction={
              <span className="flex items-center gap-1 text-[10px] text-muted-foreground">
                <span className={cn('inline-block h-1.5 w-1.5 rounded-full', isConnected ? 'bg-quant-green animate-pulse' : 'bg-quant-red')} />
                {isConnected ? '实时' : '断开'}
              </span>
            }
          >
            <div className="space-y-1">
              {Object.entries(prices).length === 0 && (
                <div className="py-6 text-center text-xs text-muted-foreground">等待行情数据...</div>
              )}
              {Object.entries(prices).map(([sym, data]) => {
                const isUp = (data.change24h || 0) >= 0
                return (
                  <div
                    key={sym}
                    className="flex items-center justify-between rounded-md px-2 py-1.5 hover:bg-white/[0.02] transition-colors"
                  >
                    <span className="text-xs font-medium text-white">{sym.replace('USDT', '/USDT')}</span>
                    <div className="text-right">
                      <div className="text-xs font-mono text-white">${formatCurrency(data.price)}</div>
                      <div className={cn('text-[10px] font-mono', isUp ? 'text-quant-green' : 'text-quant-red')}>
                        {isUp ? '+' : ''}
                        {(data.change24h || 0).toFixed(2)}%
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          </SectionCard>
        </div>

        {/* Bottom: Positions / Orders / History Tabs */}
        <SectionCard
          title="交易明细"
          headerAction={
            <div className="flex items-center gap-1">
              {([
                { key: 'positions', label: '持仓', icon: Layers, count: positions.length },
                { key: 'orders', label: '当前委托', icon: List, count: orders.length },
                { key: 'history', label: '历史成交', icon: CheckCircle2, count: history.length },
              ] as const).map((t) => (
                <button
                  key={t.key}
                  onClick={() => setActiveTab(t.key)}
                  className={cn(
                    'flex items-center gap-1 rounded-md px-2.5 py-1 text-[11px] font-medium transition-colors',
                    activeTab === t.key
                      ? 'bg-quant-gold/10 text-quant-gold'
                      : 'text-muted-foreground hover:text-foreground hover:bg-white/5'
                  )}
                >
                  <t.icon className="h-3 w-3" />
                  {t.label}
                  {t.count > 0 && (
                    <span
                      className={cn(
                        'ml-0.5 rounded-full px-1 py-0 text-[9px] font-bold',
                        activeTab === t.key ? 'bg-quant-gold/20 text-quant-gold' : 'bg-quant-bg-tertiary text-muted-foreground'
                      )}
                    >
                      {t.count}
                    </span>
                  )}
                </button>
              ))}
            </div>
          }
        >
          {/* ── Positions Table ── */}
          {activeTab === 'positions' && (
            <div className="overflow-x-auto">
              {posLoading ? (
                <div className="space-y-2">
                  {Array.from({ length: 3 }).map((_, i) => (
                    <Skeleton key={i} variant="rect" height={40} />
                  ))}
                </div>
              ) : positions.length > 0 ? (
                <DataTable<PositionItem>
                  data={positions}
                  columns={[
                    { key: 'symbol', title: '币种', render: (p) => <span className="font-semibold text-white">{p.symbol}</span> },
                    { key: 'quantity', title: '持仓量', render: (p) => <span className="font-mono">{p.quantity.toFixed(4)}</span> },
                    { key: 'entry', title: '开仓价', render: (p) => <span className="font-mono text-muted-foreground">${formatCurrency(p.avg_entry_price)}</span> },
                    { key: 'current', title: '当前价', render: (p) => <span className="font-mono text-white">${formatCurrency(p.current_price || prices[p.symbol]?.price || 0)}</span> },
                    { key: 'unrealized', title: '未实现盈亏', render: (p) => {
                      const pnl = p.unrealized_pnl || 0
                      const pnlPct = p.avg_entry_price && p.avg_entry_price > 0 ? (pnl / (p.avg_entry_price * p.quantity)) * 100 : 0
                      return (
                        <span className={cn('font-mono font-bold', pnl >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                          <span className="flex items-center gap-1">
                            {pnl >= 0 ? <ArrowUpRight className="h-3 w-3" /> : <ArrowDownRight className="h-3 w-3" />}
                            {pnl >= 0 ? '+' : ''}${formatCurrency(pnl)} ({pnlPct >= 0 ? '+' : ''}{pnlPct.toFixed(2)}%)
                          </span>
                        </span>
                      )
                    }},
                    { key: 'realized', title: '已实现盈亏', render: (p) => (
                      <span className={cn('font-mono', (p.realized_pnl || 0) >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                        {(p.realized_pnl || 0) >= 0 ? '+' : ''}${formatCurrency(p.realized_pnl || 0)}
                      </span>
                    )},
                  ]}
                  keyExtractor={(p) => p.symbol}
                />
              ) : (
                <EmptyState title="暂无持仓" description="当前没有持仓数据" />
              )}
            </div>
          )}

          {/* ── Orders Table ── */}
          {activeTab === 'orders' && (
            <div className="overflow-x-auto">
              {ordersLoading ? (
                <div className="space-y-2">
                  {Array.from({ length: 3 }).map((_, i) => (
                    <Skeleton key={i} variant="rect" height={40} />
                  ))}
                </div>
              ) : orders.length > 0 ? (
                <DataTable<OrderItem>
                  data={orders}
                  columns={[
                    { key: 'time', title: '时间', render: (o) => <span className="text-muted-foreground">{o.created_at ? new Date(o.created_at).toLocaleString() : '-'}</span> },
                    { key: 'symbol', title: '币种', render: (o) => <span className="font-semibold text-white">{o.symbol}</span> },
                    { key: 'side', title: '方向', render: (o) => (
                      <span className={cn('px-1.5 py-0.5 rounded text-[10px] font-bold', o.side === 'BUY' ? 'bg-quant-green/10 text-quant-green' : 'bg-quant-red/10 text-quant-red')}>
                        {o.side === 'BUY' ? '买入' : '卖出'}
                      </span>
                    )},
                    { key: 'type', title: '类型', render: (o) => <span className="text-muted-foreground">{o.type}</span> },
                    { key: 'price', title: '价格', render: (o) => <span className="font-mono">${formatCurrency(o.price)}</span> },
                    { key: 'quantity', title: '数量', render: (o) => <span className="font-mono">{o.quantity.toFixed(4)}</span> },
                    { key: 'filled', title: '已成交', render: (o) => <span className="font-mono text-muted-foreground">{o.filled_quantity ? o.filled_quantity.toFixed(4) : '0.0000'}</span> },
                    { key: 'status', title: '状态', render: (o) => <StatusTag status={o.status} /> },
                  ]}
                  keyExtractor={(o) => o.id}
                />
              ) : (
                <EmptyState title="暂无委托" description="当前没有进行中的委托订单" />
              )}
            </div>
          )}

          {/* ── History Table ── */}
          {activeTab === 'history' && (
            <div className="overflow-x-auto">
              {historyLoading ? (
                <div className="space-y-2">
                  {Array.from({ length: 3 }).map((_, i) => (
                    <Skeleton key={i} variant="rect" height={40} />
                  ))}
                </div>
              ) : history.length > 0 ? (
                <DataTable<OrderItem>
                  data={history}
                  columns={[
                    { key: 'time', title: '时间', render: (o) => <span className="text-muted-foreground">{o.created_at ? new Date(o.created_at).toLocaleString() : '-'}</span> },
                    { key: 'symbol', title: '币种', render: (o) => <span className="font-semibold text-white">{o.symbol}</span> },
                    { key: 'side', title: '方向', render: (o) => (
                      <span className={cn('px-1.5 py-0.5 rounded text-[10px] font-bold', o.side === 'BUY' ? 'bg-quant-green/10 text-quant-green' : 'bg-quant-red/10 text-quant-red')}>
                        {o.side === 'BUY' ? '买入' : '卖出'}
                      </span>
                    )},
                    { key: 'price', title: '成交价格', render: (o) => <span className="font-mono">${formatCurrency(o.price)}</span> },
                    { key: 'quantity', title: '成交数量', render: (o) => <span className="font-mono">{o.quantity.toFixed(4)}</span> },
                    { key: 'status', title: '状态', render: (o) => <StatusTag status={o.status} /> },
                  ]}
                  keyExtractor={(o) => o.id}
                />
              ) : (
                <EmptyState title="暂无历史成交" description="还没有已成交的订单记录" />
              )}
            </div>
          )}
        </SectionCard>
      </div>
      <ExchangeSignupModal open={showSignupModal} onClose={() => setShowSignupModal(false)} />
    </div>
  )
}
