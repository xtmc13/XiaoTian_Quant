import { registerLocale } from '../index'

/**
 * 社区域文案 —— community.* / author.* 扁平 key，自注册进 i18n 核心。
 * 覆盖：社区指标上架审核（admin，reviews/pending + review/:id，与 market/listings 两套）、
 * 作者后台收益卡（author/revenue）。动态文案用 {placeholder} 插值。
 */
const zhCN: Record<string, string> = {
  'community.review.title': '社区指标审核',
  'community.review.subtitle': '审核作者提交上架的指标（reviews/pending）',
  'community.review.empty': '暂无待审指标',
  'community.review.approve': '通过',
  'community.review.reject': '拒绝',
  'community.review.approveConfirmTitle': '通过上架',
  'community.review.approveConfirmMsg': '确认通过指标「{name}」上架社区市场？',
  'community.review.rejectTitle': '拒绝上架',
  'community.review.rejectReasonLabel': '拒绝原因（作者可见，可留空）',
  'community.review.approveOk': '已通过上架',
  'community.review.rejectOk': '已拒绝',
  'community.review.actionFail': '操作失败',
  'community.review.author': '作者',
  'community.review.free': '免费',
  'community.review.submittedAt': '提交时间',

  'author.revenue.title': '作者收益',
  'author.revenue.total': '累计收益',
  'author.revenue.sales': '累计销量',
  'author.revenue.detailTitle': '分指标明细',
  'author.revenue.indicatorCol': '指标',
  'author.revenue.salesCol': '销量',
  'author.revenue.revenueCol': '收益',
  'author.revenue.empty': '暂无收益记录',
}

const enUS: Record<string, string> = {
  'community.review.title': 'Community Indicator Review',
  'community.review.subtitle': 'Review indicators submitted for listing (reviews/pending)',
  'community.review.empty': 'No pending indicators',
  'community.review.approve': 'Approve',
  'community.review.reject': 'Reject',
  'community.review.approveConfirmTitle': 'Approve listing',
  'community.review.approveConfirmMsg': 'Approve indicator "{name}" for the community market?',
  'community.review.rejectTitle': 'Reject listing',
  'community.review.rejectReasonLabel': 'Rejection reason (visible to the author, optional)',
  'community.review.approveOk': 'Approved and listed',
  'community.review.rejectOk': 'Rejected',
  'community.review.actionFail': 'Action failed',
  'community.review.author': 'Author',
  'community.review.free': 'Free',
  'community.review.submittedAt': 'Submitted',

  'author.revenue.title': 'Author Revenue',
  'author.revenue.total': 'Total revenue',
  'author.revenue.sales': 'Total sales',
  'author.revenue.detailTitle': 'Per-indicator breakdown',
  'author.revenue.indicatorCol': 'Indicator',
  'author.revenue.salesCol': 'Sales',
  'author.revenue.revenueCol': 'Revenue',
  'author.revenue.empty': 'No revenue yet',
}

const ja: Record<string, string> = {
  'community.review.title': 'コミュニティ指標レビュー',
  'community.review.subtitle': '出品申請された指標を審査します（reviews/pending）',
  'community.review.empty': '審査待ちの指標はありません',
  'community.review.approve': '承認',
  'community.review.reject': '却下',
  'community.review.approveConfirmTitle': '出品を承認',
  'community.review.approveConfirmMsg': '指標「{name}」のコミュニティ出品を承認しますか？',
  'community.review.rejectTitle': '出品を却下',
  'community.review.rejectReasonLabel': '却下理由（作者に表示、任意）',
  'community.review.approveOk': '承認して出品しました',
  'community.review.rejectOk': '却下しました',
  'community.review.actionFail': '操作に失敗しました',
  'community.review.author': '作者',
  'community.review.free': '無料',
  'community.review.submittedAt': '申請日時',

  'author.revenue.title': '作者収益',
  'author.revenue.total': '累計収益',
  'author.revenue.sales': '累計販売数',
  'author.revenue.detailTitle': '指標別明細',
  'author.revenue.indicatorCol': '指標',
  'author.revenue.salesCol': '販売数',
  'author.revenue.revenueCol': '収益',
  'author.revenue.empty': '収益記録はまだありません',
}

registerLocale('zh-CN', zhCN)
registerLocale('en-US', enUS)
registerLocale('ja', ja)
