package handler

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/hyperopt"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/strategies"
)

// ── Hyperopt Jobs ──────────────────────────────────────────────

var (
	hyperoptJobs     = make(map[string]*hyperoptJob)
	hyperoptJobsMu   sync.RWMutex
	hyperoptJobSeq   int
)

type hyperoptJob struct {
	ID        string                `json:"id"`
	Status    string                `json:"status"` // running, completed, failed, cancelled
	Config    hyperoptJobConfig     `json:"config"`
	Result    *hyperopt.Result      `json:"result,omitempty"`
	Progress  hyperoptProgress      `json:"progress"`
	Error     string                `json:"error,omitempty"`
	CreatedAt int64                 `json:"created_at"`
	UpdatedAt int64                 `json:"updated_at"`
	ctx       context.Context
	cancel    context.CancelFunc
}

type hyperoptJobConfig struct {
	StrategyType   string  `json:"strategy_type"`
	Symbol         string  `json:"symbol"`
	Interval       string  `json:"interval"`
	InitialBalance float64 `json:"initial_balance"`
	MaxEvals       int     `json:"max_evals"`
	Sampler        string  `json:"sampler"`     // tpe, random, grid
	GridPoints     int     `json:"grid_points"`
	LossMetric     string  `json:"loss_metric"` // total_return, sharpe, profit_factor, custom
	From           string  `json:"from"`
	To             string  `json:"to"`
}

type hyperoptProgress struct {
	Done     int     `json:"done"`
	Total    int     `json:"total"`
	BestLoss float64 `json:"best_loss"`
}

// StartHyperopt starts a new hyperopt optimization job.
func StartHyperopt(c *gin.Context) {
	var body hyperoptJobConfig
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	// Defaults
	if body.Symbol == "" {
		body.Symbol = "BTCUSDT"
	}
	if body.Interval == "" {
		body.Interval = "1h"
	}
	if body.InitialBalance <= 0 {
		body.InitialBalance = 100000
	}
	if body.MaxEvals <= 0 {
		body.MaxEvals = 50
	}
	if body.MaxEvals > 500 {
		body.MaxEvals = 500 // cap to prevent abuse
	}
	if body.Sampler == "" {
		body.Sampler = "tpe"
	}
	if body.GridPoints <= 0 {
		body.GridPoints = 5
	}
	if body.LossMetric == "" {
		body.LossMetric = "sharpe"
	}

	// Parse date range
	var fromMs, toMs int64
	if body.From != "" {
		if t, err := time.Parse("2006-01-02", body.From); err == nil {
			fromMs = t.UnixMilli()
		}
	}
	if body.To != "" {
		if t, err := time.Parse("2006-01-02", body.To); err == nil {
			toMs = t.UnixMilli()
		}
	}

	// Load historical data
	var bars []model.Bar
	if DataDownloader != nil {
		bars = DataDownloader.LoadBarsForBacktest(body.Symbol, body.Interval, fromMs, toMs)
	}
	if len(bars) < 50 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "数据不足",
			"detail": fmt.Sprintf("本地存储中 %s %s 仅有 %d 根K线，至少需要 50 根。请通过数据导入功能或数据库 seeding 预先加载数据", body.Symbol, body.Interval, len(bars)),
		})
		return
	}

	// Create strategy instance to get parameters
	var strat strategy.Strategy
	switch body.StrategyType {
	case "breakout":
		strat = strategies.NewBreakoutStrategy()
	case "ema_cross":
		strat = strategies.NewEMACrossStrategy()
	case "macd":
		strat = strategies.NewMACDStrategy()
	case "rsi":
		strat = strategies.NewRSIStrategy()
	case "bollinger_bands":
		strat = strategies.NewBollingerBandsStrategy()
	case "atr_trailing_stop":
		strat = strategies.NewATRTrailingStopStrategy()
	case "dual_thrust":
		strat = strategies.NewDualThrustStrategy()
	case "renko":
		strat = strategies.NewRenkoStrategy()
	case "grid":
		strat = strategies.NewGridTradingStrategy()
	case "arbitrage":
		strat = strategies.NewArbitrageStrategy()
	case "market_making":
		strat = strategies.NewMarketMakingStrategy()
	default:
		strat = strategies.NewBreakoutStrategy()
	}

	reg := strat.GetParameters()
	if reg == nil || len(reg.Optimizable()) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "策略无优化参数",
			"detail": fmt.Sprintf("策略 %s 没有可优化的参数，请检查参数定义中的 optimize 字段", body.StrategyType),
		})
		return
	}

	// Build search space
	space := hyperopt.NewSearchSpaceFromRegistry(reg)
	if space.Dimensions() == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "搜索空间为空，参数可能缺少 min/max 范围"})
		return
	}

	// Create job
	hyperoptJobsMu.Lock()
	hyperoptJobSeq++
	jobID := fmt.Sprintf("ho-%d", hyperoptJobSeq)
	ctx, cancel := context.WithCancel(context.Background())
	job := &hyperoptJob{
		ID:        jobID,
		Status:    "running",
		Config:    body,
		Progress:  hyperoptProgress{Total: body.MaxEvals},
		CreatedAt: time.Now().UnixMilli(),
		UpdatedAt: time.Now().UnixMilli(),
		ctx:       ctx,
		cancel:    cancel,
	}
	hyperoptJobs[jobID] = job
	hyperoptJobsMu.Unlock()

	// Objective function: run backtest with given params
	objective := func(params map[string]any) (float64, map[string]float64, error) {
		// Clone strategy and apply params
		var btStrat strategy.Strategy
		switch body.StrategyType {
		case "breakout":
			btStrat = strategies.NewBreakoutStrategy()
		case "grid":
			btStrat = strategies.NewGridTradingStrategy()
		case "arbitrage":
			btStrat = strategies.NewArbitrageStrategy()
		case "market_making":
			btStrat = strategies.NewMarketMakingStrategy()
		case "ema_cross":
			btStrat = strategies.NewEMACrossStrategy()
		case "macd":
			btStrat = strategies.NewMACDStrategy()
		case "rsi":
			btStrat = strategies.NewRSIStrategy()
		case "bollinger_bands":
			btStrat = strategies.NewBollingerBandsStrategy()
		default:
			btStrat = strategies.NewBreakoutStrategy()
		}

		if err := btStrat.ApplyParams(params); err != nil {
			return math.Inf(1), nil, err
		}

		// Create backtest strategy adapter
		adapter := &strategyBacktestAdapter{Strategy: btStrat, symbol: body.Symbol}

		// Run backtest
		cfg := backtest.DefaultRunnerConfig()
		cfg.InitialBalance = body.InitialBalance
		cfg.StartTime = fromMs
		cfg.EndTime = toMs
		runner := backtest.NewRunner(cfg)
		runner.LoadBars(body.Symbol, bars)

		result, err := runner.Run(adapter)
		if err != nil {
			return math.Inf(1), nil, err
		}

		// Compute loss (lower = better)
		metrics := map[string]float64{
			"total_return_pct": result.TotalReturnPct,
			"sharpe_ratio":     result.SharpeRatio,
			"max_drawdown_pct": result.MaxDrawdownPct,
			"win_rate":         result.WinRate,
			"profit_factor":    result.ProfitFactor,
			"total_trades":     float64(result.TotalTrades),
		}

		var loss float64
		switch body.LossMetric {
		case "total_return":
			loss = -result.TotalReturnPct // maximize return
		case "sharpe":
			loss = -result.SharpeRatio // maximize sharpe
		case "profit_factor":
			loss = -result.ProfitFactor // maximize profit factor
		case "risk_reward":
			// Approximate R:R from win rate and profit factor
			if result.WinRate > 0 && result.WinRate < 100 && result.ProfitFactor > 0 {
				W := result.WinRate / 100.0
				rr := result.ProfitFactor * (1.0 - W) / W
				loss = -rr
			} else {
				loss = math.Inf(1)
			}
		case "sqn":
			// System Quality Number
			if result.TotalTrades > 1 && result.SharpeRatio != 0 {
				stdDev := math.Abs(result.TotalReturnPct / result.SharpeRatio)
				if stdDev > 0 {
					sqn := math.Sqrt(float64(result.TotalTrades)) * (result.TotalReturnPct / float64(result.TotalTrades)) / stdDev
					loss = -sqn
				} else {
					loss = math.Inf(1)
				}
			} else {
				loss = math.Inf(1)
			}
		case "custom":
			// Combined: negative return with drawdown penalty
			loss = -result.TotalReturnPct + result.MaxDrawdownPct*2
		default:
			loss = -result.SharpeRatio
		}

		// Penalize insufficient trades
		if result.TotalTrades < 5 {
			loss += 1000
		}

		return loss, metrics, nil
	}

	// Run optimization in background
	go func() {
		engineCfg := hyperopt.EngineConfig{
			MaxEvals:   body.MaxEvals,
			Sampler:    body.Sampler,
			GridPoints: body.GridPoints,
			Seed:       time.Now().UnixNano(),
		}
		engine := hyperopt.NewEngine(engineCfg, space, objective)

		engine.OnProgress = func(done, total int, bestLoss float64) {
			hyperoptJobsMu.Lock()
			if j, ok := hyperoptJobs[jobID]; ok {
				j.Progress.Done = done
				j.Progress.Total = total
				j.Progress.BestLoss = bestLoss
				j.UpdatedAt = time.Now().UnixMilli()
			}
			hyperoptJobsMu.Unlock()
		}

		result, err := engine.Run(ctx)

		// Send hyperopt completion notification
		if result != nil && result.BestTrial != nil {
			go func() {
				broadcaster := notify.NewBroadcaster()
				broadcaster.Hyperopt(
					body.StrategyType,
					result.BestParams(),
					result.BestTrial.Metrics,
					result.TotalEvals,
					result.Duration,
				)
			}()
		}

		hyperoptJobsMu.Lock()
		if j, ok := hyperoptJobs[jobID]; ok {
			j.Result = result
			j.UpdatedAt = time.Now().UnixMilli()
			if err != nil {
				j.Status = "failed"
				j.Error = err.Error()
			} else {
				j.Status = "completed"
			}
		}
		hyperoptJobsMu.Unlock()
	}()

	c.JSON(http.StatusOK, gin.H{
		"status": "started",
		"job_id": jobID,
		"config": body,
		"space": gin.H{
			"dimensions": space.Dimensions(),
			"names":      space.Names(),
		},
	})
}

// GetHyperoptJob returns the status of a hyperopt job.
func GetHyperoptJob(c *gin.Context) {
	jobID := c.Param("id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job id required"})
		return
	}

	hyperoptJobsMu.RLock()
	job, ok := hyperoptJobs[jobID]
	hyperoptJobsMu.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	// Build response
	resp := gin.H{
		"id":        job.ID,
		"status":    job.Status,
		"config":    job.Config,
		"progress":  job.Progress,
		"created_at": job.CreatedAt,
		"updated_at": job.UpdatedAt,
	}

	if job.Error != "" {
		resp["error"] = job.Error
	}

	if job.Result != nil {
		resp["result"] = gin.H{
			"total_evals":  job.Result.TotalEvals,
			"duration_ms":  job.Result.Duration.Milliseconds(),
			"sampler":      job.Result.Sampler,
			"space_dims":   job.Result.SpaceDims,
			"mean_loss":    roundFloat(job.Result.MeanLoss, 4),
			"median_loss":  roundFloat(job.Result.MedianLoss, 4),
			"std_loss":     roundFloat(job.Result.StdLoss, 4),
			"min_loss":     roundFloat(job.Result.MinLoss, 4),
			"max_loss":     roundFloat(job.Result.MaxLoss, 4),
			"best_params":  job.Result.BestParams(),
			"best_metrics": func() map[string]float64 {
				if job.Result.BestTrial != nil {
					return job.Result.BestTrial.Metrics
				}
				return nil
			}(),
		}
		// Include top 10 trials
		if len(job.Result.Trials) > 0 {
			topN := min(len(job.Result.Trials), 10)
			trials := make([]gin.H, 0, topN)
			for i := 0; i < topN; i++ {
				t := job.Result.Trials[i]
				trials = append(trials, gin.H{
					"id":      t.ID,
					"loss":    roundFloat(t.Loss, 4),
					"params":  t.Params,
					"metrics": t.Metrics,
				})
			}
			resp["top_trials"] = trials
		}
	}

	c.JSON(http.StatusOK, resp)
}

// ListHyperoptJobs returns all hyperopt jobs.
func ListHyperoptJobs(c *gin.Context) {
	hyperoptJobsMu.RLock()
	defer hyperoptJobsMu.RUnlock()

	jobs := make([]gin.H, 0, len(hyperoptJobs))
	for _, job := range hyperoptJobs {
		jobs = append(jobs, gin.H{
			"id":        job.ID,
			"status":    job.Status,
			"strategy":  job.Config.StrategyType,
			"symbol":    job.Config.Symbol,
			"progress":  job.Progress,
			"created_at": job.CreatedAt,
			"updated_at": job.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"jobs": jobs, "count": len(jobs)})
}

// CancelHyperoptJob cancels a running hyperopt job.
func CancelHyperoptJob(c *gin.Context) {
	jobID := c.Param("id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job id required"})
		return
	}

	hyperoptJobsMu.Lock()
	job, ok := hyperoptJobs[jobID]
	if ok && job.cancel != nil {
		job.cancel()
		job.Status = "cancelled"
		job.UpdatedAt = time.Now().UnixMilli()
	}
	hyperoptJobsMu.Unlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "cancelled", "job_id": jobID})
}

// DeleteHyperoptJob removes a hyperopt job from memory.
func DeleteHyperoptJob(c *gin.Context) {
	jobID := c.Param("id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job id required"})
		return
	}

	hyperoptJobsMu.Lock()
	delete(hyperoptJobs, jobID)
	hyperoptJobsMu.Unlock()

	c.JSON(http.StatusOK, gin.H{"status": "deleted", "job_id": jobID})
}

// GetHyperoptSpaces returns the search space for a strategy.
func GetHyperoptSpaces(c *gin.Context) {
	strategyType := c.Query("strategy")
	if strategyType == "" {
		strategyType = "breakout"
	}

	var strat strategy.Strategy
	switch strategyType {
	case "breakout":
		strat = strategies.NewBreakoutStrategy()
	case "grid":
		strat = strategies.NewGridTradingStrategy()
	case "arbitrage":
		strat = strategies.NewArbitrageStrategy()
	case "market_making":
		strat = strategies.NewMarketMakingStrategy()
	case "ema_cross":
		strat = strategies.NewEMACrossStrategy()
	case "macd":
		strat = strategies.NewMACDStrategy()
	case "rsi":
		strat = strategies.NewRSIStrategy()
	case "bollinger_bands":
		strat = strategies.NewBollingerBandsStrategy()
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown strategy: " + strategyType})
		return
	}

	reg := strat.GetParameters()
	if reg == nil {
		c.JSON(http.StatusOK, gin.H{
			"strategy": strategyType,
			"spaces":   []any{},
			"count":    0,
		})
		return
	}

	spaces := reg.ToHyperoptSpaces()
	defs := reg.ToJSONDefs()

	c.JSON(http.StatusOK, gin.H{
		"strategy": strategyType,
		"spaces":   spaces,
		"defs":     defs,
		"count":    len(spaces),
	})
}

// ── Strategy Backtest Adapter ──────────────────────────────────

// strategyBacktestAdapter adapts a strategy.Strategy to backtest.BacktestStrategy.
type strategyBacktestAdapter struct {
	strategy.Strategy
	symbol string
}

func (a *strategyBacktestAdapter) Symbol() string { return a.symbol }

func (a *strategyBacktestAdapter) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	// Use the strategy's OnBar method
	// We need an EventBus, but for backtest we can pass nil and handle the signal differently
	// For simplicity, we'll call the strategy's internal logic directly
	// This is a simplified adapter - full integration would need more work
	return a.Strategy.OnBar(bar, nil)
}

func (a *strategyBacktestAdapter) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return a.Strategy.OnTick(tick, nil)
}

// ExportHyperoptParams exports the best parameters from a completed job to strategy config.
func ExportHyperoptParams(c *gin.Context) {
	jobID := c.Param("id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job id required"})
		return
	}

	hyperoptJobsMu.RLock()
	job, ok := hyperoptJobs[jobID]
	hyperoptJobsMu.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	if job.Status != "completed" || job.Result == nil || job.Result.BestTrial == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job has no completed result to export"})
		return
	}

	var body struct {
		StrategyID   string            `json:"strategy_id"`   // optional: update existing
		StrategyName string            `json:"strategy_name"` // optional: name for new config
		ParamMap     map[string]string `json:"param_map"`     // required: hyperopt name → config field
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	if len(body.ParamMap) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "param_map is required"})
		return
	}

	cfg := hyperopt.ExportConfig{
		StrategyID:   body.StrategyID,
		StrategyType: job.Config.StrategyType,
		StrategyName: body.StrategyName,
		ParamMap:     body.ParamMap,
	}

	configPath := "strategy_configs.json" // relative to working directory
	if err := hyperopt.ExportBestParams(job.Result, cfg, configPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "export failed: " + err.Error()})
		return
	}

	// Also return the mapped params in response
	mapped, _ := hyperopt.ExportBestParamsToMap(job.Result, body.ParamMap)

	c.JSON(http.StatusOK, gin.H{
		"status":        "exported",
		"job_id":        jobID,
		"strategy_type": job.Config.StrategyType,
		"mapped_params": mapped,
		"config_path":   configPath,
	})
}

// roundFloat rounds a float to n decimal places.
func roundFloat(v float64, n int) float64 {
	p := math.Pow(10, float64(n))
	return math.Round(v*p) / p
}
