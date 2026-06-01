package data

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Storage ────────────────────────────────────────────────────

// Storage provides local SQLite persistence for OHLCV data.
type Storage struct {
	mu sync.RWMutex
}

// NewStorage creates a new data storage instance.
func NewStorage() *Storage {
	s := &Storage{}
	s.ensureTables()
	return s
}

func (s *Storage) db() *sql.DB {
	return store.GetDB()
}

func (s *Storage) ensureTables() {
	db := s.db()
	if db == nil {
		return
	}

	db.Exec(`CREATE TABLE IF NOT EXISTS ohlcv_data (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT NOT NULL,
		interval TEXT NOT NULL,
		time INTEGER NOT NULL,
		open REAL NOT NULL,
		high REAL NOT NULL,
		low REAL NOT NULL,
		close REAL NOT NULL,
		volume REAL NOT NULL,
		UNIQUE(symbol, interval, time)
	)`)

	db.Exec(`CREATE INDEX IF NOT EXISTS idx_ohlcv_symbol_interval_time ON ohlcv_data(symbol, interval, time)`)

	db.Exec(`CREATE TABLE IF NOT EXISTS ohlcv_download_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT NOT NULL,
		interval TEXT NOT NULL,
		bars_count INTEGER NOT NULL,
		start_time INTEGER NOT NULL,
		end_time INTEGER NOT NULL,
		downloaded_at INTEGER NOT NULL
	)`)
}

// SaveOHLCV saves a batch of OHLCV bars (upsert by symbol+interval+time).
func (s *Storage) SaveOHLCV(bars []OHLCV) (int, error) {
	db := s.db()
	if db == nil {
		return 0, fmt.Errorf("database not available")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	saved := 0
	for _, bar := range bars {
		_, err := db.Exec(
			`INSERT OR REPLACE INTO ohlcv_data (symbol, interval, time, open, high, low, close, volume)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			bar.Symbol, bar.Interval, bar.Time, bar.Open, bar.High, bar.Low, bar.Close, bar.Volume,
		)
		if err != nil {
			log.Printf("[data] save error: %v", err)
			continue
		}
		saved++
	}
	return saved, nil
}

// LoadOHLCV loads OHLCV data for a symbol/interval/time range.
func (s *Storage) LoadOHLCV(symbol, interval string, fromMs, toMs int64) []OHLCV {
	db := s.db()
	if db == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT symbol, interval, time, open, high, low, close, volume FROM ohlcv_data WHERE symbol = ? AND interval = ?"
	args := []any{symbol, interval}

	if fromMs > 0 {
		query += " AND time >= ?"
		args = append(args, fromMs)
	}
	if toMs > 0 {
		query += " AND time <= ?"
		args = append(args, toMs)
	}
	query += " ORDER BY time ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var bars []OHLCV
	for rows.Next() {
		var bar OHLCV
		rows.Scan(&bar.Symbol, &bar.Interval, &bar.Time, &bar.Open, &bar.High, &bar.Low, &bar.Close, &bar.Volume)
		bars = append(bars, bar)
	}
	return bars
}

// CountOHLCV returns the number of stored bars for a symbol/interval.
func (s *Storage) CountOHLCV(symbol, interval string) int {
	db := s.db()
	if db == nil {
		return 0
	}

	var count int
	query := "SELECT COUNT(*) FROM ohlcv_data WHERE symbol = ? AND interval = ?"
	db.QueryRow(query, symbol, interval).Scan(&count)
	return count
}

// GetCoverage returns the time range covered for each symbol/interval.
func (s *Storage) GetCoverage() []CoverageInfo {
	db := s.db()
	if db == nil {
		return nil
	}

	rows, err := db.Query(
		`SELECT symbol, interval, COUNT(*) as cnt, MIN(time) as min_t, MAX(time) as max_t
		 FROM ohlcv_data GROUP BY symbol, interval ORDER BY symbol, interval`,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []CoverageInfo
	for rows.Next() {
		var ci CoverageInfo
		rows.Scan(&ci.Symbol, &ci.Interval, &ci.BarCount, &ci.StartTime, &ci.EndTime)
		result = append(result, ci)
	}
	return result
}

// CoverageInfo describes the data coverage for a symbol/interval.
type CoverageInfo struct {
	Symbol    string `json:"symbol"`
	Interval  string `json:"interval"`
	BarCount  int    `json:"bar_count"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
}

// Prune removes data older than the given timestamp.
func (s *Storage) Prune(beforeMs int64) (int, error) {
	db := s.db()
	if db == nil {
		return 0, fmt.Errorf("database not available")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := db.Exec("DELETE FROM ohlcv_data WHERE time < ?", beforeMs)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// LogDownload records a download operation.
func (s *Storage) LogDownload(symbol, interval string, count int, startMs, endMs int64) {
	db := s.db()
	if db == nil {
		return
	}
	db.Exec(
		`INSERT INTO ohlcv_download_log (symbol, interval, bars_count, start_time, end_time, downloaded_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		symbol, interval, count, startMs, endMs, time.Now().Unix(),
	)
}

// GetAvailableSymbols returns all symbols that have data stored.
func (s *Storage) GetAvailableSymbols() []string {
	db := s.db()
	if db == nil {
		return nil
	}

	rows, err := db.Query("SELECT DISTINCT symbol FROM ohlcv_data ORDER BY symbol")
	if err != nil {
		return nil
	}
	defer rows.Close()

	var symbols []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		symbols = append(symbols, s)
	}
	return symbols
}

// GetAvailableIntervals returns all intervals available for a symbol.
func (s *Storage) GetAvailableIntervals(symbol string) []string {
	db := s.db()
	if db == nil {
		return nil
	}

	rows, err := db.Query("SELECT DISTINCT interval FROM ohlcv_data WHERE symbol = ? ORDER BY interval", symbol)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var intervals []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		intervals = append(intervals, s)
	}
	return intervals
}
