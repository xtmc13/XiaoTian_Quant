package order

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// ── Order Timeout Config ───────────────────────────────────────

// TimeoutConfig configures order timeout and emergency exit behavior.
type TimeoutConfig struct {
	EntryTimeout      time.Duration `json:"entry_timeout"`       // max time an entry order can stay unfilled
	ExitTimeout       time.Duration `json:"exit_timeout"`        // max time an exit order can stay unfilled
	ExitTimeoutCount  int           `json:"exit_timeout_count"`   // N timeouts before emergency exit
	EmergencyExit     bool          `json:"emergency_exit"`       // use market order on emergency exit
	CancelUnfilled    bool          `json:"cancel_unfilled"`      // auto-cancel unfilled orders
}

func DefaultTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		EntryTimeout:     10 * time.Minute,
		ExitTimeout:      10 * time.Minute,
		ExitTimeoutCount: 3,
		EmergencyExit:    true,
		CancelUnfilled:   true,
	}
}

// TimeoutDecider 是可选的策略决策钩子（v1.2，对标 freqtrade
// check_entry_timeout/check_exit_timeout）：挂单超时先问策略。
// 返回 (action, true) 接管默认逻辑：
//
//	"cancel" — 撤单（同现有默认）
//	"keep"   — 保留挂单继续等待（重置计时，下个超时周期再问）
//
// ok=false（未注入或策略未实现）走 tracker 现有默认逻辑。
type TimeoutDecider func(state OrderState) (action string, ok bool)

// ── Order Timeout Tracker ──────────────────────────────────────

// TimeoutTracker tracks order age and triggers emergency actions.
type TimeoutTracker struct {
	config TimeoutConfig
	mu     sync.RWMutex

	orders map[string]*OrderState
	decide TimeoutDecider
}

// OrderState tracks an individual order's timeout state.
type OrderState struct {
	OrderID      string
	Symbol       string
	Side         string
	Type         string // "entry" or "exit"
	Price        float64
	Quantity     float64
	PlacedAt     time.Time
	TimeoutCount int    // number of times this order has timed out
	Status       string // "open", "timed_out", "cancelled", "emergency"
}

// NewTimeoutTracker creates a timeout tracker.
func NewTimeoutTracker(cfg TimeoutConfig) *TimeoutTracker {
	return &TimeoutTracker{
		config: cfg,
		orders: make(map[string]*OrderState),
	}
}

// SetDecider 注入策略超时决策钩子（v1.2；nil = 纯默认逻辑）。
// 生产接线：strategy.Engine.TimeoutDecider()。
func (t *TimeoutTracker) SetDecider(d TimeoutDecider) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.decide = d
}

// Track adds an order to timeout tracking.
func (t *TimeoutTracker) Track(orderID, symbol, side, orderType string, price, quantity float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.orders[orderID] = &OrderState{
		OrderID:  orderID,
		Symbol:   symbol,
		Side:     side,
		Type:     orderType,
		Price:    price,
		Quantity: quantity,
		PlacedAt: time.Now(),
		Status:   "open",
	}

	log.Printf("[timeout] tracking %s order %s for %s", orderType, orderID, symbol)
}

// Remove stops tracking an order (called when filled).
func (t *TimeoutTracker) Remove(orderID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.orders, orderID)
}

// CheckTimeout checks all tracked orders for timeout.
// Returns a list of actions to take.
func (t *TimeoutTracker) CheckTimeout() []TimeoutAction {
	t.mu.Lock()
	defer t.mu.Unlock()

	var actions []TimeoutAction
	now := time.Now()

	for id, state := range t.orders {
		if state.Status != "open" {
			continue
		}

		timeout := t.config.EntryTimeout
		if state.Type == "exit" {
			timeout = t.config.ExitTimeout
		}

		if now.Sub(state.PlacedAt) > timeout {
			// v1.2 策略钩子优先：策略决定撤单（cancel）或保留挂单（keep）。
			if t.decide != nil {
				if action, ok := t.decide(*state); ok {
					if action == "keep" {
						state.PlacedAt = now // 策略选择保留：重置计时，下个周期再问
						continue
					}
					state.TimeoutCount++
					state.Status = "timed_out"
					actions = append(actions, TimeoutAction{
						OrderID:      id,
						Symbol:       state.Symbol,
						Action:       action,
						TimeoutCount: state.TimeoutCount,
					})
					continue
				}
			}
			state.TimeoutCount++
			state.Status = "timed_out"

			action := TimeoutAction{
				OrderID:      id,
				Symbol:       state.Symbol,
				Action:       "cancel",
				TimeoutCount: state.TimeoutCount,
			}

			// Check if emergency exit is needed
			if state.Type == "exit" && state.TimeoutCount >= t.config.ExitTimeoutCount && t.config.EmergencyExit {
				action.Action = "emergency_market"
				state.Status = "emergency"
				log.Printf("[timeout] EMERGENCY EXIT triggered for %s after %d timeouts",
					state.Symbol, state.TimeoutCount)
			}

			actions = append(actions, action)
		}
	}

	return actions
}

// TimeoutAction describes what should be done for a timed-out order.
type TimeoutAction struct {
	OrderID      string `json:"order_id"`
	Symbol       string `json:"symbol"`
	Action       string `json:"action"` // "cancel" or "emergency_market"
	TimeoutCount int    `json:"timeout_count"`
}

// ActiveCount returns the number of orders being tracked.
func (t *TimeoutTracker) ActiveCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	count := 0
	for _, s := range t.orders {
		if s.Status == "open" {
			count++
		}
	}
	return count
}

// Summary returns a summary of all tracked orders.
func (t *TimeoutTracker) Summary() []OrderState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]OrderState, 0, len(t.orders))
	for _, s := range t.orders {
		result = append(result, *s)
	}
	return result
}

// ── Emergency Exit ─────────────────────────────────────────────

// EmergencyExiter handles emergency market exits when stoploss/exit fails.
type EmergencyExiter struct {
	orderPlacer OrderPlacer
	maxRetries  int
	mu          sync.Mutex
}

// NewEmergencyExiter creates an emergency exiter.
func NewEmergencyExiter(placer OrderPlacer, maxRetries int) *EmergencyExiter {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return &EmergencyExiter{
		orderPlacer: placer,
		maxRetries:  maxRetries,
	}
}

// EmergencyExit places a market order to force-exit a position.
// Used when stoploss placement fails or exit order times out too many times.
func (e *EmergencyExiter) EmergencyExit(symbol, direction string, quantity float64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Determine exit side: LONG position → SELL, SHORT position → BUY
	exitSide := "SELL"
	if direction == "SHORT" {
		exitSide = "BUY"
	}

	var lastErr error
	for i := 0; i < e.maxRetries; i++ {
		_, err := e.orderPlacer.PlaceOrder(symbol, exitSide, "MARKET", 0, quantity)
		if err == nil {
			log.Printf("[emergency] market %s %s %.4f SUCCESS", exitSide, symbol, quantity)
			return nil
		}
		lastErr = err
		log.Printf("[emergency] retry %d/%d: %v", i+1, e.maxRetries, err)
		time.Sleep(time.Duration(i+1) * time.Second)
	}

	return fmt.Errorf("emergency exit failed after %d retries: %w", e.maxRetries, lastErr)
}
