package model

// ── Signal ──

type Signal struct {
	Symbol    string  `json:"symbol"`
	Direction string  `json:"direction"` // LONG, SHORT, CLOSE
	Strength  float64 `json:"strength"`  // 0.0 - 1.0
	Strategy  string  `json:"strategy"`
	Reason    string  `json:"reason"`
	Timestamp int64   `json:"timestamp"`
	Qty       float64 `json:"qty,omitempty"`
}
