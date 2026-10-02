import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

const statusMock = vi.fn()
const pairCodeMock = vi.fn()
const unlinkMock = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    agentDingtalkApi: {
      status: (...args: unknown[]) => statusMock(...args),
      pairCode: (...args: unknown[]) => pairCodeMock(...args),
      unlink: (...args: unknown[]) => unlinkMock(...args),
    },
  }
})

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

import { DingtalkPanel } from '../DingtalkPanel'

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <DingtalkPanel onClose={() => {}} />
    </QueryClientProvider>
  )
}

describe('DingtalkPanel 钉钉接入面板', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('已绑定（staff_id）：显示绑定状态与账号标识', async () => {
    statusMock.mockResolvedValue({ success: true, configured: true, linked: true, staff_id: 'staff_42' })
    renderPanel()
    await waitFor(() => expect(screen.getByText(/staff_42/)).toBeTruthy())
    expect(screen.getByLabelText('钉钉 绑定状态').textContent).toBe('已绑定')
  })

  it('已绑定（open_id 回退）：staff_id 缺失时读 open_id', async () => {
    statusMock.mockResolvedValue({ success: true, configured: true, linked: true, open_id: 'ou_ding_9' })
    renderPanel()
    await waitFor(() => expect(screen.getByText(/ou_ding_9/)).toBeTruthy())
  })

  it('未配置：提示钉钉环境变量', async () => {
    statusMock.mockResolvedValue({ success: true, configured: false, linked: false })
    renderPanel()
    await waitFor(() => expect(screen.getByText(/DINGTALK_CLIENT_ID/)).toBeTruthy())
    expect(screen.getByLabelText('钉钉 配置状态').textContent).toBe('未配置')
  })
})
