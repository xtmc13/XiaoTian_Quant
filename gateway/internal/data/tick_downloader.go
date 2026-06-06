package data

import (
	"fmt"
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

// TickDownloader manages tick data from local storage only.
// Network download functionality has been removed.
type TickDownloader struct {
	store *TickStorage
	jobs  map[string]*TickDownloadJob
	mu    sync.RWMutex
}

// NewTickDownloader creates a new tick data downloader using local storage only.
func NewTickDownloader(store *TickStorage) *TickDownloader {
	return &TickDownloader{
		store: store,
		jobs:  make(map[string]*TickDownloadJob),
	}
}

// StartDownload is disabled. Returns an error indicating network download is not available.
func (td *TickDownloader) StartDownload(symbol string, startTime, endTime int64) (string, error) {
	return "", fmt.Errorf("network tick download is disabled — please import tick data manually via the data import API or database seeding")
}

// runDownload is disabled.
func (td *TickDownloader) runDownload(jobID, symbol string, startTime, endTime int64) {
	td.mu.Lock()
	job := td.jobs[jobID]
	if job != nil {
		job.Status = "failed"
		job.Error = "network download is disabled"
		job.EndedAt = time.Now().UnixMilli()
	}
	td.mu.Unlock()
}

// DownloadTicks is disabled.
func (td *TickDownloader) DownloadTicks(symbol string, startTime, endTime int64) ([]model.Tick, error) {
	return nil, fmt.Errorf("DownloadTicks is disabled — network download functionality has been removed")
}

// GetJob returns a tick download job by ID.
func (td *TickDownloader) GetJob(id string) *TickDownloadJob {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return td.jobs[id]
}
