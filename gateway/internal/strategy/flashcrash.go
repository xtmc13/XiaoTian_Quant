package strategy

import (
	"sync"
	"time"
)

// PricePoint represents a single price observation at a specific time.
type PricePoint struct {
	Price     float64 `json:"price"`
	Timestamp int64   `json:"timestamp"`
}

// FlashCrashDetector monitors price history within a sliding time window to
// detect rapid price drops (flash crashes) that exceed a configurable threshold.
// When a flash crash is detected, strategies should pause opening new positions
// to avoid buying into a falling knife.
type FlashCrashDetector struct {
	Window    time.Duration // Time window for detection (default 1 minute)
	Threshold float64       // Price drop threshold as ratio (default 0.02 = 2%)

	mu       sync.RWMutex
	history  []PricePoint
	lastDrop float64 // Last measured drop ratio
}

// NewFlashCrashDetector creates a new flash crash detector with sensible defaults.
func NewFlashCrashDetector() *FlashCrashDetector {
	return &FlashCrashDetector{
		Window:    time.Minute,
		Threshold: 0.02, // 2% default
		history:   make([]PricePoint, 0, 128),
	}
}

// NewFlashCrashDetectorWithParams creates a detector with custom parameters.
func NewFlashCrashDetectorWithParams(window time.Duration, threshold float64) *FlashCrashDetector {
	if window <= 0 {
		window = time.Minute
	}
	if threshold <= 0 {
		threshold = 0.02
	}
	return &FlashCrashDetector{
		Window:    window,
		Threshold: threshold,
		history:   make([]PricePoint, 0, 128),
	}
}

// AddPrice adds a new price point to the history. It automatically evicts
// points that fall outside the detection window.
func (d *FlashCrashDetector) AddPrice(price float64, timestamp int64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Evict old points outside the window
	cutoff := timestamp - int64(d.Window.Seconds())
	idx := 0
	for i, p := range d.history {
		if p.Timestamp >= cutoff {
			idx = i
			break
		}
		idx = i + 1
	}
	if idx > 0 {
		d.history = d.history[idx:]
	}

	d.history = append(d.history, PricePoint{
		Price:     price,
		Timestamp: timestamp,
	})
}

// IsFlashCrash returns true if the price has dropped by more than the
// threshold within the detection window.
func (d *FlashCrashDetector) IsFlashCrash() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.history) < 2 {
		return false
	}

	// Find the highest price in the window
	maxPrice := d.history[0].Price
	for _, p := range d.history {
		if p.Price > maxPrice {
			maxPrice = p.Price
		}
	}

	currentPrice := d.history[len(d.history)-1].Price
	if maxPrice <= 0 {
		return false
	}

	drop := (maxPrice - currentPrice) / maxPrice
	d.lastDrop = drop

	return drop >= d.Threshold
}

// LastDrop returns the most recent measured price drop ratio.
func (d *FlashCrashDetector) LastDrop() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastDrop
}

// History returns a copy of the current price history within the window.
func (d *FlashCrashDetector) History() []PricePoint {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]PricePoint, len(d.history))
	copy(result, d.history)
	return result
}

// Reset clears all price history.
func (d *FlashCrashDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.history = d.history[:0]
	d.lastDrop = 0
}

// PriceDropInWindow calculates the price drop from the highest point in the
// current window to the current price. Returns the drop ratio (0-1).
func (d *FlashCrashDetector) PriceDropInWindow() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.history) < 2 {
		return 0
	}

	maxPrice := d.history[0].Price
	for _, p := range d.history {
		if p.Price > maxPrice {
			maxPrice = p.Price
		}
	}

	currentPrice := d.history[len(d.history)-1].Price
	if maxPrice <= 0 {
		return 0
	}

	return (maxPrice - currentPrice) / maxPrice
}
