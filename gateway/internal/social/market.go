package social

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 开放信号市场（provider 入驻 / 订阅 / 盈亏归因） ─────────────────
//
// 背景：engine.go 的 provider 为内存态（由 AI bot catalog 播种），无持久化。
// 本文件基于迁移 0017 的新表实现开放入驻与利润分成的数据层，SQL 直接走
// store.GetDB()（模仿 engine.go Store 的写法）。

// MarketProviderIDOffset 开放市场 provider 注册进引擎（engine.go）时的 id 偏移：
// catalog 播种的引擎 provider 使用小号自增 id，为避免冲突，市场 provider 的
// 引擎 id 统一为 MarketProviderIDOffset + xt_social_providers.id。
const MarketProviderIDOffset = 1000000

// MarketEngineID 把库里的 provider id 映射为引擎 provider id。
func MarketEngineID(dbID int64) int { return MarketProviderIDOffset + int(dbID) }

// MarketDBID 把引擎 provider id 反解为库里的 provider id；id 不够大时返回 0。
func MarketDBID(engineID int) int64 {
	if engineID < MarketProviderIDOffset {
		return 0
	}
	return int64(engineID - MarketProviderIDOffset)
}

// 收费模式与审核状态（与迁移 0017 的 CHECK 语义一致，代码侧校验）。
const (
	FeeModeMonthly     = "monthly"
	FeeModeProfitShare = "profit_share"
	FeeModeHybrid      = "hybrid"

	ApplyPending  = "pending"
	ApplyApproved = "approved"
	ApplyRejected = "rejected"

	SubStatusActive    = "active"
	SubStatusCancelled = "cancelled"

	SnapStatusPending = "pending"
	SnapStatusLocked  = "locked"
	SnapStatusPayable = "payable"
	SnapStatusPaid    = "paid"
	SnapStatusVoid    = "void"

	WithdrawStatusPending  = "pending"
	WithdrawStatusPaid     = "paid"
	WithdrawStatusRejected = "rejected"
)

// MaxProfitSharePct 平台分成上限（旧 fee_mode 路径，对标 CryptoRobotics 10%-30% 区间）。
const MaxProfitSharePct = 30.0

// ── 双轨定价模型（迁移 0028，对标 CryptoRobotics 双 SKU）─────────────
// pricing_model: 条目级定价模型；track: 用户对某条目实际占用的轨。
const (
	PricingSubscription = "subscription"  // 仅订阅轨（月费）
	PricingProfitShare  = "profit_share"  // 仅分成轨（盈利抽成）
	PricingBoth         = "both"          // 双轨并存，用户二选一

	TrackSubscription = "subscription"
	TrackProfitShare  = "profit_share"
)

// 双轨分成比例区间（CryptoRobotics PSH 10%-35%）。
const (
	DualTrackMinSharePct = 10.0
	DualTrackMaxSharePct = 35.0
)

// MarketSubscriptionPeriodDays 市场订阅轨每期天数（月费）。
const MarketSubscriptionPeriodDays = 30

// ValidatePricingModel 校验双轨定价模型与价格字段的组合；model 为空串表示
// 沿用 fee_mode 旧语义（不做双轨校验）。
func ValidatePricingModel(model string, monthlyFee float64, pct *float64) error {
	switch model {
	case "":
		return nil
	case PricingSubscription:
		if monthlyFee <= 0 {
			return fmt.Errorf("pricing_model=subscription 要求 monthly_fee > 0")
		}
	case PricingProfitShare:
		if pct == nil || *pct < DualTrackMinSharePct || *pct > DualTrackMaxSharePct {
			return fmt.Errorf("pricing_model=profit_share 要求 profit_share_pct 在 %.0f-%.0f 之间", DualTrackMinSharePct, DualTrackMaxSharePct)
		}
	case PricingBoth:
		if monthlyFee <= 0 {
			return fmt.Errorf("pricing_model=both 要求 monthly_fee > 0")
		}
		if pct == nil || *pct < DualTrackMinSharePct || *pct > DualTrackMaxSharePct {
			return fmt.Errorf("pricing_model=both 要求 profit_share_pct 在 %.0f-%.0f 之间", DualTrackMinSharePct, DualTrackMaxSharePct)
		}
	default:
		return fmt.Errorf("invalid pricing_model %q (want subscription|profit_share|both)", model)
	}
	return nil
}

// AvailableTracks 返回该条目当前可选的轨。pricing_model 优先；空则按旧
// fee_mode 推导（hybrid 视为双轨并存——新订阅二选一，存量 hybrid 订阅语义不变）。
func AvailableTracks(p *ProviderApply) []string {
	switch p.PricingModel {
	case PricingSubscription:
		return []string{TrackSubscription}
	case PricingProfitShare:
		return []string{TrackProfitShare}
	case PricingBoth:
		return []string{TrackSubscription, TrackProfitShare}
	}
	switch p.FeeMode {
	case FeeModeMonthly:
		return []string{TrackSubscription}
	case FeeModeHybrid:
		return []string{TrackSubscription, TrackProfitShare}
	default:
		return []string{TrackProfitShare}
	}
}

// trackAvailable 目标轨是否在条目可选轨集合内。
func trackAvailable(p *ProviderApply, track string) bool {
	for _, t := range AvailableTracks(p) {
		if t == track {
			return true
		}
	}
	return false
}

// feeModeForTrack 轨 → 写入订阅行的 fee_mode（保持旧结算查询兼容）。
func feeModeForTrack(track string) string {
	if track == TrackSubscription {
		return FeeModeMonthly
	}
	return FeeModeProfitShare
}

// ProviderApply 是 xt_social_providers 的一行（入驻申请/Provider 档案）。
type ProviderApply struct {
	ID             int64    `json:"id"`
	UserID         int64    `json:"user_id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	MonthlyFee     float64  `json:"monthly_fee"`
	ProfitSharePct *float64 `json:"profit_share_pct,omitempty"`
	FeeMode        string   `json:"fee_mode"`
	PricingModel   string   `json:"pricing_model"` // ''=旧 fee_mode 语义；subscription|profit_share|both
	ApplyStatus    string   `json:"apply_status"`
	ApplyNote      string   `json:"apply_note,omitempty"`
	ApprovedAt     int64    `json:"approved_at,omitempty"`
	IsPublic       bool     `json:"is_public"`
	CreatedAt      int64    `json:"created_at"`
	UpdatedAt      int64    `json:"updated_at"`
}

// Subscription 是 xt_social_subscriptions 的一行。
type Subscription struct {
	ID             int64  `json:"id"`
	ProviderID     int64  `json:"provider_id"`
	FollowerUserID int64  `json:"follower_user_id"`
	FeeMode        string `json:"fee_mode"`
	Track          string `json:"track"`           // ''=旧语义；subscription|profit_share
	TrackExpiresAt int64  `json:"track_expires_at"` // 订阅轨到期毫秒时间戳（0=无到期/非订阅轨）
	Status         string `json:"status"`
	CreatedAt      int64  `json:"created_at"`
	CancelledAt    int64  `json:"cancelled_at,omitempty"`
}

// ShareSubscription 是分成结算视角的订阅：联表带出 provider 的分成比例与属主。
type ShareSubscription struct {
	Subscription
	ProviderUserID int64   `json:"provider_user_id"`
	ProfitSharePct float64 `json:"profit_share_pct"`
}

// ProfitSnapshot 是 xt_social_profit_snapshots 的一行。
type ProfitSnapshot struct {
	ID             string  `json:"id"`
	ProviderID     int64   `json:"provider_id"`
	FollowerUserID int64   `json:"follower_user_id"`
	WindowDate     string  `json:"window_date"`
	CopiedPnl      float64 `json:"copied_pnl"`
	SharePct       float64 `json:"share_pct"`
	ShareAmount    float64 `json:"share_amount"`
	Status         string  `json:"status"`
	LockedUntil    int64   `json:"locked_until"`
	CreatedAt      int64   `json:"created_at"`
}

// Withdrawal 是 xt_social_withdrawals 的一行。
type Withdrawal struct {
	ID             string  `json:"id"`
	ProviderUserID int64   `json:"provider_user_id"`
	Amount         float64 `json:"amount"`
	Chain          string  `json:"chain"`
	Address        string  `json:"address"`
	TxHash         string  `json:"tx_hash"`
	Status         string  `json:"status"`
	AdminNote      string  `json:"admin_note"`
	CreatedAt      int64   `json:"created_at"`
	ProcessedAt    int64   `json:"processed_at,omitempty"`
}

// MarketService 开放市场的数据层。无状态，方法内自取 *sql.DB。
type MarketService struct{}

func NewMarketService() *MarketService { return &MarketService{} }

func (m *MarketService) db() (*sql.DB, error) {
	db := store.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not available")
	}
	return db, nil
}

// DeriveFeeMode 由月费与分成比例推导收费模式（入驻接口不直接收 fee_mode）。
func DeriveFeeMode(monthlyFee float64, pct *float64) string {
	hasShare := pct != nil && *pct > 0
	switch {
	case monthlyFee > 0 && hasShare:
		return FeeModeHybrid
	case monthlyFee > 0:
		return FeeModeMonthly
	default:
		return FeeModeProfitShare
	}
}

// ApplyProvider 写入入驻申请：apply_status=pending，返回完整档案。
func (m *MarketService) ApplyProvider(userID int64, name, description string, monthlyFee float64, pct *float64) (*ProviderApply, error) {
	return m.ApplyProviderWithPricing(userID, name, description, monthlyFee, pct, "")
}

// ApplyProviderWithPricing 同 ApplyProvider，额外写入双轨定价模型（""=旧 fee_mode 语义）。
// pricingModel 非空时先经 ValidatePricingModel 校验。
func (m *MarketService) ApplyProviderWithPricing(userID int64, name, description string, monthlyFee float64, pct *float64, pricingModel string) (*ProviderApply, error) {
	if err := ValidatePricingModel(pricingModel, monthlyFee, pct); err != nil {
		return nil, err
	}
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	res, err := db.Exec(
		`INSERT INTO xt_social_providers
			(user_id, name, description, monthly_fee, profit_share_pct, fee_mode, pricing_model, apply_status, is_public, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		userID, name, description, monthlyFee, nullableFloat(pct), DeriveFeeMode(monthlyFee, pct), pricingModel, ApplyPending, now, now,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return m.GetProvider(id)
}

// GetProvider 按 id 查 provider 档案；不存在返回 sql.ErrNoRows。
func (m *MarketService) GetProvider(id int64) (*ProviderApply, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	var p ProviderApply
	var pct sql.NullFloat64
	var approvedAt sql.NullInt64
	row := db.QueryRow(
		`SELECT id, user_id, name, description, monthly_fee, profit_share_pct, fee_mode, pricing_model,
			apply_status, apply_note, approved_at, is_public, created_at, updated_at
		 FROM xt_social_providers WHERE id = ?`, id)
	if err := row.Scan(&p.ID, &p.UserID, &p.Name, &p.Description, &p.MonthlyFee, &pct, &p.FeeMode, &p.PricingModel,
		&p.ApplyStatus, &p.ApplyNote, &approvedAt, &p.IsPublic, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	if pct.Valid {
		v := pct.Float64
		p.ProfitSharePct = &v
	}
	if approvedAt.Valid {
		p.ApprovedAt = approvedAt.Int64
	}
	return &p, nil
}

// LatestProviderByUser 返回该用户最近一条入驻申请（前端展示审核状态用）。
func (m *MarketService) LatestProviderByUser(userID int64) (*ProviderApply, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	var id int64
	row := db.QueryRow(`SELECT id FROM xt_social_providers WHERE user_id = ? ORDER BY created_at DESC LIMIT 1`, userID)
	if err := row.Scan(&id); err != nil {
		return nil, err
	}
	return m.GetProvider(id)
}

// ListApprovedProviders 返回全部已上架 provider（订阅列表用）。
func (m *MarketService) ListApprovedProviders() ([]*ProviderApply, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT id FROM xt_social_providers WHERE apply_status = ? ORDER BY approved_at ASC`, ApplyApproved)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	out := make([]*ProviderApply, 0, len(ids))
	for _, id := range ids {
		p, err := m.GetProvider(id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ApproveProvider 审核通过：仅 pending 可流转（幂等，重复通过返回当前档案）。
func (m *MarketService) ApproveProvider(id int64) (*ProviderApply, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	res, err := db.Exec(
		`UPDATE xt_social_providers SET apply_status = ?, apply_note = '', approved_at = ?, is_public = 1, updated_at = ?
		 WHERE id = ? AND apply_status = ?`,
		ApplyApproved, now, now, id, ApplyPending)
	if err != nil {
		return nil, err
	}
	p, err := m.GetProvider(id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 && p.ApplyStatus != ApplyApproved {
		return nil, fmt.Errorf("provider %d is not in pending state (current: %s)", id, p.ApplyStatus)
	}
	return p, nil
}

// RejectProvider 审核拒绝：仅 pending 可流转，note 写入申请备注。
func (m *MarketService) RejectProvider(id int64, note string) (*ProviderApply, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	res, err := db.Exec(
		`UPDATE xt_social_providers SET apply_status = ?, apply_note = ?, updated_at = ?
		 WHERE id = ? AND apply_status = ?`,
		ApplyRejected, note, now, id, ApplyPending)
	if err != nil {
		return nil, err
	}
	p, err := m.GetProvider(id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 && p.ApplyStatus != ApplyRejected {
		return nil, fmt.Errorf("provider %d is not in pending state (current: %s)", id, p.ApplyStatus)
	}
	return p, nil
}

// UpsertSubscription 建立/恢复订阅关系：已存在 active 则忽略，存在 cancelled 则复活。
func (m *MarketService) UpsertSubscription(providerID, followerUserID int64, feeMode string) error {
	db, err := m.db()
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	res, err := db.Exec(
		`UPDATE xt_social_subscriptions SET status = ?, cancelled_at = NULL, fee_mode = ?
		 WHERE provider_id = ? AND follower_user_id = ? AND status = ?`,
		SubStatusActive, feeMode, providerID, followerUserID, SubStatusCancelled)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	var active int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM xt_social_subscriptions WHERE provider_id = ? AND follower_user_id = ? AND status = ?`,
		providerID, followerUserID, SubStatusActive).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return nil
	}
	_, err = db.Exec(
		`INSERT INTO xt_social_subscriptions (provider_id, follower_user_id, fee_mode, status, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		providerID, followerUserID, feeMode, SubStatusActive, now)
	return err
}

// CancelSubscription 取消订阅关系（不影响已生成的快照）。
func (m *MarketService) CancelSubscription(providerID, followerUserID int64) error {
	db, err := m.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`UPDATE xt_social_subscriptions SET status = ?, cancelled_at = ?
		 WHERE provider_id = ? AND follower_user_id = ? AND status = ?`,
		SubStatusCancelled, time.Now().UnixMilli(), providerID, followerUserID, SubStatusActive)
	return err
}

// ── 双轨订阅（迁移 0028）────────────────────────────────────────

// GetSubscription 查某 follower 对某 provider 当前有效（active）订阅；
// 不存在返回 sql.ErrNoRows。
func (m *MarketService) GetSubscription(providerID, followerUserID int64) (*Subscription, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	row := db.QueryRow(
		`SELECT id, provider_id, follower_user_id, fee_mode, track, track_expires_at, status, created_at,
			COALESCE(cancelled_at, 0)
		 FROM xt_social_subscriptions
		 WHERE provider_id = ? AND follower_user_id = ? AND status = ?
		 ORDER BY id DESC LIMIT 1`, providerID, followerUserID, SubStatusActive)
	var s Subscription
	if err := row.Scan(&s.ID, &s.ProviderID, &s.FollowerUserID, &s.FeeMode, &s.Track, &s.TrackExpiresAt,
		&s.Status, &s.CreatedAt, &s.CancelledAt); err != nil {
		return nil, err
	}
	return &s, nil
}

// EffectiveTrack 订阅当前实际占用的轨：track 列优先，空则按 fee_mode 推导
// （旧数据 hybrid/monthly 视作订阅轨语义按 monthly 处理）。
func (s *Subscription) EffectiveTrack() string {
	if s.Track != "" {
		return s.Track
	}
	if s.FeeMode == FeeModeProfitShare {
		return TrackProfitShare
	}
	return TrackSubscription
}

// TrackExpired 订阅轨是否已到期（分成轨/无到期时间恒为 false）。
func (s *Subscription) TrackExpired(nowMs int64) bool {
	return s.EffectiveTrack() == TrackSubscription && s.TrackExpiresAt > 0 && s.TrackExpiresAt <= nowMs
}

// UpsertSubscriptionTrack 免费开通/切换分成轨：无订阅则插入 track 行；
// 已有 cancelled 行则复活；已有 active 行则原地切轨（调用方负责切轨规则校验）。
func (m *MarketService) UpsertSubscriptionTrack(providerID, followerUserID int64, track string, expiresAtMs int64) error {
	db, err := m.db()
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	feeMode := feeModeForTrack(track)
	res, err := db.Exec(
		`UPDATE xt_social_subscriptions SET status = ?, cancelled_at = NULL, fee_mode = ?, track = ?, track_expires_at = ?
		 WHERE provider_id = ? AND follower_user_id = ? AND status = ?`,
		SubStatusActive, feeMode, track, expiresAtMs, providerID, followerUserID, SubStatusCancelled)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	if _, err := m.GetSubscription(providerID, followerUserID); err == nil {
		_, err = db.Exec(
			`UPDATE xt_social_subscriptions SET fee_mode = ?, track = ?, track_expires_at = ?
			 WHERE provider_id = ? AND follower_user_id = ? AND status = ?`,
			feeMode, track, expiresAtMs, providerID, followerUserID, SubStatusActive)
		return err
	}
	_, err = db.Exec(
		`INSERT INTO xt_social_subscriptions (provider_id, follower_user_id, fee_mode, track, track_expires_at, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		providerID, followerUserID, feeMode, track, expiresAtMs, SubStatusActive, now)
	return err
}

// ListActiveShareSubscriptions 返回分成结算的作用范围：active 订阅 ∩ 已上架 provider
// ∩ 分成轨 ∩ profit_share_pct > 0。
// 分成轨判定：track='profit_share'（双轨新语义），或 track='' 且 fee_mode ∈
// (profit_share, hybrid)（旧语义兼容）；track='subscription' 一律排除。
func (m *MarketService) ListActiveShareSubscriptions() ([]*ShareSubscription, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(
		`SELECT s.id, s.provider_id, s.follower_user_id, s.fee_mode, s.status, s.created_at,
			COALESCE(s.cancelled_at, 0), p.user_id, p.profit_share_pct
		 FROM xt_social_subscriptions s
		 JOIN xt_social_providers p ON p.id = s.provider_id
		 WHERE s.status = ? AND p.apply_status = ?
			AND (s.track = ? OR (s.track = '' AND s.fee_mode IN (?, ?)))
			AND p.profit_share_pct IS NOT NULL AND p.profit_share_pct > 0`,
		SubStatusActive, ApplyApproved, TrackProfitShare, FeeModeProfitShare, FeeModeHybrid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*ShareSubscription, 0)
	for rows.Next() {
		var s ShareSubscription
		if err := rows.Scan(&s.ID, &s.ProviderID, &s.FollowerUserID, &s.FeeMode, &s.Status, &s.CreatedAt,
			&s.CancelledAt, &s.ProviderUserID, &s.ProfitSharePct); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, nil
}

// RecordCopyPnL 记录一笔归因到 provider 的 follower 已实现跟单盈亏。
// 由跟单执行层在平仓成交时调用（引擎 RecordPnL 只做内存风控，无 provider 归因）。
func (m *MarketService) RecordCopyPnL(providerID, followerUserID int64, pnl float64, note string) error {
	db, err := m.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`INSERT INTO xt_social_copy_pnl (provider_id, follower_user_id, pnl, note, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		providerID, followerUserID, pnl, note, time.Now().UnixMilli())
	return err
}

// SumCopyPnL 汇总某 follower 归因到某 provider 在 [dayStart, dayEnd) 毫秒区间内的
// 已实现盈亏；返回合计值与记录条数（无记录时 settlement 跳过该窗口）。
func (m *MarketService) SumCopyPnL(providerID, followerUserID, dayStart, dayEnd int64) (float64, int64, error) {
	db, err := m.db()
	if err != nil {
		return 0, 0, err
	}
	var sum sql.NullFloat64
	var cnt int64
	if err := db.QueryRow(
		`SELECT COUNT(*), SUM(pnl) FROM xt_social_copy_pnl
		 WHERE provider_id = ? AND follower_user_id = ? AND created_at >= ? AND created_at < ?`,
		providerID, followerUserID, dayStart, dayEnd).Scan(&cnt, &sum); err != nil {
		return 0, 0, err
	}
	if !sum.Valid {
		sum.Float64 = 0
	}
	return sum.Float64, cnt, nil
}

// InsertSnapshot 写入每日分成快照；唯一键 (provider_id, follower_user_id, window_date)
// 保证重复结算幂等（ON CONFLICT DO NOTHING）。
func (m *MarketService) InsertSnapshot(s *ProfitSnapshot) error {
	db, err := m.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`INSERT INTO xt_social_profit_snapshots
			(id, provider_id, follower_user_id, window_date, copied_pnl, share_pct, share_amount, status, locked_until, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(provider_id, follower_user_id, window_date) DO NOTHING`,
		s.ID, s.ProviderID, s.FollowerUserID, s.WindowDate, s.CopiedPnl, s.SharePct, s.ShareAmount,
		s.Status, s.LockedUntil, s.CreatedAt)
	return err
}

// PendingSnapshots 返回该 follower 对 provider 的全部 pending 快照，最旧在前
// （回撤冲抵按时间顺序先冲最早的未锁定收益）。
func (m *MarketService) PendingSnapshots(providerID, followerUserID int64) ([]*ProfitSnapshot, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(
		`SELECT id, provider_id, follower_user_id, window_date, copied_pnl, share_pct, share_amount, status, locked_until, created_at
		 FROM xt_social_profit_snapshots
		 WHERE provider_id = ? AND follower_user_id = ? AND status = ?
		 ORDER BY window_date ASC, created_at ASC`,
		providerID, followerUserID, SnapStatusPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSnapshots(rows)
}

// VoidSnapshot 把快照标记为 void（回撤冲抵）。
func (m *MarketService) VoidSnapshot(id string) error {
	db, err := m.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE xt_social_profit_snapshots SET status = ? WHERE id = ? AND status = ?`,
		SnapStatusVoid, id, SnapStatusPending)
	return err
}

// AgePendingToLocked 到期快照 pending → locked，返回变更条数。
func (m *MarketService) AgePendingToLocked(nowMilli int64) (int64, error) {
	db, err := m.db()
	if err != nil {
		return 0, err
	}
	res, err := db.Exec(
		`UPDATE xt_social_profit_snapshots SET status = ? WHERE status = ? AND locked_until <= ?`,
		SnapStatusLocked, SnapStatusPending, nowMilli)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AgeLockedToPayable 到期快照 locked → payable，返回变更条数。
// payableNotBefore 由结算引擎按 locked_until + payableDelay 计算。
func (m *MarketService) AgeLockedToPayable(payableNotBefore int64) (int64, error) {
	db, err := m.db()
	if err != nil {
		return 0, err
	}
	res, err := db.Exec(
		`UPDATE xt_social_profit_snapshots SET status = ? WHERE status = ? AND locked_until <= ?`,
		SnapStatusPayable, SnapStatusLocked, payableNotBefore)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// LatestSnapshotDate 返回已结算过的最大窗口日（YYYY-MM-DD）；无记录返回 "" 与 false。
func (m *MarketService) LatestSnapshotDate() (string, bool, error) {
	db, err := m.db()
	if err != nil {
		return "", false, err
	}
	var d sql.NullString
	if err := db.QueryRow(`SELECT MAX(window_date) FROM xt_social_profit_snapshots`).Scan(&d); err != nil {
		return "", false, err
	}
	if !d.Valid || d.String == "" {
		return "", false, nil
	}
	return d.String, true, nil
}

// EarningsSummary provider 属主的收益汇总。
type EarningsSummary struct {
	Today              float64 `json:"today"`
	Total              float64 `json:"total"`
	Payable            float64 `json:"payable"`
	Withdrawn          float64 `json:"withdrawn"`
	PendingWithdrawals float64 `json:"pending_withdrawals"`
	Available          float64 `json:"available"`
}

// Earnings 实时汇总当前用户的分成收益（提现可用额 = payable − 在途/已付提现，不落总账）。
func (m *MarketService) Earnings(userID int64) (*EarningsSummary, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	ids, err := m.providerIDsByUser(userID)
	if err != nil {
		return nil, err
	}
	s := &EarningsSummary{}
	if len(ids) == 0 {
		return s, nil
	}
	in := int64InClause(ids)

	today := time.Now().Format("2006-01-02")
	scan := func(query string, dest *float64, args ...any) error {
		var v sql.NullFloat64
		if err := db.QueryRow(query, args...).Scan(&v); err != nil {
			return err
		}
		if v.Valid {
			*dest = v.Float64
		}
		return nil
	}
	if err := scan(`SELECT SUM(share_amount) FROM xt_social_profit_snapshots
		WHERE provider_id IN `+in+` AND window_date = ? AND status != ?`, &s.Today, today, SnapStatusVoid); err != nil {
		return nil, err
	}
	if err := scan(`SELECT SUM(share_amount) FROM xt_social_profit_snapshots
		WHERE provider_id IN `+in+` AND status != ?`, &s.Total, SnapStatusVoid); err != nil {
		return nil, err
	}
	if err := scan(`SELECT SUM(share_amount) FROM xt_social_profit_snapshots
		WHERE provider_id IN `+in+` AND status = ?`, &s.Payable, SnapStatusPayable); err != nil {
		return nil, err
	}
	if err := scan(`SELECT SUM(amount) FROM xt_social_withdrawals
		WHERE provider_user_id = ? AND status = ?`, &s.Withdrawn, userID, WithdrawStatusPaid); err != nil {
		return nil, err
	}
	if err := scan(`SELECT SUM(amount) FROM xt_social_withdrawals
		WHERE provider_user_id = ? AND status = ?`, &s.PendingWithdrawals, userID, WithdrawStatusPending); err != nil {
		return nil, err
	}
	s.Available = s.Payable - s.Withdrawn - s.PendingWithdrawals
	if s.Available < 0 {
		s.Available = 0
	}
	return s, nil
}

func (m *MarketService) providerIDsByUser(userID int64) ([]int64, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT id FROM xt_social_providers WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// CreateWithdrawal 写入提现申请（余额校验由 handler 基于 Earnings().Available 完成）。
func (m *MarketService) CreateWithdrawal(userID int64, amount float64, chain, address string) (*Withdrawal, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	w := &Withdrawal{
		ID:             fmt.Sprintf("wd-%d", time.Now().UnixNano()),
		ProviderUserID: userID,
		Amount:         amount,
		Chain:          chain,
		Address:        address,
		Status:         WithdrawStatusPending,
		CreatedAt:      time.Now().UnixMilli(),
	}
	_, err = db.Exec(
		`INSERT INTO xt_social_withdrawals (id, provider_user_id, amount, chain, address, tx_hash, status, admin_note, created_at)
		 VALUES (?, ?, ?, ?, ?, '', ?, '', ?)`,
		w.ID, w.ProviderUserID, w.Amount, w.Chain, w.Address, w.Status, w.CreatedAt)
	if err != nil {
		return nil, err
	}
	return w, nil
}

const withdrawalCols = `id, provider_user_id, amount, chain, address, tx_hash, status, admin_note, created_at, COALESCE(processed_at, 0)`

func scanWithdrawal(s interface{ Scan(...any) error }) (*Withdrawal, error) {
	var w Withdrawal
	err := s.Scan(&w.ID, &w.ProviderUserID, &w.Amount, &w.Chain, &w.Address, &w.TxHash, &w.Status,
		&w.AdminNote, &w.CreatedAt, &w.ProcessedAt)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// ListWithdrawalsByUser 返回该用户的提现申请（新的在前）。
func (m *MarketService) ListWithdrawalsByUser(userID int64) ([]*Withdrawal, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT `+withdrawalCols+` FROM xt_social_withdrawals
		WHERE provider_user_id = ? ORDER BY created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*Withdrawal, 0)
	for rows.Next() {
		w, err := scanWithdrawal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

// ListWithdrawals 返回全部提现申请（admin）；status 为空返回所有。
func (m *MarketService) ListWithdrawals(status string) ([]*Withdrawal, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	var rows *sql.Rows
	if status != "" {
		rows, err = db.Query(`SELECT `+withdrawalCols+` FROM xt_social_withdrawals
			WHERE status = ? ORDER BY created_at DESC LIMIT 500`, status)
	} else {
		rows, err = db.Query(`SELECT ` + withdrawalCols + ` FROM xt_social_withdrawals
			ORDER BY created_at DESC LIMIT 500`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*Withdrawal, 0)
	for rows.Next() {
		w, err := scanWithdrawal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

// GetWithdrawal 按 id 查提现申请；不存在返回 sql.ErrNoRows。
func (m *MarketService) GetWithdrawal(id string) (*Withdrawal, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	row := db.QueryRow(`SELECT `+withdrawalCols+` FROM xt_social_withdrawals WHERE id = ?`, id)
	return scanWithdrawal(row)
}

// MarkWithdrawalPaid 打款：仅 pending 可流转（幂等），返回是否发生变更。
func (m *MarketService) MarkWithdrawalPaid(id, txHash string) (bool, error) {
	db, err := m.db()
	if err != nil {
		return false, err
	}
	res, err := db.Exec(
		`UPDATE xt_social_withdrawals SET status = ?, tx_hash = ?, processed_at = ?
		 WHERE id = ? AND status = ?`,
		WithdrawStatusPaid, txHash, time.Now().UnixMilli(), id, WithdrawStatusPending)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkWithdrawalRejected 拒绝：仅 pending 可流转（幂等），note 写给申请人。
func (m *MarketService) MarkWithdrawalRejected(id, note string) (bool, error) {
	db, err := m.db()
	if err != nil {
		return false, err
	}
	res, err := db.Exec(
		`UPDATE xt_social_withdrawals SET status = ?, admin_note = ?, processed_at = ?
		 WHERE id = ? AND status = ?`,
		WithdrawStatusRejected, note, time.Now().UnixMilli(), id, WithdrawStatusPending)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ListSnapshotsByProviderUser admin/调试视角：某 provider 属主的全部快照。
func (m *MarketService) ListSnapshotsByProvider(providerID int64, limit int) ([]*ProfitSnapshot, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(
		`SELECT id, provider_id, follower_user_id, window_date, copied_pnl, share_pct, share_amount, status, locked_until, created_at
		 FROM xt_social_profit_snapshots WHERE provider_id = ? ORDER BY window_date DESC LIMIT ?`,
		providerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSnapshots(rows)
}

func scanSnapshots(rows *sql.Rows) ([]*ProfitSnapshot, error) {
	out := make([]*ProfitSnapshot, 0)
	for rows.Next() {
		var s ProfitSnapshot
		if err := rows.Scan(&s.ID, &s.ProviderID, &s.FollowerUserID, &s.WindowDate, &s.CopiedPnl,
			&s.SharePct, &s.ShareAmount, &s.Status, &s.LockedUntil, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, nil
}

func nullableFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func int64InClause(ids []int64) string {
	out := "("
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf("%d", id)
	}
	return out + ")"
}

// IsNotFound 统一 not-found 判定（sql.ErrNoRows）。
func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }
