package marketplace

import (
	"errors"
	"fmt"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 市场条目订阅（迁移 0059 扣费链路）──
// 订阅关系存 ai_bot_subscriptions（listing_id 非空行），价格字段一律取
// 条目快照（FeeModel/FeePercent/MonthlyFee），请求体价格无效。
// free/profit_share 直接 active（无到期，分成在平仓盈亏时计提）；
// monthly 置 pending_payment，billing 订单链上核验 paid 后激活并顺延。

// SubscriptionPeriodDays 市场条目订阅每期天数（月费）。
const SubscriptionPeriodDays = 30

var (
	// ErrListingNotFound 条目不存在（API 层映射 404）。
	ErrListingNotFound = errors.New("listing not found")
	// ErrListingNotSubscribable 条目非 listed 状态不可订阅（API 层映射 409）。
	ErrListingNotSubscribable = errors.New("listing is not subscribable")
	// ErrSelfSubscription 作者不可订阅自己的条目（API 层映射 400）。
	ErrSelfSubscription = errors.New("author cannot subscribe own listing")
)

// Subscribe 创建 userID 对 listingID 的订阅（幂等）。
// 已有 active/pending_payment 订阅时返回既有行（created=false），不重复创建。
// monthly 条目返回 pending_payment 订阅，计费订单由调用方（handler 计费层）创建。
func (s *Service) Subscribe(userID int64, listingID string) (sub *store.MarketSubscriptionRecord, created bool, err error) {
	l := s.repo.GetByID(listingID)
	if l == nil {
		return nil, false, ErrListingNotFound
	}
	if l.Status != StatusListed {
		return nil, false, fmt.Errorf("%w: status=%s", ErrListingNotSubscribable, l.Status)
	}
	if l.AuthorUserID == userID {
		return nil, false, ErrSelfSubscription
	}
	if cur := store.FindMarketSubscription(int(userID), l.ID); cur != nil {
		return cur, false, nil
	}
	status := store.MarketSubActive
	if l.FeeModel == "monthly" {
		status = store.MarketSubPendingPayment
	}
	rec := &store.MarketSubscriptionRecord{
		UserID:     int(userID),
		ListingID:  l.ID,
		FeeType:    l.FeeModel,
		FeePercent: l.FeePercent,
		MonthlyFee: l.MonthlyFee,
		Status:     status,
	}
	if _, err := store.CreateMarketSubscription(rec); err != nil {
		return nil, false, err
	}
	return rec, true, nil
}

// ExpireSubscriptions 到期治理：active 且 expires_at 已过的订阅置 expired。
// 由 Engine 小时级 tick 驱动；返回处理条数。
func (s *Service) ExpireSubscriptions(nowUnix int64) (int64, error) {
	return store.ExpireMarketSubscriptions(nowUnix)
}
