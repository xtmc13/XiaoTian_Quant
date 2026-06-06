// Package ml provides TensorBoard metrics collection and querying.
package ml

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// ── TensorBoard Types ──────────────────────────────────────────

// TensorBoardScalar is a single scalar metric point.
type TensorBoardScalar struct {
	Tag       string  `json:"tag"`
	Step      int     `json:"step"`
	WallTime  float64 `json:"wall_time"`
	Value     float64 `json:"value"`
}

// TensorBoardRun represents a single TensorBoard run (experiment).
type TensorBoardRun struct {
	RunID       string            `json:"run_id"`
	RunName     string            `json:"run_name"`
	ModelType   string            `json:"model_type"` // rl, lightgbm, xgboost
	ModelID     string            `json:"model_id"`
	StartedAt   time.Time         `json:"started_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Status      string            `json:"status"` // running, completed, failed
	Tags        []string          `json:"tags"`
	Scalars     []TensorBoardScalar `json:"scalars,omitempty"`
}

// TensorBoardQueryRequest queries metrics for a run.
type TensorBoardQueryRequest struct {
	RunID   string   `json:"run_id"`
	Tags    []string `json:"tags,omitempty"`
	FromStep int     `json:"from_step,omitempty"`
	ToStep   int     `json:"to_step,omitempty"`
}

// TensorBoardQueryResult returns queried metrics.
type TensorBoardQueryResult struct {
	RunID    string                       `json:"run_id"`
	Scalars  map[string][]TensorBoardScalar `json:"scalars"`
}

// TensorBoardSummary provides a summary of all runs.
type TensorBoardSummary struct {
	Runs      []TensorBoardRun `json:"runs"`
	TotalRuns int              `json:"total_runs"`
}

// ── TensorBoard Client ─────────────────────────────────────────

// TensorBoardClient communicates with the Python ML server's TensorBoard endpoint.
type TensorBoardClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewTensorBoardClient creates a new TensorBoard client.
func NewTensorBoardClient(baseURL string) *TensorBoardClient {
	if baseURL == "" {
		baseURL = "http://localhost:8001"
	}
	return &TensorBoardClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// ListRuns returns all TensorBoard runs.
func (tc *TensorBoardClient) ListRuns() (*TensorBoardSummary, error) {
	resp, err := tc.httpClient.Get(tc.baseURL + "/tensorboard/runs")
	if err != nil {
		return nil, fmt.Errorf("tensorboard list: %w", err)
	}
	defer resp.Body.Close()

	var result TensorBoardSummary
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("tensorboard list parse: %w", err)
	}
	return &result, nil
}

// QueryScalars queries scalar metrics for a run.
func (tc *TensorBoardClient) QueryScalars(req TensorBoardQueryRequest) (*TensorBoardQueryResult, error) {
	body, _ := json.Marshal(req)
	resp, err := tc.httpClient.Post(tc.baseURL+"/tensorboard/scalars", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tensorboard query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tensorboard query failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var result TensorBoardQueryResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("tensorboard query parse: %w", err)
	}
	return &result, nil
}

// GetRun returns details for a specific run.
func (tc *TensorBoardClient) GetRun(runID string) (*TensorBoardRun, error) {
	resp, err := tc.httpClient.Get(tc.baseURL + "/tensorboard/runs/" + runID)
	if err != nil {
		return nil, fmt.Errorf("tensorboard get run: %w", err)
	}
	defer resp.Body.Close()

	var result TensorBoardRun
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("tensorboard get run parse: %w", err)
	}
	return &result, nil
}

// DeleteRun removes a TensorBoard run and its data.
func (tc *TensorBoardClient) DeleteRun(runID string) error {
	req, _ := http.NewRequest("DELETE", tc.baseURL+"/tensorboard/runs/"+runID, nil)
	resp, err := tc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("tensorboard delete: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// ── In-Memory TensorBoard Collector (Go side) ──────────────────

// TensorBoardCollector collects metrics locally and forwards to ML server.
type TensorBoardCollector struct {
	mu       sync.RWMutex
	scalars  map[string][]TensorBoardScalar // tag -> points
	runs     map[string]*TensorBoardRun
	client   *TensorBoardClient
	enabled  bool
}

// NewTensorBoardCollector creates a local collector.
func NewTensorBoardCollector(client *TensorBoardClient) *TensorBoardCollector {
	return &TensorBoardCollector{
		scalars: make(map[string][]TensorBoardScalar),
		runs:    make(map[string]*TensorBoardRun),
		client:  client,
		enabled: true,
	}
}

// SetEnabled enables or disables collection.
func (col *TensorBoardCollector) SetEnabled(v bool) {
	col.mu.Lock()
	col.enabled = v
	col.mu.Unlock()
}

// RecordScalar records a scalar metric locally.
func (col *TensorBoardCollector) RecordScalar(runID, tag string, step int, value float64) {
	col.mu.Lock()
	defer col.mu.Unlock()
	if !col.enabled {
		return
	}

	point := TensorBoardScalar{
		Tag:      tag,
		Step:     step,
		WallTime: float64(time.Now().Unix()),
		Value:    value,
	}
	key := runID + "/" + tag
	col.scalars[key] = append(col.scalars[key], point)

	// Update run
	if run, ok := col.runs[runID]; ok {
		run.UpdatedAt = time.Now()
		found := false
		for _, t := range run.Tags {
			if t == tag {
				found = true
				break
			}
		}
		if !found {
			run.Tags = append(run.Tags, tag)
		}
	}
}

// StartRun registers a new run.
func (col *TensorBoardCollector) StartRun(runID, runName, modelType, modelID string) {
	col.mu.Lock()
	defer col.mu.Unlock()
	col.runs[runID] = &TensorBoardRun{
		RunID:     runID,
		RunName:   runName,
		ModelType: modelType,
		ModelID:   modelID,
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
		Status:    "running",
		Tags:      []string{},
	}
}

// FinishRun marks a run as completed.
func (col *TensorBoardCollector) FinishRun(runID string, status string) {
	col.mu.Lock()
	defer col.mu.Unlock()
	if run, ok := col.runs[runID]; ok {
		run.Status = status
		run.UpdatedAt = time.Now()
	}
}

// GetRunScalars returns all scalars for a run.
func (col *TensorBoardCollector) GetRunScalars(runID string) map[string][]TensorBoardScalar {
	col.mu.RLock()
	defer col.mu.RUnlock()

	result := make(map[string][]TensorBoardScalar)
	prefix := runID + "/"
	for key, points := range col.scalars {
		if len(points) > 0 && points[0].Tag == key[len(prefix):] && len(key) > len(prefix) && key[:len(prefix)] == prefix {
			result[points[0].Tag] = append([]TensorBoardScalar{}, points...)
		}
	}
	return result
}

// ListLocalRuns returns all local runs.
func (col *TensorBoardCollector) ListLocalRuns() []TensorBoardRun {
	col.mu.RLock()
	defer col.mu.RUnlock()

	runs := make([]TensorBoardRun, 0, len(col.runs))
	for _, r := range col.runs {
		runs = append(runs, *r)
	}
	return runs
}
