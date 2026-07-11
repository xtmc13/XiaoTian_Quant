import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrategyCreatePanel } from '../StrategyCreatePanel'
import * as useStrategyDataModule from '@/hooks/useStrategyData'
import * as useToastModule from '@/lib/useToast'
import { configApi, strategyApi } from '@/lib/api'
import type { ExchangeConfiguredStatus } from '@/types'

const mockConfigured: Record<string, ExchangeConfiguredStatus> = {
  binance: { enabled: true, has_credentials: true, testnet: false, futures: true },
  okx: { enabled: true, has_credentials: true, testnet: false, futures: true },
}

vi.mock('@/hooks/useStrategyData', () => ({
  useStrategyData: vi.fn(),
}))

vi.mock('@/lib/useToast', async () => {
  const actual = await vi.importActual<typeof import('@/lib/useToast')>('@/lib/useToast')
  return {
    ...actual,
    toast: vi.fn(),
  }
})

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    configApi: {
      exchangesConfigured: vi.fn(),
    },
    strategyApi: {
      ...(actual.strategyApi || {}),
      createTemplate: vi.fn(),
    },
  }
})

function wrapper({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}

describe('StrategyCreatePanel', () => {
  const create = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(useStrategyDataModule.useStrategyData).mockReturnValue({
      create,
    } as unknown as ReturnType<typeof useStrategyDataModule.useStrategyData>)
    vi.mocked(configApi.exchangesConfigured).mockResolvedValue(mockConfigured)
  })

  it('renders basic fields and exchange selector', () => {
    render(<StrategyCreatePanel strategyType="martin_trend" onClose={vi.fn()} onSaved={vi.fn()} />, { wrapper })
    expect(screen.getByPlaceholderText('输入策略名称')).toBeTruthy()
    expect(screen.getByText('点击选择交易所')).toBeTruthy()
  })

  it('opens exchange modal and selects an exchange', async () => {
    render(<StrategyCreatePanel strategyType="martin_trend" onClose={vi.fn()} onSaved={vi.fn()} />, { wrapper })
    fireEvent.click(screen.getByText('点击选择交易所'))
    await waitFor(() => expect(screen.getByText('Binance')).toBeTruthy())
    fireEvent.click(screen.getByText('Binance'))
    fireEvent.click(screen.getByText('确认选择'))
    expect(screen.getByText('已选择 1 个交易所')).toBeTruthy()
  })

  it('shows validation error when no exchange is selected', async () => {
    render(<StrategyCreatePanel strategyType="martin_trend" onClose={vi.fn()} onSaved={vi.fn()} />, { wrapper })
    fireEvent.change(screen.getByPlaceholderText('输入策略名称'), { target: { value: 'Test' } })
    fireEvent.click(screen.getByText('保存策略'))
    await waitFor(() => expect(useToastModule.toast).toHaveBeenCalledWith('error', '请至少选择一个交易所'))
    expect(create).not.toHaveBeenCalled()
  })

  it('submits payload with selected exchanges', async () => {
    const onSaved = vi.fn()
    render(<StrategyCreatePanel strategyType="martin_trend" onClose={vi.fn()} onSaved={onSaved} />, { wrapper })

    fireEvent.change(screen.getByPlaceholderText('输入策略名称'), { target: { value: 'Test Strategy' } })
    fireEvent.click(screen.getByText('点击选择交易所'))
    await waitFor(() => expect(screen.getByText('Binance')).toBeTruthy())
    fireEvent.click(screen.getByText('Binance'))
    fireEvent.click(screen.getByText('确认选择'))

    fireEvent.click(screen.getByText('保存策略'))
    await waitFor(() => expect(create).toHaveBeenCalled())

    const payload = create.mock.calls[0][0] as Record<string, unknown>
    const config = JSON.parse(payload.config_json as string) as Record<string, unknown>
    expect(config.selected_exchanges).toEqual(['binance'])
  })

  it('saves as default template when checkbox is checked', async () => {
    vi.mocked(strategyApi.createTemplate).mockResolvedValue({ id: 'tpl-1' } as unknown as never)
    const onSaved = vi.fn()
    render(<StrategyCreatePanel strategyType="martin_trend" onClose={vi.fn()} onSaved={onSaved} />, { wrapper })

    fireEvent.change(screen.getByPlaceholderText('输入策略名称'), { target: { value: 'Test Strategy' } })
    fireEvent.click(screen.getByText('点击选择交易所'))
    await waitFor(() => expect(screen.getByText('Binance')).toBeTruthy())
    fireEvent.click(screen.getByText('Binance'))
    fireEvent.click(screen.getByText('确认选择'))

    fireEvent.click(screen.getByText('保存为默认策略模板'))
    fireEvent.click(screen.getByText('保存策略'))
    await waitFor(() => expect(create).toHaveBeenCalled())
    await waitFor(() => expect(strategyApi.createTemplate).toHaveBeenCalled())

    const templatePayload = (strategyApi.createTemplate as ReturnType<typeof vi.fn>).mock.calls[0][0]
    expect(templatePayload.name).toBe('Test Strategy')
    expect(templatePayload.category).toBe('spot')
    expect(templatePayload.default_config.strategy_type).toBe('martin_trend')
  })
})
