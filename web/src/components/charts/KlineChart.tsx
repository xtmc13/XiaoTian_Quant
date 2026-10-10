/**
 * Enhanced KLineChart — wraps klinecharts with drawing tools, built-in indicators,
 * signal overlays, and theme support.  Ported from XiaoTianQuant KlineChart.vue.
 */
import { useEffect, useLayoutEffect, useRef, useState, useCallback, memo } from 'react'
import { init, dispose } from 'klinecharts'
import type { Chart, Styles, DeepPartial, CandleTooltipCustomCallbackData } from 'klinecharts'
import { LineType, CandleType, TooltipShowRule, TooltipShowType } from 'klinecharts'
import { cn } from '@/lib/utils'
import type { KLineBar } from '@/lib/technicalIndicators'
import { useDebounce } from '@/hooks/useAsyncData'
import {
  registerRuntimeOverlays,
  renderIndicatorOutput,
  setIndicatorRuntimeTheme,
  setIndicatorRuntimePrecision,
  detectPrecision,
  type IndicatorOutput,
  type CreatedResource,
  type KLineLike,
} from './indicatorRuntime'
import {
  PenLine, Minus, Columns2, Columns3, ArrowRight, GripHorizontal,
  DollarSign, Frame, TrendingUp, Eraser, Settings, X,
} from 'lucide-react'

/* ── Types ───────────────────────────────────────────────────────── */

interface Signal {
  timestamp: number
  price: number
  side: 'buy' | 'sell'
  text?: string
  color?: string
  markerStyle?: 'solid' | 'dashed'
  source?: string
}

interface IndicatorConfig {
  id: string
  name: string
  shortName: string
  type: 'line' | 'band' | 'macd' | 'adx'
  params: Record<string, number>
  style?: { color?: string; lineWidth?: number }
  visible?: boolean
  instanceId?: string
}

interface IndicatorTemplate {
  id: string
  name: string
  shortName: string
  type: 'line' | 'band' | 'macd' | 'adx'
  defaultParams: Record<string, number>
  paramSchema: { key: string; label: string; type: string; min: number; max: number; step?: number }[]
}

interface KlineChartEnhancedProps {
  data?: KLineBar[]
  signals?: Signal[]
  /**
   * 指标执行返回的原始 output（QuantDinger 契约：plots/signals/layers）。
   * 由 indicatorRuntime 按量化丁格同款管线渲染：
   * plots→指标（line/bar/circle+逐点颜色尺寸，主图叠加或副图）、
   * signals→signalTag 标记（锚定 K 线高/低点）、layers→区域/线段/标签。
   */
  indicatorOutput?: IndicatorOutput | null
  loading?: boolean
  error?: string | null
  theme?: 'dark' | 'light'
  height?: number
  symbol?: string
  onRetry?: () => void
  drawingBarVisible?: boolean
  activeIndicators?: IndicatorConfig[]
  onActiveIndicatorsChange?: (indicators: IndicatorConfig[]) => void
}

/* ── Drawing tools ──────────────────────────────────────────────── */

const DRAWING_TOOLS = [
  { name: 'measure', title: '测量', icon: TrendingUp, overlay: 'priceRangeMeasure' },
  { name: 'line', title: '趋势线', icon: PenLine, overlay: 'segment' },
  { name: 'horizontalLine', title: '水平线', icon: Minus, overlay: 'horizontalStraightLine' },
  { name: 'verticalLine', title: '垂直线', icon: Columns2, overlay: 'verticalStraightLine' },
  { name: 'ray', title: '射线', icon: ArrowRight, overlay: 'rayLine' },
  { name: 'straightLine', title: '直线', icon: GripHorizontal, overlay: 'straightLine' },
  { name: 'parallelStraightLine', title: '平行线', icon: Columns3, overlay: 'parallelStraightLine' },
  { name: 'priceLine', title: '价格线', icon: DollarSign, overlay: 'priceLine' },
  { name: 'priceChannelLine', title: '价格通道', icon: Frame, overlay: 'priceChannelLine' },
  { name: 'fibonacciLine', title: '斐波那契', icon: TrendingUp, overlay: 'fibonacciLine' },
]

/* ── Indicator templates ────────────────────────────────────────── */

const INDICATOR_TEMPLATES: IndicatorTemplate[] = [
  { id: 'sma', name: 'SMA', shortName: 'SMA', type: 'line', defaultParams: { length: 20 },
    paramSchema: [{ key: 'length', label: '周期', type: 'number', min: 1, max: 300, step: 1 }] },
  { id: 'ema', name: 'EMA', shortName: 'EMA', type: 'line', defaultParams: { length: 20 },
    paramSchema: [{ key: 'length', label: '周期', type: 'number', min: 1, max: 300, step: 1 }] },
  { id: 'rsi', name: 'RSI', shortName: 'RSI', type: 'line', defaultParams: { length: 14 },
    paramSchema: [{ key: 'length', label: '周期', type: 'number', min: 1, max: 200, step: 1 }] },
  { id: 'macd', name: 'MACD', shortName: 'MACD', type: 'macd', defaultParams: { fast: 12, slow: 26, signal: 9 },
    paramSchema: [
      { key: 'fast', label: '快线', type: 'number', min: 1, max: 100, step: 1 },
      { key: 'slow', label: '慢线', type: 'number', min: 2, max: 200, step: 1 },
      { key: 'signal', label: '信号线', type: 'number', min: 1, max: 100, step: 1 },
    ] },
  { id: 'bb', name: '布林带', shortName: 'BOLL', type: 'band', defaultParams: { length: 20, mult: 2 },
    paramSchema: [
      { key: 'length', label: '周期', type: 'number', min: 1, max: 300, step: 1 },
      { key: 'mult', label: '倍数', type: 'number', min: 0.1, max: 10, step: 0.1 },
    ] },
  { id: 'atr', name: 'ATR', shortName: 'ATR', type: 'line', defaultParams: { period: 14 },
    paramSchema: [{ key: 'period', label: '周期', type: 'number', min: 1, max: 200, step: 1 }] },
  { id: 'cci', name: 'CCI', shortName: 'CCI', type: 'line', defaultParams: { length: 20 },
    paramSchema: [{ key: 'length', label: '周期', type: 'number', min: 1, max: 200, step: 1 }] },
  { id: 'williams', name: 'Williams %R', shortName: 'W%R', type: 'line', defaultParams: { length: 14 },
    paramSchema: [{ key: 'length', label: '周期', type: 'number', min: 1, max: 200, step: 1 }] },
  { id: 'mfi', name: 'MFI', shortName: 'MFI', type: 'line', defaultParams: { length: 14 },
    paramSchema: [{ key: 'length', label: '周期', type: 'number', min: 1, max: 200, step: 1 }] },
  { id: 'adx', name: 'ADX', shortName: 'ADX', type: 'adx', defaultParams: { length: 14 },
    paramSchema: [{ key: 'length', label: '周期', type: 'number', min: 1, max: 200, step: 1 }] },
  { id: 'obv', name: 'OBV', shortName: 'OBV', type: 'line', defaultParams: {}, paramSchema: [] },
  { id: 'adosc', name: 'ADOSC', shortName: 'ADOSC', type: 'line', defaultParams: { fast: 3, slow: 10 },
    paramSchema: [
      { key: 'fast', label: '快线', type: 'number', min: 1, max: 100, step: 1 },
      { key: 'slow', label: '慢线', type: 'number', min: 2, max: 200, step: 1 },
    ] },
  { id: 'ad', name: 'A/D线', shortName: 'AD', type: 'line', defaultParams: {}, paramSchema: [] },
  { id: 'kdj', name: 'KDJ', shortName: 'KDJ', type: 'line', defaultParams: { period: 9, k: 3, d: 3 },
    paramSchema: [
      { key: 'period', label: '周期', type: 'number', min: 1, max: 100, step: 1 },
      { key: 'k', label: 'K平滑', type: 'number', min: 1, max: 20, step: 1 },
      { key: 'd', label: 'D平滑', type: 'number', min: 1, max: 20, step: 1 },
    ] },
]

const INDICATOR_COLORS_DARK = ['#2962ff', '#ff9800', '#ab47bc', '#26a69a', '#ef5350', '#78909c']
const INDICATOR_COLORS_LIGHT = ['#2962ff', '#f57c00', '#7b1fa2', '#00897b', '#d32f2f', '#546e7a']

/* ── 图表主题（QuantDinger updateChartTheme 移植） ─────────── */

const CHART_FONT_FAMILY = 'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
const CHART_UP_COLOR = '#089981'
const CHART_DOWN_COLOR = '#f23645'

const CHART_THEME_COLORS = {
  dark: {
    textColorSecondary: '#787b86',
    borderColor: '#2a2a2a',
    gridLineColor: 'rgba(255, 255, 255, 0.055)',
    gridLineColorDashed: '#4a4d57',
    axisLabelColor: '#787b86',
  },
  light: {
    textColorSecondary: '#6a6d78',
    borderColor: '#e0e3eb',
    gridLineColor: '#f0f3fa',
    gridLineColorDashed: '#d6d9e0',
    axisLabelColor: '#6a6d78',
  },
}

function chartStylesFor(isDark: boolean, precision: number): DeepPartial<Styles> {
  const theme = isDark ? CHART_THEME_COLORS.dark : CHART_THEME_COLORS.light
  return {
    grid: {
      show: true,
      horizontal: { show: true, color: theme.gridLineColor, style: LineType.Solid, size: 1 },
      vertical: { show: true, color: theme.gridLineColor, style: LineType.Solid, size: 1 },
    },
    candle: {
      type: CandleType.CandleSolid,
      bar: {
        upColor: CHART_UP_COLOR, downColor: CHART_DOWN_COLOR, noChangeColor: theme.axisLabelColor,
        upBorderColor: CHART_UP_COLOR, downBorderColor: CHART_DOWN_COLOR, noChangeBorderColor: theme.axisLabelColor,
        upWickColor: CHART_UP_COLOR, downWickColor: CHART_DOWN_COLOR, noChangeWickColor: theme.axisLabelColor,
      },
      priceMark: {
        show: true,
        high: { show: true, color: theme.axisLabelColor, textOffset: 5, textSize: 10, textFamily: CHART_FONT_FAMILY, textWeight: 'normal' },
        low: { show: true, color: theme.axisLabelColor, textOffset: 5, textSize: 10, textFamily: CHART_FONT_FAMILY, textWeight: 'normal' },
        last: {
          show: true,
          upColor: CHART_UP_COLOR, downColor: CHART_DOWN_COLOR, noChangeColor: theme.axisLabelColor,
          line: { show: true, style: LineType.Dashed, dashedValue: [3, 3], size: 1 },
          text: {
            show: true, color: '#ffffff', size: 11, family: CHART_FONT_FAMILY, weight: '600',
            paddingLeft: 5, paddingRight: 5, paddingTop: 3, paddingBottom: 3, borderRadius: 2,
          },
        },
      },
      tooltip: {
        showRule: TooltipShowRule.Always, showType: TooltipShowType.Standard,
        offsetLeft: 8, offsetTop: 31, offsetRight: 8, offsetBottom: 5,
        text: {
          size: 11, family: CHART_FONT_FAMILY, weight: 'normal', color: theme.textColorSecondary,
          marginLeft: 4, marginRight: 6, marginTop: 2, marginBottom: 2,
        },
        custom: ({ current }: CandleTooltipCustomCallbackData) => {
          const d = new Date(current.timestamp)
          const pad = (n: number) => String(n).padStart(2, '0')
          return [
            { title: '时间', value: `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}` },
            { title: '开', value: current.open.toFixed(precision) },
            { title: '高', value: current.high.toFixed(precision) },
            { title: '低', value: current.low.toFixed(precision) },
            { title: '收', value: current.close.toFixed(precision) },
            { title: '量', value: Number(current.volume ?? 0).toFixed(0) },
          ]
        },
      },
      area: { point: { animation: false, animationDuration: 0 } },
    },
    indicator: {
      ohlc: {
        upColor: 'rgba(8, 153, 129, 0.72)',
        downColor: 'rgba(242, 54, 69, 0.72)',
        noChangeColor: theme.axisLabelColor,
      },
      lastValueMark: { show: false },
      tooltip: {
        showRule: TooltipShowRule.Always, showType: TooltipShowType.Standard, showName: true, showParams: true,
        offsetLeft: 8, offsetTop: 4, offsetRight: 8, offsetBottom: 4,
        text: {
          size: 11, family: CHART_FONT_FAMILY, weight: 'normal', color: theme.textColorSecondary,
          marginLeft: 4, marginRight: 6, marginTop: 2, marginBottom: 2,
        },
      },
    },
    xAxis: {
      show: true,
      axisLine: { show: false, color: theme.borderColor },
      tickLine: { show: false, color: theme.borderColor, size: 1, length: 0 },
      tickText: { show: true, color: theme.axisLabelColor, size: 11, family: CHART_FONT_FAMILY, weight: 'normal', marginStart: 4, marginEnd: 4 },
    },
    yAxis: {
      show: true,
      axisLine: { show: false, color: theme.borderColor },
      tickLine: { show: false, color: theme.borderColor, size: 1, length: 0 },
      tickText: { show: true, color: theme.axisLabelColor, size: 11, family: CHART_FONT_FAMILY, weight: 'normal', marginStart: 5, marginEnd: 5 },
    },
    separator: {
      size: 1, color: theme.borderColor, fill: true,
      activeBackgroundColor: isDark ? 'rgba(120, 123, 134, 0.12)' : 'rgba(106, 109, 120, 0.08)',
    },
    crosshair: {
      show: true,
      horizontal: {
        show: true,
        line: { show: true, style: LineType.Dashed, color: theme.gridLineColorDashed, size: 1, dashedValue: [4, 3] },
        text: {
          show: true, color: '#ffffff', backgroundColor: isDark ? '#363a45' : '#6a6d78',
          size: 11, family: CHART_FONT_FAMILY, weight: 'normal',
          paddingLeft: 5, paddingRight: 5, paddingTop: 3, paddingBottom: 3, borderRadius: 2,
        },
      },
      vertical: {
        show: true,
        line: { show: true, style: LineType.Dashed, color: theme.gridLineColorDashed, size: 1, dashedValue: [4, 3] },
        text: {
          show: true, color: '#ffffff', backgroundColor: isDark ? '#363a45' : '#6a6d78',
          size: 11, family: CHART_FONT_FAMILY, weight: 'normal',
          paddingLeft: 5, paddingRight: 5, paddingTop: 3, paddingBottom: 3, borderRadius: 2,
        },
      },
    },
    overlay: {
      point: {
        color: isDark ? '#6b7a8c' : '#94a0ad',
        borderColor: isDark ? '#6b7a8c' : '#94a0ad',
        borderSize: 2,
        radius: 4,
        activeColor: '#26a69a',
        activeBorderColor: '#26a69a',
        activeBorderSize: 2,
        activeRadius: 5,
      },
      text: {
        color: isDark ? 'rgba(236, 240, 245, 0.9)' : 'rgba(38, 44, 52, 0.88)',
        backgroundColor: 'transparent',
        size: 11,
      },
    },
  }
}

/* ── klinecharts 内置指标映射（没有的模板保持只切 chip） ────── */

interface BuiltinIndicatorSpec {
  name: string
  overlay: boolean
  calcParams: (params: Record<string, number>) => number[]
}

const BUILTIN_INDICATORS: Record<string, BuiltinIndicatorSpec> = {
  sma: { name: 'MA', overlay: true, calcParams: (p) => [p.length ?? 20] },
  ema: { name: 'EMA', overlay: true, calcParams: (p) => [p.length ?? 20] },
  rsi: { name: 'RSI', overlay: false, calcParams: (p) => [p.length ?? 14] },
  macd: { name: 'MACD', overlay: false, calcParams: (p) => [p.fast ?? 12, p.slow ?? 26, p.signal ?? 9] },
  bb: { name: 'BOLL', overlay: true, calcParams: (p) => [p.length ?? 20, p.mult ?? 2] },
  cci: { name: 'CCI', overlay: false, calcParams: (p) => [p.length ?? 20] },
  williams: { name: 'WR', overlay: false, calcParams: (p) => [p.length ?? 14] },
  adx: { name: 'DMI', overlay: false, calcParams: (p) => [p.length ?? 14, 6] },
  obv: { name: 'OBV', overlay: false, calcParams: () => [] },
  kdj: { name: 'KDJ', overlay: false, calcParams: (p) => [p.period ?? 9, p.k ?? 3, p.d ?? 3] },
}

const SUB_PANE_OPTIONS = { height: 112, minHeight: 64, dragEnabled: true }

/* ═════════════════════════════════════════════════════════════════ */
/*  Main Component                                                   */
/* ═════════════════════════════════════════════════════════════════ */

export const KlineChart = memo(function KlineChart({
  data,
  signals,
  indicatorOutput,
  loading,
  error,
  theme = 'dark',
  height = 500,
  onRetry,
  drawingBarVisible = true,
  activeIndicators = [],
  onActiveIndicatorsChange,
}: KlineChartEnhancedProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<Chart | null>(null)
  const pendingDataRef = useRef<KLineBar[] | undefined>(undefined)
  const [activeDrawingTool, setActiveDrawingTool] = useState<string | null>(null)
  const [editorTarget, setEditorTarget] = useState<IndicatorConfig | null>(null)
  const [editorForm, setEditorForm] = useState<Record<string, number>>({})
  const drawingIdsRef = useRef<string[]>([])
  /** 指标运行时创建的资源（指标实例 + overlay），重跑前整组清理 */
  const runtimeCreatedRef = useRef<CreatedResource>({ indicators: [], overlays: [] })
  const legacySignalIdsRef = useRef<string[]>([])
  const indicatorOutputRef = useRef<IndicatorOutput | null>(null)
  indicatorOutputRef.current = indicatorOutput ?? null

  // Throttle data updates to avoid render storms from high-frequency WS pushes
  const debouncedData = useDebounce(data, 300)
  const debouncedSignals = useDebounce(signals, 200)
  const debouncedIndicatorOutput = useDebounce(indicatorOutput, 250)

  const isDark = theme === 'dark'
  const colors = isDark ? INDICATOR_COLORS_DARK : INDICATOR_COLORS_LIGHT

  // Register runtime overlays once (idempotent)
  useEffect(() => { registerRuntimeOverlays() }, [])

  /* ─── QuantDinger-style indicator runtime render ───
   * 清理上一次创建的资源 → 渲染当前 output。
   * calc 闭包按时间戳对齐，K 线推送更新时图表自动重算，无需重复渲染。
   */
  const renderRuntime = useCallback(() => {
    const chart = chartRef.current as unknown as {
      removeIndicator?: (paneId: string, name: string) => void
      removeOverlay?: (id: string) => void
    } | null
    if (!chart) return
    setIndicatorRuntimeTheme(isDark ? 'dark' : 'light')
    setIndicatorRuntimePrecision(detectPrecision((debouncedData ?? []) as KLineLike[]))
    runtimeCreatedRef.current.indicators.forEach(({ paneId, name }) => {
      try { chart.removeIndicator?.(paneId, name) } catch { /* ignore */ }
    })
    runtimeCreatedRef.current.overlays.forEach((id) => {
      try { chart.removeOverlay?.(id) } catch { /* ignore */ }
    })
    runtimeCreatedRef.current = { indicators: [], overlays: [] }
    const output = indicatorOutputRef.current
    const klines = (debouncedData ?? []) as unknown as KLineLike[]
    if (!output || !klines.length) return
    renderIndicatorOutput(chart as never, output, klines, runtimeCreatedRef.current)
  }, [isDark, debouncedData])

  const renderRuntimeRef = useRef(renderRuntime)
  renderRuntimeRef.current = renderRuntime
  const activeIndicatorsRef = useRef(activeIndicators)
  activeIndicatorsRef.current = activeIndicators
  /** 内置指标实例：instanceId/id → 内置名 + paneId（重建图表后按此重放） */
  const builtinIndicatorsRef = useRef(new Map<string, { name: string; paneId: string }>())

  useEffect(() => { renderRuntime() }, [debouncedIndicatorOutput, renderRuntime])

  /* ─── klinecharts 内置指标创建/移除 ─── */
  const syncBuiltinIndicator = useCallback((indicator: IndicatorConfig) => {
    const chart = chartRef.current
    const spec = BUILTIN_INDICATORS[indicator.id]
    if (!chart || !spec) return
    const key = indicator.instanceId || indicator.id
    const calcParams = spec.calcParams(indicator.params)
    const existing = builtinIndicatorsRef.current.get(key)
    try {
      if (existing) {
        chart.overrideIndicator({ name: spec.name, calcParams }, existing.paneId)
        return
      }
      const paneId = spec.overlay
        ? chart.createIndicator({ name: spec.name, calcParams }, true, { id: 'candle_pane' })
        : chart.createIndicator({ name: spec.name, calcParams }, false, SUB_PANE_OPTIONS)
      if (paneId) builtinIndicatorsRef.current.set(key, { name: spec.name, paneId })
    } catch { /* ignore */ }
  }, [])

  const removeBuiltinIndicator = useCallback((indicator: IndicatorConfig) => {
    const key = indicator.instanceId || indicator.id
    const existing = builtinIndicatorsRef.current.get(key)
    if (!existing) return
    try { chartRef.current?.removeIndicator(existing.paneId, existing.name) } catch { /* ignore */ }
    builtinIndicatorsRef.current.delete(key)
  }, [])

  /* ─── Chart Init (re-runs when loading ends or theme flips) ─── */
  useLayoutEffect(() => {
    if (loading) return
    const el = containerRef.current
    if (!el) return

    try {
      const chart = init(el, {
        styles: chartStylesFor(isDark, detectPrecision((pendingDataRef.current ?? []) as KLineLike[])),
      })
      if (!chart) return
      chartRef.current = chart
      ;(window as unknown as Record<string, unknown>).__kcChart = chart

      try {
        chart.setOffsetRightDistance(64)
        chart.setRightMinVisibleBarCount(4)
        chart.setBarSpace(8)
      } catch { /* ignore */ }

      // VOL 成交量副图（QuantDinger 同款默认面板）
      try {
        chart.createIndicator('VOL', false, SUB_PANE_OPTIONS)
      } catch { /* ignore */ }

      // Apply data that arrived before chart init
      if (pendingDataRef.current?.length) {
        chart.applyNewData(toChartData(pendingDataRef.current))
      }

      // 图表重建后重放内置指标与指标运行时资源（旧实例已随 dispose 销毁）
      builtinIndicatorsRef.current.clear()
      activeIndicatorsRef.current.forEach((indicator) => syncBuiltinIndicator(indicator))
      renderRuntimeRef.current()

      const ro = new ResizeObserver(() => { try { chart.resize() } catch { /* ignore */ } })
      ro.observe(el)

      return () => {
        ro.disconnect()
        try { dispose(chart) } catch { /* ignore */ }
        chartRef.current = null
      }
    } catch {
      // 图表初始化错误，已在 UI 中处理
    }
  }, [loading, isDark, syncBuiltinIndicator])

  /* ─── Helper: convert KLineBar[] to klinecharts format ─── */
  function toChartData(src: KLineBar[]) {
    return src.map(d => {
      const rawTs = d.timestamp ?? d.time ?? 0
      return {
        timestamp: rawTs > 1e10 ? rawTs : rawTs * 1000,
        open: d.open, high: d.high, low: d.low, close: d.close,
        volume: d.volume || 0,
      }
    })
  }

  /* ─── Apply data ─── */
  useEffect(() => {
    pendingDataRef.current = debouncedData
    if (!chartRef.current || !debouncedData?.length) return
    chartRef.current.applyNewData(toChartData(debouncedData))
    try { chartRef.current.scrollToRealTime() } catch { /* ignore */ }
  }, [debouncedData])

  /* ─── Apply signals（旧点位格式，回测等场景） ─── */
  useEffect(() => {
    const chart = chartRef.current as unknown as {
      createOverlay?: (overlay: unknown, paneId?: string) => string | null
      removeOverlay?: (id: string) => void
    } | null
    if (!chart?.createOverlay) return
    legacySignalIdsRef.current.forEach(id => {
      try { chart.removeOverlay?.(id) } catch { /* ignore */ }
    })
    legacySignalIdsRef.current = []
    if (!debouncedSignals?.length) return

    debouncedSignals.forEach(s => {
      try {
        const overlayId = chart.createOverlay?.({
          name: 'signalTag',
          points: [{ timestamp: s.timestamp, value: s.price }],
          extendData: {
            text: s.text || (s.side === 'buy' ? 'B' : 'S'),
            color: s.color || (s.side === 'buy' ? '#22C55E' : '#EF4444'),
            side: s.side,
            markerStyle: s.markerStyle || 'solid',
            source: s.source || '',
          },
          lock: true,
        }, 'candle_pane')
        if (overlayId) legacySignalIdsRef.current.push(String(overlayId))
      } catch { /* ignore */ }
    })
  }, [debouncedSignals])

  /* ─── Drawing tool selection ─── */
  const selectDrawingTool = useCallback((toolName: string) => {
    if (!chartRef.current) return
    if (activeDrawingTool === toolName) {
      setActiveDrawingTool(null)
      return
    }
    setActiveDrawingTool(toolName)
    const tool = DRAWING_TOOLS.find(t => t.name === toolName)
    if (!tool) return
    try {
      const id = (chartRef.current as unknown as { createOverlay: (...args: unknown[]) => unknown }).createOverlay({ name: tool.overlay, lock: false })
      if (id) drawingIdsRef.current.push(String(id))
    } catch { setActiveDrawingTool(null) }
  }, [activeDrawingTool])

  const clearDrawings = useCallback(() => {
    if (!chartRef.current) return
    drawingIdsRef.current.forEach(id => {
      try { chartRef.current?.removeOverlay?.(id) } catch { /* ignore */ }
    })
    drawingIdsRef.current = []
    setActiveDrawingTool(null)
  }, [])

  /* ─── Indicator management ─── */
  const toggleIndicator = useCallback((template: IndicatorTemplate) => {
    if (!onActiveIndicatorsChange) return
    const existing = activeIndicators.find(i => i.id === template.id)
    if (existing) {
      removeBuiltinIndicator(existing)
      onActiveIndicatorsChange(activeIndicators.filter(i => i.id !== template.id))
    } else {
      const color = colors[activeIndicators.length % colors.length]
      const next: IndicatorConfig = {
        id: template.id,
        name: template.name,
        shortName: template.shortName,
        type: template.type,
        params: { ...template.defaultParams },
        style: { color, lineWidth: 2 },
        visible: true,
        instanceId: `${template.id}_${Date.now()}`,
      }
      syncBuiltinIndicator(next)
      onActiveIndicatorsChange([...activeIndicators, next])
    }
  }, [activeIndicators, onActiveIndicatorsChange, colors, syncBuiltinIndicator, removeBuiltinIndicator])

  const openEditor = useCallback((indicator: IndicatorConfig) => {
    setEditorTarget(indicator)
    setEditorForm({ ...indicator.params })
  }, [])

  const applyEditor = useCallback(() => {
    if (!editorTarget || !onActiveIndicatorsChange) return
    const updatedIndicator = { ...editorTarget, params: { ...editorForm } }
    syncBuiltinIndicator(updatedIndicator)
    const updated = activeIndicators.map(i =>
      (i.instanceId || i.id) === (editorTarget.instanceId || editorTarget.id)
        ? updatedIndicator
        : i
    )
    onActiveIndicatorsChange(updated)
    setEditorTarget(null)
  }, [editorTarget, editorForm, activeIndicators, onActiveIndicatorsChange, syncBuiltinIndicator])

  const template = editorTarget ? INDICATOR_TEMPLATES.find(t => t.id === editorTarget.id) : null

  if (loading) {
    return (
      <div className="flex items-center justify-center rounded-lg bg-quant-bg-secondary" style={{ height }}>
        <div className="animate-spin h-6 w-6 border-2 border-quant-gold/30 border-t-quant-gold rounded-full" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center gap-3 rounded-lg bg-quant-bg-secondary" style={{ height }}>
        <span className="text-sm text-quant-red">{error}</span>
        {onRetry && (
          <button onClick={onRetry} className="px-3 py-1 rounded bg-quant-gold/10 text-quant-gold text-xs hover:bg-quant-gold/20">
            重试
          </button>
        )}
      </div>
    )
  }

  return (
    <div className="flex rounded-lg overflow-hidden border border-quant-border bg-quant-bg" style={{ height }}>
      {/* Drawing toolbar */}
      {drawingBarVisible && (
        <div className="w-10 shrink-0 bg-quant-bg-secondary border-r border-quant-border flex flex-col items-center py-1.5 gap-0.5 overflow-y-auto">
          {DRAWING_TOOLS.map(tool => (
            <button
              key={tool.name}
              onClick={() => selectDrawingTool(tool.name)}
              className={cn(
                'w-7 h-7 flex items-center justify-center rounded transition-colors text-[11px]',
                activeDrawingTool === tool.name
                  ? 'bg-quant-gold/20 text-quant-gold shadow-sm'
                  : 'text-muted-foreground hover:text-foreground hover:bg-white/[0.08]',
              )}
              title={tool.title}
            >
              <tool.icon className="w-3.5 h-3.5" />
            </button>
          ))}
          <div className="w-4 h-px bg-quant-border/50 my-0.5" />
          <button onClick={clearDrawings} className="w-7 h-7 flex items-center justify-center rounded text-muted-foreground hover:text-quant-red hover:bg-quant-red/10 transition-colors" title="清除画线">
            <Eraser className="w-3.5 h-3.5" />
          </button>
        </div>
      )}

      {/* Chart content area */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Indicator toolbar */}
        <div className="flex items-center gap-0.5 px-2 py-1 border-b border-quant-border bg-quant-bg-secondary overflow-x-auto shrink-0">
          {INDICATOR_TEMPLATES.map(tpl => {
            const active = activeIndicators.some(i => i.id === tpl.id)
            return (
              <button
                key={tpl.id}
                onClick={() => toggleIndicator(tpl)}
                className={cn(
                  'px-1.5 py-0.5 rounded text-[10px] font-medium whitespace-nowrap transition-colors',
                  active
                    ? 'bg-quant-gold/15 text-quant-gold'
                    : 'text-muted-foreground/70 hover:text-foreground hover:bg-white/[0.06]',
                )}
              >
                {tpl.shortName}
              </button>
            )
          })}
        </div>

        {/* Active indicator chips */}
        {activeIndicators.length > 0 && (
          <div className="flex items-center gap-1 px-2 py-1 border-b border-quant-border bg-quant-bg-tertiary overflow-x-auto shrink-0">
            {activeIndicators.map(ind => (
              <span
                key={ind.instanceId || ind.id}
                className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] bg-quant-card border border-quant-border"
              >
                <span className="text-foreground">{ind.shortName}</span>
                <button onClick={() => { removeBuiltinIndicator(ind); onActiveIndicatorsChange?.(activeIndicators.filter(i => (i.instanceId || i.id) !== (ind.instanceId || ind.id))) }} className="text-muted-foreground hover:text-quant-red">
                  <X className="w-2.5 h-2.5" />
                </button>
                <button onClick={() => openEditor(ind)} className="text-muted-foreground hover:text-quant-gold">
                  <Settings className="w-2.5 h-2.5" />
                </button>
              </span>
            ))}
          </div>
        )}

        {/* Chart canvas */}
        <div ref={containerRef} className="flex-1 min-h-0" />
      </div>

      {/* Indicator editor modal */}
      {editorTarget && template && (
        <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setEditorTarget(null)} onKeyDown={(e) => { if (e.key === 'Escape') setEditorTarget(null) }} tabIndex={-1}>
          <div role="document" className="w-80 rounded-xl border border-quant-border bg-quant-card p-5 space-y-4" onClick={e => e.stopPropagation()}>
            <h3 className="text-sm font-bold text-foreground">编辑 {editorTarget.shortName}</h3>
            {template.paramSchema.map(field => (
              <div key={field.key}>
                <label className="text-[11px] text-muted-foreground mb-1 block">{field.label}</label>
                <input
                  type="number"
                  value={editorForm[field.key] ?? field.min}
                  onChange={e => setEditorForm(f => ({ ...f, [field.key]: Number(e.target.value) }))}
                  min={field.min}
                  max={field.max}
                  step={field.step || 1}
                  className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-1.5 text-xs text-foreground outline-none focus:border-quant-gold"
                />
              </div>
            ))}
            <div className="flex gap-2 pt-2">
              <button onClick={() => setEditorTarget(null)} className="flex-1 rounded-md border border-quant-border px-3 py-1.5 text-xs text-muted-foreground hover:text-foreground">
                取消
              </button>
              <button onClick={applyEditor} className="flex-1 rounded-md bg-quant-gold px-3 py-1.5 text-xs font-medium text-black hover:opacity-90">
                应用
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
})
