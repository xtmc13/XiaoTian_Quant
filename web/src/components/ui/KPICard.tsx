import React from 'react'
import { Skeleton } from '@/components/ui/Skeleton'
import { cn } from '@/lib/utils'
import { ArrowRight } from 'lucide-react'

export interface KPICardItem {
  label: string
  value: string | number
  icon: React.ReactNode
  variant?: 'default' | 'success' | 'error' | 'warning' | 'info'
  // Extended props for backward compatibility and richer UI
  subValue?: string | number
  subLabel?: string
  trend?: 'up' | 'down' | 'neutral'
  ringProgress?: number
  onClick?: () => void
  onNavigate?: () => void
  primary?: boolean
}

type KPICardProps = KPICardItem

const variantStyles: Record<string, { label: string; value: string }> = {
  default:  { label: 'text-[#888]', value: 'text-[#e0e0e0]' },
  success:  { label: 'text-[#888]', value: 'text-[#52c41a]' },
  error:    { label: 'text-[#888]', value: 'text-[#f5222d]' },
  warning:  { label: 'text-[#888]', value: 'text-[#faad14]' },
  info:     { label: 'text-[#888]', value: 'text-[#1890ff]' },
}

const trendIcon = (trend?: string) => {
  if (trend === 'up') return <span className="text-[#52c41a]">▲</span>
  if (trend === 'down') return <span className="text-[#f5222d]">▼</span>
  return <span className="text-[#888]">—</span>
}

const RingProgress: React.FC<{ progress: number }> = ({ progress }) => {
  const pct = Math.max(0, Math.min(100, progress))
  const r = 16
  const c = 2 * Math.PI * r
  const dash = (pct / 100) * c
  return (
    <div className="relative w-10 h-10 flex items-center justify-center">
      <svg width="40" height="40" viewBox="0 0 40 40" className="-rotate-90">
        <circle cx="20" cy="20" r={r} stroke="#2a2a2a" strokeWidth="4" fill="none" />
        <circle
          cx="20"
          cy="20"
          r={r}
          stroke="#1890ff"
          strokeWidth="4"
          fill="none"
          strokeDasharray={`${dash} ${c - dash}`}
          strokeLinecap="round"
        />
      </svg>
      <span className="absolute text-[10px] font-medium text-[#e0e0e0]">{progress}%</span>
    </div>
  )
}

export const KPICard: React.FC<KPICardProps> = ({
  label,
  value,
  icon,
  variant = 'default',
  subValue,
  subLabel,
  trend,
  ringProgress,
  onClick,
  onNavigate,
  primary,
}) => {
  const styles = variantStyles[variant] || variantStyles.default
  const clickable = !!onClick

  const body = (
    <div
      className={cn(
        'rounded-xl border border-[#1c1c1c] bg-[#111] p-4 relative',
        primary && 'ring-1 ring-[#1890ff]/50',
        clickable && 'cursor-pointer hover:bg-[#1a1a1a] transition-colors'
      )}
    >
      <div className="flex items-start justify-between">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-2">
            {icon}
            <span className={cn('text-xs', styles.label)}>{label}</span>
          </div>
          <div className={cn('text-lg font-semibold', styles.value)}>{value}</div>
          {(subValue !== undefined || subLabel || trend) && (
            <div className="flex items-center gap-2 mt-1 text-xs">
              {trend && trendIcon(trend)}
              {subValue !== undefined && (
                <span className="text-[#888]">{subValue}</span>
              )}
              {subLabel && <span className="text-[#666]">{subLabel}</span>}
            </div>
          )}
        </div>
        {ringProgress !== undefined && (
          <div className="ml-2 shrink-0">
            <RingProgress progress={ringProgress} />
          </div>
        )}
        {onNavigate && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation()
              onNavigate()
            }}
            className="ml-2 shrink-0 p-1.5 rounded-md hover:bg-[#222] text-[#888] hover:text-[#e0e0e0] transition-colors"
            aria-label={`${label} 详情`}
          >
            <ArrowRight className="w-4 h-4" />
          </button>
        )}
      </div>
    </div>
  )

  if (!clickable) return body

  return (
    <div onClick={onClick} role="button" tabIndex={0} onKeyDown={(e) => e.key === 'Enter' && onClick?.()}>
      {body}
    </div>
  )
}

interface KPIGridProps {
  items: KPICardItem[]
  isLoading?: boolean
  className?: string
}

export const KPIGrid: React.FC<KPIGridProps> = ({ items, isLoading, className }) => {
  if (isLoading) {
    return (
      <div className={cn('grid grid-cols-2 sm:grid-cols-4 gap-3', className)}>
        {Array.from({ length: items.length || 4 }).map((_, i) => (
          <Skeleton key={i} className="h-20 rounded-xl" />
        ))}
      </div>
    )
  }

  return (
    <div className={cn('grid grid-cols-2 sm:grid-cols-4 gap-3', className)}>
      {items.map((item) => (
        <KPICard key={item.label} {...item} />
      ))}
    </div>
  )
}

export default KPICard
