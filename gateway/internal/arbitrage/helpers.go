package arbitrage

import (
	"strings"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// DepthMetrics is the exported alias of depthMetrics used by helper wrappers.
type DepthMetrics = depthMetrics

// Round rounds a float to the given number of decimal places.
func Round(v float64, places int) float64 { return round(v, places) }

// WinRate calculates the win rate percentage from wins and losses.
func WinRate(wins, losses int) float64 { return winRate(wins, losses) }

// SplitSymbol extracts base and quote assets from a trading pair.
func SplitSymbol(symbol string) (base, quote string) { return splitSymbol(symbol) }

// ExtractFreeBalance tries common balance field names used by exchanges.
func ExtractFreeBalance(balances []map[string]any, asset string) float64 {
	return extractFreeBalance(balances, asset)
}

// ToFloat safely converts an interface value to float64.
func ToFloat(v any) float64 { return toFloat(v) }

// ExtractPrice extracts the last price from common ticker response fields.
func ExtractPrice(ticker map[string]any) float64 { return extractPrice(ticker) }

// ComputeDepthMetrics walks the order books for exactly qty and returns detailed metrics.
func ComputeDepthMetrics(buyOB, sellOB model.OrderBookData, qty float64) DepthMetrics {
	return computeDepthMetrics(buyOB, sellOB, qty)
}

// CalculateDepthMetrics walks the order book to compute executable prices.
func CalculateDepthMetrics(buyOB, sellOB model.OrderBookData, targetBaseQty float64) (
	execBuy, execSell, buyDepth, sellDepth, slippageBuy, slippageSell, maxQty float64, viable bool,
) {
	return calculateDepthMetrics(buyOB, sellOB, targetBaseQty)
}

// ResolveQuantity decides the final executable quantity and its metrics.
// When adaptive is true it shrinks qty to fit depth and slippage limits.
// When false it acts as a hard gate: full fill + slippage limit required.
func ResolveQuantity(
	buyOB, sellOB model.OrderBookData,
	targetQty, buyPrice float64,
	adaptive bool,
	maxSlippagePct, minOrderQty, minOrderValue float64,
) (
	execQty, execBuy, execSell, buyDepth, sellDepth, slipBuy, slipSell, maxQty float64, viable bool,
) {
	cfg := EngineConfig{
		AdaptiveQtyEnabled: adaptive,
		MaxSlippagePct:     maxSlippagePct,
		MinOrderQty:        minOrderQty,
		MinOrderValue:      minOrderValue,
	}
	return resolveQuantity(buyOB, sellOB, targetQty, buyPrice, cfg)
}

// WalkDepth walks the order book for a single leg.
// For BUY, input amount is quote value; returns base quantity received and average price.
// For SELL, input amount is base quantity; returns quote value received and average price.
func WalkDepth(ob model.OrderBookData, side string, amount float64) (output, avgPrice, slippagePct float64, filled bool) {
	if amount <= 0 {
		return 0, 0, 0, false
	}
	side = strings.ToUpper(side)
	if side == "BUY" {
		return walkAsksByQuote(ob, amount)
	}
	return walkBidsByQty(ob, amount)
}

func walkAsksByQuote(ob model.OrderBookData, quoteAmount float64) (baseQty, avgPrice, slippagePct float64, filled bool) {
	if len(ob.Asks) == 0 || quoteAmount <= 0 {
		return 0, 0, 0, false
	}
	bestAsk := ob.Asks[0][0]
	if bestAsk <= 0 {
		return 0, 0, 0, false
	}
	var spent, received float64
	remaining := quoteAmount
	for _, ask := range ob.Asks {
		price, avail := ask[0], ask[1]
		if avail <= 0 || price <= 0 {
			continue
		}
		cost := price * avail
		var take float64
		if cost <= remaining {
			take = avail
			remaining -= cost
		} else {
			take = remaining / price
			remaining = 0
		}
		spent += price * take
		received += take
		if remaining <= 0 {
			break
		}
	}
	if spent <= 0 || received <= 0 {
		return 0, 0, 0, false
	}
	avgPrice = spent / received
	slippagePct = (avgPrice - bestAsk) / bestAsk * 100
	return received, avgPrice, slippagePct, remaining <= 0
}

func walkBidsByQty(ob model.OrderBookData, baseQty float64) (quoteValue, avgPrice, slippagePct float64, filled bool) {
	if len(ob.Bids) == 0 || baseQty <= 0 {
		return 0, 0, 0, false
	}
	bestBid := ob.Bids[0][0]
	if bestBid <= 0 {
		return 0, 0, 0, false
	}
	var received float64
	remaining := baseQty
	for _, bid := range ob.Bids {
		price, avail := bid[0], bid[1]
		if avail <= 0 || price <= 0 {
			continue
		}
		take := avail
		if remaining < take {
			take = remaining
		}
		received += price * take
		remaining -= take
		if remaining <= 0 {
			break
		}
	}
	if received <= 0 || (baseQty-remaining) <= 0 {
		return 0, 0, 0, false
	}
	avgPrice = received / (baseQty - remaining)
	slippagePct = (bestBid - avgPrice) / bestBid * 100
	return received, avgPrice, slippagePct, remaining <= 0
}
