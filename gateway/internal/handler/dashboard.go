package handler

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/store"
)

func DashboardSummary(c *gin.Context) {
	mgr := portfolio.GetManager()
	if mgr == nil {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "data": gin.H{}})
		return
	}

	// ── Real-time market prices from Binance ──
	type MarketQuote struct {
		Symbol    string  `json:"symbol"`
		Price     float64 `json:"price"`
		ChangePct float64 `json:"change_pct"`
		Volume    float64 `json:"volume"`
	}
	// Fetch market quotes with timeout (non-blocking)
	marketQuotes := []MarketQuote{}
	marketDone := make(chan struct{})
	go func() {
		defer close(marketDone)
		for _, sym := range []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT"} {
			if ticker, err := fetchBinance24hrTicker(sym); err == nil {
				marketQuotes = append(marketQuotes, MarketQuote{
					Symbol:    sym,
					Price:     getFloat(ticker, "lastPrice", 0),
					ChangePct: getFloat(ticker, "priceChangePercent", 0),
					Volume:    getFloat(ticker, "quoteVolume", 0),
				})
			}
		}
	}()
	select {
	case <-marketDone:
	case <-time.After(3 * time.Second):
		// Market data timeout — continue without it
	}

	// ── Real portfolio KPIs ──
	totalEquity := mgr.TotalEquity()
	availableBalance := mgr.AvailableBalance()
	maxDrawdownPct := mgr.Drawdown()
	totalPnL := mgr.TotalPnL()

	// ── Real positions ──
	rawPositions := mgr.GetPositions()
	positions := make([]map[string]any, 0, len(rawPositions))
	for _, p := range rawPositions {
		positions = append(positions, map[string]any{
			"symbol":          p.Symbol,
			"quantity":        p.Quantity,
			"avg_entry_price": p.AvgEntryPrice,
			"unrealized_pnl":  store.RoundFloat(p.UnrealizedPnL, 2),
		})
	}

	// ── Real orders & trade stats ──
	allOrders := store.GetOrders("")
	totalTrades := 0
	winTrades := 0
	loseTrades := 0
	recentTrades := make([]map[string]any, 0)
	for _, o := range allOrders {
		status := getString(o, "status", "")
		if status == "FILLED" {
			totalTrades++
			ts := getFloat(o, "created_at", float64(time.Now().Unix()))
			pnl := getFloat(o, "realized_pnl", 0)
			if pnl > 0 {
				winTrades++
			} else if pnl < 0 {
				loseTrades++
			}
			recentTrades = append(recentTrades, map[string]any{
				"time":     ts,
				"symbol":   getString(o, "symbol", ""),
				"side":     getString(o, "side", ""),
				"price":    getFloat(o, "price", 0),
				"quantity": getFloat(o, "quantity", 0),
				"pnl":      pnl,
			})
		}
	}
	sort.Slice(recentTrades, func(i, j int) bool {
		return getFloat(recentTrades[i], "time", 0) > getFloat(recentTrades[j], "time", 0)
	})
	if len(recentTrades) > 20 {
		recentTrades = recentTrades[:20]
	}

	winRate := 0.0
	if totalTrades > 0 {
		winRate = float64(winTrades) / float64(totalTrades) * 100
	}

	// ── Equity curve from snapshots ──
	snapshots := mgr.GetSnapshots()
	dailyPnl := make([]map[string]any, 0)
	monthlyReturns := make([]map[string]any, 0)
	hourlyDist := make([]map[string]any, 24)
	for h := 0; h < 24; h++ {
		hourlyDist[h] = map[string]any{"hour": h, "count": 0, "profit": 0.0}
	}
	bestDay := 0.0
	worstDay := 0.0
	totalReturnPct := 0.0
	sharpe := 0.0

	if len(snapshots) > 0 {
		startEquity := snapshots[0].TotalEquity
		if startEquity > 0 {
			totalReturnPct = (totalEquity - startEquity) / startEquity * 100
		}

		// Daily PnL aggregated from snapshots
		dayPnls := make(map[string]float64)
		var prevEquity float64
		for i, s := range snapshots {
			day := time.UnixMilli(s.Timestamp).Format("2006-01-02")
			if i == 0 {
				prevEquity = s.TotalEquity
				continue
			}
			change := s.TotalEquity - prevEquity
			dayPnls[day] += change
			prevEquity = s.TotalEquity
		}
		for day, pnl := range dayPnls {
			dailyPnl = append(dailyPnl, map[string]any{
				"date": day, "profit": store.RoundFloat(pnl, 2),
			})
			bestDay = math.Max(bestDay, pnl)
			worstDay = math.Min(worstDay, pnl)
		}
		sort.Slice(dailyPnl, func(i, j int) bool {
			return dailyPnl[i]["date"].(string) < dailyPnl[j]["date"].(string)
		})
		if len(dailyPnl) > 30 {
			dailyPnl = dailyPnl[len(dailyPnl)-30:]
		}

		// Monthly returns
		monthPnls := make(map[string]float64)
		for i, s := range snapshots {
			month := time.UnixMilli(s.Timestamp).Format("2006-01")
			if i == 0 {
				monthPnls[month] = 0
				continue
			}
			monthPnls[month] += s.TotalEquity - snapshots[i-1].TotalEquity
		}
		for month, pnl := range monthPnls {
			monthlyReturns = append(monthlyReturns, map[string]any{
				"month": month, "profit": store.RoundFloat(pnl, 2),
			})
		}
		sort.Slice(monthlyReturns, func(i, j int) bool {
			return monthlyReturns[i]["month"].(string) < monthlyReturns[j]["month"].(string)
		})

		// Hourly distribution from trade timestamps
		for _, t := range recentTrades {
			ts := int64(getFloat(t, "time", 0))
			h := time.Unix(ts, 0).Hour()
			if h >= 0 && h < 24 {
				hourlyDist[h]["count"] = hourlyDist[h]["count"].(int) + 1
				hourlyDist[h]["profit"] = hourlyDist[h]["profit"].(float64) + getFloat(t, "pnl", 0)
			}
		}

		// Sharpe ratio approximation
		if len(dailyPnl) > 1 {
			var sum, sumSq float64
			for _, d := range dailyPnl {
				pnl := d["profit"].(float64)
				sum += pnl
				sumSq += pnl * pnl
			}
			n := float64(len(dailyPnl))
			mean := sum / n
			variance := (sumSq / n) - mean*mean
			if variance > 0 {
				sharpe = mean / math.Sqrt(variance) * math.Sqrt(365)
			}
		}
	}

	// ── Strategy stats from store configs ──
	strategyConfigs := store.GetStrategyConfigs()
	strategyStats := make([]map[string]any, 0, len(strategyConfigs))
	for id, cfg := range strategyConfigs {
		name := ""
		if n, ok := cfg["name"].(string); ok {
			name = n
		} else {
			name = id
		}
		running := false
		if r, ok := cfg["enabled"].(bool); ok {
			running = r
		}
		strategyStats = append(strategyStats, map[string]any{
			"name":    name,
			"running": running,
			"symbols": []string{},
		})
	}

	// Strategy PnL pie (aggregate by strategy name from trade tags if available)
	strategyPnlPie := make([]map[string]any, 0)

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"data": gin.H{
			"market_quotes":     marketQuotes,
			"total_equity":        store.RoundFloat(totalEquity, 2),
			"available_balance":   store.RoundFloat(availableBalance, 2),
			"total_return_pct":    store.RoundFloat(totalReturnPct, 2),
			"max_drawdown_pct":    store.RoundFloat(maxDrawdownPct, 2),
			"sharpe_ratio":        store.RoundFloat(sharpe, 3),
			"win_rate_pct":        store.RoundFloat(winRate, 1),
			"total_trades":        totalTrades,
			"daily_pnl":           dailyPnl,
			"hourly_distribution": hourlyDist,
			"monthly_returns":     monthlyReturns,
			"calendar_months":     monthlyReturns,
			"strategy_pnl_pie":    strategyPnlPie,
			"best_day":            store.RoundFloat(bestDay, 2),
			"worst_day":           store.RoundFloat(worstDay, 2),
			"positions":           positions,
			"recent_trades":       recentTrades,
			"strategy_stats":      strategyStats,
			"total_pnl":           store.RoundFloat(totalPnL, 2),
		},
	})
}

func formatDay(d int) string {
	return fmt.Sprintf("%02d", d)
}
