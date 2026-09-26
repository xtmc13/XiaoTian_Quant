package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// ── Signal Source / Subscription / Bill Repository ──
//
// 信号源订阅定价（对齐 CryptoRobotics 与 ai_bot_catalog 的 fee_model 体系）：
//   - free          免费
//   - fixed_monthly 固定月费（订阅时生成月费账单流水 + next_billing_at）
//   - profit_share  盈利分成（执行记录平仓落库时按 fee_percent 累计
//                   到订阅的 pending_share，作者结算后置 settled_share）

// SignalSourceTPSL 信号源默认阶梯止盈配置（JSON 存 tp_sl_json）。
type SignalSourceTPSL struct {
	TP1Pct float64 `json:"tp1_pct"`
	TP2Pct float64 `json:"tp2_pct"`
	TP3Pct float64 `json:"tp3_pct"`
	SLPct  float64 `json:"sl_pct"`
}

type SignalSource struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Type        string  `json:"type"` // webhook | api | manual
	OwnerUserID int     `json:"owner_user_id"`
	Enabled     bool    `json:"enabled"`
	FeeModel    string  `json:"fee_model"` // free | fixed_monthly | profit_share
	FeePercent  float64 `json:"fee_percent"`
	MonthlyFee  float64 `json:"monthly_fee"`
	TPSLJSON    string  `json:"tp_sl_json"`
	CreatedAt   int64   `json:"created_at"`
	UpdatedAt   int64   `json:"updated_at"`
}

type SignalSourceSubscription struct {
	ID            int     `json:"id"`
	SourceID      string  `json:"source_id"`
	UserID        int     `json:"user_id"`
	FeeModel      string  `json:"fee_model"`
	FeePercent    float64 `json:"fee_percent"`
	MonthlyFee    float64 `json:"monthly_fee"`
	NextBillingAt int64   `json:"next_billing_at"`
	PendingShare  float64 `json:"pending_share"`
	SettledShare  float64 `json:"settled_share"`
	Status        string  `json:"status"` // active | expired | cancelled
	CreatedAt     int64   `json:"created_at"`
}

type SignalSourceBill struct {
	ID             int     `json:"id"`
	SourceID       string  `json:"source_id"`
	UserID         int     `json:"user_id"`
	SubscriptionID int     `json:"subscription_id"`
	BillType       string  `json:"bill_type"` // monthly_fee | profit_share
	Amount         float64 `json:"amount"`
	Status         string  `json:"status"` // pending | settled
	CreatedAt      int64   `json:"created_at"`
	SettledAt      int64   `json:"settled_at"`
}

type SignalSourceRepo struct{ mu sync.RWMutex }

func NewSignalSourceRepo() *SignalSourceRepo { return &SignalSourceRepo{} }

// genSourceID 生成 "src_" 前缀的随机 id。
func genSourceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("src_%d", time.Now().UnixNano())
	}
	return "src_" + hex.EncodeToString(b)
}

// EnsureDefaultSource 保证默认 webhook 信号源存在（webhook 落库的回退归属）。
// 返回默认源 id。
func (r *SignalSourceRepo) EnsureDefaultSource() string {
	if db == nil {
		return "default"
	}
	var id string
	if err := db.QueryRow(`SELECT id FROM xt_signal_sources WHERE id='default'`).Scan(&id); err == nil {
		return id
	}
	now := time.Now().UnixMilli()
	_, _ = db.Exec(
		`INSERT INTO xt_signal_sources (id, name, type, owner_user_id, enabled, fee_model, fee_percent, monthly_fee, tp_sl_json, created_at, updated_at)
		 VALUES ('default', '默认Webhook', 'webhook', 0, 1, 'free', 0, 0, '{"tp1_pct":40,"tp2_pct":30,"tp3_pct":30,"sl_pct":3}', ?, ?)`,
		now, now,
	)
	return "default"
}

func (r *SignalSourceRepo) Create(s *SignalSource) error {
	if s.ID == "" {
		s.ID = genSourceID()
	}
	now := time.Now().UnixMilli()
	if s.CreatedAt == 0 {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	_, err := db.Exec(
		`INSERT INTO xt_signal_sources (id, name, type, owner_user_id, enabled, fee_model, fee_percent, monthly_fee, tp_sl_json, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.Name, s.Type, s.OwnerUserID, s.Enabled, s.FeeModel, s.FeePercent, s.MonthlyFee, s.TPSLJSON, s.CreatedAt, s.UpdatedAt,
	)
	return err
}

func (r *SignalSourceRepo) GetByID(id string) (*SignalSource, error) {
	var s SignalSource
	var enabled int
	err := db.QueryRow(
		`SELECT id, name, type, owner_user_id, enabled, fee_model, fee_percent, monthly_fee, tp_sl_json, created_at, updated_at FROM xt_signal_sources WHERE id=?`, id,
	).Scan(&s.ID, &s.Name, &s.Type, &s.OwnerUserID, &enabled, &s.FeeModel, &s.FeePercent, &s.MonthlyFee, &s.TPSLJSON, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s.Enabled = enabled != 0
	return &s, nil
}

func (r *SignalSourceRepo) List(enabledOnly bool) ([]*SignalSource, error) {
	query := `SELECT id, name, type, owner_user_id, enabled, fee_model, fee_percent, monthly_fee, tp_sl_json, created_at, updated_at FROM xt_signal_sources`
	if enabledOnly {
		query += ` WHERE enabled=1`
	}
	query += ` ORDER BY created_at ASC`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*SignalSource
	for rows.Next() {
		var s SignalSource
		var enabled int
		if err := rows.Scan(&s.ID, &s.Name, &s.Type, &s.OwnerUserID, &enabled, &s.FeeModel, &s.FeePercent, &s.MonthlyFee, &s.TPSLJSON, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.Enabled = enabled != 0
		result = append(result, &s)
	}
	return result, nil
}

func (r *SignalSourceRepo) Update(s *SignalSource) error {
	s.UpdatedAt = time.Now().UnixMilli()
	_, err := db.Exec(
		`UPDATE xt_signal_sources SET name=?, type=?, enabled=?, fee_model=?, fee_percent=?, monthly_fee=?, tp_sl_json=?, updated_at=? WHERE id=?`,
		s.Name, s.Type, s.Enabled, s.FeeModel, s.FeePercent, s.MonthlyFee, s.TPSLJSON, s.UpdatedAt, s.ID,
	)
	return err
}

func (r *SignalSourceRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM xt_signal_sources WHERE id=?", id)
	return err
}

// ── Subscriptions ──

func (r *SignalSourceRepo) CreateSubscription(s *SignalSourceSubscription) error {
	if s.CreatedAt == 0 {
		s.CreatedAt = time.Now().UnixMilli()
	}
	if s.Status == "" {
		s.Status = "active"
	}
	res, err := db.Exec(
		`INSERT INTO xt_signal_source_subscriptions (source_id, user_id, fee_model, fee_percent, monthly_fee, next_billing_at, pending_share, settled_share, status, created_at)
		 VALUES (?,?,?,?,?,?,0,0,?,?)`,
		s.SourceID, s.UserID, s.FeeModel, s.FeePercent, s.MonthlyFee, s.NextBillingAt, s.Status, s.CreatedAt,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	s.ID = int(id)
	return nil
}

// GetSubscription 查询用户对某信号源的活跃订阅（无则 nil）。
func (r *SignalSourceRepo) GetSubscription(sourceID string, userID int) (*SignalSourceSubscription, error) {
	var s SignalSourceSubscription
	err := db.QueryRow(
		`SELECT id, source_id, user_id, fee_model, fee_percent, monthly_fee, next_billing_at, pending_share, settled_share, status, created_at
		 FROM xt_signal_source_subscriptions WHERE source_id=? AND user_id=? AND status='active' ORDER BY id DESC LIMIT 1`,
		sourceID, userID,
	).Scan(&s.ID, &s.SourceID, &s.UserID, &s.FeeModel, &s.FeePercent, &s.MonthlyFee, &s.NextBillingAt, &s.PendingShare, &s.SettledShare, &s.Status, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListSubscriptionsBySource 返回某信号源的全部订阅（作者视角）。
func (r *SignalSourceRepo) ListSubscriptionsBySource(sourceID string) ([]*SignalSourceSubscription, error) {
	rows, err := db.Query(
		`SELECT id, source_id, user_id, fee_model, fee_percent, monthly_fee, next_billing_at, pending_share, settled_share, status, created_at
		 FROM xt_signal_source_subscriptions WHERE source_id=? ORDER BY id DESC`, sourceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*SignalSourceSubscription
	for rows.Next() {
		var s SignalSourceSubscription
		if err := rows.Scan(&s.ID, &s.SourceID, &s.UserID, &s.FeeModel, &s.FeePercent, &s.MonthlyFee, &s.NextBillingAt, &s.PendingShare, &s.SettledShare, &s.Status, &s.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, &s)
	}
	return result, nil
}

// UpdateSubscription 回填订阅（分成累计/结算、到期时间、状态）。
func (r *SignalSourceRepo) UpdateSubscription(s *SignalSourceSubscription) error {
	_, err := db.Exec(
		`UPDATE xt_signal_source_subscriptions SET next_billing_at=?, pending_share=?, settled_share=?, status=? WHERE id=?`,
		s.NextBillingAt, s.PendingShare, s.SettledShare, s.Status, s.ID,
	)
	return err
}

// AddPendingShare 给某信号源全部活跃 profit_share 订阅累计待结算分成
// （信号执行平仓落库时调用）。
func (r *SignalSourceRepo) AddPendingShare(sourceID string, profit float64) error {
	_, err := db.Exec(
		`UPDATE xt_signal_source_subscriptions SET pending_share = pending_share + ?
		 WHERE source_id=? AND status='active' AND fee_model='profit_share' AND pending_share + ? >= 0`,
		profit, sourceID, profit,
	)
	return err
}

// ── Bills ──

func (r *SignalSourceRepo) CreateBill(b *SignalSourceBill) error {
	if b.CreatedAt == 0 {
		b.CreatedAt = time.Now().UnixMilli()
	}
	if b.Status == "" {
		b.Status = "pending"
	}
	res, err := db.Exec(
		`INSERT INTO xt_signal_source_bills (source_id, user_id, subscription_id, bill_type, amount, status, created_at, settled_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		b.SourceID, b.UserID, b.SubscriptionID, b.BillType, b.Amount, b.Status, b.CreatedAt, b.SettledAt,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	b.ID = int(id)
	return nil
}

func (r *SignalSourceRepo) ListBills(sourceID string, limit int) ([]*SignalSourceBill, error) {
	query := `SELECT id, source_id, user_id, subscription_id, bill_type, amount, status, created_at, settled_at FROM xt_signal_source_bills`
	args := []any{}
	if sourceID != "" {
		query += ` WHERE source_id=?`
		args = append(args, sourceID)
	}
	query += ` ORDER BY id DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*SignalSourceBill
	for rows.Next() {
		var b SignalSourceBill
		if err := rows.Scan(&b.ID, &b.SourceID, &b.UserID, &b.SubscriptionID, &b.BillType, &b.Amount, &b.Status, &b.CreatedAt, &b.SettledAt); err != nil {
			return nil, err
		}
		result = append(result, &b)
	}
	return result, nil
}

// DeductUserCredits 从用户余额（xt_users.credits，billing 积分体系）扣款。
// 单位约定：1 credit = 1 USDT，按整数扣（credits 列为 INTEGER）。
// 余额不足或用户不存在不扣款，返回 false（调用方降级为 pending 账单流水）。
func DeductUserCredits(userID int, amount float64) bool {
	if db == nil || userID <= 0 || amount <= 0 {
		return false
	}
	credits := int64(amount + 0.5)
	if credits <= 0 {
		return false
	}
	res, err := db.Exec(`UPDATE xt_users SET credits = credits - ? WHERE id=? AND credits >= ?`, credits, userID, credits)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}
