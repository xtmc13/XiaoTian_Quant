import React, { useState } from 'react'
import { cn } from '@/lib/utils'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { formatDateTime, StatusTag } from '@/lib/tradingHelpers'
import { formatPrice } from '@/lib/tradingHelpers'
import type { Trade, Order } from '@/types'
import {
  TrendingUp, Clock, XCircle, CheckCircle2, Activity,
} from 'lucide-react'

export type TradeHistoryTab = 'positions' | 'orders' | 'history' | 'fills' | 'assets'

interface HistoryOrder extends Order {
  updated_at?: string
  avg_price?: number
  filled_quantity?: number
  realized_pnl?: number
}

interface FillTrade extends Trade {
  created_at?: string
  timestamp?: number
  avg_price?: number
  filled_quantity?: number
  fee?: number
}

export interface TradeHistoryProps {
  activeTab: TradeHistoryTab
  onTabChange: (tab: TradeHistoryTab) => void
  holdingsList?: Array<Record<string, unknown>>
  orders?: Order[]
  ordersLoading?: boolean
  historyOrders?: HistoryOrder[]
  historyLoading?: boolean
  fillTrades?: FillTrade[]
  fillsLoading?: boolean
  allBalances?: unknown
  balLoading?: boolean
  onCancelOrder?: (id: string) => void
  className?: string
}

export const TradeHistory = React.memo(function TradeHistory({
  activeTab,
  onTabChange,
  holdingsList = [],
  orders = [],
  ordersLoading = false,
  historyOrders = [],
  historyLoading = false,
  fillTrades = [],
  fillsLoading = false,
  allBalances,
  balLoading = false,
  onCancelOrder,
  className,
}: TradeHistoryProps) {
  const tabs = [
    { key: 'positions' as TradeHistoryTab, label: '持有币种', count: holdingsList.length, icon: TrendingUp },
    { key: 'orders' as TradeHistoryTab, label: '当前委托', count: orders.length, icon: Clock },
    { key: 'history' as TradeHistoryTab, label: '历史委托', count: historyOrders.length, icon: XCircle },
    { key: 'fills' as TradeHistoryTab, label: '成交记录', count: fillTrades.length, icon: CheckCircle2 },
    { key: 'assets' as TradeHistoryTab, label: '资产', count: holdingsList.length, icon: Activity },
  ]

  return (
    <div className={cn('shrink-0 border-t border-quant-border bg-quant-bg-secondary flex flex-col', className)}>
      <div className="flex border-b border-quant-border px-2 items-center justify-between shrink-0">
        <div className="flex">
          {tabs.map(t => (
            <button
              key={t.key}
              onClick={() => onTabChange(t.key)}
              className={cn(
                'px-4 py-2 text-xs font-medium transition-colors relative flex items-center gap-1.5',
                activeTab === t.key ? 'text-quant-gold' : 'text-muted-foreground hover:text-foreground'
              )}
            >
              <t.icon className="w-3.5 h-3.5" />{t.label}
              {t.count > 0 && (
                <span className={cn(
                  'ml-1 px-1.5 py-0 rounded-full text-[10px] font-bold',
                  activeTab === t.key ? 'bg-quant-gold/20 text-quant-gold' : 'bg-quant-bg-tertiary text-muted-foreground'
                )}>{t.count}</span>
              )}
              {activeTab === t.key && <span className="absolute bottom-0 left-0 right-0 h-0.5 bg-quant-gold" />}
            </button>
          ))}
        </div>
      </div>
      <div className="overflow-y-auto flex-1" style={{ maxHeight: 360 }}>
        {activeTab === 'positions' && (
          <div className="overflow-x-auto" key="tab-positions">
            {holdingsList.length ? (
              <table className="w-full text-[11px] whitespace-nowrap">
                <thead className="sticky top-0 bg-quant-bg-secondary z-10">
                  <tr className="text-muted-foreground border-b border-quant-border">
                    <th scope="col" className="text-left font-medium px-3 py-2">币种</th>
                    <th scope="col" className="text-right font-medium px-3 py-2">可用</th>
                    <th scope="col" className="text-right font-medium px-3 py-2">冻结</th>
                    <th scope="col" className="text-right font-medium px-3 py-2">总量</th>
                  </tr>
                </thead>
                <tbody>
                  {holdingsList.map((b, i) => {
                    const bal = b as Record<string, unknown>
                    const free = parseFloat(String(bal.free ?? bal.available ?? 0)) || 0
                    const locked = parseFloat(String(bal.locked ?? bal.frozen ?? 0)) || 0
                    return (
                      <tr key={String(bal.asset || bal.currency || i)} className="border-b border-quant-border/40 hover:bg-white/[0.03]">
                        <td className="px-3 py-2.5 font-medium">{String(bal.asset || bal.currency || '--')}</td>
                        <td className="px-3 py-2.5 text-right font-mono">{free.toFixed(6)}</td>
                        <td className="px-3 py-2.5 text-right font-mono">{locked > 0 ? locked.toFixed(6) : '--'}</td>
                        <td className="px-3 py-2.5 text-right font-mono">{(free + locked).toFixed(6)}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            ) : <div className="py-8 text-center text-muted-foreground text-xs">暂无持仓</div>}
          </div>
        )}

        {activeTab === 'orders' && (
          <div key="tab-orders">
            {ordersLoading ? (
              <div className="p-4 space-y-2">{Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} variant="text" height={32} />)}</div>
            ) : orders.length ? (
              <div className="overflow-x-auto">
                <table className="w-full text-[11px] whitespace-nowrap">
                  <thead className="sticky top-0 bg-quant-bg-secondary z-10">
                    <tr className="text-muted-foreground text-left">
                      <th scope="col" className="px-1.5 py-1 font-medium">时间</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">币种</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">方向</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">类型</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">价格</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">数量</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">状态</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {orders.map((o) => (
                      <tr key={o.id} className="border-t border-quant-border/40 hover:bg-white/[0.02]">
                        <td className="px-1.5 py-1 text-muted-foreground">{formatDateTime(o.created_at)}</td>
                        <td className="px-1.5 py-1 font-semibold">{o.symbol}</td>
                        <td className="px-1.5 py-1">
                          <span className={cn('text-[9px] font-bold', o.side === 'BUY' ? 'text-[#0ECB81]' : 'text-[#F6465D]')}>
                            {o.side === 'BUY' ? '买入' : '卖出'}
                          </span>
                        </td>
                        <td className="px-1.5 py-1 text-muted-foreground">{o.type}</td>
                        <td className="px-1.5 py-1 font-mono">${formatPrice(o.price, 2)}</td>
                        <td className="px-1.5 py-1 font-mono">{formatPrice(o.quantity, 4)}</td>
                        <td className="px-1.5 py-1"><StatusTag status={o.status} /></td>
                        <td className="px-1.5 py-1">
                          {onCancelOrder && (
                            <button onClick={() => onCancelOrder(o.id)} className="px-1.5 py-0.5 bg-[#F6465D]/10 text-[#F6465D] rounded text-[9px] font-medium hover:bg-[#F6465D]/20 transition-colors flex items-center gap-1">
                              <XCircle className="w-3 h-3" />取消
                            </button>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="py-6 flex items-center justify-center">
                <EmptyState title="暂无委托" description="当前没有进行中的委托订单" className="py-1 border-0 text-[10px] [&>div:first-child]:hidden" />
              </div>
            )}
          </div>
        )}

        {activeTab === 'history' && (
          <div key="tab-history">
            {historyLoading ? (
              <div className="p-4 space-y-2">{Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} variant="text" height={32} />)}</div>
            ) : historyOrders.length ? (
              <div className="overflow-x-auto">
                <table className="w-full text-[11px] whitespace-nowrap">
                  <thead className="sticky top-0 bg-quant-bg-secondary z-10">
                    <tr className="text-muted-foreground text-left">
                      <th scope="col" className="px-1.5 py-1 font-medium">时间</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">币种</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">方向</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">价格</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">数量</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">盈亏</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">状态</th>
                    </tr>
                  </thead>
                  <tbody>
                    {historyOrders.map((o) => {
                      const pnl = o.realized_pnl || 0
                      return (
                        <tr key={o.id} className="border-t border-quant-border/40 hover:bg-white/[0.02]">
                          <td className="px-1.5 py-1 text-muted-foreground">{formatDateTime(o.updated_at || o.created_at)}</td>
                          <td className="px-1.5 py-1 font-semibold">{o.symbol}</td>
                          <td className="px-1.5 py-1">
                            <span className={cn('text-[9px] font-bold', o.side === 'BUY' ? 'text-[#0ECB81]' : 'text-[#F6465D]')}>
                              {o.side === 'BUY' ? '买入' : '卖出'}
                            </span>
                          </td>
                          <td className="px-1.5 py-1 font-mono">${formatPrice(o.avg_price || o.price, 2)}</td>
                          <td className="px-1.5 py-1 font-mono">{formatPrice(o.filled_quantity, 4)}</td>
                          <td className={cn('px-1.5 py-1 font-mono font-bold', pnl >= 0 ? 'text-[#0ECB81]' : 'text-[#F6465D]')}>
                            {pnl >= 0 ? '+' : ''}{pnl.toFixed(2)}
                          </td>
                          <td className="px-1.5 py-1"><StatusTag status={o.status} /></td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="py-6 flex items-center justify-center">
                <EmptyState title="暂无历史成交" description="还没有已成交的订单记录" className="py-1 border-0 text-[10px] [&>div:first-child]:hidden" />
              </div>
            )}
          </div>
        )}

        {activeTab === 'fills' && (
          <div key="tab-fills">
            {fillsLoading ? (
              <div className="p-4 space-y-2">{Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} variant="text" height={32} />)}</div>
            ) : fillTrades.length ? (
              <div className="overflow-x-auto">
                <table className="w-full text-[11px] whitespace-nowrap">
                  <thead className="sticky top-0 bg-quant-bg-secondary z-10">
                    <tr className="text-muted-foreground text-left">
                      <th scope="col" className="px-1.5 py-1 font-medium">时间</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">币种</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">方向</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">价格</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">数量</th>
                      <th scope="col" className="px-1.5 py-1 font-medium">手续费</th>
                    </tr>
                  </thead>
                  <tbody>
                    {fillTrades.map((t, i) => (
                      <tr key={t.id || i} className="border-t border-quant-border/40 hover:bg-white/[0.02]">
                        <td className="px-1.5 py-1 text-muted-foreground">{formatDateTime(t.time || t.created_at || t.timestamp || 0)}</td>
                        <td className="px-1.5 py-1 font-semibold">{t.symbol || '--'}</td>
                        <td className="px-1.5 py-1">
                          <span className={cn('text-[9px] font-bold', t.side === 'buy' ? 'text-[#0ECB81]' : 'text-[#F6465D]')}>
                            {t.side === 'buy' ? '买入' : '卖出'}
                          </span>
                        </td>
                        <td className="px-1.5 py-1 font-mono">${formatPrice(t.price || t.avg_price, 2)}</td>
                        <td className="px-1.5 py-1 font-mono">{formatPrice(t.quantity || t.filled_quantity, 4)}</td>
                        <td className="px-1.5 py-1 font-mono text-muted-foreground">{t.fee ? formatPrice(t.fee, 4) : '--'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="py-6 flex items-center justify-center">
                <EmptyState title="暂无成交记录" description="还没有成交记录" className="py-1 border-0 text-[10px] [&>div:first-child]:hidden" />
              </div>
            )}
          </div>
        )}

        {activeTab === 'assets' && (
          <div key="tab-assets">
            {balLoading ? (
              <div className="p-4 space-y-2">{Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} variant="text" height={32} />)}</div>
            ) : (() => {
              const raw = allBalances as Record<string, unknown>
              const list = ((raw?.balances as unknown[]) || (raw?.currencies as unknown[]) || (raw?.list as unknown[]) || (Array.isArray(raw) ? raw : [])) as Record<string, unknown>[]
              if (!Array.isArray(list) || !list.length) return (
                <div className="py-6 flex items-center justify-center">
                  <EmptyState title="暂无资产数据" description="等待资产数据加载..." className="py-1 border-0 text-[10px] [&>div:first-child]:hidden" />
                </div>
              )
              return (
                <div className="overflow-x-auto">
                  <table className="w-full text-[11px] whitespace-nowrap">
                    <thead className="sticky top-0 bg-quant-bg-secondary z-10">
                      <tr className="text-muted-foreground text-left">
                        <th scope="col" className="px-3 py-2 font-medium">币种</th>
                        <th scope="col" className="text-right px-3 py-2 font-medium">可用</th>
                        <th scope="col" className="text-right px-3 py-2 font-medium">冻结</th>
                        <th scope="col" className="text-right px-3 py-2 font-medium">总计</th>
                        <th scope="col" className="text-right px-3 py-2 font-medium">估值(USDT)</th>
                      </tr>
                    </thead>
                    <tbody>
                      {list.map((b, i) => {
                        const free = parseFloat(String(b.free ?? b.available ?? b.balance ?? 0))
                        const locked = parseFloat(String(b.locked ?? b.frozen ?? 0))
                        const total = free + locked
                        return (
                          <tr key={String(b.asset || b.symbol || i)} className="border-t border-quant-border/40 hover:bg-white/[0.02]">
                            <td className="px-3 py-2 font-semibold">{String(b.asset || b.symbol || '--')}</td>
                            <td className="px-3 py-2 text-right font-mono">{free.toFixed(4)}</td>
                            <td className="px-3 py-2 text-right font-mono">{locked > 0 ? locked.toFixed(4) : '--'}</td>
                            <td className="px-3 py-2 text-right font-mono">{total.toFixed(4)}</td>
                            <td className="px-3 py-2 text-right font-mono text-muted-foreground">--</td>
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                </div>
              )
            })()}
          </div>
        )}
      </div>
    </div>
  )
})
