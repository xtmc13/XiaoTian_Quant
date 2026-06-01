/**
 * ParamPanel — renders dynamic form controls from # @param declarations.
 */
import { useMemo } from 'react'
import type { ParamDecl } from '@/lib/indicatorContract'
import { cn } from '@/lib/utils'

interface ParamPanelProps {
  params: ParamDecl[]
  values: Record<string, any>
  onChange: (name: string, value: any) => void
  className?: string
}

export function ParamPanel({ params, values, onChange, className }: ParamPanelProps) {
  if (params.length === 0) return null

  return (
    <div className={cn('space-y-2.5', className)}>
      <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground uppercase tracking-wider">
        <span className="font-medium">参数</span>
        <span className="text-quant-border">({params.length})</span>
      </div>
      {params.map((p) => (
        <ParamField key={p.name} param={p} value={values[p.name]} onChange={(v) => onChange(p.name, v)} />
      ))}
    </div>
  )
}

function ParamField({ param, value, onChange }: { param: ParamDecl; value: any; onChange: (v: any) => void }) {
  const controlledValue = value !== undefined ? value : param.default

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between">
        <label className="text-[11px] text-foreground font-medium">{param.name}</label>
        <span className="text-[10px] text-muted-foreground font-mono">{formatValue(controlledValue)}</span>
      </div>
      {param.type === 'bool' ? (
        <button
          onClick={() => onChange(!controlledValue)}
          className={cn(
            'relative inline-flex h-5 w-9 items-center rounded-full transition-colors',
            controlledValue ? 'bg-quant-gold' : 'bg-quant-border'
          )}
        >
          <span
            className={cn(
              'inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform',
              controlledValue ? 'translate-x-4.5' : 'translate-x-0.5'
            )}
          />
        </button>
      ) : param.values && param.values.length > 0 ? (
        <select
          value={String(controlledValue)}
          onChange={(e) => onChange(parseTypedValue(param.type, e.target.value))}
          className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1 text-[11px] text-white outline-none focus:border-quant-gold"
        >
          {param.values.map((v, i) => (
            <option key={i} value={String(v)}>
              {String(v)}
            </option>
          ))}
        </select>
      ) : param.range && (param.type === 'int' || param.type === 'float') ? (
        <div className="flex items-center gap-2">
          <input
            type="range"
            min={param.range.min}
            max={param.range.max}
            step={param.range.step}
            value={controlledValue}
            onChange={(e) => onChange(parseTypedValue(param.type, e.target.value))}
            className="flex-1 h-1 accent-quant-gold bg-quant-border rounded-lg appearance-none cursor-pointer"
          />
          <input
            type="number"
            min={param.range.min}
            max={param.range.max}
            step={param.range.step}
            value={controlledValue}
            onChange={(e) => onChange(parseTypedValue(param.type, e.target.value))}
            className="w-14 rounded border border-quant-border bg-quant-bg px-1.5 py-0.5 text-[10px] text-white text-right font-mono outline-none focus:border-quant-gold"
          />
        </div>
      ) : param.type === 'int' ? (
        <input
          type="number"
          value={controlledValue}
          onChange={(e) => onChange(parseInt(e.target.value, 10) || 0)}
          className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1 text-[11px] text-white outline-none focus:border-quant-gold"
        />
      ) : param.type === 'float' ? (
        <input
          type="number"
          step="any"
          value={controlledValue}
          onChange={(e) => onChange(parseFloat(e.target.value) || 0)}
          className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1 text-[11px] text-white outline-none focus:border-quant-gold"
        />
      ) : (
        <input
          type="text"
          value={controlledValue}
          onChange={(e) => onChange(e.target.value)}
          className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1 text-[11px] text-white outline-none focus:border-quant-gold"
        />
      )}
      {param.description && (
        <p className="text-[10px] text-muted-foreground leading-tight">{param.description}</p>
      )}
    </div>
  )
}

function parseTypedValue(type_: string, value: string): any {
  switch (type_) {
    case 'int':
      return parseInt(value, 10) || 0
    case 'float':
      return parseFloat(value) || 0
    case 'bool':
      return value === 'true' || value === '1'
    default:
      return value
  }
}

function formatValue(v: any): string {
  if (typeof v === 'boolean') return v ? 'ON' : 'OFF'
  if (typeof v === 'number') {
    if (Number.isInteger(v)) return String(v)
    return v.toFixed(2)
  }
  return String(v)
}
