import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import React from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
// 测试环境不经 main.tsx，显式注册词条
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/chrome'
import { I18nProvider } from '@/i18n'
import { AppUpdateBanner } from '../AppUpdateBanner'

function renderBanner() {
  return render(
    <I18nProvider>
      <AppUpdateBanner />
    </I18nProvider>
  )
}

function mockHealth(version: string) {
  return vi.fn(() =>
    Promise.resolve({ ok: true, json: () => Promise.resolve({ data: { version } }) } as Response)
  )
}

describe('AppUpdateBanner 新版本检测', () => {
  const originalReload = window.location.reload

  beforeEach(() => {
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { ...window.location, reload: vi.fn() },
    })
  })
  afterEach(() => {
    Object.defineProperty(window, 'location', { configurable: true, value: { ...window.location, reload: originalReload } })
    vi.unstubAllEnvs()
    vi.unstubAllGlobals()
  })

  it('服务端版本与构建版本一致：不弹横幅', async () => {
    vi.stubEnv('VITE_APP_VERSION', '3.1.6')
    vi.stubGlobal('fetch', mockHealth('3.1.6'))
    renderBanner()
    await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalled())
    expect(screen.queryByTestId('app-update-banner')).toBeNull()
  })

  it('服务端版本不同：弹横幅并显示新版本号，点击触发刷新', async () => {
    vi.stubEnv('VITE_APP_VERSION', '3.1.4')
    vi.stubGlobal('fetch', mockHealth('3.1.6'))
    renderBanner()
    const btn = await screen.findByTestId('app-update-banner')
    expect(btn.textContent).toContain('3.1.6')
    fireEvent.click(btn)
    expect(window.location.reload).toHaveBeenCalled()
  })

  it('dev 构建（未注入版本）：不请求不打扰', async () => {
    vi.stubEnv('VITE_APP_VERSION', '')
    vi.stubGlobal('fetch', mockHealth('3.1.6'))
    renderBanner()
    await new Promise((r) => setTimeout(r, 50))
    expect(vi.mocked(fetch)).not.toHaveBeenCalled()
    expect(screen.queryByTestId('app-update-banner')).toBeNull()
  })
})
