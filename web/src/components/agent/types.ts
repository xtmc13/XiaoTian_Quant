import type { AgentToolCall } from '@/lib/api'

// ── 面板内消息模型（在会话消息基础上补充流式状态） ──
export interface AgentChatMsg {
  id?: string
  role: 'user' | 'assistant' | 'system'
  content: string
  /** 思考过程（reasoning），流式追加 */
  reasoning?: string
  toolCalls?: AgentToolCall[]
  streaming?: boolean
  error?: boolean
}

// ── 本地设置（xt-agent-settings） ──
export interface AgentSettings {
  system_prompt: string
  temperature: number
  /** "provider" 或 "provider:model"；空串 = 默认（不指定 model） */
  model: string
}

export const SETTINGS_KEY = 'xt-agent-settings'

export function loadSettings(): AgentSettings {
  const fallback: AgentSettings = { system_prompt: '', temperature: 0.7, model: '' }
  try {
    const raw = localStorage.getItem(SETTINGS_KEY)
    if (!raw) return fallback
    const parsed = JSON.parse(raw)
    return {
      system_prompt: typeof parsed.system_prompt === 'string' ? parsed.system_prompt : '',
      temperature: typeof parsed.temperature === 'number' ? parsed.temperature : 0.7,
      model: typeof parsed.model === 'string' ? parsed.model : '',
    }
  } catch {
    return fallback
  }
}

export function saveSettings(settings: AgentSettings) {
  try {
    localStorage.setItem(SETTINGS_KEY, JSON.stringify(settings))
  } catch {
    /* 存储满等情况静默失败 */
  }
}

// ── 工具调用中文标签 ──
export const TOOL_LABELS: Record<string, string> = {
  get_market: '查询行情',
  get_market_data: '查询行情',
  get_ticker: '查询行情',
  get_klines: '查询K线',
  get_portfolio: '查询持仓',
  get_positions: '查询持仓',
  get_pnl: '查询盈亏',
  get_signals: '查询AI信号',
  create_grid_bot: '创建网格机器人',
  create_bot: '创建机器人',
  list_bots: '查询机器人列表',
  // cron 插件
  create_scheduled_job: '创建定时任务',
  list_scheduled_jobs: '列出定时任务',
  toggle_scheduled_job: '启停定时任务',
  delete_scheduled_job: '删除定时任务',
  run_scheduled_job: '立即运行任务',
  // memory 插件
  save_memory: '保存记忆',
  search_memory: '检索记忆',
  list_memories: '列出记忆',
  delete_memory: '删除记忆',
  // skills 插件
  save_skill: '保存技能',
  run_skill: '执行技能',
  list_skills: '列出技能',
  delete_skill: '删除技能',
  // subagents 插件
  delegate_task: '派发子任务',
}

export function toolLabel(name: string): string {
  return TOOL_LABELS[name] || name
}

// ── 剪贴板写入（带降级） ──
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    try {
      const el = document.createElement('textarea')
      el.value = text
      el.style.position = 'fixed'
      el.style.opacity = '0'
      document.body.appendChild(el)
      el.select()
      document.execCommand('copy')
      document.body.removeChild(el)
      return true
    } catch {
      return false
    }
  }
}

// ── 相对时间 ──
export function relativeTime(ts?: number | string | null): string {
  if (ts === undefined || ts === null || ts === '') return ''
  const time = typeof ts === 'number' ? (ts < 1e12 ? ts * 1000 : ts) : new Date(ts).getTime()
  if (Number.isNaN(time)) return ''
  const diff = Date.now() - time
  if (diff < 60_000) return '刚刚'
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`
  if (diff < 7 * 86_400_000) return `${Math.floor(diff / 86_400_000)} 天前`
  const d = new Date(time)
  return `${d.getMonth() + 1}月${d.getDate()}日`
}

// ── 快捷指令 ──
export const QUICK_COMMANDS = ['分析 BTCUSDT', '我的持仓和盈亏', '今日 AI 信号', '创建一个 BTC 网格机器人（模拟盘）']
