/* 图表交易(Trade-from-chart)纯工具:
   持仓均价线 / 未成交委托线的计算、格式化,以及与 klinecharts 实例同步价格线的逻辑。
   不依赖 React / DOM,便于单测。*/

export interface ChartPriceLine {
  id: string
  price: number
  /** 价格轴上的标注文本 */
  text: string
  /** 图例前缀(如 持仓均价 / 买入委托) */
  legend: string
  color: string
  dashed: boolean
}

export const POSITION_LINE_COLOR = '#0ECB81' // 持仓均价 - 绿
export const ORDER_LINE_COLOR = '#F0B90B' // 未成交限价委托 - 黄
/** 图上最多同时绘制的委托线数量,防止极端情况下拖慢渲染 */
export const MAX_ORDER_LINES = 20

export interface OrderLike {
  id: string
  symbol: string
  side: string
  type?: string
  status?: string
  price?: number
  quantity?: number
  avg_price?: number
  filled_quantity?: number
}

export interface PositionLike {
  avg_entry_price?: number
  entry_price?: number
  entryPrice?: number
  avgPrice?: number
  openPrice?: number
}

export const OPEN_ORDER_STATUSES = new Set(['NEW', 'PARTIALLY_FILLED', 'PENDING', 'OPEN'])

export function formatLinePrice(price: number, precision: number): string {
  if (!Number.isFinite(price) || price <= 0) return '--'
  return price.toFixed(Math.max(0, Math.min(8, Math.trunc(precision) || 0)))
}

/** 从持仓对象中按字段优先级提取开仓均价 */
export function extractEntryPrice(position: PositionLike | null | undefined): number | null {
  if (!position) return null
  const candidates = [
    position.avg_entry_price,
    position.entry_price,
    position.entryPrice,
    position.avgPrice,
    position.openPrice,
  ]
  for (const p of candidates) {
    const v = Number(p)
    if (Number.isFinite(v) && v > 0) return v
  }
  return null
}

/** 当前交易对的一条持仓均价线 */
export function buildPositionPriceLine(
  position: PositionLike | null | undefined,
  precision: number,
  label = '持仓均价'
): ChartPriceLine[] {
  const price = extractEntryPrice(position)
  if (price == null) return []
  return [
    {
      id: 'xt-chart-position',
      price,
      text: formatLinePrice(price, precision),
      legend: label,
      color: POSITION_LINE_COLOR,
      dashed: false,
    },
  ]
}

/** 当前交易对的未成交限价/条件委托线 */
export function buildOrderPriceLines(
  symbol: string,
  orders: OrderLike[] | null | undefined,
  precision: number
): ChartPriceLine[] {
  if (!symbol || !orders?.length) return []
  return orders
    .filter(
      (o) =>
        o != null &&
        o.symbol === symbol &&
        o.price != null &&
        Number(o.price) > 0 &&
        OPEN_ORDER_STATUSES.has(String(o.status ?? 'NEW').toUpperCase()) &&
        (o.type === 'LIMIT' || o.type === 'STOP_LIMIT')
    )
    .slice(0, MAX_ORDER_LINES)
    .map((o) => ({
      id: `xt-chart-order-${o.id}`,
      price: Number(o.price),
      text: formatLinePrice(Number(o.price), precision),
      legend: String(o.side).toUpperCase() === 'BUY' ? '买入委托' : '卖出委托',
      color: ORDER_LINE_COLOR,
      dashed: true,
    }))
}

/** 现货持仓成本估算:用已有成交历史做成交量加权平均(买入记正、卖出记负)。
    只有历史净数量 > 0 时才给出结果,否则返回 null(不画线)。 */
export function computeSpotAvgEntryPrice(
  symbol: string,
  historyOrders: OrderLike[] | null | undefined
): number | null {
  if (!symbol || !historyOrders?.length) return null
  let netQty = 0
  let cost = 0
  for (const o of historyOrders) {
    if (!o || o.symbol !== symbol) continue
    const qty = Number(o.filled_quantity ?? o.quantity ?? 0)
    const px = Number(o.avg_price ?? o.price ?? 0)
    if (!Number.isFinite(qty) || !Number.isFinite(px) || px <= 0) continue
    const signed = String(o.side).toUpperCase() === 'BUY' ? qty : -qty
    netQty += signed
    cost += signed * px
  }
  if (netQty <= 0 || cost <= 0) return null
  return cost / netQty
}

/* ── 与 klinecharts 实例同步价格线 ─────────────────────────────────── */

export interface ChartApiLike {
  createOverlay: (value: unknown) => unknown
  overrideOverlay: (value: unknown) => void
  removeOverlay: (id: string) => void
}

export interface ActivePriceLine {
  price: number
  text: string
}

/**
 * 以增量方式把期望的价格线同步到图表实例:
 * 已存在且未变化 → 不动;价格变化 → overrideOverlay 原位更新;消失 → removeOverlay;新增 → createOverlay。
 * 使用 klinecharts 内置 simpleTag overlay(横向射线 + 价格轴文本标注),单点即完整图形。
 * @param active 组件持有的"当前图表实例上已存在的线"记录,sync 会就地维护它
 */
export function syncPriceLines(
  api: ChartApiLike,
  active: Map<string, ActivePriceLine>,
  lines: ChartPriceLine[]
): void {
  const desired = new Map(lines.map((l) => [l.id, l]))
  for (const id of Array.from(active.keys())) {
    if (!desired.has(id)) {
      try {
        api.removeOverlay(id)
      } catch {
        /* ignore */
      }
      active.delete(id)
    }
  }
  for (const line of lines) {
    const prev = active.get(line.id)
    if (prev != null) {
      if (prev.price === line.price && prev.text === line.text) continue
      try {
        api.overrideOverlay({
          id: line.id,
          points: [{ value: line.price }],
          extendData: line.text,
        })
        active.set(line.id, { price: line.price, text: line.text })
      } catch {
        /* ignore */
      }
    } else {
      try {
        api.createOverlay({
          name: 'simpleTag',
          id: line.id,
          points: [{ value: line.price }],
          lock: true,
          extendData: line.text,
          styles: {
            line: { color: line.color, style: line.dashed ? 'dash' : 'solid', size: 1 },
            text: { color: line.color, size: 10 },
          },
        })
        active.set(line.id, { price: line.price, text: line.text })
      } catch {
        /* ignore */
      }
    }
  }
}
