// ── 斜杠命令目录 + 技能调色板（客户端本地；技能列表来自后端 skills 插件） ──
export interface SlashCommand {
  name: string
  /** 无参命令在 Enter 时直接执行 */
  args?: string
  description: string
  icon: 'new' | 'stop' | 'retry' | 'model' | 'clear' | 'clock' | 'brain' | 'zap' | 'send' | 'gauge'
}

export const SLASH_COMMANDS: SlashCommand[] = [
  { name: 'new', description: '开始新对话（清空当前会话）', icon: 'new' },
  { name: 'stop', description: '停止当前正在生成的回复', icon: 'stop' },
  { name: 'retry', description: '重新生成上一条助手回复', icon: 'retry' },
  { name: 'model', description: '切换模型厂商与型号', icon: 'model' },
  { name: 'usage', description: '用量与概览', icon: 'gauge' },
  { name: 'cron', description: '打开定时任务面板', icon: 'clock' },
  { name: 'memory', description: '打开记忆面板', icon: 'brain' },
  { name: 'skills', description: '打开技能面板', icon: 'zap' },
  { name: 'telegram', description: '打开 Telegram 接入面板', icon: 'send' },
  { name: 'clear', description: '清空当前对话（同 /new）', icon: 'clear' },
]

/** SkillItem 技能（调色板用），来自 /api/agent/skills */
export interface SkillItem {
  name: string
  description: string
  body: string
}

/** PaletteItem 调色板行：内置命令或用户技能 */
export interface PaletteItem {
  kind: 'command' | 'skill'
  name: string
  description: string
  icon: SlashCommand['icon']
  command?: SlashCommand
  skill?: SkillItem
}

/** 输入以 / 开头时返回过滤后的命令+技能候选；否则返回 null（面板关闭） */
export function completeSlash(input: string): SlashCommand[] | null {
  if (!input.startsWith('/')) return null
  const query = input.slice(1).split(/\s/)[0].toLowerCase()
  if (input.slice(1).includes(' ')) return [] // 已进入参数阶段，不再补全
  if (!query) return SLASH_COMMANDS
  return SLASH_COMMANDS.filter((c) => c.name.startsWith(query))
}

/** completePalette 命令 + 技能合并调色板（技能按名前缀匹配，排在命令后） */
export function completePalette(input: string, skills: SkillItem[]): PaletteItem[] | null {
  if (!input.startsWith('/')) return null
  const afterSlash = input.slice(1)
  if (afterSlash.includes(' ')) return []
  const query = afterSlash.toLowerCase()
  const items: PaletteItem[] = []
  for (const c of SLASH_COMMANDS) {
    if (!query || c.name.startsWith(query)) {
      items.push({ kind: 'command', name: c.name, description: c.description, icon: c.icon, command: c })
    }
  }
  for (const s of skills) {
    if (!query || s.name.toLowerCase().startsWith(query)) {
      items.push({ kind: 'skill', name: s.name, description: s.description || '（无说明）', icon: 'zap', skill: s })
    }
  }
  return items
}

/** 输入是否精确等于某条无参命令（隐式接受：Enter 直接执行）；技能不参与 */
export function exactMatch(input: string): SlashCommand | null {
  const body = input.trim()
  if (body.includes(' ')) return null
  const name = body.startsWith('/') ? body.slice(1).toLowerCase() : ''
  if (!name) return null
  return SLASH_COMMANDS.find((c) => c.name === name) || null
}
