package community

import (
	"database/sql"
	"fmt"
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
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		description TEXT DEFAULT '',
		code TEXT NOT NULL,
		symbol TEXT DEFAULT 'BTCUSDT',
		interval TEXT DEFAULT '1h',
		author_id INTEGER NOT NULL,
		author_name TEXT DEFAULT '',
		total_return REAL DEFAULT 0,
		sharpe_ratio REAL DEFAULT 0,
		max_drawdown REAL DEFAULT 0,
		win_rate REAL DEFAULT 0,
		total_trades INTEGER DEFAULT 0,
		profit_factor REAL DEFAULT 0,
		avg_rating REAL DEFAULT 0,
		rating_count INTEGER DEFAULT 0,
		comment_count INTEGER DEFAULT 0,
		download_count INTEGER DEFAULT 0,
		view_count INTEGER DEFAULT 0,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`)

	db.Exec(`CREATE TABLE IF NOT EXISTS strategy_comments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		strategy_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		user_name TEXT DEFAULT '',
		content TEXT NOT NULL,
		created_at INTEGER NOT NULL
	)`)

	db.Exec(`CREATE TABLE IF NOT EXISTS strategy_ratings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		strategy_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		rating INTEGER NOT NULL,
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

	result, err := db.Exec(
		`INSERT INTO strategy_marketplace (name, description, code, symbol, interval, author_id, author_name,
		 total_return, sharpe_ratio, max_drawdown, win_rate, total_trades, profit_factor,
		 created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		name, description, code, symbol, interval, userID, authorName,
		totalReturn, sharpe, maxDD, winRate, totalTrades, profitFactor,
		now, now,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
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
		wheres = append(wheres, "(name LIKE ? OR description LIKE ?)")
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

	querySQL := fmt.Sprintf(
		`SELECT id, name, description, code, symbol, interval,
		 author_id, author_name,
		 total_return, sharpe_ratio, max_drawdown, win_rate, total_trades, profit_factor,
		 avg_rating, rating_count, comment_count, download_count, view_count,
		 created_at, updated_at
		 FROM strategy_marketplace WHERE %s %s LIMIT ? OFFSET ?`, whereSQL, orderSQL)

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
		 FROM strategy_marketplace WHERE id = ?`, strategyID,
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
	db.Exec(`UPDATE strategy_marketplace SET view_count = view_count + 1 WHERE id = ?`, strategyID)

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
	result, err := db.Exec(
		`INSERT INTO strategy_comments (strategy_id, user_id, user_name, content, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		strategyID, userID, userName, content, now,
	)
	if err != nil {
		return nil, err
	}

	db.Exec(`UPDATE strategy_marketplace SET comment_count = comment_count + 1 WHERE id = ?`, strategyID)

	id, _ := result.LastInsertId()
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
		 FROM strategy_comments WHERE strategy_id = ? ORDER BY created_at ASC`,
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
		`INSERT INTO strategy_ratings (strategy_id, user_id, rating) VALUES (?, ?, ?)
		 ON CONFLICT(strategy_id, user_id) DO UPDATE SET rating = ?`,
		strategyID, userID, rating, rating,
	)

	// Recalculate average
	var avgRating float64
	var count int
	db.QueryRow(
		`SELECT COALESCE(AVG(rating), 0), COUNT(*) FROM strategy_ratings WHERE strategy_id = ?`,
		strategyID,
	).Scan(&avgRating, &count)

	db.Exec(
		`UPDATE strategy_marketplace SET avg_rating = ?, rating_count = ?, updated_at = ? WHERE id = ?`,
		avgRating, count, time.Now().Unix(), strategyID,
	)

	return nil
}

// GetStrategyLeaderboard returns top strategies by a metric.
func (s *Service) GetStrategyLeaderboard(sortBy string, limit int) ([]StrategyItem, error) {
	items, _, err := s.GetMarketStrategies(0, 1, limit, "", sortBy)
	return items, err
}
