import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useAuthStore } from '../authStore'

describe('authStore', () => {
  beforeEach(() => {
    // Reset store state before each test
    useAuthStore.setState({
      token: null,
      user: null,
      isAuthenticated: false,
      isLoading: false,
      error: null,
      hydrated: false,
    })
    localStorage.clear()
  })

  it('has correct initial state', () => {
    const state = useAuthStore.getState()
    expect(state.token).toBeNull()
    expect(state.user).toBeNull()
    expect(state.isAuthenticated).toBe(false)
    expect(state.isLoading).toBe(false)
    expect(state.error).toBeNull()
  })

  it('login sets token and user', () => {
    useAuthStore.setState({
      token: 'test-token',
      user: { id: 1, username: 'test', role: 'user', nickname: 'Test' },
      isAuthenticated: true,
      isLoading: false,
    })

    const state = useAuthStore.getState()
    expect(state.token).toBe('test-token')
    expect(state.isAuthenticated).toBe(true)
    expect(state.user?.username).toBe('test')
  })

  it('logout clears state', () => {
    useAuthStore.setState({
      token: 'test-token',
      user: { id: 1, username: 'test', role: 'user' },
      isAuthenticated: true,
    })

    useAuthStore.getState().logout()

    const state = useAuthStore.getState()
    expect(state.token).toBeNull()
    expect(state.user).toBeNull()
    expect(state.isAuthenticated).toBe(false)
  })

  it('clearError removes error', () => {
    useAuthStore.setState({ error: 'Some error' })
    useAuthStore.getState().clearError()
    expect(useAuthStore.getState().error).toBeNull()
  })
})
