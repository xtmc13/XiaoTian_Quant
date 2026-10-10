package store

import (
	"fmt"
	"time"
)

// ── 市场条目订阅（迁移 0059）──
// 复用 ai_bot_subscriptions 表：listing_id 非空即市场条目订阅行
// （区别于 bot_instance_id 直连的实例订阅）。价格字段（fee_type/fee_percent/
// monthly_fee）是订阅创建时从条目快照的取值，服务端权威，请求体不可改。

// 市场条目订阅状态机：pending_payment ──paid──▶ active ──到期──▶ expired
// （任意状态可 cancel → cancelled；已付不退，到期失效）。
const (
	MarketSubActive         = "active"
	MarketSubPendingPayment = "pending_payment"
	MarketSubExpired        = "expired"
	MarketSubCancelled      = "cancelled"
)

// MarketSubscriptionRecord 市场条目订阅（ai_bot_subscriptions 的 listing 维度行）。
type MarketSubscriptionRecord struct {
	ID            int     `json:"id"`
	UserID        int     `json:"user_id"`
	BotInstanceID string  `json:"bot_instance_id"`
	ListingID     string  `json:"listing_id"`
	FeeType       string  `json:"fee_type"` // free | monthly | profit_share（条目快照）
	FeePercent    float64 `json:"fee_percent"`
	MonthlyFee    float64 `json:"monthly_fee"`
	Status        string  `json:"status"`     // active|pending_payment|expired|cancelled
	ExpiresAt     int64   `json:"expires_at"` // Unix 秒；0 = 无到期
	CreatedAt     int64   `json:"created_at"`
}

const marketSubscriptionCols = `id, user_id, bot_instance_id, listing_id, fee_type, fee_percent, monthly_fee, status, expires_at, created_at`

func scanMarketSubscription(row interface{ Scan(...any) error }) (*MarketSubscriptionRecord, error) {
	var r MarketSubscriptionRecord
	if err := row.Scan(&r.ID, &r.UserID, &r.BotInstanceID, &r.ListingID, &r.FeeType,
		&r.FeePercent, &r.MonthlyFee, &r.Status, &r.ExpiresAt, &r.CreatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateMarketSubscription 落库新订阅（价格字段由调用方从条目快照取定）。
func CreateMarketSubscription(rec *MarketSubscriptionRecord) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("db not initialized")
	}
	if rec.CreatedAt == 0 {
		rec.CreatedAt = time.Now().Unix()
	}
	res, err := db.Exec(`INSERT INTO ai_bot_subscriptions
		(user_id, bot_instance_id, listing_id, fee_type, fee_percent, monthly_fee, next_billing_at, status, expires_at, created_at)
		VALUES (?,?,?,?,?,?,0,?,?,?)`,
		rec.UserID, rec.BotInstanceID, rec.ListingID, rec.FeeType, rec.FeePercent, rec.MonthlyFee,
		rec.Status, rec.ExpiresAt, rec.CreatedAt)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	rec.ID = int(id)
	return rec.ID, nil
}

// GetMarketSubscriptionByID 按 id 查订阅；不存在返回 nil。
func GetMarketSubscriptionByID(id int) *MarketSubscriptionRecord {
	if db == nil {
		return nil
	}
	rec, err := scanMarketSubscription(db.QueryRow(`SELECT `+marketSubscriptionCols+`
		FROM ai_bot_subscriptions WHERE id=?`, id))
	if err != nil {
		return nil
	}
	return rec
}

// FindMarketSubscription 查用户对条目的进行中订阅（active/pending_payment），
// 供订阅下单幂等：已有进行中订阅时不重复创建。无则返回 nil。
func FindMarketSubscription(userID int, listingID string) *MarketSubscriptionRecord {
	if db == nil {
		return nil
	}
	rec, err := scanMarketSubscription(db.QueryRow(`SELECT `+marketSubscriptionCols+`
		FROM ai_bot_subscriptions
		WHERE user_id=? AND listing_id=? AND status IN (?, ?)
		ORDER BY created_at DESC LIMIT 1`,
		userID, listingID, MarketSubActive, MarketSubPendingPayment))
	if err != nil {
		return nil
	}
	return rec
}

// ListMarketSubscriptionsByUser 当前用户全部市场条目订阅（新→旧）。
func ListMarketSubscriptionsByUser(userID int) []*MarketSubscriptionRecord {
	out := []*MarketSubscriptionRecord{}
	if db == nil {
		return out
	}
	rows, err := db.Query(`SELECT `+marketSubscriptionCols+`
		FROM ai_bot_subscriptions WHERE user_id=? AND listing_id != '' ORDER BY created_at DESC`, userID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		rec, err := scanMarketSubscription(rows)
		if err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// ExpireMarketSubscriptions 到期治理：active 且 expires_at 已过的市场条目订阅
// 置 expired（expires_at=0 的 free/profit_share 订阅无到期，不受影响）。
func ExpireMarketSubscriptions(now int64) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("db not initialized")
	}
	res, err := db.Exec(`UPDATE ai_bot_subscriptions SET status=?
		WHERE listing_id != '' AND status=? AND expires_at > 0 AND expires_at <= ?`,
		MarketSubExpired, MarketSubActive, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
