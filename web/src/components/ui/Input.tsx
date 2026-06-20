import React, { forwardRef } from 'react'
import { cn } from '@/lib/utils'

export interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  label?: string
  error?: string
  helperText?: string
  leftIcon?: React.ReactNode
  rightIcon?: React.ReactNode
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { label, error, helperText, leftIcon, rightIcon, className, ...props },
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
        {leftIcon && (
          <div className="absolute left-3 top-1/2 -translate-y-1/2 text-[#666]">
            {leftIcon}
          </div>
        )}
        <input
          ref={ref}
          className={cn(
            'w-full rounded-xl border bg-[#0a0a0a] text-[#e0e0e0] placeholder-[#555]',
            'transition-all duration-200 focus:outline-none focus:ring-2 focus:ring-[#1890ff]/30',
            'text-sm px-3 py-2.5',
            leftIcon && 'pl-9',
            rightIcon && 'pr-9',
            error
              ? 'border-[#f5222d] focus:border-[#f5222d]'
              : 'border-[#2a2a2a] focus:border-[#1890ff] hover:border-[#333]',
            props.disabled && 'opacity-50 cursor-not-allowed',
            className
          )}
          {...props}
        />
        {rightIcon && (
          <div className="absolute right-3 top-1/2 -translate-y-1/2 text-[#666]">
            {rightIcon}
          </div>
        )}
      </div>
      {error && <p className="mt-1 text-xs text-[#f5222d]">{error}</p>}
      {helperText && !error && <p className="mt-1 text-xs text-[#666]">{helperText}</p>}
    </div>
  )
})

export default Input
