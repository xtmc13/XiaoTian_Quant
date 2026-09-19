import { registerLocale } from '../index'

/**
 * 仪表盘文案 —— 扁平 key，自注册进 i18n 核心。
 * key 约定：dashboard.* Dashboard 页面（KPI/图表/套利/资产/日历/AI 代理/排行/风控/ML/新手引导/机器人）。
 * 含 {n}/{v}/{w}/{l}/{s}/{p}/{a}/{done}/{total}/{y}/{m} 占位符的词条需在调用处 replace。
 * 缺失语言回退中文（核心层处理）。
 */
const zhCN: Record<string, string> = {
  // ── KPI 卡片 ──
  'dashboard.kpi.total-equity': '总资产估值',
  'dashboard.kpi.today-pnl': '今日盈亏',
  'dashboard.kpi.win-rate': '胜率',
  'dashboard.kpi.last-30d': '近30天',
  'dashboard.kpi.profit-factor': '盈亏比',
  'dashboard.kpi.profit-factor-sub': 'Profit Factor',
  'dashboard.kpi.max-drawdown': '最大回撤',
  'dashboard.kpi.historical-max': '历史最大',
  'dashboard.kpi.total-trades': '总交易数',
  'dashboard.kpi.trades-unit': '笔',
  'dashboard.kpi.running-strategies': '运行策略',
  'dashboard.kpi.total-strategies': '{n} 个总策略',

  // ── 通用状态 ──
  'dashboard.status.running': '运行中',
  'dashboard.status.normal': '正常',
  'dashboard.status.error': '异常',
  'dashboard.status.deployed': '已部署',
  'dashboard.status.idle': '空闲',
  'dashboard.status.online': '在线',
  'dashboard.status.offline': '离线',

  // ── 图表卡片 ──
  'dashboard.chart.equity': '权益曲线',
  'dashboard.chart.realtime': '实时',
  'dashboard.chart.daily-pnl-dist': '日盈亏分布',
  'dashboard.chart.profit': '盈利',
  'dashboard.chart.loss': '亏损',

  // ── 套利绩效 ──
  'dashboard.arb.title': '套利绩效',
  'dashboard.arb.view-monitor': '套利监控',
  'dashboard.arb.total-pnl': '累计套利盈亏',
  'dashboard.arb.pnl-count': '盈亏笔数',
  'dashboard.arb.win-loss-count': '{w} 盈 / {l} 亏',
  'dashboard.arb.total-trades': '套利交易笔数',
  'dashboard.arb.total-fees': '总手续费',
  'dashboard.arb.fee-value': '费 {v}',
  'dashboard.arb.win-rate': '套利胜率',
  'dashboard.arb.best-worst': '最佳 {w} / 最差 {l}',
  'dashboard.arb.avg-pnl': '平均单笔收益',
  'dashboard.arb.avg-per-trade': '每笔平均',
  'dashboard.arb.equity-curve': '套利累计收益曲线',
  'dashboard.arb.daily-pnl-dist': '套利日收益分布',

  // ── 资产分布 ──
  'dashboard.asset.title': '资产分布',
  'dashboard.asset.spot': '现货',
  'dashboard.asset.futures': '合约',
  'dashboard.asset.funding': '资金',
  'dashboard.asset.earn': '理财',
  'dashboard.asset.total': '合计',
  'dashboard.asset.no-data': '暂无交易所数据',
  'dashboard.asset.no-data-desc': '在设置页面配置您的交易所 API',

  // ── 盈亏日历 ──
  'dashboard.calendar.title': '盈亏日历',
  'dashboard.calendar.prev-month': '上个月',
  'dashboard.calendar.next-month': '下个月',
  'dashboard.calendar.today': '今天',
  'dashboard.calendar.year-month': '{y}年{m}月',
  'dashboard.calendar.win-days': '赢 {n}天',
  'dashboard.calendar.loss-days': '亏 {n}天',
  'dashboard.calendar.weekdays': '日,一,二,三,四,五,六',

  // ── AI 多智能体 ──
  'dashboard.ai.title': 'AI 多智能体状态',
  'dashboard.ai.agent-market': '市场情报',
  'dashboard.ai.agent-strategy': '策略生成',
  'dashboard.ai.agent-risk': '风控AI',
  'dashboard.ai.detail-signals': '{n} 条新信号',
  'dashboard.ai.detail-pending': '{n} 个策略待审',
  'dashboard.ai.detail-safe': '所有指标安全',
  'dashboard.ai.waiting-data': '等待AI智能体数据...',

  // ── 运行中策略 / 策略行 ──
  'dashboard.strategy.title': '运行中策略',
  'dashboard.strategy.no-running': '暂无运行中的策略',
  'dashboard.strategy.no-running-desc': '在策略页面启动您的第一个策略',
  'dashboard.strategy.go-strategy': '去策略页面',
  'dashboard.strategy.dir-long': '多',
  'dashboard.strategy.dir-short': '空',
  'dashboard.strategy.dir-both': '双向',
  'dashboard.strategy.tp-full': '全仓止盈',
  'dashboard.strategy.tp-tail': '尾单',
  'dashboard.strategy.tp-head-tail': '首尾',
  'dashboard.strategy.tp-moving': '移动',
  'dashboard.strategy.order-desc': '{n}单·首{a}U',
  'dashboard.strategy.params-line': '补差{s}% · 止盈{p}%',
  'dashboard.strategy.open-macd-golden': 'MACD金叉',
  'dashboard.strategy.open-macd-death': 'MACD死叉',
  'dashboard.strategy.open-ema': 'EMA拐点',
  'dashboard.strategy.open-market': '市价',

  // ── 策略排行榜 ──
  'dashboard.ranking.title': '策略排行榜',
  'dashboard.ranking.view-all': '全部',
  'dashboard.ranking.col-strategy': '策略',
  'dashboard.ranking.col-return': '收益',
  'dashboard.ranking.win-rate': '胜率 {v}%',
  'dashboard.ranking.sharpe': '夏普 {v}',
  'dashboard.ranking.no-data': '暂无排行数据',
  'dashboard.ranking.no-data-desc': '运行策略后将显示排行榜',

  // ── 风控状态 ──
  'dashboard.risk.title': '风控状态',
  'dashboard.risk.view-center': '查看风控中心',
  'dashboard.risk.global-protection': '全局交易保护',
  'dashboard.risk.trading-suspended': '交易暂停',
  'dashboard.risk.normal': '正常运行',
  'dashboard.risk.active-rules': '活跃规则',
  'dashboard.risk.blocked-pairs': '阻断交易对',
  'dashboard.risk.last-triggered': '最近触发',
  'dashboard.risk.no-data': '暂无风控数据',

  // ── ML 模型状态 ──
  'dashboard.ml.title': 'ML 模型状态',
  'dashboard.ml.view-management': '查看模型管理',
  'dashboard.ml.model-count': '模型数量',
  'dashboard.ml.latest-status': '最新状态',
  'dashboard.ml.feature-pipeline': '特征管道',
  'dashboard.ml.service': 'ML 服务',
  'dashboard.ml.features': '特征',
  'dashboard.ml.no-models': '暂无训练好的模型',

  // ── 新手引导 ──
  'dashboard.guide.quick-start': '快速开始',
  'dashboard.guide.title': '创建您的第一个量化策略',
  'dashboard.guide.desc': '完成 {done}/{total} 步即可开始量化交易。选择策略类型、配置参数、一键启动实盘。',
  'dashboard.guide.strategy-market': '策略市场',
  'dashboard.guide.create-strategy': '创建策略',
  'dashboard.guide.close-tip': '关闭提示',
  'dashboard.guide.step-connect': '连接交易所',
  'dashboard.guide.step-create': '创建首个策略',
  'dashboard.guide.step-launch': '启动自动交易',
  'dashboard.guide.action-settings': '去设置',
  'dashboard.guide.action-create': '创建',
  'dashboard.guide.action-launch': '启动',

  // ── 运行中机器人 ──
  'dashboard.bots.title': '运行中机器人 ({n})',
  'dashboard.bots.enter-center': '进入机器人中心',
  'dashboard.bots.none': '暂无运行中的机器人',
  'dashboard.bots.unnamed': '未命名',
  'dashboard.bots.more': '还有 {n} 个运行中…',
}

const enUS: Record<string, string> = {
  // ── KPI Cards ──
  'dashboard.kpi.total-equity': 'Total Equity Value',
  'dashboard.kpi.today-pnl': "Today's PnL",
  'dashboard.kpi.win-rate': 'Win Rate',
  'dashboard.kpi.last-30d': 'Last 30 Days',
  'dashboard.kpi.profit-factor': 'Profit Factor',
  'dashboard.kpi.profit-factor-sub': 'Profit Factor',
  'dashboard.kpi.max-drawdown': 'Max Drawdown',
  'dashboard.kpi.historical-max': 'Historical Max',
  'dashboard.kpi.total-trades': 'Total Trades',
  'dashboard.kpi.trades-unit': 'trades',
  'dashboard.kpi.running-strategies': 'Running Strategies',
  'dashboard.kpi.total-strategies': '{n} total strategies',

  // ── Common status ──
  'dashboard.status.running': 'Running',
  'dashboard.status.normal': 'Normal',
  'dashboard.status.error': 'Error',
  'dashboard.status.deployed': 'Deployed',
  'dashboard.status.idle': 'Idle',
  'dashboard.status.online': 'Online',
  'dashboard.status.offline': 'Offline',

  // ── Chart cards ──
  'dashboard.chart.equity': 'Equity Curve',
  'dashboard.chart.realtime': 'Live',
  'dashboard.chart.daily-pnl-dist': 'Daily PnL Distribution',
  'dashboard.chart.profit': 'Profit',
  'dashboard.chart.loss': 'Loss',

  // ── Arbitrage performance ──
  'dashboard.arb.title': 'Arbitrage Performance',
  'dashboard.arb.view-monitor': 'Arbitrage Monitor',
  'dashboard.arb.total-pnl': 'Cumulative Arb PnL',
  'dashboard.arb.pnl-count': 'Win/Loss Trades',
  'dashboard.arb.win-loss-count': '{w} W / {l} L',
  'dashboard.arb.total-trades': 'Arbitrage Trades',
  'dashboard.arb.total-fees': 'Total Fees',
  'dashboard.arb.fee-value': 'Fee {v}',
  'dashboard.arb.win-rate': 'Arbitrage Win Rate',
  'dashboard.arb.best-worst': 'Best {w} / Worst {l}',
  'dashboard.arb.avg-pnl': 'Avg PnL per Trade',
  'dashboard.arb.avg-per-trade': 'Per-trade average',
  'dashboard.arb.equity-curve': 'Cumulative Arb Return Curve',
  'dashboard.arb.daily-pnl-dist': 'Daily Arb PnL Distribution',

  // ── Asset distribution ──
  'dashboard.asset.title': 'Asset Distribution',
  'dashboard.asset.spot': 'Spot',
  'dashboard.asset.futures': 'Futures',
  'dashboard.asset.funding': 'Funding',
  'dashboard.asset.earn': 'Earn',
  'dashboard.asset.total': 'Total',
  'dashboard.asset.no-data': 'No exchange data yet',
  'dashboard.asset.no-data-desc': 'Configure your exchange API in Settings',

  // ── PnL calendar ──
  'dashboard.calendar.title': 'PnL Calendar',
  'dashboard.calendar.prev-month': 'Previous month',
  'dashboard.calendar.next-month': 'Next month',
  'dashboard.calendar.today': 'Today',
  'dashboard.calendar.year-month': '{y}-{m}',
  'dashboard.calendar.win-days': '{n} wins',
  'dashboard.calendar.loss-days': '{n} losses',
  'dashboard.calendar.weekdays': 'S,M,T,W,T,F,S',

  // ── AI multi-agent ──
  'dashboard.ai.title': 'AI Multi-Agent Status',
  'dashboard.ai.agent-market': 'Market Intelligence',
  'dashboard.ai.agent-strategy': 'Strategy Generation',
  'dashboard.ai.agent-risk': 'Risk Control AI',
  'dashboard.ai.detail-signals': '{n} new signals',
  'dashboard.ai.detail-pending': '{n} strategies pending review',
  'dashboard.ai.detail-safe': 'All indicators safe',
  'dashboard.ai.waiting-data': 'Waiting for AI agent data...',

  // ── Running strategies / strategy row ──
  'dashboard.strategy.title': 'Running Strategies',
  'dashboard.strategy.no-running': 'No running strategies yet',
  'dashboard.strategy.no-running-desc': 'Launch your first strategy on the Strategies page',
  'dashboard.strategy.go-strategy': 'Go to Strategies',
  'dashboard.strategy.dir-long': 'Long',
  'dashboard.strategy.dir-short': 'Short',
  'dashboard.strategy.dir-both': 'Both',
  'dashboard.strategy.tp-full': 'Full TP',
  'dashboard.strategy.tp-tail': 'Tail',
  'dashboard.strategy.tp-head-tail': 'Head+Tail',
  'dashboard.strategy.tp-moving': 'Trailing',
  'dashboard.strategy.order-desc': '{n} orders · first {a}U',
  'dashboard.strategy.params-line': 'Gap {s}% · TP {p}%',
  'dashboard.strategy.open-macd-golden': 'MACD Golden Cross',
  'dashboard.strategy.open-macd-death': 'MACD Death Cross',
  'dashboard.strategy.open-ema': 'EMA Pivot',
  'dashboard.strategy.open-market': 'Market',

  // ── Strategy ranking ──
  'dashboard.ranking.title': 'Strategy Leaderboard',
  'dashboard.ranking.view-all': 'View All',
  'dashboard.ranking.col-strategy': 'Strategy',
  'dashboard.ranking.col-return': 'Return',
  'dashboard.ranking.win-rate': 'Win rate {v}%',
  'dashboard.ranking.sharpe': 'Sharpe {v}',
  'dashboard.ranking.no-data': 'No ranking data yet',
  'dashboard.ranking.no-data-desc': 'Run strategies to see the leaderboard',

  // ── Risk control ──
  'dashboard.risk.title': 'Risk Control Status',
  'dashboard.risk.view-center': 'View Risk Center',
  'dashboard.risk.global-protection': 'Global Trading Protection',
  'dashboard.risk.trading-suspended': 'Trading Suspended',
  'dashboard.risk.normal': 'Running Normally',
  'dashboard.risk.active-rules': 'Active Rules',
  'dashboard.risk.blocked-pairs': 'Blocked Pairs',
  'dashboard.risk.last-triggered': 'Last Triggered',
  'dashboard.risk.no-data': 'No risk control data',

  // ── ML model status ──
  'dashboard.ml.title': 'ML Model Status',
  'dashboard.ml.view-management': 'View Model Management',
  'dashboard.ml.model-count': 'Models',
  'dashboard.ml.latest-status': 'Latest Status',
  'dashboard.ml.feature-pipeline': 'Feature Pipeline',
  'dashboard.ml.service': 'ML Service',
  'dashboard.ml.features': 'features',
  'dashboard.ml.no-models': 'No trained models yet',

  // ── Setup guide ──
  'dashboard.guide.quick-start': 'Quick Start',
  'dashboard.guide.title': 'Create Your First Quant Strategy',
  'dashboard.guide.desc':
    'Complete {done}/{total} steps to start quantitative trading. Choose a strategy type, configure parameters, and launch live trading with one click.',
  'dashboard.guide.strategy-market': 'Strategy Market',
  'dashboard.guide.create-strategy': 'Create Strategy',
  'dashboard.guide.close-tip': 'Dismiss hint',
  'dashboard.guide.step-connect': 'Connect an exchange',
  'dashboard.guide.step-create': 'Create your first strategy',
  'dashboard.guide.step-launch': 'Launch auto trading',
  'dashboard.guide.action-settings': 'Go to Settings',
  'dashboard.guide.action-create': 'Create',
  'dashboard.guide.action-launch': 'Launch',

  // ── Running bots ──
  'dashboard.bots.title': 'Running Bots ({n})',
  'dashboard.bots.enter-center': 'Enter Bots Center',
  'dashboard.bots.none': 'No running bots',
  'dashboard.bots.unnamed': 'Unnamed',
  'dashboard.bots.more': '{n} more running…',
}

const ja: Record<string, string> = {
  // ── KPI カード ──
  'dashboard.kpi.total-equity': '総資産評価額',
  'dashboard.kpi.today-pnl': '今日の損益',
  'dashboard.kpi.win-rate': '勝率',
  'dashboard.kpi.last-30d': '過去30日',
  'dashboard.kpi.profit-factor': '損益比',
  'dashboard.kpi.profit-factor-sub': 'プロフィットファクター',
  'dashboard.kpi.max-drawdown': '最大ドローダウン',
  'dashboard.kpi.historical-max': '過去最大',
  'dashboard.kpi.total-trades': '総取引数',
  'dashboard.kpi.trades-unit': '件',
  'dashboard.kpi.running-strategies': '実行中戦略',
  'dashboard.kpi.total-strategies': '総戦略 {n} 件',

  // ── 共通ステータス ──
  'dashboard.status.running': '実行中',
  'dashboard.status.normal': '正常',
  'dashboard.status.error': '異常',
  'dashboard.status.deployed': 'デプロイ済み',
  'dashboard.status.idle': 'アイドル',
  'dashboard.status.online': 'オンライン',
  'dashboard.status.offline': 'オフライン',

  // ── チャートカード ──
  'dashboard.chart.equity': '資産曲線',
  'dashboard.chart.realtime': 'リアルタイム',
  'dashboard.chart.daily-pnl-dist': '日次損益分布',
  'dashboard.chart.profit': '利益',
  'dashboard.chart.loss': '損失',

  // ── 裁定パフォーマンス ──
  'dashboard.arb.title': '裁定取引パフォーマンス',
  'dashboard.arb.view-monitor': '裁定モニター',
  'dashboard.arb.total-pnl': '累計裁定損益',
  'dashboard.arb.pnl-count': '損益回数',
  'dashboard.arb.win-loss-count': '{w} 勝 / {l} 敗',
  'dashboard.arb.total-trades': '裁定取引回数',
  'dashboard.arb.total-fees': '総手数料',
  'dashboard.arb.fee-value': '手数料 {v}',
  'dashboard.arb.win-rate': '裁定勝率',
  'dashboard.arb.best-worst': '最高 {w} / 最低 {l}',
  'dashboard.arb.avg-pnl': '1注文平均損益',
  'dashboard.arb.avg-per-trade': '注文あたり平均',
  'dashboard.arb.equity-curve': '裁定累計収益曲線',
  'dashboard.arb.daily-pnl-dist': '裁定日次損益分布',

  // ── 資産分布 ──
  'dashboard.asset.title': '資産分布',
  'dashboard.asset.spot': '現物',
  'dashboard.asset.futures': '先物',
  'dashboard.asset.funding': '資金',
  'dashboard.asset.earn': '運用',
  'dashboard.asset.total': '合計',
  'dashboard.asset.no-data': '取引所データがありません',
  'dashboard.asset.no-data-desc': '設定ページで取引所 API を構成してください',

  // ── 損益カレンダー ──
  'dashboard.calendar.title': '損益カレンダー',
  'dashboard.calendar.prev-month': '前の月',
  'dashboard.calendar.next-month': '次の月',
  'dashboard.calendar.today': '今日',
  'dashboard.calendar.year-month': '{y}年{m}月',
  'dashboard.calendar.win-days': '{n} 日勝ち',
  'dashboard.calendar.loss-days': '{n} 日負け',
  'dashboard.calendar.weekdays': '日,月,火,水,木,金,土',

  // ── AI マルチエージェント ──
  'dashboard.ai.title': 'AI マルチエージェント状態',
  'dashboard.ai.agent-market': '市場情報',
  'dashboard.ai.agent-strategy': '戦略生成',
  'dashboard.ai.agent-risk': 'リスク管理AI',
  'dashboard.ai.detail-signals': '新規シグナル {n} 件',
  'dashboard.ai.detail-pending': '承認待ち戦略 {n} 件',
  'dashboard.ai.detail-safe': '全指標は正常です',
  'dashboard.ai.waiting-data': 'AI エージェントのデータを待機中...',

  // ── 実行中戦略 / 戦略行 ──
  'dashboard.strategy.title': '実行中の戦略',
  'dashboard.strategy.no-running': '実行中の戦略はありません',
  'dashboard.strategy.no-running-desc': '戦略ページで最初の戦略を開始してください',
  'dashboard.strategy.go-strategy': '戦略ページへ',
  'dashboard.strategy.dir-long': 'ロング',
  'dashboard.strategy.dir-short': 'ショート',
  'dashboard.strategy.dir-both': '両方向',
  'dashboard.strategy.tp-full': '全額利確',
  'dashboard.strategy.tp-tail': '末尾',
  'dashboard.strategy.tp-head-tail': '先頭+末尾',
  'dashboard.strategy.tp-moving': 'トレーリング',
  'dashboard.strategy.order-desc': '{n} 注文・初回 {a}U',
  'dashboard.strategy.params-line': '追加幅 {s}%・利確 {p}%',
  'dashboard.strategy.open-macd-golden': 'MACDゴールデンクロス',
  'dashboard.strategy.open-macd-death': 'MACDデッドクロス',
  'dashboard.strategy.open-ema': 'EMA転換点',
  'dashboard.strategy.open-market': '成行',

  // ── 戦略ランキング ──
  'dashboard.ranking.title': '戦略ランキング',
  'dashboard.ranking.view-all': 'すべて',
  'dashboard.ranking.col-strategy': '戦略',
  'dashboard.ranking.col-return': '収益',
  'dashboard.ranking.win-rate': '勝率 {v}%',
  'dashboard.ranking.sharpe': 'シャープ {v}',
  'dashboard.ranking.no-data': 'ランキングデータがありません',
  'dashboard.ranking.no-data-desc': '戦略を実行するとランキングが表示されます',

  // ── リスク管理 ──
  'dashboard.risk.title': 'リスク管理ステータス',
  'dashboard.risk.view-center': 'リスク管理センターへ',
  'dashboard.risk.global-protection': 'グローバル取引保護',
  'dashboard.risk.trading-suspended': '取引停止中',
  'dashboard.risk.normal': '正常に稼働中',
  'dashboard.risk.active-rules': '有効なルール',
  'dashboard.risk.blocked-pairs': 'ブロック中ペア',
  'dashboard.risk.last-triggered': '最近のトリガー',
  'dashboard.risk.no-data': 'リスク管理データがありません',

  // ── ML モデルステータス ──
  'dashboard.ml.title': 'ML モデルステータス',
  'dashboard.ml.view-management': 'モデル管理へ',
  'dashboard.ml.model-count': 'モデル数',
  'dashboard.ml.latest-status': '最新ステータス',
  'dashboard.ml.feature-pipeline': '特徴量パイプライン',
  'dashboard.ml.service': 'ML サービス',
  'dashboard.ml.features': '特徴量',
  'dashboard.ml.no-models': '学習済みモデルがありません',

  // ── クイックスタート ──
  'dashboard.guide.quick-start': 'クイックスタート',
  'dashboard.guide.title': '最初の量化戦略を作成',
  'dashboard.guide.desc':
    '{done}/{total} ステップを完了すると自動売買を開始できます。戦略タイプを選び、パラメータを設定して、ワンクリックで実取引を開始します。',
  'dashboard.guide.strategy-market': '戦略マーケット',
  'dashboard.guide.create-strategy': '戦略を作成',
  'dashboard.guide.close-tip': 'ヒントを閉じる',
  'dashboard.guide.step-connect': '取引所を接続',
  'dashboard.guide.step-create': '最初の戦略を作成',
  'dashboard.guide.step-launch': '自動売買を開始',
  'dashboard.guide.action-settings': '設定へ',
  'dashboard.guide.action-create': '作成',
  'dashboard.guide.action-launch': '開始',

  // ── 実行中ボット ──
  'dashboard.bots.title': '実行中ボット ({n})',
  'dashboard.bots.enter-center': 'ボットセンターへ',
  'dashboard.bots.none': '実行中のボットはありません',
  'dashboard.bots.unnamed': '未命名',
  'dashboard.bots.more': '他 {n} 件実行中…',
}

registerLocale('zh-CN', zhCN)
registerLocale('en-US', enUS)
registerLocale('ja', ja)
