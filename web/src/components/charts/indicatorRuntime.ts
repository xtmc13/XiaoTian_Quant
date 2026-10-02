/**
 * indicatorRuntime.ts
 *
 * 量化丁格（QuantDinger）指标渲染运行时的移植实现。
 * 参考：QuantDinger 前端 KLineChart chunk 的 output 契约处理管线
 * （normalize → plots→indicator / signals→signalTag overlay / layers→overlay）。
 *
 * 与原实现的关键差异（为何重写成这样）：
 *  - plots 支持 line/bar/circle 三种图形 + 逐点动态颜色/尺寸（Pine plotstyle 语义）
 *  - signals 按"稀疏事件"语义处理：支持 renderMode（events/points/edge）、
 *    价格/布尔/对象三种点位格式、去重、上限、边沿化，标记画在 K 线高/低点外侧
 *  - layers 支持 zone（区域）/ line（线段）/ label（标签）三种视觉图层
 *  - 指标按内容签名注册（同代码同 plots 复用），重跑先清旧实例再建，杜绝残留
 *  - calc 按时间戳对齐（而不是裸索引），数据错位也不串
 */

import { registerIndicator, registerOverlay } from 'klinecharts'

/* ── 输入类型 ─────────────────────────────────────────────── */

export interface KLineLike {
  timestamp?: number
  time?: number
  open?: number
  high?: number
  low?: number
  close?: number
  volume?: number
}

export interface PlotPointObject {
  value?: number | null
  y?: number | null
  data?: number | null
  color?: string
  fillColor?: string
  backgroundColor?: string
  size?: number
  radius?: number
  r?: number
}

export type PlotPoint = number | string | boolean | null | undefined | PlotPointObject

export interface IndicatorPlotSpec {
  name?: string
  title?: string | false
  data?: PlotPoint[]
  color?: string
  overlay?: boolean
  type?: string
  size?: number
  radius?: number
  lineWidth?: number
  lineStyle?: string
  style?: string
  opacity?: number
  borderColor?: string
  borderSize?: number
  baseValue?: number
  laneLabel?: string
  rowLabel?: string
  label?: string
  group?: string
  laneName?: string
}

export interface IndicatorSignalSpec {
  type?: string
  action?: string
  side?: string
  text?: string
  color?: string
  data?: unknown[]
  textData?: unknown[]
  renderMode?: string
  mode?: string
}

export interface IndicatorLayerSpec {
  type?: string
  name?: string
  text?: string
  startIndex?: number
  endIndex?: number
  fromIndex?: number
  toIndex?: number
  index?: number
  start?: number | string
  end?: number | string
  from?: number | string
  to?: number | string
  x1?: number | string
  x2?: number | string
  top?: number
  high?: number
  y1?: number
  price1?: number
  bottom?: number
  low?: number
  y2?: number
  price2?: number
  price?: number
  value?: number
  level?: number
  y?: number
  timestamp?: number
  time?: number
  color?: string
  fillColor?: string
  borderColor?: string
  borderSize?: number
  opacity?: number
  dashed?: boolean
  lineWidth?: number
  fontSize?: number
  textColor?: string
  side?: string
  startTime?: number | string
  endTime?: number | string
}

export interface IndicatorOutput {
  name?: string
  description?: string
  plots?: IndicatorPlotSpec[]
  signals?: IndicatorSignalSpec[]
  layers?: IndicatorLayerSpec[]
  calculatedVars?: Record<string, unknown>
}

export interface SignalPoint {
  timestamp: number
  price: number
  anchorPrice: number
  side: 'buy' | 'sell'
  action: string
  color: string
  text: string
  rawPrice: number | null
}

export interface NormalizedIndicatorOutput {
  name: string
  description: string
  plots: IndicatorPlotSpec[]
  layers: IndicatorLayerSpec[]
  signalPoints: SignalPoint[]
}

/* ── 模块级主题/精度（图表组件注入） ────────────────────────── */

let runtimeTheme: 'dark' | 'light' = 'dark'
let runtimePrecision = 2

export function setIndicatorRuntimeTheme(theme: 'dark' | 'light'): void {
  runtimeTheme = theme
}

export function setIndicatorRuntimePrecision(precision: number): void {
  if (Number.isFinite(precision)) runtimePrecision = Math.max(0, Math.min(8, Math.floor(precision)))
}

export function detectPrecision(klines: KLineLike[]): number {
  let precision = 2
  for (const k of klines) {
    const close = Number(k?.close)
    if (!Number.isFinite(close)) continue
    const decimals = String(close).split('.')[1]?.length ?? 0
    if (decimals > precision) precision = Math.min(decimals, 8)
    if (precision >= 8) break
  }
  return precision
}

/* ── 基础工具 ─────────────────────────────────────────────── */

function numOrNull(value: unknown): number | null {
  const n = Number(value)
  return Number.isFinite(n) ? n : null
}

export function tsOf(k: KLineLike | undefined | null): number | null {
  const raw = Number(k?.timestamp ?? k?.time)
  if (!Number.isFinite(raw)) return null
  return raw < 1e10 ? raw * 1000 : raw
}

/** #RRGGBB + alpha → rgba()，非 hex 原样返回 */
function hexToRgba(color: string, alpha: number): string {
  const hex = String(color || '').replace('#', '')
  if (!/^[0-9a-fA-F]{6}$/.test(hex)) return color
  const r = parseInt(hex.slice(0, 2), 16)
  const g = parseInt(hex.slice(2, 4), 16)
  const b = parseInt(hex.slice(4, 6), 16)
  return `rgba(${r}, ${g}, ${b}, ${alpha})`
}

function isHexColor(value: unknown): boolean {
  return /^#?[0-9a-fA-F]{6}$/.test(String(value ?? '').trim())
}

/** 优先用 fillColor/borderColor/opacity 组合出最终颜色（Ze） */
function resolveAlphaColor(fill: unknown, fallback: string, alpha: number): string {
  const candidate = String(fill ?? '').trim() || fallback
  if (isHexColor(candidate)) return hexToRgba(candidate, alpha)
  if (/^rgba?\(/i.test(candidate)) return candidate
  return hexToRgba(fallback, alpha)
}

/** 文本宽度估算（CJK 全宽，拉丁 0.58 宽），带上下限（_n） */
function estimateTextWidth(text: string, fontSize = 10, min = 40, max = 140): number {
  const width = String(text || '')
    .split('')
    .reduce((sum, ch) => sum + (ch.charCodeAt(0) > 255 ? fontSize : fontSize * 0.58), 0)
  return Math.max(min, Math.min(max, width + 18))
}

/** 回测标记的短文本（Fa） */
function shortSignalText(text: unknown, side: string): string {
  const raw = String(text ?? '').trim()
  const lower = raw.toLowerCase()
  if (lower.includes('liquid')) return 'LQ'
  if (lower.includes('trailing')) return 'TR'
  if (lower.includes('profit') || lower.includes('tp')) return 'TP'
  if (lower.includes('stop') || lower.includes('sl')) return 'SL'
  if (lower.includes('open long')) return 'L'
  if (lower.includes('open short')) return 'S'
  if (lower.includes('close long')) return 'XL'
  if (lower.includes('close short')) return 'XS'
  if (lower.includes('signal')) return side === 'buy' ? 'L?' : 'S?'
  if (/^[A-Za-z0-9?+]{1,4}$/.test(raw)) return raw
  return side === 'buy' ? 'L' : 'S'
}

const INACTIVE_STRINGS = new Set(['', '0', 'false', 'none', 'null', 'nan', 'na', 'n/a'])
const MAX_SIGNAL_POINTS = 320

/* ── signals 归一化 ──────────────────────────────────────── */

function priceRangeOf(klines: KLineLike[]): { priceRange: number } {
  let min = Infinity
  let max = -Infinity
  for (const k of klines) {
    const high = numOrNull(k?.high)
    const low = numOrNull(k?.low)
    if (high != null) max = Math.max(max, high)
    if (low != null) min = Math.min(min, low)
  }
  const range = max - min
  const scale = Math.max(Math.abs(max || 0), Math.abs(min || 0), 1)
  return { priceRange: Number.isFinite(range) && range > 0 ? range : scale * 0.01 }
}

/**
 * 标记落点价格（Ba）：
 * 无价格时按 K 线高/低点外扩（买在下、卖在上）；有价格时夹到 K 线外侧附近，
 * 避免标记压线。
 */
function markerPrice(
  rawPrice: number | null,
  kline: KLineLike | undefined,
  side: string,
  settings: { priceRange: number },
): number | null {
  const high = numOrNull(kline?.high)
  const low = numOrNull(kline?.low)
  if (high == null || low == null) return numOrNull(rawPrice)
  const range = Math.max(settings.priceRange || 0, Math.abs(high || low || 1) * 0.002, 1e-6)
  const span = Math.max(high - low, 0)
  const gap = Math.min(range * 0.035, Math.max(range * 0.012, span * 0.18))
  const clampRange = Math.max(gap, Math.min(range * 0.055, Math.max(range * 0.035, span * 0.65)))
  const price = numOrNull(rawPrice)
  const isBuy = side === 'buy'
  const anchor = isBuy ? low : high
  const fallback = isBuy ? anchor - gap : anchor + gap
  if (price == null) return fallback
  if (isBuy) return price >= low ? fallback : Math.min(anchor - gap, Math.max(anchor - clampRange, price))
  return price <= high ? fallback : Math.max(anchor + gap, Math.min(anchor + clampRange, price))
}

function sideOfSignal(signal: IndicatorSignalSpec): 'buy' | 'sell' {
  const side = String(signal?.side || '').toLowerCase()
  const type = String(signal?.type || signal?.action || 'buy').toLowerCase()
  const raw = side || type
  if (['sell', 'short', 'exit', 'close', 'close_long', 'reduce'].includes(raw)) return 'sell'
  if (['buy', 'long', 'entry', 'open', 'open_long', 'add'].includes(raw)) return 'buy'
  return type.includes('sell') || type.includes('short') || type.includes('exit') ? 'sell' : 'buy'
}

function textOfSignal(signal: IndicatorSignalSpec, side: string): string {
  return signal?.text != null && String(signal.text).trim()
    ? String(signal.text)
    : side === 'buy' ? 'B' : 'S'
}

function colorOfSignal(signal: IndicatorSignalSpec, side: string): string {
  return signal?.color ? signal.color : side === 'buy' ? '#22C55E' : '#EF4444'
}

interface ParsedSignalPoint {
  active: boolean
  rawPrice: number | null
  text?: unknown
  color?: string
}

/** 信号点位解析（Ha）：数字非零激活、布尔、字符串、对象都支持 */
function parseSignalPoint(point: unknown): ParsedSignalPoint {
  if (point == null || point === false) return { active: false, rawPrice: null }
  if (typeof point === 'string') {
    const trimmed = point.trim().toLowerCase()
    return INACTIVE_STRINGS.has(trimmed)
      ? { active: false, rawPrice: null }
      : { active: true, rawPrice: numOrNull(point) }
  }
  if (typeof point === 'number') {
    const price = numOrNull(point)
    return { active: price != null && price !== 0, rawPrice: price }
  }
  if (typeof point === 'boolean') return { active: point, rawPrice: null }
  if (typeof point === 'object') {
    const obj = point as Record<string, unknown>
    const active = obj.active ?? obj.signal ?? obj.triggered ?? obj.hit ?? obj.visible
    const price = numOrNull(obj.price ?? obj.value ?? obj.y ?? obj.data)
    return {
      active: active != null ? !!active : price != null && price !== 0,
      rawPrice: price,
      text: obj.text ?? obj.label ?? obj.name,
      color: obj.color as string | undefined,
    }
  }
  return { active: false, rawPrice: null }
}

/** renderMode 判定（Ka）：显式声明优先；密度超 18% 自动边沿化 */
function renderModeOf(signal: IndicatorSignalSpec, activeCount: number, total: number): 'events' | 'points' | 'edge' {
  const mode = String(signal?.renderMode || signal?.mode || '').toLowerCase()
  if (['event', 'events'].includes(mode)) return 'events'
  if (['point', 'points', 'marker', 'markers', 'raw'].includes(mode)) return 'points'
  if (['state', 'continuous', 'condition'].includes(mode)) return 'edge'
  return (total > 0 ? activeCount / total : 0) > 0.18 ? 'edge' : 'events'
}

function pointActiveAt(points: ParsedSignalPoint[], index: number, mode: 'events' | 'points' | 'edge'): boolean {
  if (!points[index]?.active) return false
  if (mode === 'points') return true
  if (mode === 'edge') return !points[index - 1]?.active
  return true
}

/**
 * output 归一化（Cn）：
 * 把 plots/signals/layers 原始结构转成渲染可用的 signalPoints，
 * 并做去重、上限截断。
 */
export function normalizeIndicatorOutput(output: unknown, klines: KLineLike[]): NormalizedIndicatorOutput {
  const raw = output && typeof output === 'object' ? (output as IndicatorOutput) : {}
  const result: NormalizedIndicatorOutput = {
    name: raw.name ? String(raw.name) : '',
    description: raw.description ? String(raw.description) : '',
    plots: Array.isArray(raw.plots) ? raw.plots : [],
    layers: Array.isArray(raw.layers) ? raw.layers : [],
    signalPoints: [],
  }
  if (!Array.isArray(raw.signals) || !klines.length) return result
  const settings = priceRangeOf(klines)
  const seen = new Set<string>()
  for (const signal of raw.signals) {
    if (!signal || !Array.isArray(signal.data)) continue
    const action = String(signal.type || signal.action || 'buy').toLowerCase()
    const side = sideOfSignal(signal)
    const isBuy = side === 'buy'
    const defaultText = textOfSignal(signal, side)
    const signalData = signal.data ?? []
    const limit = Math.min(signalData.length, klines.length)
    const points: ParsedSignalPoint[] = Array.from({ length: limit }, (_, i) => parseSignalPoint(signalData[i]))
    const activeCount = points.reduce((sum, p) => sum + (p.active ? 1 : 0), 0)
    const mode = renderModeOf(signal, activeCount, limit)
    for (let i = 0; i < limit; i++) {
      if (!pointActiveAt(points, i, mode)) continue
      const point = points[i]
      const kline = klines[i]
      const timestamp = tsOf(kline)
      if (timestamp == null) continue
      const anchorPrice = numOrNull(isBuy ? kline?.low : kline?.high)
      const price = markerPrice(point.rawPrice, kline, side, settings)
      if (anchorPrice == null || price == null) continue
      let text = defaultText
      if (Array.isArray(signal.textData) && signal.textData[i] != null) {
        text = String(signal.textData[i])
      } else if (point.text != null && String(point.text).trim()) {
        text = String(point.text)
      }
      const color = point.color || colorOfSignal(signal, side)
      const dedupKey = `${timestamp}:${side}:${String(text)}`
      if (seen.has(dedupKey)) continue
      seen.add(dedupKey)
      result.signalPoints.push({
        timestamp,
        price,
        anchorPrice,
        side,
        action,
        color,
        text,
        rawPrice: point.rawPrice,
      })
    }
  }
  if (result.signalPoints.length > MAX_SIGNAL_POINTS) {
    result.signalPoints = result.signalPoints.slice(-MAX_SIGNAL_POINTS)
  }
  return result
}

/* ── plots → figures ─────────────────────────────────────── */

const CIRCLE_TYPE_ALIASES = ['circle', 'dot', 'point', 'scatter']

function normalizeFigureType(type: unknown): 'circle' | 'bar' | 'line' {
  const raw = String(type || 'line').toLowerCase()
  if (CIRCLE_TYPE_ALIASES.includes(raw)) return 'circle'
  if (['histogram', 'column'].includes(raw)) return 'bar'
  if (['circle', 'bar', 'line'].includes(raw)) return raw as 'circle' | 'bar' | 'line'
  return 'line'
}

function positiveNumber(value: unknown, fallback: number): number {
  const n = Number(value)
  return Number.isFinite(n) && n > 0 ? n : fallback
}

function defaultPalette(index: number): string {
  const dark = ['#2962ff', '#ff9800', '#ab47bc', '#26a69a', '#ef5350', '#78909c']
  const light = ['#2962ff', '#f57c00', '#7b1fa2', '#00897b', '#d32f2f', '#546e7a']
  const palette = runtimeTheme === 'dark' ? dark : light
  return palette[index % palette.length]
}

/** 点位对象取值（Wt） */
function plotPointValue(point: unknown): number | null | undefined {
  if (point && typeof point === 'object') {
    const obj = point as PlotPointObject
    const value = obj.value ?? obj.y ?? obj.data
    return value ?? null
  }
  return point as number | null | undefined
}

/** 点位对象颜色（ir） */
function plotPointColor(point: unknown): string | null {
  if (point && typeof point === 'object') {
    const obj = point as PlotPointObject
    return obj.color || obj.fillColor || obj.backgroundColor || null
  }
  return null
}

/** 点位对象尺寸（or） */
function plotPointSize(point: unknown): number | null {
  if (point && typeof point === 'object') {
    const obj = point as PlotPointObject
    const size = Number(obj.size ?? obj.radius ?? obj.r)
    return Number.isFinite(size) && size > 0 ? size : null
  }
  return null
}

/** 真值判定（sr）：布尔/字符串/数字/object.active 都认 */
function isTruthyPoint(value: unknown): boolean {
  if (value == null) return false
  if (typeof value === 'boolean') return value
  if (typeof value === 'string') {
    const normalized = value.trim().toLowerCase()
    return !!normalized && !INACTIVE_STRINGS.has(normalized)
  }
  const n = Number(value)
  return Number.isFinite(n) && n !== 0
}

/** plot 序列数值化（lr）：灯带序列把真值映射成车道号，否则取数值/null */
function normalizePlotValues(data: unknown[] | undefined, lamp?: { force: boolean; lane: number }): Array<number | null> {
  if (!Array.isArray(data)) return []
  if (lamp?.force) {
    const lane = Number.isFinite(lamp.lane) && lamp.lane > 0 ? lamp.lane : 1
    return data.map((point) => (isTruthyPoint(plotPointValue(point)) ? lane : null))
  }
  return data.map((point) => {
    const value = plotPointValue(point)
    const n = numOrNull(value)
    return n
  })
}

function normalizePlotColors(data: unknown[] | undefined): Array<string | null> {
  return Array.isArray(data) ? data.map(plotPointColor) : []
}

function normalizePlotSizes(data: unknown[] | undefined): Array<number | null> {
  return Array.isArray(data) ? data.map(plotPointSize) : []
}

/** 灯带命名（Bn）：去掉 bull/bear/on/off 等状态词，留核心语义 */
function laneLabelOf(plot: IndicatorPlotSpec, index: number): string {
  const explicit = plot?.laneLabel ?? plot?.rowLabel ?? plot?.label ?? plot?.group ?? plot?.laneName
  const cleaned = String(explicit || plot?.name || plot?.title || `lamp_${index}`)
    .replace(/\b(red|green|on|off|bullish|bearish|bull|bear|long|short|up|down|light|lamp|buy|sell|entry|exit|signal|signals)\b/gi, '')
    .replace(/[_-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
  return cleaned || `L${index + 1}`
}

/** 灯带点颜色归一（mr） */
function lampDotColor(color: unknown, fallback = '#22c55e'): string {
  const raw = String(color || '').toLowerCase()
  if (raw.includes('ff3b30') || raw.includes('ef4444') || raw.includes('red')) return '#ff4d4f'
  if (raw.includes('35c759') || raw.includes('22c55e') || raw.includes('green')) return '#22c55e'
  return (color as string) || fallback
}

function isCounterPlot(plot: IndicatorPlotSpec): boolean {
  const name = String(plot?.name || plot?.title || '').toLowerCase()
  return /(count|score|total|sum|summary)/.test(name)
}

/** 灯带自动识别（dr）：类型命中，或名字像状态且取值是少量整数 */
function looksLikeLampPlot(plot: IndicatorPlotSpec): boolean {
  if (!plot) return false
  if (CIRCLE_TYPE_ALIASES.includes(String(plot.type || '').toLowerCase())) return true
  const name = String(plot.name || plot.title || '').toLowerCase()
  if (!/(lamp|light|red|green|on|off|bull|bear|signal|state|trend)/.test(name)) return false
  const values = (Array.isArray(plot.data) ? plot.data : [])
    .map(plotPointValue)
    .filter((v) => v != null)
    .slice(0, 80)
  if (!values.length) return true
  const numeric = values.map(Number).filter(Number.isFinite)
  if (!numeric.length) return true
  const distinct = new Set(numeric.map((v) => Number(v.toFixed(6))))
  return numeric.every((v) => Math.abs(v - Math.round(v)) < 1e-6) && distinct.size <= 8
}

interface LampBeltMeta {
  enabled: boolean
  laneByIndex: Map<number, number>
  hiddenIndexes: Set<number>
  laneCount: number
  labelByLane?: Map<number, string>
  laneKeys?: string[]
}

/** 灯带分析（hr）：≥3 个状态序列才组成灯带面板 */
function analyzeLampBelt(plots: IndicatorPlotSpec[]): LampBeltMeta {
  const labels: string[] = []
  const labelByIndex = new Map<number, string>()
  plots.forEach((plot, index) => {
    if (!looksLikeLampPlot(plot) || isCounterPlot(plot)) return
    const label = laneLabelOf(plot, index)
    labelByIndex.set(index, label)
    if (!labels.includes(label)) labels.push(label)
  })
  if (labels.length < 3) {
    return { enabled: false, laneByIndex: new Map(), hiddenIndexes: new Set(), laneCount: 0 }
  }
  const laneByIndex = new Map<number, number>()
  const labelByLane = new Map<number, string>()
  labelByIndex.forEach((label, index) => {
    const lane = labels.length - labels.indexOf(label)
    laneByIndex.set(index, lane)
    labelByLane.set(lane, label)
  })
  const hiddenIndexes = new Set<number>()
  plots.forEach((plot, index) => {
    if (isCounterPlot(plot)) hiddenIndexes.add(index)
  })
  return { enabled: true, laneByIndex, hiddenIndexes, laneCount: labels.length, labelByLane, laneKeys: labels }
}

interface BuiltFigure {
  figureKey: string
  figure: Record<string, unknown>
  colorKey: string
  sizeKey: string
  lamp: { lane: number; label: string; color: string; size: number } | null
}

/**
 * 单 plot → figure（fr）：
 * 三种图形类型 + 逐点动态颜色/尺寸（通过额外的数据列 `${key}__color` / `${key}__size`），
 * circle 支持动态半径，bar 支持灯带车道布局。
 */
function buildFigure(
  plot: IndicatorPlotSpec,
  plotIndex: number,
  fallbackKey: string,
  lamp?: { force: boolean; lane: number },
): BuiltFigure {
  const name = plot.name || fallbackKey || `PLOT_${plotIndex}`
  const key = String(name)
    .toLowerCase()
    .replace(/\s+/g, '_')
    .replace(/[^a-z0-9_]/g, '_')
    .replace(/^_+|_+$/g, '') || `plot_${plotIndex}`
  const figureType = lamp?.force ? 'bar' : normalizeFigureType(plot.type)
  const baseColor = plot.color || defaultPalette(plotIndex)
  const defaultSize = positiveNumber(plot.size ?? plot.radius, figureType === 'circle' ? 5 : 1.5)
  const lineWidth = positiveNumber(plot.lineWidth ?? plot.size, 1.5)
  const borderSize = positiveNumber(plot.borderSize, 1)
  const colorKey = `${key}__color`
  const sizeKey = `${key}__size`

  const resolveStyle = (row: Record<string, unknown> = {}) => {
    const runtimeColor = (row[colorKey] as string) || baseColor
    const runtimeSize = positiveNumber(row[sizeKey], defaultSize)
    const style: Record<string, unknown> = { color: runtimeColor }
    const opacity = Number(plot.opacity)
    if (Number.isFinite(opacity)) style.opacity = opacity
    return { style, runtimeColor, runtimeSize }
  }

  const figure: Record<string, unknown> = {
    key,
    title: lamp?.force || plot.title === false ? '' : plot.title || plot.name || name,
    type: figureType,
    lamp: lamp?.force
      ? { lane: lamp.lane, label: laneLabelOf(plot, plotIndex), color: lampDotColor(baseColor), size: defaultSize }
      : null,
    styles: (data: { current?: { indicatorData?: Record<string, unknown> } } | undefined) => {
      const row = data?.current?.indicatorData || {}
      const { style, runtimeColor, runtimeSize } = resolveStyle(row)
      if (figureType === 'circle') {
        return {
          ...style,
          borderColor: plot.borderColor || runtimeColor,
          borderSize,
          r: runtimeSize,
          radius: runtimeSize,
        }
      }
      if (figureType === 'bar') {
        return {
          ...style,
          style: lamp?.force ? 'fill' : style.style,
          borderColor: plot.borderColor || runtimeColor,
          borderSize: lamp?.force ? 0 : borderSize,
        }
      }
      return {
        ...style,
        size: lineWidth,
        style: plot.lineStyle || plot.style || 'solid',
      }
    },
  }

  if (figureType === 'circle') {
    figure.attrs = (params: { coordinate?: { current?: Record<string, number> }; data?: { current?: Record<string, unknown> } }) => {
      const row = params?.data?.current || {}
      const coordinate = params?.coordinate?.current || {}
      const { runtimeSize } = resolveStyle(row)
      return { x: coordinate.x, y: coordinate[key], r: runtimeSize }
    }
  }

  if (lamp?.force && figureType === 'bar') {
    figure.attrs = (params: {
      coordinate?: { current?: Record<string, number> }
      data?: { current?: Record<string, unknown> }
      barSpace?: { bar?: number; gapBar?: number }
      bounding?: { left?: number }
    }) => {
      const row = params?.data?.current || {}
      const coordinate = params?.coordinate?.current || {}
      const runtimeSize = positiveNumber(row[sizeKey], defaultSize)
      const barSpace = Math.max(3, Math.min(5, (Number(params?.barSpace?.bar ?? params?.barSpace?.gapBar ?? 8) || 8) * 0.42, runtimeSize * 0.82))
      const dotHeight = Math.max(8, Math.min(12, barSpace * 2.15))
      const leftBound = Number(params?.bounding?.left || 0) + 82
      const x = Number(coordinate.x)
      if (!Number.isFinite(x) || x < leftBound) {
        return { x: -9999, y: -9999, width: 0, height: 0 }
      }
      return {
        x: x - barSpace / 2,
        y: coordinate[key] - dotHeight / 2,
        width: barSpace,
        height: dotHeight,
        r: dotHeight / 2,
      }
    }
  }

  if (figureType === 'bar' && plot.baseValue != null) {
    figure.baseValue = Number(plot.baseValue)
  }

  return { figureKey: key, figure, colorKey, sizeKey, lamp: (figure.lamp as BuiltFigure['lamp']) ?? null }
}

export interface PlotGroupBuild {
  figures: Array<Record<string, unknown>>
  plotDataMap: Record<string, Array<number | null> | Array<string | null>>
  lampBeltMeta: LampBeltMeta
  lampBeltExtendData: { enabled: boolean; laneCount: number; lanes: Array<{ lane: number; label: string }>; figures: Array<{ key: string; lane: number; label: string; color: string; size: number }> } | null
}

function laneLabelShort(label: unknown, lane: number): string {
  return String(label || `L${lane}`).replace(/[_-]+/g, ' ').replace(/\s+/g, ' ').trim().toUpperCase().slice(0, 8)
}

/**
 * plots 组 → figures + 数据列（h）：
 * 数值列 + 逐点颜色列 + 逐点尺寸列；灯带启用时附加车道背景/标签列与图形。
 */
export function buildPlotGroup(plots: IndicatorPlotSpec[], dataLength: number): PlotGroupBuild {
  const figures: Array<Record<string, unknown>> = []
  const plotDataMap: PlotGroupBuild['plotDataMap'] = {}
  const lampBelt = analyzeLampBelt(plots)
  const visible = plots
    .map((plot, plotIdx) => ({ plot, plotIdx }))
    .filter(({ plotIdx }) => !lampBelt.hiddenIndexes.has(plotIdx))

  for (const { plot, plotIdx } of visible) {
    const forceLamp = lampBelt.laneByIndex.has(plotIdx)
    const lampLane = lampBelt.laneByIndex.get(plotIdx) ?? 0
    const fallbackKey = `PLOT_${plotIdx}`
    const built = buildFigure(plot, plotIdx, fallbackKey, forceLamp ? { force: true, lane: lampLane } : undefined)
    figures.push(built.figure)
    plotDataMap[built.figureKey] = normalizePlotValues(plot.data, forceLamp ? { force: true, lane: lampLane } : undefined)
    plotDataMap[built.colorKey] = normalizePlotColors(plot.data)
    plotDataMap[built.sizeKey] = normalizePlotSizes(plot.data)
  }

  // 灯带车道背景 + 标签（br）
  if (lampBelt.enabled && lampBelt.labelByLane) {
    lampBelt.labelByLane.forEach((label, lane) => {
      const bgKey = `__lamp_lane_${lane}_bg`
      const labelKey = `__lamp_lane_${lane}_label`
      const short = laneLabelShort(label, lane)
      plotDataMap[bgKey] = new Array(dataLength).fill(lane)
      plotDataMap[labelKey] = new Array(dataLength).fill(lane)
      figures.push({
        key: bgKey,
        title: '',
        type: 'rect',
        attrs: (params: { coordinate?: { current?: Record<string, unknown> }; bounding?: { left?: number } }) => {
          const value = params?.coordinate?.current?.[bgKey]
          const left = Number(params?.bounding?.left || 0)
          return Number.isFinite(value)
            ? { x: left + 8, y: (value as number) - 9, width: 58, height: 18 }
            : { x: -9999, y: -9999, width: 0, height: 0 }
        },
        styles: () => ({
          style: 'stroke_fill',
          color: runtimeTheme === 'dark' ? 'rgba(8,12,18,0.86)' : 'rgba(255,255,255,0.92)',
          borderColor: runtimeTheme === 'dark' ? 'rgba(255,255,255,0.10)' : 'rgba(15,23,42,0.12)',
          borderSize: 1,
        }),
      })
      figures.push({
        key: labelKey,
        title: '',
        type: 'text',
        attrs: (params: { coordinate?: { current?: Record<string, unknown> }; bounding?: { left?: number } }) => {
          const value = params?.coordinate?.current?.[labelKey]
          const left = Number(params?.bounding?.left || 0)
          return Number.isFinite(value)
            ? { x: left + 14, y: value as number, text: short, align: 'left', baseline: 'middle' }
            : { x: -9999, y: -9999, text: '' }
        },
        styles: () => ({
          color: runtimeTheme === 'dark' ? 'rgba(226,232,240,0.94)' : 'rgba(31,41,55,0.92)',
          size: 10,
          weight: '700',
        }),
      })
    })
  }

  // 灯带面板元数据（yr）
  let lampBeltExtendData: PlotGroupBuild['lampBeltExtendData'] = null
  if (lampBelt.enabled) {
    const lanes: Array<{ lane: number; label: string }> = []
    lampBelt.labelByLane?.forEach((label, lane) => lanes.push({ lane, label }))
    lampBeltExtendData = {
      enabled: true,
      laneCount: lampBelt.laneCount,
      lanes,
      figures: figures
        .filter((f) => (f as { lamp?: unknown }).lamp)
        .map((f) => {
          const lamp = (f as { key: string; lamp: { lane: number; label: string; color: string; size: number } }).lamp
          return { key: f.key as string, lane: lamp.lane, label: lamp.label, color: lamp.color, size: lamp.size }
        }),
    }
  }

  return { figures, plotDataMap, lampBeltMeta: lampBelt, lampBeltExtendData }
}

/**
 * 指标 calc（Ht）：按时间戳把图表数据对齐到 plot 数据的索引，
 * 返回逐 bar 的 `{key: value|null}` 行。
 */
export function makePlotCalc(klines: KLineLike[], plotDataMap: Record<string, unknown[]>) {
  return (kLineDataList: Array<Record<string, unknown>>) => {
    const source = Array.isArray(kLineDataList) ? kLineDataList : []
    const indexByTs = new Map<number, number>()
    source.forEach((kline, index) => {
      const ts = tsOf(kline as KLineLike)
      if (ts !== null) indexByTs.set(ts, index)
    })
    return source.map((kline, dataIndex) => {
      const ts = tsOf(kline as KLineLike)
      const alignedIndex = ts !== null && indexByTs.has(ts) ? indexByTs.get(ts)! : dataIndex
      const row: Record<string, unknown> = {}
      for (const key in plotDataMap) {
        const series = plotDataMap[key]
        row[key] = alignedIndex >= 0 && alignedIndex < series.length ? series[alignedIndex] : null
      }
      return row
    })
  }
}

/** plots 分主图/副图组（Jn） */
export function splitOverlayPane(plots: IndicatorPlotSpec[]): { overlayPlots: IndicatorPlotSpec[]; panePlots: IndicatorPlotSpec[] } {
  return {
    overlayPlots: plots.filter((p) => p && p.overlay !== false),
    panePlots: plots.filter((p) => p && p.overlay === false),
  }
}

/** 副图窗格选项（zn）：灯带面板更高 */
export function paneOptionsFor(plots: IndicatorPlotSpec[], lampBeltMeta: LampBeltMeta): { height: number; dragEnabled: boolean } {
  const hasLamp = lampBeltMeta?.enabled || plots.some((p) => CIRCLE_TYPE_ALIASES.includes(String(p?.type || '').toLowerCase()))
  const laneCount = Number(lampBeltMeta?.laneCount || 0)
  return { height: hasLamp ? Math.max(170, Math.min(280, 82 + laneCount * 22)) : 100, dragEnabled: true }
}

/** 内容签名：同代码同 plots 结构 → 同名指标，重跑零注册泄漏 */
export function contentSignature(payload: unknown): string {
  const json = JSON.stringify(payload) || ''
  let hash = 5381
  for (let i = 0; i < json.length; i++) {
    hash = ((hash << 5) + hash + json.charCodeAt(i)) | 0
  }
  return (hash >>> 0).toString(36)
}

/**
 * 注册指标（te）：重名直接覆盖注册（klinecharts 9 register 为覆盖语义），
 * 返回是否成功。
 */
export function registerRuntimeIndicator(options: {
  signature: string
  calc: (data: Array<Record<string, unknown>>) => Array<Record<string, unknown>>
  figures: Array<Record<string, unknown>>
  calcParams?: unknown[]
  precision?: number
  isPriceSeries?: boolean
  shortName?: string | null
  extra?: Record<string, unknown>
}): boolean {
  try {
    const indicator = {
      name: options.signature,
      shortName: options.shortName || options.signature,
      calc: options.calc,
      figures: options.figures,
      calcParams: options.calcParams ?? [],
      precision: options.precision ?? runtimePrecision,
      series: options.isPriceSeries ? 'price' : 'normal',
      ...options.extra,
    }
    registerIndicator(indicator as never)
    return true
  } catch {
    return false
  }
}

/* ── 灯带面板绘制（pr/wr 移植） ────────────────────────────── */

function roundedRectPath(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  width: number,
  height: number,
  radius: number,
): void {
  const r = Math.max(0, Math.min(radius, width / 2, height / 2))
  ctx.beginPath()
  ctx.moveTo(x + r, y)
  ctx.lineTo(x + width - r, y)
  ctx.quadraticCurveTo(x + width, y, x + width, y + r)
  ctx.lineTo(x + width, y + height - r)
  ctx.quadraticCurveTo(x + width, y + height, x + width - r, y + height)
  ctx.lineTo(x + r, y + height)
  ctx.quadraticCurveTo(x, y + height, x, y + height - r)
  ctx.lineTo(x, y + r)
  ctx.quadraticCurveTo(x, y, x + r, y)
  ctx.closePath()
}

function makeLampBeltDraw(shortName: string) {
  return ({ ctx, indicator, bounding }: { ctx: CanvasRenderingContext2D; indicator?: { shortName?: string; extendData?: { lampBelt?: { enabled?: boolean; laneCount?: number; lanes?: Array<{ lane: number; label: string }> } } }; bounding?: { left?: number; top?: number; width?: number; height?: number } }) => {
    const lampBelt = indicator?.extendData?.lampBelt
    if (!lampBelt?.enabled) return false
    try {
      const isDark = runtimeTheme === 'dark'
      const left = Number(bounding?.left || 0)
      const top = Number(bounding?.top || 0)
      const width = Number(bounding?.width || 0)
      const height = Number(bounding?.height || 0)
      const laneCount = Math.max(1, Number(lampBelt.laneCount || 0))
      if (!width || !height || !laneCount) return false
      const paddingX = 12
      const headerH = 26
      const paddingBottom = 8
      const laneLabelWidth = 82
      const headerTop = top + 7
      const contentTop = top + headerH
      const laneHeight = Math.max(24, (height - headerH - paddingBottom) / laneCount)
      const lanes = Array.isArray(lampBelt.lanes) ? lampBelt.lanes : []
      const labelByLane = new Map(lanes.map((l) => [Number(l.lane), l.label]))
      const bg0 = isDark ? 'rgba(255,255,255,0.018)' : 'rgba(17,24,39,0.022)'
      const bg1 = isDark ? 'rgba(255,255,255,0.026)' : 'rgba(17,24,39,0.03)'
      const bg2 = isDark ? 'rgba(255,255,255,0.045)' : 'rgba(17,24,39,0.06)'
      const panelBg = isDark ? 'rgba(8,12,18,0.84)' : 'rgba(255,255,255,0.9)'
      const borderColor = isDark ? 'rgba(255,255,255,0.09)' : 'rgba(15,23,42,0.12)'
      const headerColor = isDark ? 'rgba(220,226,235,0.78)' : 'rgba(45,55,72,0.78)'
      const labelColor = isDark ? 'rgba(226,232,240,0.9)' : 'rgba(31,41,55,0.9)'
      const laneY = (lane: number): number | null => {
        const n = Number(lane)
        if (!Number.isFinite(n)) return null
        const clamped = Math.max(1, Math.min(laneCount, n))
        return contentTop + (laneCount - clamped + 0.5) * laneHeight
      }

      ctx.save()
      roundedRectPath(ctx, left + 6, top + 7, Math.max(0, width - 12), Math.max(0, height - 14), 8)
      ctx.fillStyle = bg0
      ctx.fill()
      ctx.font = '600 12px Inter, Arial, sans-serif'
      ctx.fillStyle = headerColor
      ctx.textAlign = 'left'
      ctx.textBaseline = 'alphabetic'
      ctx.fillText(shortName || indicator?.shortName || 'Lamp Belt', left + paddingX, top + 17)
      ctx.font = '700 10px Inter, Arial, sans-serif'
      for (let lane = 1; lane <= laneCount; lane++) {
        const y = laneY(lane)
        if (y == null) continue
        ctx.fillStyle = lane % 2 === 0 ? bg1 : bg2
        ctx.fillRect(left + laneLabelWidth, y - laneHeight / 2, Math.max(0, width - laneLabelWidth - 10), laneHeight)
        const label = labelByLane.get(lane)
        if (label) {
          const text = String(label).toUpperCase().slice(0, 14)
          const textWidth = ctx.measureText(text).width
          const pillWidth = textWidth + 12
          const pillX = left + laneLabelWidth - pillWidth - 6
          const pillY = y - 8
          ctx.beginPath()
          ctx.rect(pillX, pillY, pillWidth, 16)
          ctx.fillStyle = panelBg
          ctx.fill()
          ctx.strokeStyle = borderColor
          ctx.lineWidth = 1
          ctx.stroke()
          ctx.fillStyle = labelColor
          ctx.textAlign = 'left'
          ctx.textBaseline = 'middle'
          ctx.fillText(text, pillX + 6, y + 1)
        }
      }
      ctx.restore()
      return true
    } catch {
      return false
    }
  }
}

function makeLampBeltTooltip(name: string) {
  return () => ({ name: name || 'Lamp Belt', calcParamsText: '', icons: [], values: [] })
}

/* ── Overlays ─────────────────────────────────────────────── */

/**
 * signalTag 覆盖层（QuantDinger 同款视觉）：
 * 连接线在 K 线高/低点锚定圆点与标签之间，圆角标签按文字动态定宽（CJK 感知），
 * 支持 dashed 标记与回测多泳道模式。
 */
export const SIGNAL_TAG_OVERLAY = {
  name: 'signalTag',
  totalStep: 1,
  lock: true,
  needDefaultPointFigure: false,
  needDefaultXAxisFigure: false,
  needDefaultYAxisFigure: false,
  checkEventOn: () => false,
  createPointFigures: ({ coordinates, overlay }: { coordinates: Array<{ x: number; y: number }>; overlay: { extendData?: Record<string, unknown> } }) => {
    const extendData = overlay.extendData || {}
    const color = (extendData.color as string) || '#555555'
    const dashed = (extendData.markerStyle as string) === 'dashed'
    const isBacktest = extendData.source === 'backtest'
    if (!coordinates[0]) return []
    const x = coordinates[0].x
    const anchorY = coordinates[0].y
    const tagY = coordinates[1] ? coordinates[1].y : anchorY
    const padX = isBacktest ? 5 : 8
    const padY = isBacktest ? 2 : 4
    const fontSize = Number(extendData.fontSize) || (isBacktest ? 11 : 12)
    const text = String(extendData.text || '')
    const textWidth = text.split('').reduce((sum, ch) => sum + (ch.charCodeAt(0) > 255 ? 11 : 6.5), 0)
    const boxWidth = Math.max(textWidth + padX * 2, isBacktest ? 28 : 20)
    const boxHeight = fontSize + padY * 2
    const side = (extendData.side as string) || (extendData.type as string) || 'buy'
    const isBuy = side === 'buy'

    // 回测紧凑模式：右侧泳道小标签
    if (isBacktest && extendData.labelMode !== 'full') {
      const lane = Math.max(0, Math.min(3, Number(extendData.lane) || 0))
      const shortText = shortSignalText(extendData.shortText || text, side)
      const smallSize = Number(extendData.fontSize) || (dashed ? 9 : 10)
      const boxH = dashed ? 13 : 15
      const boxW = Math.max(18, Math.min(isBacktest ? 72 : 38, shortText.length * 7 + 10))
      const laneOffset = lane * 16
      const boxTop = isBuy ? anchorY + laneOffset : anchorY - boxH - laneOffset
      const dotY = tagY
      const textY = isBuy ? boxTop : boxTop + boxH
      const fill = dashed ? 'rgba(0,0,0,0)' : color
      const textColor = dashed ? hexToRgba(color, 0.86) : '#ffffff'
      return [
        {
          type: 'line',
          attrs: { coordinates: [{ x, y: dotY }, { x, y: textY }] },
          styles: { style: 'stroke', color: hexToRgba(color, dashed ? 0.34 : 0.46), dashedValue: dashed ? [2, 4] : [2, 3] },
          ignoreEvent: true,
        },
        {
          type: 'circle',
          attrs: { x, y: dotY, r: dashed ? 2 : 2.5 },
          styles: dashed
            ? { style: 'stroke', color: hexToRgba(color, 0.82), lineWidth: 1.2 }
            : { style: 'fill', color },
          ignoreEvent: true,
        },
        {
          type: 'rect',
          attrs: { x: x - boxW / 2, y: boxTop, width: boxW, height: boxH, r: 4 },
          styles: {
            style: dashed ? 'stroke' : 'stroke_fill',
            color: fill,
            borderColor: dashed ? hexToRgba(color, 0.86) : color,
            borderSize: 1,
            borderDashedValue: dashed ? [3, 3] : [],
          },
          ignoreEvent: true,
        },
        {
          type: 'text',
          attrs: { x, y: boxTop + boxH / 2, text: shortText, align: 'center', baseline: 'middle' },
          styles: { color: textColor, size: smallSize, weight: '700', backgroundColor: 'transparent' },
          ignoreEvent: true,
        },
      ]
    }

    // 标准模式：标签贴在 K 线高/低点外侧
    const boxTop = isBuy ? anchorY : anchorY - boxHeight
    const dotY = tagY
    const connectorBottom = isBuy ? boxTop : boxTop + boxHeight
    const connectorStyle = isBacktest
      ? { style: 'stroke', color, dashedValue: dashed ? [3, 2] : [4, 3] }
      : { style: 'stroke', color, dashedValue: [2, 2] }
    const figures: Array<Record<string, unknown>> = [
      {
        type: 'line',
        attrs: { coordinates: [{ x, y: dotY }, { x, y: connectorBottom }] },
        styles: connectorStyle,
        ignoreEvent: true,
      },
      {
        type: 'circle',
        attrs: { x, y: dotY, r: isBacktest ? 2.5 : 3 },
        styles: { style: 'fill', color },
        ignoreEvent: true,
      },
      {
        type: 'rect',
        attrs: { x: x - boxWidth / 2, y: boxTop, width: boxWidth, height: boxHeight, r: 4 },
        styles: { style: 'fill', color },
        ignoreEvent: true,
      },
      {
        type: 'text',
        attrs: { x, y: boxTop + boxHeight / 2, text, align: 'center', baseline: 'middle' },
        styles: { color: '#ffffff', size: fontSize, weight: 'bold', backgroundColor: 'transparent' },
        ignoreEvent: true,
      },
    ]
    return figures
  },
}

export const ZONE_OVERLAY = {
  name: 'xtIndicatorZone',
  totalStep: 2,
  lock: true,
  needDefaultPointFigure: false,
  needDefaultXAxisFigure: false,
  needDefaultYAxisFigure: false,
  checkEventOn: () => false,
  createPointFigures: ({ coordinates, overlay }: { coordinates: Array<{ x: number; y: number }>; overlay: { extendData?: Record<string, unknown> } }) => {
    if (!coordinates[0] || !coordinates[1]) return []
    const extendData = overlay.extendData || {}
    const left = Math.min(coordinates[0].x, coordinates[1].x)
    const right = Math.max(coordinates[0].x, coordinates[1].x)
    const top = Math.min(coordinates[0].y, coordinates[1].y)
    const bottom = Math.max(coordinates[0].y, coordinates[1].y)
    const isDark = runtimeTheme === 'dark'
    const text = String(extendData.text || '')
    const lower = text.toLowerCase()
    const semanticColor =
      lower.includes('risk') || lower.includes('atr') ? '#fa8c16'
      : lower.includes('support') ? '#13c2c2'
      : lower.includes('resistance') ? '#f5222d'
      : '#1890ff'
    const color = (extendData.color as string) || semanticColor
    const opacity = Number.isFinite(Number(extendData.opacity)) ? Math.min(Number(extendData.opacity), 0.1) : isDark ? 0.075 : 0.055
    const fill = resolveAlphaColor(extendData.fillColor, color, opacity)
    const border = resolveAlphaColor(extendData.borderColor, color, isDark ? 0.48 : 0.42)
    const figures: Array<Record<string, unknown>> = [
      {
        type: 'rect',
        attrs: { x: left, y: top, width: Math.max(1, right - left), height: Math.max(1, bottom - top) },
        styles: {
          style: 'stroke_fill',
          color: fill,
          borderColor: border,
          borderSize: Number(extendData.borderSize || 1),
          borderDashedValue: extendData.dashed === false ? [] : [5, 4],
        },
        ignoreEvent: true,
      },
    ]
    if (text) {
      const size = Number(extendData.fontSize || 10)
      const labelWidth = estimateTextWidth(text, size, 48, 150)
      const labelHeight = Math.max(17, size + 8)
      const labelX = left + 8
      const labelY = top + 8
      figures.push(
        {
          type: 'rect',
          attrs: { x: labelX, y: labelY, width: labelWidth, height: labelHeight, r: 5 },
          styles: {
            style: 'stroke_fill',
            color: isDark ? 'rgba(14, 18, 25, 0.78)' : 'rgba(255, 255, 255, 0.78)',
            borderColor: resolveAlphaColor(extendData.borderColor, color, isDark ? 0.38 : 0.3),
            borderSize: 1,
          },
          ignoreEvent: true,
        },
        {
          type: 'text',
          attrs: { x: labelX + 9, y: labelY + labelHeight / 2, text, align: 'left', baseline: 'middle' },
          styles: {
            color: (extendData.textColor as string) || color,
            size,
            weight: '700',
            backgroundColor: 'transparent',
          },
          ignoreEvent: true,
        },
      )
    }
    return figures
  },
}

export const LINE_OVERLAY = {
  name: 'xtIndicatorLine',
  totalStep: 2,
  lock: true,
  needDefaultPointFigure: false,
  needDefaultXAxisFigure: false,
  needDefaultYAxisFigure: false,
  checkEventOn: () => false,
  createPointFigures: ({ coordinates, overlay }: { coordinates: Array<{ x: number; y: number }>; overlay: { extendData?: Record<string, unknown> } }) => {
    if (!coordinates[0] || !coordinates[1]) return []
    const extendData = overlay.extendData || {}
    const color = (extendData.color as string) || '#1890ff'
    const figures: Array<Record<string, unknown>> = [
      {
        type: 'line',
        attrs: { coordinates: [{ x: coordinates[0].x, y: coordinates[0].y }, { x: coordinates[1].x, y: coordinates[1].y }] },
        styles: { style: 'stroke', color, size: Number(extendData.lineWidth || 1), dashedValue: extendData.dashed ? [5, 5] : [] },
        ignoreEvent: true,
      },
    ]
    if (extendData.text) {
      figures.push({
        type: 'text',
        attrs: { x: coordinates[1].x + 6, y: coordinates[1].y - 6, text: String(extendData.text), align: 'left', baseline: 'bottom' },
        styles: { color: (extendData.textColor as string) || color, size: Number(extendData.fontSize || 10), weight: '700', backgroundColor: 'transparent' },
        ignoreEvent: true,
      })
    }
    return figures
  },
}

export const LABEL_OVERLAY = {
  name: 'xtIndicatorLabel',
  totalStep: 1,
  lock: true,
  needDefaultPointFigure: false,
  needDefaultXAxisFigure: false,
  needDefaultYAxisFigure: false,
  checkEventOn: () => false,
  createPointFigures: ({ coordinates, overlay }: { coordinates: Array<{ x: number; y: number }>; overlay: { extendData?: Record<string, unknown> } }) => {
    if (!coordinates[0]) return []
    const extendData = overlay.extendData || {}
    const isDark = runtimeTheme === 'dark'
    const color = (extendData.color as string) || '#1890ff'
    const text = String(extendData.text || '')
    const size = Number(extendData.fontSize || 11)
    const textWidth = estimateTextWidth(text, size, 40, 160)
    const boxHeight = Math.max(18, size + 8)
    const figures: Array<Record<string, unknown>> = [
      {
        type: 'rect',
        attrs: { x: coordinates[0].x + 6, y: coordinates[0].y - boxHeight / 2, width: textWidth, height: boxHeight, r: 5 },
        styles: {
          style: 'stroke_fill',
          color: resolveAlphaColor(extendData.fillColor, isDark ? '#0e1219' : '#ffffff', isDark ? 0.85 : 0.92),
          borderColor: resolveAlphaColor(extendData.borderColor, color, isDark ? 0.5 : 0.42),
          borderSize: 1,
        },
        ignoreEvent: true,
      },
      {
        type: 'text',
        attrs: { x: coordinates[0].x + 6 + textWidth / 2, y: coordinates[0].y, text, align: 'center', baseline: 'middle' },
        styles: { color: (extendData.textColor as string) || color, size, weight: '700', backgroundColor: 'transparent' },
        ignoreEvent: true,
      },
    ]
    return figures
  },
}

export function registerRuntimeOverlays(): void {
  for (const overlay of [SIGNAL_TAG_OVERLAY, ZONE_OVERLAY, LINE_OVERLAY, LABEL_OVERLAY]) {
    try {
      registerOverlay(overlay as never)
    } catch {
      /* 已注册则跳过 */
    }
  }
}

/* ── layers → overlay specs（On/nt/at 移植） ────────────────── */

/** 索引/时间戳 → klinecharts 时间戳（nt）：索引越界时按时间值解析 */
function resolveTime(value: unknown, klines: KLineLike[], indexFallback: number): number | null {
  const raw = value ?? indexFallback
  if (raw == null) return null
  const numeric = Number(raw)
  if (!Number.isFinite(numeric)) return null
  if (Number.isInteger(numeric) && numeric >= 0 && numeric < klines.length) {
    const ts = tsOf(klines[numeric])
    return ts
  }
  return numeric < 1e10 ? numeric * 1000 : numeric
}

function firstNumber(...values: unknown[]): number | null {
  for (const value of values) {
    const n = Number(value)
    if (Number.isFinite(n)) return n
  }
  return null
}

export interface OverlaySpec {
  name: string
  points: Array<{ timestamp: number; value: number }>
  extendData: Record<string, unknown>
  lock: boolean
}

export function layersToOverlaySpecs(layers: IndicatorLayerSpec[], klines: KLineLike[]): OverlaySpec[] {
  if (!Array.isArray(layers) || !layers.length || !klines.length) return []
  const specs: OverlaySpec[] = []
  const lastIndex = klines.length - 1
  for (const layer of layers) {
    if (!layer || typeof layer !== 'object') continue
    const type = String(layer.type || '').toLowerCase()
    if (['zone', 'box', 'rect', 'area'].includes(type)) {
      const startIdx = layer.startIndex ?? layer.fromIndex ?? layer.index ?? 0
      const endIdxRaw = Number(layer.endIndex ?? layer.toIndex ?? layer.end)
      const endIdx = Number.isFinite(endIdxRaw) ? endIdxRaw : lastIndex
      const startTs = resolveTime(layer.start ?? layer.from ?? layer.x1, klines, startIdx)
      const endTs = resolveTime(layer.end ?? layer.to ?? layer.x2, klines, endIdx)
      const top = firstNumber(layer.top, layer.high, layer.y1, layer.price1)
      const bottom = firstNumber(layer.bottom, layer.low, layer.y2, layer.price2)
      if (startTs == null || endTs == null || top == null || bottom == null) continue
      specs.push({
        name: 'xtIndicatorZone',
        points: [
          { timestamp: startTs, value: top },
          { timestamp: endTs, value: bottom },
        ],
        extendData: {
          text: layer.text || layer.name || '',
          color: layer.color,
          fillColor: layer.fillColor,
          borderColor: layer.borderColor,
          opacity: layer.opacity,
          dashed: layer.dashed,
          fontSize: layer.fontSize,
          textColor: layer.textColor,
        },
        lock: true,
      })
      continue
    }
    if (['line', 'segment', 'level', 'ray'].includes(type)) {
      const startIdx = layer.startIndex ?? layer.fromIndex ?? layer.index ?? 0
      const endIdxRaw = Number(layer.endIndex ?? layer.toIndex ?? layer.end)
      const endIdx = Number.isFinite(endIdxRaw) ? endIdxRaw : lastIndex
      const startTs = resolveTime(layer.start ?? layer.from ?? layer.x1 ?? layer.startTime, klines, startIdx)
      const endTs = resolveTime(layer.end ?? layer.to ?? layer.x2 ?? layer.endTime, klines, endIdx)
      const startPrice = firstNumber(layer.y1, layer.price1, layer.price, layer.level)
      const endPrice = firstNumber(layer.y2, layer.price2, layer.price, layer.level)
      if (startTs == null || endTs == null || startPrice == null || endPrice == null) continue
      specs.push({
        name: 'xtIndicatorLine',
        points: [
          { timestamp: startTs, value: startPrice },
          { timestamp: endTs, value: endPrice },
        ],
        extendData: {
          text: layer.text || layer.name || '',
          color: layer.color,
          lineWidth: layer.lineWidth,
          dashed: layer.dashed,
          fontSize: layer.fontSize,
          textColor: layer.textColor,
        },
        lock: true,
      })
      continue
    }
    if (['label', 'tag', 'note'].includes(type)) {
      const ts = resolveTime(layer.timestamp ?? layer.time ?? layer.index, klines, layer.index ?? lastIndex)
      const price = firstNumber(layer.price, layer.value, layer.y)
      if (ts == null || price == null) continue
      specs.push({
        name: 'xtIndicatorLabel',
        points: [{ timestamp: ts, value: price }],
        extendData: {
          text: layer.text || layer.name || '',
          color: layer.color,
          fillColor: layer.fillColor,
          borderColor: layer.borderColor,
          side: layer.side,
          fontSize: layer.fontSize,
          textColor: layer.textColor,
        },
        lock: true,
      })
    }
  }
  return specs
}

/* ── 顶层渲染入口 ──────────────────────────────────────────── */

export interface CreatedResource {
  indicators: Array<{ paneId: string; name: string }>
  overlays: string[]
}

interface ChartLike {
  createIndicator?: (name: string, isStack?: boolean, paneOptions?: unknown) => string | null
  createOverlay?: (overlay: unknown, paneId?: string) => string | null
}

/**
 * 把归一化后的 output 渲染到图表上（QuantDinger ge() 的移植）：
 *  1. plots 分主图/副图组，按内容签名注册指标并创建实例
 *  2. signalPoints → signalTag overlay（锚点=K线高/低点，标签在外侧）
 *  3. layers → zone/line/label overlay
 * 所有创建的资源记录到 created，由调用方负责重跑前清理。
 */
export function renderIndicatorOutput(
  chart: ChartLike,
  output: IndicatorOutput,
  klines: KLineLike[],
  created: CreatedResource,
): void {
  if (!chart || !klines.length) return
  const normalized = normalizeIndicatorOutput(output, klines)

  // ── plots → indicators ──
  const plots = normalized.plots.filter((p) => p && Array.isArray(p.data) && p.data.length > 0)
  if (plots.length > 0) {
    const name = normalized.name || 'Custom Indicator'
    const { overlayPlots, panePlots } = splitOverlayPane(plots)
    const groups = [
      { plots: overlayPlots, shouldOverlay: true, groupName: 'overlay' },
      { plots: panePlots, shouldOverlay: false, groupName: 'pane' },
    ]
    for (const group of groups) {
      if (!group.plots.length) continue
      const build = buildPlotGroup(group.plots, klines.length)
      const signature = `XT_${group.groupName}_${contentSignature({
        v: 1,
        name,
        plots: group.plots.map((p) => ({ name: p.name, title: p.title, type: p.type, overlay: p.overlay, color: p.color })),
        lampLaneCount: build.lampBeltMeta.laneCount,
        len: klines.length,
      })}`
      const extra: Record<string, unknown> = {}
      if (build.lampBeltMeta.enabled && build.lampBeltExtendData) {
        extra.minValue = 1
        extra.maxValue = build.lampBeltMeta.laneCount
        extra.extendData = { lampBelt: build.lampBeltExtendData }
        extra.draw = makeLampBeltDraw(name)
        extra.createTooltipDataSource = makeLampBeltTooltip(name)
      }
      const ok = registerRuntimeIndicator({
        signature,
        calc: makePlotCalc(klines, build.plotDataMap),
        figures: build.figures,
        calcParams: [],
        isPriceSeries: group.shouldOverlay,
        shortName: name,
        extra,
      })
      if (!ok) continue
      const paneId = group.shouldOverlay
        ? chart.createIndicator?.(signature, false, { id: 'candle_pane' })
        : chart.createIndicator?.(signature, false, paneOptionsFor(group.plots, build.lampBeltMeta))
      if (paneId) {
        created.indicators.push({ paneId, name: signature })
      } else if (group.shouldOverlay) {
        created.indicators.push({ paneId: 'candle_pane', name: signature })
      }
    }
  }

  // ── signals → signalTag overlays ──
  if (typeof chart.createOverlay === 'function') {
    for (const point of normalized.signalPoints) {
      try {
        const overlayId = chart.createOverlay(
          {
            name: 'signalTag',
            points: [
              { timestamp: point.timestamp, value: point.anchorPrice },
              { timestamp: point.timestamp, value: point.price },
            ],
            extendData: {
              text: point.text,
              color: point.color,
              side: point.side,
              action: point.action,
              rawPrice: point.rawPrice,
            },
            lock: true,
          },
          'candle_pane',
        )
        if (overlayId) created.overlays.push(String(overlayId))
      } catch {
        /* 单点失败不影响其余 */
      }
    }

    // ── layers → zone/line/label overlays ──
    for (const spec of layersToOverlaySpecs(normalized.layers, klines)) {
      try {
        const overlayId = chart.createOverlay(spec, 'candle_pane')
        if (overlayId) created.overlays.push(String(overlayId))
      } catch {
        /* 单个图层失败不影响其余 */
      }
    }
  }
}
