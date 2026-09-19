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

// 相同文案的 toast 同时只显示一个：重复触发时重置其计时器（延长显示），
// 既避免后台轮询堆叠刷屏，也保证用户连续操作（如连点撤单）始终有反馈。
const activeToasts = new Map<string, { id: number; timer: ReturnType<typeof setTimeout> }>() // key → 显示中的 toast

function dismiss(key: string) {
  const active = activeToasts.get(key)
  if (!active) return
  activeToasts.delete(key)
  toasts = toasts.filter(t => t.id !== active.id)
  notify()
}

export function toast(type: ToastType, message: string) {
  const key = `${type}:${message}`
  const existing = activeToasts.get(key)
  if (existing) {
    clearTimeout(existing.timer)
    existing.timer = setTimeout(() => dismiss(key), 3500)
    return
  }

  const id = nextId++
  const timer = setTimeout(() => dismiss(key), 3500)
  activeToasts.set(key, { id, timer })
  toasts = [...toasts, { id, type, message }]
  notify()
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
            t.type === 'error' && 'bg-[#F6465D]/90 text-foreground',
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

