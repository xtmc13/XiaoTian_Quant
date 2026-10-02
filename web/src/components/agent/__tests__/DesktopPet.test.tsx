import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import React from 'react'

vi.mock('@/stores/authStore', () => ({
  useAuthStore: (selector?: (s: { isAuthenticated: boolean }) => boolean) =>
    selector ? selector({ isAuthenticated: true }) : { isAuthenticated: true },
}))

// 隔离面板实现：桌宠的行为只依赖 open/variant 这两个 props
vi.mock('../AgentChatPanel', () => ({
  AgentChatPanel: (props: { open: boolean; variant?: string }) => (
    <div data-testid="chat-panel" data-open={String(props.open)} data-variant={props.variant || 'float'} />
  ),
}))

import { DesktopPet } from '../DesktopPet'

async function flushFrames() {
  // 等待 toggle 里的双 requestAnimationFrame（jsdom 下每帧约 16ms）完成
  await act(async () => {
    await new Promise((r) => setTimeout(r, 80))
  })
}

function renderPet() {
  return render(
    <MemoryRouter>
      <DesktopPet />
    </MemoryRouter>
  )
}

describe('桌宠', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  it('渲染桌宠（role=button + 打开小天助手）', () => {
    renderPet()
    const pet = screen.getByLabelText('打开小天助手')
    expect(pet.getAttribute('role')).toBe('button')
    expect(pet.getAttribute('aria-expanded')).toBe('false')
  })

  it('点击全屏展开助手，Esc 收起，再点击重新展开', async () => {
    renderPet()
    const pet = screen.getByLabelText('打开小天助手')

    fireEvent.click(pet)
    await flushFrames()
    expect(screen.getByTestId('chat-panel').dataset.variant).toBe('full')
    expect(screen.getByLabelText('收起小天助手').getAttribute('aria-expanded')).toBe('true')

    fireEvent.keyDown(window, { key: 'Escape' })
    await flushFrames()
    const overlay = screen.getByTestId('chat-panel').parentElement as HTMLElement
    expect(overlay.style.clipPath).toContain('circle(0px')

    fireEvent.click(screen.getByLabelText('打开小天助手'))
    await flushFrames()
    expect(screen.getByTestId('chat-panel').parentElement?.style.clipPath).toContain('circle(150%')
  })

  it('拖动超过阈值不触发展开，且位置持久化', async () => {
    renderPet()
    const pet = screen.getByLabelText('打开小天助手')

    fireEvent.pointerDown(pet, { clientX: 100, clientY: 100, pointerId: 1 })
    fireEvent.pointerMove(pet, { clientX: 180, clientY: 150, pointerId: 1 })
    fireEvent.pointerUp(pet, { pointerId: 1 })
    await flushFrames()

    // 未展开
    expect(screen.queryByTestId('chat-panel')).toBeNull()
    // 位置已存：默认右下角起点(1024-72-24, 768-72-96)=(928,600) + 位移(80,50) → x 被 clamp 到 952
    const saved = JSON.parse(localStorage.getItem('xt-pet-pos') || '{}')
    expect(saved.x).toBe(952)
    expect(saved.y).toBe(650)
  })

  it('微小位移（<6px）视为点击，触发展开', async () => {
    renderPet()
    const pet = screen.getByLabelText('打开小天助手')

    fireEvent.pointerDown(pet, { clientX: 100, clientY: 100, pointerId: 1 })
    fireEvent.pointerMove(pet, { clientX: 102, clientY: 101, pointerId: 1 })
    fireEvent.pointerUp(pet, { pointerId: 1 })
    fireEvent.click(pet)
    await flushFrames()

    expect(screen.getByTestId('chat-panel')).toBeTruthy()
    expect(localStorage.getItem('xt-pet-pos')).toBeNull()
  })

  it('设置入口：改尺寸持久化，恢复默认形象', async () => {
    renderPet()
    fireEvent.click(screen.getByLabelText('桌宠设置'))

    const slider = screen.getByLabelText('桌宠尺寸') as HTMLInputElement
    fireEvent.change(slider, { target: { value: '100' } })
    expect(localStorage.getItem('xt-pet-size')).toBe('100')

    expect(screen.getByText('默认')).toBeTruthy()
    expect(screen.getByLabelText('关闭桌宠设置')).toBeTruthy()
  })
})
