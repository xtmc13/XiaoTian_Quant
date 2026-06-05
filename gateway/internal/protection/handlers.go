package protection

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// ── CooldownPeriod ─────────────────────────────────────────────
// Prevents entering a new trade on a pair immediately after closing one.
// This gives the pair time to "cool down" and avoids overtrading.

type CooldownPeriod struct {
	StopDurationCandles int `json:"stop_duration_candles"` // number of candles to wait
	Timeframe           string `json:"timeframe"`          // candle timeframe (e.g., "1h", "15m")

	mu          sync.RWMutex
	lastExits   map[string]time.Time // symbol -> last exit time
}

func NewCooldownPeriod(params map[string]any) (*CooldownPeriod, error) {
	p := &CooldownPeriod{
		lastExits: make(map[string]time.Time),
	}
	if v, ok := toInt(params["stop_duration_candles"]); ok {
		p.StopDurationCandles = v
	}
	if v, ok := params["timeframe"].(string); ok {
		p.Timeframe = v
	}
	if p.StopDurationCandles <= 0 {
		p.StopDurationCandles = 5
	}
	if p.Timeframe == "" {
		p.Timeframe = "1h"
	}
	return p, nil
}

func (p *CooldownPeriod) Name() string        { return "CooldownPeriod" }
func (p *CooldownPeriod) Description() string { return "Wait N candles after selling before re-entering" }

func (p *CooldownPeriod) Check(ctx ProtectionContext) ProtectionResult {
	p.mu.RLock()
	lastExit, ok := p.lastExits[ctx.Symbol]
	p.mu.RUnlock()

	if !ok {
		return ProtectionResult{Blocked: false}
	}

	// Calculate cooldown duration
	duration := p.candleDuration()
	cooldown := time.Duration(p.StopDurationCandles) * duration
	resumeTime := lastExit.Add(cooldown)

	if ctx.CurrentTime.Before(resumeTime) {
		return ProtectionResult{
			Blocked:    true,
			Reason:     fmt.Sprintf("CooldownPeriod: %s cooling down for %d %s candles", ctx.Symbol, p.StopDurationCandles, p.Timeframe),
			ResumeTime: resumeTime,
			Pair:       ctx.Symbol,
		}
	}

	return ProtectionResult{Blocked: false}
}

func (p *CooldownPeriod) Validate() error {
	if p.StopDurationCandles <= 0 {
		return fmt.Errorf("CooldownPeriod: stop_duration_candles must be > 0")
	}
	if p.Timeframe == "" {
		return fmt.Errorf("CooldownPeriod: timeframe is required")
	}
	return nil
}

func (p *CooldownPeriod) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastExits = make(map[string]time.Time)
}

// RecordExit records that a trade was exited for a symbol.
func (p *CooldownPeriod) RecordExit(symbol string, exitTime time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastExits[symbol] = exitTime
}

func (p *CooldownPeriod) candleDuration() time.Duration {
	switch p.Timeframe {
	case "1m":
		return time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	case "2h":
		return 2 * time.Hour
	case "4h":
		return 4 * time.Hour
	case "6h":
		return 6 * time.Hour
	case "8h":
		return 8 * time.Hour
	case "12h":
		return 12 * time.Hour
	case "1d":
		return 24 * time.Hour
	case "3d":
		return 72 * time.Hour
	case "1w":
		return 7 * 24 * time.Hour
	default:
		return time.Hour
	}
}

// ── StoplossGuard ──────────────────────────────────────────────
// Stops trading if a certain number of stoplosses occur within a time window.

type StoplossGuard struct {
	LookbackPeriodCandles int `json:"lookback_period_candles"` // time window to look back
	TradeLimit            int `json:"trade_limit"`             // max stoplosses allowed in window
	StopDurationCandles   int `json:"stop_duration_candles"` // how long to stop after limit reached
	Timeframe             string `json:"timeframe"`
	OnlyPerPair           bool   `json:"only_per_pair"`          // if true, only blocks the affected pair

	mu            sync.RWMutex
	stoplosses    []stoplossRecord // global stoploss history
	pairStoplosses map[string][]stoplossRecord // per-pair stoploss history
}

type stoplossRecord struct {
	Symbol string    `json:"symbol"`
	Time   time.Time `json:"time"`
}

func NewStoplossGuard(params map[string]any) (*StoplossGuard, error) {
	p := &StoplossGuard{
		stoplosses:     make([]stoplossRecord, 0),
		pairStoplosses: make(map[string][]stoplossRecord),
	}
	if v, ok := toInt(params["lookback_period_candles"]); ok {
		p.LookbackPeriodCandles = v
	}
	if v, ok := toInt(params["trade_limit"]); ok {
		p.TradeLimit = v
	}
	if v, ok := toInt(params["stop_duration_candles"]); ok {
		p.StopDurationCandles = v
	}
	if v, ok := params["timeframe"].(string); ok {
		p.Timeframe = v
	}
	if v, ok := params["only_per_pair"].(bool); ok {
		p.OnlyPerPair = v
	}

	if p.LookbackPeriodCandles <= 0 {
		p.LookbackPeriodCandles = 24
	}
	if p.TradeLimit <= 0 {
		p.TradeLimit = 4
	}
	if p.StopDurationCandles <= 0 {
		p.StopDurationCandles = 12
	}
	if p.Timeframe == "" {
		p.Timeframe = "1h"
	}
	return p, nil
}

func (p *StoplossGuard) Name() string        { return "StoplossGuard" }
func (p *StoplossGuard) Description() string { return "Stop trading after N stoplosses in a window" }

func (p *StoplossGuard) Check(ctx ProtectionContext) ProtectionResult {
	p.mu.RLock()
	defer p.mu.RUnlock()

	lookback := p.lookbackDuration()
	cutoff := ctx.CurrentTime.Add(-lookback)

	// Check per-pair stoplosses
	if p.OnlyPerPair {
		pairRecords := p.pairStoplosses[ctx.Symbol]
		count := countRecent(pairRecords, cutoff)
		if count >= p.TradeLimit {
			resumeTime := ctx.CurrentTime.Add(time.Duration(p.StopDurationCandles) * p.candleDuration())
			return ProtectionResult{
				Blocked:    true,
				Reason:     fmt.Sprintf("StoplossGuard: %d stoplosses on %s in last %d %s candles", count, ctx.Symbol, p.LookbackPeriodCandles, p.Timeframe),
				ResumeTime: resumeTime,
				Pair:       ctx.Symbol,
			}
		}
		return ProtectionResult{Blocked: false}
	}

	// Check global stoplosses
	count := countRecent(p.stoplosses, cutoff)
	if count >= p.TradeLimit {
		resumeTime := ctx.CurrentTime.Add(time.Duration(p.StopDurationCandles) * p.candleDuration())
		return ProtectionResult{
			Blocked:    true,
			Reason:     fmt.Sprintf("StoplossGuard: %d stoplosses in last %d %s candles", count, p.LookbackPeriodCandles, p.Timeframe),
			ResumeTime: resumeTime,
		}
	}

	return ProtectionResult{Blocked: false}
}

func (p *StoplossGuard) Validate() error {
	if p.LookbackPeriodCandles <= 0 {
		return fmt.Errorf("StoplossGuard: lookback_period_candles must be > 0")
	}
	if p.TradeLimit <= 0 {
		return fmt.Errorf("StoplossGuard: trade_limit must be > 0")
	}
	if p.StopDurationCandles <= 0 {
		return fmt.Errorf("StoplossGuard: stop_duration_candles must be > 0")
	}
	return nil
}

func (p *StoplossGuard) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stoplosses = p.stoplosses[:0]
	p.pairStoplosses = make(map[string][]stoplossRecord)
}

// RecordStoploss records a stoploss event.
func (p *StoplossGuard) RecordStoploss(symbol string, t time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	rec := stoplossRecord{Symbol: symbol, Time: t}
	p.stoplosses = append(p.stoplosses, rec)

	if p.pairStoplosses[symbol] == nil {
		p.pairStoplosses[symbol] = make([]stoplossRecord, 0)
	}
	p.pairStoplosses[symbol] = append(p.pairStoplosses[symbol], rec)

	// Cleanup old records
	p.cleanup(t.Add(-p.lookbackDuration() * 2))
}

func (p *StoplossGuard) cleanup(cutoff time.Time) {
	// Global
	newGlobal := make([]stoplossRecord, 0, len(p.stoplosses))
	for _, r := range p.stoplosses {
		if r.Time.After(cutoff) {
			newGlobal = append(newGlobal, r)
		}
	}
	p.stoplosses = newGlobal

	// Per-pair
	for sym, records := range p.pairStoplosses {
		newRecords := make([]stoplossRecord, 0, len(records))
		for _, r := range records {
			if r.Time.After(cutoff) {
				newRecords = append(newRecords, r)
			}
		}
		p.pairStoplosses[sym] = newRecords
	}
}

func (p *StoplossGuard) lookbackDuration() time.Duration {
	return time.Duration(p.LookbackPeriodCandles) * p.candleDuration()
}

func (p *StoplossGuard) candleDuration() time.Duration {
	switch p.Timeframe {
	case "1m":
		return time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	case "2h":
		return 2 * time.Hour
	case "4h":
		return 4 * time.Hour
	case "6h":
		return 6 * time.Hour
	case "8h":
		return 8 * time.Hour
	case "12h":
		return 12 * time.Hour
	case "1d":
		return 24 * time.Hour
	case "3d":
		return 72 * time.Hour
	case "1w":
		return 7 * 24 * time.Hour
	default:
		return time.Hour
	}
}

func countRecent(records []stoplossRecord, cutoff time.Time) int {
	count := 0
	for _, r := range records {
		if r.Time.After(cutoff) {
			count++
		}
	}
	return count
}

// ── MaxDrawdown ────────────────────────────────────────────────
// Stops all trading if the portfolio drawdown exceeds a threshold.

type MaxDrawdown struct {
	MaxDrawdownPct      float64 `json:"max_drawdown_pct"`      // 0.0 to 1.0
	LookbackPeriodCandles int   `json:"lookback_period_candles"` // lookback window
	Timeframe           string  `json:"timeframe"`
	StopDurationCandles int     `json:"stop_duration_candles"` // how long to stop
}

func NewMaxDrawdown(params map[string]any) (*MaxDrawdown, error) {
	p := &MaxDrawdown{}
	if v, ok := toFloat(params["max_drawdown_pct"]); ok {
		p.MaxDrawdownPct = v
	}
	if v, ok := toInt(params["lookback_period_candles"]); ok {
		p.LookbackPeriodCandles = v
	}
	if v, ok := params["timeframe"].(string); ok {
		p.Timeframe = v
	}
	if v, ok := toInt(params["stop_duration_candles"]); ok {
		p.StopDurationCandles = v
	}

	if p.MaxDrawdownPct <= 0 {
		p.MaxDrawdownPct = 0.20 // 20% default
	}
	if p.LookbackPeriodCandles <= 0 {
		p.LookbackPeriodCandles = 48 // 48 candles
	}
	if p.StopDurationCandles <= 0 {
		p.StopDurationCandles = 12
	}
	if p.Timeframe == "" {
		p.Timeframe = "1h"
	}
	return p, nil
}

func (p *MaxDrawdown) Name() string        { return "MaxDrawdown" }
func (p *MaxDrawdown) Description() string { return "Stop trading if drawdown exceeds threshold" }

func (p *MaxDrawdown) Check(ctx ProtectionContext) ProtectionResult {
	if ctx.CurrentDrawdown >= p.MaxDrawdownPct {
		resumeTime := ctx.CurrentTime.Add(p.stopDuration())
		return ProtectionResult{
			Blocked:    true,
			Reason:     fmt.Sprintf("MaxDrawdown: drawdown %.2f%% exceeds limit %.2f%%", ctx.CurrentDrawdown*100, p.MaxDrawdownPct*100),
			ResumeTime: resumeTime,
		}
	}
	return ProtectionResult{Blocked: false}
}

func (p *MaxDrawdown) Validate() error {
	if p.MaxDrawdownPct <= 0 || p.MaxDrawdownPct >= 1 {
		return fmt.Errorf("MaxDrawdown: max_drawdown_pct must be between 0 and 1")
	}
	return nil
}

func (p *MaxDrawdown) Reset() {}

func (p *MaxDrawdown) stopDuration() time.Duration {
	return time.Duration(p.StopDurationCandles) * p.candleDuration()
}

func (p *MaxDrawdown) candleDuration() time.Duration {
	switch p.Timeframe {
	case "1m":
		return time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	case "2h":
		return 2 * time.Hour
	case "4h":
		return 4 * time.Hour
	case "6h":
		return 6 * time.Hour
	case "8h":
		return 8 * time.Hour
	case "12h":
		return 12 * time.Hour
	case "1d":
		return 24 * time.Hour
	case "3d":
		return 72 * time.Hour
	case "1w":
		return 7 * 24 * time.Hour
	default:
		return time.Hour
	}
}

// ── LowProfitPairs ─────────────────────────────────────────────
// Locks pairs that have low combined profit over a lookback period.

type LowProfitPairs struct {
	LookbackPeriodCandles int     `json:"lookback_period_candles"`
	MinProfitRatio      float64 `json:"min_profit_ratio"`      // minimum profit ratio (e.g., 0.01 = 1%)
	MinTradeCount       int     `json:"min_trade_count"`       // minimum trades to evaluate
	Timeframe           string  `json:"timeframe"`
	StopDurationCandles int     `json:"stop_duration_candles"`

	mu          sync.RWMutex
	tradeHistory map[string][]TradeRecord // per-pair trade history
}

func NewLowProfitPairs(params map[string]any) (*LowProfitPairs, error) {
	p := &LowProfitPairs{
		tradeHistory: make(map[string][]TradeRecord),
	}
	if v, ok := toInt(params["lookback_period_candles"]); ok {
		p.LookbackPeriodCandles = v
	}
	if v, ok := toFloat(params["min_profit_ratio"]); ok {
		p.MinProfitRatio = v
	}
	if v, ok := toInt(params["min_trade_count"]); ok {
		p.MinTradeCount = v
	}
	if v, ok := params["timeframe"].(string); ok {
		p.Timeframe = v
	}
	if v, ok := toInt(params["stop_duration_candles"]); ok {
		p.StopDurationCandles = v
	}

	if p.LookbackPeriodCandles <= 0 {
		p.LookbackPeriodCandles = 24
	}
	if p.MinProfitRatio <= 0 {
		p.MinProfitRatio = 0.01 // 1%
	}
	if p.MinTradeCount <= 0 {
		p.MinTradeCount = 4
	}
	if p.StopDurationCandles <= 0 {
		p.StopDurationCandles = 12
	}
	if p.Timeframe == "" {
		p.Timeframe = "1h"
	}
	return p, nil
}

func (p *LowProfitPairs) Name() string        { return "LowProfitPairs" }
func (p *LowProfitPairs) Description() string { return "Lock pairs with low profit over a period" }

func (p *LowProfitPairs) Check(ctx ProtectionContext) ProtectionResult {
	p.mu.RLock()
	defer p.mu.RUnlock()

	records := p.tradeHistory[ctx.Symbol]
	if len(records) < p.MinTradeCount {
		return ProtectionResult{Blocked: false}
	}

	lookback := p.lookbackDuration()
	cutoff := ctx.CurrentTime.Add(-lookback)

	var totalProfit, totalVolume float64
	tradeCount := 0
	for _, r := range records {
		if r.ExitTime.After(cutoff) {
			totalProfit += r.PnL
			totalVolume += r.EntryPrice * r.Quantity
			tradeCount++
		}
	}

	if tradeCount < p.MinTradeCount {
		return ProtectionResult{Blocked: false}
	}

	profitRatio := 0.0
	if totalVolume > 0 {
		profitRatio = totalProfit / totalVolume
	}

	if profitRatio < p.MinProfitRatio {
		resumeTime := ctx.CurrentTime.Add(p.stopDuration())
		return ProtectionResult{
			Blocked:    true,
			Reason:     fmt.Sprintf("LowProfitPairs: %s profit ratio %.4f below %.4f over %d trades", ctx.Symbol, profitRatio, p.MinProfitRatio, tradeCount),
			ResumeTime: resumeTime,
			Pair:       ctx.Symbol,
		}
	}

	return ProtectionResult{Blocked: false}
}

func (p *LowProfitPairs) Validate() error {
	if p.LookbackPeriodCandles <= 0 {
		return fmt.Errorf("LowProfitPairs: lookback_period_candles must be > 0")
	}
	if p.MinProfitRatio <= 0 {
		return fmt.Errorf("LowProfitPairs: min_profit_ratio must be > 0")
	}
	if p.MinTradeCount <= 0 {
		return fmt.Errorf("LowProfitPairs: min_trade_count must be > 0")
	}
	return nil
}

func (p *LowProfitPairs) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tradeHistory = make(map[string][]TradeRecord)
}

// RecordTrade records a completed trade for profit evaluation.
func (p *LowProfitPairs) RecordTrade(trade TradeRecord) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.tradeHistory[trade.Symbol] == nil {
		p.tradeHistory[trade.Symbol] = make([]TradeRecord, 0)
	}
	p.tradeHistory[trade.Symbol] = append(p.tradeHistory[trade.Symbol], trade)

	// Cleanup old records
	cutoff := trade.ExitTime.Add(-p.lookbackDuration() * 2)
	newRecords := make([]TradeRecord, 0)
	for _, r := range p.tradeHistory[trade.Symbol] {
		if r.ExitTime.After(cutoff) {
			newRecords = append(newRecords, r)
		}
	}
	p.tradeHistory[trade.Symbol] = newRecords
}

func (p *LowProfitPairs) lookbackDuration() time.Duration {
	return time.Duration(p.LookbackPeriodCandles) * p.candleDuration()
}

func (p *LowProfitPairs) stopDuration() time.Duration {
	return time.Duration(p.StopDurationCandles) * p.candleDuration()
}

func (p *LowProfitPairs) candleDuration() time.Duration {
	switch p.Timeframe {
	case "1m":
		return time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	case "2h":
		return 2 * time.Hour
	case "4h":
		return 4 * time.Hour
	case "6h":
		return 6 * time.Hour
	case "8h":
		return 8 * time.Hour
	case "12h":
		return 12 * time.Hour
	case "1d":
		return 24 * time.Hour
	case "3d":
		return 72 * time.Hour
	case "1w":
		return 7 * 24 * time.Hour
	default:
		return time.Hour
	}
}

// ── Helpers ────────────────────────────────────────────────────

func toInt(v any) (int, bool) {
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	}
	return 0, false
}

func toFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	}
	return 0, false
}

// ── DailyLossLimit ─────────────────────────────────────────────
// Blocks trading if daily loss exceeds a threshold.

type DailyLossLimit struct {
	MaxDailyLossPct float64 `json:"max_daily_loss_pct"` // max loss % per day
	ResetHour       int     `json:"reset_hour"`         // hour of day to reset (0-23)

	mu         sync.RWMutex
	dayLoss    float64   // accumulated loss today
	lastReset  time.Time // last reset time
}

func NewDailyLossLimit(params map[string]any) (*DailyLossLimit, error) {
	p := &DailyLossLimit{}
	if v, ok := toFloat(params["max_daily_loss_pct"]); ok {
		p.MaxDailyLossPct = v
	}
	if v, ok := toInt(params["reset_hour"]); ok {
		p.ResetHour = v
	}
	if p.MaxDailyLossPct <= 0 {
		p.MaxDailyLossPct = 5.0
	}
	if p.ResetHour < 0 || p.ResetHour > 23 {
		p.ResetHour = 0
	}
	p.lastReset = time.Now()
	return p, nil
}

func (p *DailyLossLimit) Name() string        { return "DailyLossLimit" }
func (p *DailyLossLimit) Description() string { return "Stop trading if daily loss exceeds threshold" }

func (p *DailyLossLimit) Check(ctx ProtectionContext) ProtectionResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Reset at reset hour
	now := ctx.CurrentTime
	if now.Day() != p.lastReset.Day() || now.After(time.Date(now.Year(), now.Month(), now.Day(), p.ResetHour, 0, 0, 0, now.Location())) && p.lastReset.Before(time.Date(now.Year(), now.Month(), now.Day(), p.ResetHour, 0, 0, 0, now.Location())) {
		p.dayLoss = 0
		p.lastReset = now
	}

	// Accumulate loss from trade history
	for _, t := range ctx.TradeHistory {
		if t.ExitTime.Day() == now.Day() && t.PnL < 0 {
			p.dayLoss += -t.PnL
		}
	}

	if p.dayLoss > 0 && ctx.TotalBalance > 0 {
		lossPct := p.dayLoss / ctx.TotalBalance * 100
		if lossPct >= p.MaxDailyLossPct {
			return ProtectionResult{
				Blocked:    true,
				Reason:     fmt.Sprintf("DailyLossLimit: daily loss %.2f%% exceeds %.2f%% limit", lossPct, p.MaxDailyLossPct),
				ResumeTime: time.Date(now.Year(), now.Month(), now.Day()+1, p.ResetHour, 0, 0, 0, now.Location()),
			}
		}
	}

	return ProtectionResult{Blocked: false}
}

func (p *DailyLossLimit) Validate() error {
	if p.MaxDailyLossPct <= 0 {
		return fmt.Errorf("DailyLossLimit: max_daily_loss_pct must be > 0")
	}
	return nil
}

func (p *DailyLossLimit) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dayLoss = 0
	p.lastReset = time.Now()
}

// ── ConsecutiveLosses ──────────────────────────────────────────
// Blocks trading after N consecutive losing trades.

type ConsecutiveLosses struct {
	MaxConsecutive int `json:"max_consecutive"` // max consecutive losses allowed
	StopDuration   int `json:"stop_duration"`   // minutes to stop after limit

	mu         sync.RWMutex
	consecutive int
	lastLossTime time.Time
}

func NewConsecutiveLosses(params map[string]any) (*ConsecutiveLosses, error) {
	p := &ConsecutiveLosses{}
	if v, ok := toInt(params["max_consecutive"]); ok {
		p.MaxConsecutive = v
	}
	if v, ok := toInt(params["stop_duration"]); ok {
		p.StopDuration = v
	}
	if p.MaxConsecutive <= 0 {
		p.MaxConsecutive = 3
	}
	if p.StopDuration <= 0 {
		p.StopDuration = 30
	}
	return p, nil
}

func (p *ConsecutiveLosses) Name() string        { return "ConsecutiveLosses" }
func (p *ConsecutiveLosses) Description() string { return "Stop trading after N consecutive losses" }

func (p *ConsecutiveLosses) Check(ctx ProtectionContext) ProtectionResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Count consecutive losses from trade history
	consecutive := 0
	for i := len(ctx.TradeHistory) - 1; i >= 0; i-- {
		if ctx.TradeHistory[i].PnL < 0 {
			consecutive++
		} else {
			break
		}
	}
	p.consecutive = consecutive

	if consecutive >= p.MaxConsecutive {
		resumeTime := ctx.CurrentTime.Add(time.Duration(p.StopDuration) * time.Minute)
		return ProtectionResult{
			Blocked:    true,
			Reason:     fmt.Sprintf("ConsecutiveLosses: %d consecutive losses (max %d)", consecutive, p.MaxConsecutive),
			ResumeTime: resumeTime,
		}
	}

	return ProtectionResult{Blocked: false}
}

func (p *ConsecutiveLosses) Validate() error {
	if p.MaxConsecutive <= 0 {
		return fmt.Errorf("ConsecutiveLosses: max_consecutive must be > 0")
	}
	return nil
}

func (p *ConsecutiveLosses) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.consecutive = 0
}

// ── Overtrading ────────────────────────────────────────────────
// Blocks trading if trade frequency exceeds a threshold.

type Overtrading struct {
	MaxTradesPerHour int `json:"max_trades_per_hour"` // max trades per hour

	mu         sync.RWMutex
	tradeTimes []time.Time
}

func NewOvertrading(params map[string]any) (*Overtrading, error) {
	p := &Overtrading{}
	if v, ok := toInt(params["max_trades_per_hour"]); ok {
		p.MaxTradesPerHour = v
	}
	if p.MaxTradesPerHour <= 0 {
		p.MaxTradesPerHour = 10
	}
	p.tradeTimes = make([]time.Time, 0)
	return p, nil
}

func (p *Overtrading) Name() string        { return "Overtrading" }
func (p *Overtrading) Description() string { return "Prevent excessive trading frequency" }

func (p *Overtrading) Check(ctx ProtectionContext) ProtectionResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Clean old trades outside the hour window
	cutoff := ctx.CurrentTime.Add(-time.Hour)
	valid := make([]time.Time, 0, len(p.tradeTimes))
	for _, t := range p.tradeTimes {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	p.tradeTimes = valid

	// Add current trades
	for _, t := range ctx.TradeHistory {
		if t.ExitTime.After(cutoff) {
			p.tradeTimes = append(p.tradeTimes, t.ExitTime)
		}
	}

	if len(p.tradeTimes) >= p.MaxTradesPerHour {
		// Find earliest trade to calculate resume time
		earliest := p.tradeTimes[0]
		for _, t := range p.tradeTimes {
			if t.Before(earliest) {
				earliest = t
			}
		}
		resumeTime := earliest.Add(time.Hour)
		return ProtectionResult{
			Blocked:    true,
			Reason:     fmt.Sprintf("Overtrading: %d trades in last hour (max %d)", len(p.tradeTimes), p.MaxTradesPerHour),
			ResumeTime: resumeTime,
		}
	}

	return ProtectionResult{Blocked: false}
}

func (p *Overtrading) Validate() error {
	if p.MaxTradesPerHour <= 0 {
		return fmt.Errorf("Overtrading: max_trades_per_hour must be > 0")
	}
	return nil
}

func (p *Overtrading) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tradeTimes = make([]time.Time, 0)
}

// ── PriceJump ──────────────────────────────────────────────────
// Blocks trading after extreme price movements to avoid volatility.

type PriceJump struct {
	MaxJumpPct    float64 `json:"max_jump_pct"`    // max price change % to trigger block
	StopDuration  int     `json:"stop_duration"`   // minutes to stop after jump
	LookbackBars  int     `json:"lookback_bars"`   // bars to check for jump

	mu         sync.RWMutex
	lastJump   time.Time
}

func NewPriceJump(params map[string]any) (*PriceJump, error) {
	p := &PriceJump{}
	if v, ok := toFloat(params["max_jump_pct"]); ok {
		p.MaxJumpPct = v
	}
	if v, ok := toInt(params["stop_duration"]); ok {
		p.StopDuration = v
	}
	if v, ok := toInt(params["lookback_bars"]); ok {
		p.LookbackBars = v
	}
	if p.MaxJumpPct <= 0 {
		p.MaxJumpPct = 5.0
	}
	if p.StopDuration <= 0 {
		p.StopDuration = 15
	}
	if p.LookbackBars <= 0 {
		p.LookbackBars = 3
	}
	return p, nil
}

func (p *PriceJump) Name() string        { return "PriceJump" }
func (p *PriceJump) Description() string { return "Pause trading after extreme price movements" }

func (p *PriceJump) Check(ctx ProtectionContext) ProtectionResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if we're still in the stop period from a previous jump
	if !p.lastJump.IsZero() {
		resumeTime := p.lastJump.Add(time.Duration(p.StopDuration) * time.Minute)
		if ctx.CurrentTime.Before(resumeTime) {
			return ProtectionResult{
				Blocked:    true,
				Reason:     fmt.Sprintf("PriceJump: cooling down after extreme price movement"),
				ResumeTime: resumeTime,
			}
		}
		p.lastJump = time.Time{}
	}

	// Check trade history for large PnL % (proxy for price jump)
	for _, t := range ctx.TradeHistory {
		if math.Abs(t.PnLPct) >= p.MaxJumpPct {
			p.lastJump = ctx.CurrentTime
			resumeTime := ctx.CurrentTime.Add(time.Duration(p.StopDuration) * time.Minute)
			return ProtectionResult{
				Blocked:    true,
				Reason:     fmt.Sprintf("PriceJump: detected %.2f%% price jump (max %.2f%%)", t.PnLPct, p.MaxJumpPct),
				ResumeTime: resumeTime,
			}
		}
	}

	return ProtectionResult{Blocked: false}
}

func (p *PriceJump) Validate() error {
	if p.MaxJumpPct <= 0 {
		return fmt.Errorf("PriceJump: max_jump_pct must be > 0")
	}
	return nil
}

func (p *PriceJump) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastJump = time.Time{}
}
