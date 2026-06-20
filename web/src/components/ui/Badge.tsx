import React from 'react'
import { cn } from '@/lib/utils'

export type BadgeVariant = 'default' | 'success' | 'warning' | 'error' | 'info' | 'neutral'

export interface BadgeProps {
  variant?: BadgeVariant
  children: React.ReactNode
  className?: string
  dot?: boolean
  dotClassName?: string
}

export const Badge = React.memo(function Badge({
  variant = 'default',
  children,
  className,
  dot = false,
  dotClassName,
}: BadgeProps) {
  const variantClasses: Record<BadgeVariant, string> = {
    default: 'bg-[#2a2a2a] text-[#ccc] border-[#333]',
    success: 'bg-[#52c41a]/10 text-[#52c41a] border-[#52c41a]/20',
    warning: 'bg-[#faad14]/10 text-[#faad14] border-[#faad14]/20',
    error: 'bg-[#f5222d]/10 text-[#f5222d] border-[#f5222d]/20',
    info: 'bg-[#1890ff]/10 text-[#1890ff] border-[#1890ff]/20',
    neutral: 'bg-[#1c1c1c] text-[#888] border-[#2a2a2a]',
  }

  const dotColors: Record<BadgeVariant, string> = {
    default: 'bg-[#ccc]',
    success: 'bg-[#52c41a]',
    warning: 'bg-[#faad14]',
    error: 'bg-[#f5222d]',
    info: 'bg-[#1890ff]',
    neutral: 'bg-[#888]',
  }

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-medium border',
        variantClasses[variant],
        className
      )}
    >
      {dot && (
        <span
          className={cn(
            'w-1.5 h-1.5 rounded-full',
            dotColors[variant],
            dotClassName
          )}
        />
      )}
      {children}
    </span>
  )
})

export default Badge
