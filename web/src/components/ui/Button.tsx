import React from 'react'
import { cn } from '@/lib/utils'

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'default' | 'primary' | 'secondary' | 'danger' | 'ghost' | 'outline'
  size?: 'sm' | 'md' | 'lg'
  isLoading?: boolean
  leftIcon?: React.ReactNode
  rightIcon?: React.ReactNode
}

export const Button = React.memo(function Button({
  variant = 'default',
  size = 'md',
  isLoading = false,
  leftIcon,
  rightIcon,
  children,
  className,
  disabled,
  ...props
}: ButtonProps) {
  const variantClasses = {
    default: 'bg-[#2a2a2a] text-[#e0e0e0] border border-[#333] hover:bg-[#333] hover:border-[#444]',
    primary: 'bg-[#1890ff] text-white border border-[#1890ff] hover:bg-[#40a9ff] hover:border-[#40a9ff]',
    secondary: 'bg-[#52c41a] text-white border border-[#52c41a] hover:bg-[#73d13d] hover:border-[#73d13d]',
    danger: 'bg-[#f5222d] text-white border border-[#f5222d] hover:bg-[#ff4d4f] hover:border-[#ff4d4f]',
    ghost: 'bg-transparent text-[#aaa] border border-transparent hover:bg-[#1c1c1c] hover:text-[#e0e0e0]',
    outline: 'bg-transparent text-[#1890ff] border border-[#1890ff] hover:bg-[#1890ff]/10',
  }

  const sizeClasses = {
    sm: 'px-3 py-1.5 text-xs rounded-lg',
    md: 'px-4 py-2 text-sm rounded-xl',
    lg: 'px-6 py-3 text-base rounded-xl',
  }

  return (
    <button
      className={cn(
        'inline-flex items-center justify-center gap-1.5 font-medium transition-all duration-200',
        'focus:outline-none focus:ring-2 focus:ring-[#1890ff]/30',
        'disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:bg-current',
        variantClasses[variant],
        sizeClasses[size],
        className
      )}
      disabled={disabled || isLoading}
      {...props}
    >
      {isLoading && (
        <svg className="animate-spin h-4 w-4" viewBox="0 0 24 24">
          <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" fill="none" />
          <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
        </svg>
      )}
      {!isLoading && leftIcon}
      {children}
      {!isLoading && rightIcon}
    </button>
  )
})
