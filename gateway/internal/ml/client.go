// Package ml provides a Go client for the ML Server Python sidecar.
// The ML server handles model training, prediction, and feature engineering.
package ml

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ── Client ─────────────────────────────────────────────────────

// Client communicates with the Python ML server.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new ML service client.
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "http://localhost:8001"
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// ── Types ──────────────────────────────────────────────────────

type TrainConfig struct {
	ModelID       string           `json:"model_id"`
	ModelType     string           `json:"model_type"`   // lightgbm, xgboost
	TaskType      string           `json:"task_type"`     // regression, classification
	Symbol        string           `json:"symbol"`
	Interval      string           `json:"interval"`
	Bars          []map[string]any `json:"bars"`
	FeatureConfig map[string]any   `json:"feature_config,omitempty"`
	LabelConfig   map[string]any   `json:"label_config,omitempty"`
	ModelParams   map[string]any   `json:"model_params,omitempty"`
}

type TrainResult struct {
	Success      bool              `json:"success"`
	ModelID      string            `json:"model_id"`
	ModelType    string            `json:"model_type"`
	Metrics      map[string]any    `json:"metrics"`
	FeatureCount int               `json:"feature_count"`
	TrainSamples int               `json:"train_samples"`
	TestSamples  int               `json:"test_samples"`
}

type PredictInput struct {
	ModelID string           `json:"model_id"`
	Bars    []map[string]any `json:"bars"`
}

type PredictResult struct {
	Success    bool    `json:"success"`
	ModelID    string  `json:"model_id"`
	Prediction float64 `json:"prediction"`
	Direction  string  `json:"direction"`
	Strength   float64 `json:"strength"`
}

type FeatureResult struct {
	Success      bool     `json:"success"`
	FeatureCount int      `json:"feature_count"`
	FeatureNames []string `json:"feature_names"`
	SampleCount  int      `json:"sample_count"`
}

type ModelInfo struct {
	ModelID      string         `json:"model_id"`
	ModelType    string         `json:"model_type"`
	TaskType     string         `json:"task_type"`
	TrainedAt    string         `json:"trained_at"`
	Metrics      map[string]any `json:"metrics"`
	FeatureCount int            `json:"feature_count"`
}

type ImportanceItem struct {
	Name       string  `json:"name"`
	Importance float64 `json:"importance"`
}

// ── API Methods ────────────────────────────────────────────────

// Health checks if the ML server is reachable.
func (c *Client) Health() error {
	resp, err := c.httpClient.Get(c.baseURL + "/health")
	if err != nil {
		return fmt.Errorf("ml server unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("ml server returned %d", resp.StatusCode)
	}
	return nil
}

// Train starts model training.
func (c *Client) Train(cfg TrainConfig) (*TrainResult, error) {
	body, _ := json.Marshal(cfg)
	resp, err := c.httpClient.Post(c.baseURL+"/train", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ml train: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ml train failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var result TrainResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ml train parse: %w", err)
	}
	return &result, nil
}

// Predict makes a prediction using a trained model.
func (c *Client) Predict(input PredictInput) (*PredictResult, error) {
	body, _ := json.Marshal(input)
	resp, err := c.httpClient.Post(c.baseURL+"/predict", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ml predict: %w", err)
	}
	defer resp.Body.Close()

	var result PredictResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ml predict parse: %w", err)
	}
	return &result, nil
}

// ListModels returns all trained models.
func (c *Client) ListModels() ([]ModelInfo, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/models")
	if err != nil {
		return nil, fmt.Errorf("ml list: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Models []ModelInfo `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ml list parse: %w", err)
	}
	return result.Models, nil
}

// GetModel returns details for a specific model.
func (c *Client) GetModel(modelID string) (*ModelInfo, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/models/" + modelID)
	if err != nil {
		return nil, fmt.Errorf("ml get: %w", err)
	}
	defer resp.Body.Close()

	var result ModelInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ml get parse: %w", err)
	}
	return &result, nil
}

// DeleteModel deletes a trained model.
func (c *Client) DeleteModel(modelID string) error {
	req, _ := http.NewRequest("DELETE", c.baseURL+"/models/"+modelID, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ml delete: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// ExportModel downloads the model as JSON tree structure for local inference.
func (c *Client) ExportModel(modelID string) (*ExportedModel, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/models/" + modelID + "/export")
	if err != nil {
		return nil, fmt.Errorf("ml export: %w", err)
	}
	defer resp.Body.Close()

	var result ExportedModel
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ml export parse: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("ml export failed for %s", modelID)
	}
	return &result, nil
}

// FeatureImportance gets feature importance for a model.
func (c *Client) FeatureImportance(modelID string) ([]ImportanceItem, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/models/" + modelID + "/importance")
	if err != nil {
		return nil, fmt.Errorf("ml importance: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Success    bool             `json:"success"`
		Importance []ImportanceItem `json:"importance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ml importance parse: %w", err)
	}
	return result.Importance, nil
}

// GenerateFeatures generates features from OHLCV data.
func (c *Client) GenerateFeatures(bars []map[string]any, config map[string]any) (*FeatureResult, error) {
	body, _ := json.Marshal(map[string]any{
		"bars":   bars,
		"config": config,
	})
	resp, err := c.httpClient.Post(c.baseURL+"/features/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ml features: %w", err)
	}
	defer resp.Body.Close()

	var result FeatureResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ml features parse: %w", err)
	}
	return &result, nil
}
