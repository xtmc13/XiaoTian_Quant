import React from 'react'
import { cn } from '@/lib/utils'

interface EmptyStateProps {
  icon?: React.ReactNode
  title: string
  description?: string
  action?: React.ReactNode
  // Backward-compatible action shorthand
  actionLabel?: string
  onAction?: () => void
  className?: string
}

export const EmptyState: React.FC<EmptyStateProps> = ({
  icon,
  title,
  description,
  action,
  actionLabel,
  onAction,
  className,
}) => {
  const actionNode = action ?? (actionLabel ? (
    <button
      type="button"
      onClick={onAction}
      className="px-4 py-2 text-sm rounded-md bg-[#1890ff] text-white hover:bg-[#40a9ff] transition-colors"
    >
      {actionLabel}
    </button>
  ) : null)

  return (
    <div className={cn('text-center py-8 text-[#555]', className)}>
      {icon && (
        <div className="w-8 h-8 mx-auto mb-2 opacity-50 flex justify-center items-center">
          {icon}
        </div>
      )}
      <p className="text-sm">{title}</p>
      {description && <p className="text-xs text-[#666] mt-1">{description}</p>}
      {actionNode && <div className="mt-4">{actionNode}</div>}
    </div>
  )
}

export default EmptyState
