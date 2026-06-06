// Package ml provides RL (Reinforcement Learning) client extensions.
// Aligns with FreqAI's RL approach: Base3ActionRLEnv / Base5ActionRLEnv.
package ml

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ── RL Types ───────────────────────────────────────────────────

// RLTrainConfig configures an RL training run.
type RLTrainConfig struct {
	ModelID          string           `json:"model_id"`
	Algorithm        string           `json:"algorithm"`         // qlearning, ppo, a2c, sac
	NActions         int              `json:"n_actions"`         // 3 or 5
	Symbol           string           `json:"symbol"`
	Interval         string           `json:"interval"`
	Bars             []map[string]any `json:"bars"`
	Episodes         int              `json:"episodes"`
	LearningRate     float64          `json:"learning_rate"`
	Discount         float64          `json:"discount"`
	Epsilon          float64          `json:"epsilon"`
	WindowSize       int              `json:"window_size"`
	InitialBalance   float64          `json:"initial_balance"`
	Commission       float64          `json:"commission"`
	FeatureConfig    map[string]any   `json:"feature_config,omitempty"`
	UseTensorBoard   bool             `json:"use_tensorboard"`
	TensorBoardRunID string           `json:"tensorboard_run_id,omitempty"`
}

// RLTrainResult holds the outcome of RL training.
type RLTrainResult struct {
	Success          bool              `json:"success"`
	ModelID          string            `json:"model_id"`
	Algorithm        string            `json:"algorithm"`
	NActions         int               `json:"n_actions"`
	Episodes         int               `json:"episodes"`
	FinalBalance     float64           `json:"final_balance"`
	TotalPnL         float64           `json:"total_pnl"`
	BestReward       float64           `json:"best_reward"`
	AvgRewardLast10  float64           `json:"avg_reward_last_10"`
	QTableSize       int               `json:"q_table_size,omitempty"`
	EpisodeRewards   []float64         `json:"episode_rewards"`
	Metrics          map[string]any    `json:"metrics"`
	TensorBoardURL   string            `json:"tensorboard_url,omitempty"`
	DurationSec      float64           `json:"duration_sec"`
	Error            string            `json:"error,omitempty"`
}

// RLPredictInput sends bars for RL agent inference.
type RLPredictInput struct {
	ModelID string           `json:"model_id"`
	Bars    []map[string]any `json:"bars"`
}

// RLPredictResult returns the RL agent's action.
type RLPredictResult struct {
	Success    bool    `json:"success"`
	ModelID    string  `json:"model_id"`
	Action     int     `json:"action"`      // 0=SHORT/FULL_SHORT, 1=NEUTRAL, 2=LONG/FULL_LONG
	ActionName string  `json:"action_name"`
	Confidence float64 `json:"confidence"`
	Position   float64 `json:"position"`    // -1.0 to 1.0
}

// RLEvalResult holds evaluation metrics for an RL agent.
type RLEvalResult struct {
	Success        bool              `json:"success"`
	ModelID        string            `json:"model_id"`
	TotalReturnPct float64           `json:"total_return_pct"`
	SharpeRatio    float64           `json:"sharpe_ratio"`
	MaxDrawdownPct float64           `json:"max_drawdown_pct"`
	WinRate        float64           `json:"win_rate"`
	Trades         int               `json:"trades"`
	AvgTradeReturn float64           `json:"avg_trade_return"`
	Metrics        map[string]any    `json:"metrics"`
}

// ── RL Client Methods ─────────────────────────────────────────

// RLTrain starts reinforcement learning training.
func (c *Client) RLTrain(cfg RLTrainConfig) (*RLTrainResult, error) {
	body, _ := json.Marshal(cfg)
	resp, err := c.httpClient.Post(c.baseURL+"/rl/train", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("rl train: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("rl train failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var result RLTrainResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("rl train parse: %w", err)
	}
	return &result, nil
}

// RLPredict gets the RL agent's action for given bars.
func (c *Client) RLPredict(input RLPredictInput) (*RLPredictResult, error) {
	body, _ := json.Marshal(input)
	resp, err := c.httpClient.Post(c.baseURL+"/rl/predict", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("rl predict: %w", err)
	}
	defer resp.Body.Close()

	var result RLPredictResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("rl predict parse: %w", err)
	}
	return &result, nil
}

// RLEvaluate evaluates a trained RL agent on test data.
func (c *Client) RLEvaluate(modelID string, bars []map[string]any) (*RLEvalResult, error) {
	body, _ := json.Marshal(map[string]any{
		"model_id": modelID,
		"bars":     bars,
	})
	resp, err := c.httpClient.Post(c.baseURL+"/rl/evaluate", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("rl evaluate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("rl evaluate failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var result RLEvalResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("rl evaluate parse: %w", err)
	}
	return &result, nil
}

// ListRLModels returns all trained RL models.
func (c *Client) ListRLModels() ([]ModelInfo, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/rl/models")
	if err != nil {
		return nil, fmt.Errorf("rl list: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Models []ModelInfo `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("rl list parse: %w", err)
	}
	return result.Models, nil
}

// DeleteRLModel deletes a trained RL model.
func (c *Client) DeleteRLModel(modelID string) error {
	req, _ := http.NewRequest("DELETE", c.baseURL+"/rl/models/"+modelID, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("rl delete: %w", err)
	}
	defer resp.Body.Close()
	return nil
}
