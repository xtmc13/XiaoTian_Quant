package indicator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// SandboxConfig holds configuration for the Python sandbox service.
type SandboxConfig struct {
	BaseURL string
	Client  *http.Client
}

// DefaultSandboxConfig returns config from environment or defaults.
func DefaultSandboxConfig() *SandboxConfig {
	baseURL := os.Getenv("SANDBOX_URL")
	if baseURL == "" {
		baseURL = "http://localhost:9000"
	}
	return &SandboxConfig{
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// SandboxExecuteRequest mirrors the Python sandbox /execute request.
type SandboxExecuteRequest struct {
	Code    string                 `json:"code"`
	DfJSON  []map[string]any       `json:"df_json,omitempty"`
	Params  map[string]any         `json:"params,omitempty"`
	Timeout int                    `json:"timeout,omitempty"`
}

// SandboxExecuteResponse mirrors the Python sandbox /execute response.
type SandboxExecuteResponse struct {
	Success   bool           `json:"success"`
	Msg       string         `json:"msg"`
	Output    map[string]any `json:"output,omitempty"`
	Error     string         `json:"error,omitempty"`
	ErrorType string         `json:"error_type,omitempty"`
}

// SandboxAnalyzeRequest mirrors the Python sandbox /analyze request.
type SandboxAnalyzeRequest struct {
	Code string `json:"code"`
}

// SandboxAnalyzeResponse mirrors the Python sandbox /analyze response.
type SandboxAnalyzeResponse struct {
	Success bool              `json:"success"`
	Hints   []ValidationHint  `json:"hints"`
}

// Execute calls the Python sandbox to safely execute indicator code.
func (c *SandboxConfig) Execute(code string, params map[string]any) (*SandboxExecuteResponse, error) {
	reqBody, err := json.Marshal(SandboxExecuteRequest{
		Code:    code,
		Params:  params,
		Timeout: 20,
	})
	if err != nil {
		return nil, err
	}

	resp, err := c.Client.Post(c.BaseURL+"/execute", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("sandbox execute request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sandbox returned status %d", resp.StatusCode)
	}

	var result SandboxExecuteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("sandbox response decode failed: %w", err)
	}
	return &result, nil
}

// Analyze calls the Python sandbox for static code analysis.
func (c *SandboxConfig) Analyze(code string) (*SandboxAnalyzeResponse, error) {
	reqBody, err := json.Marshal(SandboxAnalyzeRequest{Code: code})
	if err != nil {
		return nil, err
	}

	resp, err := c.Client.Post(c.BaseURL+"/analyze", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("sandbox analyze request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sandbox returned status %d", resp.StatusCode)
	}

	var result SandboxAnalyzeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("sandbox response decode failed: %w", err)
	}
	return &result, nil
}
