// Package execution holds the execution-cost model shared by paper trading
// and backtesting. Keeping fee/slippage/partial-fill math in one place is what
// makes results comparable across paths; each path may supply different
// parameters, but the formulas must not diverge.
package execution

import "math"

// Config is the execution model parameters. Zero values are normalized to the
// defaults so a missing config does not silently produce free trading.
type Config struct {
	FeeRate         float64 // taker fee as a fraction of notional, e.g. 0.001
	Slippage        float64 // adverse price fraction, e.g. 0.0005
	PartialFillRate float64 // fraction of remaining qty fillable per touch, (0,1]
}

func Default() Config {
	return Config{FeeRate: 0.001, Slippage: 0.0005, PartialFillRate: 1.0}
}

// Normalize clamps invalid values: fees/slippage cannot be negative, and a
// partial-fill rate outside (0,1] would either never fill or overfill.
func Normalize(c Config) Config {
	if c.FeeRate < 0 {
		c.FeeRate = 0
	}
	if c.Slippage < 0 {
		c.Slippage = 0
	}
	if c.PartialFillRate <= 0 || c.PartialFillRate > 1 {
		c.PartialFillRate = 1
	}
	return c
}

// Fee returns the fee charged on a fill notional.
func Fee(notional float64, c Config) float64 {
	if notional <= 0 {
		return 0
	}
	c = Normalize(c)
	return math.Abs(notional) * c.FeeRate
}

// ApplySlippage moves the reference price adversely. direction follows the
// backtest convention: LONG/CLOSE buy-side semantics use "LONG", SHORT uses
// "SHORT"; anything else is treated as a sell. u must be in [0,1) and is
// supplied by the caller's RNG so results stay reproducible.
func ApplySlippage(price float64, direction string, c Config, u float64) float64 {
	if price <= 0 {
		return price
	}
	c = Normalize(c)
	if u < 0 {
		u = 0
	}
	if u >= 1 {
		u = 0.999999
	}
	switch direction {
	case "LONG":
		return price * (1 + c.Slippage + u*2*c.Slippage)
	case "SHORT", "CLOSE", "SELL":
		return price * (1 - c.Slippage - u*2*c.Slippage)
	default:
		return price
	}
}

// SplitFill returns the quantity filled on this touch. A rate of 1 fills the
// whole remaining quantity; lower rates simulate partial fills.
func SplitFill(remaining float64, c Config) float64 {
	if remaining <= 0 {
		return 0
	}
	c = Normalize(c)
	if c.PartialFillRate >= 1 {
		return remaining
	}
	return remaining * c.PartialFillRate
}

// FillPlan is the outcome of one limit-order touch.
type FillPlan struct {
	Fill   float64 // quantity filled this touch
	Fee    float64 // fee on this fill
	Filled float64 // cumulative filled quantity after this touch
	Full   bool    // whether the order is now complete
}

// PlanLimitFill applies the shared model to a limit-order touch. The fill price
// is the order's limit price (no slippage for resting liquidity), quantity is
// capped at the remaining size, and the fee is charged on this fill's notional.
func PlanLimitFill(price, quantity, filled float64, c Config) (FillPlan, bool) {
	if price <= 0 || quantity <= 0 {
		return FillPlan{}, false
	}
	remaining := quantity - filled
	fill := SplitFill(remaining, c)
	if fill <= 0 {
		return FillPlan{}, false
	}
	filledNow := filled + fill
	plan := FillPlan{Fill: fill, Fee: Fee(fill*price, c), Filled: filledNow}
	if filledNow >= quantity {
		plan.Filled = quantity
		plan.Full = true
	}
	return plan, true
}
