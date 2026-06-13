import { create } from 'zustand'

export type ToastType = 'success' | 'error' | 'warning' | 'info'

export interface Toast {
  id: string
  type: ToastType
  message: string
  duration?: number
}

interface ToastState {
  toasts: Toast[]
  addToast: (toast: Omit<Toast, 'id'>) => void
  removeToast: (id: string) => void
  clearAll: () => void
}

let toastId = 0

// 冷却追踪：相同消息在 COOLDOWN_MS 内不重复弹出，解决后台轮询刷屏问题
const recentMessages = new Map<string, number>() // key → timestamp
const COOLDOWN_MS = 30_000 // 30 秒冷却

export const useToastStore = create<ToastState>((set) => ({
  toasts: [],
  addToast: (toast) => {
    const key = `${toast.type}:${toast.message}`
    const now = Date.now()
    const lastShown = recentMessages.get(key)
    if (lastShown && (now - lastShown) < COOLDOWN_MS) {
      return // 冷却期内，跳过
    }
    recentMessages.set(key, now)

    const id = `toast-${++toastId}`
    set((state) => ({
      toasts: [...state.toasts, { ...toast, id, duration: toast.duration ?? 4000 }],
    }))
    // Auto remove
    setTimeout(() => {
      set((state) => ({
        toasts: state.toasts.filter((t) => t.id !== id),
      }))
    }, toast.duration ?? 4000)
  },
  removeToast: (id) =>
    set((state) => ({
      toasts: state.toasts.filter((t) => t.id !== id),
    })),
  clearAll: () => set({ toasts: [] }),
}))
