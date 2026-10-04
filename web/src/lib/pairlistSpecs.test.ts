import { describe, it, expect } from 'vitest'
import { mergeTemplatesWithSpecs, type SpecTemplate } from './pairlistSpecs'
import type { PairlistComponentSpec } from '@/lib/api'

const localTemplates: SpecTemplate[] = [
  {
    name: 'StaticPairList',
    label: '静态交易对列表',
    description: '手动指定交易对',
    defaultParams: { pairs: ['BTCUSDT'] },
    fields: [{ key: 'pairs', label: '交易对', type: 'tags' }],
  },
  {
    name: 'AgeFilter',
    label: '上市时间过滤',
    description: '过滤上市时间太短的交易对',
    defaultParams: { min_age_days: 7 },
    fields: [{ key: 'min_age_days', label: '最小上市天数', type: 'number' }],
  },
]

describe('mergeTemplatesWithSpecs', () => {
  it('规格为空时原样返回本地清单', () => {
    const { templates, backendNames } = mergeTemplatesWithSpecs(localTemplates, null)
    expect(templates).toBe(localTemplates)
    expect(backendNames.size).toBe(0)
  })

  it('规格与本地交集保留本地模板并标记 backend；规格独有生成通用模板', () => {
    const specs: PairlistComponentSpec[] = [
      {
        name: 'StaticPairList',
        label: '静态名单',
        description: '手动指定交易对',
        params: [{ key: 'pairs', label: '交易对', type: 'tags' }],
      },
      {
        name: 'VolumePairList',
        label: '成交量排行',
        description: '按 24h 成交量取前 N',
        params: [
          { key: 'top_n', label: '前 N 名', type: 'number', default: 30, min: 1, max: 500 },
          { key: 'sort', label: '排序', type: 'select', options: ['asc', 'desc'], default: 'desc' },
        ],
      },
    ]
    const { templates, backendNames } = mergeTemplatesWithSpecs(localTemplates, specs)

    // 本地两个都保留（AgeFilter 虽不在规格中但工厂支持，不裁剪）
    expect(templates.map((t) => t.name)).toEqual(['StaticPairList', 'AgeFilter', 'VolumePairList'])
    // 交集保留本地（label 用本地版本）
    expect(templates[0].label).toBe('静态交易对列表')
    // 规格独有：由参数生成默认值与字段（select 带 options）
    const gen = templates[2]
    expect(gen.defaultParams).toEqual({ top_n: 30, sort: 'desc' })
    expect(gen.fields[0]).toMatchObject({ key: 'top_n', type: 'number', min: 1, max: 500 })
    expect(gen.fields[1]).toMatchObject({ key: 'sort', type: 'select', options: [{ value: 'asc', label: 'asc' }, { value: 'desc', label: 'desc' }] })
    // backend 标记只含规格内名字
    expect(backendNames.has('StaticPairList')).toBe(true)
    expect(backendNames.has('VolumePairList')).toBe(true)
    expect(backendNames.has('AgeFilter')).toBe(false)
  })
})
