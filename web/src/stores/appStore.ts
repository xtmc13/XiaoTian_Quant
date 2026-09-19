import { create } from 'zustand'
import { persist } from 'zustand/middleware'

type Theme = 'dark' | 'light'
type Layout = 'sidebar' | 'top'
type SidebarBehavior = 'hover' | 'click'

interface AppState {
  theme: Theme
  layout: Layout
  sidebarCollapsed: boolean
  sidebarBehavior: SidebarBehavior
  fixedHeader: boolean
  language: string
  /** 界面缩放倍率，0.9 / 1 / 1.1 */
  uiScale: number
  setTheme: (theme: Theme) => void
  setLayout: (layout: Layout) => void
  setSidebarCollapsed: (v: boolean) => void
  setSidebarBehavior: (v: SidebarBehavior) => void
  toggleSidebar: () => void
  setFixedHeader: (v: boolean) => void
  setLanguage: (lang: string) => void
  setUiScale: (v: number) => void
}

function applyThemeClass(theme: Theme) {
  const html = document.documentElement
  if (theme === 'dark') {
    html.classList.add('dark')
    html.classList.remove('light')
  } else {
    html.classList.remove('dark')
    html.classList.add('light')
  }
}

function applyUiScale(v: number) {
  // rem 布局随根字号等比缩放
  document.documentElement.style.fontSize = `${v * 100}%`
}

export const useAppStore = create<AppState>()(
  persist(
    (set) => ({
      theme: 'dark',
      layout: 'sidebar',
      sidebarCollapsed: false,
      sidebarBehavior: 'hover',
      fixedHeader: true,
      language: 'zh-CN',
      uiScale: 1,
      setTheme: (theme) => {
        set({ theme })
        applyThemeClass(theme)
      },
      setLayout: (layout) => set({ layout }),
      setSidebarCollapsed: (v) => set({ sidebarCollapsed: v }),
      setSidebarBehavior: (v) => set({ sidebarBehavior: v }),
      toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
      setFixedHeader: (v) => set({ fixedHeader: v }),
      setLanguage: (lang) => set({ language: lang }),
      setUiScale: (v) => {
        set({ uiScale: v })
        applyUiScale(v)
      },
    }),
    { name: 'xt-app' }
  )
)

// 启动时把持久化的外观设置应用到 DOM（刷新后主题/缩放不回跳）
export function bootstrapAppearance() {
  try {
    const raw = localStorage.getItem('xt-app')
    if (!raw) return
    const state = JSON.parse(raw)?.state as Partial<AppState> | undefined
    if (!state) return
    applyThemeClass(state.theme === 'light' ? 'light' : 'dark')
    if (typeof state.uiScale === 'number' && state.uiScale > 0) {
      applyUiScale(state.uiScale)
    }
  } catch {
    /* ignore malformed persisted state */
  }
}
