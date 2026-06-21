package data

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
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
	Exchange  string `json:"exchange"`
	Status    string `json:"status"`   // pending, running, done, failed
	Progress  int    `json:"progress"` // bars downloaded
	Total     int    `json:"total"`    // estimated total bars
	Error     string `json:"error,omitempty"`
	StartedAt int64  `json:"started_at"`
	EndedAt   int64  `json:"ended_at,omitempty"`
}

// DownloadConfig configures download behavior.
type DownloadConfig struct {
	Exchange   string   `json:"exchange"`
	Symbols    []string `json:"symbols"`
	Intervals  []string `json:"intervals"`
	StartDate  string   `json:"start_date"`  // "2024-01-01"
	EndDate    string   `json:"end_date"`     // "2024-12-31"
	MaxWorkers int      `json:"max_workers"`  // concurrent downloads
}

// ── Downloader ─────────────────────────────────────────────────

// Downloader manages historical OHLCV data from exchanges and local storage.
type Downloader struct {
	store *Storage
	jobs  map[string]*DownloadJob
	mu    sync.RWMutex
}

// NewDownloader creates a new data downloader.
func NewDownloader(store *Storage) *Downloader {
	return &Downloader{
		store: store,
		jobs:  make(map[string]*DownloadJob),
	}
}

// StartDownload starts an asynchronous download job for the configured symbols/intervals.
func (d *Downloader) StartDownload(cfg DownloadConfig) (string, error) {
	if cfg.Exchange == "" {
		cfg.Exchange = "binance"
	}
	if len(cfg.Symbols) == 0 {
		return "", fmt.Errorf("at least one symbol is required")
	}
	if len(cfg.Intervals) == 0 {
		cfg.Intervals = []string{"1h"}
	}
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 4
	}

	jobID := uuid.New().String()
	job := &DownloadJob{
		ID:        jobID,
		Exchange:  cfg.Exchange,
		Status:    "pending",
		StartedAt: time.Now().UnixMilli(),
	}
	d.mu.Lock()
	d.jobs[jobID] = job
	d.mu.Unlock()

	go d.runDownload(jobID, cfg)
	return jobID, nil
}

// runDownload performs paginated kline download for all symbol/interval pairs.
func (d *Downloader) runDownload(jobID string, cfg DownloadConfig) {
	d.mu.Lock()
	job := d.jobs[jobID]
	if job == nil {
		d.mu.Unlock()
		return
	}
	job.Status = "running"
	d.mu.Unlock()

	startMs := parseDate(cfg.StartDate)
	endMs := parseDate(cfg.EndDate)
	if endMs == 0 {
		endMs = time.Now().UnixMilli()
	}
	// Include the full end date
	endMs += 24 * 60 * 60 * 1000

	client, err := clientFactory(cfg.Exchange)
	if err != nil {
		d.failJob(jobID, err.Error())
		return
	}

	type task struct {
		symbol, interval string
	}
	var tasks []task
	for _, s := range cfg.Symbols {
		for _, i := range cfg.Intervals {
			tasks = append(tasks, task{symbol: s, interval: i})
		}
	}

	log.Printf("[data] download job %s started: exchange=%s tasks=%d range=%d..%d", jobID, cfg.Exchange, len(tasks), startMs, endMs)

	sem := make(chan struct{}, cfg.MaxWorkers)
	var wg sync.WaitGroup
	errors := make([]string, 0)
	errMu := sync.Mutex{}

	d.mu.Lock()
	job = d.jobs[jobID]
	if job != nil {
		job.Total = len(tasks)
	}
	d.mu.Unlock()

	for _, t := range tasks {
		wg.Add(1)
		sem <- struct{}{}
		go func(symbol, interval string) {
			defer wg.Done()
			defer func() { <-sem }()
			d.mu.Lock()
			job = d.jobs[jobID]
			if job != nil {
				job.Symbol = symbol
				job.Interval = interval
			}
			d.mu.Unlock()
			if err := d.downloadRange(client, symbol, interval, startMs, endMs); err != nil {
				errMu.Lock()
				errors = append(errors, fmt.Sprintf("%s/%s: %v", symbol, interval, err))
				errMu.Unlock()
			} else {
				d.mu.Lock()
				job = d.jobs[jobID]
				if job != nil {
					job.Progress++
				}
				d.mu.Unlock()
			}
		}(t.symbol, t.interval)
	}
	wg.Wait()

	if len(errors) > 0 {
		d.failJob(jobID, fmt.Sprintf("%d/%d tasks failed: %v", len(errors), len(tasks), errors))
		return
	}
	d.mu.Lock()
	job = d.jobs[jobID]
	if job != nil {
		job.Status = "done"
		job.Progress = job.Total
		job.EndedAt = time.Now().UnixMilli()
	}
	d.mu.Unlock()
}

// downloadRange downloads all klines for a symbol/interval in the given time range using pagination.
func (d *Downloader) downloadRange(client KlineClient, symbol, interval string, startMs, endMs int64) error {
	log.Printf("[data] downloading %s/%s from %d to %d", symbol, interval, startMs, endMs)
	limit := 1000
	stepMs := int64(limit) * intervalToMs(interval)
	if stepMs <= 0 {
		stepMs = 60_000 * 1000
	}

	for from := startMs; from < endMs; {
		to := from + stepMs
		if to > endMs {
			to = endMs
		}
		bars, err := client.GetKlinesRange(symbol, interval, from, to, limit)
		if err != nil {
			log.Printf("[data] download error %s/%s: %v", symbol, interval, err)
			return err
		}
		log.Printf("[data] fetched %s/%s %d bars [%d..%d]", symbol, interval, len(bars), from, to)
		if len(bars) == 0 {
			// No more data available; move forward to avoid infinite loop
			from = to
			continue
		}
		if _, err := d.store.SaveOHLCV(bars); err != nil {
			log.Printf("[data] save error %s/%s: %v", symbol, interval, err)
			return err
		}
		// Advance to just after the last returned bar
		last := bars[len(bars)-1].Time
		if last <= from {
			from = to
		} else {
			from = last + intervalToMs(interval)
		}
	}
	log.Printf("[data] completed %s/%s", symbol, interval)
	return nil
}

// failJob marks a job as failed with the given error message.
func (d *Downloader) failJob(jobID, msg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	job := d.jobs[jobID]
	if job == nil {
		return
	}
	job.Status = "failed"
	job.Error = msg
	job.EndedAt = time.Now().UnixMilli()
}

// FetchOHLCV fetches OHLCV data from the exchange for the given range and saves it to storage.
func (d *Downloader) FetchOHLCV(exchange, symbol, interval string, fromMs, toMs int64) ([]OHLCV, error) {
	client, err := clientFactory(exchange)
	if err != nil {
		return nil, err
	}
	bars, err := client.GetKlinesRange(symbol, interval, fromMs, toMs, 1000)
	if err != nil {
		return nil, err
	}
	if len(bars) > 0 {
		if _, err := d.store.SaveOHLCV(bars); err != nil {
			return bars, err
		}
	}
	return bars, nil
}

// IncrementalUpdate downloads new klines since the latest stored bar for a symbol/interval.
func (d *Downloader) IncrementalUpdate(exchange, symbol, interval string) (*DownloadJob, error) {
	info := d.GetDataInfo(symbol, interval)
	fromMs := info.Latest
	if fromMs == 0 {
		// No existing data; require explicit start date
		return nil, fmt.Errorf("no existing data for %s/%s; use StartDownload with a start_date", symbol, interval)
	}
	toMs := time.Now().UnixMilli()
	cfg := DownloadConfig{
		Exchange:   exchange,
		Symbols:    []string{symbol},
		Intervals:  []string{interval},
		MaxWorkers: 1,
	}
	jobID, err := d.StartDownload(cfg)
	if err != nil {
		return nil, err
	}
	// Override time range for this job by directly running download
	go func(id string) {
		client, err := clientFactory(exchange)
		if err != nil {
			d.failJob(id, err.Error())
			return
		}
		d.mu.Lock()
		job := d.jobs[id]
		if job != nil {
			job.Status = "running"
		}
		d.mu.Unlock()
		if err := d.downloadRange(client, symbol, interval, fromMs, toMs); err != nil {
			d.failJob(id, err.Error())
			return
		}
		d.mu.Lock()
		job = d.jobs[id]
		if job != nil {
			job.Status = "done"
			job.EndedAt = time.Now().UnixMilli()
		}
		d.mu.Unlock()
	}(jobID)
	return d.GetJob(jobID), nil
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

// ListJobs returns all download jobs.
func (d *Downloader) ListJobs() []*DownloadJob {
	d.mu.RLock()
	defer d.mu.RUnlock()
	jobs := make([]*DownloadJob, 0, len(d.jobs))
	for _, j := range d.jobs {
		jobs = append(jobs, j)
	}
	return jobs
}

// ToBar converts OHLCV to model.Bar for backtesting.
func (o OHLCV) ToBar() model.Bar {
	return model.Bar{
		Symbol:   o.Symbol,
		Interval: o.Interval,
		Open:     o.Open,
		High:     o.High,
		Low:      o.Low,
		Close:    o.Close,
		Volume:   o.Volume,
		Time:     o.Time,
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
