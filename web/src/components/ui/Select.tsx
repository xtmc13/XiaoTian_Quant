import React, { forwardRef } from 'react'
import { ChevronDown } from 'lucide-react'
import { cn } from '@/lib/utils'

export interface SelectOption {
  value: string
  label: string
  disabled?: boolean
}

export interface SelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {
  label?: string
  error?: string
  helperText?: string
  options: SelectOption[]
  placeholder?: string
}

export const Select = forwardRef<HTMLSelectElement, SelectProps>(function Select(
  { label, error, helperText, options, placeholder, className, ...props },
  ref
) {
  return (
    <div className="w-full">
      {label && (
        <label className="block text-xs font-medium text-[#aaaaaa] mb-1.5">
          {label}
          {props.required && <span className="text-[#f5222d] ml-0.5">*</span>}
        </label>
      )}
      <div className="relative">
        <select
          ref={ref}
          className={cn(
            'w-full rounded-xl border bg-[#0a0a0a] text-[#e0e0e0]',
            'transition-all duration-200 focus:outline-none focus:ring-2 focus:ring-[#1890ff]/30',
            'text-sm px-3 py-2.5 appearance-none',
            error
              ? 'border-[#f5222d] focus:border-[#f5222d]'
              : 'border-[#2a2a2a] focus:border-[#1890ff] hover:border-[#333]',
            props.disabled && 'opacity-50 cursor-not-allowed',
            className
          )}
          {...props}
        >
          {placeholder && (
            <option value="" disabled>
              {placeholder}
            </option>
          )}
          {options.map((opt) => (
            <option key={opt.value} value={opt.value} disabled={opt.disabled}>
              {opt.label}
            </option>
          ))}
        </select>
        <ChevronDown className="absolute right-3 top-1/2 -translate-y-1/2 h-4 w-4 text-[#666] pointer-events-none" />
      </div>
      {error && <p className="mt-1 text-xs text-[#f5222d]">{error}</p>}
      {helperText && !error && <p className="mt-1 text-xs text-[#666]">{helperText}</p>}
    </div>
  )
})

export default Select
