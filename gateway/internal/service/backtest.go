package service

import (
	"math"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// BacktestService runs strategy backtests.
type BacktestService struct {
	mu sync.Mutex
}

// BacktestConfig holds parameters for a backtest run.
type BacktestConfig struct {
	Symbol         string  `json:"symbol"`
	Interval       string  `json:"interval"`
	NumBars        int     `json:"num_bars"`
	InitialBalance float64 `json:"initial_balance"`
	FeeRate        float64 `json:"fee_rate"`
	Slippage       float64 `json:"slippage"`
	PositionSize   float64 `json:"position_size"`
}

// BacktestResult holds the results of a backtest.
type BacktestResult struct {
	InitialBalance  float64              `json:"initial_balance"`
	FinalEquity     float64              `json:"final_equity"`
	TotalReturnPct  float64              `json:"total_return_pct"`
	MaxDrawdownPct  float64              `json:"max_drawdown_pct"`
	SharpeRatio     float64              `json:"sharpe_ratio"`
	WinRatePct      float64              `json:"win_rate_pct"`
	ProfitFactor    float64              `json:"profit_factor"`
	TotalTrades     int                  `json:"total_trades"`
	EquityCurve     []map[string]float64 `json:"equity_curve"`
	Trades          []map[string]any     `json:"trades"`
	Simulated       bool                 `json:"simulated"` // true if using generated data (no real klines)
}

var (
	btSvc     *BacktestService
	btSvcOnce sync.Once
)

// GetBacktestService returns the singleton backtest service.
func GetBacktestService() *BacktestService {
	btSvcOnce.Do(func() {
		btSvc = &BacktestService{}
	})
	return btSvc
}

// Run runs a backtest with the given config and price data.
func (bs *BacktestService) Run(config BacktestConfig, klines []map[string]any) *BacktestResult {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if config.NumBars <= 0 {
		config.NumBars = 500
	}
	if config.InitialBalance <= 0 {
		config.InitialBalance = 100000.0
	}
	if config.FeeRate <= 0 {
		config.FeeRate = 0.001
	}
	if config.Slippage <= 0 {
		config.Slippage = 0.0005
	}
	if config.PositionSize <= 0 {
		config.PositionSize = 1.0
	}

	// Use provided klines or generate simulated prices
	prices := make([]float64, config.NumBars)
	times := make([]int64, config.NumBars)
	simulated := false
	if len(klines) >= config.NumBars {
		for i, k := range klines[:config.NumBars] {
			prices[i] = k["close"].(float64)
			times[i] = int64(time.Now().UnixMilli()) - int64((config.NumBars-i)*60000)
		}
	} else {
		// Generate linear drift as visual placeholder (results will be marked as simulated)
		simulated = true
		basePrice := 68000.0
		for i := 0; i < config.NumBars; i++ {
			drift := float64(i) * basePrice * 0.00005
			prices[i] = basePrice + drift
			times[i] = int64(time.Now().UnixMilli()) - int64((config.NumBars-i)*60000)
		}
	}

	// Run simulated backtest
	balance := config.InitialBalance
	position := 0.0
	equityCurve := make([]map[string]float64, 0)
	trades := make([]map[string]any, 0)
	peak := balance

	for i := 0; i < len(prices); i++ {
		price := prices[i]
		qty := 0.01 * config.PositionSize

		// Simple mean-reversion strategy: buy every 20 bars
		if i > 5 && i%20 == 0 && balance >= price*qty {
			cost := price * qty * (1 + config.FeeRate + config.Slippage)
			if balance >= cost {
				balance -= cost
				position += qty
				trades = append(trades, map[string]any{
					"entry_price": price, "qty": qty, "side": "buy", "bar": i,
					"time": float64(times[i]),
				})
			}
		} else if position > 0 && i%20 == 15 {
			proceeds := price * position * (1 - config.FeeRate - config.Slippage)
			lastTrade := trades[len(trades)-1]
			entryPrice := lastTrade["entry_price"].(float64)
			pnl := (price - entryPrice) * position
			balance += proceeds
			trades[len(trades)-1]["exit_price"] = price
			trades[len(trades)-1]["pnl"] = pnl
			trades[len(trades)-1]["exit_time"] = float64(times[i])
			position = 0
		}

		eq := balance + position*price
		if eq > peak {
			peak = eq
		}
		equityCurve = append(equityCurve, map[string]float64{
			"time":  float64(times[i]),
			"equity": store.RoundFloat(eq, 2),
		})
	}

	// Final equity
	finalEquity := balance
	if position > 0 {
		finalEquity += position * prices[len(prices)-1]
	}

	// Calculate KPIs
	totalReturnPct := (finalEquity - config.InitialBalance) / config.InitialBalance * 100

	// Max drawdown
	peak2 := config.InitialBalance
	maxDD := 0.0
	for _, pt := range equityCurve {
		eq := pt["equity"]
		if eq > peak2 {
			peak2 = eq
		}
		dd := (peak2 - eq) / peak2 * 100
		if dd > maxDD {
			maxDD = dd
		}
	}

	// Win rate & profit factor
	winCount := 0
	lossCount := 0
	totalProfit := 0.0
	totalLoss := 0.0
	sellTrades := 0
	for _, t := range trades {
		if t["side"] == "buy" && t["exit_price"] != nil {
			sellTrades++
			pnl := t["pnl"].(float64)
			if pnl > 0 {
				winCount++
				totalProfit += pnl
			} else {
				lossCount++
				totalLoss -= pnl
			}
		}
	}

	winRate := 0.0
	if winCount+lossCount > 0 {
		winRate = float64(winCount) / float64(winCount+lossCount) * 100
	}

	profitFactor := 0.0
	if totalLoss > 0 {
		profitFactor = totalProfit / totalLoss
	} else if totalProfit > 0 {
		profitFactor = totalProfit
	}

	// Sharpe ratio (simplified)
	returns := make([]float64, 0)
	for i := 1; i < len(equityCurve); i++ {
		r := (equityCurve[i]["equity"] - equityCurve[i-1]["equity"]) / equityCurve[i-1]["equity"]
		returns = append(returns, r)
	}
	avgReturn := 0.0
	for _, r := range returns {
		avgReturn += r
	}
	if len(returns) > 0 {
		avgReturn /= float64(len(returns))
	}
	stdReturn := 0.0
	for _, r := range returns {
		stdReturn += (r - avgReturn) * (r - avgReturn)
	}
	if len(returns) > 1 {
		stdReturn = math.Sqrt(stdReturn / float64(len(returns)-1))
	}
	sharpe := 0.0
	if stdReturn > 0 {
		sharpe = avgReturn / stdReturn * math.Sqrt(float64(len(prices)))
	}

	return &BacktestResult{
		InitialBalance:  config.InitialBalance,
		FinalEquity:     store.RoundFloat(finalEquity, 2),
		TotalReturnPct:  store.RoundFloat(totalReturnPct, 2),
		MaxDrawdownPct:  store.RoundFloat(maxDD, 2),
		SharpeRatio:     store.RoundFloat(sharpe, 3),
		WinRatePct:      store.RoundFloat(winRate, 1),
		ProfitFactor:    store.RoundFloat(profitFactor, 2),
		TotalTrades:     sellTrades,
		EquityCurve:     equityCurve,
		Trades:          trades,
		Simulated:       simulated,
	}
}

// RunMultiple runs backtests for multiple symbol/timeframe combinations.
func (bs *BacktestService) RunMultiple(configs []BacktestConfig) []*BacktestResult {
	results := make([]*BacktestResult, len(configs))
	var wg sync.WaitGroup
	for i, cfg := range configs {
		wg.Add(1)
		go func(idx int, c BacktestConfig) {
			defer wg.Done()
			results[idx] = bs.Run(c, nil)
		}(i, cfg)
	}
	wg.Wait()
	return results
}
