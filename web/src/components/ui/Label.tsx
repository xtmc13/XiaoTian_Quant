import React from 'react'
import { cn } from '@/lib/utils'

export interface LabelProps extends React.LabelHTMLAttributes<HTMLLabelElement> {
  required?: boolean
}

export const Label = React.memo(function Label({
  children,
  className,
  required,
  ...props
}: LabelProps) {
  return (
    <label
      className={cn('block text-xs font-medium text-[#aaaaaa]', className)}
      {...props}
    >
      {children}
      {required && <span className="text-[#f5222d] ml-0.5">*</span>}
    </label>
  )
})

export default Label
