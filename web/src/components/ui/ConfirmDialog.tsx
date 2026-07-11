import { useState, useEffect, useCallback } from 'react'
import { X, AlertTriangle } from 'lucide-react'
import { cn } from '@/lib/utils'

export type ConfirmVariant = 'default' | 'danger'

export interface ConfirmDialogOptions {
  title: string
  message?: string
  confirmText?: string
  cancelText?: string
  variant?: ConfirmVariant
  input?: boolean
  inputLabel?: string
  defaultValue?: string
  inputType?: string
}

interface ConfirmDialogState extends ConfirmDialogOptions {
  open: boolean
  resolve: ((value: boolean | string | null) => void) | null
}

const initialState: ConfirmDialogState = {
  open: false,
  title: '',
  resolve: null,
}

export function useConfirmDialog() {
  const [state, setState] = useState<ConfirmDialogState>(initialState)

  const close = useCallback(() => {
    setState((prev) => ({ ...prev, open: false }))
  }, [])

  const confirm = useCallback((options: Omit<ConfirmDialogOptions, 'input'>): Promise<boolean> => {
    return new Promise((resolve) => {
      setState({
        ...initialState,
        ...options,
        input: false,
        open: true,
        resolve: (value) => resolve(Boolean(value)),
      })
    })
  }, [])

  const prompt = useCallback((options: ConfirmDialogOptions): Promise<string | null> => {
    return new Promise((resolve) => {
      setState({
        ...initialState,
        ...options,
        input: true,
        open: true,
        resolve: (value) => resolve(value === null ? null : String(value)),
      })
    })
  }, [])

  const handleConfirm = useCallback(
    (value: string) => {
      if (state.input) {
        state.resolve?.(value)
      } else {
        state.resolve?.(true)
      }
      close()
    },
    [state.resolve, state.input, close]
  )

  const handleCancel = useCallback(() => {
    state.resolve?.(state.input ? null : false)
    close()
  }, [state.resolve, state.input, close])

  const Dialog = useCallback(
    () => (
      <ConfirmDialog
        open={state.open}
        title={state.title}
        message={state.message}
        confirmText={state.confirmText}
        cancelText={state.cancelText}
        variant={state.variant}
        input={state.input}
        inputLabel={state.inputLabel}
        defaultValue={state.defaultValue}
        inputType={state.inputType}
        onConfirm={handleConfirm}
        onCancel={handleCancel}
      />
    ),
    [state, handleConfirm, handleCancel]
  )

  return { confirm, prompt, Dialog }
}

interface ConfirmDialogProps extends ConfirmDialogOptions {
  open: boolean
  onConfirm: (value: string) => void
  onCancel: () => void
}

export function ConfirmDialog({
  open,
  title,
  message,
  confirmText = '确认',
  cancelText = '取消',
  variant = 'default',
  input = false,
  inputLabel,
  defaultValue = '',
  inputType = 'text',
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const [inputValue, setInputValue] = useState(defaultValue)

  useEffect(() => {
    if (open) {
      setInputValue(defaultValue)
    }
  }, [open, defaultValue])

  if (!open) return null

  const isDanger = variant === 'danger'

  return (
    <div
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
      onClick={onCancel}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onCancel()
      }}
      tabIndex={-1}
    >
      <div
        role="document"
        className="w-full max-w-sm rounded-xl border border-quant-border bg-quant-card p-5 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start gap-3 mb-3">
          {isDanger && (
            <div className="mt-0.5 shrink-0 rounded-full bg-red-500/10 p-1.5">
              <AlertTriangle className="h-4 w-4 text-red-400" />
            </div>
          )}
          <div className="flex-1">
            <h3 className="text-base font-semibold text-foreground">{title}</h3>
            {message && <p className="mt-1 text-sm text-muted-foreground">{message}</p>}
          </div>
          <button
            onClick={onCancel}
            className="shrink-0 p-1 rounded text-muted-foreground hover:text-foreground hover:bg-white/5"
            aria-label="关闭"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {input && (
          <div className="mt-4">
            {inputLabel && <label className="mb-1.5 block text-xs text-muted-foreground">{inputLabel}</label>}
            <input
              type={inputType}
              value={inputValue}
              onChange={(e) => setInputValue(e.target.value)}
              className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white placeholder-muted-foreground outline-none transition-colors focus:border-quant-gold"
              autoFocus
              onKeyDown={(e) => {
                if (e.key === 'Enter') onConfirm(inputValue)
              }}
            />
          </div>
        )}

        <div className="mt-5 flex items-center justify-end gap-2">
          <button
            onClick={onCancel}
            className="px-3 py-1.5 rounded-md text-sm font-medium text-muted-foreground hover:text-foreground hover:bg-white/5 transition-colors"
          >
            {cancelText}
          </button>
          <button
            onClick={() => onConfirm(input ? inputValue : '')}
            className={cn(
              'px-3 py-1.5 rounded-md text-sm font-medium transition-colors',
              isDanger ? 'bg-red-500/20 text-red-400 hover:bg-red-500/30' : 'bg-quant-gold text-black hover:opacity-90'
            )}
          >
            {confirmText}
          </button>
        </div>
      </div>
    </div>
  )
}
