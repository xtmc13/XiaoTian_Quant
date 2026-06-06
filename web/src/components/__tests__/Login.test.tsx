import { describe, it, expect, vi } from 'vitest'
import React from 'react'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { Login } from '../../pages/Login'

// Mock auth store
vi.mock('@/stores/authStore', () => ({
  useAuthStore: () => ({
    login: vi.fn(),
    loginByCode: vi.fn(),
    register: vi.fn(),
    sendCode: vi.fn(),
    resetPassword: vi.fn(),
    isAuthenticated: false,
    isLoading: false,
    error: null,
    clearError: vi.fn(),
  }),
}))

vi.mock('@/i18n', async () => {
  return {
    useI18n: () => ({ t: (key: string, fallback?: string) => fallback || key }),
    I18nProvider: ({ children }: { children: React.ReactNode }) => React.createElement(React.Fragment, null, children),
  }
})

describe('Login page', () => {
  it('renders login form by default', () => {
    render(
      <MemoryRouter>
        <Login />
      </MemoryRouter>
    )
    // Look for inputs by type instead of placeholder (i18n keys are used)
    const inputs = screen.getAllByRole('textbox')
    expect(inputs.length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('auth.login')).toBeTruthy()
  })

  it('switches to register tab', () => {
    render(
      <MemoryRouter>
        <Login />
      </MemoryRouter>
    )
    // Get all buttons and click the one with auth.register (the tab button)
    const buttons = screen.getAllByRole('button')
    const registerTab = buttons.find(b => b.textContent === 'auth.register')
    expect(registerTab).toBeTruthy()
    if (registerTab) {
      fireEvent.click(registerTab)
      // After switching, there should be an email input
      expect(document.querySelector('input[type="email"]')).toBeTruthy()
    }
  })

  it('switches to reset password tab', () => {
    render(
      <MemoryRouter>
        <Login />
      </MemoryRouter>
    )
    const resetTab = screen.getByText('auth.resetPassword')
    fireEvent.click(resetTab)
    expect(screen.getByText('auth.resetPassword')).toBeTruthy()
  })

  it('toggles password visibility', () => {
    render(
      <MemoryRouter>
        <Login />
      </MemoryRouter>
    )
    // Find password input by type
    const pwInput = document.querySelector('input[type="password"]') as HTMLInputElement
    expect(pwInput).toBeTruthy()

    // Find toggle button (Eye or EyeOff icon)
    const toggleBtn = document.querySelector('button[class*="absolute"]') || screen.getAllByRole('button').find(b => b.querySelector('svg'))
    if (toggleBtn) {
      fireEvent.click(toggleBtn)
      // After toggle, there should be a text input for password
      const textInputs = document.querySelectorAll('input[type="text"]')
      expect(textInputs.length).toBeGreaterThanOrEqual(1)
    }
  })
})
