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
      token: null,
      user: null,
      isAuthenticated: false,
      isLoading: false,
      error: null,
      hydrated: false,

      login: async (username, password) => {
        set({ isLoading: true, error: null })
        try {
          const res = await authApi.login(username, password)
          const token = res.access_token
          localStorage.setItem('xt-token', token)
          set({ token, isAuthenticated: true, user: res.user })
        } catch (e: any) {
          set({ error: e.message || '登录失败' })
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
        } catch (e: any) {
          set({ error: e.message || '验证码登录失败' })
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
        } catch (e: any) {
          set({ error: e.message || '注册失败' })
          throw e
        } finally {
          set({ isLoading: false })
        }
      },

      sendCode: async (email, codeType) => {
        set({ isLoading: true, error: null })
        try {
          await authApi.sendCode(email, codeType)
        } catch (e: any) {
          set({ error: e.message || '发送验证码失败' })
          throw e
        } finally {
          set({ isLoading: false })
        }
      },

      resetPassword: async (email, code, password) => {
        set({ isLoading: true, error: null })
        try {
          await authApi.resetPassword(email, code, password)
        } catch (e: any) {
          set({ error: e.message || '重置密码失败' })
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
      onRehydrateStorage: () => (state) => {
        if (state) {
          state.hydrated = true
        }
      },
    }
  )
)
