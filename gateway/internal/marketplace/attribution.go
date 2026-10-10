package marketplace

import (
	"fmt"

	"github.com/xiaotian-quant/gateway/internal/social"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 利润分成归因闭环（迁移 0060）──────────────────────────────────
// 三段接线共享的小模块：
//   1. 建档：条目 approve 时作者在 xt_social_providers 无记录则自动建档
//      （直接 approved——条目本身已过人工审核），provider_id 回写条目；
//   2. 实例归因：订阅实例化克隆的机器人（origin_listing_id 非空）平仓时，
//      校验订阅者对该条目仍有 active profit_share 订阅，然后落
//      xt_social_copy_pnl（结算引擎日窗汇总的数据源；亏损落负数行，note 打
//      loss 标，由结算引擎冲抵 pending 快照）；
//   3. 信号源归因：executor 平仓计提 pending_share 处，属主已建档时同步落
//      xt_social_copy_pnl（pending_share 字段保留兼容）。
// 归因失败一律由调用方记日志，不阻断平仓主流程。分成金额由结算引擎按
// provider 的 profit_share_pct 计算（调用方只记原始 pnl，不算比例）。

// clampSharePct 把条目分成比例钳进 B 栈双轨区间（10-35）；<=0 返回 nil
// （不分成，provider 建档后分成结算自然空转）。
func clampSharePct(feePercent float64) *float64 {
	if feePercent <= 0 {
		return nil
	}
	v := feePercent
	if v < social.DualTrackMinSharePct {
		v = social.DualTrackMinSharePct
	}
	if v > social.DualTrackMaxSharePct {
		v = social.DualTrackMaxSharePct
	}
	return &v
}

// EnsureListingProvider 保证条目作者在 xt_social_providers 有建档并把
// provider_id 回写条目（立即持久化，幂等）。已有记录（无论审核状态）直接
// 复用最新一条，不重复建档；无记录则按条目收费快照建档：name 取作者昵称
// （回落条目名），分成比例按条目快照钳 10-35，月费同条目。
func EnsureListingProvider(l *store.MarketListingRecord) (int64, error) {
	if l == nil {
		return 0, fmt.Errorf("nil listing")
	}
	if l.ProviderID > 0 {
		return l.ProviderID, nil
	}
	repo := store.NewMarketListingRepo()
	mkt := social.NewMarketService()
	if p, err := mkt.LatestProviderByUser(l.AuthorUserID); err == nil && p != nil {
		l.ProviderID = p.ID
		return p.ID, repo.Update(l)
	}
	name := l.Name
	if u := store.FindUserByID(int(l.AuthorUserID)); u != nil {
		if nick, _ := u["nickname"].(string); nick != "" {
			name = nick
		}
	}
	p, err := mkt.ApplyProviderWithPricing(l.AuthorUserID, name, l.Description, l.MonthlyFee, clampSharePct(l.FeePercent), "")
	if err != nil {
		return 0, fmt.Errorf("apply provider: %w", err)
	}
	if _, err := mkt.ApproveProvider(p.ID); err != nil {
		return 0, fmt.Errorf("approve provider %d: %w", p.ID, err)
	}
	l.ProviderID = p.ID
	return p.ID, repo.Update(l)
}

// ensureShareTrack 只在 follower 对该 provider 没有任何 active 订阅时补建
// 分成轨订阅行——结算引擎的作用范围取自 xt_social_subscriptions，而市场条目
// 订阅存在 ai_bot_subscriptions，两边都要就位闭环才通。已有 active 行不动
// （不覆盖既有订阅轨/切轨语义）。
func ensureShareTrack(mkt *social.MarketService, providerID, followerUserID int64) error {
	if _, err := mkt.GetSubscription(providerID, followerUserID); err == nil {
		return nil
	} else if !social.IsNotFound(err) {
		return err
	}
	return mkt.UpsertSubscriptionTrack(providerID, followerUserID, social.TrackProfitShare, 0)
}

// lossNote 给亏损归因行的 note 打 loss 标（盈亏分流按 pnl 符号，note 仅供审计）。
func lossNote(pnl float64, note string) string {
	if pnl >= 0 {
		return note
	}
	if note == "" {
		return "loss"
	}
	return "loss:" + note
}

// AttributeListingPnL 把订阅者克隆实例的一笔已实现盈亏归因到条目 provider 的
// 钱轨（亏损落负数行，结算引擎日窗汇总后冲抵 pending 快照）。静默跳过：
// 无溯源标记、条目未建档、订阅者无 active profit_share 订阅、pnl=0。
// 其余错误返回给调用方记日志。
func AttributeListingPnL(listingID string, subscriberUserID int64, pnl float64, note string) error {
	if listingID == "" || subscriberUserID <= 0 || pnl == 0 {
		return nil
	}
	l := store.NewMarketListingRepo().GetByID(listingID)
	if l == nil || l.ProviderID <= 0 {
		return nil
	}
	sub := store.FindMarketSubscription(int(subscriberUserID), l.ID)
	if sub == nil || sub.Status != store.MarketSubActive || sub.FeeType != "profit_share" || sub.FeePercent <= 0 {
		return nil
	}
	mkt := social.NewMarketService()
	if err := ensureShareTrack(mkt, l.ProviderID, subscriberUserID); err != nil {
		return err
	}
	return mkt.RecordCopyPnL(l.ProviderID, subscriberUserID, pnl, lossNote(pnl, note))
}

// AttributeSignalSourcePnL executor 孤岛收口：信号源属主在 xt_social_providers
// 有 approved 建档时，把平仓盈亏镜像到钱轨（亏损落负数行供结算冲抵）。
// 覆盖范围与 AddPendingShare 一致（该信号源全部 active profit_share 订阅者）；
// pending_share 计提保持不动。
func AttributeSignalSourcePnL(src *store.SignalSource, pnl float64, note string) error {
	if src == nil || pnl == 0 || src.FeeModel != "profit_share" || src.FeePercent <= 0 {
		return nil
	}
	mkt := social.NewMarketService()
	p, err := mkt.LatestProviderByUser(int64(src.OwnerUserID))
	if err != nil {
		if social.IsNotFound(err) {
			return nil
		}
		return err
	}
	if p == nil || p.ApplyStatus != social.ApplyApproved {
		return nil
	}
	subs, err := store.NewSignalSourceRepo().ListSubscriptionsBySource(src.ID)
	if err != nil {
		return err
	}
	for _, sub := range subs {
		if sub.Status != "active" || sub.FeeModel != "profit_share" {
			continue
		}
		if err := ensureShareTrack(mkt, p.ID, int64(sub.UserID)); err != nil {
			return err
		}
		if err := mkt.RecordCopyPnL(p.ID, int64(sub.UserID), pnl, lossNote(pnl, note)); err != nil {
			return err
		}
	}
	return nil
}
