import { registerLocale } from '../index'

/**
 * 管理页（UserManage）新增区块文案 —— admin.* 扁平 key，自注册进 i18n 核心。
 * 覆盖：用户禁用/启用、运营概览、最近活动。动态文案用 {placeholder} 插值，
 * 调用侧 .replace()（与 market.ts 同模式）。
 */
const zhCN: Record<string, string> = {
  'admin.users.disable': '禁用',
  'admin.users.enable': '启用',
  'admin.users.disableConfirmTitle': '禁用用户',
  'admin.users.disableConfirmMsg': '确认禁用用户 {name}？禁用后该用户的登录态立即失效，无法继续使用平台。',
  'admin.users.disabledOk': '用户已禁用',
  'admin.users.enabledOk': '用户已启用',
  'admin.users.actionFail': '操作失败',

  'admin.summary.title': '运营概览',
  'admin.summary.pendingOrders': '挂单',
  'admin.summary.totalTrades': '总成交',
  'admin.summary.activeStrategies': '活跃策略',
  'admin.summary.unreadAlerts': '未读告警',
  'admin.summary.uptimeHours': '运行时长 (h)',
  'admin.summary.memoryMb': '内存 (MB)',

  'admin.activity.title': '最近活动',
  'admin.activity.empty': '暂无动态',
  'admin.activity.type.trade': '成交',
  'admin.activity.type.risk': '风控',
  'admin.activity.type.audit': '审计',
  'admin.activity.type.notification': '通知',

  'admin.tabs.communityReview': '指标审核',
}

const enUS: Record<string, string> = {
  'admin.users.disable': 'Disable',
  'admin.users.enable': 'Enable',
  'admin.users.disableConfirmTitle': 'Disable user',
  'admin.users.disableConfirmMsg': 'Disable user {name}? Their existing sessions are revoked immediately.',
  'admin.users.disabledOk': 'User disabled',
  'admin.users.enabledOk': 'User enabled',
  'admin.users.actionFail': 'Action failed',

  'admin.summary.title': 'Overview',
  'admin.summary.pendingOrders': 'Pending orders',
  'admin.summary.totalTrades': 'Total trades',
  'admin.summary.activeStrategies': 'Active strategies',
  'admin.summary.unreadAlerts': 'Unread alerts',
  'admin.summary.uptimeHours': 'Uptime (h)',
  'admin.summary.memoryMb': 'Memory (MB)',

  'admin.activity.title': 'Recent activity',
  'admin.activity.empty': 'No recent activity',
  'admin.activity.type.trade': 'Trade',
  'admin.activity.type.risk': 'Risk',
  'admin.activity.type.audit': 'Audit',
  'admin.activity.type.notification': 'Notice',

  'admin.tabs.communityReview': 'Indicator Review',
}

const ja: Record<string, string> = {
  'admin.users.disable': '無効化',
  'admin.users.enable': '有効化',
  'admin.users.disableConfirmTitle': 'ユーザーを無効化',
  'admin.users.disableConfirmMsg': 'ユーザー {name} を無効化しますか？既存のログインセッションは直ちに失効します。',
  'admin.users.disabledOk': 'ユーザーを無効化しました',
  'admin.users.enabledOk': 'ユーザーを有効化しました',
  'admin.users.actionFail': '操作に失敗しました',

  'admin.summary.title': '概要',
  'admin.summary.pendingOrders': '未約定注文',
  'admin.summary.totalTrades': '総約定',
  'admin.summary.activeStrategies': '稼働中ストラテジー',
  'admin.summary.unreadAlerts': '未読アラート',
  'admin.summary.uptimeHours': '稼働時間 (h)',
  'admin.summary.memoryMb': 'メモリ (MB)',

  'admin.activity.title': '最近のアクティビティ',
  'admin.activity.empty': 'アクティビティはありません',
  'admin.activity.type.trade': '約定',
  'admin.activity.type.risk': 'リスク',
  'admin.activity.type.audit': '監査',
  'admin.activity.type.notification': '通知',

  'admin.tabs.communityReview': '指標レビュー',
}

registerLocale('zh-CN', zhCN)
registerLocale('en-US', enUS)
registerLocale('ja', ja)
