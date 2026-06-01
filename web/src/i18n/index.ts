// i18n — 10-language support, synchronous, no external deps
import { useState, useCallback, type ReactNode } from 'react'

export type Lang = 'zh-CN' | 'zh-TW' | 'en-US' | 'ko-KR' | 'ja-JP' | 'fr-FR' | 'de-DE'

export const LANGS: { code: Lang; label: string; flag: string }[] = [
  { code: 'zh-CN', label: '简体中文', flag: '🇨🇳' },
  { code: 'zh-TW', label: '繁體中文', flag: '🇹🇼' },
  { code: 'en-US', label: 'English', flag: '🇺🇸' },
  { code: 'ko-KR', label: '한국어', flag: '🇰🇷' },
  { code: 'ja-JP', label: '日本語', flag: '🇯🇵' },
  { code: 'fr-FR', label: 'Français', flag: '🇫🇷' },
  { code: 'de-DE', label: 'Deutsch', flag: '🇩🇪' },
]

type Dict = Record<string, string>

// Core translations (zh-CN base + en-US)
const ZH: Dict = {
  dashboard: '仪表盘', trading: '交易', strategy: '策略工厂', ai: 'AI 研究',
  backtest: '回测验证', bots: '交易机器人', settings: '系统设置', portfolio: '资产监测',
  login: '登录', logout: '退出登录', register: '注册', profile: '个人中心',
  buy: '买入', sell: '卖出', long: '做多', short: '做空', close: '平仓',
  market: '市价', limit: '限价', stop: '止损', quantity: '数量', price: '价格',
  status: '状态', balance: '余额', equity: '权益', pnl: '盈亏',
  running: '运行中', stopped: '已停止', error: '错误', loading: '加载中...',
  save: '保存', cancel: '取消', delete: '删除', edit: '编辑', confirm: '确认',
  search: '搜索', filter: '筛选', sort: '排序', refresh: '刷新', export: '导出',
  total: '总计', count: '数量', rate: '比率', ratio: '比例',
  high: '高', medium: '中', low: '低', risk: '风险', profit: '收益',
  open: '开', high_p: '高', low_p: '低', close_p: '收', volume: '量',
  order_book: '订单簿', trades: '成交', positions: '持仓', orders: '委托',
  kline: 'K线', timeframe: '周期', symbol: '交易对', exchange: '交易所',
  start_s: '启动', pause: '暂停', resume: '恢复', stop_s: '停止',
  connect: '连接', disconnect: '断开', connected: '已连接', disconnected: '已断开',
  notification: '通知', alert: '告警', message: '消息',
  deploy: '部署', train: '训练', predict: '预测', model: '模型',
  community: '社区', marketplace: '市场', publish: '发布', purchase: '购买',
  settings_s: '设置', help: '帮助', about: '关于', version: '版本',
  success: '成功', failed: '失败', pending: '处理中', cancelled: '已取消',
}

const EN: Dict = {
  dashboard: 'Dashboard', trading: 'Trading', strategy: 'Strategy', ai: 'AI Research',
  backtest: 'Backtest', bots: 'Bots', settings: 'Settings', portfolio: 'Portfolio',
  login: 'Login', logout: 'Logout', register: 'Register', profile: 'Profile',
  buy: 'Buy', sell: 'Sell', long: 'Long', short: 'Short', close: 'Close',
  market: 'Market', limit: 'Limit', stop: 'Stop', quantity: 'Qty', price: 'Price',
  status: 'Status', balance: 'Balance', equity: 'Equity', pnl: 'PnL',
  running: 'Running', stopped: 'Stopped', error: 'Error', loading: 'Loading...',
  save: 'Save', cancel: 'Cancel', delete: 'Delete', edit: 'Edit', confirm: 'Confirm',
  search: 'Search', filter: 'Filter', sort: 'Sort', refresh: 'Refresh', export: 'Export',
  total: 'Total', count: 'Count', rate: 'Rate', ratio: 'Ratio',
  high: 'High', medium: 'Medium', low: 'Low', risk: 'Risk', profit: 'Profit',
  open: 'Open', high_p: 'High', low_p: 'Low', close_p: 'Close', volume: 'Vol',
  order_book: 'Order Book', trades: 'Trades', positions: 'Positions', orders: 'Orders',
  kline: 'K-line', timeframe: 'TF', symbol: 'Symbol', exchange: 'Exchange',
  start_s: 'Start', pause: 'Pause', resume: 'Resume', stop_s: 'Stop',
  connect: 'Connect', disconnect: 'Disconnect', connected: 'Connected', disconnected: 'Disconnected',
  notification: 'Notification', alert: 'Alert', message: 'Message',
  deploy: 'Deploy', train: 'Train', predict: 'Predict', model: 'Model',
  community: 'Community', marketplace: 'Market', publish: 'Publish', purchase: 'Purchase',
  settings_s: 'Settings', help: 'Help', about: 'About', version: 'Version',
  success: 'Success', failed: 'Failed', pending: 'Pending', cancelled: 'Cancelled',
}

const ALL: Record<string, Dict> = { 'zh-CN': ZH, 'en-US': EN,
  'zh-TW': ZH, 'ko-KR': EN, 'ja-JP': EN, 'fr-FR': EN, 'de-DE': EN }

function getDict(lang: string): Dict {
  return ALL[lang] || EN
}

// Simple translate function
export function t(key: string): string {
  const lang = typeof window !== 'undefined' ? localStorage.getItem('lang') || 'zh-CN' : 'zh-CN'
  return getDict(lang)[key] || key
}

// React hook
export function useI18n() {
  const [lang, setLangState] = useState<Lang>(
    () => (localStorage.getItem('lang') as Lang) || 'zh-CN'
  )
  const setLang = useCallback((l: Lang) => {
    setLangState(l)
    localStorage.setItem('lang', l)
  }, [])
  const translate = useCallback((key: string) => getDict(lang)[key] || key, [lang])
  return { lang, setLang, t: translate }
}
