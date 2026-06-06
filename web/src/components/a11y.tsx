import { useEffect, useRef, ReactNode } from 'react'

/**
 * SkipLink — 键盘用户可跳过导航直达主内容。
 */
export function SkipLink({ targetId = 'main-content' }: { targetId?: string }) {
  return (
    <a
      href={`#${targetId}`}
      className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-[9999] focus:px-4 focus:py-2 focus:bg-quant-gold focus:text-quant-bg focus:rounded-md focus:font-medium"
    >
      跳转到主内容
    </a>
  )
}

/**
 * FocusTrap — 将焦点限制在模态框/对话框内（无障碍要求）。
 */
export function FocusTrap({
  children,
  isActive,
  onEscape,
}: {
  children: ReactNode
  isActive: boolean
  onEscape?: () => void
}) {
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!isActive) return
    const container = containerRef.current
    if (!container) return

    // Focus first focusable element
    const focusable = container.querySelectorAll<HTMLElement>(
      'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
    )
    if (focusable.length > 0) {
      focusable[0].focus()
    }

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onEscape?.()
        return
      }
      if (e.key !== 'Tab') return

      const elements = Array.from(
        container.querySelectorAll<HTMLElement>(
          'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
        )
      ).filter((el) => !('disabled' in el && (el as HTMLButtonElement | HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement).disabled) && el.offsetParent !== null)

      if (elements.length === 0) return

      const first = elements[0]
      const last = elements[elements.length - 1]

      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isActive, onEscape])

  return <div ref={containerRef}>{children}</div>
}

/**
 * LiveRegion — 屏幕阅读器通知区域。
 */
export function LiveRegion({
  message,
  priority = 'polite',
}: {
  message: string
  priority?: 'polite' | 'assertive'
}) {
  return (
    <div
      aria-live={priority}
      aria-atomic="true"
      className="sr-only"
    >
      {message}
    </div>
  )
}

/**
 * AccessibleButton — 带完整 aria 支持的按钮。
 */
export function AccessibleButton({
  children,
  onClick,
  ariaLabel,
  ariaExpanded,
  ariaControls,
  ariaPressed,
  disabled,
  className = '',
  type = 'button',
}: {
  children: ReactNode
  onClick?: () => void
  ariaLabel: string
  ariaExpanded?: boolean
  ariaControls?: string
  ariaPressed?: boolean
  disabled?: boolean
  className?: string
  type?: 'button' | 'submit' | 'reset'
}) {
  return (
    <button
      type={type}
      onClick={onClick}
      disabled={disabled}
      aria-label={ariaLabel}
      aria-expanded={ariaExpanded}
      aria-controls={ariaControls}
      aria-pressed={ariaPressed}
      className={`disabled:opacity-50 disabled:cursor-not-allowed ${className}`}
    >
      {children}
    </button>
  )
}

/**
 * AccessibleInput — 带 label + error + aria 支持的输入框。
 */
export function AccessibleInput({
  id,
  label,
  value,
  onChange,
  error,
  type = 'text',
  placeholder,
  required,
  className = '',
}: {
  id: string
  label: string
  value: string
  onChange: (val: string) => void
  error?: string
  type?: string
  placeholder?: string
  required?: boolean
  className?: string
}) {
  return (
    <div className={className}>
      <label htmlFor={id} className="block text-sm font-medium text-quant-text mb-1">
        {label}
        {required && <span className="text-red-400 ml-1">*</span>}
      </label>
      <input
        id={id}
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        required={required}
        aria-invalid={!!error}
        aria-describedby={error ? `${id}-error` : undefined}
        className={`w-full px-3 py-2 bg-quant-card border rounded-md text-sm text-quant-text placeholder:text-quant-text-secondary focus:outline-none focus:ring-1 focus:ring-quant-gold/50 ${
          error ? 'border-red-500/50' : 'border-quant-border'
        }`}
      />
      {error && (
        <p id={`${id}-error`} className="mt-1 text-xs text-red-400" role="alert">
          {error}
        </p>
      )}
    </div>
  )
}
