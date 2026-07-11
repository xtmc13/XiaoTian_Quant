package store

import (
	"encoding/json"
	"sync"
	"time"
)

// ── Portfolio Snapshot Repository ──

type PortfolioSnapshotRepo struct{ mu sync.RWMutex }

func NewPortfolioSnapshotRepo() *PortfolioSnapshotRepo { return &PortfolioSnapshotRepo{} }

func (r *PortfolioSnapshotRepo) Save(totalEquity, availableBalance, marginUsed, drawdown, netExposure float64, positions, balances any) error {
	posJSON, _ := json.Marshal(positions)
	balJSON, _ := json.Marshal(balances)
	now := time.Now().UnixMilli()
	_, err := db.Exec(
		`INSERT INTO xt_portfolio_snapshots (total_equity, available_balance, margin_used, drawdown, net_exposure, positions_json, balances_json, timestamp)
		 VALUES (?,?,?,?,?,?,?,?)`,
		totalEquity, availableBalance, marginUsed, drawdown, netExposure, string(posJSON), string(balJSON), now,
	)
	return err
}

func (r *PortfolioSnapshotRepo) GetRecent(limit int) ([]map[string]any, error) {
	rows, err := db.Query(`SELECT total_equity, available_balance, margin_used, drawdown, net_exposure, timestamp FROM xt_portfolio_snapshots ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		var equity, avail, margin, dd, exposure float64
		var ts int64
		rows.Scan(&equity, &avail, &margin, &dd, &exposure, &ts)
		result = append(result, map[string]any{
			"total_equity": equity, "available_balance": avail, "margin_used": margin,
			"drawdown": dd, "net_exposure": exposure, "timestamp": ts,
		})
	}
	return result, nil
}
