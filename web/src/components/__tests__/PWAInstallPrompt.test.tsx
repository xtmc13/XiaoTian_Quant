import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { PWAInstallPrompt } from '../PWAInstallPrompt'
import * as pwa from '@/lib/pwa'

describe('PWAInstallPrompt', () => {
  const originalMatchMedia = window.matchMedia

  beforeEach(() => {
    localStorage.clear()
    vi.spyOn(pwa, 'isStandalone').mockReturnValue(false)
    vi.spyOn(pwa, 'canInstall').mockReturnValue(true)
    vi.spyOn(pwa, 'promptInstall').mockResolvedValue(true)
  })

  afterEach(() => {
    vi.restoreAllMocks()
    window.matchMedia = originalMatchMedia
  })

  it('shows install prompt when installable', () => {
    render(<PWAInstallPrompt />)
    expect(screen.getByText('安装小天量化')).toBeTruthy()
    expect(screen.getByText('添加到主屏幕，离线可用，启动更快')).toBeTruthy()
  })

  it('hides when already in standalone mode', () => {
    vi.spyOn(pwa, 'isStandalone').mockReturnValue(true)
    const { container } = render(<PWAInstallPrompt />)
    expect(container.firstChild).toBeNull()
  })

  it('hides when previously dismissed', () => {
    localStorage.setItem('xt-pwa-dismissed', 'true')
    const { container } = render(<PWAInstallPrompt />)
    expect(container.firstChild).toBeNull()
  })

  it('calls promptInstall when install button clicked', async () => {
    render(<PWAInstallPrompt />)
    const installBtn = screen.getByText('安装')
    fireEvent.click(installBtn)
    expect(pwa.promptInstall).toHaveBeenCalled()
  })

  it('dismisses and persists when close button clicked', () => {
    render(<PWAInstallPrompt />)
    const closeBtn = screen.getByLabelText('关闭')
    fireEvent.click(closeBtn)
    expect(localStorage.getItem('xt-pwa-dismissed')).toBe('true')
  })
})
