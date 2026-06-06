/// <reference types="vite/client" />

/**
 * Global type declarations for xiaotian-quant web frontend.
 * These extend the global Window interface and third-party modules.
 */

/* ── Window extensions ── */

declare global {
  interface Window {
    /** E2E test authentication bypass flag */
    __E2E_AUTH__?: boolean
  }
}

/* ── klinecharts module augmentation ── */
declare module 'klinecharts' {
  // Re-export the public API surface that the app actually uses.
  // This silences "Could not find a declaration file" errors without
  // needing a full @types/klinecharts package.

  export interface Chart {
    applyNewData(data: KLineData[]): void
    updateData(data: KLineData): void
    createOverlay(overlay: OverlayCreate): string | null
    removeOverlay(id: string): void
    setStyles(styles: Record<string, unknown>): void
    resize(): void
    getConvertPictureUrl(includeOverlay?: boolean, type?: string, backgroundColor?: string): string
    subscribeAction(type: string, callback: (data: unknown) => void): void
    unsubscribeAction(type: string, callback: (data: unknown) => void): void
    getDataList(): KLineData[]
    getVisibleRange(): { from: number; to: number; realFrom: number; realTo: number }
    scrollToRealTime(): void
    zoomAtCoordinate(scale: number, x: number, y: number): void
    convertToPixel(data: { timestamp: number; dataIndex?: number; value?: number }, paneId?: string): { x: number; y: number } | null
    convertFromPixel(pixel: { x: number; y: number }, paneId?: string): { timestamp: number; dataIndex?: number; value?: number } | null
    getIndicatorByPaneId(paneId?: string, name?: string): Indicator[]
    createIndicator(value: string | IndicatorCreate, isStack?: boolean, paneId?: string, callback?: (indicator: Indicator | null) => void): string | null
    overrideIndicator(indicator: IndicatorCreate, paneId?: string): void
    removeIndicator(paneId: string, name?: string): void
    setPriceVolumePrecision(pricePrecision: number, volumePrecision: number): void
    setTimezone(timezone: string): void
    getTimezone(): string
    setLocale(locale: string): void
    getLocale(): string
    destroy(): void
  }

  export interface KLineData {
    timestamp: number
    open: number
    high: number
    low: number
    close: number
    volume?: number
    turnover?: number
  }

  export interface OverlayCreate {
    name: string
    id?: string
    points?: Array<{ timestamp: number; value?: number; dataIndex?: number }>
    extendData?: Record<string, unknown>
    styles?: Record<string, unknown>
    lock?: boolean
  }

  export interface Indicator {
    name: string
    shortName?: string
    precision?: number
    calcParams?: number[]
    figures?: Array<Record<string, unknown>>
    result?: unknown[][]
  }

  export interface IndicatorCreate {
    name: string
    shortName?: string
    calcParams?: number[]
    precision?: number
    styles?: Record<string, unknown>
    extendData?: unknown
  }

  export function init(ds: HTMLElement | string, styles?: Record<string, unknown>): Chart
  export function dispose(ds: HTMLElement | string | Chart): void
  export function registerOverlay(overlay: {
    name: string
    totalStep?: number
    needDefaultPointFigure?: boolean
    needDefaultXAxisFigure?: boolean
    needDefaultYAxisFigure?: boolean
    onDrawStart?: (event: unknown) => boolean
    onDrawing?: (event: unknown) => boolean
    onDrawEnd?: (event: unknown) => boolean
    onClick?: (event: unknown) => boolean
    onRightClick?: (event: unknown) => boolean
    onPressedMove?: (event: unknown) => boolean
    onMouseEnter?: (event: unknown) => boolean
    onMouseLeave?: (event: unknown) => boolean
    onRemoved?: (event: unknown) => boolean
    extendData?: (data: unknown) => Record<string, unknown>
    createPointFigures?: (params: unknown) => Array<Record<string, unknown>>
    createXAxisFigures?: (params: unknown) => Array<Record<string, unknown>>
    createYAxisFigures?: (params: unknown) => Array<Record<string, unknown>>
  }): void
  export function registerFigure(figure: Record<string, unknown>): void
  export function version(): string
}

export {}
