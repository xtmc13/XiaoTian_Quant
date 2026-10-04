import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { Sidebar } from '../Sidebar'
import { I18nProvider } from '@/i18n'
import '@/i18n/locales/nav'
import { useAuthStore } from '@/stores/authStore'
import { useAppStore } from '@/stores/appStore'

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <I18nProvider>
        <Sidebar />
      </I18nProvider>
    </MemoryRouter>
  )
}

describe('Sidebar 导航接入', () => {
  beforeEach(() => {
    useAuthStore.setState({
      user: { id: 2, username: 'user', role: 'user' },
      token: 't',
      isAuthenticated: true,
    } as never)
    useAppStore.setState({ sidebarCollapsed: false, sidebarBehavior: 'fixed' } as never)
  })

  it('机器人中心组包含信号机器人入口', () => {
    renderAt('/bots/signal')
    const link = screen.getByRole('link', { name: '信号机器人' })
    expect(link.getAttribute('href')).toBe('/bots/signal')
  })

  it('AI 组包含 AI 总览 / RL 强化学习 / TensorBoard', () => {
    renderAt('/ai/rl')
    expect(screen.getByRole('link', { name: 'AI 总览' }).getAttribute('href')).toBe('/ai')
    expect(screen.getByRole('link', { name: 'RL 强化学习' }).getAttribute('href')).toBe('/ai/rl')
    expect(screen.getByRole('link', { name: 'TensorBoard' }).getAttribute('href')).toBe('/ai/tensorboard')
  })

  it('系统组包含系统状态 / 系统日志', () => {
    renderAt('/status')
    expect(screen.getByRole('button', { name: '系统' })).toBeTruthy()
    expect(screen.getByRole('link', { name: '系统状态' }).getAttribute('href')).toBe('/status')
    expect(screen.getByRole('link', { name: '系统日志' }).getAttribute('href')).toBe('/logs')
  })

  it('高级组包含数据下载 / 指标告警', () => {
    renderAt('/data')
    expect(screen.getByRole('link', { name: '数据下载' }).getAttribute('href')).toBe('/data')
    expect(screen.getByRole('link', { name: '指标告警' }).getAttribute('href')).toBe('/alerts')
  })
})
