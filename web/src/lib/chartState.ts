// 图表状态持久化:指标(main/sub)与用户画线(overlays)存 localStorage,
// 重进页面自动恢复。klinecharts 无公开枚举接口,通过 pro 补丁透传的
// getIndicatorStore/getOverlayStore 访问内部仓库(_instances Map 的 key
// 即 paneId)。价格线 overlay(xt-chart-position/xt-chart-order-*)由
// ChartTrading 按数据实时同步,不属于用户画线,序列化时排除。

export interface SavedOverlay {
  name: string
  paneId: string
  points: unknown[]
  extendData?: unknown
  lock?: boolean
}

export interface SavedChartState {
  main: string[]
  sub: string[]
  overlays: SavedOverlay[]
}

const chartKey = (page: string) => `xt-trading-chart-${page}`

export function loadChartState(page: string): SavedChartState | null {
  try {
    const raw = localStorage.getItem(chartKey(page))
    if (!raw) return null
    const s = JSON.parse(raw) as SavedChartState
    if (!Array.isArray(s.main) || !Array.isArray(s.sub) || !Array.isArray(s.overlays)) return null
    return s
  } catch {
    return null
  }
}

export function saveChartState(page: string, state: SavedChartState): void {
  try {
    localStorage.setItem(chartKey(page), JSON.stringify(state))
  } catch {
    /* ignore */
  }
}

/* eslint-disable @typescript-eslint/no-explicit-any */
type AnyRec = Record<string, any>

function paneIdsOf(store: AnyRec | null | undefined): string[] {
  try {
    const m = store?._instances
    if (m instanceof Map) return Array.from(m.keys()) as string[]
  } catch {
    /* ignore */
  }
  return []
}

/** 从图表实例捕获当前指标与画线(纯序列化,可 JSON 化) */
export function captureChartState(api: unknown): SavedChartState {
  const a = api as AnyRec
  const result: SavedChartState = { main: [], sub: [], overlays: [] }
  try {
    const indStore = typeof a?.getIndicatorStore === 'function' ? a.getIndicatorStore() : null
    for (const paneId of paneIdsOf(indStore)) {
      const instances = indStore.getInstances(paneId) as AnyRec[] | undefined
      const names = (instances ?? [])
        .map((i) => i?.name)
        .filter((n): n is string => typeof n === 'string' && n.length > 0)
      if (paneId === 'candle_pane') result.main = names
      else result.sub.push(...names)
    }
  } catch {
    /* ignore */
  }
  try {
    const ovStore = typeof a?.getOverlayStore === 'function' ? a.getOverlayStore() : null
    for (const paneId of paneIdsOf(ovStore)) {
      const instances = ovStore.getInstances(paneId) as AnyRec[] | undefined
      for (const i of instances ?? []) {
        const id = String(i?.id ?? '')
        if (id.startsWith('xt-chart-')) continue // 程序同步的价格线,非用户画线
        const name = i?.name ?? i?._name
        if (typeof name !== 'string' || !name) continue
        const points = Array.isArray(i?.points) ? JSON.parse(JSON.stringify(i.points)) : []
        if (!points.length) continue
        result.overlays.push({
          name,
          paneId,
          points,
          extendData: i?.extendData != null ? JSON.parse(JSON.stringify(i.extendData)) : undefined,
          lock: i?.lock != null ? Boolean(i.lock) : undefined,
        })
      }
    }
  } catch {
    /* ignore */
  }
  return result
}

/** 把保存的画线重新贴回图表(指标在 KLineChartPro 初始化时用配置恢复) */
export function applyChartOverlays(api: unknown, state: SavedChartState | null): void {
  if (!state) return
  const a = api as AnyRec
  if (typeof a?.createOverlay !== 'function') return
  for (const ov of state.overlays) {
    try {
      a.createOverlay(
        {
          name: ov.name,
          points: ov.points,
          ...(ov.extendData !== undefined ? { extendData: ov.extendData } : {}),
          ...(ov.lock !== undefined ? { lock: ov.lock } : {}),
        },
        ov.paneId
      )
    } catch {
      /* ignore */
    }
  }
}
