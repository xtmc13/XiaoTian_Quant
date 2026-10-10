import { describe, it, expect } from 'vitest'
import { deriveTimeframeFromCRA } from '@/components/strategy/StrategyCreateForm'
import type { CRAParams } from '@/components/strategy/CRAParamForm'
import { createDefaultCRAParams } from '@/lib/strategyUtils'

describe('deriveTimeframeFromCRA：工作周期只跟开仓指标周期', () => {
  it('开仓 1h + 补仓 5m → 工作周期 1h（不被补仓拖低）', () => {
    const p: CRAParams = {
      ...createDefaultCRAParams('contract'),
      openMacdEnabled: true,
      openMacdPeriod: '1h',
      addMacdEnabled: true,
      addMacdPeriod: '5m',
      addEmaEnabled: true,
      addEmaPeriod: '15m',
    }
    expect(deriveTimeframeFromCRA(p)).toBe('1h')
  })

  it('只开补仓指标（开仓全关）→ 不硬编码，返回空（保留用户选择）', () => {
    const p: CRAParams = {
      ...createDefaultCRAParams('contract'),
      addMacdEnabled: true,
      addMacdPeriod: '5m',
    }
    expect(deriveTimeframeFromCRA(p)).toBe('')
  })

  it('多个开仓旧键取最快者；布林周期参与推导', () => {
    const p: CRAParams = {
      ...createDefaultCRAParams('contract'),
      openTrendEmaEnabled: true,
      openTrendEmaPeriod: '4h',
      openBollingerEnabled: true,
      openBollingerPeriod: '30m',
    }
    expect(deriveTimeframeFromCRA(p)).toBe('30m')
  })

  it('开仓周期为 close（关闭=跟随工作周期）不参与推导', () => {
    const p: CRAParams = {
      ...createDefaultCRAParams('contract'),
      openMacdEnabled: true,
      openMacdPeriod: 'close',
      addMacdEnabled: true,
      addMacdPeriod: '30m',
    }
    expect(deriveTimeframeFromCRA(p)).toBe('')
  })
})
