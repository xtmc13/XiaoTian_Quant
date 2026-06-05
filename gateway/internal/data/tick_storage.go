package data

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Tick Storage ─────────────────────────────────────────────────

// TickStorage provides local SQLite persistence for tick data.
type TickStorage struct {
	mu sync.RWMutex
}

// NewTickStorage creates a new tick storage instance.
func NewTickStorage() *TickStorage {
	s := &TickStorage{}
	s.ensureTables()
	return s
}

func (ts *TickStorage) db() *sql.DB {
	return store.GetDB()
}

func (ts *TickStorage) ensureTables() {
	db := ts.db()
	if db == nil {
		return
	}

	db.Exec(`CREATE TABLE IF NOT EXISTS ticks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT NOT NULL,
		bid REAL NOT NULL,
		ask REAL NOT NULL,
		last REAL NOT NULL,
		volume REAL NOT NULL,
		timestamp INTEGER NOT NULL,
		UNIQUE(symbol, timestamp)
	)`)

	db.Exec(`CREATE INDEX IF NOT EXISTS idx_ticks_symbol_timestamp ON ticks(symbol, timestamp)`)
}

// SaveTicks saves a batch of ticks (upsert by symbol+timestamp).
func (ts *TickStorage) SaveTicks(symbol string, ticks []model.Tick) (int, error) {
	db := ts.db()
	if db == nil {
		return 0, fmt.Errorf("database not available")
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	saved := 0
	for _, tick := range ticks {
		_, err := db.Exec(
			`INSERT OR REPLACE INTO ticks (symbol, bid, ask, last, volume, timestamp)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			symbol, tick.Bid, tick.Ask, tick.Last, tick.Volume, tick.Timestamp,
		)
		if err != nil {
			log.Printf("[tick_storage] save error: %v", err)
			continue
		}
		saved++
	}
	return saved, nil
}

// LoadTicks loads tick data for a symbol/time range.
func (ts *TickStorage) LoadTicks(symbol string, start, end int64) ([]model.Tick, error) {
	db := ts.db()
	if db == nil {
		return nil, fmt.Errorf("database not available")
	}

	ts.mu.RLock()
	defer ts.mu.RUnlock()

	query := "SELECT symbol, bid, ask, last, volume, timestamp FROM ticks WHERE symbol = ?"
	args := []any{symbol}

	if start > 0 {
		query += " AND timestamp >= ?"
		args = append(args, start)
	}
	if end > 0 {
		query += " AND timestamp <= ?"
		args = append(args, end)
	}
	query += " ORDER BY timestamp ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ticks []model.Tick
	for rows.Next() {
		var tick model.Tick
		if err := rows.Scan(&tick.Symbol, &tick.Bid, &tick.Ask, &tick.Last, &tick.Volume, &tick.Timestamp); err != nil {
			continue
		}
		ticks = append(ticks, tick)
	}
	return ticks, nil
}

// GetTickCount returns the number of stored ticks for a symbol.
func (ts *TickStorage) GetTickCount(symbol string) int {
	db := ts.db()
	if db == nil {
		return 0
	}

	var count int
	query := "SELECT COUNT(*) FROM ticks WHERE symbol = ?"
	db.QueryRow(query, symbol).Scan(&count)
	return count
}

// DeleteOldTicks removes ticks older than the given timestamp for a symbol.
func (ts *TickStorage) DeleteOldTicks(symbol string, before int64) (int, error) {
	db := ts.db()
	if db == nil {
		return 0, fmt.Errorf("database not available")
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	result, err := db.Exec("DELETE FROM ticks WHERE symbol = ? AND timestamp < ?", symbol, before)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// TickDataInfo holds metadata about stored tick data.
type TickDataInfo struct {
	Symbol       string    `json:"symbol"`
	Count        int       `json:"count"`
	Earliest     int64     `json:"earliest"`
	Latest       int64     `json:"latest"`
	EarliestTime time.Time `json:"earliest_time"`
	LatestTime   time.Time `json:"latest_time"`
}

// GetTickInfo returns metadata about stored tick data for a symbol.
func (ts *TickStorage) GetTickInfo(symbol string) TickDataInfo {
	db := ts.db()
	if db == nil {
		return TickDataInfo{Symbol: symbol}
	}

	var count int
	var earliest, latest int64
	db.QueryRow("SELECT COUNT(*) FROM ticks WHERE symbol = ?", symbol).Scan(&count)
	db.QueryRow("SELECT MIN(timestamp), MAX(timestamp) FROM ticks WHERE symbol = ?", symbol).Scan(&earliest, &latest)

	return TickDataInfo{
		Symbol:       symbol,
		Count:        count,
		Earliest:     earliest,
		Latest:       latest,
		EarliestTime: time.UnixMilli(earliest),
		LatestTime:   time.UnixMilli(latest),
	}
}
