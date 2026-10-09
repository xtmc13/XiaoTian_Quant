import { describe, it, expect } from 'vitest'
import { STRAT_TYPES } from '../StrategyFormFields'

// 创建入口类型清单治理记录：
// - 2026-10-04 下线 顺势多/顺势空/逆势稳健/逆势保守（用户决策，引擎别名冗余）；
// - 2026-10-09 加回经典指标策略 EMA/MACD/RSI/布林带（用户决策，后端工厂与
//   paramDefs 均真实具备：ema_cross/macd/rsi/bollinger_bands）。
// 存量记录与后端工厂映射保留兼容（mapCRAFactory），清单只管创建入口。
describe('STRAT_TYPES 创建入口', () => {
  it('合约类型不再提供顺势多/顺势空/逆势稳健/逆势保守', () => {
    const values = STRAT_TYPES.contract.map((t) => t.value)
    expect(values).not.toContain('trend_long')
    expect(values).not.toContain('trend_short')
    expect(values).not.toContain('counter_stable')
    expect(values).not.toContain('counter_safe')
  })

  it('合约创建入口：CRA 主类型 + 经典指标策略（EMA/MACD/RSI/布林带）', () => {
    const values = STRAT_TYPES.contract.map((t) => t.value)
    // 后端落点：cra_contract/smart_money 直接注册；high_frequency(合约语境)/head_tail_arbitrage 经 mapCRAFactory → cra_contract
    for (const v of ['cra_contract', 'high_frequency', 'head_tail_arbitrage', 'smart_money']) {
      expect(values).toContain(v)
    }
    for (const v of ['ema_cross', 'macd', 'rsi', 'bollinger_bands']) {
      expect(values).toContain(v)
    }
  })

  it('现货创建入口同样含经典指标策略', () => {
    const values = STRAT_TYPES.spot.map((t) => t.value)
    for (const v of ['ema_cross', 'macd', 'rsi', 'bollinger_bands']) {
      expect(values).toContain(v)
    }
  })
})
