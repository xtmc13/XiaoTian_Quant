import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

const statusMock = vi.fn()
const pairCodeMock = vi.fn()
const unlinkMock = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    agentFeishuApi: {
      status: (...args: unknown[]) => statusMock(...args),
      pairCode: (...args: unknown[]) => pairCodeMock(...args),
      unlink: (...args: unknown[]) => unlinkMock(...args),
    },
  }
})

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

import { FeishuPanel } from '../FeishuPanel'

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <FeishuPanel onClose={() => {}} />
    </QueryClientProvider>
  )
}

describe('FeishuPanel 飞书接入面板', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('未配置：提示环境变量，状态卡显示未配置/未绑定', async () => {
    statusMock.mockResolvedValue({ success: true, configured: false, linked: false, open_id: '' })
    renderPanel()
    await waitFor(() => expect(screen.getByText(/FEISHU_APP_ID/)).toBeTruthy())
    expect(screen.getByLabelText('飞书 配置状态').textContent).toBe('未配置')
    expect(screen.getByLabelText('飞书 绑定状态').textContent).toBe('未绑定')
    expect(screen.queryByRole('button', { name: /生成配对码/ })).toBeNull()
  })

  it('已配置未绑定：生成配对码并展示倒计时', async () => {
    statusMock.mockResolvedValue({ success: true, configured: true, linked: false, open_id: '' })
    pairCodeMock.mockResolvedValue({ success: true, code: '654321', expires_in: 600 })
    renderPanel()

    await waitFor(() => expect(screen.getByRole('button', { name: /生成配对码/ })).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: /生成配对码/ }))

    await waitFor(() => expect(screen.getByText('654321')).toBeTruthy())
    expect(screen.getByLabelText('配对码倒计时').textContent).toMatch(/^10:0/)
    expect(screen.getByLabelText('复制配对码')).toBeTruthy()
  })

  it('已绑定：显示 open_id，两步确认后解绑', async () => {
    statusMock.mockResolvedValue({ success: true, configured: true, linked: true, open_id: 'ou_abc123' })
    unlinkMock.mockResolvedValue({ success: true })
    renderPanel()

    await waitFor(() => expect(screen.getByText(/ou_abc123/)).toBeTruthy())
    expect(screen.getByLabelText('飞书 绑定状态').textContent).toBe('已绑定')

    fireEvent.click(screen.getByLabelText('解绑 飞书'))
    expect(unlinkMock).not.toHaveBeenCalled()
    fireEvent.click(screen.getByLabelText('确认解绑 飞书'))
    await waitFor(() => expect(unlinkMock).toHaveBeenCalledTimes(1))
  })
})
