import { describe, it, expect } from 'vitest'
import { STRAT_TYPES } from '../StrategyFormFields'

// 创建入口下线"顺势多/顺势空"（trend_long/trend_short）：对齐币富 CRA 模型——
// 顺势/逆势由指标选择器的 EMA 承担，不再是独立策略类型。存量记录与后端工厂映射
// 保留兼容（mapCRAFactory），仅封创建入口。
describe('STRAT_TYPES 创建入口', () => {
  it('合约类型不再提供顺势多/顺势空', () => {
    const values = STRAT_TYPES.contract.map((t) => t.value)
    expect(values).not.toContain('trend_long')
    expect(values).not.toContain('trend_short')
  })

  it('合约创建入口保留 CRA 主类型与逆势/高频等', () => {
    const values = STRAT_TYPES.contract.map((t) => t.value)
    expect(values).toContain('cra_contract')
    expect(values).toContain('counter_stable')
  })
})
