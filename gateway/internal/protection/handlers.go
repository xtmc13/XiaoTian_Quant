package protection

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// ── CooldownPeriod ─────────────────────────────────────────────
// Prevents entering a new trade on a pair immediately after closing one.
// This gives the pair time to "cool down" and avoids overtrading.
// 对标 freqtrade CooldownPeriod：以该对最近平仓时间为锚点，
// 锁定时长支持 stop_duration_candles × timeframe 或 stop_duration（分钟），
// 也支持 unlock_at（"HH:MM"）固定时刻解锁。

type CooldownPeriod struct {
	windowParams

	mu          sync.RWMutex
	lastExits   map[string]time.Time // symbol -> last exit time
}

func NewCooldownPeriod(params map[string]any) (*CooldownPeriod, error) {
	p := &CooldownPeriod{
		lastExits: make(map[string]time.Time),
	}
	p.windowParams = parseWindowParams(params, 5, 5, "1h")
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

	resumeTime := p.lockEnd(lastExit)
	if ctx.CurrentTime.Before(resumeTime) {
		return ProtectionResult{
			Blocked:    true,
			Reason:     fmt.Sprintf("CooldownPeriod: %s cooling down (%s)", ctx.Symbol, p.describeWindow()),
			ResumeTime: resumeTime,
			Pair:       ctx.Symbol,
		}
	}

	return ProtectionResult{Blocked: false}
}

func (p *CooldownPeriod) describeWindow() string {
	if p.UnlockAt != "" {
		return fmt.Sprintf("until %s", p.UnlockAt)
	}
	if p.StopDurationCandles > 0 {
		return fmt.Sprintf("%d %s candles", p.StopDurationCandles, p.Timeframe)
	}
	return fmt.Sprintf("%d minutes", p.StopDurationMinutes)
}

func (p *CooldownPeriod) Validate() error {
	if p.StopDurationCandles <= 0 && p.StopDurationMinutes <= 0 && p.UnlockAt == "" {
		return fmt.Errorf("CooldownPeriod: stop_duration_candles / stop_duration / unlock_at 至少一项有效")
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

// ── StoplossGuard ──────────────────────────────────────────────
// 对标 freqtrade StoplossGuard：
// 在回溯窗口内出现 trade_limit 次止损（盈利 < required_profit 才计入）后，
// 锁定交易至「最近一次止损时间 + stop_duration」（或 unlock_at 固定时刻）。
//   - only_per_pair=false：全局检查与单对检查同时进行（全局触发锁全局，单对触发锁该对）
//   - only_per_pair=true：仅关闭全局检查，单对检查始终生效
//   - only_per_side=true：仅计入与当前信号同方向的止损
// 锁定期满后自动解锁（锚点固定，不会像滑动窗口一样无限续期）。

type StoplossGuard struct {
	windowParams

	TradeLimit     int     `json:"trade_limit"`      // 窗口内允许的止损次数上限
	OnlyPerPair    bool    `json:"only_per_pair"`    // true = 关闭全局检查（单对检查始终生效）
	OnlyPerSide    bool    `json:"only_per_side"`    // true = 只统计与当前信号同方向的止损
	RequiredProfit float64 `json:"required_profit"` // 盈利率 >= 此值的"止损"不计入（如移动止盈）

	mu            sync.RWMutex
	stoplosses    []stoplossRecord            // global stoploss history
	pairStoplosses map[string][]stoplossRecord // per-pair stoploss history
}

type stoplossRecord struct {
	Symbol string    `json:"symbol"`
	Time   time.Time `json:"time"`
	Profit float64   `json:"profit"` // 盈利率（ratio，如 -0.05）
	Side   string    `json:"side"`   // "LONG"/"SHORT"，空 = 未知
}

func NewStoplossGuard(params map[string]any) (*StoplossGuard, error) {
	p := &StoplossGuard{
		stoplosses:     make([]stoplossRecord, 0),
		pairStoplosses: make(map[string][]stoplossRecord),
	}
	p.windowParams = parseWindowParams(params, 24, 12, "1h")
	if v, ok := toInt(params["trade_limit"]); ok {
		p.TradeLimit = v
	}
	if v, ok := params["only_per_pair"].(bool); ok {
		p.OnlyPerPair = v
	}
	if v, ok := params["only_per_side"].(bool); ok {
		p.OnlyPerSide = v
	}
	if v, ok := toFloat(params["required_profit"]); ok {
		p.RequiredProfit = v
	}

	if p.TradeLimit <= 0 {
		p.TradeLimit = 4
	}
	return p, nil
}

func (p *StoplossGuard) Name() string        { return "StoplossGuard" }
func (p *StoplossGuard) Description() string { return "Stop trading after N stoplosses in a window" }

func (p *StoplossGuard) Check(ctx ProtectionContext) ProtectionResult {
	p.mu.RLock()
	defer p.mu.RUnlock()

	cutoff := ctx.CurrentTime.Add(-p.lookback())

	// 全局检查（only_per_pair=true 时关闭，与 freqtrade 一致）
	if !p.OnlyPerPair {
		count, last := p.matchCount(p.stoplosses, cutoff, ctx.Side)
		if count >= p.TradeLimit {
			resumeTime := p.lockEnd(last)
			if ctx.CurrentTime.Before(resumeTime) {
				return ProtectionResult{
					Blocked:    true,
					Reason:     fmt.Sprintf("StoplossGuard: %d stoplosses in lookback window, locking all pairs until %s", count, resumeTime.Format(time.RFC3339)),
					ResumeTime: resumeTime,
				}
			}
		}
	}

	// 单对检查（始终生效）
	count, last := p.matchCount(p.pairStoplosses[ctx.Symbol], cutoff, ctx.Side)
	if count >= p.TradeLimit {
		resumeTime := p.lockEnd(last)
		if ctx.CurrentTime.Before(resumeTime) {
			return ProtectionResult{
				Blocked:    true,
				Reason:     fmt.Sprintf("StoplossGuard: %d stoplosses on %s in lookback window, locking pair until %s", count, ctx.Symbol, resumeTime.Format(time.RFC3339)),
				ResumeTime: resumeTime,
				Pair:       ctx.Symbol,
			}
		}
	}

	return ProtectionResult{Blocked: false}
}

// matchCount 统计窗口内符合条件的止损：时间在 cutoff 之后、
// 盈利 < required_profit、方向匹配（only_per_side 时）。
// 返回数量与最近一次止损时间（锁定锚点）。
func (p *StoplossGuard) matchCount(records []stoplossRecord, cutoff time.Time, side string) (int, time.Time) {
	count := 0
	var last time.Time
	for _, r := range records {
		if !r.Time.After(cutoff) {
			continue
		}
		if r.Profit >= p.RequiredProfit {
			continue
		}
		if p.OnlyPerSide && side != "" && r.Side != "" && r.Side != side {
			continue
		}
		count++
		if r.Time.After(last) {
			last = r.Time
		}
	}
	return count, last
}

func (p *StoplossGuard) Validate() error {
	if p.LookbackPeriodCandles <= 0 && p.LookbackPeriodMinutes <= 0 {
		return fmt.Errorf("StoplossGuard: lookback_period_candles / lookback_period 至少一项 > 0")
	}
	if p.TradeLimit <= 0 {
		return fmt.Errorf("StoplossGuard: trade_limit must be > 0")
	}
	if p.StopDurationCandles <= 0 && p.StopDurationMinutes <= 0 && p.UnlockAt == "" {
		return fmt.Errorf("StoplossGuard: stop_duration_candles / stop_duration / unlock_at 至少一项有效")
	}
	return nil
}

func (p *StoplossGuard) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stoplosses = p.stoplosses[:0]
	p.pairStoplosses = make(map[string][]stoplossRecord)
}

// RecordStoploss records a stoploss event（盈利未知，始终计入，保持旧行为）。
func (p *StoplossGuard) RecordStoploss(symbol string, t time.Time) {
	p.RecordStoplossDetail(symbol, t, math.Inf(-1), "")
}

// RecordStoplossDetail records a stoploss with profit ratio and side
// （对标 freqtrade：profit >= required_profit 的"止损"不计入，如移动止盈）。
func (p *StoplossGuard) RecordStoplossDetail(symbol string, t time.Time, profit float64, side string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	rec := stoplossRecord{Symbol: symbol, Time: t, Profit: profit, Side: side}
	p.stoplosses = append(p.stoplosses, rec)

	if p.pairStoplosses[symbol] == nil {
		p.pairStoplosses[symbol] = make([]stoplossRecord, 0)
	}
	p.pairStoplosses[symbol] = append(p.pairStoplosses[symbol], rec)

	// Cleanup old records
	p.cleanup(t.Add(-p.lookback() * 2))
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

// ── MaxDrawdown ────────────────────────────────────────────────
// 对标 freqtrade MaxDrawdownProtection：账户级最大回撤超过阈值后，
// 全局停止开新仓，锁定至「窗口内最近一笔平仓 + stop_duration」（或 unlock_at）。
//
// 回撤计算（calculation_mode）：
//   - "ratios"（默认）：窗口内交易按平仓时间排序，累计盈利率曲线的
//     峰谷落差（freqtrade legacy ratios 口径）
//   - "equity"：窗口起始权益 = ctx.TotalBalance - 窗口累计盈亏，
//     回撤 = max((峰值权益 - 权益) / 峰值权益)
//
// trade_limit：窗口内交易数不足时不触发（freqtrade 默认 1）。
// ctx.TradeHistory 为空时回退到旧行为：直接使用外部计算的 ctx.CurrentDrawdown。

type MaxDrawdown struct {
	windowParams

	MaxDrawdownPct float64 `json:"max_drawdown_pct"` // 最大允许回撤（0-1；alias: max_allowed_drawdown）
	TradeLimit     int     `json:"trade_limit"`      // 窗口内至少 N 笔交易才评估
	CalculationMode string `json:"calculation_mode"` // "ratios" | "equity"
}

func NewMaxDrawdown(params map[string]any) (*MaxDrawdown, error) {
	p := &MaxDrawdown{}
	p.windowParams = parseWindowParams(params, 48, 12, "1h")
	if v, ok := toFloat(params["max_drawdown_pct"]); ok {
		p.MaxDrawdownPct = v
	} else if v, ok := toFloat(params["max_allowed_drawdown"]); ok {
		p.MaxDrawdownPct = v
	}
	if v, ok := toInt(params["trade_limit"]); ok {
		p.TradeLimit = v
	}
	if v, ok := params["calculation_mode"].(string); ok && v != "" {
		p.CalculationMode = v
	}

	if p.MaxDrawdownPct <= 0 {
		p.MaxDrawdownPct = 0.20 // 20% default
	}
	if p.TradeLimit <= 0 {
		p.TradeLimit = 1
	}
	if p.CalculationMode != "equity" && p.CalculationMode != "ratios" {
		p.CalculationMode = "ratios"
	}
	return p, nil
}

func (p *MaxDrawdown) Name() string        { return "MaxDrawdown" }
func (p *MaxDrawdown) Description() string { return "Stop opening new trades if account drawdown exceeds threshold" }

func (p *MaxDrawdown) Check(ctx ProtectionContext) ProtectionResult {
	cutoff := ctx.CurrentTime.Add(-p.lookback())

	// 窗口内交易（按平仓时间升序）
	windowTrades := make([]TradeRecord, 0, len(ctx.TradeHistory))
	for _, t := range ctx.TradeHistory {
		if t.ExitTime.After(cutoff) {
			windowTrades = append(windowTrades, t)
		}
	}
	sort.Slice(windowTrades, func(i, j int) bool { return windowTrades[i].ExitTime.Before(windowTrades[j].ExitTime) })

	if len(windowTrades) > 0 {
		// freqtrade 语义：窗口内交易数不足 trade_limit 时不触发
		if len(windowTrades) < p.TradeLimit {
			return ProtectionResult{Blocked: false}
		}
		var dd float64
		if p.CalculationMode == "equity" {
			dd = equityDrawdown(windowTrades, ctx.TotalBalance)
		} else {
			dd = ratioDrawdown(windowTrades)
		}
		if dd > p.MaxDrawdownPct {
			resumeTime := p.lockEnd(windowTrades[len(windowTrades)-1].ExitTime)
			if ctx.CurrentTime.Before(resumeTime) {
				return ProtectionResult{
					Blocked:    true,
					Reason:     fmt.Sprintf("MaxDrawdown: drawdown %.2f%% exceeds limit %.2f%% (%s mode, %d trades), locking until %s", dd*100, p.MaxDrawdownPct*100, p.CalculationMode, len(windowTrades), resumeTime.Format(time.RFC3339)),
					ResumeTime: resumeTime,
				}
			}
		}
		return ProtectionResult{Blocked: false}
	}

	// 无交易历史：回退到外部提供的回撤值（兼容旧行为）
	if ctx.CurrentDrawdown >= p.MaxDrawdownPct {
		resumeTime := p.lockEnd(ctx.CurrentTime)
		return ProtectionResult{
			Blocked:    true,
			Reason:     fmt.Sprintf("MaxDrawdown: drawdown %.2f%% exceeds limit %.2f%%", ctx.CurrentDrawdown*100, p.MaxDrawdownPct*100),
			ResumeTime: resumeTime,
		}
	}
	return ProtectionResult{Blocked: false}
}

// tradeProfitRatio 返回单笔交易盈利率（PnLPct 为 ratio 口径；
// 缺失时由 PnL / 名义价值推导）。
func tradeProfitRatio(t TradeRecord) float64 {
	if t.PnLPct != 0 {
		return t.PnLPct
	}
	notional := t.EntryPrice * t.Quantity
	if notional > 0 {
		return t.PnL / notional
	}
	return 0
}

// ratioDrawdown 计算累计盈利率曲线的峰谷落差（freqtrade legacy ratios 口径）。
func ratioDrawdown(trades []TradeRecord) float64 {
	cum := 0.0
	peak := 0.0
	maxDD := 0.0
	for _, t := range trades {
		cum += tradeProfitRatio(t)
		if cum > peak {
			peak = cum
		}
		if dd := peak - cum; dd > maxDD {
			maxDD = dd
		}
	}
	return maxDD
}

// equityDrawdown 计算权益曲线的相对回撤。
// 窗口起始权益 = 当前总权益 - 窗口内累计盈亏。
func equityDrawdown(trades []TradeRecord, totalBalance float64) float64 {
	sumPnL := 0.0
	for _, t := range trades {
		sumPnL += t.PnL
	}
	startBalance := totalBalance - sumPnL
	if startBalance <= 0 {
		return 0
	}
	equity := startBalance
	peak := startBalance
	maxDD := 0.0
	for _, t := range trades {
		equity += t.PnL
		if equity > peak {
			peak = equity
		}
		if peak > 0 {
			if dd := (peak - equity) / peak; dd > maxDD {
				maxDD = dd
			}
		}
	}
	return maxDD
}

func (p *MaxDrawdown) Validate() error {
	if p.MaxDrawdownPct <= 0 || p.MaxDrawdownPct >= 1 {
		return fmt.Errorf("MaxDrawdown: max_drawdown_pct must be between 0 and 1")
	}
	if p.CalculationMode != "ratios" && p.CalculationMode != "equity" {
		return fmt.Errorf("MaxDrawdown: calculation_mode must be 'ratios' or 'equity'")
	}
	return nil
}

func (p *MaxDrawdown) Reset() {}

// ── LowProfitPairs ─────────────────────────────────────────────
// 对标 freqtrade LowProfitPairs：窗口内某交易对的累计盈利率
// （各笔交易盈利率之和，ratio 口径）低于 min_profit_ratio 时锁定该对，
// 锁定至「窗口内最近一笔平仓 + stop_duration」（或 unlock_at）。
//   - min_trade_count（freqtrade trade_limit）：窗口内交易数不足时不评估
//   - only_per_side：仅统计与当前信号同方向的交易

type LowProfitPairs struct {
	windowParams

	MinProfitRatio float64 `json:"min_profit_ratio"` // 最低累计盈利率（alias: required_profit）
	MinTradeCount  int     `json:"min_trade_count"`  // 窗口内至少 N 笔交易才评估（alias: trade_limit）
	OnlyPerSide    bool    `json:"only_per_side"`

	mu          sync.RWMutex
	tradeHistory map[string][]TradeRecord // per-pair trade history
}

func NewLowProfitPairs(params map[string]any) (*LowProfitPairs, error) {
	p := &LowProfitPairs{
		tradeHistory: make(map[string][]TradeRecord),
	}
	p.windowParams = parseWindowParams(params, 24, 12, "1h")
	if v, ok := toFloat(params["min_profit_ratio"]); ok {
		p.MinProfitRatio = v
	} else if v, ok := toFloat(params["required_profit"]); ok {
		p.MinProfitRatio = v
	}
	if v, ok := toInt(params["min_trade_count"]); ok {
		p.MinTradeCount = v
	} else if v, ok := toInt(params["trade_limit"]); ok {
		p.MinTradeCount = v
	}
	if v, ok := params["only_per_side"].(bool); ok {
		p.OnlyPerSide = v
	}

	if p.MinProfitRatio <= 0 {
		p.MinProfitRatio = 0.01 // 1%
	}
	if p.MinTradeCount <= 0 {
		p.MinTradeCount = 4
	}
	return p, nil
}

func (p *LowProfitPairs) Name() string        { return "LowProfitPairs" }
func (p *LowProfitPairs) Description() string { return "Lock pairs with low profit over a period" }

func (p *LowProfitPairs) Check(ctx ProtectionContext) ProtectionResult {
	p.mu.RLock()
	records := p.tradeHistory[ctx.Symbol]
	p.mu.RUnlock()

	cutoff := ctx.CurrentTime.Add(-p.lookback())

	// 窗口内、方向匹配的交易
	var windowTrades []TradeRecord
	for _, r := range records {
		if !r.ExitTime.After(cutoff) {
			continue
		}
		if p.OnlyPerSide && ctx.Side != "" && r.Side != "" && r.Side != ctx.Side {
			continue
		}
		windowTrades = append(windowTrades, r)
	}

	if len(windowTrades) < p.MinTradeCount {
		return ProtectionResult{Blocked: false}
	}

	// freqtrade 口径：窗口内各笔交易盈利率（ratio）求和
	profit := 0.0
	var lastExit time.Time
	for _, r := range windowTrades {
		profit += tradeProfitRatio(r)
		if r.ExitTime.After(lastExit) {
			lastExit = r.ExitTime
		}
	}

	if profit < p.MinProfitRatio {
		resumeTime := p.lockEnd(lastExit)
		if ctx.CurrentTime.Before(resumeTime) {
			return ProtectionResult{
				Blocked:    true,
				Reason:     fmt.Sprintf("LowProfitPairs: %s profit %.4f < %.4f over %d trades, locking until %s", ctx.Symbol, profit, p.MinProfitRatio, len(windowTrades), resumeTime.Format(time.RFC3339)),
				ResumeTime: resumeTime,
				Pair:       ctx.Symbol,
			}
		}
	}

	return ProtectionResult{Blocked: false}
}

func (p *LowProfitPairs) Validate() error {
	if p.LookbackPeriodCandles <= 0 && p.LookbackPeriodMinutes <= 0 {
		return fmt.Errorf("LowProfitPairs: lookback_period_candles / lookback_period 至少一项 > 0")
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
	cutoff := trade.ExitTime.Add(-p.lookback() * 2)
	newRecords := make([]TradeRecord, 0)
	for _, r := range p.tradeHistory[trade.Symbol] {
		if r.ExitTime.After(cutoff) {
			newRecords = append(newRecords, r)
		}
	}
	p.tradeHistory[trade.Symbol] = newRecords
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
