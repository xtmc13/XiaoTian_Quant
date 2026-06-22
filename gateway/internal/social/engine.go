package social

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Signal ─────────────────────────────────────────────────────

// Signal represents a trading signal from a strategy or trader.
type Signal struct {
	ID          string    `json:"id"`
	ProviderID  int       `json:"provider_id"`   // signal source user/strategy
	ProviderName string   `json:"provider_name"`
	Symbol      string    `json:"symbol"`
	Direction   string    `json:"direction"`     // buy, sell, close
	Price       float64   `json:"price"`         // suggested entry price
	StopLoss    float64   `json:"stop_loss"`
	TakeProfit  float64   `json:"take_profit"`
	Size        float64   `json:"size"`          // position size ratio (0-1)
	Confidence  float64   `json:"confidence"`    // 0-100
	Strategy    string    `json:"strategy"`      // strategy type/name
	Reason      string    `json:"reason"`        // signal rationale
	Timestamp   int64     `json:"timestamp"`
	ExpiresAt   int64     `json:"expires_at"`    // signal validity
}

// IsExpired returns true if the signal has expired.
func (s *Signal) IsExpired() bool {
	return s.ExpiresAt > 0 && time.Now().UnixMilli() > s.ExpiresAt
}

// ── Signal Provider ────────────────────────────────────────────

// ProviderStats tracks a signal provider's performance.
type ProviderStats struct {
	ProviderID      int     `json:"provider_id"`
	TotalSignals    int     `json:"total_signals"`
	WinCount        int     `json:"win_count"`
	LossCount       int     `json:"loss_count"`
	WinRate         float64 `json:"win_rate"`
	AvgReturnPct    float64 `json:"avg_return_pct"`
	SharpeRatio     float64 `json:"sharpe_ratio"`
	MaxDrawdownPct  float64 `json:"max_drawdown_pct"`
	FollowerCount   int     `json:"follower_count"`
	MonthlyFee      float64 `json:"monthly_fee"`
	IsPublic        bool    `json:"is_public"`
}

// ── Copy Trading ─────────────────────────────────────────────────

// CopyConfig configures how a follower copies a provider's signals.
type CopyConfig struct {
	FollowerID   int     `json:"follower_id"`
	ProviderID   int     `json:"provider_id"`
	Enabled      bool    `json:"enabled"`
	Multiplier   float64 `json:"multiplier"`    // position size multiplier (0.1x - 5x)
	MaxPosition  float64 `json:"max_position"`  // max % of portfolio per trade
	MaxDailyLoss float64 `json:"max_daily_loss"` // daily loss limit %
	SlippagePct  float64 `json:"slippage_pct"`  // allowed slippage %
	AutoExecute  bool    `json:"auto_execute"`  // auto-execute or manual confirm
	Symbols      []string `json:"symbols"`      // whitelist symbols (empty = all)
}

func DefaultCopyConfig(followerID, providerID int) CopyConfig {
	return CopyConfig{
		FollowerID:   followerID,
		ProviderID:   providerID,
		Enabled:      true,
		Multiplier:   1.0,
		MaxPosition:  0.1,  // 10% max per trade
		MaxDailyLoss: 0.05, // 5% daily loss limit
		SlippagePct:  0.5,  // 0.5% slippage
		AutoExecute:  false, // manual confirm by default
	}
}

// ── Signal Engine ──────────────────────────────────────────────

// Engine manages signal distribution and copy trading execution.
type Engine struct {
	mu         sync.RWMutex
	providers  map[int]*ProviderStats
	followers  map[int]map[int]*CopyConfig // providerID -> followerID -> config
	signals    []Signal                    // recent signals (ring buffer)
	maxSignals int

	// Risk tracking
	dailyLosses    map[int]float64 // followerID -> cumulative daily loss
	dailyLossDay   string          // date string for daily reset
	getMarketPrice func(symbol string) float64

	// Callbacks
	OnSignal     func(s Signal)
	OnCopyTrade  func(followerID int, signal Signal, cfg CopyConfig)
	OnRiskBlock  func(followerID int, reason string)
}

// NewEngine creates a signal engine.
func NewEngine() *Engine {
	return &Engine{
		providers:   make(map[int]*ProviderStats),
		followers:   make(map[int]map[int]*CopyConfig),
		maxSignals:  10000,
		dailyLosses: make(map[int]float64),
	}
}

// SetMarketPriceProvider sets a callback for fetching current market prices.
func (e *Engine) SetMarketPriceProvider(fn func(symbol string) float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.getMarketPrice = fn
}

// RegisterProvider registers a signal provider.
func (e *Engine) RegisterProvider(providerID int, monthlyFee float64, isPublic bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.providers[providerID] = &ProviderStats{
		ProviderID:   providerID,
		MonthlyFee:   monthlyFee,
		IsPublic:     isPublic,
		FollowerCount: 0,
	}
}

// Follow starts following a provider.
func (e *Engine) Follow(cfg CopyConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	provider, ok := e.providers[cfg.ProviderID]
	if !ok {
		return fmt.Errorf("provider %d not found", cfg.ProviderID)
	}
	if !provider.IsPublic && cfg.FollowerID != cfg.ProviderID {
		return fmt.Errorf("provider %d is not public", cfg.ProviderID)
	}

	if e.followers[cfg.ProviderID] == nil {
		e.followers[cfg.ProviderID] = make(map[int]*CopyConfig)
	}
	e.followers[cfg.ProviderID][cfg.FollowerID] = &cfg
	provider.FollowerCount++

	return nil
}

// Unfollow stops following a provider.
func (e *Engine) Unfollow(followerID, providerID int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if followers, ok := e.followers[providerID]; ok {
		if _, ok := followers[followerID]; ok {
			delete(followers, followerID)
			if provider, ok := e.providers[providerID]; ok {
				provider.FollowerCount--
			}
		}
	}
}

// UpdateFollowConfig updates an existing follower's copy config.
func (e *Engine) UpdateFollowConfig(followerID, providerID int, update CopyConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	followers, ok := e.followers[providerID]
	if !ok || followers[followerID] == nil {
		return fmt.Errorf("not following provider %d", providerID)
	}
	cfg := followers[followerID]
	cfg.Enabled = update.Enabled
	if update.Multiplier > 0 {
		cfg.Multiplier = update.Multiplier
	}
	if update.MaxPosition > 0 {
		cfg.MaxPosition = update.MaxPosition
	}
	if update.MaxDailyLoss > 0 {
		cfg.MaxDailyLoss = update.MaxDailyLoss
	}
	if update.SlippagePct > 0 {
		cfg.SlippagePct = update.SlippagePct
	}
	cfg.AutoExecute = update.AutoExecute
	if len(update.Symbols) > 0 {
		cfg.Symbols = update.Symbols
	}
	return nil
}

// PublishSignal broadcasts a signal to all followers.
func (e *Engine) PublishSignal(s Signal) {
	e.mu.Lock()
	e.signals = append(e.signals, s)
	if len(e.signals) > e.maxSignals {
		e.signals = e.signals[len(e.signals)-e.maxSignals:]
	}
	e.mu.Unlock()

	// Update provider stats
	e.mu.Lock()
	if provider, ok := e.providers[s.ProviderID]; ok {
		provider.TotalSignals++
	}
	e.mu.Unlock()

	if e.OnSignal != nil {
		e.OnSignal(s)
	}

	// Distribute to followers
	e.distribute(s)
}

func (e *Engine) distribute(s Signal) {
	e.mu.RLock()
	followers := e.followers[s.ProviderID]
	e.mu.RUnlock()

	for followerID, cfg := range followers {
		if !cfg.Enabled {
			continue
		}
		if s.IsExpired() {
			continue
		}
		if len(cfg.Symbols) > 0 && !contains(cfg.Symbols, s.Symbol) {
			continue
		}

		// Risk check
		if blocked, reason := e.riskCheck(followerID, s, cfg); blocked {
			if e.OnRiskBlock != nil {
				e.OnRiskBlock(followerID, reason)
			}
			continue
		}

		if e.OnCopyTrade != nil {
			e.OnCopyTrade(followerID, s, *cfg)
		}
	}
}

func (e *Engine) riskCheck(followerID int, s Signal, cfg *CopyConfig) (bool, string) {
	// Check max position size
	if cfg.MaxPosition > 0 && s.Size*cfg.Multiplier > cfg.MaxPosition {
		return true, fmt.Sprintf("position size %.2f exceeds max %.2f", s.Size*cfg.Multiplier, cfg.MaxPosition)
	}

	// Check slippage: compare signal price with current market price
	if cfg.SlippagePct > 0 && s.Price > 0 {
		e.mu.RLock()
		getPrice := e.getMarketPrice
		e.mu.RUnlock()
		if getPrice != nil {
			marketPrice := getPrice(s.Symbol)
			if marketPrice > 0 {
				slippage := math.Abs(s.Price-marketPrice) / marketPrice * 100
				if slippage > cfg.SlippagePct {
					return true, fmt.Sprintf("slippage %.2f%% exceeds limit %.2f%% (signal: %.4f, market: %.4f)",
						slippage, cfg.SlippagePct, s.Price, marketPrice)
				}
			}
		}
	}

	// Check daily loss limit
	if cfg.MaxDailyLoss > 0 {
		e.mu.Lock()
		today := time.Now().Format("2006-01-02")
		if e.dailyLossDay != today {
			e.dailyLosses = make(map[int]float64)
			e.dailyLossDay = today
		}
		loss := e.dailyLosses[followerID]
		e.mu.Unlock()
		if loss < -cfg.MaxDailyLoss {
			return true, fmt.Sprintf("daily loss %.2f%% exceeds limit %.2f%%", -loss, cfg.MaxDailyLoss)
		}
	}

	return false, ""
}

// RecordPnL records profit/loss for a follower's daily limit tracking.
func (e *Engine) RecordPnL(followerID int, pnl float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	today := time.Now().Format("2006-01-02")
	if e.dailyLossDay != today {
		e.dailyLosses = make(map[int]float64)
		e.dailyLossDay = today
	}
	e.dailyLosses[followerID] += pnl
}

// GetProviderSignals returns recent signals from a provider.
func (e *Engine) GetProviderSignals(providerID int, limit int) []Signal {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []Signal
	for i := len(e.signals) - 1; i >= 0 && len(result) < limit; i-- {
		if e.signals[i].ProviderID == providerID {
			result = append(result, e.signals[i])
		}
	}
	return result
}

// GetFollowerConfigs returns all copy configs for a follower.
func (e *Engine) GetFollowerConfigs(followerID int) []CopyConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []CopyConfig
	for _, followers := range e.followers {
		if cfg, ok := followers[followerID]; ok {
			result = append(result, *cfg)
		}
	}
	return result
}

// GetProviderStats returns stats for a provider.
func (e *Engine) GetProviderStats(providerID int) *ProviderStats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.providers[providerID]
}

// GetPublicProviders returns all public signal providers.
func (e *Engine) GetPublicProviders() []ProviderStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []ProviderStats
	for _, p := range e.providers {
		if p.IsPublic {
			result = append(result, *p)
		}
	}
	return result
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ── Signal Store (persistence) ─────────────────────────────────

// Store persists signals to database.
type Store struct{}

func NewStore() *Store { return &Store{} }

// SaveSignal persists a signal.
func (s *Store) SaveSignal(sig Signal) error {
	db := store.GetDB()
	if db == nil {
		return fmt.Errorf("database not available")
	}
	_, err := db.Exec(
		`INSERT INTO social_signals (id, provider_id, symbol, direction, price, stop_loss, take_profit, size, confidence, strategy, reason, timestamp, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET price=excluded.price, confidence=excluded.confidence`,
		sig.ID, sig.ProviderID, sig.Symbol, sig.Direction, sig.Price, sig.StopLoss, sig.TakeProfit,
		sig.Size, sig.Confidence, sig.Strategy, sig.Reason, sig.Timestamp, sig.ExpiresAt,
	)
	return err
}

// SaveCopyTrade persists a copy trade execution.
func (s *Store) SaveCopyTrade(followerID int, signal Signal, executed bool, reason string) error {
	db := store.GetDB()
	if db == nil {
		return fmt.Errorf("database not available")
	}
	_, err := db.Exec(
		`INSERT INTO copy_trades (follower_id, signal_id, provider_id, symbol, direction, executed, reason, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		followerID, signal.ID, signal.ProviderID, signal.Symbol, signal.Direction, executed, reason, time.Now().Unix(),
	)
	return err
}
