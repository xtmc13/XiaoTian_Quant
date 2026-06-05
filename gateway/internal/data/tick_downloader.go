package data

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── Types ────────────────────────────────────────────────────────

// TickDownloadJob tracks the progress of a tick download operation.
type TickDownloadJob struct {
	ID        string `json:"id"`
	Symbol    string `json:"symbol"`
	Status    string `json:"status"` // pending, running, done, failed
	Progress  int    `json:"progress"` // ticks downloaded
	Total     int    `json:"total"`    // estimated total ticks
	Error     string `json:"error,omitempty"`
	StartedAt int64  `json:"started_at"`
	EndedAt   int64  `json:"ended_at,omitempty"`
}

// TickDownloader fetches historical tick (aggregate trade) data from exchanges.
type TickDownloader struct {
	store      *TickStorage
	httpClient *http.Client
	jobs       map[string]*TickDownloadJob
	mu         sync.RWMutex
}

// NewTickDownloader creates a new tick data downloader.
func NewTickDownloader(store *TickStorage) *TickDownloader {
	return &TickDownloader{
		store:      store,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		jobs:       make(map[string]*TickDownloadJob),
	}
}

// StartDownload initiates a tick download job. Returns the job ID.
func (td *TickDownloader) StartDownload(symbol string, startTime, endTime int64) (string, error) {
	if symbol == "" {
		return "", fmt.Errorf("no symbol specified")
	}

	jobID := fmt.Sprintf("tick_dl_%d", time.Now().UnixMilli())
	job := &TickDownloadJob{
		ID:        jobID,
		Symbol:    symbol,
		Status:    "pending",
		StartedAt: time.Now().UnixMilli(),
	}

	td.mu.Lock()
	td.jobs[jobID] = job
	td.mu.Unlock()

	// Run async
	go td.runDownload(jobID, symbol, startTime, endTime)
	return jobID, nil
}

func (td *TickDownloader) runDownload(jobID, symbol string, startTime, endTime int64) {
	td.mu.Lock()
	job := td.jobs[jobID]
	job.Status = "running"
	td.mu.Unlock()

	ticks, err := td.DownloadTicks(symbol, startTime, endTime)

	td.mu.Lock()
	job.EndedAt = time.Now().UnixMilli()
	if err != nil {
		job.Status = "failed"
		job.Error = err.Error()
	} else {
		job.Status = "done"
		job.Progress = len(ticks)
		job.Total = len(ticks)
		if td.store != nil && len(ticks) > 0 {
			if _, saveErr := td.store.SaveTicks(symbol, ticks); saveErr != nil {
				job.Error = saveErr.Error()
			}
		}
	}
	td.mu.Unlock()

	log.Printf("[tick_data] download %s complete: %d ticks, status=%s", jobID, len(ticks), job.Status)
}

// DownloadTicks fetches aggregate trades from Binance and converts them to model.Tick.
func (td *TickDownloader) DownloadTicks(symbol string, startTime, endTime int64) ([]model.Tick, error) {
	var allTicks []model.Tick
	var fromID int64 = 0

	for {
		url := fmt.Sprintf(
			"https://api.binance.com/api/v3/aggTrades?symbol=%s&limit=1000",
			symbol,
		)
		if fromID > 0 {
			url += fmt.Sprintf("&fromId=%d", fromID)
		} else if startTime > 0 {
			url += fmt.Sprintf("&startTime=%d", startTime)
		}
		if endTime > 0 {
			url += fmt.Sprintf("&endTime=%d", endTime)
		}

		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Accept", "application/json")

		resp, err := td.httpClient.Do(req)
		if err != nil {
			return allTicks, fmt.Errorf("binance request failed: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return allTicks, err
		}

		var raw []struct {
			A int64  `json:"a"` // Aggregate tradeId
			P string `json:"p"` // Price
			Q string `json:"q"` // Quantity
			F int64  `json:"f"` // First tradeId
			L int64  `json:"l"` // Last tradeId
			T int64  `json:"T"` // Timestamp
			M bool   `json:"m"` // Was the buyer the maker?
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			return allTicks, fmt.Errorf("binance parse error: %w (body: %.200s)", err, string(body))
		}

		if len(raw) == 0 {
			break
		}

		// Parse ticks
		batch := make([]model.Tick, 0, len(raw))
		for _, t := range raw {
			price := parseFloat(t.P)
			qty := parseFloat(t.Q)
			batch = append(batch, model.Tick{
				Symbol:    symbol,
				Bid:       price,
				Ask:       price,
				Last:      price,
				Volume:    qty,
				Timestamp: t.T,
			})
		}

		allTicks = append(allTicks, batch...)

		// If we got fewer than 1000, we're at the end
		if len(raw) < 1000 {
			break
		}

		// Advance pagination using last aggregate trade ID + 1
		fromID = raw[len(raw)-1].A + 1

		// Check if we've exceeded endTime
		if endTime > 0 && raw[len(raw)-1].T >= endTime {
			break
		}

		// Rate limit: 50ms between requests = max 1200 req/min
		time.Sleep(50 * time.Millisecond)
	}

	return allTicks, nil
}

// GetJob returns a tick download job by ID.
func (td *TickDownloader) GetJob(id string) *TickDownloadJob {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return td.jobs[id]
}
