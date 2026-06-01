package portfolio

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── Portfolio Manager ──

// Manager manages multi-account portfolio state.
type Manager struct {
	accounts  map[string]*model.AccountData
	positions map[string]*model.PositionData // positionID -> position
	snapshots []model.PortfolioSnapshot
	peak      float64
	mu        sync.RWMutex

	// Callbacks
	OnPositionUpdate func(pos model.PositionData)
}

var (
	pmgr     *Manager
	pmgrOnce sync.Once
)

// GetManager returns the global portfolio manager.
func GetManager() *Manager {
	pmgrOnce.Do(func() {
		pmgr = NewManager()
	})
	return pmgr
}

// NewManager creates a new portfolio manager.
func NewManager() *Manager {
	m := &Manager{
		accounts:  make(map[string]*model.AccountData),
		positions: make(map[string]*model.PositionData),
	}
	m.accounts["default"] = &model.AccountData{
		ID:       "default",
		Exchange: "paper",
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 100000, Free: 100000, Used: 0},
		},
		Positions: make(map[string]*model.PositionData),
		CreatedAt: time.Now().UnixMilli(),
	}
	m.peak = 100000
	return m
}

// GetAccount returns an account by ID.
func (m *Manager) GetAccount(id string) *model.AccountData {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.accounts[id]
}

// UpdateBalance updates an account's balance.
func (m *Manager) UpdateBalance(accountID, currency string, total, free, used float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	acct := m.accounts[accountID]
	if acct == nil {
		return
	}
	acct.Balances[currency] = &model.Balance{
		Currency: currency,
		Total:    total,
		Free:     free,
		Used:     used,
	}
}

// UpdatePosition updates or creates a position.
func (m *Manager) UpdatePosition(pos model.PositionData) {
	m.mu.Lock()
	m.positions[pos.ID] = &pos
	if acct, ok := m.accounts["default"]; ok {
		acct.Positions[pos.ID] = &pos
	}
	m.mu.Unlock()

	if m.OnPositionUpdate != nil {
		m.OnPositionUpdate(pos)
	}
}

// RemovePosition removes a closed position.
func (m *Manager) RemovePosition(positionID string) {
	m.mu.Lock()
	delete(m.positions, positionID)
	if acct, ok := m.accounts["default"]; ok {
		delete(acct.Positions, positionID)
	}
	m.mu.Unlock()
}

// GetPositions returns all open positions.
func (m *Manager) GetPositions() []*model.PositionData {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*model.PositionData, 0, len(m.positions))
	for _, pos := range m.positions {
		copy_ := *pos
		result = append(result, &copy_)
	}
	return result
}

// ── Equity & KPIs ──

// TotalEquity calculates the total portfolio equity.
func (m *Manager) TotalEquity() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	equity := 0.0
	for _, acct := range m.accounts {
		for _, bal := range acct.Balances {
			equity += bal.Total
		}
	}
	for _, pos := range m.positions {
		equity += pos.UnrealizedPnL
	}
	return equity
}

// AvailableBalance returns the sum of free balances.
func (m *Manager) AvailableBalance() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := 0.0
	for _, acct := range m.accounts {
		for _, bal := range acct.Balances {
			total += bal.Free
		}
	}
	return total
}

// MarginUsed returns the sum of used balances.
func (m *Manager) MarginUsed() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := 0.0
	for _, acct := range m.accounts {
		for _, bal := range acct.Balances {
			total += bal.Used
		}
	}
	return total
}

// Drawdown calculates the current drawdown from peak equity.
func (m *Manager) Drawdown() float64 {
	equity := m.TotalEquity()
	m.mu.Lock()
	if equity > m.peak {
		m.peak = equity
	}
	peak := m.peak
	m.mu.Unlock()

	if peak <= 0 {
		return 0
	}
	return (peak - equity) / peak * 100
}

// TotalPnL returns the sum of all unrealized PnL across positions.
func (m *Manager) TotalPnL() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := 0.0
	for _, pos := range m.positions {
		total += pos.UnrealizedPnL
	}
	return total
}

// NetExposure calculates total exposure as a percentage of equity.
func (m *Manager) NetExposure() float64 {
	equity := m.TotalEquity()
	if equity <= 0 {
		return 0
	}
	totalExposure := 0.0
	m.mu.RLock()
	for _, pos := range m.positions {
		totalExposure += math.Abs(pos.Quantity * pos.CurrentPrice)
	}
	m.mu.RUnlock()
	return totalExposure / equity * 100
}

// Snapshot records an equity snapshot.
func (m *Manager) Snapshot() {
	snap := model.PortfolioSnapshot{
		TotalEquity:      m.TotalEquity(),
		AvailableBalance: m.AvailableBalance(),
		MarginUsed:       m.MarginUsed(),
		Drawdown:         m.Drawdown(),
		NetExposure:      m.NetExposure(),
		Positions:        m.GetPositions(),
		Timestamp:        time.Now().UnixMilli(),
	}

	m.mu.Lock()
	m.snapshots = append(m.snapshots, snap)
	if len(m.snapshots) > 5000 {
		m.snapshots = m.snapshots[len(m.snapshots)-5000:]
	}
	m.mu.Unlock()
}

// GetSnapshots returns equity history.
func (m *Manager) GetSnapshots() []model.PortfolioSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]model.PortfolioSnapshot, len(m.snapshots))
	copy(result, m.snapshots)
	return result
}

// GetLatestSnapshot returns the most recent snapshot.
func (m *Manager) GetLatestSnapshot() *model.PortfolioSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.snapshots) == 0 {
		return nil
	}
	s := m.snapshots[len(m.snapshots)-1]
	return &s
}

// ── Position Sizing ──

// FixedFraction returns a position size based on a percentage of equity.
func FixedFraction(equity float64, fraction float64) float64 {
	return equity * fraction
}

// KellyCriterion calculates position size using the Kelly formula.
// Uses half-Kelly by default for safety.
func KellyCriterion(winRate, avgWin, avgLoss float64, halfKelly bool) float64 {
	if avgLoss <= 0 {
		return 0
	}
	b := avgWin / avgLoss
	p := winRate
	q := 1 - p
	kelly := (p*b - q) / b
	if kelly <= 0 {
		return 0
	}
	if halfKelly {
		kelly /= 2
	}
	return kelly
}

// RiskBudget sizes a position based on max risk per trade and stop-loss distance.
func RiskBudget(equity, riskPct, stopLossPct float64) float64 {
	riskAmount := equity * riskPct
	if stopLossPct <= 0 {
		return 0
	}
	return riskAmount / stopLossPct
}

// EqualWeight divides equity equally among N positions.
func EqualWeight(equity float64, numPositions int) float64 {
	if numPositions <= 0 {
		return 0
	}
	return equity / float64(numPositions)
}

// VolatilityAdjusted scales position size inversely with volatility.
func VolatilityAdjusted(baseSize, volatility, targetVol float64) float64 {
	if volatility <= 0 {
		return baseSize
	}
	adjustment := targetVol / volatility
	adjusted := baseSize * adjustment
	// Clamp to [0.1x, 2x] of base size
	if adjusted < baseSize*0.1 {
		adjusted = baseSize * 0.1
	}
	if adjusted > baseSize*2 {
		adjusted = baseSize * 2
	}
	return adjusted
}

// ── Monitor ──

// Monitor provides periodic portfolio health checks.
type Monitor struct {
	manager        *Manager
	lastCheck      time.Time
	checkInterval  time.Duration
	dailyLossPct   float64
	maxConsecutive int
	tradeResults   []float64 // positive = win, negative = loss

	OnAlert func(level, message string)
	mu      sync.Mutex
}

func NewMonitor(mgr *Manager) *Monitor {
	return &Monitor{
		manager:        mgr,
		checkInterval:  30 * time.Second,
		dailyLossPct:   5.0,
		maxConsecutive: 5,
	}
}

// Check performs a health check on the portfolio.
func (mon *Monitor) Check() []string {
	mon.mu.Lock()
	defer mon.mu.Unlock()

	var alerts []string
	now := time.Now()

	// Check drawdown
	dd := mon.manager.Drawdown()
	if dd > mon.dailyLossPct {
		alerts = append(alerts, "Daily loss exceeded")
	}

	// Check consecutive losses
	if len(mon.tradeResults) >= mon.maxConsecutive {
		recent := mon.tradeResults[len(mon.tradeResults)-mon.maxConsecutive:]
		allLoss := true
		for _, r := range recent {
			if r >= 0 {
				allLoss = false
				break
			}
		}
		if allLoss {
			alerts = append(alerts, "Consecutive losses detected")
		}
	}

	mon.lastCheck = now
	return alerts
}

// RecordTrade records a trade result for monitoring.
func (mon *Monitor) RecordTrade(pnl float64) {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	mon.tradeResults = append(mon.tradeResults, pnl)
	if len(mon.tradeResults) > 100 {
		mon.tradeResults = mon.tradeResults[len(mon.tradeResults)-100:]
	}
}

// ── KPIs ──

// SharpeRatio calculates the annualized Sharpe ratio from a series of returns.
func SharpeRatio(returns []float64, riskFreeRate float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	avg := 0.0
	for _, r := range returns {
		avg += r
	}
	avg /= float64(len(returns))

	variance := 0.0
	for _, r := range returns {
		variance += (r - avg) * (r - avg)
	}
	std := math.Sqrt(variance / float64(len(returns)-1))
	if std == 0 {
		return 0
	}

	return (avg - riskFreeRate) / std * math.Sqrt(252) // Annualized
}

// SortinoRatio calculates the Sortino ratio (downside deviation only).
func SortinoRatio(returns []float64, riskFreeRate float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	avg := 0.0
	for _, r := range returns {
		avg += r
	}
	avg /= float64(len(returns))

	downVar := 0.0
	count := 0
	for _, r := range returns {
		if r < 0 {
			downVar += r * r
			count++
		}
	}
	if count <= 1 || downVar == 0 {
		return 0
	}
	downStd := math.Sqrt(downVar / float64(count-1))
	return (avg - riskFreeRate) / downStd * math.Sqrt(252)
}

// MaxDrawdownPct calculates maximum drawdown from equity curve.
func MaxDrawdownPct(equity []float64) float64 {
	if len(equity) == 0 {
		return 0
	}
	peak := equity[0]
	maxDD := 0.0
	for _, eq := range equity {
		if eq > peak {
			peak = eq
		}
		dd := (peak - eq) / peak * 100
		if dd > maxDD {
			maxDD = dd
		}
	}
	return maxDD
}

// WinRate calculates the win rate from PnL results.
func WinRate(pnls []float64) float64 {
	if len(pnls) == 0 {
		return 0
	}
	wins := 0
	for _, p := range pnls {
		if p > 0 {
			wins++
		}
	}
	return float64(wins) / float64(len(pnls)) * 100
}

// CalmarRatio calculates return / max drawdown.
func CalmarRatio(totalReturn, maxDD float64) float64 {
	if maxDD == 0 {
		return 0
	}
	return totalReturn / maxDD
}

// ── Sizing Helpers ──

// Sizer provides convenient position sizing calculations.
type Sizer struct {
	Equity      float64
	RiskPct     float64
	StopLossPct float64
}

func NewSizer(equity float64) *Sizer {
	return &Sizer{
		Equity:      equity,
		RiskPct:     0.02,
		StopLossPct: 0.05,
	}
}

func (s *Sizer) FixedFrac(fraction float64) float64 {
	return FixedFraction(s.Equity, fraction)
}

func (s *Sizer) Kelly(winRate, avgWin, avgLoss float64) float64 {
	return KellyCriterion(winRate, avgWin, avgLoss, true)
}

func (s *Sizer) RiskBudget_() float64 {
	return RiskBudget(s.Equity, s.RiskPct, s.StopLossPct)
}

func (s *Sizer) EqualWeight_(n int) float64 {
	return EqualWeight(s.Equity, n)
}

// ── Convenience: Parse returns from snapshots ──

// ReturnsFromSnapshots extracts period returns from equity snapshots.
func ReturnsFromSnapshots(snapshots []model.PortfolioSnapshot) []float64 {
	if len(snapshots) < 2 {
		return nil
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Timestamp < snapshots[j].Timestamp
	})
	returns := make([]float64, len(snapshots)-1)
	for i := 1; i < len(snapshots); i++ {
		prev := snapshots[i-1].TotalEquity
		if prev > 0 {
			returns[i-1] = (snapshots[i].TotalEquity - prev) / prev
		}
	}
	return returns
}
