import React from 'react'
import { Skeleton } from '@/components/ui/Skeleton'
import { cn } from '@/lib/utils'

export interface KPICardItem {
  label: string
  value: string | number
  icon: React.ReactNode
  variant?: 'default' | 'success' | 'error' | 'warning' | 'info'
}

interface KPICardProps extends KPICardItem {}

const variantStyles: Record<string, { label: string; value: string }> = {
  default:  { label: 'text-[#888]', value: 'text-[#e0e0e0]' },
  success:  { label: 'text-[#888]', value: 'text-[#52c41a]' },
  error:    { label: 'text-[#888]', value: 'text-[#f5222d]' },
  warning:  { label: 'text-[#888]', value: 'text-[#faad14]' },
  info:     { label: 'text-[#888]', value: 'text-[#1890ff]' },
}

export const KPICard: React.FC<KPICardProps> = ({ label, value, icon, variant = 'default' }) => {
  const styles = variantStyles[variant] || variantStyles.default
  return (
    <div className="rounded-xl border border-[#1c1c1c] bg-[#111] p-4">
      <div className="flex items-center gap-2 mb-2">
        {icon}
        <span className={cn('text-xs', styles.label)}>{label}</span>
      </div>
      <div className={cn('text-lg font-semibold', styles.value)}>{value}</div>
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
