import { describe, it, expect } from 'vitest'
import { completePalette, completeSlash, exactMatch, SLASH_COMMANDS, type SkillItem } from '../slash'

const SKILLS: SkillItem[] = [
  { name: '每日复盘', description: '收盘复盘流程', body: '1. 看持仓…' },
  { name: '巡检', description: '', body: '检查机器人…' },
]

describe('斜杠命令补全', () => {
  it('非 / 开头返回 null（面板关闭）', () => {
    expect(completeSlash('你好')).toBeNull()
    expect(completeSlash(' /new')).toBeNull()
  })

  it('仅 "/" 返回全部命令', () => {
    expect(completeSlash('/')).toEqual(SLASH_COMMANDS)
  })

  it('按前缀过滤', () => {
    const names = (completeSlash('/ne') || []).map((c) => c.name)
    expect(names).toEqual(['new'])
    expect(completeSlash('/x')).toEqual([])
  })

  it('进入参数阶段后不再补全', () => {
    expect(completeSlash('/new xxx')).toEqual([])
  })

  it('exactMatch 只匹配无参精确命令', () => {
    expect(exactMatch('/new')?.name).toBe('new')
    expect(exactMatch('/stop')).not.toBeNull()
    expect(exactMatch('/new arg')).toBeNull()
    expect(exactMatch('new')).toBeNull()
    expect(exactMatch('/unknown')).toBeNull()
  })
})

describe('命令+技能合并调色板', () => {
  it('/ 返回命令在前、技能在后', () => {
    const items = completePalette('/', SKILLS)!
    expect(items.length).toBe(SLASH_COMMANDS.length + SKILLS.length)
    expect(items[0].kind).toBe('command')
    expect(items[SLASH_COMMANDS.length].kind).toBe('skill')
  })

  it('技能按名前缀匹配（中文名）', () => {
    const items = completePalette('/每日', SKILLS)!
    expect(items.length).toBe(1)
    expect(items[0].kind).toBe('skill')
    expect(items[0].name).toBe('每日复盘')
    expect(items[0].skill?.body).toContain('看持仓')
  })

  it('命令优先，技能跟随', () => {
    const items = completePalette('/s', SKILLS)!
    const kinds = items.map((i) => `${i.kind}:${i.name}`)
    expect(kinds).toContain('command:stop')
    expect(kinds).toContain('command:skills')
    // 中文技能名按中文前缀匹配
    const zh = completePalette('/巡', SKILLS)!
    expect(zh.map((i) => `${i.kind}:${i.name}`)).toContain('skill:巡检')
  })

  it('进入参数阶段返回空', () => {
    expect(completePalette('/每日复盘 现在', SKILLS)).toEqual([])
  })
})
