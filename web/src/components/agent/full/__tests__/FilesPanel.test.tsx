import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

const checkpointsMock = vi.fn()
const rollbackMock = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    agentFilesApi: {
      checkpoints: (...args: unknown[]) => checkpointsMock(...args),
      rollback: (...args: unknown[]) => rollbackMock(...args),
    },
  }
})

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

import { FilesPanel } from '../FilesPanel'
import { useAuthStore } from '@/stores/authStore'
import { toast } from '@/lib/useToast'

const CP1 = {
  id: 'ck_1',
  path: 'user_data/strategies/grid.py',
  size: 2048,
  conversation_id: 'conv_abcdef123456',
  created_at: Math.floor(Date.now() / 1000) - 300,
}
const CP2 = {
  id: 'ck_2',
  path: 'config.json',
  size: 512,
  conversation_id: 'conv_xyz789',
  created_at: Math.floor(Date.now() / 1000) - 7200,
}

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <FilesPanel onClose={() => {}} />
    </QueryClientProvider>
  )
}

describe('FilesPanel 文件回滚面板', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    checkpointsMock.mockResolvedValue({ success: true, checkpoints: [CP1, CP2] })
    rollbackMock.mockResolvedValue({ success: true, restored: CP1.path })
  })

  it('管理员：列出检查点（路径 / 大小 / 会话短 id），两步确认后回滚并提示', async () => {
    useAuthStore.setState({ user: { id: 1, username: 'admin', role: 'admin' }, token: 't', isAuthenticated: true })
    renderPanel()
    await waitFor(() => expect(screen.getByText('user_data/strategies/grid.py')).toBeTruthy())
    expect(screen.getByText('config.json')).toBeTruthy()
    expect(screen.getByText('2.0 KB')).toBeTruthy()
    expect(screen.getByText('512 B')).toBeTruthy()
    expect(screen.getByText('会话 conv_abc')).toBeTruthy()

    // 两步确认：先点「恢复」，再点「确认恢复」
    fireEvent.click(screen.getByLabelText('恢复 user_data/strategies/grid.py'))
    expect(rollbackMock).not.toHaveBeenCalled()
    fireEvent.click(screen.getByLabelText('确认恢复 user_data/strategies/grid.py'))
    await waitFor(() => expect(rollbackMock).toHaveBeenCalledWith('ck_1'))
    await waitFor(() => expect(toast).toHaveBeenCalledWith('success', '已恢复 user_data/strategies/grid.py'))
  })

  it('管理员：空检查点显示空态', async () => {
    useAuthStore.setState({ user: { id: 1, username: 'admin', role: 'admin' }, token: 't', isAuthenticated: true })
    checkpointsMock.mockResolvedValue({ success: true, checkpoints: [] })
    renderPanel()
    await waitFor(() => expect(screen.getByText(/暂无检查点/)).toBeTruthy())
  })

  it('非管理员：显示无权限提示且不请求接口', async () => {
    useAuthStore.setState({ user: { id: 2, username: 'user', role: 'user' }, token: 't', isAuthenticated: true })
    renderPanel()
    expect(screen.getByText(/无权限/)).toBeTruthy()
    expect(checkpointsMock).not.toHaveBeenCalled()
  })
})
