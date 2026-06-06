package data

import (
	"fmt"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── Types ──────────────────────────────────────────────────────

// OHLCV represents a single candlestick.
type OHLCV struct {
	Symbol   string  `json:"symbol"`
	Interval string  `json:"interval"`
	Time     int64   `json:"time"`     // unix milliseconds
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Close    float64 `json:"close"`
	Volume   float64 `json:"volume"`
}

// DownloadJob tracks the progress of a download operation.
type DownloadJob struct {
	ID        string `json:"id"`
	Symbol    string `json:"symbol"`
	Interval  string `json:"interval"`
	Status    string `json:"status"` // pending, running, done, failed
	Progress  int    `json:"progress"` // bars downloaded
	Total     int    `json:"total"`    // estimated total bars
	Error     string `json:"error,omitempty"`
	StartedAt int64  `json:"started_at"`
	EndedAt   int64  `json:"ended_at,omitempty"`
}

// DownloadConfig configures download behavior.
type DownloadConfig struct {
	Symbols    []string `json:"symbols"`
	Intervals  []string `json:"intervals"`
	StartDate  string   `json:"start_date"`  // "2024-01-01"
	EndDate    string   `json:"end_date"`     // "2024-12-31"
	MaxWorkers int      `json:"max_workers"`  // concurrent downloads
}

// ── Downloader ─────────────────────────────────────────────────

// Downloader manages historical OHLCV data from local storage only.
// Network download functionality has been removed.
type Downloader struct {
	store *Storage
	jobs  map[string]*DownloadJob
	mu    sync.RWMutex
}

// NewDownloader creates a new data downloader using local storage only.
func NewDownloader(store *Storage) *Downloader {
	return &Downloader{
		store: store,
		jobs:  make(map[string]*DownloadJob),
	}
}

// StartDownload is disabled. Returns an error indicating network download is not available.
func (d *Downloader) StartDownload(cfg DownloadConfig) (string, error) {
	return "", fmt.Errorf("network download is disabled — please import data manually via the data import API or database seeding")
}

// runDownload is disabled.
func (d *Downloader) runDownload(jobID string, cfg DownloadConfig) {
	d.mu.Lock()
	job := d.jobs[jobID]
	if job != nil {
		job.Status = "failed"
		job.Error = "network download is disabled"
		job.EndedAt = time.Now().UnixMilli()
	}
	d.mu.Unlock()
}

// FetchOHLCV is disabled. Use LoadBarsForBacktest to read from local storage.
func (d *Downloader) FetchOHLCV(symbol, interval string, fromMs, toMs int64) ([]OHLCV, error) {
	return nil, fmt.Errorf("FetchOHLCV is disabled — data must be loaded from local storage via LoadBarsForBacktest")
}

// IncrementalUpdate is disabled.
func (d *Downloader) IncrementalUpdate(symbol, interval string, fromMs, toMs int64) (*DownloadJob, error) {
	return nil, fmt.Errorf("incremental update is disabled — network download functionality has been removed")
}

// GetStorage returns the underlying storage for direct access.
func (d *Downloader) GetStorage() *Storage {
	return d.store
}

// GetJob returns a download job by ID.
func (d *Downloader) GetJob(id string) *DownloadJob {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.jobs[id]
}

// ToBar converts OHLCV to model.Bar for backtesting.
func (o OHLCV) ToBar() model.Bar {
	return model.Bar{
		Symbol: o.Symbol,
		Open:   o.Open,
		High:   o.High,
		Low:    o.Low,
		Close:  o.Close,
		Volume: o.Volume,
		Time:   o.Time,
	}
}

// LoadBarsForBacktest loads historical bars from local storage for backtesting.
func (d *Downloader) LoadBarsForBacktest(symbol, interval string, fromMs, toMs int64) []model.Bar {
	ohlcv := d.store.LoadOHLCV(symbol, interval, fromMs, toMs)
	bars := make([]model.Bar, len(ohlcv))
	for i, o := range ohlcv {
		bars[i] = o.ToBar()
	}
	return bars
}

// GetDataInfo returns metadata about stored data for a symbol/interval.
func (d *Downloader) GetDataInfo(symbol, interval string) DataInfo {
	count := d.store.CountOHLCV(symbol, interval)
	coverage := d.store.GetCoverage()
	for _, c := range coverage {
		if c.Symbol == symbol && c.Interval == interval {
			return DataInfo{
				Symbol:       symbol,
				Interval:     interval,
				Count:        count,
				Earliest:     c.StartTime,
				Latest:       c.EndTime,
				EarliestTime: time.UnixMilli(c.StartTime),
				LatestTime:   time.UnixMilli(c.EndTime),
			}
		}
	}
	return DataInfo{Symbol: symbol, Interval: interval, Count: count}
}

// SaveBars saves bars to local storage.
func (d *Downloader) SaveBars(bars []model.Bar) error {
	if len(bars) == 0 {
		return nil
	}
	ohlcv := make([]OHLCV, len(bars))
	for i, b := range bars {
		ohlcv[i] = OHLCV{
			Symbol:   b.Symbol,
			Interval: b.Interval,
			Time:     b.Time,
			Open:     b.Open,
			High:     b.High,
			Low:      b.Low,
			Close:    b.Close,
			Volume:   b.Volume,
		}
	}
	_, err := d.store.SaveOHLCV(ohlcv)
	return err
}

// DataInfo holds metadata about stored historical data.
type DataInfo struct {
	Symbol       string    `json:"symbol"`
	Interval     string    `json:"interval"`
	Count        int       `json:"count"`
	Earliest     int64     `json:"earliest"`
	Latest       int64     `json:"latest"`
	EarliestTime time.Time `json:"earliest_time"`
	LatestTime   time.Time `json:"latest_time"`
}

// Parse helpers
func parseFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	}
	return 0
}
