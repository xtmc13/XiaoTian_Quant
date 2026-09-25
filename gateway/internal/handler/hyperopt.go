package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/hyperopt"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/protection"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/strategies"
)

// ── Hyperopt Jobs ──────────────────────────────────────────────

var (
	hyperoptJobs   = make(map[string]*hyperoptJob)
	hyperoptJobsMu sync.RWMutex
	hyperoptJobSeq int
)

type hyperoptJob struct {
	ID        string            `json:"id"`
	UserID    int64             `json:"user_id"`
	Status    string            `json:"status"` // running, completed, failed, cancelled
	Config    hyperoptJobConfig `json:"config"`
	Result    *hyperopt.Result  `json:"result,omitempty"`
	Progress  hyperoptProgress  `json:"progress"`
	Error     string            `json:"error,omitempty"`
	CreatedAt int64             `json:"created_at"`
	UpdatedAt int64             `json:"updated_at"`
	ctx       context.Context
	cancel    context.CancelFunc
}

type hyperoptJobConfig struct {
	StrategyType   string  `json:"strategy_type"`
	StrategyID     string  `json:"strategy_id"` // 可选：目标策略配置，epochs 一键回写用
	Symbol         string  `json:"symbol"`
	Interval       string  `json:"interval"`
	InitialBalance float64 `json:"initial_balance"`
	MaxEvals       int     `json:"max_evals"`
	Sampler        string  `json:"sampler"` // tpe, random, grid
	GridPoints     int     `json:"grid_points"`
	LossMetric     string  `json:"loss_metric"` // total_return, sharpe, profit_factor, custom（旧式开关）
	Loss           string  `json:"loss"`        // 可选：hyperopt.GetLossFunc 注册表名（only_profit, sqn, ...），优先于 loss_metric
	From           string  `json:"from"`
	To             string  `json:"to"`

	// spaces 优化空间选择（对标 freqtrade --spaces）：["default", "protection"]。
	// 缺省 = ["default"]（仅策略参数）。勾选 "protection" 时必须提供 protections。
	Spaces []string `json:"spaces"`
	// protections 是 protection 空间的基础配置（name + params），
	// 与 internal/protection.BuildManagerFromConfig 的配置结构一致；
	// 优化时其可调参数（见 hyperopt.ProtectionSpaceRegistry）各生成一个维度。
	Protections []protection.ProtectionConfig `json:"protections"`
	// protection_ranges 可选：按维度名（protection__<Name>__<param>）覆盖搜索范围。
	ProtectionRanges map[string]hyperopt.SpaceRangeOverride `json:"protection_ranges"`
}

// hasSpace 判断优化空间是否被勾选。
func (c *hyperoptJobConfig) hasSpace(name string) bool {
	for _, s := range c.Spaces {
		if s == name {
			return true
		}
	}
	return false
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
	// loss 参数走注册表（hyperopt.LossFuncNames），未知名称直接拒绝。
	if body.Loss != "" {
		valid := false
		for _, n := range hyperopt.LossFuncNames() {
			if n == body.Loss {
				valid = true
				break
			}
		}
		if !valid {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":  "unknown loss function: " + body.Loss,
				"detail": "可用值: " + fmt.Sprintf("%v", hyperopt.LossFuncNames()),
			})
			return
		}
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

	// spaces 语义（对标 freqtrade --spaces）：缺省只优化策略参数；
	// 显式给出 spaces 时按勾选生成对应维度（如只勾 protection 则跳过策略参数）。
	useDefaultSpace := len(body.Spaces) == 0 || body.hasSpace("default")
	protectionSpaceEnabled := body.hasSpace("protection")

	reg := strat.GetParameters()
	if useDefaultSpace && (reg == nil || len(reg.Optimizable()) == 0) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "策略无优化参数",
			"detail": fmt.Sprintf("策略 %s 没有可优化的参数，请检查参数定义中的 optimize 字段", body.StrategyType),
		})
		return
	}

	// Build search space
	space := hyperopt.NewSearchSpaceFromRegistry(reg)
	if !useDefaultSpace {
		space = hyperopt.NewSearchSpaceFromRegistry(strategy.NewParamRegistry())
	}

	// protection 空间（对标 freqtrade --spaces protection）：
	// 为每个配置的 protection 的可调参数生成维度，回测评分时应用对应配置。
	if protectionSpaceEnabled {
		if len(body.Protections) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":  "protection 空间需要 protections 基础配置",
				"detail": "勾选 protection 空间时必须提供 protections（name + params），可优化参数见 GET /api/hyperopt/spaces?space=protection",
			})
			return
		}
		protSpaces, err := hyperopt.BuildProtectionSpaces(body.Protections, body.ProtectionRanges)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		space.AddSpaces(protSpaces)
	}

	if space.Dimensions() == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "搜索空间为空，参数可能缺少 min/max 范围或未勾选优化空间"})
		return
	}

	// Create job
	hyperoptJobsMu.Lock()
	hyperoptJobSeq++
	jobID := fmt.Sprintf("ho-%d", hyperoptJobSeq)
	ctx, cancel := context.WithCancel(context.Background())
	job := &hyperoptJob{
		ID:        jobID,
		UserID:    int64(getUserID(c)),
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
		// 拆分策略参数与 protection 参数（protection 维度带 protection__ 前缀）
		stratParams, protParams := hyperopt.SplitProtectionParams(params)

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

		if err := btStrat.ApplyParams(stratParams); err != nil {
			return math.Inf(1), nil, err
		}

		// Create backtest strategy adapter
		adapter := &strategyBacktestAdapter{Strategy: btStrat, symbol: body.Symbol}

		// protection 空间：按采样参数构建 protection 配置并挂到回测上
		// （对标 freqtrade hyperopt 回测评分时 enable_protections）。
		var btStrategy backtest.BacktestStrategy = adapter
		var protected *hyperopt.ProtectedBacktestStrategy
		if protectionSpaceEnabled {
			protCfg := hyperopt.ApplyProtectionParams(body.Protections, protParams)
			mgr, err := protection.BuildManagerFromConfig(protection.Config{Protections: protCfg})
			if err != nil {
				return math.Inf(1), nil, fmt.Errorf("build protections: %w", err)
			}
			protected = hyperopt.NewProtectedBacktestStrategy(adapter, mgr, body.Interval)
			btStrategy = protected
		}

		// Run backtest
		cfg := backtest.DefaultRunnerConfig()
		cfg.InitialBalance = body.InitialBalance
		cfg.StartTime = fromMs
		cfg.EndTime = toMs
		runner := backtest.NewRunner(cfg)
		runner.LoadBars(body.Symbol, bars)

		result, err := runner.Run(btStrategy)
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
		if protected != nil {
			metrics["blocked_entries"] = float64(protected.BlockedEntries())
		}

		var loss float64
		if body.Loss != "" {
			// 注册表损失函数：由回测结果构造结构化指标后统一求值。
			bm := &hyperopt.BacktestMetrics{
				TotalReturnPct: result.TotalReturnPct,
				MaxDrawdownPct: result.MaxDrawdownPct,
				SharpeRatio:    result.SharpeRatio,
				SortinoRatio:   result.SortinoRatio,
				CalmarRatio:    result.CalmarRatio,
				WinRate:        result.WinRate,
				ProfitFactor:   result.ProfitFactor,
				TotalTrades:    result.TotalTrades,
			}
			if d := avgHoldingMinutes(result.Trades); d > 0 {
				bm.AvgDurationMin = d
			}
			loss = hyperopt.GetLossFunc(body.Loss)(bm)
		} else {
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

		// 每轮 trial 落库 xt_hyperopt_epochs（engine 保持纯算法、不依赖 store，
		// 持久化挂在 OnTrialComplete 回调上）。
		lossName := body.Loss
		if lossName == "" {
			lossName = body.LossMetric
		}
		persistEpochs := store.GetDB() != nil
		epochRepo := store.NewHyperoptEpochRepo()
		engine.OnTrialComplete = func(trial hyperopt.Trial) {
			if !persistEpochs {
				return
			}
			paramsJSON, err := json.Marshal(trial.Params)
			if err != nil {
				return
			}
			metricsJSON, err := json.Marshal(trial.Metrics)
			if err != nil {
				return
			}
			rec := &store.HyperoptEpochRecord{
				ID:          fmt.Sprintf("%s-t%d", jobID, trial.ID),
				UserID:      job.UserID,
				JobID:       jobID,
				StrategyID:  body.StrategyID,
				TrialID:     trial.ID,
				ParamsJSON:  string(paramsJSON),
				MetricsJSON: string(metricsJSON),
				Loss:        trial.Loss,
				LossName:    lossName,
				CreatedAt:   trial.Timestamp,
			}
			if err := epochRepo.Create(rec); err != nil {
				log.Printf("hyperopt: persist epoch %s: %v", rec.ID, err)
			}
		}

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

// hyperoptJobGet 取 job；不存在 404，属他人（且非 admin）403。
func hyperoptJobGet(c *gin.Context) (*hyperoptJob, bool) {
	jobID := c.Param("id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job id required"})
		return nil, false
	}

	hyperoptJobsMu.RLock()
	job, ok := hyperoptJobs[jobID]
	hyperoptJobsMu.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return nil, false
	}
	if !requireOwner(c, job.UserID) {
		return nil, false
	}
	return job, true
}

// GetHyperoptJob returns the status of a hyperopt job.
func GetHyperoptJob(c *gin.Context) {
	job, ok := hyperoptJobGet(c)
	if !ok {
		return
	}

	// Build response
	resp := gin.H{
		"id":               job.ID,
		"status":           job.Status,
		"strategy_type":    job.Config.StrategyType,
		"symbol":           job.Config.Symbol,
		"interval":         job.Config.Interval,
		"best_score":       job.Progress.BestLoss,
		"best_params":      job.Result.BestParams(),
		"trials_completed": job.Progress.Done,
		"total_trials":     job.Progress.Total,
		"config":           job.Config,
		"progress":         job.Progress,
		"created_at":       job.CreatedAt,
		"updated_at":       job.UpdatedAt,
	}

	if job.Error != "" {
		resp["error"] = job.Error
	}

	if job.Result != nil {
		resp["result"] = gin.H{
			"total_evals": job.Result.TotalEvals,
			"duration_ms": job.Result.Duration.Milliseconds(),
			"sampler":     job.Result.Sampler,
			"space_dims":  job.Result.SpaceDims,
			"mean_loss":   roundFloat(job.Result.MeanLoss, 4),
			"median_loss": roundFloat(job.Result.MedianLoss, 4),
			"std_loss":    roundFloat(job.Result.StdLoss, 4),
			"min_loss":    roundFloat(job.Result.MinLoss, 4),
			"max_loss":    roundFloat(job.Result.MaxLoss, 4),
			"best_params": job.Result.BestParams(),
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

// ListHyperoptJobs returns hyperopt jobs visible to the current user
// （本人的 + 历史无属主；admin/未注入用户看全部）。
func ListHyperoptJobs(c *gin.Context) {
	hyperoptJobsMu.RLock()
	defer hyperoptJobsMu.RUnlock()

	uid, injected := ctxUserID(c)
	restricted := injected && !ctxIsAdmin(c)

	jobs := make([]gin.H, 0, len(hyperoptJobs))
	for _, job := range hyperoptJobs {
		if restricted && job.UserID != 0 && job.UserID != int64(uid) {
			continue
		}
		jobs = append(jobs, gin.H{
			"id":               job.ID,
			"user_id":          job.UserID,
			"status":           job.Status,
			"strategy_type":    job.Config.StrategyType,
			"symbol":           job.Config.Symbol,
			"interval":         job.Config.Interval,
			"best_score":       job.Progress.BestLoss,
			"best_params":      job.Result.BestParams(),
			"trials_completed": job.Progress.Done,
			"total_trials":     job.Progress.Total,
			"progress":         job.Progress,
			"created_at":       job.CreatedAt,
			"updated_at":       job.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"jobs": jobs, "count": len(jobs)})
}

// CancelHyperoptJob cancels a running hyperopt job.
func CancelHyperoptJob(c *gin.Context) {
	jobID := c.Param("id")
	if _, ok := hyperoptJobGet(c); !ok {
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

	c.JSON(http.StatusOK, gin.H{"status": "cancelled", "job_id": jobID})
}

// DeleteHyperoptJob removes a hyperopt job from memory.
func DeleteHyperoptJob(c *gin.Context) {
	jobID := c.Param("id")
	if _, ok := hyperoptJobGet(c); !ok {
		return
	}

	hyperoptJobsMu.Lock()
	delete(hyperoptJobs, jobID)
	hyperoptJobsMu.Unlock()

	c.JSON(http.StatusOK, gin.H{"status": "deleted", "job_id": jobID})
}

// GetHyperoptSpaces returns the search space for a strategy.
// ?strategy=<type>            策略参数空间（原有行为）
// ?space=protection           protection 空间全部可调参数（注册表）
// ?space=protection&protections=StoplossGuard,MaxDrawdown  指定 protection 的维度
func GetHyperoptSpaces(c *gin.Context) {
	if c.Query("space") == "protection" {
		var base []protection.ProtectionConfig
		if names := c.Query("protections"); names != "" {
			for _, n := range strings.Split(names, ",") {
				n = strings.TrimSpace(n)
				if n != "" {
					base = append(base, protection.ProtectionConfig{Name: n})
				}
			}
		} else {
			for _, n := range hyperopt.ProtectionSpaceNames() {
				base = append(base, protection.ProtectionConfig{Name: n})
			}
		}
		spaces, err := hyperopt.BuildProtectionSpaces(base, nil)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// 输出形状与前端 HyperoptSpace 接口一致（小写键、字符串类型）
		views := make([]gin.H, 0, len(spaces))
		for _, sp := range spaces {
			views = append(views, gin.H{
				"name":    sp.Name,
				"type":    paramTypeString(sp.Type),
				"low":     sp.Min,
				"high":    sp.Max,
				"step":    sp.Step,
				"choices": sp.Options,
			})
		}
		c.JSON(http.StatusOK, gin.H{
			"space":  "protection",
			"spaces": views,
			"count":  len(views),
		})
		return
	}

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
	if !requireOwner(c, job.UserID) {
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

// avgHoldingMinutes 从回测平仓仓位计算平均持仓时长（分钟）；
// 无平仓或时长非法时返回 0（LossShortTradeDur 会退化到 Sharpe 代理）。
func avgHoldingMinutes(trades []backtest.Position) float64 {
	var sumMs int64
	var n int
	for _, p := range trades {
		if !p.IsClosed || p.ExitTime <= p.EntryTime {
			continue
		}
		sumMs += p.ExitTime - p.EntryTime
		n++
	}
	if n == 0 {
		return 0
	}
	return float64(sumMs) / float64(n) / 60000.0
}

// roundFloat rounds a float to n decimal places.
func roundFloat(v float64, n int) float64 {
	p := math.Pow(10, float64(n))
	return math.Round(v*p) / p
}

// paramTypeString 把 strategy.ParamType 转成前端可读字符串。
func paramTypeString(t strategy.ParamType) string {
	switch t {
	case strategy.ParamInt:
		return "int"
	case strategy.ParamFloat:
		return "float"
	case strategy.ParamBool:
		return "bool"
	case strategy.ParamCategorical:
		return "categorical"
	default:
		return "unknown"
	}
}
