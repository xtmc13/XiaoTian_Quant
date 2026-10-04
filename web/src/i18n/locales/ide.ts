import { registerLocale } from '../index'

/**
 * 指标 IDE 附加面板文案 —— ide.sandbox.*（Python 策略沙箱运行，
 * POST /strategies-python/run）与 ide.experiments.*（实验记录，
 * GET /experiments + /experiment/status/:id）扁平 key。
 */
const zhCN: Record<string, string> = {
  // ── Python 沙箱 ──
  'ide.sandbox.title': 'Python 沙箱运行',
  'ide.sandbox.desc': '将当前代码发送到 Python 策略引擎执行（独立于图表渲染管线）',
  'ide.sandbox.mode': '模式',
  'ide.sandbox.modeIndicator': '指标 (indicator)',
  'ide.sandbox.modeScript': '脚本 (script)',
  'ide.sandbox.run': '沙箱运行',
  'ide.sandbox.running': '执行中...',
  'ide.sandbox.failed': '沙箱执行失败',
  'ide.sandbox.signals': '信号（{n}）',
  'ide.sandbox.noSignals': '执行完成，无信号输出',
  'ide.sandbox.time': '时间',
  'ide.sandbox.action': '动作',
  'ide.sandbox.price': '价格',
  'ide.sandbox.reason': '原因',

  // ── 实验记录 ──
  'ide.experiments.title': '实验记录',
  'ide.experiments.refresh': '刷新',
  'ide.experiments.empty': '暂无实验记录（自动调参/敏感性分析会生成记录）',
  'ide.experiments.name': '实验',
  'ide.experiments.status': '状态',
  'ide.experiments.bestScore': '最佳评分',
  'ide.experiments.isReturn': '样本内收益',
  'ide.experiments.oosReturn': '样本外收益',
  'ide.experiments.duration': '耗时',
  'ide.experiments.createdAt': '创建时间',
}

const enUS: Record<string, string> = {
  'ide.sandbox.title': 'Python Sandbox Run',
  'ide.sandbox.desc': 'Send current code to the Python strategy engine (independent of the chart pipeline)',
  'ide.sandbox.mode': 'Mode',
  'ide.sandbox.modeIndicator': 'Indicator',
  'ide.sandbox.modeScript': 'Script',
  'ide.sandbox.run': 'Run in Sandbox',
  'ide.sandbox.running': 'Running...',
  'ide.sandbox.failed': 'Sandbox execution failed',
  'ide.sandbox.signals': 'Signals ({n})',
  'ide.sandbox.noSignals': 'Finished, no signals emitted',
  'ide.sandbox.time': 'Time',
  'ide.sandbox.action': 'Action',
  'ide.sandbox.price': 'Price',
  'ide.sandbox.reason': 'Reason',

  'ide.experiments.title': 'Experiment History',
  'ide.experiments.refresh': 'Refresh',
  'ide.experiments.empty': 'No experiments yet (auto-tune / sensitivity runs create records)',
  'ide.experiments.name': 'Experiment',
  'ide.experiments.status': 'Status',
  'ide.experiments.bestScore': 'Best Score',
  'ide.experiments.isReturn': 'IS Return',
  'ide.experiments.oosReturn': 'OOS Return',
  'ide.experiments.duration': 'Duration',
  'ide.experiments.createdAt': 'Created At',
}

const ja: Record<string, string> = {
  'ide.sandbox.title': 'Python サンドボックス実行',
  'ide.sandbox.desc': '現在のコードを Python 戦略エンジンに送信して実行（チャート描画パイプラインとは独立）',
  'ide.sandbox.mode': 'モード',
  'ide.sandbox.modeIndicator': 'インジケーター (indicator)',
  'ide.sandbox.modeScript': 'スクリプト (script)',
  'ide.sandbox.run': 'サンドボックス実行',
  'ide.sandbox.running': '実行中...',
  'ide.sandbox.failed': 'サンドボックス実行に失敗',
  'ide.sandbox.signals': 'シグナル（{n}）',
  'ide.sandbox.noSignals': '実行完了、シグナル出力なし',
  'ide.sandbox.time': '時刻',
  'ide.sandbox.action': 'アクション',
  'ide.sandbox.price': '価格',
  'ide.sandbox.reason': '理由',

  'ide.experiments.title': '実験履歴',
  'ide.experiments.refresh': '更新',
  'ide.experiments.empty': '実験履歴はありません（自動調整/感度分析で生成されます）',
  'ide.experiments.name': '実験',
  'ide.experiments.status': '状態',
  'ide.experiments.bestScore': 'ベストスコア',
  'ide.experiments.isReturn': 'IS 収益',
  'ide.experiments.oosReturn': 'OOS 収益',
  'ide.experiments.duration': '所要時間',
  'ide.experiments.createdAt': '作成日時',
}

registerLocale('zh-CN', zhCN)
registerLocale('en-US', enUS)
registerLocale('ja', ja)
