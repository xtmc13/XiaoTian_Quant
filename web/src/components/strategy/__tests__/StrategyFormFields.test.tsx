import { describe, it, expect } from 'vitest'
import { STRAT_TYPES } from '../StrategyFormFields'

// 创建入口下线"顺势多/顺势空/逆势稳健/逆势保守"（2026-10-04 用户决策）：
// 对齐币富 CRA 模型——顺势/逆势由指标选择器的 EMA 承担，不再是独立策略类型。
// 存量记录与后端工厂映射保留兼容（mapCRAFactory），仅封创建入口。
describe('STRAT_TYPES 创建入口', () => {
  it('合约类型不再提供顺势多/顺势空/逆势稳健/逆势保守', () => {
    const values = STRAT_TYPES.contract.map((t) => t.value)
    expect(values).not.toContain('trend_long')
    expect(values).not.toContain('trend_short')
    expect(values).not.toContain('counter_stable')
    expect(values).not.toContain('counter_safe')
  })

  it('合约创建入口保留四个有真实工厂支撑的类型', () => {
    const values = STRAT_TYPES.contract.map((t) => t.value)
    // 后端落点：cra_contract/smart_money 直接注册；high_frequency(合约语境)/head_tail_arbitrage 经 mapCRAFactory → cra_contract
    expect(values).toEqual(['cra_contract', 'high_frequency', 'head_tail_arbitrage', 'smart_money'])
  })
})
