/**
 * 统一 toast 入口（历史模块，保留 API 兼容）。
 *
 * 曾经这里维护一套独立的 module 级 toast 队列 + 独立 ToastContainer，
 * 但该容器只被交易页挂载，其它 20+ 页面调 toast() 没有任何可见反馈。
 * 现在 toast() 委托给 zustand 全局 store（Layout 统一挂载 ToastContainer），
 * 用户动作 toast 走 bypassCooldown，保证连点操作（如连续撤单）每次都有反馈。
 */
import { useToastStore } from '@/stores/toastStore'

type ToastType = 'success' | 'error' | 'info' | 'warning'

export function toast(type: ToastType, message: string) {
  useToastStore.getState().addToast({ type, message, duration: 3500 }, { bypassCooldown: true })
}

/**
 * @deprecated 全局 ToastContainer 已在 Layout 挂载（@/components/ToastContainer），
 * 页面内无需再挂。保留导出仅为兼容旧引用，渲染为空。
 */
export function ToastContainer() {
  return null
}
