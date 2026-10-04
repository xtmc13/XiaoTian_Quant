import { registerLocale } from '../index'

/**
 * AI 异步多模型共识分析文案 —— ai.async.* 扁平 key，自注册进 i18n 核心。
 * 对应 gateway POST /analysis/start + GET /analysis/result（任务轮询）。
 */
const zhCN: Record<string, string> = {
  'ai.async.button': '多模型共识',
  'ai.async.title': '多模型共识分析',
  'ai.async.desc': '并行调用多个已配置的 AI 模型对同一标的做技术研判，汇总投票得出共识信号',
  'ai.async.models': '参与模型',
  'ai.async.noModels': '暂无已配置的 AI 模型（请先在设置页配置 API Key）',
  'ai.async.interval': 'K线周期',
  'ai.async.start': '开始分析',
  'ai.async.starting': '提交中...',
  'ai.async.processing': '多模型分析中，请稍候...',
  'ai.async.startFailed': '启动失败',
  'ai.async.timeout': '分析超时，请重试',
  'ai.async.selectOne': '请至少选择一个模型',
  'ai.async.consensus': '共识信号',
  'ai.async.agreement': '一致度 {pct}%',
  'ai.async.votes': '多 {b} / 空 {s} / 中性 {n}',
  'ai.async.bullish': '看多',
  'ai.async.bearish': '看空',
  'ai.async.neutral': '中性',
  'ai.async.confidence': '置信度 {pct}%',
}

const enUS: Record<string, string> = {
  'ai.async.button': 'Multi-Model',
  'ai.async.title': 'Multi-Model Consensus Analysis',
  'ai.async.desc': 'Run multiple configured AI models in parallel on the same symbol and aggregate votes into a consensus signal',
  'ai.async.models': 'Models',
  'ai.async.noModels': 'No AI models configured (set API keys in Settings first)',
  'ai.async.interval': 'Interval',
  'ai.async.start': 'Start Analysis',
  'ai.async.starting': 'Submitting...',
  'ai.async.processing': 'Multi-model analysis running...',
  'ai.async.startFailed': 'Failed to start',
  'ai.async.timeout': 'Analysis timed out, please retry',
  'ai.async.selectOne': 'Select at least one model',
  'ai.async.consensus': 'Consensus',
  'ai.async.agreement': 'Agreement {pct}%',
  'ai.async.votes': 'Bull {b} / Bear {s} / Neutral {n}',
  'ai.async.bullish': 'Bullish',
  'ai.async.bearish': 'Bearish',
  'ai.async.neutral': 'Neutral',
  'ai.async.confidence': 'Confidence {pct}%',
}

const ja: Record<string, string> = {
  'ai.async.button': 'マルチモデル',
  'ai.async.title': 'マルチモデルコンセンサス分析',
  'ai.async.desc': '設定済みの複数 AI モデルを並列実行し、投票を集約してコンセンサスシグナルを得ます',
  'ai.async.models': 'モデル',
  'ai.async.noModels': '設定済みの AI モデルがありません（先に設定ページで API キーを設定）',
  'ai.async.interval': '足種',
  'ai.async.start': '分析開始',
  'ai.async.starting': '送信中...',
  'ai.async.processing': 'マルチモデル分析実行中...',
  'ai.async.startFailed': '開始に失敗',
  'ai.async.timeout': '分析がタイムアウトしました。再試行してください',
  'ai.async.selectOne': 'モデルを 1 つ以上選択してください',
  'ai.async.consensus': 'コンセンサス',
  'ai.async.agreement': '一致度 {pct}%',
  'ai.async.votes': '強気 {b} / 弱気 {s} / 中立 {n}',
  'ai.async.bullish': '強気',
  'ai.async.bearish': '弱気',
  'ai.async.neutral': '中立',
  'ai.async.confidence': '信頼度 {pct}%',
}

registerLocale('zh-CN', zhCN)
registerLocale('en-US', enUS)
registerLocale('ja', ja)
