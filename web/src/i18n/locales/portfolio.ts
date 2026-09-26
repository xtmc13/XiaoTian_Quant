import { registerLocale, flatten } from '../index'

/* ── 资产/持仓页（已平仓持仓列表 + 交易分享卡入口） ── */

const zh = flatten({
  portfolio: {
    closed: {
      title: '已平仓持仓',
      symbol: '交易对',
      side: '方向',
      long: '多',
      short: '空',
      entry: '开仓价',
      exit: '平仓价',
      pnl: '盈亏额',
      pnlPct: '盈亏%',
      closedAt: '平仓时间',
      actions: '操作',
      share: '分享',
      shareFail: '分享卡生成失败',
      empty: '暂无已平仓持仓',
      emptyDesc: '平仓后的持仓会出现在这里，可生成分享卡',
      prev: '上一页',
      next: '下一页',
      page: '第 {page} 页',
    },
  },
})

const en = flatten({
  portfolio: {
    closed: {
      title: 'Closed Positions',
      symbol: 'Pair',
      side: 'Side',
      long: 'Long',
      short: 'Short',
      entry: 'Entry',
      exit: 'Exit',
      pnl: 'PnL',
      pnlPct: 'PnL %',
      closedAt: 'Closed At',
      actions: 'Actions',
      share: 'Share',
      shareFail: 'Failed to generate share card',
      empty: 'No closed positions',
      emptyDesc: 'Closed positions appear here and can be shared as cards',
      prev: 'Prev',
      next: 'Next',
      page: 'Page {page}',
    },
  },
})

const ja = flatten({
  portfolio: {
    closed: {
      title: '決済済みポジション',
      symbol: '通貨ペア',
      side: '方向',
      long: 'ロング',
      short: 'ショート',
      entry: 'エントリー価格',
      exit: '決済価格',
      pnl: '損益',
      pnlPct: '損益%',
      closedAt: '決済日時',
      actions: '操作',
      share: '共有',
      shareFail: '共有カードの生成に失敗しました',
      empty: '決済済みポジションはありません',
      emptyDesc: '決済されたポジションはここに表示され、共有カードを生成できます',
      prev: '前へ',
      next: '次へ',
      page: '{page} ページ目',
    },
  },
})

registerLocale('zh-CN', zh)
registerLocale('en-US', en)
registerLocale('ja', ja)
