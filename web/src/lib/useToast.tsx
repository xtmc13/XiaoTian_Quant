import { useState, useEffect, useCallback } from 'react'

type ToastType = 'success' | 'error' | 'info' | 'warning'

interface ToastItem {
  id: number
  type: ToastType
  message: string
}

let nextId = 1
let listeners: Array<(toasts: ToastItem[]) => void> = []
let toasts: ToastItem[] = []

function notify() {
  listeners.forEach(l => l([...toasts]))
}

export function toast(type: ToastType, message: string) {
  const id = nextId++
  toasts = [...toasts, { id, type, message }]
  notify()
  setTimeout(() => {
    toasts = toasts.filter(t => t.id !== id)
    notify()
  }, 3500)
}

export function useToast() {
  const [state, setState] = useState<ToastItem[]>([])

  useEffect(() => {
    listeners.push(setState)
    return () => { listeners = listeners.filter(l => l !== setState) }
  }, [])

  return { toasts: state }
}

export function ToastContainer() {
  const { toasts } = useToast()

  if (toasts.length === 0) return null

  return (
    <div className="fixed top-4 right-4 z-[9999] flex flex-col gap-2 pointer-events-none">
      {toasts.map(t => (
        <div
          key={t.id}
          className={cn(
            'px-4 py-2.5 rounded text-sm font-medium shadow-lg animate-in slide-in-from-right',
            t.type === 'success' && 'bg-[#0ECB81]/90 text-black',
            t.type === 'error' && 'bg-[#F6465D]/90 text-white',
            t.type === 'info' && 'bg-quant-card border border-quant-border text-foreground',
            t.type === 'warning' && 'bg-yellow-500/90 text-black',
          )}
        >
          {t.message}
        </div>
      ))}
    </div>
  )
}

import { cn } from './utils'

