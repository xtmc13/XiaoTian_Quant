import React, { useEffect, useMemo, useRef, useState } from 'react'
import { cn } from '@/lib/utils'
import {
  buildOrderPriceLines,
  buildPositionPriceLine,
  formatLinePrice,
  syncPriceLines,
  type ActivePriceLine,
  type ChartApiLike,
  type ChartPriceLine,
  type PositionLike,
} from './chartOverlays'
import type { ChartApi } from '@/lib/tradingHelpers'
import type { Order } from '@/types'

export interface ChartTradingProps {
  /** 图表容器(KLineChartPro 挂载点),用于监听点击/移动与定位浮层 */
  chartContainerRef: React.RefObject<HTMLDivElement | null>
  /** 图表实例(KLineChartPro 内部的 klinecharts Chart),页面已轮询就绪 */
  chartApiRef: React.MutableRefObject<ChartApi | null>
  symbol: string
  orders?: Order[] | null
  /** 当前持仓(用于均价线);传 null 不画 */
  position?: PositionLike | null
  /** 持仓线图例文案(现货为估算成本时可传 "持仓成本") */
  positionLabel?: string
  pricePrecision: number
  /** 图表上选定价格后的回调(填入限价输入框等),页面负责视觉反馈 */
  onPriceSelect: (price: number) => void
}

interface CrosshairState {
  price: number
  x: number
  y: number
}

const CANDLE_PANE_ID = 'candle_pane'

interface ChartSize {
  left?: number
  top?: number
  width?: number
  height?: number
}

/* klinecharts 图表实例的内部状态(版本固定 9.8.x,页面亦通过 _chartApi 访问内部) */
interface ChartInternal {
  _chartStore?: {
    getOverlayStore?: () => { getProgressInstanceInfo?: () => unknown }
  }
}

/**
 * 图表交易(Trade-from-chart):
 * 1. 点击价格轴/K 线区域 → 以该处价格回调 onPriceSelect(画线工具进行中不触发);
 * 2. 十字光标价格浮标按钮 → 点击同样回调;
 * 3. 持仓均价线(绿,实线)与未成交限价委托线(黄,虚线),随数据增量同步到图上并配图例。
 * 组件自身不渲染可见容器,所有浮层绝对定位于页面 chart 列(需 position: relative 祖先)。
 */
export function ChartTrading({
  chartContainerRef,
  chartApiRef,
  symbol,
  orders,
  position,
  positionLabel = '持仓均价',
  pricePrecision,
  onPriceSelect,
}: ChartTradingProps) {
  const lines = useMemo<ChartPriceLine[]>(
    () => [
      ...buildPositionPriceLine(position, pricePrecision, positionLabel),
      ...buildOrderPriceLines(symbol, orders, pricePrecision),
    ],
    [symbol, orders, position, positionLabel, pricePrecision]
  )

  /* KLineChartPro 实例就绪/重建(周期切换会重建)检测 */
  const [chartApi, setChartApi] = useState<ChartApi | null>(null)
  useEffect(() => {
    const timer = window.setInterval(() => {
      setChartApi((prev) => (chartApiRef.current === prev ? prev : chartApiRef.current))
    }, 300)
    return () => window.clearInterval(timer)
  }, [chartApiRef])

  /* 增量同步价格线;实例更换时重建记录,卸载时清理 */
  const overlayStateRef = useRef<{ api: ChartApi; active: Map<string, ActivePriceLine> } | null>(null)
  useEffect(() => {
    if (!chartApi) return
    if (overlayStateRef.current?.api !== chartApi) {
      overlayStateRef.current = { api: chartApi, active: new Map() }
    }
    syncPriceLines(chartApi as unknown as ChartApiLike, overlayStateRef.current.active, lines)
  }, [chartApi, lines])
  useEffect(() => {
    return () => {
      const state = overlayStateRef.current
      if (!state) return
      for (const id of Array.from(state.active.keys())) {
        try {
          state.api.removeOverlay(id)
        } catch {
          /* ignore */
        }
      }
      state.active.clear()
    }
  }, [])

  const onPriceSelectRef = useRef(onPriceSelect)
  onPriceSelectRef.current = onPriceSelect
  const [crosshair, setCrosshair] = useState<CrosshairState | null>(null)

  /* 点击价格轴/K线区域 → 价格;移动 → 跟踪十字价 */
  useEffect(() => {
    const container = chartContainerRef.current
    if (!container) return
    let rafId = 0

    const priceAtPoint = (clientX: number, clientY: number): number | null => {
      const api = chartApiRef.current
      if (!api) return null
      try {
        const rect = container.getBoundingClientRect()
        const x = clientX - rect.left
        const y = clientY - rect.top
        const main = api.getSize?.(CANDLE_PANE_ID, 'main') as ChartSize | null
        if (!main || typeof main.top !== 'number' || typeof main.height !== 'number') return null
        if (y < main.top || y > main.top + main.height) return null
        const point = api.convertFromPixel?.([{ x, y }], {
          paneId: CANDLE_PANE_ID,
          absolute: true,
        }) as { value?: number } | Array<{ value?: number }> | null
        const value = Array.isArray(point) ? point[0]?.value : point?.value
        if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return null
        return value
      } catch {
        return null
      }
    }

    const isUiClick = (target: EventTarget | null): boolean =>
      target instanceof Element &&
      target.closest(
        '.klinecharts-pro-period-bar,.klinecharts-pro-drawing-bar,.klinecharts-pro-modal,button,input,select,a,[role="button"]'
      ) !== null

    const isDrawingOverlay = (): boolean => {
      const api = chartApiRef.current as (ChartApi & ChartInternal) | null
      try {
        return api?._chartStore?.getOverlayStore?.().getProgressInstanceInfo?.() != null
      } catch {
        return false
      }
    }

    const handleClick = (e: MouseEvent) => {
      if (e.button !== 0 || isUiClick(e.target) || isDrawingOverlay()) return
      const price = priceAtPoint(e.clientX, e.clientY)
      if (price != null) onPriceSelectRef.current(price)
    }

    const handleMove = (e: MouseEvent) => {
      if (rafId) return
      rafId = window.requestAnimationFrame(() => {
        rafId = 0
        const rect = container.getBoundingClientRect()
        const price = priceAtPoint(e.clientX, e.clientY)
        setCrosshair(
          price != null ? { price, x: e.clientX - rect.left, y: e.clientY - rect.top } : null
        )
      })
    }

    const handleLeave = () => setCrosshair(null)

    container.addEventListener('click', handleClick)
    container.addEventListener('mousemove', handleMove)
    container.addEventListener('mouseleave', handleLeave)
    return () => {
      container.removeEventListener('click', handleClick)
      container.removeEventListener('mousemove', handleMove)
      container.removeEventListener('mouseleave', handleLeave)
      if (rafId) window.cancelAnimationFrame(rafId)
    }
  }, [chartContainerRef, chartApiRef])

  const containerWidth = chartContainerRef.current?.clientWidth ?? 0
  const buttonLeft = crosshair ? Math.min(crosshair.x + 12, Math.max(0, containerWidth - 160)) : 0
  const buttonTop = crosshair ? crosshair.y + 14 : 0

  return (
    <>
      {/* 价格线图例(左上角,避开 KLineChartPro 周期栏/画线栏) */}
      {lines.length > 0 && (
        <div
          aria-hidden
          className="pointer-events-none absolute left-14 top-11 z-10 flex select-none flex-col items-start gap-0.5"
        >
          {lines.map((l) => (
            <div
              key={l.id}
              className="flex items-center gap-1.5 rounded bg-black/45 px-1.5 py-0.5 font-mono text-[10px] leading-4"
            >
              <span
                className={cn('w-3 border-t', l.dashed ? 'border-dashed' : 'border-solid')}
                style={{ borderColor: l.color }}
              />
              <span className="text-gray-300">{l.legend}</span>
              <span style={{ color: l.color }}>{l.text}</span>
            </div>
          ))}
        </div>
      )}
      {/* 十字光标价格 → 一键填价 */}
      {crosshair && (
        <button
          type="button"
          onClick={() => onPriceSelectRef.current(crosshair.price)}
          style={{ left: buttonLeft, top: buttonTop }}
          className="absolute z-20 whitespace-nowrap rounded border border-quant-gold/60 bg-quant-bg-secondary/95 px-2 py-1 text-[11px] font-mono text-quant-gold shadow-lg transition-colors hover:bg-quant-gold/10"
        >
          以 {formatLinePrice(crosshair.price, pricePrecision)} 下单
        </button>
      )}
    </>
  )
}
