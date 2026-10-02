import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

const listMock = vi.fn()
const createMock = vi.fn()
const toggleMock = vi.fn()
const removeMock = vi.fn()
const runMock = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    agentCronApi: {
      list: (...args: unknown[]) => listMock(...args),
      create: (...args: unknown[]) => createMock(...args),
      toggle: (...args: unknown[]) => toggleMock(...args),
      remove: (...args: unknown[]) => removeMock(...args),
      run: (...args: unknown[]) => runMock(...args),
    },
    agentPluginApi: {
      list: () => Promise.resolve({ plugins: [] }),
    },
  }
})

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

import { CronPanel } from '../CronPanel'

const JOB = {
  id: 'cr_1',
  user_id: 2,
  name: '每日持仓汇报',
  prompt: '汇总持仓',
  schedule: '0 8 * * *',
  timezone: 'Asia/Shanghai',
  channel: 'web',
  enabled: true,
  next_run_at: 1790966400,
  last_run_at: 0,
  last_status: '',
  last_result: '',
  created_at: 0,
  updated_at: 0,
}

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <CronPanel onClose={() => {}} />
    </QueryClientProvider>
  )
}

describe('CronPanel 定时任务面板', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({ jobs: [JOB] })
    createMock.mockResolvedValue({ job: JOB })
    toggleMock.mockResolvedValue({ id: JOB.id, enabled: false })
    removeMock.mockResolvedValue({ deleted: true })
    runMock.mockResolvedValue({ id: JOB.id, started: true })
  })

  it('列出任务并展示表达式与下次执行时间', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('每日持仓汇报')).toBeTruthy())
    expect(screen.getByText('0 8 * * *')).toBeTruthy()
    expect(screen.getByText(/下次/)).toBeTruthy()
  })

  it('新建任务：预设生成 cron 表达式', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('每日持仓汇报')).toBeTruthy())
    fireEvent.click(screen.getByLabelText('新建定时任务'))
    fireEvent.change(screen.getByLabelText('任务名'), { target: { value: '每小时巡检' } })
    fireEvent.change(screen.getByLabelText('任务指令'), { target: { value: '检查机器人状态' } })
    fireEvent.change(screen.getByLabelText('调度预设'), { target: { value: '0 * * * *' } })
    fireEvent.click(screen.getByText('创建'))

    await waitFor(() => expect(createMock).toHaveBeenCalledTimes(1))
    const [body] = createMock.mock.calls[0] as [Record<string, unknown>]
    expect(body).toMatchObject({ name: '每小时巡检', prompt: '检查机器人状态', schedule: '0 * * * *', channel: 'web' })
  })

  it('启停开关调用 toggle', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('每日持仓汇报')).toBeTruthy())
    fireEvent.click(screen.getByLabelText('停用 每日持仓汇报'))
    await waitFor(() => expect(toggleMock).toHaveBeenCalledWith('cr_1', false))
  })

  it('删除任务调用 remove', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('每日持仓汇报')).toBeTruthy())
    fireEvent.click(screen.getByLabelText('删除任务 每日持仓汇报'))
    await waitFor(() => expect(removeMock).toHaveBeenCalledWith('cr_1'))
  })
})
