package experiment

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/indicator"
	"github.com/xiaotian-quant/gateway/internal/middleware"
)

// RunExperimentHandler godoc
// POST /api/experiment/run
// Runs a full parameter optimization experiment (DE or TPE) with OOS validation.
func RunExperimentHandler(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	_, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized"})
		return
	}

	var req ExperimentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": err.Error(), "data": nil})
		return
	}

	if req.Code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "code is required", "data": nil})
		return
	}
	if req.Symbol == "" {
		req.Symbol = "BTCUSDT"
	}
	if req.Optimizer == "" {
		req.Optimizer = "de"
	}

	result, err := RunExperiment(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Save result so ExperimentStatusHandler can query it
	result.Status = "completed"
	if uid, ok := userID.(int); ok {
		result.UserID = int64(uid)
	}
	SetExperimentState(result.ExperimentID, result)

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"result":      result,
		"data":        result,
		"best_params": result.BestParams,
	})
}

// SensitivityAnalysisHandler godoc
// POST /api/experiment/sensitivity
// Performs one-at-a-time sensitivity analysis around best params.
func SensitivityAnalysisHandler(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	_, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized"})
		return
	}

	var req struct {
		Code        string            `json:"code" binding:"required"`
		Symbol      string            `json:"symbol"`
		Interval    string            `json:"interval"`
		Klines      []map[string]any  `json:"klines,omitempty"`
		BaseParams  map[string]any    `json:"base_params"`
		ParamSpace  ParamSpace        `json:"param_space,omitempty"`
		BacktestConfig backtest.RunnerConfig `json:"backtest_config,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	if req.Symbol == "" {
		req.Symbol = "BTCUSDT"
	}

	space := req.ParamSpace
	if len(space) == 0 && req.Code != "" {
		parsed := indicator.ParseSource(req.Code)
		space = FromIndicatorParseResult(parsed)
	}

	bars := klinesToBars(req.Klines, req.Symbol, req.Interval)
	if len(bars) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "no bar data"})
		return
	}

	btCfg := req.BacktestConfig
	if btCfg.InitialBalance <= 0 {
		btCfg.InitialBalance = 10000
	}
	scoreCfg := DefaultScoreConfig()

	result := SensitivityAnalysis(req.Code, req.Symbol, req.BaseParams, space, bars, btCfg, scoreCfg)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"result":  result,
		"data":    result,
	})
}

// experimentStates tracks running/completed experiments with results.
var experimentStates = make(map[string]*ExperimentResult)
var experimentStatesMu sync.RWMutex

// SetExperimentState records an experiment state for status queries.
func SetExperimentState(id string, result *ExperimentResult) {
	experimentStatesMu.Lock()
	defer experimentStatesMu.Unlock()
	experimentStates[id] = result
}

// ExperimentStatusHandler godoc
// GET /api/experiment/status/:id
// Returns the status of an experiment, whether still running or completed.
// H8: 非属主（且非 admin）按 not_found 处理（避免枚举他人实验）。
func ExperimentStatusHandler(c *gin.Context) {
	id := c.Param("id")

	experimentStatesMu.RLock()
	result, exists := experimentStates[id]
	experimentStatesMu.RUnlock()

	if !exists || !experimentOwned(c, result.UserID) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"result":  gin.H{"experiment_id": id, "status": "not_found"},
			"data":    gin.H{"experiment_id": id, "status": "not_found"},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"result":  result,
		"data":    result,
	})
}

// experimentOwned 校验实验归属：未注入用户/admin 放行；
// 无属主(0)对所有登录用户可见（保持单用户时代行为）；其余仅属主本人。
func experimentOwned(c *gin.Context, ownerUserID int64) bool {
	v, injected := c.Get(middleware.UserIDKey)
	if !injected {
		return true
	}
	role, _ := c.Get(middleware.RoleKey)
	if r, ok := role.(string); ok && r == "admin" {
		return true
	}
	if ownerUserID == 0 {
		return true
	}
	uid, ok := v.(int)
	return ok && int64(uid) == ownerUserID
}

// ExperimentListHandler godoc
// GET /api/experiments
// 返回当前进程记录的实验摘要列表（按属主过滤）。
// 每项含 id/name/status/指标摘要(best_score + IS/OOS 收益与回撤)/创建时间。
func ExperimentListHandler(c *gin.Context) {
	experimentStatesMu.RLock()
	items := make([]gin.H, 0, len(experimentStates))
	for id, r := range experimentStates {
		if !experimentOwned(c, r.UserID) {
			continue
		}
		item := gin.H{
			"id":          id,
			"experiment_id": id,
			"name":        r.Name,
			"status":      r.Status,
			"best_score":  r.BestScore,
			"duration_ms": r.DurationMs,
			"created_at":  r.CreatedAt,
			"user_id":     r.UserID,
		}
		if r.ISResult != nil {
			item["is_return_pct"] = r.ISResult.TotalReturnPct
			item["is_max_drawdown_pct"] = r.ISResult.MaxDrawdownPct
		}
		if r.OOSResult != nil {
			item["oos_return_pct"] = r.OOSResult.TotalReturnPct
			item["oos_max_drawdown_pct"] = r.OOSResult.MaxDrawdownPct
		}
		items = append(items, item)
	}
	experimentStatesMu.RUnlock()
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// WalkForwardHandler godoc
// POST /api/experiment/walk-forward
// Runs walk-forward validation on an indicator.
func WalkForwardHandler(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	_, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "unauthorized"})
		return
	}

	var req struct {
		Code           string            `json:"code" binding:"required"`
		Symbol         string            `json:"symbol"`
		Interval       string            `json:"interval"`
		Klines         []map[string]any  `json:"klines,omitempty"`
		Params         map[string]any    `json:"params"`
		WindowSize     int               `json:"window_size"`
		TestSize       int               `json:"test_size"`
		StepSize       int               `json:"step_size"`
		BacktestConfig backtest.RunnerConfig `json:"backtest_config,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if req.Symbol == "" {
		req.Symbol = "BTCUSDT"
	}
	if req.WindowSize <= 0 {
		req.WindowSize = 200
	}
	if req.TestSize <= 0 {
		req.TestSize = 50
	}
	if req.StepSize <= 0 {
		req.StepSize = req.TestSize
	}

	bars := klinesToBars(req.Klines, req.Symbol, req.Interval)
	if len(bars) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "no bar data"})
		return
	}

	strategy := NewIndicatorStrategy(req.Code, req.Symbol, req.Params)
	if err := strategy.Precompute(bars); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	btCfg := req.BacktestConfig
	if btCfg.InitialBalance <= 0 {
		btCfg.InitialBalance = 10000
	}
	scoreCfg := DefaultScoreConfig()
	wfCfg := WalkForwardConfig{
		WindowSize: req.WindowSize,
		TestSize:   req.TestSize,
		StepSize:   req.StepSize,
	}

	result := WalkForward(bars, strategy, btCfg, scoreCfg, wfCfg)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"result":  result,
		"data":    result,
	})
}
