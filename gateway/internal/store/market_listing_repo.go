package store

import (
	"fmt"
	"time"
)

// ── 市场条目上架准入 Repository ──
// 表结构见 migrations/sql/0029_market_listings.sql。
// 对标 CryptoRobotics 创作者市场：作者提交自有 AI 机器人实例进入考核期
// (probation)，达标后经人工审核方可上架(listed)；统计日快照驱动市场卡片。

// MarketListingRecord 市场条目（状态机载体）。
type MarketListingRecord struct {
	ID                 string  `json:"id"`
	AuthorUserID       int64   `json:"author_user_id"`
	BotInstanceID      string  `json:"bot_instance_id"`
	Kind               string  `json:"kind"` // robot | signal（indicator 预留）
	Name               string  `json:"name"`
	Description        string  `json:"description"`
	FeeModel           string  `json:"fee_model"` // free | monthly | profit_share
	FeePercent         float64 `json:"fee_percent"`
	MonthlyFee         float64 `json:"monthly_fee"`
	Status             string  `json:"status"` // draft|probation|pending_review|listed|rejected|delisted
	RejectReason       string  `json:"reject_reason"`
	ProbationStartedAt int64   `json:"probation_started_at"`
	RuleMinDays        int     `json:"rule_min_days"`
	RuleMinTrades      int     `json:"rule_min_trades"`
	RuleMaxDrawdownPct float64 `json:"rule_max_drawdown_pct"`
	ReviewedBy         int64   `json:"reviewed_by"`
	ReviewedAt         int64   `json:"reviewed_at"`
	ListedAt           int64   `json:"listed_at"`
	DelistReason       string  `json:"delist_reason"`
	CreatedAt          int64   `json:"created_at"`
	UpdatedAt          int64   `json:"updated_at"`
}

// MarketListingStatsRecord 标准化统计日快照（卡片 = 最新一行）。
type MarketListingStatsRecord struct {
	ListingID           string  `json:"listing_id"`
	Date                string  `json:"date"` // YYYY-MM-DD (UTC)
	TotalReturnPct      float64 `json:"total_return_pct"`
	AnnualizedReturnPct float64 `json:"annualized_return_pct"`
	MaxDrawdownPct      float64 `json:"max_drawdown_pct"`
	WinRate             float64 `json:"win_rate"`
	ProfitFactor        float64 `json:"profit_factor"`
	SharpeRatio         float64 `json:"sharpe_ratio"`
	TotalTrades         int     `json:"total_trades"`
	MonthlyReturnPct    float64 `json:"monthly_return_pct"`
	Followers           int     `json:"followers"`
	RunningDays         int     `json:"running_days"`
	CreatedAt           int64   `json:"created_at"`
}

type MarketListingRepo struct{}

func NewMarketListingRepo() *MarketListingRepo { return &MarketListingRepo{} }

const marketListingColumns = `id, author_user_id, bot_instance_id, kind, name, description,
	fee_model, fee_percent, monthly_fee, status, reject_reason,
	probation_started_at, rule_min_days, rule_min_trades, rule_max_drawdown_pct,
	reviewed_by, reviewed_at, listed_at, delist_reason, created_at, updated_at`

func scanMarketListing(row interface{ Scan(...any) error }) (*MarketListingRecord, error) {
	var r MarketListingRecord
	err := row.Scan(&r.ID, &r.AuthorUserID, &r.BotInstanceID, &r.Kind, &r.Name, &r.Description,
		&r.FeeModel, &r.FeePercent, &r.MonthlyFee, &r.Status, &r.RejectReason,
		&r.ProbationStartedAt, &r.RuleMinDays, &r.RuleMinTrades, &r.RuleMaxDrawdownPct,
		&r.ReviewedBy, &r.ReviewedAt, &r.ListedAt, &r.DelistReason, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Create 插入新条目（初始 draft）。
func (r *MarketListingRepo) Create(rec *MarketListingRecord) error {
	if db == nil {
		return fmt.Errorf("db not initialized")
	}
	now := time.Now().Unix()
	if rec.CreatedAt == 0 {
		rec.CreatedAt = now
	}
	if rec.UpdatedAt == 0 {
		rec.UpdatedAt = rec.CreatedAt
	}
	_, err := db.Exec(`INSERT INTO xt_market_listings (`+marketListingColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.AuthorUserID, rec.BotInstanceID, rec.Kind, rec.Name, rec.Description,
		rec.FeeModel, rec.FeePercent, rec.MonthlyFee, rec.Status, rec.RejectReason,
		rec.ProbationStartedAt, rec.RuleMinDays, rec.RuleMinTrades, rec.RuleMaxDrawdownPct,
		rec.ReviewedBy, rec.ReviewedAt, rec.ListedAt, rec.DelistReason, rec.CreatedAt, rec.UpdatedAt)
	return err
}

// Update 全字段回写（状态机迁移由 marketplace 包判定后调用）。
func (r *MarketListingRepo) Update(rec *MarketListingRecord) error {
	if db == nil {
		return fmt.Errorf("db not initialized")
	}
	rec.UpdatedAt = time.Now().Unix()
	_, err := db.Exec(`UPDATE xt_market_listings SET
		author_user_id=?, bot_instance_id=?, kind=?, name=?, description=?,
		fee_model=?, fee_percent=?, monthly_fee=?, status=?, reject_reason=?,
		probation_started_at=?, rule_min_days=?, rule_min_trades=?, rule_max_drawdown_pct=?,
		reviewed_by=?, reviewed_at=?, listed_at=?, delist_reason=?, created_at=?, updated_at=?
		WHERE id=?`,
		rec.AuthorUserID, rec.BotInstanceID, rec.Kind, rec.Name, rec.Description,
		rec.FeeModel, rec.FeePercent, rec.MonthlyFee, rec.Status, rec.RejectReason,
		rec.ProbationStartedAt, rec.RuleMinDays, rec.RuleMinTrades, rec.RuleMaxDrawdownPct,
		rec.ReviewedBy, rec.ReviewedAt, rec.ListedAt, rec.DelistReason, rec.CreatedAt, rec.UpdatedAt,
		rec.ID)
	return err
}

func (r *MarketListingRepo) GetByID(id string) *MarketListingRecord {
	if db == nil {
		return nil
	}
	rec, err := scanMarketListing(db.QueryRow(`SELECT `+marketListingColumns+` FROM xt_market_listings WHERE id=?`, id))
	if err != nil {
		return nil
	}
	return rec
}

func (r *MarketListingRepo) ListByAuthor(authorUserID int64) []*MarketListingRecord {
	return r.listWhere(`WHERE author_user_id=? ORDER BY updated_at DESC`, authorUserID)
}

func (r *MarketListingRepo) ListByStatus(status string) []*MarketListingRecord {
	return r.listWhere(`WHERE status=? ORDER BY updated_at DESC`, status)
}

// ListListed 公开市场列表（listed），sort: return|drawdown|followers（默认 updated）。
func (r *MarketListingRepo) ListListed(sort string, asc bool, limit, offset int) []*MarketListingRecord {
	col := "updated_at"
	dir := "DESC"
	switch sort {
	case "return":
		col = `COALESCE((SELECT s.total_return_pct FROM xt_market_listing_stats s WHERE s.listing_id = xt_market_listings.id ORDER BY s.date DESC LIMIT 1), 0)`
	case "drawdown":
		col = `COALESCE((SELECT s.max_drawdown_pct FROM xt_market_listing_stats s WHERE s.listing_id = xt_market_listings.id ORDER BY s.date DESC LIMIT 1), 0)`
	case "followers":
		col = `COALESCE((SELECT s.followers FROM xt_market_listing_stats s WHERE s.listing_id = xt_market_listings.id ORDER BY s.date DESC LIMIT 1), 0)`
	}
	if asc {
		dir = "ASC"
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return r.listWhere(fmt.Sprintf(`WHERE status='listed' ORDER BY %s %s, updated_at DESC LIMIT ? OFFSET ?`, col, dir), limit, offset)
}

func (r *MarketListingRepo) CountListed() int {
	if db == nil {
		return 0
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM xt_market_listings WHERE status='listed'`).Scan(&n); err != nil {
		return 0
	}
	return n
}

func (r *MarketListingRepo) listWhere(where string, args ...any) []*MarketListingRecord {
	if db == nil {
		return nil
	}
	rows, err := db.Query(`SELECT `+marketListingColumns+` FROM xt_market_listings `+where, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []*MarketListingRecord
	for rows.Next() {
		rec, err := scanMarketListing(rows)
		if err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// ── 统计快照 ──

// UpsertStats 按 (listing_id, date) 幂等写日快照。
func (r *MarketListingRepo) UpsertStats(s *MarketListingStatsRecord) error {
	if db == nil {
		return fmt.Errorf("db not initialized")
	}
	if s.CreatedAt == 0 {
		s.CreatedAt = time.Now().Unix()
	}
	_, err := db.Exec(`INSERT INTO xt_market_listing_stats
		(listing_id, date, total_return_pct, annualized_return_pct, max_drawdown_pct,
		 win_rate, profit_factor, sharpe_ratio, total_trades, monthly_return_pct,
		 followers, running_days, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(listing_id, date) DO UPDATE SET
			total_return_pct=excluded.total_return_pct,
			annualized_return_pct=excluded.annualized_return_pct,
			max_drawdown_pct=excluded.max_drawdown_pct,
			win_rate=excluded.win_rate,
			profit_factor=excluded.profit_factor,
			sharpe_ratio=excluded.sharpe_ratio,
			total_trades=excluded.total_trades,
			monthly_return_pct=excluded.monthly_return_pct,
			followers=excluded.followers,
			running_days=excluded.running_days`,
		s.ListingID, s.Date, s.TotalReturnPct, s.AnnualizedReturnPct, s.MaxDrawdownPct,
		s.WinRate, s.ProfitFactor, s.SharpeRatio, s.TotalTrades, s.MonthlyReturnPct,
		s.Followers, s.RunningDays, s.CreatedAt)
	return err
}

func scanMarketStats(row interface{ Scan(...any) error }) (*MarketListingStatsRecord, error) {
	var s MarketListingStatsRecord
	err := row.Scan(&s.ListingID, &s.Date, &s.TotalReturnPct, &s.AnnualizedReturnPct, &s.MaxDrawdownPct,
		&s.WinRate, &s.ProfitFactor, &s.SharpeRatio, &s.TotalTrades, &s.MonthlyReturnPct,
		&s.Followers, &s.RunningDays, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

const marketStatsColumns = `listing_id, date, total_return_pct, annualized_return_pct, max_drawdown_pct,
	win_rate, profit_factor, sharpe_ratio, total_trades, monthly_return_pct,
	followers, running_days, created_at`

// LatestStats 取最新快照（市场卡片数据源）；无快照返回 nil。
func (r *MarketListingRepo) LatestStats(listingID string) *MarketListingStatsRecord {
	if db == nil {
		return nil
	}
	s, err := scanMarketStats(db.QueryRow(`SELECT `+marketStatsColumns+`
		FROM xt_market_listing_stats WHERE listing_id=? ORDER BY date DESC LIMIT 1`, listingID))
	if err != nil {
		return nil
	}
	return s
}

// StatsSeries 取快照序列（升序），limit<=0 时默认 90。
func (r *MarketListingRepo) StatsSeries(listingID string, limit int) []*MarketListingStatsRecord {
	if db == nil {
		return nil
	}
	if limit <= 0 {
		limit = 90
	}
	rows, err := db.Query(`SELECT `+marketStatsColumns+`
		FROM xt_market_listing_stats WHERE listing_id=? ORDER BY date ASC LIMIT ?`, listingID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []*MarketListingStatsRecord{}
	for rows.Next() {
		s, err := scanMarketStats(rows)
		if err != nil {
			continue
		}
		out = append(out, s)
	}
	return out
}

// CountActiveFollowers 统计该条目付费/免费跟踪者数：
// 以上架条目 id 为 catalog_id 创建的实例上的 active 订阅数。
func (r *MarketListingRepo) CountActiveFollowers(listingID string) int {
	if db == nil {
		return 0
	}
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM ai_bot_subscriptions s
		JOIN ai_bot_instances i ON s.bot_instance_id = i.id
		WHERE i.catalog_id = ? AND s.status = 'active'`, listingID).Scan(&n)
	if err != nil {
		return 0
	}
	return n
}

// ── 考核规则（全局设置） ──

// GetMarketSetting 读单个设置项；不存在返回空串。
func GetMarketSetting(key string) string {
	if db == nil {
		return ""
	}
	var v string
	if err := db.QueryRow(`SELECT value FROM xt_market_settings WHERE key=?`, key).Scan(&v); err != nil {
		return ""
	}
	return v
}

// SetMarketSetting 写设置项（upsert）。
func SetMarketSetting(key, value string) error {
	if db == nil {
		return fmt.Errorf("db not initialized")
	}
	_, err := db.Exec(`INSERT INTO xt_market_settings (key, value, updated_at) VALUES (?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, time.Now().Unix())
	return err
}

// ── AI 机器人实例考核窗口查询 ──

// SaveAIBotSnapshotAt 显式时间戳版快照写入（考核统计回填/测试用）。
func SaveAIBotSnapshotAt(botInstanceID string, equity, unrealized, realized, totalReturn float64, ts int64) {
	_, err := db.Exec(`
		INSERT INTO ai_bot_snapshots
		(bot_instance_id, total_equity, unrealized_pnl, realized_pnl, total_return_pct, timestamp)
		VALUES (?, ?, ?, ?, ?, ?)
	`, botInstanceID, equity, unrealized, realized, totalReturn, ts)
	if err != nil {
		fmt.Printf("[AIBotStore] snapshot-at error: %v\n", err)
	}
}

// GetAIBotSnapshotsSince 取 ts >= since 的快照（时间升序），供考核窗口权益曲线。
func GetAIBotSnapshotsSince(botInstanceID string, since int64) []map[string]any {
	rows, err := db.Query(`
		SELECT total_equity, unrealized_pnl, realized_pnl, total_return_pct, timestamp
		FROM ai_bot_snapshots WHERE bot_instance_id=? AND timestamp>=? ORDER BY timestamp ASC
	`, botInstanceID, since)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		var equity, unr, real, tr float64
		var ts int64
		if err := rows.Scan(&equity, &unr, &real, &tr, &ts); err != nil {
			continue
		}
		result = append(result, map[string]any{
			"total_equity": equity, "unrealized_pnl": unr,
			"realized_pnl": real, "total_return_pct": tr, "timestamp": ts,
		})
	}
	return result
}

// GetAIBotTradesSince 取 closed_at >= since 的已平仓交易（时间升序）。
func GetAIBotTradesSince(botInstanceID string, since int64) []map[string]any {
	rows, err := db.Query(`
		SELECT id, bot_instance_id, symbol, side, entry_price, exit_price, quantity,
		       pnl, pnl_pct, close_reason, opened_at, closed_at
		FROM ai_bot_trades WHERE bot_instance_id=? AND closed_at>=? ORDER BY closed_at ASC
	`, botInstanceID, since)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		var id int
		var botID, symbol, side, closeReason string
		var entryPrice, exitPrice, qty, pnl, pnlPct float64
		var openedAt, closedAt int64
		if err := rows.Scan(&id, &botID, &symbol, &side, &entryPrice, &exitPrice, &qty,
			&pnl, &pnlPct, &closeReason, &openedAt, &closedAt); err != nil {
			continue
		}
		result = append(result, map[string]any{
			"id": id, "bot_instance_id": botID, "symbol": symbol, "side": side,
			"entry_price": entryPrice, "exit_price": exitPrice, "quantity": qty,
			"pnl": pnl, "pnl_pct": pnlPct, "close_reason": closeReason,
			"opened_at": openedAt, "closed_at": closedAt,
		})
	}
	return result
}

// ── 上架后的 ai_bot_catalog 同步（复用既有实例创建/订阅流） ──

// UpsertMarketCatalogEntry 审核通过时把条目写入 ai_bot_catalog（is_builtin=0），
// 使用户可通过既有 catalog→instance→subscription 流跟踪/订阅该条目。
func UpsertMarketCatalogEntry(rec *MarketListingRecord, strategyType, marketType string, now int64) error {
	if db == nil {
		return fmt.Errorf("db not initialized")
	}
	if strategyType == "" {
		strategyType = "ai_alpha"
	}
	if marketType == "" {
		marketType = "spot"
	}
	_, err := db.Exec(`INSERT INTO ai_bot_catalog
		(id, name, description, strategy_type, market_type, risk_level,
		 fee_model, fee_percent, monthly_fee, performance_json, config_json,
		 is_builtin, is_active, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,'','{}',0,1,?,?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, description=excluded.description,
			strategy_type=excluded.strategy_type, market_type=excluded.market_type,
			fee_model=excluded.fee_model, fee_percent=excluded.fee_percent,
			monthly_fee=excluded.monthly_fee, is_active=1, updated_at=excluded.updated_at`,
		rec.ID, rec.Name, rec.Description, strategyType, marketType, "medium",
		rec.FeeModel, rec.FeePercent, rec.MonthlyFee, now, now)
	return err
}

// DeactivateMarketCatalogEntry 下架时关闭 catalog 可见性（不再允许新订阅）。
func DeactivateMarketCatalogEntry(id string) {
	if db == nil {
		return
	}
	db.Exec(`UPDATE ai_bot_catalog SET is_active=0, updated_at=? WHERE id=?`, time.Now().Unix(), id)
}
