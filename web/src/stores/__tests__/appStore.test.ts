import { describe, it, expect } from 'vitest'
import { useAppStore } from '../appStore'

describe('appStore', () => {
  it('has dark theme by default', () => {
    const state = useAppStore.getState()
    expect(state.theme).toBe('dark')
  })

  it('toggles theme', () => {
    useAppStore.getState().setTheme('light')
    expect(useAppStore.getState().theme).toBe('light')

    useAppStore.getState().setTheme('dark')
    expect(useAppStore.getState().theme).toBe('dark')
  })

  it('toggles sidebar', () => {
    const initial = useAppStore.getState().sidebarCollapsed
    useAppStore.getState().toggleSidebar()
    expect(useAppStore.getState().sidebarCollapsed).toBe(!initial)
  })

  it('sets layout', () => {
    useAppStore.getState().setLayout('top')
    expect(useAppStore.getState().layout).toBe('top')
  })

  it('sets language', () => {
    useAppStore.getState().setLanguage('en-US')
    expect(useAppStore.getState().language).toBe('en-US')
  })
})
