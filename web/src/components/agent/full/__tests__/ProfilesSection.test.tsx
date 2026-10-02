import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

const listMock = vi.fn()
const createMock = vi.fn()
const activateMock = vi.fn()
const removeMock = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    agentProfilesApi: {
      list: (...args: unknown[]) => listMock(...args),
      create: (...args: unknown[]) => createMock(...args),
      activate: (...args: unknown[]) => activateMock(...args),
      remove: (...args: unknown[]) => removeMock(...args),
    },
  }
})

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

import { SettingsModal } from '../SettingsModal'
import { useAuthStore } from '@/stores/authStore'

const DEFAULT_P = { id: 1, name: '主档案', is_active: true, is_default: true, created_at: 1_700_000_000 }
const WORK_P = { id: 2, name: '工作', is_active: false, is_default: false, created_at: 1_700_100_000 }

function renderSettings() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <SettingsModal
        settings={{ system_prompt: '', temperature: 0.7, model: '' }}
        onSave={() => {}}
        onClose={() => {}}
        providers={[]}
      />
    </QueryClientProvider>
  )
}

async function openProfilesSection() {
  fireEvent.click(screen.getByRole('button', { name: '档案' }))
  await waitFor(() => expect(listMock).toHaveBeenCalled())
}

describe('设置 · 档案区', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    useAuthStore.setState({ user: { id: 2, username: 'user', role: 'user' }, token: 't', isAuthenticated: true })
    listMock.mockResolvedValue({ success: true, profiles: [DEFAULT_P, WORK_P] })
    createMock.mockResolvedValue({ success: true, id: 3 })
    activateMock.mockResolvedValue({ success: true })
    removeMock.mockResolvedValue({ success: true })
  })

  it('列出档案：默认 chip + 创建日期，激活态 radio 选中', async () => {
    renderSettings()
    await openProfilesSection()
    await waitFor(() => expect(screen.getByText('工作')).toBeTruthy())
    expect(screen.getByText('主档案')).toBeTruthy()
    expect((screen.getByLabelText('激活档案 主档案') as HTMLInputElement).checked).toBe(true)
    expect((screen.getByLabelText('激活档案 工作') as HTMLInputElement).checked).toBe(false)
    // 默认档案没有删除按钮，非默认有
    expect(screen.queryByLabelText('删除档案 主档案')).toBeNull()
    expect(screen.getByLabelText('删除档案 工作')).toBeTruthy()
  })

  it('激活非当前档案调用 activate 并刷新', async () => {
    renderSettings()
    await openProfilesSection()
    await waitFor(() => expect(screen.getByText('工作')).toBeTruthy())
    fireEvent.click(screen.getByLabelText('切换激活 工作'))
    await waitFor(() => expect(activateMock).toHaveBeenCalledWith(2))
    await waitFor(() => expect(listMock.mock.calls.length).toBeGreaterThanOrEqual(2))
  })

  it('新建档案', async () => {
    renderSettings()
    await openProfilesSection()
    fireEvent.change(screen.getByLabelText('新档案名称'), { target: { value: '研究' } })
    fireEvent.click(screen.getByRole('button', { name: /新建/ }))
    await waitFor(() => expect(createMock).toHaveBeenCalledWith('研究'))
  })

  it('删除激活档案后重新拉取（回退到默认档案）', async () => {
    // 工作 激活中，删除后后端回退：默认变为激活
    listMock.mockResolvedValueOnce({
      success: true,
      profiles: [
        { ...DEFAULT_P, is_active: false },
        { ...WORK_P, is_active: true },
      ],
    })
    renderSettings()
    await openProfilesSection()
    await waitFor(() => expect((screen.getByLabelText('激活档案 工作') as HTMLInputElement).checked).toBe(true))

    listMock.mockResolvedValue({ success: true, profiles: [DEFAULT_P] })
    fireEvent.click(screen.getByLabelText('删除档案 工作'))
    fireEvent.click(screen.getByLabelText('确认删除档案 工作'))
    await waitFor(() => expect(removeMock).toHaveBeenCalledWith(2))
    // 刷新后默认档案回到激活态
    await waitFor(() => expect((screen.getByLabelText('激活档案 主档案') as HTMLInputElement).checked).toBe(true))
    expect(screen.queryByLabelText('激活档案 工作')).toBeNull()
  })

  it('管理员可见全部用户档案分组', async () => {
    useAuthStore.setState({ user: { id: 1, username: 'admin', role: 'admin' }, token: 't', isAuthenticated: true })
    listMock.mockResolvedValue({
      success: true,
      profiles: [DEFAULT_P],
      admin_all: [
        { user_id: 2, username: 'trader_a', profiles: [{ ...WORK_P, is_active: true }] },
        {
          user_id: 3,
          username: 'trader_b',
          profiles: [{ id: 5, name: '复盘', is_active: false, is_default: true, created_at: 1_700_200_000 }],
        },
      ],
    })
    renderSettings()
    await openProfilesSection()
    await waitFor(() => expect(screen.getByText('全部用户档案')).toBeTruthy())
    expect(screen.getByText('trader_a')).toBeTruthy()
    expect(screen.getByText('trader_b')).toBeTruthy()
    expect(screen.getByText('复盘')).toBeTruthy()
    // trader_a 的激活档案带激活标记
    expect(screen.getByText('激活中')).toBeTruthy()
    // 非默认档案可删除（trader_a 的工作）；默认档案（复盘）不可删
    expect(screen.getByLabelText('删除档案 工作')).toBeTruthy()
    expect(screen.queryByLabelText('删除档案 复盘')).toBeNull()
  })

  it('非管理员不显示全部用户档案（即使接口返回 admin_all）', async () => {
    listMock.mockResolvedValue({
      success: true,
      profiles: [DEFAULT_P],
      admin_all: [{ user_id: 2, username: 'trader_a', profiles: [WORK_P] }],
    })
    renderSettings()
    await openProfilesSection()
    await waitFor(() => expect(screen.getByText('主档案')).toBeTruthy())
    expect(screen.queryByText('全部用户档案')).toBeNull()
    expect(screen.queryByText('trader_a')).toBeNull()
  })
})
