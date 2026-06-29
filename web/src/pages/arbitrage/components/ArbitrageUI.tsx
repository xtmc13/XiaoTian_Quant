import { cn } from '@/lib/utils'

export function Toggle({
  value,
  onChange,
  disabled,
}: {
  value: boolean
  onChange: (v: boolean) => void
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      onClick={() => !disabled && onChange(!value)}
      disabled={disabled}
      className={cn(
        'relative h-5 w-10 rounded-full transition-colors',
        value ? 'bg-quant-gold' : 'bg-quant-border',
        disabled && 'opacity-50'
      )}
      role="switch"
      aria-checked={value}
    >
      <span
        className={cn(
          'absolute top-0.5 h-4 w-4 rounded-full bg-white transition-transform',
          value ? 'left-5' : 'left-0.5'
        )}
      />
    </button>
  )
}

export function TextInput({
  value,
  onChange,
  placeholder,
}: {
  value: string
  onChange: (v: string) => void
  placeholder?: string
}) {
  return (
    <input
      type="text"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white placeholder-muted-foreground outline-none transition-colors focus:border-quant-gold"
    />
  )
}

export function NumberInput({
  value,
  onChange,
  placeholder,
  min,
  max,
  step,
}: {
  value: number
  onChange: (v: number) => void
  placeholder?: string
  min?: number
  max?: number
  step?: number
}) {
  return (
    <input
      type="number"
      min={min}
      max={max}
      step={step}
      value={value}
      onChange={(e) => onChange(Number(e.target.value))}
      placeholder={placeholder}
      className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white placeholder-muted-foreground outline-none transition-colors focus:border-quant-gold"
    />
  )
}
