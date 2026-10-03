// 提示词指令库：用户可增删的常用指令，localStorage 持久化（仅本机浏览器）。
export interface PromptItem {
  id: string
  label: string
  text: string
}

const STORAGE_KEY = 'xt-agent-prompt-lib-v1'

export const PROMPT_DEFAULTS: PromptItem[] = [
  { id: 'default-1', label: '复盘今日交易', text: '帮我复盘今天的交易情况，找出问题和改进点。' },
  { id: 'default-2', label: '分析行情走势', text: '分析一下当前大盘走势，给出关键位和操作建议。' },
  { id: 'default-3', label: '检查机器人状态', text: '检查一下我所有运行中机器人的状态和盈亏。' },
]

export function loadPromptLib(): PromptItem[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return PROMPT_DEFAULTS
    const list = JSON.parse(raw) as PromptItem[]
    if (!Array.isArray(list)) return PROMPT_DEFAULTS
    return list
      .filter((p) => p && typeof p.label === 'string' && typeof p.text === 'string')
      .map((p) => ({ id: typeof p.id === 'string' ? p.id : `${Date.now()}-${Math.random()}`, label: p.label, text: p.text }))
  } catch {
    return PROMPT_DEFAULTS
  }
}

export function savePromptLib(list: PromptItem[]): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(list))
  } catch {
    // 存储满/隐私模式：静默失败，本次会话内仍可用
  }
}
