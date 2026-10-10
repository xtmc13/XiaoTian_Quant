import { describe, it, expect } from 'vitest'
import { STRAT_TYPES, INDICATOR_STRAT_SHORTCUTS } from '../StrategyFormFields'
import { OPEN_INDICATORS } from '../indicatorPresets'

// 创建入口类型清单治理记录：
// - 2026-10-04 下线 顺势多/顺势空/逆势稳健/逆势保守（用户决策，引擎别名冗余）；
// - 2026-10-09 纠偏：指标策略走币富模型——CRA 壳 + 预选开仓指标
//   （INDICATOR_STRAT_SHORTCUTS 快捷卡跳 /create?type=cra_*&indicator=xx），
//   不是裸经典策略类型（ema_cross/macd/rsi/bollinger_bands 无 CRA 壳，
//   创建后表单提示"不支持 CRA 补仓/移动止盈参数"，与用户初衷相悖，已撤下）。
// 存量记录与后端工厂映射保留兼容（mapCRAFactory），清单只管创建入口。
describe('STRAT_TYPES 创建入口', () => {
  it('合约类型不再提供顺势多/顺势空/逆势稳健/逆势保守', () => {
    const values = STRAT_TYPES.contract.map((t) => t.value)
    expect(values).not.toContain('trend_long')
    expect(values).not.toContain('trend_short')
    expect(values).not.toContain('counter_stable')
    expect(values).not.toContain('counter_safe')
  })

  it('合约创建入口只剩四个有真实工厂支撑的类型', () => {
    const values = STRAT_TYPES.contract.map((t) => t.value)
    // 后端落点：cra_contract/smart_money 直接注册；high_frequency(合约语境)/head_tail_arbitrage 经 mapCRAFactory → cra_contract
    expect(values).toEqual(['cra_contract', 'high_frequency', 'head_tail_arbitrage', 'smart_money'])
  })

  it('裸经典指标类型不在创建入口（无 CRA 壳，2026-10-09 纠偏撤下）', () => {
    for (const market of ['spot', 'contract'] as const) {
      const values = STRAT_TYPES[market].map((t) => t.value)
      for (const bare of ['ema_cross', 'macd', 'rsi', 'bollinger_bands']) {
        expect(values).not.toContain(bare)
      }
    }
  })
})

describe('INDICATOR_STRAT_SHORTCUTS 指标策略快捷卡（CRA 壳 + 预选开仓指标）', () => {
  it('四张快捷卡：EMA/MACD/RSI/布林带，indicator 均为选择器合法 key', () => {
    expect(INDICATOR_STRAT_SHORTCUTS.map((t) => t.label)).toEqual([
      'EMA 策略',
      'MACD 策略',
      'RSI 策略',
      '布林带策略',
    ])
    const pickerKeys = OPEN_INDICATORS.map((d) => d.key as string)
    expect(INDICATOR_STRAT_SHORTCUTS.map((t) => t.indicator)).toEqual(['ema_cross', 'macd', 'rsi', 'bollinger'])
    for (const t of INDICATOR_STRAT_SHORTCUTS) {
      // 快捷卡 indicator 必须是开仓指标选择器真实具备的选项（否则预选静默失效）
      expect(pickerKeys).toContain(t.indicator)
    }
    // value 互不相同（React key / 测试锚点）
    expect(new Set(INDICATOR_STRAT_SHORTCUTS.map((t) => t.value)).size).toBe(INDICATOR_STRAT_SHORTCUTS.length)
  })
})
