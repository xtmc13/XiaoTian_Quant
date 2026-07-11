package model

// ── Risk Alert ──

type RiskAlert struct {
	Level     string `json:"level"` // INFO, WARN, CRITICAL
	CheckName string `json:"check_name"`
	Message   string `json:"message"`
	Symbol    string `json:"symbol,omitempty"`
	Timestamp int64  `json:"timestamp"`
}
