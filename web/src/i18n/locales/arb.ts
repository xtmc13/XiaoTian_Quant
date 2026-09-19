import { registerLocale } from '../index'

/**
 * 套利页面组文案 —— 扁平 key，自注册进 i18n 核心。
 * key 约定：arb.cross.* 跨所套利；arb.tri.* 币种套利；arb.ui.* 两页共享组件。
 * 模板内 {placeholder} 由调用方 .replace() 插值（沿用 Login.tsx 既有写法）。
 * 页头标题复用 nav.arbitrage-cross / nav.arbitrage-triangular，不在此重复定义。
 * 缺失语言回退中文（核心层处理）。
 */
const zhCN: Record<string, string> = {
  // ── 跨所套利：页头/副标题 ──
  'arb.cross.subtitle': '跨交易所套利监控与执行',

  // ── 跨所套利：KPI ──
  'arb.cross.checks': '检测次数',
  'arb.cross.executions': '执行次数',
  'arb.cross.sub-executed': '已执行',

  // ── 跨所套利：配置表单 ──
  'arb.cross.config-title': '引擎配置',
  'arb.cross.min-spread': '最小价差 (%)',
  'arb.cross.order-size': '订单数量',
  'arb.cross.fee-a': '买入所手续费 (小数)',
  'arb.cross.fee-b': '卖出所手续费 (小数)',
  'arb.cross.poll-interval': '轮询间隔 (秒)',
  'arb.cross.min-order-value': '最小订单金额 (USD)',

  // ── 跨所套利：交易所配置提示 ──
  'arb.cross.no-exchange-warning': '系统中没有可用的交易所配置。请先在 Settings / 交易所账号 中配置 API Key。',
  'arb.cross.save-hint': '勾选交易所后点击「保存配置」生效，已选 {count} 个',

  // ── 跨所套利：机会表 ──
  'arb.cross.opportunities-title': '套利机会',
  'arb.cross.hdr-symbol': '交易对',
  'arb.cross.hdr-buy-ex': '买入所',
  'arb.cross.hdr-sell-ex': '卖出所',
  'arb.cross.hdr-buy-price': '买价/可执行',
  'arb.cross.hdr-sell-price': '卖价/可执行',
  'arb.cross.hdr-net-spread': '净价差 %',
  'arb.cross.hdr-slippage': '滑点(买/卖)',
  'arb.cross.hdr-qty': '目标/调整数量',
  'arb.cross.hdr-max-exec': '最大可成交',
  'arb.cross.hdr-est-profit': '预估净利润',
  'arb.cross.exec-prefix': '实',
  'arb.cross.gross-prefix': '毛',
  'arb.cross.insufficient-depth': '深度不足',
  'arb.cross.below-threshold': '未达阈值',
  'arb.cross.no-opportunity': '暂无套利机会',

  // ── 跨所套利：toast ──
  'arb.cross.toast-engine-started': '套利引擎已启动',
  'arb.cross.toast-engine-stopped': '套利引擎已停止',
  'arb.cross.toast-start-failed': '启动失败',
  'arb.cross.toast-stop-failed': '停止失败',
  'arb.cross.toast-exchange-added': '交易所已加入套利',
  'arb.cross.toast-add-failed': '加入失败',
  'arb.cross.toast-exchange-removed': '交易所已移出套利',
  'arb.cross.toast-remove-failed': '移除失败',
  'arb.cross.toast-execute-submitted': '套利执行已提交',
  'arb.cross.toast-partial-failed': '部分交易所未生效：',
  'arb.cross.op-failed': '操作失败',

  // ── 跨所套利：确认框 ──
  'arb.cross.confirm-execute-title': '确认执行真实套利交易？',
  'arb.cross.confirm-execute-msg': '{symbol}：在 {buy} 买入，在 {sell} 卖出，预计价差 {spread}%',

  // ── 币种套利：页头/副标题 ──
  'arb.tri.subtitle': '同交易所多币种循环路径套利',

  // ── 币种套利：KPI ──
  'arb.tri.scans': '扫描次数',
  'arb.tri.cycles': '循环数',
  'arb.tri.sub-detected': '已检测',

  // ── 币种套利：配置表单 ──
  'arb.tri.config-title': '币种套利配置',
  'arb.tri.quote-asset': '计价资产',
  'arb.tri.min-profit': '最小净利润 (%)',
  'arb.tri.order-size': '订单金额',
  'arb.tri.fee-rate': '手续费率 (小数)',
  'arb.tri.exec-timeout': '执行超时 (ms)',
  'arb.tri.save-hint': '勾选交易所后点击「保存配置」生效，已选 {count} 个（多交易所同时监控，币种相同）',

  // ── 币种套利：机会表 ──
  'arb.tri.opportunities-title': '币种套利机会',
  'arb.tri.hdr-cycle': '循环路径',
  'arb.tri.hdr-exchange': '交易所',
  'arb.tri.hdr-start-qty': '起点数量',
  'arb.tri.hdr-end-qty': '终点数量',
  'arb.tri.hdr-net-profit': '净利润 %',
  'arb.tri.hdr-total-fees': '总手续费',
  'arb.tri.hdr-leg-slippage': '三腿滑点',
  'arb.tri.no-opportunity': '暂无币种套利机会',

  // ── 币种套利：toast ──
  'arb.tri.toast-execute-submitted': '币种套利执行已提交',

  // ── 币种套利：确认框 ──
  'arb.tri.confirm-execute-title': '确认执行真实币种套利交易？',
  'arb.tri.confirm-execute-msg': '{cycle}，预计净利润 {profit}%',

  // ── 共享：KPI ──
  'arb.ui.engine-status': '引擎状态',
  'arb.ui.running': '运行中',
  'arb.ui.stopped': '已停止',
  'arb.ui.monitoring': '监控中',
  'arb.ui.click-to-start': '点击启动',
  'arb.ui.total-profit': '总利润',
  'arb.ui.cumulative': '累计',
  'arb.ui.sub-total-scan': '总扫描',

  // ── 共享：按钮 ──
  'arb.ui.start-engine': '启动引擎',
  'arb.ui.stop-engine': '停止引擎',
  'arb.ui.start-engine-title': '启动套利引擎',
  'arb.ui.need-two-exchanges': '至少需要 2 个已加入套利的交易所',
  'arb.ui.config': '配置',
  'arb.ui.history': '历史',
  'arb.ui.close': '关闭',
  'arb.ui.save-config': '保存配置',
  'arb.ui.execute': '执行',
  'arb.ui.cancel': '取消',
  'arb.ui.close-position': '平仓',
  'arb.ui.fail': '失败',
  'arb.ui.mark-as-failed': '标记为失败',
  'arb.ui.mark-fail': '标记失败',

  // ── 共享：配置表单 ──
  'arb.ui.symbols': '交易对（逗号分隔）',
  'arb.ui.max-positions': '最大持仓数',
  'arb.ui.max-slippage': '最大滑点 (%)',
  'arb.ui.min-order-qty': '最小订单数量',
  'arb.ui.auto-execute': '自动执行',
  'arb.ui.dry-run': '模拟运行',
  'arb.ui.adaptive-qty': '自适应数量',
  'arb.ui.loading-config': '加载配置中...',
  'arb.ui.exchange-selection': '交易所选择',
  'arb.ui.loading-exchanges': '加载交易所配置中...',
  'arb.ui.configured': '已配置',
  'arb.ui.configured-testnet': '已配置 · 测试网',
  'arb.ui.missing-credentials': '缺少凭证',
  'arb.ui.not-enabled': '未启用',
  'arb.ui.not-ready': '未就绪',
  'arb.ui.added': '已加入',
  'arb.ui.added-arb': '已加入套利',

  // ── 共享：机会区/持仓区/历史 ──
  'arb.ui.latest-scan': '最新扫描结果',
  'arb.ui.hdr-action': '操作',
  'arb.ui.scanning': '引擎正在扫描中...',
  'arb.ui.start-to-scan': '启动引擎后开始扫描',
  'arb.ui.active-positions': '活跃持仓',
  'arb.ui.no-active-positions': '无活跃持仓',
  'arb.ui.history-record': '历史记录',
  'arb.ui.no-history': '无历史记录',

  // ── 共享：确认框 ──
  'arb.ui.confirm-close-msg': '确认将持仓 {target} 平仓？',
  'arb.ui.confirm-fail-msg': '确认将持仓 {target} 标记为失败？',
  'arb.ui.actual-sell-price': '实际卖出价（USD）',
  'arb.ui.invalid-sell-price': '请输入有效的卖出价',

  // ── 共享：toast ──
  'arb.ui.toast-config-saved': '配置已保存',
  'arb.ui.toast-save-failed': '保存失败',
  'arb.ui.toast-execute-failed': '执行失败',
  'arb.ui.toast-position-closed': '持仓已平仓',
  'arb.ui.toast-close-failed': '平仓失败',
  'arb.ui.toast-position-failed': '持仓已标记为失败',
  'arb.ui.toast-mark-failed': '标记失败',
}

const enUS: Record<string, string> = {
  // ── Cross-exchange: header/subtitle ──
  'arb.cross.subtitle': 'Cross-exchange arbitrage monitoring & execution',

  // ── Cross-exchange: KPI ──
  'arb.cross.checks': 'Checks',
  'arb.cross.executions': 'Executions',
  'arb.cross.sub-executed': 'Executed',

  // ── Cross-exchange: config form ──
  'arb.cross.config-title': 'Engine Config',
  'arb.cross.min-spread': 'Min Spread (%)',
  'arb.cross.order-size': 'Order Size',
  'arb.cross.fee-a': 'Buy Exchange Fee (decimal)',
  'arb.cross.fee-b': 'Sell Exchange Fee (decimal)',
  'arb.cross.poll-interval': 'Poll Interval (s)',
  'arb.cross.min-order-value': 'Min Order Value (USD)',

  // ── Cross-exchange: exchange config hints ──
  'arb.cross.no-exchange-warning':
    'No usable exchange configuration found. Please set up API keys in Settings / Exchange Accounts first.',
  'arb.cross.save-hint': 'Selected exchanges take effect after clicking "Save Config". {count} selected',

  // ── Cross-exchange: opportunities table ──
  'arb.cross.opportunities-title': 'Arbitrage Opportunities',
  'arb.cross.hdr-symbol': 'Pair',
  'arb.cross.hdr-buy-ex': 'Buy Ex.',
  'arb.cross.hdr-sell-ex': 'Sell Ex.',
  'arb.cross.hdr-buy-price': 'Buy/Exec Price',
  'arb.cross.hdr-sell-price': 'Sell/Exec Price',
  'arb.cross.hdr-net-spread': 'Net Spread %',
  'arb.cross.hdr-slippage': 'Slippage (B/S)',
  'arb.cross.hdr-qty': 'Target/Adj Qty',
  'arb.cross.hdr-max-exec': 'Max Executable',
  'arb.cross.hdr-est-profit': 'Est. Net Profit',
  'arb.cross.exec-prefix': 'Exec',
  'arb.cross.gross-prefix': 'Gross',
  'arb.cross.insufficient-depth': 'Insufficient depth',
  'arb.cross.below-threshold': 'Below threshold',
  'arb.cross.no-opportunity': 'No arbitrage opportunities',

  // ── Cross-exchange: toasts ──
  'arb.cross.toast-engine-started': 'Arbitrage engine started',
  'arb.cross.toast-engine-stopped': 'Arbitrage engine stopped',
  'arb.cross.toast-start-failed': 'Failed to start',
  'arb.cross.toast-stop-failed': 'Failed to stop',
  'arb.cross.toast-exchange-added': 'Exchange added to arbitrage',
  'arb.cross.toast-add-failed': 'Failed to add',
  'arb.cross.toast-exchange-removed': 'Exchange removed from arbitrage',
  'arb.cross.toast-remove-failed': 'Failed to remove',
  'arb.cross.toast-execute-submitted': 'Arbitrage execution submitted',
  'arb.cross.toast-partial-failed': 'Some exchanges failed to apply: ',
  'arb.cross.op-failed': 'operation failed',

  // ── Cross-exchange: confirm dialogs ──
  'arb.cross.confirm-execute-title': 'Execute real arbitrage trade?',
  'arb.cross.confirm-execute-msg': '{symbol}: buy on {buy}, sell on {sell}, estimated spread {spread}%',

  // ── Triangular: header/subtitle ──
  'arb.tri.subtitle': 'Multi-currency cycle arbitrage on a single exchange',

  // ── Triangular: KPI ──
  'arb.tri.scans': 'Scans',
  'arb.tri.cycles': 'Cycles',
  'arb.tri.sub-detected': 'Detected',

  // ── Triangular: config form ──
  'arb.tri.config-title': 'Currency Arbitrage Config',
  'arb.tri.quote-asset': 'Quote Asset',
  'arb.tri.min-profit': 'Min Net Profit (%)',
  'arb.tri.order-size': 'Order Size',
  'arb.tri.fee-rate': 'Fee Rate (decimal)',
  'arb.tri.exec-timeout': 'Execution Timeout (ms)',
  'arb.tri.save-hint':
    'Selected exchanges take effect after clicking "Save Config". {count} selected (multiple exchanges monitored simultaneously, same symbols)',

  // ── Triangular: opportunities table ──
  'arb.tri.opportunities-title': 'Currency Arbitrage Opportunities',
  'arb.tri.hdr-cycle': 'Cycle Path',
  'arb.tri.hdr-exchange': 'Exchange',
  'arb.tri.hdr-start-qty': 'Start Qty',
  'arb.tri.hdr-end-qty': 'End Qty',
  'arb.tri.hdr-net-profit': 'Net Profit %',
  'arb.tri.hdr-total-fees': 'Total Fees',
  'arb.tri.hdr-leg-slippage': 'Leg Slippage',
  'arb.tri.no-opportunity': 'No currency arbitrage opportunities',

  // ── Triangular: toasts ──
  'arb.tri.toast-execute-submitted': 'Currency arbitrage execution submitted',

  // ── Triangular: confirm dialogs ──
  'arb.tri.confirm-execute-title': 'Execute real currency arbitrage trade?',
  'arb.tri.confirm-execute-msg': '{cycle}, estimated net profit {profit}%',

  // ── Shared: KPI ──
  'arb.ui.engine-status': 'Engine Status',
  'arb.ui.running': 'Running',
  'arb.ui.stopped': 'Stopped',
  'arb.ui.monitoring': 'Monitoring',
  'arb.ui.click-to-start': 'Click to start',
  'arb.ui.total-profit': 'Total Profit',
  'arb.ui.cumulative': 'Cumulative',
  'arb.ui.sub-total-scan': 'Total scans',

  // ── Shared: buttons ──
  'arb.ui.start-engine': 'Start Engine',
  'arb.ui.stop-engine': 'Stop Engine',
  'arb.ui.start-engine-title': 'Start arbitrage engine',
  'arb.ui.need-two-exchanges': 'At least 2 exchanges added to arbitrage required',
  'arb.ui.config': 'Config',
  'arb.ui.history': 'History',
  'arb.ui.close': 'Close',
  'arb.ui.save-config': 'Save Config',
  'arb.ui.execute': 'Execute',
  'arb.ui.cancel': 'Cancel',
  'arb.ui.close-position': 'Close',
  'arb.ui.fail': 'Fail',
  'arb.ui.mark-as-failed': 'Mark as Failed',
  'arb.ui.mark-fail': 'Mark Failed',

  // ── Shared: config form ──
  'arb.ui.symbols': 'Symbols (comma-separated)',
  'arb.ui.max-positions': 'Max Positions',
  'arb.ui.max-slippage': 'Max Slippage (%)',
  'arb.ui.min-order-qty': 'Min Order Qty',
  'arb.ui.auto-execute': 'Auto Execute',
  'arb.ui.dry-run': 'Dry Run',
  'arb.ui.adaptive-qty': 'Adaptive Qty',
  'arb.ui.loading-config': 'Loading config...',
  'arb.ui.exchange-selection': 'Exchange Selection',
  'arb.ui.loading-exchanges': 'Loading exchange config...',
  'arb.ui.configured': 'Configured',
  'arb.ui.configured-testnet': 'Configured · Testnet',
  'arb.ui.missing-credentials': 'Missing credentials',
  'arb.ui.not-enabled': 'Not enabled',
  'arb.ui.not-ready': 'Not ready',
  'arb.ui.added': 'Added',
  'arb.ui.added-arb': 'Added to arbitrage',

  // ── Shared: opportunities/positions/history ──
  'arb.ui.latest-scan': 'Latest scan result',
  'arb.ui.hdr-action': 'Action',
  'arb.ui.scanning': 'Engine scanning...',
  'arb.ui.start-to-scan': 'Start the engine to begin scanning',
  'arb.ui.active-positions': 'Active Positions',
  'arb.ui.no-active-positions': 'No active positions',
  'arb.ui.history-record': 'History',
  'arb.ui.no-history': 'No history records',

  // ── Shared: confirm dialogs ──
  'arb.ui.confirm-close-msg': 'Close position {target}?',
  'arb.ui.confirm-fail-msg': 'Mark position {target} as failed?',
  'arb.ui.actual-sell-price': 'Actual sell price (USD)',
  'arb.ui.invalid-sell-price': 'Please enter a valid sell price',

  // ── Shared: toasts ──
  'arb.ui.toast-config-saved': 'Config saved',
  'arb.ui.toast-save-failed': 'Failed to save',
  'arb.ui.toast-execute-failed': 'Execution failed',
  'arb.ui.toast-position-closed': 'Position closed',
  'arb.ui.toast-close-failed': 'Failed to close',
  'arb.ui.toast-position-failed': 'Position marked as failed',
  'arb.ui.toast-mark-failed': 'Failed to mark',
}

const ja: Record<string, string> = {
  // ── 跨所裁定：ヘッダー/サブタイトル ──
  'arb.cross.subtitle': '跨取引所アービトラージの監視と実行',

  // ── 跨所裁定：KPI ──
  'arb.cross.checks': '検出回数',
  'arb.cross.executions': '実行回数',
  'arb.cross.sub-executed': '実行済み',

  // ── 跨所裁定：設定フォーム ──
  'arb.cross.config-title': 'エンジン設定',
  'arb.cross.min-spread': '最小スプレッド (%)',
  'arb.cross.order-size': '注文数量',
  'arb.cross.fee-a': '買い取引所手数料 (小数)',
  'arb.cross.fee-b': '売り取引所手数料 (小数)',
  'arb.cross.poll-interval': 'ポーリング間隔 (秒)',
  'arb.cross.min-order-value': '最小注文金額 (USD)',

  // ── 跨所裁定：取引所設定ヒント ──
  'arb.cross.no-exchange-warning':
    '利用可能な取引所設定がありません。先に Settings / 取引所アカウント で API Key を設定してください。',
  'arb.cross.save-hint': '取引所を選択して「設定を保存」をクリックすると有効になります。選択中 {count} 件',

  // ── 跨所裁定：機会テーブル ──
  'arb.cross.opportunities-title': 'アービトラージ機会',
  'arb.cross.hdr-symbol': 'ペア',
  'arb.cross.hdr-buy-ex': '買い取引所',
  'arb.cross.hdr-sell-ex': '売り取引所',
  'arb.cross.hdr-buy-price': '買価/実行可能',
  'arb.cross.hdr-sell-price': '売価/実行可能',
  'arb.cross.hdr-net-spread': 'ネットスプレッド %',
  'arb.cross.hdr-slippage': 'スリッページ(買/売)',
  'arb.cross.hdr-qty': '目標/調整数量',
  'arb.cross.hdr-max-exec': '最大実行可能',
  'arb.cross.hdr-est-profit': '推定純利益',
  'arb.cross.exec-prefix': '実',
  'arb.cross.gross-prefix': '総',
  'arb.cross.insufficient-depth': '深度不足',
  'arb.cross.below-threshold': '閾値未達',
  'arb.cross.no-opportunity': 'アービトラージ機会なし',

  // ── 跨所裁定：トースト ──
  'arb.cross.toast-engine-started': 'アービトラージエンジンを開始しました',
  'arb.cross.toast-engine-stopped': 'アービトラージエンジンを停止しました',
  'arb.cross.toast-start-failed': '開始に失敗しました',
  'arb.cross.toast-stop-failed': '停止に失敗しました',
  'arb.cross.toast-exchange-added': '取引所をアービトラージに追加しました',
  'arb.cross.toast-add-failed': '追加に失敗しました',
  'arb.cross.toast-exchange-removed': '取引所をアービトラージから削除しました',
  'arb.cross.toast-remove-failed': '削除に失敗しました',
  'arb.cross.toast-execute-submitted': 'アービトラージ実行を送信しました',
  'arb.cross.toast-partial-failed': '一部の取引所が適用できませんでした: ',
  'arb.cross.op-failed': '操作に失敗',

  // ── 跨所裁定：確認ダイアログ ──
  'arb.cross.confirm-execute-title': '実際のアービトラージ取引を実行しますか？',
  'arb.cross.confirm-execute-msg': '{symbol}: {buy} で買い、{sell} で売り、予想スプレッド {spread}%',

  // ── 通貨裁定：ヘッダー/サブタイトル ──
  'arb.tri.subtitle': '同一取引所でのマルチ通貨サイクルアービトラージ',

  // ── 通貨裁定：KPI ──
  'arb.tri.scans': 'スキャン回数',
  'arb.tri.cycles': 'サイクル数',
  'arb.tri.sub-detected': '検出済み',

  // ── 通貨裁定：設定フォーム ──
  'arb.tri.config-title': '通貨アービトラージ設定',
  'arb.tri.quote-asset': '基軸通貨',
  'arb.tri.min-profit': '最小純利益 (%)',
  'arb.tri.order-size': '注文金額',
  'arb.tri.fee-rate': '手数料率 (小数)',
  'arb.tri.exec-timeout': '実行タイムアウト (ms)',
  'arb.tri.save-hint':
    '取引所を選択して「設定を保存」をクリックすると有効になります。選択中 {count} 件（複数取引所を同時監視、通貨は共通）',

  // ── 通貨裁定：機会テーブル ──
  'arb.tri.opportunities-title': '通貨アービトラージ機会',
  'arb.tri.hdr-cycle': 'サイクルパス',
  'arb.tri.hdr-exchange': '取引所',
  'arb.tri.hdr-start-qty': '開始数量',
  'arb.tri.hdr-end-qty': '終了数量',
  'arb.tri.hdr-net-profit': '純利益 %',
  'arb.tri.hdr-total-fees': '総手数料',
  'arb.tri.hdr-leg-slippage': 'レッグスリッページ',
  'arb.tri.no-opportunity': '通貨アービトラージ機会なし',

  // ── 通貨裁定：トースト ──
  'arb.tri.toast-execute-submitted': '通貨アービトラージ実行を送信しました',

  // ── 通貨裁定：確認ダイアログ ──
  'arb.tri.confirm-execute-title': '実際の通貨アービトラージ取引を実行しますか？',
  'arb.tri.confirm-execute-msg': '{cycle}、予想純利益 {profit}%',

  // ── 共有：KPI ──
  'arb.ui.engine-status': 'エンジン状態',
  'arb.ui.running': '実行中',
  'arb.ui.stopped': '停止済み',
  'arb.ui.monitoring': '監視中',
  'arb.ui.click-to-start': 'クリックで開始',
  'arb.ui.total-profit': '総利益',
  'arb.ui.cumulative': '累計',
  'arb.ui.sub-total-scan': '総スキャン',

  // ── 共有：ボタン ──
  'arb.ui.start-engine': 'エンジン開始',
  'arb.ui.stop-engine': 'エンジン停止',
  'arb.ui.start-engine-title': 'アービトラージエンジンを開始',
  'arb.ui.need-two-exchanges': 'アービトラージに追加された取引所が2つ以上必要です',
  'arb.ui.config': '設定',
  'arb.ui.history': '履歴',
  'arb.ui.close': '閉じる',
  'arb.ui.save-config': '設定を保存',
  'arb.ui.execute': '実行',
  'arb.ui.cancel': 'キャンセル',
  'arb.ui.close-position': '決済',
  'arb.ui.fail': '失敗',
  'arb.ui.mark-as-failed': '失敗としてマーク',
  'arb.ui.mark-fail': '失敗にする',

  // ── 共有：設定フォーム ──
  'arb.ui.symbols': 'ペア（カンマ区切り）',
  'arb.ui.max-positions': '最大ポジション数',
  'arb.ui.max-slippage': '最大スリッページ (%)',
  'arb.ui.min-order-qty': '最小注文数量',
  'arb.ui.auto-execute': '自動実行',
  'arb.ui.dry-run': 'デモ実行',
  'arb.ui.adaptive-qty': '適応数量',
  'arb.ui.loading-config': '設定を読み込み中...',
  'arb.ui.exchange-selection': '取引所選択',
  'arb.ui.loading-exchanges': '取引所設定を読み込み中...',
  'arb.ui.configured': '設定済み',
  'arb.ui.configured-testnet': '設定済み · テストネット',
  'arb.ui.missing-credentials': '認証情報不足',
  'arb.ui.not-enabled': '有効化されていません',
  'arb.ui.not-ready': '未準備',
  'arb.ui.added': '追加済み',
  'arb.ui.added-arb': 'アービトラージに追加済み',

  // ── 共有：機会/ポジション/履歴 ──
  'arb.ui.latest-scan': '最新スキャン結果',
  'arb.ui.hdr-action': '操作',
  'arb.ui.scanning': 'エンジンがスキャン中...',
  'arb.ui.start-to-scan': 'エンジンを開始するとスキャンします',
  'arb.ui.active-positions': 'アクティブポジション',
  'arb.ui.no-active-positions': 'アクティブポジションなし',
  'arb.ui.history-record': '履歴',
  'arb.ui.no-history': '履歴なし',

  // ── 共有：確認ダイアログ ──
  'arb.ui.confirm-close-msg': 'ポジション {target} を決済しますか？',
  'arb.ui.confirm-fail-msg': 'ポジション {target} を失敗としてマークしますか？',
  'arb.ui.actual-sell-price': '実際の売価（USD）',
  'arb.ui.invalid-sell-price': '有効な売価を入力してください',

  // ── 共有：トースト ──
  'arb.ui.toast-config-saved': '設定を保存しました',
  'arb.ui.toast-save-failed': '保存に失敗しました',
  'arb.ui.toast-execute-failed': '実行に失敗しました',
  'arb.ui.toast-position-closed': 'ポジションを決済しました',
  'arb.ui.toast-close-failed': '決済に失敗しました',
  'arb.ui.toast-position-failed': 'ポジションを失敗としてマークしました',
  'arb.ui.toast-mark-failed': 'マークに失敗しました',
}

registerLocale('zh-CN', zhCN)
registerLocale('en-US', enUS)
registerLocale('ja', ja)
