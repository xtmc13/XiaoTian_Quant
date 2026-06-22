import React from 'react'
import { cn } from '@/lib/utils'

export interface SwitchProps extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'type' | 'onChange'> {
  label?: string
  labelPosition?: 'left' | 'right'
  onCheckedChange?: (checked: boolean) => void
  onChange?: React.ChangeEventHandler<HTMLInputElement>
}

export const Switch = React.forwardRef<HTMLInputElement, SwitchProps>(function Switch(
  { label, labelPosition = 'right', className, onCheckedChange, onChange, ...props },
  ref
) {
  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    onCheckedChange?.(e.target.checked)
    onChange?.(e)
  }

  return (
    <label className={cn('inline-flex items-center gap-2 cursor-pointer', className)}>
      {label && labelPosition === 'left' && (
        <span className={cn('text-sm', props.disabled ? 'text-[#555]' : 'text-[#aaa]')}>
          {label}
        </span>
      )}
      <div className="relative">
        <input
          ref={ref}
          type="checkbox"
          className="sr-only peer"
          {...props}
          onChange={handleChange}
        />
        <div
          className={cn(
            'w-10 h-5.5 rounded-full transition-all duration-200 border-2',
            'bg-[#2a2a2a] border-[#333] peer-checked:bg-[#1890ff] peer-checked:border-[#1890ff]',
            'peer-focus:ring-2 peer-focus:ring-[#1890ff]/30',
            'after:content-[""] after:absolute after:top-0.5 after:left-0.5',
            'after:bg-[#888] after:rounded-full after:h-4 after:w-4',
            'after:transition-all after:duration-200',
            'peer-checked:after:translate-x-5 peer-checked:after:bg-white'
          )}
          style={{ width: '2.5rem', height: '1.375rem' }}
        >
          <div
            className="absolute top-[2px] left-[2px] rounded-full bg-[#888] transition-all duration-200 peer-checked:translate-x-5 peer-checked:bg-white"
            style={{
              width: 'calc(1.375rem - 4px)',
              height: 'calc(1.375rem - 4px)',
            }}
          />
        </div>
      </div>
      {label && labelPosition === 'right' && (
        <span className={cn('text-sm', props.disabled ? 'text-[#555]' : 'text-[#aaa]')}>
          {label}
        </span>
      )}
    </label>
  )
})

export default Switch
