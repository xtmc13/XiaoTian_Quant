package store

import (
	"sync"
)

// ── Market Data Repository ──

type MarketBarRecord struct {
	Symbol    string  `json:"symbol"`
	Interval  string  `json:"interval"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	Volume    float64 `json:"volume"`
	Timestamp int64   `json:"timestamp"`
}

type MarketDataRepo struct{ mu sync.RWMutex }

func NewMarketDataRepo() *MarketDataRepo { return &MarketDataRepo{} }

func (r *MarketDataRepo) Create(b *MarketBarRecord) error {
	_, err := db.Exec(
		`INSERT OR REPLACE INTO market_bars (symbol, interval, open, high, low, close, volume, timestamp) VALUES (?,?,?,?,?,?,?,?)`,
		b.Symbol, b.Interval, b.Open, b.High, b.Low, b.Close, b.Volume, b.Timestamp,
	)
	return err
}

func (r *MarketDataRepo) BatchCreate(bars []MarketBarRecord) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO market_bars (symbol, interval, open, high, low, close, volume, timestamp) VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, b := range bars {
		if _, err := stmt.Exec(b.Symbol, b.Interval, b.Open, b.High, b.Low, b.Close, b.Volume, b.Timestamp); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (r *MarketDataRepo) GetBars(symbol, interval string, startTime, endTime int64, limit int) ([]*MarketBarRecord, error) {
	query := "SELECT symbol, interval, open, high, low, close, volume, timestamp FROM market_bars WHERE symbol=? AND interval=?"
	args := []any{symbol, interval}
	if startTime > 0 {
		query += " AND timestamp >= ?"
		args = append(args, startTime)
	}
	if endTime > 0 {
		query += " AND timestamp <= ?"
		args = append(args, endTime)
	}
	query += " ORDER BY timestamp ASC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*MarketBarRecord
	for rows.Next() {
		var b MarketBarRecord
		if err := rows.Scan(&b.Symbol, &b.Interval, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume, &b.Timestamp); err != nil {
			return nil, err
		}
		result = append(result, &b)
	}
	return result, nil
}

func (r *MarketDataRepo) GetByID(id string) (*MarketBarRecord, error) {
	row := db.QueryRow(`SELECT symbol, interval, open, high, low, close, volume, timestamp FROM market_bars WHERE id=?`, id)
	var b MarketBarRecord
	err := row.Scan(&b.Symbol, &b.Interval, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume, &b.Timestamp)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *MarketDataRepo) List(filter map[string]any, limit int) ([]*MarketBarRecord, error) {
	return nil, nil
}
func (r *MarketDataRepo) Update(b *MarketBarRecord) error { return r.Create(b) }
func (r *MarketDataRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM market_bars WHERE id=?", id)
	return err
}
