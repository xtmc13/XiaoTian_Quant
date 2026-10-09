import { registerLocale } from '../index'

/**
 * 策略运行面板（RuntimePanel）文案 —— 扁平 key，自注册进 i18n 核心。
 * key 约定：runtime.*；缺失语言回退中文（核心层处理）。
 *
 * H4：pending_close = CRA 在途平仓徽标（后端 RuntimeStatus.pending_close_kind
 * 仅在途时透出）。kind.* 为各在途形态标签，未知 kind 回退原始字符串。
 */
const zhCN: Record<string, string> = {
  'runtime.pending_close': '平仓在途：{kind}',
  'runtime.pending_close.kind.tail': '尾单止盈',
  'runtime.pending_close.kind.head_tail': '首尾止盈',
  'runtime.pending_close.kind.reverse_tp': '反向止盈',
  'runtime.pending_close.kind.reverse_sl': '反向止损',
  'runtime.pending_close.kind.burn_dual': '对向燃烧斩仓',
  'runtime.pending_close.kind.burn_global': '全局燃烧斩仓',
  'runtime.pending_close.kind.manual_reduce': '手动减仓',
  'runtime.pending_close.kind.manual_close': '手动清仓',
}

const enUS: Record<string, string> = {
  'runtime.pending_close': 'Close in flight: {kind}',
  'runtime.pending_close.kind.tail': 'tail take-profit',
  'runtime.pending_close.kind.head_tail': 'head+tail take-profit',
  'runtime.pending_close.kind.reverse_tp': 'reverse take-profit',
  'runtime.pending_close.kind.reverse_sl': 'reverse stop-loss',
  'runtime.pending_close.kind.burn_dual': 'dual burn cut',
  'runtime.pending_close.kind.burn_global': 'global burn cut',
  'runtime.pending_close.kind.manual_reduce': 'manual reduce',
  'runtime.pending_close.kind.manual_close': 'manual close-all',
}

const ja: Record<string, string> = {
  'runtime.pending_close': '決済処理中：{kind}',
  'runtime.pending_close.kind.tail': '尾端利確',
  'runtime.pending_close.kind.head_tail': '先端・尾端利確',
  'runtime.pending_close.kind.reverse_tp': '逆方向利確',
  'runtime.pending_close.kind.reverse_sl': '逆方向損切り',
  'runtime.pending_close.kind.burn_dual': '対向バーン損切り',
  'runtime.pending_close.kind.burn_global': '全局バーン損切り',
  'runtime.pending_close.kind.manual_reduce': '手動減倉',
  'runtime.pending_close.kind.manual_close': '手動全決済',
}

registerLocale('zh-CN', zhCN)
registerLocale('en-US', enUS)
registerLocale('ja', ja)
