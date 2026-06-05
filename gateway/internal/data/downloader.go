package data

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
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

// Downloader fetches historical OHLCV data from exchanges.
type Downloader struct {
	store      *Storage
	httpClient *http.Client
	jobs       map[string]*DownloadJob
	mu         sync.RWMutex
}

// NewDownloader creates a new data downloader.
func NewDownloader(store *Storage) *Downloader {
	return &Downloader{
		store:      store,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		jobs:       make(map[string]*DownloadJob),
	}
}

// StartDownload initiates a download job. Returns the job ID.
func (d *Downloader) StartDownload(cfg DownloadConfig) (string, error) {
	if len(cfg.Symbols) == 0 {
		return "", fmt.Errorf("no symbols specified")
	}
	if len(cfg.Intervals) == 0 {
		cfg.Intervals = []string{"1h"}
	}
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 2
	}

	jobID := fmt.Sprintf("dl_%d", time.Now().UnixMilli())
	job := &DownloadJob{
		ID:        jobID,
		Status:    "pending",
		StartedAt: time.Now().UnixMilli(),
	}

	d.mu.Lock()
	d.jobs[jobID] = job
	d.mu.Unlock()

	// Run async
	go d.runDownload(jobID, cfg)
	return jobID, nil
}

func (d *Downloader) runDownload(jobID string, cfg DownloadConfig) {
	d.mu.Lock()
	job := d.jobs[jobID]
	job.Status = "running"
	d.mu.Unlock()

	totalTasks := len(cfg.Symbols) * len(cfg.Intervals)
	sem := make(chan struct{}, cfg.MaxWorkers)
	var wg sync.WaitGroup
	var errs []string
	var muErrs sync.Mutex
	totalBars := 0

	for _, sym := range cfg.Symbols {
		for _, interval := range cfg.Intervals {
			wg.Add(1)
			go func(symbol, interval string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				bars, err := d.downloadSymbol(symbol, interval, cfg.StartDate, cfg.EndDate)
				if err != nil {
					muErrs.Lock()
					errs = append(errs, fmt.Sprintf("%s/%s: %v", symbol, interval, err))
					muErrs.Unlock()
					return
				}

				d.store.SaveOHLCV(bars)

				d.mu.Lock()
				job.Progress += len(bars)
				totalBars += len(bars)
				d.mu.Unlock()

				log.Printf("[data] downloaded %s %s: %d bars", symbol, interval, len(bars))
			}(sym, interval)
		}
	}

	wg.Wait()

	d.mu.Lock()
	job.Total = totalTasks
	job.EndedAt = time.Now().UnixMilli()
	if len(errs) > 0 {
		job.Status = "failed"
		job.Error = strings.Join(errs, "; ")
	} else {
		job.Status = "done"
	}
	d.mu.Unlock()

	log.Printf("[data] download %s complete: %d bars, status=%s", jobID, totalBars, job.Status)
}

// FetchOHLCV downloads OHLCV bars directly from the exchange (bypassing local storage).
func (d *Downloader) FetchOHLCV(symbol, interval string, fromMs, toMs int64) ([]OHLCV, error) {
	return d.fetchFromBinance(symbol, interval, fromMs, toMs)
}

// GetStorage returns the underlying storage for direct access.
func (d *Downloader) GetStorage() *Storage {
	return d.store
}

func (d *Downloader) downloadSymbol(symbol, interval, startDate, endDate string) ([]OHLCV, error) {
	var startMs, endMs int64

	if startDate != "" {
		t, err := time.Parse("2006-01-02", startDate)
		if err != nil {
			return nil, fmt.Errorf("invalid start date: %w", err)
		}
		startMs = t.UnixMilli()
	}
	if endDate != "" {
		t, err := time.Parse("2006-01-02", endDate)
		if err != nil {
			return nil, fmt.Errorf("invalid end date: %w", err)
		}
		endMs = t.UnixMilli() + 86400000 // end of day
	} else {
		endMs = time.Now().UnixMilli()
	}

	// Check what we already have locally to avoid re-downloading
	var existing []OHLCV
	if startMs > 0 {
		existing = d.store.LoadOHLCV(symbol, interval, startMs, endMs)
		if len(existing) > 0 {
			// Resume from the last known timestamp
			lastTs := existing[len(existing)-1].Time
			if lastTs+1 > startMs {
				startMs = lastTs + 1 // resume after last bar
			}
		}
	}

	// Binance API max 1000 bars per request
	bars, err := d.fetchFromBinance(symbol, interval, startMs, endMs)
	if err != nil {
		return nil, err
	}

	// Merge with existing
	result := make([]OHLCV, 0, len(existing)+len(bars))
	result = append(result, existing...)
	result = append(result, bars...)

	return result, nil
}

func (d *Downloader) fetchFromBinance(symbol, interval string, fromMs, toMs int64) ([]OHLCV, error) {
	var allBars []OHLCV
	currentFrom := fromMs

	for {
		url := fmt.Sprintf(
			"https://api.binance.com/api/v3/klines?symbol=%s&interval=%s&limit=1000",
			symbol, interval,
		)
		if currentFrom > 0 {
			url += fmt.Sprintf("&startTime=%d", currentFrom)
		}
		if toMs > 0 {
			url += fmt.Sprintf("&endTime=%d", toMs)
		}

		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Accept", "application/json")

		resp, err := d.httpClient.Do(req)
		if err != nil {
			return allBars, fmt.Errorf("binance request failed: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return allBars, err
		}

		var raw [][]any
		if err := json.Unmarshal(body, &raw); err != nil {
			return allBars, fmt.Errorf("binance parse error: %w (body: %.200s)", err, string(body))
		}

		if len(raw) == 0 {
			break
		}

		// Parse bars
		batch := make([]OHLCV, 0, len(raw))
		for _, k := range raw {
			if len(k) < 6 {
				continue
			}
			batch = append(batch, OHLCV{
				Symbol:   symbol,
				Interval: interval,
				Time:     int64(k[0].(float64)),
				Open:     parseFloat(k[1]),
				High:     parseFloat(k[2]),
				Low:      parseFloat(k[3]),
				Close:    parseFloat(k[4]),
				Volume:   parseFloat(k[5]),
			})
		}

		allBars = append(allBars, batch...)

		// If we got fewer than 1000, we're at the end
		if len(raw) < 1000 {
			break
		}

		// Advance to next batch (avoid duplicating the last bar)
		lastTime := int64(raw[len(raw)-1][0].(float64))
		currentFrom = lastTime + 1

		// Rate limit: 1 request per 200ms (5 req/s, Binance allows 1200/min)
		time.Sleep(200 * time.Millisecond)
	}

	return allBars, nil
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

// LoadBarsForBacktest loads historical bars from storage for backtesting.
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
