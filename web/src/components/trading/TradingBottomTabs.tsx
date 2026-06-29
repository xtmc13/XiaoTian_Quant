import React, { useState } from 'react'
import { cn } from '@/lib/utils'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { formatPrice, formatDateTime, StatusTag } from '@/lib/tradingHelpers'
import type { Order, Trade, PortfolioPosition } from '@/types'

/* ── Spot Positions (holdings) ─────────────────────────────────────── */

export interface SpotPositionsTabProps {
  holdings: Array<Record<string, unknown>>
}

export function SpotPositionsTab({ holdings }: SpotPositionsTabProps) {
  if (!holdings.length) {
    return <div className="py-8 text-center text-muted-foreground text-xs">暂无持仓</div>
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-[11px] whitespace-nowrap">
        <thead className="sticky top-0 bg-quant-bg-secondary z-10">
          <tr className="text-muted-foreground border-b border-quant-border">
            <th scope="col" className="text-left font-medium px-3 py-2">
              币种
            </th>
            <th scope="col" className="text-right font-medium px-3 py-2">
              可用
            </th>
            <th scope="col" className="text-right font-medium px-3 py-2">
              冻结
            </th>
            <th scope="col" className="text-right font-medium px-3 py-2">
              总量
            </th>
          </tr>
        </thead>
        <tbody>
          {holdings.map((b, i) => {
            const free = parseFloat(String(b.free ?? b.available ?? 0)) || 0
            const locked = parseFloat(String(b.locked ?? b.frozen ?? 0)) || 0
            return (
              <tr
                key={String(b.asset || b.currency || i)}
                className="border-b border-quant-border/40 hover:bg-white/[0.03]"
              >
                <td className="px-3 py-2.5 font-medium">{String(b.asset || b.currency || '--')}</td>
                <td className="px-3 py-2.5 text-right font-mono">{free.toFixed(6)}</td>
                <td className="px-3 py-2.5 text-right font-mono">{locked > 0 ? locked.toFixed(6) : '--'}</td>
                <td className="px-3 py-2.5 text-right font-mono">{(free + locked).toFixed(6)}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

/* ── Contract Positions ───────────────────────────────────────────── */

export interface ContractPositionsTabProps {
  positions: Array<PortfolioPosition & Record<string, unknown>>
  markPrice: number
  leverage: number
  onClose: (pos: PortfolioPosition & Record<string, unknown>) => void
  submitting: boolean
}

export function ContractPositionsTab({
  positions,
  markPrice,
  leverage,
  onClose,
  submitting,
}: ContractPositionsTabProps) {
  if (!positions.length) {
    return <div className="py-8 text-center text-muted-foreground text-xs">暂无持仓</div>
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-[11px] whitespace-nowrap">
        <thead className="sticky top-0 bg-quant-bg-secondary z-10">
          <tr className="text-muted-foreground border-b border-quant-border">
            <th scope="col" className="text-left font-medium px-3 py-2">
              合约
            </th>
            <th scope="col" className="text-left font-medium px-3 py-2">
              方向/数量
            </th>
            <th scope="col" className="text-right font-medium px-3 py-2">
              开仓价
            </th>
            <th scope="col" className="text-right font-medium px-3 py-2">
              标记价
            </th>
            <th scope="col" className="text-right font-medium px-3 py-2">
              强平价
            </th>
            <th scope="col" className="text-right font-medium px-3 py-2">
              保证金
            </th>
            <th scope="col" className="text-right font-medium px-3 py-2">
              未实现盈亏
            </th>
            <th scope="col" className="text-right font-medium px-3 py-2">
              操作
            </th>
          </tr>
        </thead>
        <tbody>
          {positions.map((pos, i) => {
            const isLong = (pos.side || '').toUpperCase() === 'LONG' || (pos.side || '').toUpperCase() === 'BUY'
            const entryPx = Number(pos.avg_entry_price || pos.entry_price || 0)
            const qty = Number(pos.quantity || 0)
            const margin = Number(pos.margin || 0)
            const posLeverage = Number(pos.leverage || leverage || 1)
            const notional = qty * markPrice
            const upnl = isLong ? (markPrice - entryPx) * qty : (entryPx - markPrice) * qty
            const upnlPct = margin > 0 ? (upnl / margin) * 100 : 0
            const liqPx = Number(pos.liquidation_price || 0)
            return (
              <tr key={String(pos.id || i)} className="border-b border-quant-border/40 hover:bg-white/[0.03]">
                <td className="px-3 py-2.5 font-medium">{pos.symbol || '--'} 永续</td>
                <td className="px-3 py-2.5">
                  <span
                    className={cn(
                      'inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-bold',
                      isLong ? 'bg-[#0ECB81]/10 text-[#0ECB81]' : 'bg-[#F6465D]/10 text-[#F6465D]'
                    )}
                  >
                    <span className={cn('w-1.5 h-1.5 rounded-full', isLong ? 'bg-[#0ECB81]' : 'bg-[#F6465D]')} />
                    {isLong ? '多' : '空'} {qty.toFixed(3)}
                  </span>
                  <span className="text-[10px] text-muted-foreground ml-1">{posLeverage}x</span>
                </td>
                <td className="px-3 py-2.5 text-right font-mono">{entryPx > 0 ? entryPx.toFixed(2) : '--'}</td>
                <td className="px-3 py-2.5 text-right font-mono">{markPrice > 0 ? markPrice.toFixed(2) : '--'}</td>
                <td className="px-3 py-2.5 text-right font-mono text-[#F6465D]">
                  {liqPx > 0 ? liqPx.toFixed(2) : '--'}
                </td>
                <td className="px-3 py-2.5 text-right font-mono">{margin > 0 ? margin.toFixed(2) : '--'} USDT</td>
                <td className="px-3 py-2.5 text-right font-mono">
                  <span className={cn(upnl >= 0 ? 'text-[#0ECB81]' : 'text-[#F6465D]')}>
                    {upnl >= 0 ? '+' : ''}
                    {upnl.toFixed(2)}
                  </span>
                  <span className="text-muted-foreground ml-1">
                    ({upnlPct >= 0 ? '+' : ''}
                    {upnlPct.toFixed(2)}%)
                  </span>
                </td>
                <td className="px-3 py-2.5 text-right">
                  <button
                    onClick={() => onClose(pos)}
                    disabled={submitting}
                    className={cn(
                      'px-2 py-1 rounded text-[10px] font-medium transition-colors',
                      submitting
                        ? 'bg-muted text-muted-foreground cursor-not-allowed'
                        : isLong
                          ? 'bg-[#F6465D]/10 text-[#F6465D] hover:bg-[#F6465D]/20'
                          : 'bg-[#0ECB81]/10 text-[#0ECB81] hover:bg-[#0ECB81]/20'
                    )}
                  >
                    {submitting ? '平仓中...' : `平${isLong ? '多' : '空'}`}
                  </button>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

/* ── Orders Tab ─────────────────────────────────────────────────────── */

export interface OrdersTabProps {
  orders: Order[]
  loading: boolean
  onCancel: (id: string) => void
}

export function OrdersTab({ orders, loading, onCancel }: OrdersTabProps) {
  if (loading) {
    return (
      <div className="p-4 space-y-2">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} variant="text" height={32} />
        ))}
      </div>
    )
  }
  if (!orders?.length) {
    return (
      <div className="py-6 flex items-center justify-center">
        <EmptyState
          title="暂无委托"
          description="当前没有进行中的委托订单"
          className="py-1 border-0 text-[10px] [&>div:first-child]:hidden"
        />
      </div>
    )
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-[11px] whitespace-nowrap">
        <thead className="sticky top-0 bg-quant-bg-secondary z-10">
          <tr className="text-muted-foreground text-left">
            <th scope="col" className="px-1.5 py-1 font-medium">
              时间
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              币种
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              方向
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              类型
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              价格
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              数量
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              状态
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              操作
            </th>
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
              <td className="px-1.5 py-1">
                <StatusTag status={o.status} />
              </td>
              <td className="px-1.5 py-1">
                <button
                  onClick={() => onCancel(o.id)}
                  className="px-1.5 py-0.5 bg-[#F6465D]/10 text-[#F6465D] rounded text-[9px] font-medium hover:bg-[#F6465D]/20 transition-colors flex items-center gap-1"
                >
                  <span className="w-3 h-3 inline-block">✕</span>取消
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

/* ── History Tab ──────────────────────────────────────────────────── */

export interface HistoryTabProps {
  orders: Array<Order & { updated_at?: string; avg_price?: number; filled_quantity?: number; realized_pnl?: number }>
  loading: boolean
}

export function HistoryTab({ orders, loading }: HistoryTabProps) {
  if (loading) {
    return (
      <div className="p-4 space-y-2">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} variant="text" height={32} />
        ))}
      </div>
    )
  }
  if (!orders?.length) {
    return (
      <div className="py-6 flex items-center justify-center">
        <EmptyState
          title="暂无历史成交"
          description="还没有已成交的订单记录"
          className="py-1 border-0 text-[10px] [&>div:first-child]:hidden"
        />
      </div>
    )
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-[11px] whitespace-nowrap">
        <thead className="sticky top-0 bg-quant-bg-secondary z-10">
          <tr className="text-muted-foreground text-left">
            <th scope="col" className="px-1.5 py-1 font-medium">
              时间
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              币种
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              方向
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              价格
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              数量
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              盈亏
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              状态
            </th>
          </tr>
        </thead>
        <tbody>
          {orders.map((o) => {
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
                  {pnl >= 0 ? '+' : ''}
                  {pnl.toFixed(2)}
                </td>
                <td className="px-1.5 py-1">
                  <StatusTag status={o.status} />
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

/* ── Fills Tab ────────────────────────────────────────────────────── */

export interface FillsTabProps {
  fills: Array<
    Trade & { created_at?: string; timestamp?: number; avg_price?: number; filled_quantity?: number; fee?: number }
  >
  loading: boolean
  symbol: string
}

export function FillsTab({ fills, loading, symbol }: FillsTabProps) {
  if (loading) {
    return (
      <div className="p-4 space-y-2">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} variant="text" height={32} />
        ))}
      </div>
    )
  }
  if (!fills?.length) {
    return (
      <div className="py-6 flex items-center justify-center">
        <EmptyState
          title="暂无成交记录"
          description="还没有成交记录"
          className="py-1 border-0 text-[10px] [&>div:first-child]:hidden"
        />
      </div>
    )
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-[11px] whitespace-nowrap">
        <thead className="sticky top-0 bg-quant-bg-secondary z-10">
          <tr className="text-muted-foreground text-left">
            <th scope="col" className="px-1.5 py-1 font-medium">
              时间
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              币种
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              方向
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              价格
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              数量
            </th>
            <th scope="col" className="px-1.5 py-1 font-medium">
              手续费
            </th>
          </tr>
        </thead>
        <tbody>
          {fills.map((t, i) => (
            <tr key={t.id || i} className="border-t border-quant-border/40 hover:bg-white/[0.02]">
              <td className="px-1.5 py-1 text-muted-foreground">
                {formatDateTime(t.time || t.created_at || t.timestamp || 0)}
              </td>
              <td className="px-1.5 py-1 font-semibold">{t.symbol || symbol}</td>
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
  )
}

/* ── Assets Tab ─────────────────────────────────────────────────────── */

export interface AssetsTabProps {
  balances: Array<Record<string, unknown>>
  loading: boolean
}

export function AssetsTab({ balances, loading }: AssetsTabProps) {
  if (loading) {
    return (
      <div className="p-4 space-y-2">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} variant="text" height={32} />
        ))}
      </div>
    )
  }
  if (!balances?.length) {
    return (
      <div className="py-6 flex items-center justify-center">
        <EmptyState
          title="暂无资产数据"
          description="等待资产数据加载..."
          className="py-1 border-0 text-[10px] [&>div:first-child]:hidden"
        />
      </div>
    )
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-[11px] whitespace-nowrap">
        <thead className="sticky top-0 bg-quant-bg-secondary z-10">
          <tr className="text-muted-foreground text-left">
            <th scope="col" className="px-3 py-2 font-medium">
              币种
            </th>
            <th scope="col" className="text-right px-3 py-2 font-medium">
              可用
            </th>
            <th scope="col" className="text-right px-3 py-2 font-medium">
              冻结
            </th>
            <th scope="col" className="text-right px-3 py-2 font-medium">
              总计
            </th>
            <th scope="col" className="text-right px-3 py-2 font-medium">
              估值(USDT)
            </th>
          </tr>
        </thead>
        <tbody>
          {balances.map((b, i) => {
            const free = parseFloat(String(b.free ?? b.available ?? b.balance ?? 0))
            const locked = parseFloat(String(b.locked ?? b.frozen ?? 0))
            const total = free + locked
            return (
              <tr
                key={String(b.asset || b.symbol || i)}
                className="border-t border-quant-border/40 hover:bg-white/[0.02]"
              >
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
}

/* ── Plans Tab (Contract only) ──────────────────────────────────────── */

export interface PlansTabProps {
  orders: Order[]
  onCancel: (id: string) => void
}

export function PlansTab({ orders, onCancel }: PlansTabProps) {
  const planOrders = orders.filter((o) => o.tp_price || o.sl_price || o.type === 'STOP_LIMIT')
  return (
    <div>
      <div className="flex items-center justify-between px-3 py-2 border-b border-quant-border">
        <span className="text-xs font-medium text-foreground">计划委托</span>
        <span className="text-[10px] text-muted-foreground">止盈止损 / 条件单 / 跟踪止损</span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-[11px] whitespace-nowrap">
          <thead className="sticky top-0 bg-quant-bg-secondary z-10">
            <tr className="text-muted-foreground border-b border-quant-border">
              <th scope="col" className="text-left font-medium px-3 py-2">
                类型
              </th>
              <th scope="col" className="text-left font-medium px-3 py-2">
                币种
              </th>
              <th scope="col" className="text-right font-medium px-3 py-2">
                触发价
              </th>
              <th scope="col" className="text-right font-medium px-3 py-2">
                委托价
              </th>
              <th scope="col" className="text-right font-medium px-3 py-2">
                数量
              </th>
              <th scope="col" className="text-right font-medium px-3 py-2">
                状态
              </th>
              <th scope="col" className="text-right font-medium px-3 py-2">
                操作
              </th>
            </tr>
          </thead>
          <tbody>
            {orders
              .filter((o) => o.tp_price || o.sl_price)
              .map((o) => (
                <tr key={`tp-${o.id}`} className="border-b border-quant-border/40 hover:bg-white/[0.03]">
                  <td className="px-3 py-2">
                    <span
                      className={cn(
                        'text-[10px] px-1.5 py-0.5 rounded font-bold',
                        o.tp_price ? 'bg-[#0ECB81]/10 text-[#0ECB81]' : 'bg-[#F6465D]/10 text-[#F6465D]'
                      )}
                    >
                      {o.tp_price ? '止盈' : '止损'}
                    </span>
                  </td>
                  <td className="px-3 py-2 font-medium">{o.symbol}</td>
                  <td className="px-3 py-2 text-right font-mono">
                    {o.tp_price ? o.tp_price.toFixed(2) : o.sl_price?.toFixed(2)}
                  </td>
                  <td className="px-3 py-2 text-right font-mono text-muted-foreground">市价</td>
                  <td className="px-3 py-2 text-right font-mono">{o.quantity.toFixed(4)}</td>
                  <td className="px-3 py-2 text-right">
                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-quant-bg-tertiary text-muted-foreground">
                      监控中
                    </span>
                  </td>
                  <td className="px-3 py-2 text-right">
                    <button
                      onClick={() => onCancel(o.id)}
                      className="px-2 py-0.5 rounded text-[10px] bg-[#F6465D]/10 text-[#F6465D] hover:bg-[#F6465D]/20 transition-colors"
                    >
                      取消
                    </button>
                  </td>
                </tr>
              ))}
            {orders
              .filter((o) => o.type === 'STOP_LIMIT')
              .map((o) => (
                <tr key={`stop-${o.id}`} className="border-b border-quant-border/40 hover:bg-white/[0.03]">
                  <td className="px-3 py-2">
                    <span className="text-[10px] px-1.5 py-0.5 rounded font-bold bg-quant-gold/10 text-quant-gold">
                      条件单
                    </span>
                  </td>
                  <td className="px-3 py-2 font-medium">{o.symbol}</td>
                  <td className="px-3 py-2 text-right font-mono">{o.tp_price ? o.tp_price.toFixed(2) : '--'}</td>
                  <td className="px-3 py-2 text-right font-mono">{o.price.toFixed(2)}</td>
                  <td className="px-3 py-2 text-right font-mono">{o.quantity.toFixed(4)}</td>
                  <td className="px-3 py-2 text-right">
                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-quant-bg-tertiary text-muted-foreground">
                      {o.status}
                    </span>
                  </td>
                  <td className="px-3 py-2 text-right">
                    <button
                      onClick={() => onCancel(o.id)}
                      className="px-2 py-0.5 rounded text-[10px] bg-[#F6465D]/10 text-[#F6465D] hover:bg-[#F6465D]/20 transition-colors"
                    >
                      取消
                    </button>
                  </td>
                </tr>
              ))}
          </tbody>
        </table>
        {planOrders.length === 0 && (
          <div className="py-6 flex items-center justify-center">
            <EmptyState
              title="暂无计划委托"
              description="计划委托包括止盈止损和条件委托"
              className="py-1 border-0 text-[10px] [&>div:first-child]:hidden"
            />
          </div>
        )}
      </div>
    </div>
  )
}
