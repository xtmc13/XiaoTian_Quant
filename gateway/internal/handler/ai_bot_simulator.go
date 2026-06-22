package handler

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Paper trading simulator for AI bots ──────────────────────────
//
// This simulator maintains in-memory paper positions for each running AI bot
// instance.  It generates synthetic price action (real market price + small
// random noise) and pseudo-random entry/exit signals.  When a position hits
// TP/SL or is closed by signal, a trade row is persisted to ai_bot_trades.
// Metrics (unrealized/realized PnL, total return, drawdown, sharpe, win rate,
// total trades) are computed from the trade history + open positions and
// returned to the snapshot worker so the DB stays authoritative.

// paperPosition represents an open simulated trade.
type paperPosition struct {
	Symbol     string
	Side       string // LONG / SHORT
	Qty        float64
	EntryPrice float64
	MarketPrice float64
	TPPrice    float64
	SLPrice    float64
	OpenedAt   int64
}

// paperBotState holds runtime simulation state for one bot instance.
type paperBotState struct {
	mu            sync.Mutex
	positions     []*paperPosition
	lastPrice     float64
	seedPrice     float64
	tradeCount    int
	winCount      int
	lossCount     int
	totalRealized float64
}

var (
	paperBots   = map[string]*paperBotState{}
	paperBotsMu sync.RWMutex
)

// getOrCreatePaperState returns the simulator state for a bot, rehydrating
// from DB trade history if needed.
func getOrCreatePaperState(botID string, symbol string, initialBalance float64) *paperBotState {
	paperBotsMu.Lock()
	defer paperBotsMu.Unlock()
	if s, ok := paperBots[botID]; ok {
		return s
	}
	s := &paperBotState{
		positions: []*paperPosition{},
		seedPrice: 0,
		lastPrice: 0,
	}
	// Rehydrate realized PnL and win/loss counts from closed trades.
	trades := store.GetAIBotTrades(botID, 10000)
	for _, t := range trades {
		pnl := getFloat(t, "pnl", 0)
		s.totalRealized += pnl
		s.tradeCount++
		if pnl > 0 {
			s.winCount++
		} else if pnl < 0 {
			s.lossCount++
		}
	}
	paperBots[botID] = s
	return s
}

// resetPaperState clears simulator state (used when a bot is stopped/reset).
func resetPaperState(botID string) {
	paperBotsMu.Lock()
	defer paperBotsMu.Unlock()
	delete(paperBots, botID)
}

// simulateAIBotStep runs one simulation tick for a running AI bot instance.
// It returns the updated metrics that should be persisted to the DB.
func simulateAIBotStep(inst map[string]any) (unrealized, realized, totalReturn, equity, maxDrawdown, sharpe, winRate float64, totalTrades int, err error) {
	id := getString(inst, "id", "")
	symbol := getString(inst, "symbol", "BTCUSDT")
	initialBalance := getFloat(inst, "initial_balance", 10000)
	if initialBalance <= 0 {
		initialBalance = 10000
	}

	// Parse config_json for simulation parameters.
	configJSON := getString(inst, "config_json", "{}")
	var cfg map[string]any
	if json.Unmarshal([]byte(configJSON), &cfg) != nil || cfg == nil {
		cfg = map[string]any{}
	}
	tpPct := getFloat(cfg, "take_profit_pct", 3.0)
	if tpPct <= 0 {
		tpPct = 3.0
	}
	slPct := getFloat(cfg, "stop_loss_pct", 2.0)
	if slPct <= 0 {
		slPct = 2.0
	}
	maxPositions := getInt(cfg, "max_positions", 1)
	if maxPositions <= 0 {
		maxPositions = 1
	}
	positionSizePct := getFloat(cfg, "position_size_pct", 10.0)
	if positionSizePct <= 0 {
		positionSizePct = 10.0
	}

	state := getOrCreatePaperState(id, symbol, initialBalance)
	state.mu.Lock()
	defer state.mu.Unlock()

	// 1. Determine current market price.
	marketPrice := fetchBinancePrice(symbol)
	if marketPrice <= 0 {
		// Fallback to synthetic random walk seeded by symbol hash.
		marketPrice = syntheticPrice(symbol, state.lastPrice)
	}
	if state.seedPrice == 0 {
		state.seedPrice = marketPrice
	}
	state.lastPrice = marketPrice

	// 2. Update open positions and check TP/SL.
	var remaining []*paperPosition
	for _, pos := range state.positions {
		pos.MarketPrice = marketPrice
		exitPrice, reason, closed := checkPositionExit(pos, tpPct, slPct)
		if closed {
			pnl, _ := closePosition(id, pos, exitPrice, reason)
			state.totalRealized += pnl
			state.tradeCount++
			if pnl > 0 {
				state.winCount++
			} else if pnl < 0 {
				state.lossCount++
			}
			continue
		}
		remaining = append(remaining, pos)
	}
	state.positions = remaining

	// 3. Possibly open a new position.
	if len(state.positions) < maxPositions {
		if shouldOpenPosition(state, symbol) {
			side := "LONG"
			if rand.Float64() > 0.55 {
				side = "SHORT"
			}
			qty := (initialBalance * positionSizePct / 100) / marketPrice
			if qty <= 0 {
				qty = 0.001
			}
			pos := openPosition(symbol, side, qty, marketPrice, tpPct, slPct)
			state.positions = append(state.positions, pos)
		}
	}

	// 4. Compute metrics.
	unrealized = 0
	for _, pos := range state.positions {
		unrealized += positionUnrealizedPnl(pos)
	}
	realized = state.totalRealized
	equity = initialBalance + unrealized + realized
	totalReturn = ((equity - initialBalance) / initialBalance) * 100
	totalTrades = state.tradeCount
	if state.tradeCount > 0 {
		winRate = float64(state.winCount) / float64(state.tradeCount) * 100
	}
	maxDrawdown = estimateMaxDrawdown(id, equity)
	sharpe = estimateSharpe(id, totalReturn)

	return
}

func checkPositionExit(pos *paperPosition, tpPct, slPct float64) (exitPrice float64, reason string, closed bool) {
	if pos.Side == "LONG" {
		if pos.MarketPrice >= pos.TPPrice {
			return pos.TPPrice, "tp", true
		}
		if pos.MarketPrice <= pos.SLPrice {
			return pos.SLPrice, "sl", true
		}
	} else {
		if pos.MarketPrice <= pos.TPPrice {
			return pos.TPPrice, "tp", true
		}
		if pos.MarketPrice >= pos.SLPrice {
			return pos.SLPrice, "sl", true
		}
	}
	return 0, "", false
}

func openPosition(symbol, side string, qty, price, tpPct, slPct float64) *paperPosition {
	var tp, sl float64
	if side == "LONG" {
		tp = price * (1 + tpPct/100)
		sl = price * (1 - slPct/100)
	} else {
		tp = price * (1 - tpPct/100)
		sl = price * (1 + slPct/100)
	}
	return &paperPosition{
		Symbol:      symbol,
		Side:        side,
		Qty:         qty,
		EntryPrice:  price,
		MarketPrice: price,
		TPPrice:     tp,
		SLPrice:     sl,
		OpenedAt:    time.Now().Unix(),
	}
}

func closePosition(botID string, pos *paperPosition, exitPrice float64, reason string) (pnl, pnlPct float64) {
	if pos.Side == "LONG" {
		pnl = (exitPrice - pos.EntryPrice) * pos.Qty
	} else {
		pnl = (pos.EntryPrice - exitPrice) * pos.Qty
	}
	if pos.EntryPrice > 0 {
		pnlPct = (pnl / (pos.EntryPrice * pos.Qty)) * 100
	}
	now := time.Now().Unix()
	store.SaveAIBotTrade(map[string]any{
		"bot_instance_id": botID,
		"symbol":          pos.Symbol,
		"side":            pos.Side,
		"entry_price":     pos.EntryPrice,
		"exit_price":      exitPrice,
		"quantity":        pos.Qty,
		"pnl":             pnl,
		"pnl_pct":         pnlPct,
		"tp_price":        pos.TPPrice,
		"sl_price":        pos.SLPrice,
		"close_reason":    reason,
		"opened_at":       pos.OpenedAt,
		"closed_at":       now,
	})
	return
}

func positionUnrealizedPnl(pos *paperPosition) float64 {
	if pos.Side == "LONG" {
		return (pos.MarketPrice - pos.EntryPrice) * pos.Qty
	}
	return (pos.EntryPrice - pos.MarketPrice) * pos.Qty
}

// shouldOpenPosition returns true pseudo-randomly; bots that already have
// positions are less likely to open more.
func shouldOpenPosition(state *paperBotState, symbol string) bool {
	// Deterministic but pseudo-random based on time and symbol.
	h := hashSymbol(symbol) + time.Now().Unix()
	r := rand.New(rand.NewSource(h))
	baseProb := 0.08
	if len(state.positions) > 0 {
		baseProb = 0.03
	}
	return r.Float64() < baseProb
}

// syntheticPrice produces a random walk around a seeded price when real market
// data is unavailable.
func syntheticPrice(symbol string, lastPrice float64) float64 {
	if lastPrice <= 0 {
		// Seed from symbol hash to keep the price stable across restarts.
		base := 10000.0 + float64(hashSymbol(symbol)%90000)
		return base
	}
	volatility := 0.002
	change := (rand.Float64()*2 - 1) * volatility
	return lastPrice * (1 + change)
}

func hashSymbol(s string) int64 {
	var h int64 = 5381
	for _, c := range s {
		h = ((h << 5) + h) + int64(c)
	}
	if h < 0 {
		return -h
	}
	return h
}

// estimateMaxDrawdown uses recent snapshots to compute max peak-to-trough drop.
func estimateMaxDrawdown(botID string, currentEquity float64) float64 {
	snaps := store.GetAIBotSnapshots(botID, 90)
	if len(snaps) == 0 {
		return 0
	}
	peak := currentEquity
	maxDD := 0.0
	for i := len(snaps) - 1; i >= 0; i-- {
		e := getFloat(snaps[i], "total_equity", 0)
		if e > peak {
			peak = e
		}
		if peak > 0 {
			dd := (peak - e) / peak * 100
			if dd > maxDD {
				maxDD = dd
			}
		}
	}
	return maxDD
}

// estimateSharpe approximates annualized Sharpe from snapshot returns.
func estimateSharpe(botID string, totalReturn float64) float64 {
	snaps := store.GetAIBotSnapshots(botID, 30)
	if len(snaps) < 2 {
		return 0
	}
	var returns []float64
	prev := getFloat(snaps[len(snaps)-1], "total_equity", 0)
	for i := len(snaps) - 2; i >= 0; i-- {
		cur := getFloat(snaps[i], "total_equity", 0)
		if prev > 0 {
			returns = append(returns, (cur-prev)/prev)
		}
		prev = cur
	}
	if len(returns) < 2 {
		return 0
	}
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))
	var variance float64
	for _, r := range returns {
		variance += math.Pow(r-mean, 2)
	}
	std := math.Sqrt(variance / float64(len(returns)))
	if std == 0 {
		return 0
	}
	// Annualized roughly assuming snapshot interval ~60s; 252 trading days.
	return (mean / std) * math.Sqrt(252*24*60)
}

// getInt helper (handler package may not have one for map[string]any).
func getInt(m map[string]any, key string, def int) int {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case int64:
			return int(val)
		case float64:
			return int(val)
		case string:
			var i int
			if _, err := fmt.Sscanf(val, "%d", &i); err == nil {
				return i
			}
		}
	}
	return def
}
