import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { authApi } from '@/lib/api'

interface User {
  id?: number
  username: string
  role: string
  nickname?: string
  email?: string
}

interface AuthState {
  token: string | null
  user: User | null
  isAuthenticated: boolean
  isLoading: boolean
  error: string | null
  hydrated: boolean
  // actions
  login: (username: string, password: string) => Promise<void>
  loginByCode: (email: string, code: string) => Promise<void>
  register: (data: { username: string; password: string; email: string; code: string; nickname?: string }) => Promise<void>
  sendCode: (email: string, codeType: string) => Promise<void>
  resetPassword: (email: string, code: string, password: string) => Promise<void>
  fetchUser: () => Promise<void>
  logout: () => void
  clearError: () => void
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      token: (typeof window !== 'undefined' && window.__E2E_AUTH__) ? 'e2e-test-token' : null,
      user: (typeof window !== 'undefined' && window.__E2E_AUTH__)
        ? { id: 1, username: 'e2e_user', role: 'user', nickname: 'E2E Tester' }
        : null,
      isAuthenticated: (typeof window !== 'undefined' && window.__E2E_AUTH__) || false,
      isLoading: false,
      error: null,
      hydrated: true,

      login: async (username, password) => {
        set({ isLoading: true, error: null })
        try {
          // E2E test bypass
          if (typeof window !== 'undefined' && window.__E2E_AUTH__) {
            const token = 'e2e-test-token'
            localStorage.setItem('xt-token', token)
            set({
              token,
              isAuthenticated: true,
              user: { id: 1, username: 'e2e_user', role: 'user', nickname: 'E2E Tester' },
            })
            return
          }
          const res = await authApi.login(username, password)
          const token = res.access_token
          localStorage.setItem('xt-token', token)
          set({ token, isAuthenticated: true, user: res.user })
        } catch (e: unknown) {
          const err = e instanceof Error ? e : new Error(String(e))
          set({ error: err.message || '登录失败' })
          throw e
        } finally {
          set({ isLoading: false })
        }
      },

      loginByCode: async (email, code) => {
        set({ isLoading: true, error: null })
        try {
          const res = await authApi.loginCode(email, code)
          const token = res.access_token
          localStorage.setItem('xt-token', token)
          set({ token, isAuthenticated: true, user: res.user })
        } catch (e: unknown) {
          const err = e instanceof Error ? e : new Error(String(e))
          set({ error: err.message || '验证码登录失败' })
          throw e
        } finally {
          set({ isLoading: false })
        }
      },

      register: async (data) => {
        set({ isLoading: true, error: null })
        try {
          const res = await authApi.register(data)
          const token = res.access_token
          localStorage.setItem('xt-token', token)
          set({ token, isAuthenticated: true, user: res.user })
        } catch (e: unknown) {
          const err = e instanceof Error ? e : new Error(String(e))
          set({ error: err.message || '注册失败' })
          throw e
        } finally {
          set({ isLoading: false })
        }
      },

      sendCode: async (email, codeType) => {
        set({ isLoading: true, error: null })
        try {
          await authApi.sendCode(email, codeType)
        } catch (e: unknown) {
          const err = e instanceof Error ? e : new Error(String(e))
          set({ error: err.message || '发送验证码失败' })
          throw e
        } finally {
          set({ isLoading: false })
        }
      },

      resetPassword: async (email, code, password) => {
        set({ isLoading: true, error: null })
        try {
          await authApi.resetPassword(email, code, password)
        } catch (e: unknown) {
          const err = e instanceof Error ? e : new Error(String(e))
          set({ error: err.message || '重置密码失败' })
          throw e
        } finally {
          set({ isLoading: false })
        }
      },

      fetchUser: async () => {
        try {
          const user = await authApi.me()
          set({ user })
        } catch {
          // silently fail; user may be viewing public pages
        }
      },

      logout: () => {
        localStorage.removeItem('xt-token')
        set({ token: null, user: null, isAuthenticated: false, error: null })
      },

      clearError: () => set({ error: null }),
    }),
    {
      name: 'xt-auth',
      partialize: (state) => ({ token: state.token, user: state.user, isAuthenticated: state.isAuthenticated }),
      merge: (persistedState, currentState) => {
        // E2E test bypass (window flag only, not localStorage token)
        const e2eAuth = typeof window !== 'undefined' && window.__E2E_AUTH__
        if (e2eAuth) {
          return {
            ...currentState,
            isAuthenticated: true,
            token: 'e2e-test-token',
            user: { id: 1, username: 'e2e_user', role: 'user', nickname: 'E2E Tester' },
            hydrated: true,
          }
        }
        // Strip corrupted E2E test data from previous versions
        const restored = { ...currentState, ...(persistedState as object), hydrated: true }
        if (restored.token === 'e2e-test-token') {
          restored.token = null
          restored.isAuthenticated = false
          restored.user = null
          if (typeof window !== 'undefined') {
            localStorage.removeItem('xt-token')
          }
        }
        return restored
      },
      onRehydrateStorage: () => (state) => {
        if (state) {
          state.hydrated = true
        }
      },
    }
  )
)
