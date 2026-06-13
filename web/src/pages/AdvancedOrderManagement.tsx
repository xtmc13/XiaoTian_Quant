import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { advancedOrderApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import {
  Layers,
  Target,
  Snowflake,
  TrendingUp,
  Plus,
  Trash2,
  CheckCircle2,
  RefreshCw,
  AlertCircle,
  ArrowUpDown,
  ChevronDown,
  ChevronUp,
  DollarSign,
  Percent,
  Hash,
} from 'lucide-react'

/* ── Types ── */
interface OrderForm {
  symbol: string
  side: 'buy' | 'sell'
  quantity: number
  price?: number
}

interface OCOForm extends OrderForm {
  stopPrice: number
  limitPrice: number
  [key: string]: unknown
}

interface BracketForm extends OrderForm {
  entryPrice: number
  stopLossPrice: number
  takeProfitPrice: number
}

interface IcebergForm extends OrderForm {
  totalQuantity: number
  sliceSize: number
  [key: string]: unknown
}

/* ── Page ── */
export function AdvancedOrderManagement() {
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useState<'oco' | 'bracket' | 'iceberg' | 'trailing'>('oco')
  const [orderSymbol, setOrderSymbol] = useState('BTCUSDT')

  // Forms — symbol is managed by shared orderSymbol state
  const [ocoForm, setOcoForm] = useState<OCOForm>({
    symbol: orderSymbol, side: 'buy', quantity: 0.1, stopPrice: 65000, limitPrice: 66000,
  })
  const [bracketForm, setBracketForm] = useState<BracketForm>({
    symbol: orderSymbol, side: 'buy', quantity: 0.1, entryPrice: 70000, stopLossPrice: 68000, takeProfitPrice: 75000,
  })
  const [icebergForm, setIcebergForm] = useState<IcebergForm>({
    symbol: orderSymbol, side: 'buy', quantity: 0.01, totalQuantity: 1.0, sliceSize: 0.1,
  })

  // Queries
  const { data: ocoList } = useQuery({
    queryKey: ['advanced-orders-oco'],
    queryFn: () => advancedOrderApi.oco.list(),
  })
  const { data: bracketList } = useQuery({
    queryKey: ['advanced-orders-bracket'],
    queryFn: () => advancedOrderApi.bracket.list(),
  })
  const { data: icebergList } = useQuery({
    queryKey: ['advanced-orders-iceberg'],
    queryFn: () => advancedOrderApi.iceberg.list(),
  })

  // Mutations
  const ocoPlace = useMutation({
    mutationFn: advancedOrderApi.oco.place,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['advanced-orders-oco'] }),
  })
  const ocoCancel = useMutation({
    mutationFn: advancedOrderApi.oco.cancel,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['advanced-orders-oco'] }),
  })
  const bracketPlace = useMutation({
    mutationFn: advancedOrderApi.bracket.place,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['advanced-orders-bracket'] }),
  })
  const bracketCancel = useMutation({
    mutationFn: advancedOrderApi.bracket.cancel,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['advanced-orders-bracket'] }),
  })
  const icebergPlace = useMutation({
    mutationFn: advancedOrderApi.iceberg.place,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['advanced-orders-iceberg'] }),
  })
  const icebergCancel = useMutation({
    mutationFn: advancedOrderApi.iceberg.cancel,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['advanced-orders-iceberg'] }),
  })

  const tabs = [
    { key: 'oco' as const, label: 'OCO 订单', icon: <Target className="w-4 h-4" /> },
    { key: 'bracket' as const, label: 'Bracket 订单', icon: <Layers className="w-4 h-4" /> },
    { key: 'iceberg' as const, label: '冰山订单', icon: <Snowflake className="w-4 h-4" /> },
    { key: 'trailing' as const, label: '跟踪止损', icon: <TrendingUp className="w-4 h-4" /> },
  ]

  return (
    <div className="h-full overflow-y-auto">
      <div className="p-4 md:p-6 space-y-6 max-w-7xl mx-auto">
        <PageHeader
          title="高级订单管理"
          subtitle="OCO、Bracket、冰山订单和跟踪止损"
          actions={<ArrowUpDown className="w-6 h-6 text-quant-gold" />}
        />

        {/* KPI Cards */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <KPICard
            label="OCO 订单"
            value={ocoList?.length ?? 0}
            icon={<Target className="w-4 h-4 text-quant-gold" />}
            subValue="活跃"
            trend="neutral"
          />
          <KPICard
            label="Bracket 订单"
            value={bracketList?.length ?? 0}
            icon={<Layers className="w-4 h-4 text-quant-gold" />}
            subValue="活跃"
            trend="neutral"
          />
          <KPICard
            label="冰山订单"
            value={icebergList?.length ?? 0}
            icon={<Snowflake className="w-4 h-4 text-quant-gold" />}
            subValue="活跃"
            trend="neutral"
          />
          <KPICard
            label="跟踪止损"
            value={0}
            icon={<TrendingUp className="w-4 h-4 text-quant-gold" />}
            subValue="开发中"
            trend="neutral"
          />
        </div>

        {/* Tabs */}
        <div className="flex items-center gap-1 p-1 rounded-lg bg-quant-bg-secondary">
          {tabs.map((tab) => (
            <button
              key={tab.key}
              onClick={() => setActiveTab(tab.key)}
              className={cn(
                'flex items-center gap-1.5 px-3 py-2 rounded-md text-xs font-medium transition-colors flex-1 justify-center',
                activeTab === tab.key
                  ? 'bg-quant-gold/10 text-quant-gold'
                  : 'text-muted-foreground hover:text-foreground hover:bg-white/5'
              )}
            >
              {tab.icon}
              {tab.label}
            </button>
          ))}
        </div>

        {/* Shared symbol selector */}
        <div className="flex items-center gap-3 px-4 py-2 bg-quant-bg-secondary rounded-lg border border-quant-border">
          <label className="text-xs text-muted-foreground shrink-0">交易对</label>
          <input
            value={orderSymbol}
            onChange={(e) => setOrderSymbol(e.target.value.toUpperCase())}
            className="flex-1 bg-quant-bg border border-quant-border rounded px-3 py-1.5 text-sm font-mono focus:outline-none focus:border-quant-gold"
            placeholder="BTCUSDT"
          />
        </div>

        {/* OCO Tab */}
        {activeTab === 'oco' && (
          <div className="space-y-4">
            <SectionCard title="新建 OCO 订单">
              <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">交易对</label>
                  <input
                    type="text"
                    value={ocoForm.symbol}
                    onChange={(e) => setOcoForm({ ...ocoForm, symbol: e.target.value.toUpperCase() })}
                    className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                    aria-label="交易对"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">方向</label>
                  <select
                    value={ocoForm.side}
                    onChange={(e) => setOcoForm({ ...ocoForm, side: e.target.value as 'buy' | 'sell' })}
                    className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                  >
                    <option value="buy">买入</option>
                    <option value="sell">卖出</option>
                  </select>
                </div>
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">数量</label>
                  <input
                    type="number"
                    value={ocoForm.quantity}
                    onChange={(e) => setOcoForm({ ...ocoForm, quantity: parseFloat(e.target.value) })}
                    className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                    aria-label="数量"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">止损价</label>
                  <input
                    type="number"
                    value={ocoForm.stopPrice}
                    onChange={(e) => setOcoForm({ ...ocoForm, stopPrice: parseFloat(e.target.value) })}
                    className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                    aria-label="止损价"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">限价</label>
                  <input
                    type="number"
                    value={ocoForm.limitPrice}
                    onChange={(e) => setOcoForm({ ...ocoForm, limitPrice: parseFloat(e.target.value) })}
                    className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                    aria-label="限价"
                  />
                </div>
              </div>
              <div className="mt-3">
                <button
                  onClick={() => ocoPlace.mutate({ ...ocoForm, symbol: orderSymbol })}
                  disabled={ocoPlace.isPending}
                  className={cn(
                    'flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors',
                    ocoPlace.isPending
                      ? 'bg-muted text-muted-foreground cursor-not-allowed'
                      : 'bg-quant-gold text-white hover:bg-quant-gold/90'
                  )}
                >
                  {ocoPlace.isPending ? <RefreshCw className="w-4 h-4 animate-spin" /> : <Plus className="w-4 h-4" />}
                  提交 OCO
                </button>
              </div>
            </SectionCard>

            <SectionCard title="活跃 OCO 订单">
              <div className="space-y-2">
                {!ocoList?.length ? (
                  <EmptyState
                    icon={<Target className="w-10 h-10 text-muted-foreground" />}
                    title="无活跃 OCO 订单"
                    description="使用上方表单创建 OCO 订单"
                  />
                ) : (
                  (Array.isArray(ocoList) ? ocoList : []).map((order) => (
                    <div key={order.id} className="flex items-center justify-between p-3 rounded-md bg-quant-bg-secondary">
                      <div className="flex items-center gap-3">
                        <Target className="w-4 h-4 text-quant-gold" />
                        <div>
                          <div className="text-sm font-medium">{order.symbol}</div>
                          <div className="text-xs text-muted-foreground">
                            {order.side} {order.quantity} @ 止损 {order.stop_price} / 限价 {order.limit_price}
                          </div>
                        </div>
                      </div>
                      <button
                        onClick={() => ocoCancel.mutate(order.id)}
                        disabled={ocoCancel.isPending}
                        className="p-1.5 rounded-md hover:bg-red-500/10 text-muted-foreground hover:text-red-400 transition-colors"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  ))
                )}
              </div>
            </SectionCard>
          </div>
        )}

        {/* Bracket Tab */}
        {activeTab === 'bracket' && (
          <div className="space-y-4">
            <SectionCard title="新建 Bracket 订单">
              <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">交易对</label>
                  <input
                    type="text"
                    value={bracketForm.symbol}
                    onChange={(e) => setBracketForm({ ...bracketForm, symbol: e.target.value.toUpperCase() })}
                    className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                    aria-label="交易对"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">方向</label>
                  <select
                    value={bracketForm.side}
                    onChange={(e) => setBracketForm({ ...bracketForm, side: e.target.value as 'buy' | 'sell' })}
                    className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                  >
                    <option value="buy">买入</option>
                    <option value="sell">卖出</option>
                  </select>
                </div>
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">数量</label>
                  <input
                    type="number"
                    value={bracketForm.quantity}
                    onChange={(e) => setBracketForm({ ...bracketForm, quantity: parseFloat(e.target.value) })}
                    className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                    aria-label="数量"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">入场价</label>
                  <input
                    type="number"
                    value={bracketForm.entryPrice}
                    onChange={(e) => setBracketForm({ ...bracketForm, entryPrice: parseFloat(e.target.value) })}
                    className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                    aria-label="入场价"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">止损价</label>
                  <input
                    type="number"
                    value={bracketForm.stopLossPrice}
                    onChange={(e) => setBracketForm({ ...bracketForm, stopLossPrice: parseFloat(e.target.value) })}
                    className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                    aria-label="止损价"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">止盈价</label>
                  <input
                    type="number"
                    value={bracketForm.takeProfitPrice}
                    onChange={(e) => setBracketForm({ ...bracketForm, takeProfitPrice: parseFloat(e.target.value) })}
                    className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                    aria-label="止盈价"
                  />
                </div>
              </div>
              <div className="mt-3">
                <button
                  onClick={() => bracketPlace.mutate({ ...bracketForm, symbol: orderSymbol })}
                  disabled={bracketPlace.isPending}
                  className={cn(
                    'flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors',
                    bracketPlace.isPending
                      ? 'bg-muted text-muted-foreground cursor-not-allowed'
                      : 'bg-quant-gold text-white hover:bg-quant-gold/90'
                  )}
                >
                  {bracketPlace.isPending ? <RefreshCw className="w-4 h-4 animate-spin" /> : <Plus className="w-4 h-4" />}
                  提交 Bracket
                </button>
              </div>
            </SectionCard>

            <SectionCard title="活跃 Bracket 订单">
              <div className="space-y-2">
                {!bracketList?.length ? (
                  <EmptyState
                    icon={<Layers className="w-10 h-10 text-muted-foreground" />}
                    title="无活跃 Bracket 订单"
                    description="使用上方表单创建 Bracket 订单"
                  />
                ) : (
                  (Array.isArray(bracketList) ? bracketList : []).map((order) => (
                    <div key={order.id} className="flex items-center justify-between p-3 rounded-md bg-quant-bg-secondary">
                      <div className="flex items-center gap-3">
                        <Layers className="w-4 h-4 text-quant-gold" />
                        <div>
                          <div className="text-sm font-medium">{order.symbol}</div>
                          <div className="text-xs text-muted-foreground">
                            入场 {order.entry_price} / 止损 {order.stop_loss_price} / 止盈 {order.take_profit_price}
                          </div>
                        </div>
                      </div>
                      <button
                        onClick={() => bracketCancel.mutate(order.id)}
                        disabled={bracketCancel.isPending}
                        className="p-1.5 rounded-md hover:bg-red-500/10 text-muted-foreground hover:text-red-400 transition-colors"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  ))
                )}
              </div>
            </SectionCard>
          </div>
        )}

        {/* Iceberg Tab */}
        {activeTab === 'iceberg' && (
          <div className="space-y-4">
            <SectionCard title="新建冰山订单">
              <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">交易对</label>
                  <input
                    type="text"
                    value={icebergForm.symbol}
                    onChange={(e) => setIcebergForm({ ...icebergForm, symbol: e.target.value.toUpperCase() })}
                    className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                    aria-label="交易对"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">方向</label>
                  <select
                    value={icebergForm.side}
                    onChange={(e) => setIcebergForm({ ...icebergForm, side: e.target.value as 'buy' | 'sell' })}
                    className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                  >
                    <option value="buy">买入</option>
                    <option value="sell">卖出</option>
                  </select>
                </div>
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">单次数量</label>
                  <input
                    type="number"
                    value={icebergForm.quantity}
                    onChange={(e) => setIcebergForm({ ...icebergForm, quantity: parseFloat(e.target.value) })}
                    className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                    aria-label="单次数量"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">总数量</label>
                  <input
                    type="number"
                    value={icebergForm.totalQuantity}
                    onChange={(e) => setIcebergForm({ ...icebergForm, totalQuantity: parseFloat(e.target.value) })}
                    className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                    aria-label="总数量"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">切片大小</label>
                  <input
                    type="number"
                    value={icebergForm.sliceSize}
                    onChange={(e) => setIcebergForm({ ...icebergForm, sliceSize: parseFloat(e.target.value) })}
                    className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                  />
                </div>
              </div>
              <div className="mt-3">
                <button
                  onClick={() => icebergPlace.mutate({ ...icebergForm, symbol: orderSymbol })}
                  disabled={icebergPlace.isPending}
                  className={cn(
                    'flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors',
                    icebergPlace.isPending
                      ? 'bg-muted text-muted-foreground cursor-not-allowed'
                      : 'bg-quant-gold text-white hover:bg-quant-gold/90'
                  )}
                >
                  {icebergPlace.isPending ? <RefreshCw className="w-4 h-4 animate-spin" /> : <Plus className="w-4 h-4" />}
                  提交冰山订单
                </button>
              </div>
            </SectionCard>

            <SectionCard title="活跃冰山订单">
              <div className="space-y-2">
                {!icebergList?.length ? (
                  <EmptyState
                    icon={<Snowflake className="w-10 h-10 text-muted-foreground" />}
                    title="无活跃冰山订单"
                    description="使用上方表单创建冰山订单"
                  />
                ) : (
                  (Array.isArray(icebergList) ? icebergList : []).map((order) => (
                    <div key={order.id} className="flex items-center justify-between p-3 rounded-md bg-quant-bg-secondary">
                      <div className="flex items-center gap-3">
                        <Snowflake className="w-4 h-4 text-quant-gold" />
                        <div>
                          <div className="text-sm font-medium">{order.symbol}</div>
                          <div className="text-xs text-muted-foreground">
                            总 {order.total_quantity} / 切片 {order.slice_size} / 已执行 {order.executed_quantity || 0}
                          </div>
                        </div>
                      </div>
                      <button
                        onClick={() => icebergCancel.mutate(order.id)}
                        disabled={icebergCancel.isPending}
                        className="p-1.5 rounded-md hover:bg-red-500/10 text-muted-foreground hover:text-red-400 transition-colors"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  ))
                )}
              </div>
            </SectionCard>
          </div>
        )}

        {/* Trailing Tab */}
        {activeTab === 'trailing' && (
          <div className="space-y-4">
            <SectionCard title="跟踪止损">
              <div className="p-8 text-center">
                <TrendingUp className="w-12 h-12 text-muted-foreground mx-auto mb-3" />
                <div className="text-sm font-medium text-muted-foreground">跟踪止损功能开发中</div>
                <div className="text-xs text-muted-foreground mt-1">将在后续版本支持 ATR 和百分比两种跟踪模式</div>
              </div>
            </SectionCard>
          </div>
        )}
      </div>
    </div>
  )
}
