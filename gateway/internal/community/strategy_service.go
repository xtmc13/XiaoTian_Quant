package community

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// StrategyItem is the public view of a strategy in the marketplace.
type StrategyItem struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Code         string  `json:"code"`
	Symbol       string  `json:"symbol"`
	Interval     string  `json:"interval"`
	AuthorID     int     `json:"author_id"`
	AuthorName   string  `json:"author_name"`
	// Backtest results
	TotalReturn   float64 `json:"total_return"`
	SharpeRatio   float64 `json:"sharpe_ratio"`
	MaxDrawdown   float64 `json:"max_drawdown"`
	WinRate       float64 `json:"win_rate"`
	TotalTrades   int     `json:"total_trades"`
	ProfitFactor  float64 `json:"profit_factor"`
	// Community
	AvgRating     float64 `json:"avg_rating"`
	RatingCount   int     `json:"rating_count"`
	CommentCount  int     `json:"comment_count"`
	DownloadCount int     `json:"download_count"`
	ViewCount     int     `json:"view_count"`
	IsOwn         bool    `json:"is_own"`
	CreatedAt     int64   `json:"created_at"`
	UpdatedAt     int64   `json:"updated_at"`
	// KPI & Overfit
	KPIScore    *KPIScore    `json:"kpi_score,omitempty"`
	OverfitRisk *OverfitResult `json:"overfit_risk,omitempty"`
}

// StrategyComment is a user comment on a strategy.
type StrategyComment struct {
	ID         int    `json:"id"`
	StrategyID int    `json:"strategy_id"`
	UserID     int    `json:"user_id"`
	UserName   string `json:"user_name"`
	Content    string `json:"content"`
	CreatedAt  int64  `json:"created_at"`
}

// StrategyRating is a user rating for a strategy.
type StrategyRating struct {
	StrategyID int `json:"strategy_id"`
	UserID     int `json:"user_id"`
	Rating     int `json:"rating"` // 1-5
}

// ensureStrategyTables creates the strategy marketplace tables if they don't exist.
func ensureStrategyTables() error {
	db := store.GetDB()
	if db == nil {
		return nil
	}

	db.Exec(`CREATE TABLE IF NOT EXISTS strategy_marketplace (
		id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT DEFAULT '',
		code TEXT NOT NULL,
		symbol TEXT DEFAULT 'BTCUSDT',
		interval TEXT DEFAULT '1h',
		author_id BIGINT NOT NULL,
		author_name TEXT DEFAULT '',
		total_return DOUBLE PRECISION DEFAULT 0,
		sharpe_ratio DOUBLE PRECISION DEFAULT 0,
		max_drawdown DOUBLE PRECISION DEFAULT 0,
		win_rate DOUBLE PRECISION DEFAULT 0,
		total_trades BIGINT DEFAULT 0,
		profit_factor DOUBLE PRECISION DEFAULT 0,
		avg_rating DOUBLE PRECISION DEFAULT 0,
		rating_count BIGINT DEFAULT 0,
		comment_count BIGINT DEFAULT 0,
		download_count BIGINT DEFAULT 0,
		view_count BIGINT DEFAULT 0,
		created_at BIGINT NOT NULL,
		updated_at BIGINT NOT NULL
	)`)

	db.Exec(`CREATE TABLE IF NOT EXISTS strategy_comments (
		id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
		strategy_id BIGINT NOT NULL,
		user_id BIGINT NOT NULL,
		user_name TEXT DEFAULT '',
		content TEXT NOT NULL,
		created_at BIGINT NOT NULL
	)`)

	db.Exec(`CREATE TABLE IF NOT EXISTS strategy_ratings (
		id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
		strategy_id BIGINT NOT NULL,
		user_id BIGINT NOT NULL,
		rating BIGINT NOT NULL,
		UNIQUE(strategy_id, user_id)
	)`)

	return nil
}

// PublishStrategy publishes a strategy to the marketplace.
func (s *Service) PublishStrategy(userID int, name, description, code, symbol, interval, authorName string, bt *BacktestSummary) (int64, error) {
	db := store.GetDB()
	if db == nil {
		return 0, fmt.Errorf("database not available")
	}
	ensureStrategyTables()

	now := time.Now().Unix()

	var totalReturn, sharpe, maxDD, winRate, profitFactor float64
	var totalTrades int
	if bt != nil {
		totalReturn = bt.TotalReturn
		sharpe = bt.Sharpe
		maxDD = bt.MaxDrawdown
		winRate = bt.WinRate
		totalTrades = bt.TotalTrades
		profitFactor = bt.ProfitFactor
	}

	var id int64
	err := db.QueryRow(
		`INSERT INTO strategy_marketplace (name, description, code, symbol, interval, author_id, author_name,
		 total_return, sharpe_ratio, max_drawdown, win_rate, total_trades, profit_factor,
		 created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING id`,
		name, description, code, symbol, interval, userID, authorName,
		totalReturn, sharpe, maxDD, winRate, totalTrades, profitFactor,
		now, now,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// BacktestSummary is embedded in a strategy publication.
type BacktestSummary struct {
	TotalReturn  float64 `json:"total_return"`
	Sharpe       float64 `json:"sharpe"`
	MaxDrawdown  float64 `json:"max_drawdown"`
	WinRate      float64 `json:"win_rate"`
	TotalTrades  int     `json:"total_trades"`
	ProfitFactor float64 `json:"profit_factor"`
}

// GetMarketStrategies returns the strategy marketplace listing.
func (s *Service) GetMarketStrategies(userID int, page, pageSize int, keyword, sortBy string) ([]StrategyItem, int, error) {
	db := store.GetDB()
	if db == nil {
		return nil, 0, nil
	}
	ensureStrategyTables()

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 12
	}
	offset := (page - 1) * pageSize

	wheres := []string{"1=1"}
	args := []any{}

	if keyword != "" {
		wheres = append(wheres, fmt.Sprintf("(name LIKE $%d OR description LIKE $%d)", len(args)+1, len(args)+2))
		args = append(args, "%"+keyword+"%", "%"+keyword+"%")
	}

	whereSQL := strings.Join(wheres, " AND ")

	// Count
	var total int
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM strategy_marketplace WHERE %s", whereSQL)
	db.QueryRow(countSQL, args...).Scan(&total)

	// Sort
	orderSQL := "ORDER BY created_at DESC"
	switch sortBy {
	case "rating":
		orderSQL = "ORDER BY avg_rating DESC"
	case "return":
		orderSQL = "ORDER BY total_return DESC"
	case "sharpe":
		orderSQL = "ORDER BY sharpe_ratio DESC"
	case "popular":
		orderSQL = "ORDER BY download_count DESC"
	case "newest":
		orderSQL = "ORDER BY created_at DESC"
	}

	limitN, offsetN := len(args)+1, len(args)+2
	querySQL := fmt.Sprintf(
		`SELECT id, name, description, code, symbol, interval,
		 author_id, author_name,
		 total_return, sharpe_ratio, max_drawdown, win_rate, total_trades, profit_factor,
		 avg_rating, rating_count, comment_count, download_count, view_count,
		 created_at, updated_at
		 FROM strategy_marketplace WHERE %s %s LIMIT $%d OFFSET $%d`, whereSQL, orderSQL, limitN, offsetN)

	args = append(args, pageSize, offset)
	rows, err := db.Query(querySQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []StrategyItem
	for rows.Next() {
		si := StrategyItem{}
		rows.Scan(
			&si.ID, &si.Name, &si.Description, &si.Code, &si.Symbol, &si.Interval,
			&si.AuthorID, &si.AuthorName,
			&si.TotalReturn, &si.SharpeRatio, &si.MaxDrawdown, &si.WinRate, &si.TotalTrades, &si.ProfitFactor,
			&si.AvgRating, &si.RatingCount, &si.CommentCount, &si.DownloadCount, &si.ViewCount,
			&si.CreatedAt, &si.UpdatedAt,
		)
		si.IsOwn = userID > 0 && si.AuthorID == userID
		items = append(items, si)
	}
	if items == nil {
		items = []StrategyItem{}
	}
	return items, total, nil
}

// GetStrategyDetail returns a single strategy with comments.
func (s *Service) GetStrategyDetail(strategyID int) (*StrategyItem, []StrategyComment, error) {
	db := store.GetDB()
	if db == nil {
		return nil, nil, fmt.Errorf("database not available")
	}
	ensureStrategyTables()

	si := &StrategyItem{}
	err := db.QueryRow(
		`SELECT id, name, description, code, symbol, interval,
		 author_id, author_name,
		 total_return, sharpe_ratio, max_drawdown, win_rate, total_trades, profit_factor,
		 avg_rating, rating_count, comment_count, download_count, view_count,
		 created_at, updated_at
		 FROM strategy_marketplace WHERE id = $1`, strategyID,
	).Scan(
		&si.ID, &si.Name, &si.Description, &si.Code, &si.Symbol, &si.Interval,
		&si.AuthorID, &si.AuthorName,
		&si.TotalReturn, &si.SharpeRatio, &si.MaxDrawdown, &si.WinRate, &si.TotalTrades, &si.ProfitFactor,
		&si.AvgRating, &si.RatingCount, &si.CommentCount, &si.DownloadCount, &si.ViewCount,
		&si.CreatedAt, &si.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil, fmt.Errorf("strategy not found")
	}
	if err != nil {
		return nil, nil, err
	}
	si.IsOwn = false

	// Increment view count
	db.Exec(`UPDATE strategy_marketplace SET view_count = view_count + 1 WHERE id = $1`, strategyID)

	// Load comments
	comments, _ := s.GetStrategyComments(strategyID)

	return si, comments, nil
}

// AddStrategyComment adds a comment to a strategy.
func (s *Service) AddStrategyComment(strategyID, userID int, userName, content string) (*StrategyComment, error) {
	db := store.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not available")
	}
	ensureStrategyTables()

	now := time.Now().Unix()
	var id int64
	err := db.QueryRow(
		`INSERT INTO strategy_comments (strategy_id, user_id, user_name, content, created_at)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		strategyID, userID, userName, content, now,
	).Scan(&id)
	if err != nil {
		return nil, err
	}

	db.Exec(`UPDATE strategy_marketplace SET comment_count = comment_count + 1 WHERE id = $1`, strategyID)

	return &StrategyComment{
		ID:         int(id),
		StrategyID: strategyID,
		UserID:     userID,
		UserName:   userName,
		Content:    content,
		CreatedAt:  now,
	}, nil
}

// GetStrategyComments returns comments for a strategy.
func (s *Service) GetStrategyComments(strategyID int) ([]StrategyComment, error) {
	db := store.GetDB()
	if db == nil {
		return nil, nil
	}

	rows, err := db.Query(
		`SELECT id, strategy_id, user_id, user_name, content, created_at
		 FROM strategy_comments WHERE strategy_id = $1 ORDER BY created_at ASC`,
		strategyID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []StrategyComment
	for rows.Next() {
		c := StrategyComment{}
		rows.Scan(&c.ID, &c.StrategyID, &c.UserID, &c.UserName, &c.Content, &c.CreatedAt)
		comments = append(comments, c)
	}
	if comments == nil {
		comments = []StrategyComment{}
	}
	return comments, nil
}

// RateStrategy adds or updates a rating for a strategy.
func (s *Service) RateStrategy(strategyID, userID, rating int) error {
	db := store.GetDB()
	if db == nil {
		return fmt.Errorf("database not available")
	}
	ensureStrategyTables()

	if rating < 1 {
		rating = 1
	}
	if rating > 5 {
		rating = 5
	}

	// Upsert rating
	db.Exec(
		`INSERT INTO strategy_ratings (strategy_id, user_id, rating) VALUES ($1, $2, $3)
		 ON CONFLICT(strategy_id, user_id) DO UPDATE SET rating = $4`,
		strategyID, userID, rating, rating,
	)

	// Recalculate average
	var avgRating float64
	var count int
	db.QueryRow(
		`SELECT COALESCE(AVG(rating), 0), COUNT(*) FROM strategy_ratings WHERE strategy_id = $1`,
		strategyID,
	).Scan(&avgRating, &count)

	db.Exec(
		`UPDATE strategy_marketplace SET avg_rating = $1, rating_count = $2, updated_at = $3 WHERE id = $4`,
		avgRating, count, time.Now().Unix(), strategyID,
	)

	return nil
}

// GetStrategyLeaderboard returns top strategies by a metric.
func (s *Service) GetStrategyLeaderboard(sortBy string, limit int) ([]StrategyItem, error) {
	if limit < 1 || limit > 50 {
		limit = 10
	}
	if sortBy == "kpi" {
		items, _, err := s.GetMarketStrategies(0, 1, 200, "", "")
		if err != nil {
			return nil, err
		}
		for i := range items {
			overfit := ComputeOverfitRisk(items[i].TotalReturn, items[i].SharpeRatio, items[i].MaxDrawdown, items[i].TotalTrades, 0.7)
			items[i].OverfitRisk = overfit
			items[i].KPIScore = CalculateKPIScore(&items[i], overfit)
		}
		sort.Slice(items, func(i, j int) bool {
			return items[i].KPIScore.TotalScore > items[j].KPIScore.TotalScore
		})
		if limit < len(items) {
			items = items[:limit]
		}
		return items, nil
	}
	items, _, err := s.GetMarketStrategies(0, 1, limit, "", sortBy)
	return items, err
}

// GetTrendingStrategies returns recently popular strategies.
func (s *Service) GetTrendingStrategies(limit int) ([]StrategyItem, error) {
	db := store.GetDB()
	if db == nil {
		return nil, nil
	}
	ensureStrategyTables()
	if limit < 1 || limit > 50 {
		limit = 10
	}
	rows, err := db.Query(`
		SELECT id, name, description, code, symbol, interval,
		 author_id, author_name,
		 total_return, sharpe_ratio, max_drawdown, win_rate, total_trades, profit_factor,
		 avg_rating, rating_count, comment_count, download_count, view_count,
		 created_at, updated_at
		 FROM strategy_marketplace
		 ORDER BY (download_count + comment_count + rating_count) DESC, updated_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []StrategyItem
	for rows.Next() {
		si := StrategyItem{}
		rows.Scan(
			&si.ID, &si.Name, &si.Description, &si.Code, &si.Symbol, &si.Interval,
			&si.AuthorID, &si.AuthorName,
			&si.TotalReturn, &si.SharpeRatio, &si.MaxDrawdown, &si.WinRate, &si.TotalTrades, &si.ProfitFactor,
			&si.AvgRating, &si.RatingCount, &si.CommentCount, &si.DownloadCount, &si.ViewCount,
			&si.CreatedAt, &si.UpdatedAt,
		)
		items = append(items, si)
	}
	if items == nil {
		items = []StrategyItem{}
	}
	return items, nil
}
