package handler

import (
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/ws"
)

// StartAIBotSnapshotWorker starts a background goroutine that periodically
// records performance snapshots for all running AI bot instances.
func StartAIBotSnapshotWorker(interval time.Duration) {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			instances := store.GetAIBotInstances(0) // 0 returns all users
			for _, inst := range instances {
				if getString(inst, "status", "stopped") != "running" {
					continue
				}
				id := getString(inst, "id", "")
				if id == "" {
					continue
				}

				executionMode := getString(inst, "execution_mode", "paper")
				var equity, unrealized, realized, totalReturn, maxDrawdown, sharpe, winRate float64
				var totalTrades int

				if executionMode == "paper" || executionMode == "signal" {
					// Run paper-trading simulation step.
					var err error
					unrealized, realized, totalReturn, equity, maxDrawdown, sharpe, winRate, totalTrades, err = simulateAIBotStep(inst)
					if err != nil {
						inst["status"] = "error"
						inst["error_message"] = err.Error()
						store.SaveAIBotInstance(inst)
						continue
					}
				} else {
					// Live mode: read current metrics from instance until real
					// position integration is wired in.
					totalReturn = getFloat(inst, "total_return_pct", 0)
					unrealized = getFloat(inst, "unrealized_pnl", 0)
					realized = getFloat(inst, "realized_pnl", 0)
					initialBalance := getFloat(inst, "initial_balance", 10000)
					equity = initialBalance + unrealized + realized
					maxDrawdown = getFloat(inst, "max_drawdown_pct", 0)
					sharpe = getFloat(inst, "sharpe_ratio", 0)
					winRate = getFloat(inst, "win_rate", 0) * 100
					totalTrades = getInt(inst, "total_trades", 0)
				}

				// Persist snapshot and update instance metrics.
				store.SaveAIBotSnapshot(id, equity, unrealized, realized, totalReturn)
				store.UpdateAIBotInstanceMetrics(id, unrealized, realized, totalReturn, maxDrawdown, sharpe, winRate/100, totalTrades)

				// Broadcast real-time status update to connected clients.
				if hub := ws.GetHub(); hub != nil {
					hub.Broadcast(ws.Message{
						Channel: "ai-bot",
						Type:    "bot_status_update",
						Data: map[string]any{
							"bot_id":           id,
							"user_id":          getInt(inst, "user_id", 0),
							"status":           getString(inst, "status", "stopped"),
							"total_equity":     equity,
							"unrealized_pnl":   unrealized,
							"realized_pnl":     realized,
							"total_return_pct": totalReturn,
							"max_drawdown_pct": maxDrawdown,
							"sharpe_ratio":     sharpe,
							"win_rate":         winRate / 100,
							"total_trades":     totalTrades,
							"timestamp":        time.Now().Unix(),
						},
					})
				}
			}
		}
	}()
}
