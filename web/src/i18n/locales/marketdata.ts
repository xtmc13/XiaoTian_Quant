import { registerLocale } from '../index'

/**
 * 市场数据页（/market-data）文案 —— market.* 扁平 key，自注册进 i18n 核心。
 * 对应 gateway /api/dataproviders/* 数据生态。
 */
const zhCN: Record<string, string> = {
  'market.title': '市场数据',
  'market.subtitle': '情绪 · 宏观 · 新闻 · 热力图 · 经济日历',
  'market.refresh': '刷新',
  'market.loading': '加载中...',

  // 恐惧贪婪
  'market.fearGreed.title': '恐惧贪婪指数',
  'market.fearGreed.extremeFear': '极度恐惧',
  'market.fearGreed.fear': '恐惧',
  'market.fearGreed.neutral': '中性',
  'market.fearGreed.greed': '贪婪',
  'market.fearGreed.extremeGreed': '极度贪婪',
  'market.fearGreed.history7d': '近 7 日走势',

  // 衍生品情绪
  'market.derivatives.title': '衍生品情绪',
  'market.derivatives.funding': '资金费率',
  'market.derivatives.longShort': '多空比',
  'market.derivatives.liquidations': '24h 爆仓',
  'market.derivatives.long': '多',
  'market.derivatives.short': '空',

  // 宏观
  'market.macro.title': '宏观序列 (FRED)',
  'market.macro.latest': '最新值',
  'market.macro.date': '日期',
  'market.macro.notConfigured': '未配置 FRED_API_KEY，宏观数据不可用',

  // 新闻
  'market.news.title': '加密新闻',
  'market.news.filterPlaceholder': '按币种过滤，如 BTC',
  'market.news.all': '全部',
  'market.news.empty': '暂无相关新闻',

  // 热力图
  'market.heatmap.title': '市值热力图（24h）',
  'market.heatmap.change': '涨跌幅',
  'market.heatmap.volume': '成交额',

  // 经济日历
  'market.calendar.title': '本周经济日历',
  'market.calendar.event': '事件',
  'market.calendar.time': '时间',
  'market.calendar.impact': '重要性',
  'market.calendar.forecast': '预期',
  'market.calendar.previous': '前值',
  'market.calendar.impactHigh': '高',
  'market.calendar.impactMedium': '中',
  'market.calendar.impactLow': '低',
  'market.calendar.impactHoliday': '假日',
  'market.calendar.empty': '本周暂无重要事件',

  // 源健康
  'market.sources.title': '数据源状态',
  'market.sources.ok': '正常',
  'market.sources.stale': '过期缓存',
  'market.sources.noData': '暂无数据',
  'market.sources.notConfigured': '未配置',
  'market.sources.circuit': '熔断',
  'market.sources.circuitOpen': '已开闸',
  'market.sources.circuitHalfOpen': '半开',
  'market.sources.circuitClosed': '闭合',
  'market.sources.requiresKey': '需 API Key',

  // 降级提示
  'market.degraded.stale': '上游暂不可用，展示缓存数据',
  'market.degraded.unavailable': '数据暂不可用',
  'market.degraded.notConfigured': '该数据源未配置 API Key',
  'market.degraded.refreshing': '后台刷新中...',
}

const enUS: Record<string, string> = {
  'market.title': 'Market Data',
  'market.subtitle': 'Sentiment · Macro · News · Heatmap · Economic Calendar',
  'market.refresh': 'Refresh',
  'market.loading': 'Loading...',

  'market.fearGreed.title': 'Fear & Greed Index',
  'market.fearGreed.extremeFear': 'Extreme Fear',
  'market.fearGreed.fear': 'Fear',
  'market.fearGreed.neutral': 'Neutral',
  'market.fearGreed.greed': 'Greed',
  'market.fearGreed.extremeGreed': 'Extreme Greed',
  'market.fearGreed.history7d': 'Last 7 days',

  'market.derivatives.title': 'Derivatives Sentiment',
  'market.derivatives.funding': 'Funding Rate',
  'market.derivatives.longShort': 'Long/Short Ratio',
  'market.derivatives.liquidations': '24h Liquidations',
  'market.derivatives.long': 'Long',
  'market.derivatives.short': 'Short',

  'market.macro.title': 'Macro Series (FRED)',
  'market.macro.latest': 'Latest',
  'market.macro.date': 'Date',
  'market.macro.notConfigured': 'FRED_API_KEY not configured; macro data unavailable',

  'market.news.title': 'Crypto News',
  'market.news.filterPlaceholder': 'Filter by coin, e.g. BTC',
  'market.news.all': 'All',
  'market.news.empty': 'No related news',

  'market.heatmap.title': 'Market Cap Heatmap (24h)',
  'market.heatmap.change': 'Change',
  'market.heatmap.volume': 'Volume',

  'market.calendar.title': 'This Week Economic Calendar',
  'market.calendar.event': 'Event',
  'market.calendar.time': 'Time',
  'market.calendar.impact': 'Impact',
  'market.calendar.forecast': 'Forecast',
  'market.calendar.previous': 'Previous',
  'market.calendar.impactHigh': 'High',
  'market.calendar.impactMedium': 'Medium',
  'market.calendar.impactLow': 'Low',
  'market.calendar.impactHoliday': 'Holiday',
  'market.calendar.empty': 'No major events this week',

  'market.sources.title': 'Data Sources',
  'market.sources.ok': 'OK',
  'market.sources.stale': 'Stale',
  'market.sources.noData': 'No data',
  'market.sources.notConfigured': 'Not configured',
  'market.sources.circuit': 'Circuit',
  'market.sources.circuitOpen': 'Open',
  'market.sources.circuitHalfOpen': 'Half-open',
  'market.sources.circuitClosed': 'Closed',
  'market.sources.requiresKey': 'API key required',

  'market.degraded.stale': 'Upstream unavailable; showing cached data',
  'market.degraded.unavailable': 'Data temporarily unavailable',
  'market.degraded.notConfigured': 'API key not configured for this source',
  'market.degraded.refreshing': 'Refreshing in background...',
}

const ja: Record<string, string> = {
  'market.title': 'マーケットデータ',
  'market.subtitle': 'センチメント · マクロ · ニュース · ヒートマップ · 経済カレンダー',
  'market.refresh': '更新',
  'market.loading': '読み込み中...',

  'market.fearGreed.title': '恐怖・貪欲指数',
  'market.fearGreed.extremeFear': '極度の恐怖',
  'market.fearGreed.fear': '恐怖',
  'market.fearGreed.neutral': '中立',
  'market.fearGreed.greed': '貪欲',
  'market.fearGreed.extremeGreed': '極度の貪欲',
  'market.fearGreed.history7d': '過去7日間',

  'market.derivatives.title': 'デリバティブセンチメント',
  'market.derivatives.funding': '資金調達率',
  'market.derivatives.longShort': 'ロング/ショート比率',
  'market.derivatives.liquidations': '24時間清算',
  'market.derivatives.long': 'ロング',
  'market.derivatives.short': 'ショート',

  'market.macro.title': 'マクロ系列 (FRED)',
  'market.macro.latest': '最新値',
  'market.macro.date': '日付',
  'market.macro.notConfigured': 'FRED_API_KEY 未設定のためマクロデータは利用できません',

  'market.news.title': '暗号資産ニュース',
  'market.news.filterPlaceholder': '銘柄で絞り込み（例: BTC）',
  'market.news.all': 'すべて',
  'market.news.empty': '関連ニュースがありません',

  'market.heatmap.title': '時価総額ヒートマップ（24時間）',
  'market.heatmap.change': '変動率',
  'market.heatmap.volume': '出来高',

  'market.calendar.title': '今週の経済カレンダー',
  'market.calendar.event': 'イベント',
  'market.calendar.time': '時刻',
  'market.calendar.impact': '重要度',
  'market.calendar.forecast': '予想',
  'market.calendar.previous': '前回',
  'market.calendar.impactHigh': '高',
  'market.calendar.impactMedium': '中',
  'market.calendar.impactLow': '低',
  'market.calendar.impactHoliday': '祝日',
  'market.calendar.empty': '今週の重要イベントはありません',

  'market.sources.title': 'データソース状態',
  'market.sources.ok': '正常',
  'market.sources.stale': '古いキャッシュ',
  'market.sources.noData': 'データなし',
  'market.sources.notConfigured': '未設定',
  'market.sources.circuit': 'サーキット',
  'market.sources.circuitOpen': 'オープン',
  'market.sources.circuitHalfOpen': 'ハーフオープン',
  'market.sources.circuitClosed': 'クローズ',
  'market.sources.requiresKey': 'API キー必須',

  'market.degraded.stale': '上流が一時的に利用不可。キャッシュを表示中',
  'market.degraded.unavailable': 'データは一時的に利用できません',
  'market.degraded.notConfigured': 'このソースの API キーが未設定です',
  'market.degraded.refreshing': 'バックグラウンドで更新中...',
}

registerLocale('zh-CN', zhCN)
registerLocale('en-US', enUS)
registerLocale('ja', ja)
