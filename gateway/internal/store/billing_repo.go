package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ── Billing Order Repository ──

// BillingOrder 计费订单：USDT 多链转账与 Stripe 信用卡支付统一入账。
// AmountMicro 为微单位（1 USDT = 1_000_000），全程整数比较，不用浮点。
type BillingOrder struct {
	ID          string `json:"order_id"`
	UserID      int64  `json:"user_id"`
	PlanID      string `json:"plan_id"`
	Purpose     string `json:"purpose"` // plan（默认）| market_subscription（市场条目订阅轨扣费）
	RefID       string `json:"ref_id"`  // purpose=market_subscription 时为 provider id
	Chain       string `json:"chain"`
	Address     string `json:"address"`
	AmountMicro int64  `json:"amount_usdt"`
	TxHash      string `json:"tx_hash"`
	Status      string `json:"status"` // pending/confirming/paid/failed/expired
	FailReason  string `json:"fail_reason"`
	Attempts    int    `json:"attempts"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	ConfirmedAt int64  `json:"confirmed_at"`
}

// Billing 订单用途（迁移 0028）。
const (
	BillingPurposePlan               = "plan"
	BillingPurposeMarketSubscription = "market_subscription"
)

// Billing 订单状态机常量。
const (
	BillingStatusPending    = "pending"
	BillingStatusConfirming = "confirming"
	BillingStatusPaid       = "paid"
	BillingStatusFailed     = "failed"
	BillingStatusExpired    = "expired"
)

// BillingSubscription 当前用户套餐/积分（xt_users 三列的读模型）。
type BillingSubscription struct {
	Plan         string `json:"plan"`
	VipExpiresAt int64  `json:"vip_expires_at"`
	Credits      int64  `json:"credits"`
}

var errBillingNoRows = errors.New("billing order not found")

type BillingRepo struct{ mu sync.Mutex }

func NewBillingRepo() *BillingRepo { return &BillingRepo{} }

const billingOrderCols = `id, user_id, plan_id, purpose, ref_id, chain, address, amount_usdt, tx_hash, status, fail_reason, attempts, created_at, updated_at, confirmed_at`

func scanBillingOrder(row interface{ Scan(...any) error }) (*BillingOrder, error) {
	var o BillingOrder
	err := row.Scan(&o.ID, &o.UserID, &o.PlanID, &o.Purpose, &o.RefID, &o.Chain, &o.Address, &o.AmountMicro,
		&o.TxHash, &o.Status, &o.FailReason, &o.Attempts, &o.CreatedAt, &o.UpdatedAt, &o.ConfirmedAt)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// Create 落库新订单（状态 pending）。
func (r *BillingRepo) Create(o *BillingOrder) error {
	if o.CreatedAt == 0 {
		o.CreatedAt = time.Now().Unix()
	}
	o.UpdatedAt = o.CreatedAt
	if o.Status == "" {
		o.Status = BillingStatusPending
	}
	if o.Purpose == "" {
		o.Purpose = BillingPurposePlan
	}
	_, err := db.Exec(
		`INSERT INTO billing_orders (id, user_id, plan_id, purpose, ref_id, chain, address, amount_usdt, tx_hash, status, fail_reason, attempts, created_at, updated_at, confirmed_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.ID, o.UserID, o.PlanID, o.Purpose, o.RefID, o.Chain, o.Address, o.AmountMicro, o.TxHash, o.Status,
		o.FailReason, o.Attempts, o.CreatedAt, o.UpdatedAt, o.ConfirmedAt,
	)
	return err
}

// GetByID 按订单号查询。
func (r *BillingRepo) GetByID(id string) (*BillingOrder, error) {
	return scanBillingOrder(db.QueryRow(`SELECT `+billingOrderCols+` FROM billing_orders WHERE id=?`, id))
}

// FindByTxHash 查 tx_hash 占用：同一哈希不能服务多个订单（幂等/防重放）。
func (r *BillingRepo) FindByTxHash(hash string) (*BillingOrder, error) {
	return scanBillingOrder(db.QueryRow(`SELECT `+billingOrderCols+` FROM billing_orders WHERE tx_hash=?`, hash))
}

// ListByUser 返回指定用户订单（新→旧）。
func (r *BillingRepo) ListByUser(userID int64, limit int) ([]*BillingOrder, error) {
	return r.list(`SELECT `+billingOrderCols+` FROM billing_orders WHERE user_id=? ORDER BY created_at DESC LIMIT ?`, userID, limit)
}

// ListAll 返回全部订单（admin 用）。
func (r *BillingRepo) ListAll(limit int) ([]*BillingOrder, error) {
	return r.list(`SELECT `+billingOrderCols+` FROM billing_orders ORDER BY created_at DESC LIMIT ?`, limit)
}

func (r *BillingRepo) list(query string, args ...any) ([]*BillingOrder, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BillingOrder
	for rows.Next() {
		o, err := scanBillingOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

// ListVerifyCandidates 供后台核验器轮询：pending/confirming 全部，
// 以及 failed 但仍带哈希、尝试次数未达上限的可重试订单。
func (r *BillingRepo) ListVerifyCandidates(limit, maxAttempts int) ([]*BillingOrder, error) {
	return r.list(`SELECT `+billingOrderCols+` FROM billing_orders
		WHERE (status IN ('pending','confirming') OR (status='failed' AND tx_hash != '' AND attempts < ?))
		  AND chain != 'stripe' AND tx_hash != ''
		ORDER BY updated_at ASC LIMIT ?`, maxAttempts, limit)
}

// ExpireStalePending 将创建超过 ttlSec 仍未提交 tx_hash 的 pending 订单置为 expired。
func (r *BillingRepo) ExpireStalePending(cutoff int64) (int64, error) {
	res, err := db.Exec(`UPDATE billing_orders SET status='expired', updated_at=?
		WHERE status='pending' AND tx_hash='' AND created_at < ?`, time.Now().Unix(), cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SubmitTxHash 提交/重提交易哈希：仅 pending/failed 可提交，尝试次数清零重新核验。
// 带状态条件更新防止并发把 paid 订单退回。
func (r *BillingRepo) SubmitTxHash(id, hash string) (*BillingOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	res, err := db.Exec(`UPDATE billing_orders SET tx_hash=?, status='pending', fail_reason='', attempts=0, updated_at=?
		WHERE id=? AND status IN ('pending','failed')`, hash, time.Now().Unix(), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errBillingNoRows
	}
	return r.GetByID(id)
}

// MarkConfirming 交易已上链但确认数不足。
func (r *BillingRepo) MarkConfirming(id string) error {
	_, err := db.Exec(`UPDATE billing_orders SET status='confirming', fail_reason='', updated_at=? WHERE id=? AND status IN ('pending','confirming','failed')`,
		time.Now().Unix(), id)
	return err
}

// IncAttempts 核验尝试 +1（网络错误/暂未上链时累加，避免无限轮询）。
func (r *BillingRepo) IncAttempts(id string) (int, error) {
	_, err := db.Exec(`UPDATE billing_orders SET attempts=attempts+1, updated_at=? WHERE id=?`, time.Now().Unix(), id)
	if err != nil {
		return 0, err
	}
	o, err := r.GetByID(id)
	if err != nil {
		return 0, err
	}
	return o.Attempts, nil
}

// MarkExpired 订单超时过期（只覆盖未支付状态，paid 不受影响）。
func (r *BillingRepo) MarkExpired(id string) error {
	_, err := db.Exec(`UPDATE billing_orders SET status='expired', updated_at=? WHERE id=? AND status IN ('pending','confirming')`,
		time.Now().Unix(), id)
	return err
}

// MarkFailed 核验不通过（地址/金额不符、密钥未配置、重试耗尽等）。
func (r *BillingRepo) MarkFailed(id, reason string) error {
	_, err := db.Exec(`UPDATE billing_orders SET status='failed', fail_reason=?, updated_at=? WHERE id=? AND status != 'paid'`,
		reason, time.Now().Unix(), id)
	return err
}

// GrantPlanTx 在单个 SQLite 事务内完成「订单置 paid + 用户套餐/积分发放」，保证要么都成功要么都不生效。
// 幂等：订单已是 paid（或并发下被别的请求先置 paid）时返回 granted=false，不会重复发放。
// periodDays<0 表示终身会员（vip_expires_at=-1）；否则在现有有效期上顺延。
func (r *BillingRepo) GrantPlanTx(orderID string, userID int64, planID string, credits, periodDays int64, now int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	// 条件更新：只从非 paid 状态流转，已 paid 直接判定为重复发放。
	res, err := tx.Exec(`UPDATE billing_orders SET status='paid', fail_reason='', confirmed_at=?, updated_at=?
		WHERE id=? AND status != 'paid'`, now, now, orderID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil // 已发放过（幂等）
	}

	if credits > 0 {
		if _, err := tx.Exec(`UPDATE xt_users SET credits = credits + ? WHERE id=?`, credits, userID); err != nil {
			return false, err
		}
	}
	if planID != "" {
		if periodDays < 0 {
			if _, err := tx.Exec(`UPDATE xt_users SET plan=?, vip_expires_at=-1 WHERE id=?`, planID, userID); err != nil {
				return false, err
			}
		} else {
			// 有效期顺延：现有有效期在未来则叠加，否则从当前时间起算。
			if _, err := tx.Exec(`UPDATE xt_users SET plan=?,
				vip_expires_at = MAX(COALESCE(vip_expires_at,0), ?) + ? WHERE id=?`,
				planID, now, periodDays*86400, userID); err != nil {
				return false, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// GrantMarketSubscriptionTx 市场条目订阅轨发放（迁移 0028 双轨 SKU）：
// 单事务内订单置 paid + 激活/顺延 follower 对 provider 的订阅轨
// （xt_social_subscriptions: track='subscription', fee_mode='monthly'）。
// 幂等同 GrantPlanTx：已 paid 返回 granted=false。
// 有效期顺延：现有到期时间在未来则叠加，否则从当前时间起算 periodDays。
func (r *BillingRepo) GrantMarketSubscriptionTx(orderID string, userID, providerID, periodDays, now int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`UPDATE billing_orders SET status='paid', fail_reason='', confirmed_at=?, updated_at=?
		WHERE id=? AND status != 'paid'`, now, now, orderID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil // 已发放过（幂等）
	}

	nowMs := now * 1000
	periodMs := periodDays * 86400 * 1000
	// 已有 active 行：切到订阅轨并顺延（MAX 处理到期后续费/未到期续费两种）。
	res, err = tx.Exec(
		`UPDATE xt_social_subscriptions SET fee_mode='monthly', track='subscription',
			track_expires_at = MAX(track_expires_at, ?) + ?
		 WHERE provider_id=? AND follower_user_id=? AND status='active'`,
		nowMs, periodMs, providerID, userID)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// cancelled 行复活：从当前时间起算。
		res, err = tx.Exec(
			`UPDATE xt_social_subscriptions SET status='active', cancelled_at=NULL, fee_mode='monthly',
				track='subscription', track_expires_at=?
			 WHERE provider_id=? AND follower_user_id=? AND status='cancelled'`,
			nowMs+periodMs, providerID, userID)
		if err != nil {
			return false, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			if _, err = tx.Exec(
				`INSERT INTO xt_social_subscriptions (provider_id, follower_user_id, fee_mode, track, track_expires_at, status, created_at)
				 VALUES (?, ?, 'monthly', 'subscription', ?, 'active', ?)`,
				providerID, userID, nowMs+periodMs, nowMs); err != nil {
				return false, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// GetSubscription 读取用户当前套餐/积分。
func (r *BillingRepo) GetSubscription(userID int64) (*BillingSubscription, error) {
	var s BillingSubscription
	err := db.QueryRow(`SELECT COALESCE(plan,''), COALESCE(vip_expires_at,0), COALESCE(credits,0) FROM xt_users WHERE id=?`, userID).
		Scan(&s.Plan, &s.VipExpiresAt, &s.Credits)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user not found")
		}
		return nil, err
	}
	return &s, nil
}
