import { createDefaultAddPositions } from '@/lib/strategyUtils'
import type { CRAParams } from './CRAParamForm'

export type PresetKey = 'conservative' | 'balanced' | 'aggressive'

export interface Preset {
  key: PresetKey
  label: string
  desc: string
  color: string
  params: (market: 'spot' | 'contract') => Partial<CRAParams>
}

export const STRATEGY_PRESETS: Preset[] = [
  {
    key: 'conservative',
    label: '保守型',
    desc: '低风险，小仓位分批入场，严格风控',
    color: 'text-emerald-400 border-emerald-500/30 bg-emerald-500/5',
    params: (market) => ({
      orderCount: 5,
      firstOrderAmount: 50,
      enableAddPosition: true,
      addPositions: createDefaultAddPositions(
        market === 'contract' ? 'contract_martin' : 'martin',
        5,
        market === 'contract' ? undefined : 5,
        0.3
      ),
      tpRatio: 1.5,
      profitCallback: 0.2,
      waterfall: 1.5,
      onlineOrderLimit: 5,
      stopLossRatio: 5,
    }),
  },
  {
    key: 'balanced',
    label: '平衡型',
    desc: '适中风险，标准参数配置',
    color: 'text-amber-400 border-amber-500/30 bg-amber-500/5',
    params: (market) => ({
      orderCount: 7,
      firstOrderAmount: 100,
      enableAddPosition: true,
      addPositions: createDefaultAddPositions(
        market === 'contract' ? 'contract_martin' : 'martin',
        7,
        market === 'contract' ? undefined : 3,
        0.3
      ),
      tpRatio: 1.3,
      profitCallback: 0.1,
      waterfall: 2,
      onlineOrderLimit: 10,
    }),
  },
  {
    key: 'aggressive',
    label: '激进型',
    desc: '高收益高回撤，适合趋势行情',
    color: 'text-red-400 border-red-500/30 bg-red-500/5',
    params: (market) => ({
      orderCount: 10,
      firstOrderAmount: 200,
      enableAddPosition: true,
      addPositions: createDefaultAddPositions(
        market === 'contract' ? 'contract_martin' : 'martin',
        10,
        market === 'contract' ? undefined : 1.5,
        0.3
      ),
      tpRatio: 2.0,
      profitCallback: 0.05,
      waterfall: 4,
      onlineOrderLimit: 20,
      openDouble: true,
      followTrend: true,
    }),
  },
]
