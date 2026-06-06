package community

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Service ───────────────────────────────────────────────────────

type Service struct{}

func NewService() *Service { return &Service{} }

// ── Overfit Detection ─────────────────────────────────────────────

// OverfitResult holds the overfit risk analysis for an indicator.
type OverfitResult struct {
	Score           float64 `json:"score"`             // 0-100, higher = more overfit
	RiskLevel       string  `json:"risk_level"`        // "low", "medium", "high"
	InSampleReturn  float64 `json:"in_sample_return"`  // training period return
	OutSampleReturn float64 `json:"out_sample_return"` // test period return
	ReturnRatio     float64 `json:"return_ratio"`      // out_sample / in_sample
	StabilityScore  float64 `json:"stability_score"`   // parameter stability
}

// ComputeOverfitRisk calculates overfit risk from in-sample vs out-sample returns.
// sampleRatio is the fraction used for training (e.g., 0.7 = 70% train, 30% test).
func ComputeOverfitRisk(totalReturn, sharpe, maxDrawdown float64, totalTrades int, sampleRatio float64) *OverfitResult {
	if totalTrades < 10 || sampleRatio <= 0 || sampleRatio >= 1 {
		return &OverfitResult{Score: 0, RiskLevel: "insufficient_data"}
	}

	// Estimate in-sample vs out-sample returns using the ratio
	// Simplified model: if 70% train, 30% test
	inSampleReturn := totalReturn * sampleRatio
	outSampleReturn := totalReturn * (1 - sampleRatio)

	// Return ratio: how much of the return persists out-of-sample
	returnRatio := 0.0
	if math.Abs(inSampleReturn) > 0.01 {
		returnRatio = outSampleReturn / inSampleReturn
	}

	// Stability score based on Sharpe and number of trades
	stabilityScore := 50.0
	if sharpe > 1.0 {
		stabilityScore += (sharpe - 1.0) * 10
	}
	if totalTrades > 50 {
		stabilityScore += float64(totalTrades-50) * 0.2
	}
	stabilityScore = math.Min(stabilityScore, 100)
	stabilityScore = math.Max(stabilityScore, 0)

	// Overfit score: lower return ratio + lower stability = higher overfit
	overfitScore := (1.0 - returnRatio) * 50 + (1.0 - stabilityScore/100) * 50
	if returnRatio < 0 {
		overfitScore = 90 // negative out-sample = severe overfit
	}
	overfitScore = math.Min(overfitScore, 100)
	overfitScore = math.Max(overfitScore, 0)

	riskLevel := "low"
	if overfitScore > 60 {
		riskLevel = "high"
	} else if overfitScore > 30 {
		riskLevel = "medium"
	}

	return &OverfitResult{
		Score:           math.Round(overfitScore*10) / 10,
		RiskLevel:       riskLevel,
		InSampleReturn:  math.Round(inSampleReturn*100) / 100,
		OutSampleReturn: math.Round(outSampleReturn*100) / 100,
		ReturnRatio:     math.Round(returnRatio*100) / 100,
		StabilityScore:  math.Round(stabilityScore*10) / 10,
	}
}

// SaveOverfitRisk persists overfit analysis for a strategy.
func (s *Service) SaveOverfitRisk(strategyID int, result *OverfitResult) error {
	db := store.GetDB()
	if db == nil || result == nil {
		return fmt.Errorf("database not available")
	}
	now := time.Now().Unix()
	_, err := db.Exec(`
		INSERT INTO strategy_overfit (strategy_id, score, risk_level, in_sample_return, out_sample_return, return_ratio, stability_score, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(strategy_id) DO UPDATE SET
			score = excluded.score,
			risk_level = excluded.risk_level,
			in_sample_return = excluded.in_sample_return,
			out_sample_return = excluded.out_sample_return,
			return_ratio = excluded.return_ratio,
			stability_score = excluded.stability_score,
			updated_at = excluded.updated_at`,
		strategyID, result.Score, result.RiskLevel, result.InSampleReturn, result.OutSampleReturn,
		result.ReturnRatio, result.StabilityScore, now,
	)
	return err
}

// GetOverfitRisk retrieves persisted overfit analysis for a strategy.
func (s *Service) GetOverfitRisk(strategyID int) (*OverfitResult, error) {
	db := store.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not available")
	}
	var result OverfitResult
	err := db.QueryRow(`
		SELECT score, risk_level, in_sample_return, out_sample_return, return_ratio, stability_score
		FROM strategy_overfit WHERE strategy_id = ?`, strategyID,
	).Scan(&result.Score, &result.RiskLevel, &result.InSampleReturn, &result.OutSampleReturn, &result.ReturnRatio, &result.StabilityScore)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ReviewStatus constants for indicator moderation.
const (
	ReviewPending  = "pending"
	ReviewApproved = "approved"
	ReviewRejected = "rejected"
)

// ReviewResult holds moderation outcome.
type ReviewResult struct {
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	ReviewedAt int64 `json:"reviewed_at"`
	ReviewerID int   `json:"reviewer_id"`
}

// ReviewIndicator approves or rejects a pending indicator.
func (s *Service) ReviewIndicator(indicatorID int, reviewerID int, approve bool, reason string) (*ReviewResult, error) {
	db := store.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not available")
	}

	// Verify indicator exists and is pending
	var currentStatus string
	err := db.QueryRow(`SELECT review_status FROM indicator_codes WHERE id = ?`, indicatorID).Scan(&currentStatus)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("indicator not found")
	}
	if err != nil {
		return nil, err
	}
	if currentStatus != ReviewPending && currentStatus != "" {
		return nil, fmt.Errorf("indicator is not pending review (current: %s)", currentStatus)
	}

	now := time.Now().Unix()
	status := ReviewRejected
	if approve {
		status = ReviewApproved
		reason = ""
	}

	_, err = db.Exec(
		`UPDATE indicator_codes SET review_status = ?, review_reason = ?, reviewed_at = ?, reviewer_id = ? WHERE id = ?`,
		status, reason, now, reviewerID, indicatorID,
	)
	if err != nil {
		return nil, err
	}

	return &ReviewResult{
		Status:     status,
		Reason:     reason,
		ReviewedAt: now,
		ReviewerID: reviewerID,
	}, nil
}

// GetPendingReviews returns indicators awaiting moderation.
func (s *Service) GetPendingReviews(page, pageSize int) ([]MarketIndicator, int, error) {
	db := store.GetDB()
	if db == nil {
		return nil, 0, fmt.Errorf("database not available")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	var total int
	db.QueryRow(`SELECT COUNT(*) FROM indicator_codes WHERE publish_to_community = 1 AND review_status = 'pending'`).Scan(&total)

	rows, err := db.Query(
		`SELECT id, name, description, pricing_type, price, purchase_count, avg_rating, rating_count, view_count, user_id, created_at
		 FROM indicator_codes WHERE publish_to_community = 1 AND review_status = 'pending'
		 ORDER BY created_at ASC LIMIT ? OFFSET ?`,
		pageSize, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var indicators []MarketIndicator
	var ids []int
	for rows.Next() {
		var mi MarketIndicator
		var desc sql.NullString
		if err := rows.Scan(&mi.ID, &mi.Name, &desc, &mi.PricingType, &mi.Price, &mi.PurchaseCount, &mi.AvgRating, &mi.RatingCount, &mi.ViewCount, &mi.AuthorID, &mi.CreatedAt); err != nil {
			continue
		}
		mi.Description = desc.String
		indicators = append(indicators, mi)
		ids = append(ids, mi.ID)
	}

	authorNames := s.resolveAuthorNames(db, extractUniqueAuthorIDs(indicators))
	for i := range indicators {
		indicators[i].AuthorName = authorNames[indicators[i].AuthorID]
	}

	return indicators, total, nil
}

// ── Revenue Sharing ────────────────────────────────────────────

// RevenueShareConfig configures how purchase revenue is split.
type RevenueShareConfig struct {
	AuthorPct    float64 // author percentage (e.g. 0.70 = 70%)
	PlatformPct  float64 // platform percentage (e.g. 0.30 = 30%)
}

func DefaultRevenueShareConfig() RevenueShareConfig {
	return RevenueShareConfig{AuthorPct: 0.70, PlatformPct: 0.30}
}

// RevenueRecord tracks a single purchase's revenue distribution.
type RevenueRecord struct {
	ID          int     `json:"id"`
	IndicatorID int     `json:"indicator_id"`
	BuyerID     int     `json:"buyer_id"`
	SellerID    int     `json:"seller_id"`
	Price       float64 `json:"price"`
	AuthorShare float64 `json:"author_share"`
	PlatformShare float64 `json:"platform_share"`
	CreatedAt   int64   `json:"created_at"`
}

// RecordRevenue records the revenue split for a purchase.
func (s *Service) RecordRevenue(indicatorID, buyerID, sellerID int, price float64, cfg RevenueShareConfig) error {
	db := store.GetDB()
	if db == nil {
		return fmt.Errorf("database not available")
	}

	authorShare := price * cfg.AuthorPct
	platformShare := price * cfg.PlatformPct

	_, err := db.Exec(
		`INSERT INTO indicator_revenue (indicator_id, buyer_id, seller_id, price, author_share, platform_share, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		indicatorID, buyerID, sellerID, price, authorShare, platformShare, time.Now().Unix(),
	)
	return err
}

// GetAuthorRevenue returns total revenue for an author.
func (s *Service) GetAuthorRevenue(authorID int) (totalSales int, totalRevenue float64, err error) {
	db := store.GetDB()
	if db == nil {
		return 0, 0, fmt.Errorf("database not available")
	}

	var sales int
	var revenue float64
	err = db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(author_share), 0) FROM indicator_revenue WHERE seller_id = ?`,
		authorID,
	).Scan(&sales, &revenue)
	return sales, revenue, err
}

// GetAuthorRevenueDetails returns per-indicator revenue breakdown.
func (s *Service) GetAuthorRevenueDetails(authorID int) ([]IndicatorRevenue, error) {
	db := store.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not available")
	}

	rows, err := db.Query(
		`SELECT indicator_id, COUNT(*) as sales, COALESCE(SUM(author_share), 0) as revenue
		 FROM indicator_revenue WHERE seller_id = ? GROUP BY indicator_id ORDER BY revenue DESC`,
		authorID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []IndicatorRevenue
	for rows.Next() {
		var r IndicatorRevenue
		rows.Scan(&r.IndicatorID, &r.Sales, &r.Revenue)
		result = append(result, r)
	}
	return result, nil
}

// IndicatorRevenue is revenue summary for a single indicator.
type IndicatorRevenue struct {
	IndicatorID int     `json:"indicator_id"`
	Sales       int     `json:"sales"`
	Revenue     float64 `json:"revenue"`
}

// ── Wilson Score Rating ───────────────────────────────────────

// ComputeWilsonScore calculates the lower bound of Wilson score confidence interval.
// This gives a more reliable ranking than raw average rating, especially with few reviews.
// Formula: (p + z²/2n - z*sqrt((p(1-p)+z²/4n)/n)) / (1+z²/n)
// where p = positive proportion, z = 1.96 (95% CI), n = total ratings.
func ComputeWilsonScore(avgRating float64, ratingCount int) float64 {
	if ratingCount == 0 {
		return 0
	}
	// Normalize avgRating from [1,5] to [0,1] proportion
	p := (avgRating - 1.0) / 4.0
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}

	n := float64(ratingCount)
	z := 1.96 // 95% confidence

	// Wilson score lower bound
	dividend := p + z*z/(2*n) - z*math.Sqrt((p*(1-p)+z*z/(4*n))/n)
	divisor := 1 + z*z/n

	score := dividend / divisor
	if score < 0 {
		score = 0
	}

	// Scale back to [1,5] range
	return 1.0 + score*4.0
}

// GetMarketIndicatorsWithWilsonScore returns indicators with Wilson score ranking.
func (s *Service) GetMarketIndicatorsWithWilsonScore(userID int, page, pageSize int, keyword, pricingType string) ([]MarketIndicator, int, error) {
	indicators, total, err := s.GetMarketIndicators(userID, page, pageSize, keyword, pricingType, "")
	if err != nil {
		return nil, 0, err
	}

	// Compute Wilson score for each indicator
	for i := range indicators {
		indicators[i].Score = ComputeWilsonScore(indicators[i].AvgRating, indicators[i].RatingCount)
	}

	// Sort by Wilson score descending
	for i := 0; i < len(indicators)-1; i++ {
		for j := i + 1; j < len(indicators); j++ {
			if indicators[j].Score > indicators[i].Score {
				indicators[i], indicators[j] = indicators[j], indicators[i]
			}
		}
	}

	return indicators, total, nil
}
type MarketIndicator struct {
	ID                   int                `json:"id"`
	Name                 string             `json:"name"`
	Description          string             `json:"description"`
	PricingType          string             `json:"pricing_type"`
	Price                float64            `json:"price"`
	PurchaseCount        int                `json:"purchase_count"`
	AvgRating            float64            `json:"avg_rating"`
	RatingCount          int                `json:"rating_count"`
	ViewCount            int                `json:"view_count"`
	Score                float64            `json:"score"`
	TotalReturn          float64            `json:"total_return"`
	Sharpe               float64            `json:"sharpe"`
	MaxDrawdown          float64            `json:"max_drawdown"`
	AuthorID             int                `json:"author_id"`
	AuthorName           string             `json:"author_name"`
	IsPurchased          bool               `json:"is_purchased"`
	IsOwn                bool               `json:"is_own"`
	CreatedAt            int64              `json:"created_at"`
}

// PublishIndicator publishes a user's indicator to the community marketplace.
func (s *Service) PublishIndicator(userID, indicatorID int, pricingType string, price float64) error {
	db := store.GetDB()
	if db == nil {
		return fmt.Errorf("database not available")
	}
	_, err := db.Exec(
		`UPDATE indicator_codes SET publish_to_community = 1, pricing_type = ?, price = ?, review_status = 'pending', updated_at = ? WHERE id = ? AND user_id = ?`,
		pricingType, price, time.Now().Unix(), indicatorID, userID,
	)
	return err
}

// GetMarketIndicators returns the community marketplace listing.
func (s *Service) GetMarketIndicators(userID int, page, pageSize int, keyword, pricingType, sortBy string) ([]MarketIndicator, int, error) {
	db := store.GetDB()
	if db == nil {
		return nil, 0, fmt.Errorf("database not available")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 12
	}
	offset := (page - 1) * pageSize

	// Build WHERE
	wheres := []string{"publish_to_community = 1", "(review_status = 'approved' OR review_status IS NULL OR review_status = '')"}
	args := []any{}
	if keyword != "" {
		wheres = append(wheres, "(name LIKE ? OR description LIKE ?)")
		args = append(args, "%"+keyword+"%", "%"+keyword+"%")
	}
	if pricingType == "free" {
		wheres = append(wheres, "(pricing_type = 'free' OR price <= 0)")
	} else if pricingType == "paid" {
		wheres = append(wheres, "pricing_type != 'free' AND price > 0")
	}
	whereSQL := strings.Join(wheres, " AND ")

	// Count total
	var total int
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM indicator_codes WHERE %s", whereSQL)
	if err := db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Order by
	orderSQL := "updated_at DESC"
	switch sortBy {
	case "newest":
		orderSQL = "created_at DESC"
	case "hot":
		orderSQL = "purchase_count DESC, view_count DESC"
	case "rating":
		orderSQL = "avg_rating DESC, rating_count DESC"
	case "price_asc":
		orderSQL = "price ASC"
	case "price_desc":
		orderSQL = "price DESC"
	case "score":
		// score is synthetic; fall back to composite ordering
		orderSQL = "(avg_rating * rating_count + purchase_count) DESC"
	}

	querySQL := fmt.Sprintf(
		`SELECT id, name, description, pricing_type, price, purchase_count, avg_rating, rating_count, view_count, user_id, created_at
		 FROM indicator_codes WHERE %s ORDER BY %s LIMIT ? OFFSET ?`,
		whereSQL, orderSQL,
	)
	args = append(args, pageSize, offset)

	rows, err := db.Query(querySQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var indicators []MarketIndicator
	var ids []int
	for rows.Next() {
		var mi MarketIndicator
		var desc sql.NullString
		err := rows.Scan(&mi.ID, &mi.Name, &desc, &mi.PricingType, &mi.Price, &mi.PurchaseCount, &mi.AvgRating, &mi.RatingCount, &mi.ViewCount, &mi.AuthorID, &mi.CreatedAt)
		if err != nil {
			continue
		}
		mi.Description = desc.String
		indicators = append(indicators, mi)
		ids = append(ids, mi.ID)
	}

	// Batch check purchased status
	if userID > 0 && len(ids) > 0 {
		purchased := s.getPurchasedIDs(userID, ids)
		for i := range indicators {
			indicators[i].IsPurchased = purchased[indicators[i].ID]
			indicators[i].IsOwn = indicators[i].AuthorID == userID
		}
	}

	// Resolve author names
	authorNames := s.resolveAuthorNames(db, extractUniqueAuthorIDs(indicators))
	for i := range indicators {
		indicators[i].AuthorName = authorNames[indicators[i].AuthorID]
	}

	return indicators, total, nil
}

// PurchaseIndicator handles buying an indicator.
func (s *Service) PurchaseIndicator(buyerID, indicatorID int) (bool, string, error) {
	db := store.GetDB()
	if db == nil {
		return false, "database not available", fmt.Errorf("database not available")
	}

	var sellerID int
	var price float64
	var pricingType string
	err := db.QueryRow(
		`SELECT user_id, price, pricing_type FROM indicator_codes WHERE id = ? AND publish_to_community = 1`,
		indicatorID,
	).Scan(&sellerID, &price, &pricingType)
	if err == sql.ErrNoRows {
		return false, "indicator_not_found", nil
	}
	if err != nil {
		return false, "db_error", err
	}

	if sellerID == buyerID {
		return false, "cannot_buy_own", nil
	}

	// Check already purchased
	var existing int
	err = db.QueryRow(`SELECT 1 FROM indicator_purchases WHERE indicator_id = ? AND buyer_id = ?`, indicatorID, buyerID).Scan(&existing)
	if err == nil {
		return false, "already_purchased", nil
	}

	// Copy indicator to buyer
	var code, name, description string
	var isEncrypted int
	err = db.QueryRow(
		`SELECT code, name, description, is_encrypted FROM indicator_codes WHERE id = ?`, indicatorID,
	).Scan(&code, &name, &description, &isEncrypted)
	if err != nil {
		return false, "indicator_not_found", nil
	}

	now := time.Now().Unix()
	db.Exec(
		`INSERT INTO indicator_codes (user_id, name, description, code, is_encrypted, source_indicator_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		buyerID, name, description, code, isEncrypted, indicatorID, now, now,
	)
	db.Exec(
		`INSERT INTO indicator_purchases (indicator_id, buyer_id, seller_id, price, created_at) VALUES (?, ?, ?, ?, ?)`,
		indicatorID, buyerID, sellerID, price, now,
	)
	db.Exec(
		`UPDATE indicator_codes SET purchase_count = COALESCE(purchase_count, 0) + 1 WHERE id = ?`, indicatorID,
	)

	return true, "success", nil
}

// AddComment adds a rating+comment to an indicator.
func (s *Service) AddComment(userID, indicatorID int, rating int, content string) (bool, string, error) {
	db := store.GetDB()
	if db == nil {
		return false, "database not available", fmt.Errorf("database not available")
	}

	// Must be purchased (or own)
	var owned int
	err := db.QueryRow(`SELECT 1 FROM indicator_codes WHERE id = ? AND user_id = ?`, indicatorID, userID).Scan(&owned)
	if err != nil {
		err = db.QueryRow(`SELECT 1 FROM indicator_purchases WHERE indicator_id = ? AND buyer_id = ?`, indicatorID, userID).Scan(&owned)
		if err != nil {
			return false, "not_purchased", nil
		}
	}

	// One comment per user per indicator
	var existing int
	err = db.QueryRow(`SELECT 1 FROM indicator_comments WHERE indicator_id = ? AND user_id = ? AND parent_id = 0`, indicatorID, userID).Scan(&existing)
	if err == nil {
		return false, "already_commented", nil
	}

	rating = max(1, min(5, rating))
	content = strings.TrimSpace(content)
	if len(content) > 500 {
		content = content[:500]
	}
	now := time.Now().Unix()

	_, err = db.Exec(
		`INSERT INTO indicator_comments (indicator_id, user_id, rating, content, parent_id, created_at, updated_at) VALUES (?, ?, ?, ?, 0, ?, ?)`,
		indicatorID, userID, rating, content, now, now,
	)
	if err != nil {
		return false, "db_error", err
	}

	// Update avg_rating on indicator_codes
	db.Exec(`
		UPDATE indicator_codes SET
			rating_count = (SELECT COUNT(*) FROM indicator_comments WHERE indicator_id = ? AND parent_id = 0 AND is_deleted = 0),
			avg_rating = COALESCE((SELECT AVG(rating) FROM indicator_comments WHERE indicator_id = ? AND parent_id = 0 AND is_deleted = 0), 0)
		WHERE id = ?`,
		indicatorID, indicatorID, indicatorID,
	)

	return true, "success", nil
}

// GetComments returns comments for an indicator.
func (s *Service) GetComments(indicatorID int, page, pageSize int) ([]Comment, int, error) {
	db := store.GetDB()
	if db == nil {
		return nil, 0, fmt.Errorf("database not available")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	var total int
	db.QueryRow(`SELECT COUNT(*) FROM indicator_comments WHERE indicator_id = ? AND parent_id = 0 AND is_deleted = 0`, indicatorID).Scan(&total)

	rows, err := db.Query(`
		SELECT c.id, c.rating, c.content, c.created_at, u.id as uid, u.nickname, u.username
		FROM indicator_comments c
		LEFT JOIN xt_users u ON c.user_id = u.id
		WHERE c.indicator_id = ? AND c.parent_id = 0 AND c.is_deleted = 0
		ORDER BY c.created_at DESC LIMIT ? OFFSET ?`,
		indicatorID, pageSize, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var comments []Comment
	for rows.Next() {
		var c Comment
		var nick, uname sql.NullString
		err := rows.Scan(&c.ID, &c.Rating, &c.Content, &c.CreatedAt, &c.UserID, &nick, &uname)
		if err != nil {
			continue
		}
		c.UserNickname = coalesceString(nick.String, uname.String, "User")
		comments = append(comments, c)
	}
	return comments, total, nil
}

// Comment is a user review on an indicator.
type Comment struct {
	ID           int    `json:"id"`
	Rating       int    `json:"rating"`
	Content      string `json:"content"`
	CreatedAt    int64  `json:"created_at"`
	UserID       int    `json:"user_id"`
	UserNickname string `json:"user_nickname"`
}

// ── Helpers ───────────────────────────────────────────────────────

func (s *Service) getPurchasedIDs(userID int, indicatorIDs []int) map[int]bool {
	result := make(map[int]bool)
	if len(indicatorIDs) == 0 {
		return result
	}
	db := store.GetDB()
	if db == nil {
		return result
	}
	// SQLite doesn't support unlimited IN clauses; chunk if needed
	placeholders := make([]string, len(indicatorIDs))
	args := make([]any, 0, len(indicatorIDs)+1)
	args = append(args, userID)
	for i, id := range indicatorIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	rows, err := db.Query(
		fmt.Sprintf("SELECT indicator_id FROM indicator_purchases WHERE buyer_id = ? AND indicator_id IN (%s)", strings.Join(placeholders, ",")),
		args...,
	)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			result[id] = true
		}
	}
	return result
}

func (s *Service) resolveAuthorNames(db *sql.DB, authorIDs []int) map[int]string {
	result := make(map[int]string)
	if len(authorIDs) == 0 || db == nil {
		return result
	}
	placeholders := make([]string, len(authorIDs))
	args := make([]any, len(authorIDs))
	for i, id := range authorIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := db.Query(
		fmt.Sprintf("SELECT id, nickname, username FROM xt_users WHERE id IN (%s)", strings.Join(placeholders, ",")),
		args...,
	)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var nick, uname sql.NullString
		if rows.Scan(&id, &nick, &uname) == nil {
			name := coalesceString(nick.String, uname.String, "User")
			result[id] = name
		}
	}
	return result
}

func extractUniqueAuthorIDs(items []MarketIndicator) []int {
	seen := make(map[int]bool)
	var ids []int
	for _, item := range items {
		if !seen[item.AuthorID] {
			seen[item.AuthorID] = true
			ids = append(ids, item.AuthorID)
		}
	}
	return ids
}

func coalesceString(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ── i18n helpers ──────────────────────────────────────────────────

// PickLocalized selects the best translation from an i18n payload.
func PickLocalized(rawText string, i18nPayload string, acceptLang, sourceLang string) string {
	if i18nPayload == "" {
		return rawText
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(i18nPayload), &payload); err != nil {
		return rawText
	}
	if payload == nil {
		return rawText
	}

	// Exact match
	if v := payload[acceptLang]; v != "" {
		return v
	}
	// Prefix match (e.g. zh-HK -> zh-CN)
	if acceptLang != "" && strings.Contains(acceptLang, "-") {
		prefix := strings.Split(acceptLang, "-")[0] + "-"
		for k, v := range payload {
			if strings.HasPrefix(strings.ToLower(k), strings.ToLower(prefix)) && v != "" {
				return v
			}
		}
	}
	// English fallback
	if v := payload["en-US"]; v != "" {
		return v
	}
	// Source language fallback
	if v := payload[sourceLang]; v != "" {
		return v
	}
	return rawText
}

// DetectSourceLanguage makes a rough guess of the text's language.
func DetectSourceLanguage(text string) string {
	if text == "" {
		return "en-US"
	}
	hasCJK := strings.ContainsFunc(text, func(r rune) bool { return r >= '一' && r <= '鿿' })
	hasKana := strings.ContainsFunc(text, func(r rune) bool { return r >= '぀' && r <= 'ヿ' })
	hasHangul := strings.ContainsFunc(text, func(r rune) bool { return r >= '가' && r <= '힯' })
	hasArabic := strings.ContainsFunc(text, func(r rune) bool { return r >= '؀' && r <= 'ۿ' })

	if hasKana {
		return "ja-JP"
	}
	if hasHangul {
		return "ko-KR"
	}
	if hasArabic {
		return "ar-SA"
	}
	if hasCJK {
		return "zh-CN"
	}
	return "en-US"
}
