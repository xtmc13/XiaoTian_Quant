import React from 'react'
import { cn } from '@/lib/utils'

export interface SliderProps {
  label?: string
  min: number
  max: number
  step?: number
  value: number
  onChange: (value: number) => void
  disabled?: boolean
  showValue?: boolean
  valueFormatter?: (v: number) => string
  className?: string
}

export const Slider = React.memo(function Slider({
  label,
  min,
  max,
  step = 1,
  value,
  onChange,
  disabled = false,
  showValue = true,
  valueFormatter = (v) => String(v),
  className,
}: SliderProps) {
  const percentage = ((value - min) / (max - min)) * 100

  return (
    <div className={cn('w-full', className)}>
      <div className="flex items-center justify-between mb-1.5">
        {label && (
          <label className="text-xs font-medium text-[#aaaaaa]">
            {label}
          </label>
        )}
        {showValue && (
          <span className="text-xs font-mono text-[#1890ff]">
            {valueFormatter(value)}
          </span>
        )}
      </div>
      <div className="relative flex items-center">
        <input
          type="range"
          min={min}
          max={max}
          step={step}
          value={value}
          onChange={(e) => onChange(Number(e.target.value))}
          disabled={disabled}
          className={cn(
            'w-full h-1.5 rounded-full appearance-none cursor-pointer',
            'bg-[#2a2a2a] accent-[#1890ff]',
            'focus:outline-none focus:ring-2 focus:ring-[#1890ff]/30',
            disabled && 'opacity-50 cursor-not-allowed'
          )}
          style={{
            background: `linear-gradient(to right, #1890ff 0%, #1890ff ${percentage}%, #2a2a2a ${percentage}%, #2a2a2a 100%)`,
          }}
        />
      </div>
      <div className="flex justify-between mt-1">
        <span className="text-[10px] text-[#555]">{valueFormatter(min)}</span>
        <span className="text-[10px] text-[#555]">{valueFormatter(max)}</span>
      </div>
    </div>
  )
})

export default Slider
