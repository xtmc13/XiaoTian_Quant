/**
 * Enhanced KLineChart — wraps klinecharts with drawing tools, built-in indicators,
 * signal overlays, and theme support.  Ported from QuantDinger KlineChart.vue.
 */
import { useEffect, useRef, useState, useCallback, useMemo } from 'react'
import { init, dispose, registerOverlay } from 'klinecharts'
import type { Chart } from 'klinecharts'
import { cn } from '@/lib/utils'
import type { KLineBar } from '@/lib/technicalIndicators'
import {
  PenLine, Minus, Columns2, Columns3, ArrowRight, GripHorizontal,
  DollarSign, Frame, TrendingUp, Eraser, Settings, Eye, EyeOff, X,
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

const INDICATOR_COLORS_DARK = ['#13c2c2', '#e040fb', '#ffeb3b', '#00e676', '#ff6d00', '#9c27b0']
const INDICATOR_COLORS_LIGHT = ['#13c2c2', '#9c27b0', '#f57c00', '#1976d2', '#c2185b', '#7b1fa2']

/* ── Register signal overlay ───────────────────────────────────── */

function ensureSignalOverlay() {
  try {
    registerOverlay({
      name: 'signalTag',
      totalStep: 1,
      lock: true,
      needDefaultPointFigure: false,
      needDefaultXAxisFigure: false,
      needDefaultYAxisFigure: false,
      createPointFigures: ({ coordinates, overlay }: any) => {
        if (!coordinates[0]) return []
        const x = coordinates[0].x
        const signalY = coordinates[0].y
        const color = overlay.extendData?.color || '#555'
        const text = String(overlay.extendData?.text || '')
        const isBuy = overlay.extendData?.side === 'buy'
        const fontSize = 11
        const boxPaddingX = 6
        const boxPaddingY = 3
        const textWidth = text.length * 7
        const boxWidth = Math.max(textWidth + boxPaddingX * 2, 20)
        const boxHeight = fontSize + boxPaddingY * 2
        const boxY = isBuy ? signalY : signalY - boxHeight

        return [
          {
            type: 'circle',
            attrs: { x, y: signalY, r: 3.5 },
            styles: { style: 'fill', color },
            ignoreEvent: true,
          },
          {
            type: 'rect',
            attrs: { x: x - boxWidth / 2, y: boxY, width: boxWidth, height: boxHeight, r: 3 },
            styles: { style: 'fill', color, borderSize: 0 },
            ignoreEvent: true,
          },
          {
            type: 'text',
            attrs: { x, y: boxY + boxHeight / 2, text, align: 'center', baseline: 'middle' },
            styles: { color: '#ffffff', size: fontSize, weight: 'bold' },
            ignoreEvent: true,
          },
        ]
      },
    })
  } catch (_) {}
}

/* ═════════════════════════════════════════════════════════════════ */
/*  Main Component                                                   */
/* ═════════════════════════════════════════════════════════════════ */

export function KlineChart({
  data,
  signals,
  loading,
  error,
  theme = 'dark',
  height = 500,
  symbol,
  onRetry,
  drawingBarVisible = true,
  activeIndicators = [],
  onActiveIndicatorsChange,
}: KlineChartEnhancedProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<Chart | null>(null)
  const [activeDrawingTool, setActiveDrawingTool] = useState<string | null>(null)
  const [editorTarget, setEditorTarget] = useState<IndicatorConfig | null>(null)
  const [editorForm, setEditorForm] = useState<Record<string, number>>({})
  const signalIdsRef = useRef<string[]>([])
  const drawingIdsRef = useRef<string[]>([])

  const isDark = theme === 'dark'
  const colors = isDark ? INDICATOR_COLORS_DARK : INDICATOR_COLORS_LIGHT

  // Ensure overlays and indicators are registered
  useEffect(() => { ensureSignalOverlay() }, [])

  /* ─── Chart Init ─── */
  useEffect(() => {
    const el = containerRef.current
    if (!el || el.clientWidth === 0 || el.clientHeight === 0) return

    try {
      const chart = init(el, {
        styles: {
          candle: {
            bar: {
              upColor: '#03A66D', downColor: '#CF304A',
              upBorderColor: '#03A66D', downBorderColor: '#CF304A',
              upWickColor: '#03A66D', downWickColor: '#CF304A',
            },
          },
          grid: {
            horizontal: { color: 'rgba(43,49,57,0.3)', size: 1 },
            vertical: { color: 'rgba(43,49,57,0.3)', size: 1 },
          },
        },
      })

      if (!chart) return
      chartRef.current = chart

      const ro = new ResizeObserver(() => { try { chart.resize() } catch (_) {} })
      ro.observe(el)

      return () => {
        ro.disconnect()
        try { dispose(chart) } catch (_) {}
        chartRef.current = null
      }
    } catch (e) {
      console.error('[KlineChart] init error:', e)
    }
  }, [])

  /* ─── Apply data ─── */
  useEffect(() => {
    if (!chartRef.current || !data?.length) return
    chartRef.current.applyNewData(
      data.map(d => ({
        timestamp: (d.timestamp || d.time || 0) * (d.timestamp && d.timestamp > 1e10 ? 1 : 1000),
        open: d.open, high: d.high, low: d.low, close: d.close,
        volume: d.volume || 0,
      }))
    )
  }, [data])

  /* ─── Apply signals ─── */
  useEffect(() => {
    if (!chartRef.current || !signals?.length) return
    // Clear previous
    signalIdsRef.current.forEach(id => {
      try { chartRef.current?.removeOverlay?.(id) } catch (_) {}
    })
    signalIdsRef.current = []

    signals.forEach(s => {
      try {
        const overlayId = (chartRef.current as any)?.createOverlay?.({
          name: 'signalTag',
          points: [{ timestamp: s.timestamp, value: s.price, dataIndex: 0 }],
          extendData: {
            text: s.text || (s.side === 'buy' ? '买' : '卖'),
            color: s.color || (s.side === 'buy' ? '#03A66D' : '#CF304A'),
            side: s.side,
            markerStyle: s.markerStyle || 'solid',
            source: s.source || '',
          },
        })
        if (overlayId) signalIdsRef.current.push(String(overlayId))
      } catch (_) {}
    })
  }, [signals])

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
      const id = (chartRef.current as any).createOverlay({ name: tool.overlay, lock: false })
      if (id) drawingIdsRef.current.push(String(id))
    } catch (_) { setActiveDrawingTool(null) }
  }, [activeDrawingTool])

  const clearDrawings = useCallback(() => {
    if (!chartRef.current) return
    drawingIdsRef.current.forEach(id => {
      try { chartRef.current?.removeOverlay?.(id) } catch (_) {}
    })
    drawingIdsRef.current = []
    setActiveDrawingTool(null)
  }, [])

  /* ─── Indicator management ─── */
  const toggleIndicator = useCallback((template: IndicatorTemplate) => {
    if (!onActiveIndicatorsChange) return
    const existing = activeIndicators.find(i => i.id === template.id)
    if (existing) {
      onActiveIndicatorsChange(activeIndicators.filter(i => i.id !== template.id))
    } else {
      const color = colors[activeIndicators.length % colors.length]
      onActiveIndicatorsChange([
        ...activeIndicators,
        {
          id: template.id,
          name: template.name,
          shortName: template.shortName,
          type: template.type,
          params: { ...template.defaultParams },
          style: { color, lineWidth: 2 },
          visible: true,
          instanceId: `${template.id}_${Date.now()}`,
        },
      ])
    }
  }, [activeIndicators, onActiveIndicatorsChange, colors])

  const openEditor = useCallback((indicator: IndicatorConfig) => {
    setEditorTarget(indicator)
    setEditorForm({ ...indicator.params })
  }, [])

  const applyEditor = useCallback(() => {
    if (!editorTarget || !onActiveIndicatorsChange) return
    const updated = activeIndicators.map(i =>
      (i.instanceId || i.id) === (editorTarget.instanceId || editorTarget.id)
        ? { ...i, params: { ...editorForm } }
        : i
    )
    onActiveIndicatorsChange(updated)
    setEditorTarget(null)
  }, [editorTarget, editorForm, activeIndicators, onActiveIndicatorsChange])

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
    <div className="flex rounded-lg overflow-hidden border border-quant-border" style={{ height }}>
      {/* Drawing toolbar */}
      {drawingBarVisible && (
        <div className="w-10 shrink-0 bg-quant-bg-secondary border-r border-quant-border flex flex-col items-center py-2 gap-1 overflow-y-auto">
          {DRAWING_TOOLS.map(tool => (
            <button
              key={tool.name}
              onClick={() => selectDrawingTool(tool.name)}
              className={cn(
                'w-8 h-8 flex items-center justify-center rounded transition-colors',
                activeDrawingTool === tool.name
                  ? 'bg-quant-gold/20 text-quant-gold'
                  : 'text-muted-foreground hover:text-foreground hover:bg-white/5',
              )}
              title={tool.title}
            >
              <tool.icon className="w-4 h-4" />
            </button>
          ))}
          <div className="w-5 h-px bg-quant-border my-1" />
          <button onClick={clearDrawings} className="w-8 h-8 flex items-center justify-center rounded text-muted-foreground hover:text-quant-red hover:bg-quant-red/10 transition-colors" title="清除所有画线">
            <Eraser className="w-4 h-4" />
          </button>
        </div>
      )}

      {/* Chart content area */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Indicator toolbar */}
        <div className="flex items-center gap-1 px-2 py-1.5 border-b border-quant-border bg-quant-bg-secondary overflow-x-auto shrink-0">
          {INDICATOR_TEMPLATES.map(tpl => {
            const active = activeIndicators.some(i => i.id === tpl.id)
            return (
              <button
                key={tpl.id}
                onClick={() => toggleIndicator(tpl)}
                className={cn(
                  'px-2 py-0.5 rounded text-[10px] font-medium whitespace-nowrap transition-colors',
                  active
                    ? 'bg-quant-gold/20 text-quant-gold'
                    : 'bg-quant-bg-tertiary text-muted-foreground hover:text-foreground',
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
                <button onClick={() => onActiveIndicatorsChange?.(activeIndicators.filter(i => (i.instanceId || i.id) !== (ind.instanceId || ind.id)))} className="text-muted-foreground hover:text-quant-red">
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
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setEditorTarget(null)}>
          <div className="w-80 rounded-xl border border-quant-border bg-quant-card p-5 space-y-4" onClick={e => e.stopPropagation()}>
            <h3 className="text-sm font-bold text-white">编辑 {editorTarget.shortName}</h3>
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
                  className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-1.5 text-xs text-white outline-none focus:border-quant-gold"
                />
              </div>
            ))}
            <div className="flex gap-2 pt-2">
              <button onClick={() => setEditorTarget(null)} className="flex-1 rounded-md border border-quant-border px-3 py-1.5 text-xs text-muted-foreground hover:text-white">
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
}
