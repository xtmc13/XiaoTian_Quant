/* 阶梯智能单(Ladder Smart Orders)纯工具:
   价格分布计算(等差/等比)、图表价格线构建、进度与状态文案。
   不依赖 React / DOM / klinecharts,便于单测。*/

import type { LadderOrder, LadderStatus } from '@/types'
import { formatLinePrice, type ChartPriceLine } from './chartOverlays'

export type LadderDistribution = 'arithmetic' | 'geometric'

/**
 * 在 [low, high] 区间生成 count 个档位价格(升序)。
 * arithmetic: 等差(等价差);geometric: 等比(等涨跌幅)。count=1 时取 low。
 */
export function distributePrices(mode: LadderDistribution, count: number, low: number, high: number): number[] {
  const n = Math.max(1, Math.min(10, Math.trunc(count)))
  if (!(low > 0) || !(high > 0)) return []
  const lo = Math.min(low, high)
  const hi = Math.max(low, high)
  if (n === 1) return [lo]
  const out: number[] = []
  for (let i = 0; i < n; i++) {
    const t = i / (n - 1)
    const v = mode === 'geometric' ? lo * Math.pow(hi / lo, t) : lo + (hi - lo) * t
    out.push(v)
  }
  return out
}

/** 等分 USDT 总额到各档(返回与档位同长的金额数组,末档吸收舍入误差) */
export function splitAmount(totalUSDT: number, count: number): number[] {
  const n = Math.max(1, Math.trunc(count))
  if (!(totalUSDT > 0)) return Array(n).fill(0)
  const each = Math.floor((totalUSDT / n) * 1e8) / 1e8
  const out = Array(n).fill(each)
  out[n - 1] = Math.round((totalUSDT - each * (n - 1)) * 1e8) / 1e8
  return out
}

/** 等分止盈比例(合计=100,末档吸收舍入误差) */
export function splitClosePct(count: number): number[] {
  const n = Math.max(1, Math.trunc(count))
  const each = Math.floor((100 / n) * 100) / 100
  const out = Array(n).fill(each)
  out[n - 1] = Math.round((100 - each * (n - 1)) * 100) / 100
  return out
}

/* ── 图表价格线 ── */

export const LADDER_ENTRY_COLOR = '#4A9CFF' // 入场档 - 蓝
export const LADDER_TARGET_COLOR = '#0ECB81' // 止盈目标 - 绿
export const LADDER_AVG_COLOR = '#EAECEF' // 平均入场 - 白(虚线)
export const LADDER_SL_COLOR = '#F6465D' // 止损 - 红(虚线)

export interface LadderLineDrag {
  ladderId: string
  kind: 'entry' | 'target' | 'sl'
  index: number
}

export interface LadderChartLine extends ChartPriceLine {
  /** 可拖拽改价时携带定位信息;不可拖(已成交/终态)则缺省 */
  drag?: LadderLineDrag
}

const DRAGGABLE_LEG_STATUSES = new Set(['pending', 'open', 'waiting'])
export const LADDER_ACTIVE_STATUSES = new Set<LadderStatus>(['active', 'stopping'])

/**
 * 当前 symbol 的阶梯单图表线:
 * 未成交入场档(蓝) / 止盈目标(绿) / 平均入场价(白虚线) / 当前止损(红虚线)。
 * 活动单的可改价档位带 drag 信息(图表层据此启用拖拽)。
 */
export function buildLadderPriceLines(
  ladders: LadderOrder[] | null | undefined,
  symbol: string,
  precision: number
): LadderChartLine[] {
  if (!symbol || !ladders?.length) return []
  const out: LadderChartLine[] = []
  for (const lad of ladders) {
    if (!lad || lad.symbol !== symbol || !LADDER_ACTIVE_STATUSES.has(lad.status)) continue
    const isBuy = lad.side !== 'SELL'
    const amendable = lad.status === 'active'
    lad.entries?.forEach((e, i) => {
      if (!e || !DRAGGABLE_LEG_STATUSES.has(e.status) || !(e.price > 0)) return
      const drag = amendable && !(e.filled > 0) ? { ladderId: lad.id, kind: 'entry' as const, index: i } : undefined
      out.push({
        id: `xt-ladder-${lad.id}-e${i}`,
        price: e.price,
        text: formatLinePrice(e.price, precision),
        legend: `${isBuy ? '阶梯买' : '阶梯卖'}#${i + 1} ${e.filled > 0 ? `${e.filled}/${e.qty}` : ''}`.trim(),
        color: LADDER_ENTRY_COLOR,
        dashed: true,
        drag,
      })
    })
    lad.targets?.forEach((t, i) => {
      if (!t || !DRAGGABLE_LEG_STATUSES.has(t.status) || !(t.price > 0)) return
      const drag = amendable && !(t.filled > 0) ? { ladderId: lad.id, kind: 'target' as const, index: i } : undefined
      out.push({
        id: `xt-ladder-${lad.id}-t${i}`,
        price: t.price,
        text: formatLinePrice(t.price, precision),
        legend: `目标#${i + 1} ${t.close_pct}%${t.filled > 0 ? ` 已盈${t.filled}` : ''}`,
        color: LADDER_TARGET_COLOR,
        dashed: true,
        drag,
      })
    })
    if (lad.filled_qty > 0 && lad.avg_entry > 0) {
      out.push({
        id: `xt-ladder-${lad.id}-avg`,
        price: lad.avg_entry,
        text: formatLinePrice(lad.avg_entry, precision),
        legend: '阶梯均价',
        color: LADDER_AVG_COLOR,
        dashed: true,
      })
    }
    if (lad.current_sl > 0) {
      out.push({
        id: `xt-ladder-${lad.id}-sl`,
        price: lad.current_sl,
        text: formatLinePrice(lad.current_sl, precision),
        legend: lad.breakeven_armed ? 'SL(保本)' : '阶梯SL',
        color: LADDER_SL_COLOR,
        dashed: true,
        drag: amendable ? { ladderId: lad.id, kind: 'sl', index: -1 } : undefined,
      })
    }
  }
  return out
}

/* ── 进度与文案 ── */

export interface LadderProgress {
  /** 入场成交进度 0-1 */
  filledPct: number
  /** 已止盈(占已入场量) 0-1 */
  closedPct: number
  /** 已触发目标数 */
  targetsFilled: number
  targetsTotal: number
}

export function ladderProgress(lad: LadderOrder): LadderProgress {
  const total = lad.total_qty || 0
  const filled = lad.filled_qty || 0
  const closed = lad.closed_qty || 0
  return {
    filledPct: total > 0 ? Math.min(1, filled / total) : 0,
    closedPct: filled > 0 ? Math.min(1, closed / filled) : 0,
    targetsFilled: (lad.targets ?? []).filter((t) => t.status === 'filled').length,
    targetsTotal: (lad.targets ?? []).length,
  }
}

export function ladderStatusLabel(s: LadderStatus): string {
  switch (s) {
    case 'active':
      return '进行中'
    case 'stopping':
      return '平仓中'
    case 'completed':
      return '已完成'
    case 'cancelled':
      return '已撤销'
    case 'stopped':
      return '已止损'
    case 'flattened':
      return '已全平'
    case 'failed':
      return '失败'
    default:
      return s
  }
}

export function ladderLegStatusLabel(s: string): string {
  switch (s) {
    case 'pending':
      return '待挂出'
    case 'open':
      return '挂单中'
    case 'partial':
      return '部分成交'
    case 'filled':
      return '已成交'
    case 'cancelled':
      return '已撤销'
    case 'waiting':
      return '待激活'
    default:
      return s
  }
}
