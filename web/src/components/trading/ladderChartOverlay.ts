/* 阶梯单图表叠加层:基于 klinecharts 9.8 registerOverlay 的自定义价格线。
   - 未成交档位/目标/SL 各一条水平线 + 图内标签;
   - 可改价的线 lock=false,按压拖动改变价位,松手(onPressedMoveEnd)回调 onReprice;
   - 拖拽期间线跟随十字价(performEventPressedMove 把单点 value 钉在光标价)。*/

import { registerOverlay } from 'klinecharts'
import type {
  OverlayCreateFiguresCallbackParams,
  OverlayEvent,
  OverlayFigure,
  OverlayPerformEventParams,
} from 'klinecharts'
import type { ChartApiLike } from './chartOverlays'
import type { LadderChartLine, LadderLineDrag } from './ladderUtils'

export const LADDER_OVERLAY_NAME = 'xtLadderLine'

interface LadderExtendData {
  text?: string
  color?: string
  dashed?: boolean
  onReprice?: (price: number) => void
}

let registered = false

/** 注册阶梯线 overlay 模板(幂等;首次 sync 前调用)。 */
export function registerLadderOverlayOnce(): void {
  if (registered) return
  registered = true
  registerOverlay({
    name: LADDER_OVERLAY_NAME,
    totalStep: 1,
    needDefaultPointFigure: false,
    needDefaultXAxisFigure: false,
    needDefaultYAxisFigure: false,
    createPointFigures: ({ overlay, coordinates, bounding }: OverlayCreateFiguresCallbackParams): OverlayFigure[] => {
      const y = coordinates[0]?.y ?? 0
      const ext = (overlay.extendData ?? {}) as LadderExtendData
      const color = ext.color ?? '#999'
      return [
        {
          type: 'line',
          attrs: { coordinates: [{ x: 0, y }, { x: bounding.width, y }] },
          styles: { style: ext.dashed === false ? 'solid' : 'dashed', size: 1, color },
        },
        {
          type: 'text',
          attrs: { x: 6, y: y - 2, text: ext.text ?? '', align: 'left', baseline: 'bottom' },
          styles: { color, size: 10 },
        },
      ]
    },
    performEventPressedMove: ({ points, performPoint }: OverlayPerformEventParams) => {
      // 单点线:拖动即把价位钉到光标所在价
      if (performPoint?.value != null && points[0]) {
        points[0].value = performPoint.value
      }
    },
    onPressedMoveEnd: (event: OverlayEvent) => {
      const ext = (event.overlay.extendData ?? {}) as LadderExtendData
      const v = event.overlay.points?.[0]?.value
      if (typeof ext.onReprice === 'function' && typeof v === 'number' && v > 0) {
        ext.onReprice(v)
      }
      return false
    },
  })
}

export interface ActiveLadderLine {
  price: number
  text: string
}

/**
 * 增量同步阶梯线到图表(与 syncPriceLines 同策略,但用可拖拽的自定义 overlay):
 * 消失的移除;价格/文案变化的重建(extendData 回调与 lock 态随创建参数生效);
 * 未变化的不动(拖拽中的线不会被打断,服务端回写后随下一次数据刷新重建)。
 */
export function syncLadderOverlayLines(
  api: ChartApiLike,
  active: Map<string, ActiveLadderLine>,
  lines: LadderChartLine[],
  onReprice: (drag: LadderLineDrag, price: number) => void
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
    if (prev && prev.price === line.price && prev.text === line.text) continue
    if (prev) {
      try {
        api.removeOverlay(line.id)
      } catch {
        /* ignore */
      }
    }
    try {
      const drag = line.drag
      api.createOverlay({
        name: LADDER_OVERLAY_NAME,
        id: line.id,
        points: [{ value: line.price }],
        lock: !drag,
        extendData: {
          text: `${line.legend} ${line.text}`,
          color: line.color,
          dashed: line.dashed,
          onReprice: drag ? (v: number) => onReprice(drag, v) : undefined,
        } satisfies LadderExtendData,
      })
      active.set(line.id, { price: line.price, text: line.text })
    } catch {
      /* ignore */
    }
  }
}
