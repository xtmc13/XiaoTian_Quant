import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

const listMock = vi.fn()
const createMock = vi.fn()
const removeMock = vi.fn()
const profilesListMock = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    agentMemoryApi: {
      list: (...args: unknown[]) => listMock(...args),
      create: (...args: unknown[]) => createMock(...args),
      remove: (...args: unknown[]) => removeMock(...args),
    },
    agentProfilesApi: {
      list: (...args: unknown[]) => profilesListMock(...args),
      create: vi.fn(),
      activate: vi.fn(),
      remove: vi.fn(),
    },
  }
})

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

import { MemoryPanel } from '../MemoryPanel'

const MEM = {
  id: 'mem_1',
  user_id: 2,
  scope: 'user',
  kind: 'preference',
  content: '偏好低杠杆短线',
  source_conversation_id: '',
  importance: 4,
  created_at: 0,
  updated_at: 0,
}

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryPanel onClose={() => {}} />
    </QueryClientProvider>
  )
}

describe('MemoryPanel 记忆面板', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({ memories: [MEM] })
    createMock.mockResolvedValue({ memory: MEM })
    removeMock.mockResolvedValue({ deleted: true })
    profilesListMock.mockResolvedValue({ success: true, profiles: [] })
  })

  it('头部展示当前激活档案 chip', async () => {
    profilesListMock.mockResolvedValue({
      success: true,
      profiles: [
        { id: 1, name: '默认', is_active: false, is_default: true, created_at: 0 },
        { id: 2, name: '工作', is_active: true, is_default: false, created_at: 0 },
      ],
    })
    renderPanel()
    await waitFor(() => expect(screen.getByLabelText('当前档案').textContent).toContain('档案:工作'))
  })

  it('无激活档案时不渲染 chip', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('偏好低杠杆短线')).toBeTruthy())
    expect(screen.queryByLabelText('当前档案')).toBeNull()
  })

  it('列出记忆并展示类型标签', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('偏好低杠杆短线')).toBeTruthy())
    // 「偏好」同时出现在类型过滤 chip 和记忆标签中
    expect(screen.getAllByText('偏好').length).toBeGreaterThanOrEqual(2)
  })

  it('搜索过滤记忆', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('偏好低杠杆短线')).toBeTruthy())
    fireEvent.change(screen.getByLabelText('搜索记忆'), { target: { value: '不存在的关键词' } })
    expect(screen.queryByText('偏好低杠杆短线')).toBeNull()
  })

  it('手动新增记忆', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('偏好低杠杆短线')).toBeTruthy())
    fireEvent.click(screen.getByLabelText('新增记忆'))
    fireEvent.change(screen.getByLabelText('记忆内容'), { target: { value: '止损纪律严格' } })
    fireEvent.change(screen.getByLabelText('记忆类型'), { target: { value: 'preference' } })
    fireEvent.change(screen.getByLabelText('重要度'), { target: { value: '5' } })
    fireEvent.click(screen.getByText('保存'))

    await waitFor(() => expect(createMock).toHaveBeenCalledTimes(1))
    const [body] = createMock.mock.calls[0] as [Record<string, unknown>]
    expect(body).toEqual({ content: '止损纪律严格', kind: 'preference', importance: 5 })
  })

  it('删除记忆调用 remove', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('偏好低杠杆短线')).toBeTruthy())
    fireEvent.click(screen.getByLabelText('删除记忆 偏好低杠杆短线'))
    await waitFor(() => expect(removeMock).toHaveBeenCalledWith('mem_1'))
  })
})
