package store

import (
	"sync"
	"time"
)

// ── 组合回测 Repository ──
// 表结构见 migrations/sql/0013_portfolio_backtests.sql。

type PortfolioBacktestRecord struct {
	ID             string  `json:"id"`
	UserID         int64   `json:"user_id"`
	Name           string  `json:"name"`
	Timeframe      string  `json:"timeframe"`
	Rebalance      string  `json:"rebalance"`
	StartTime      int64   `json:"start_time"`
	EndTime        int64   `json:"end_time"`
	InitialCapital float64 `json:"initial_capital"`
	FinalEquity    float64 `json:"final_equity"`
	TotalReturnPct float64 `json:"total_return_pct"`
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
	SharpeRatio    float64 `json:"sharpe_ratio"`
	SortinoRatio   float64 `json:"sortino_ratio"`
	CalmarRatio    float64 `json:"calmar_ratio"`
	WinRate        float64 `json:"win_rate"`
	ProfitFactor   float64 `json:"profit_factor"`
	TotalTrades    int     `json:"total_trades"`
	LegsJSON       string  `json:"legs_json"`
	ResultJSON     string  `json:"result_json"`
	DurationMs     int64   `json:"duration_ms"`
	CreatedAt      int64   `json:"created_at"`
}

type PortfolioBacktestRepo struct{ mu sync.RWMutex }

func NewPortfolioBacktestRepo() *PortfolioBacktestRepo { return &PortfolioBacktestRepo{} }

func (r *PortfolioBacktestRepo) Create(rec *PortfolioBacktestRecord) error {
	if rec.CreatedAt == 0 {
		rec.CreatedAt = time.Now().UnixMilli()
	}
	_, err := db.Exec(
		`INSERT INTO xt_portfolio_backtests
			(id, user_id, name, timeframe, rebalance, start_time, end_time, initial_capital,
			 final_equity, total_return_pct, max_drawdown_pct, sharpe_ratio, sortino_ratio,
			 calmar_ratio, win_rate, profit_factor, total_trades, legs_json, result_json, duration_ms, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.UserID, rec.Name, rec.Timeframe, rec.Rebalance, rec.StartTime, rec.EndTime,
		rec.InitialCapital, rec.FinalEquity, rec.TotalReturnPct, rec.MaxDrawdownPct, rec.SharpeRatio,
		rec.SortinoRatio, rec.CalmarRatio, rec.WinRate, rec.ProfitFactor, rec.TotalTrades,
		rec.LegsJSON, rec.ResultJSON, rec.DurationMs, rec.CreatedAt,
	)
	return err
}

const portfolioBTColumns = `id, user_id, name, timeframe, rebalance, start_time, end_time, initial_capital,
	final_equity, total_return_pct, max_drawdown_pct, sharpe_ratio, sortino_ratio,
	calmar_ratio, win_rate, profit_factor, total_trades, legs_json, result_json, duration_ms, created_at`

func (r *PortfolioBacktestRepo) scan(s rowScanner) (*PortfolioBacktestRecord, error) {
	var rec PortfolioBacktestRecord
	err := s.Scan(&rec.ID, &rec.UserID, &rec.Name, &rec.Timeframe, &rec.Rebalance,
		&rec.StartTime, &rec.EndTime, &rec.InitialCapital, &rec.FinalEquity,
		&rec.TotalReturnPct, &rec.MaxDrawdownPct, &rec.SharpeRatio, &rec.SortinoRatio,
		&rec.CalmarRatio, &rec.WinRate, &rec.ProfitFactor, &rec.TotalTrades,
		&rec.LegsJSON, &rec.ResultJSON, &rec.DurationMs, &rec.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// ListByUser 返回该用户的最近组合回测，limit<=0 时默认 50。
func (r *PortfolioBacktestRepo) ListByUser(userID int64, limit int) ([]*PortfolioBacktestRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(
		`SELECT `+portfolioBTColumns+` FROM xt_portfolio_backtests WHERE user_id = ? ORDER BY created_at DESC LIMIT ?`,
		userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PortfolioBacktestRecord
	for rows.Next() {
		rec, err := r.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// GetByID 取单条（属主校验由 handler 做）。
func (r *PortfolioBacktestRepo) GetByID(id string) (*PortfolioBacktestRecord, error) {
	row := db.QueryRow(`SELECT `+portfolioBTColumns+` FROM xt_portfolio_backtests WHERE id = ?`, id)
	return r.scan(row)
}

// DeleteByID 删除单条。
func (r *PortfolioBacktestRepo) DeleteByID(id string) error {
	_, err := db.Exec(`DELETE FROM xt_portfolio_backtests WHERE id = ?`, id)
	return err
}
