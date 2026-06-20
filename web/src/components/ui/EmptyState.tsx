import React from 'react'
import { cn } from '@/lib/utils'

interface EmptyStateProps {
  icon?: React.ReactNode
  title: string
  description?: string
  action?: React.ReactNode
  className?: string
}

export const EmptyState: React.FC<EmptyStateProps> = ({
  icon,
  title,
  description,
  action,
  className,
}) => (
  <div className={cn('text-center py-8 text-[#555]', className)}>
    {icon && (
      <div className="w-8 h-8 mx-auto mb-2 opacity-50 flex justify-center items-center">
        {icon}
      </div>
    )}
    <p className="text-sm">{title}</p>
    {description && <p className="text-xs text-[#666] mt-1">{description}</p>}
    {action && <div className="mt-4">{action}</div>}
  </div>
)

export default EmptyState
