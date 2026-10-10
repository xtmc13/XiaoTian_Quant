import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { StrategyDetailPanel } from '../StrategyDetailPanel'
import type { StrategyItem } from '@/types'

// 实时运行面板（5s 轮询 + i18n）不在本测试范围
vi.mock('@/components/strategy/RuntimePanel', () => ({ RuntimePanel: () => null }))

const base = {
  id: 's1',
  name: '测试策略',
  symbol: 'BTCUSDT',
  timeframe: '15m',
  status: 'stopped',
  market_type: 'swap',
  total_pnl: 0,
  config_json: '{}',
} as unknown as StrategyItem

function renderPanel(strategy: StrategyItem) {
  return render(
    <StrategyDetailPanel strategy={strategy} onStart={vi.fn()} onStop={vi.fn()} onEdit={vi.fn()} onDelete={vi.fn()} />
  )
}

describe('StrategyDetailPanel 智能补仓/移动止盈参数区（用户可见命名）', () => {
  it('壳策略（cra_contract）→ 参数区标题为「智能补仓/移动止盈参数」', () => {
    renderPanel({ ...base, strategy_type: 'cra_contract' } as StrategyItem)
    expect(screen.getByText('智能补仓/移动止盈参数')).toBeTruthy()
  })

  it('指标/脚本策略（macd）→ 如实提示不支持智能补仓/移动止盈参数', () => {
    renderPanel({ ...base, strategy_type: 'macd' } as StrategyItem)
    expect(screen.getByText(/不支持智能补仓\/移动止盈参数/)).toBeTruthy()
  })
})
