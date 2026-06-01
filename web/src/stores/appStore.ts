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
  setTheme: (theme: Theme) => void
  setLayout: (layout: Layout) => void
  setSidebarCollapsed: (v: boolean) => void
  setSidebarBehavior: (v: SidebarBehavior) => void
  toggleSidebar: () => void
  setFixedHeader: (v: boolean) => void
  setLanguage: (lang: string) => void
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
      setTheme: (theme) => {
        set({ theme })
        const html = document.documentElement
        if (theme === 'dark') {
          html.classList.add('dark')
          html.classList.remove('light')
        } else {
          html.classList.remove('dark')
          html.classList.add('light')
        }
      },
      setLayout: (layout) => set({ layout }),
      setSidebarCollapsed: (v) => set({ sidebarCollapsed: v }),
      setSidebarBehavior: (v) => set({ sidebarBehavior: v }),
      toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
      setFixedHeader: (v) => set({ fixedHeader: v }),
      setLanguage: (lang) => set({ language: lang }),
    }),
    { name: 'xt-app' }
  )
)
