import { registerLocale, flatten } from '../index'

/* ── 交易页（合约/现货下单面板增量词条；存量 trading.* 在 zh-CN.ts/en-US.ts） ── */

const zh = flatten({
  trading: {
    cancelAll: '全部撤单',
    cancelAllPending: '撤单中...',
    cancelAllOk: '已提交全部撤单',
  },
})

const en = flatten({
  trading: {
    cancelAll: 'Cancel All',
    cancelAllPending: 'Cancelling...',
    cancelAllOk: 'All orders submitted for cancellation',
  },
})

const ja = flatten({
  trading: {
    cancelAll: '全注文取消',
    cancelAllPending: '取消中...',
    cancelAllOk: '全注文の取消を送信しました',
  },
})

registerLocale('zh-CN', zh)
registerLocale('en-US', en)
registerLocale('ja', ja)
