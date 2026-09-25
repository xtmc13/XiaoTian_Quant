package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/backtest/analysis"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 回测偏差检测（对标 freqtrade lookahead-analysis / recursive-analysis）──
// 异步任务：POST 创建任务后立即返回 job id，后台goroutine 跑变体回测，
// 结果落库 xt_analysis_jobs，GET 查询。

var analysisJobSeq int64

type analysisJobConfig struct {
	StrategyType   string  `json:"strategy_type"`
	StrategyID     string  `json:"strategy_id"` // 可选：从策略配置解析 strategy_type
	Symbol         string  `json:"symbol"`
	Interval       string  `json:"interval"`
	InitialBalance float64 `json:"initial_balance"`
	From           string  `json:"from"`
	To             string  `json:"to"`
	MinSignals     int     `json:"min_signals"`
	MaxEntryChecks int     `json:"max_entry_checks"` // lookahead 逐入场截断检查上限
	PrefixLevels   int     `json:"prefix_levels"`    // recursive 前缀档位
}

// analysisNativeStrategies 与 newBacktestStrategy 支持的原生策略枚举一致。
var analysisNativeStrategies = map[string]bool{
	"breakout": true, "sma_cross": true, "martin_trend": true, "wallstreet": true,
	"macd_golden_long": true, "macd_death_short": true, "ema_follow_trend": true,
	"ema_counter_trend": true, "dual_burn": true, "global_burn": true,
	"trend_long": true, "trend_short": true, "counter_stable": true, "head_tail_arb": true,
}

// StartLookaheadAnalysis 启动 lookahead 偏差分析任务。
func StartLookaheadAnalysis(c *gin.Context) {
	startAnalysisJob(c, "lookahead")
}

// StartRecursiveAnalysis 启动 recursive 递归偏差分析任务。
func StartRecursiveAnalysis(c *gin.Context) {
	startAnalysisJob(c, "recursive")
}

func startAnalysisJob(c *gin.Context, kind string) {
	var body analysisJobConfig
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	if body.Symbol == "" {
		body.Symbol = "BTCUSDT"
	}
	if body.Interval == "" {
		body.Interval = "1h"
	}
	if body.InitialBalance <= 0 {
		body.InitialBalance = 100000
	}

	// strategy_id 优先：从策略配置解析原生策略类型
	if body.StrategyID != "" {
		rec, err := store.NewStrategyConfigRepo().GetByID(body.StrategyID)
		if err != nil || rec == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "strategy config not found: " + body.StrategyID})
			return
		}
		if !requireOwner(c, rec.UserID) {
			return
		}
		body.StrategyType = rec.StrategyType
		if body.Symbol == "" && rec.Symbol != "" {
			body.Symbol = rec.Symbol
		}
	}
	if !analysisNativeStrategies[body.StrategyType] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "unsupported strategy_type: " + body.StrategyType,
			"detail": "偏差检测目前支持原生回测策略（与 /backtest/run 同一枚举），合约/CRA 策略暂不支持",
		})
		return
	}

	if store.GetDB() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储不可用，无法创建分析任务"})
		return
	}

	// 解析时间范围并加载本地历史数据（与 hyperopt 同一口径）
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
	var bars []model.Bar
	if DataDownloader != nil {
		bars = DataDownloader.LoadBarsForBacktest(body.Symbol, body.Interval, fromMs, toMs)
	}
	minBars := 100
	if len(bars) < minBars {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "数据不足",
			"detail": fmt.Sprintf("本地存储中 %s %s 仅有 %d 根K线，偏差检测至少需要 %d 根。请通过数据导入功能预先加载数据", body.Symbol, body.Interval, len(bars), minBars),
		})
		return
	}

	paramsJSON, _ := json.Marshal(body)
	seq := atomic.AddInt64(&analysisJobSeq, 1)
	jobID := fmt.Sprintf("ana-%d-%d", time.Now().UnixMilli(), seq)
	repo := store.NewAnalysisJobRepo()
	rec := &store.AnalysisJobRecord{
		ID:           jobID,
		UserID:       getUserID(c),
		Kind:         kind,
		Status:       "running",
		Symbol:       body.Symbol,
		Interval:     body.Interval,
		StrategyType: body.StrategyType,
		ParamsJSON:   string(paramsJSON),
	}
	if err := repo.Create(rec); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建任务失败: " + err.Error()})
		return
	}

	cfg := analysis.Config{
		Symbol:         body.Symbol,
		InitialBalance: body.InitialBalance,
		MinSignals:     body.MinSignals,
		MaxEntryChecks: body.MaxEntryChecks,
		PrefixLevels:   body.PrefixLevels,
	}
	// 原生策略为事件驱动、无需预计算，工厂每次返回全新实例即可。
	factory := func(symbol string, _ []model.Bar) (backtest.BacktestStrategy, error) {
		return newBacktestStrategy(body.StrategyType, symbol), nil
	}

	go func() {
		var resultJSON string
		var errMsg string
		switch kind {
		case "lookahead":
			if res, err := analysis.RunLookahead(cfg, factory, bars); err != nil {
				errMsg = err.Error()
			} else if data, err := json.Marshal(res); err != nil {
				errMsg = err.Error()
			} else {
				resultJSON = string(data)
			}
		case "recursive":
			if res, err := analysis.RunRecursive(cfg, factory, bars); err != nil {
				errMsg = err.Error()
			} else if data, err := json.Marshal(res); err != nil {
				errMsg = err.Error()
			} else {
				resultJSON = string(data)
			}
		}
		status := "completed"
		if errMsg != "" {
			status = "failed"
		}
		if err := repo.Finish(jobID, status, resultJSON, errMsg); err != nil {
			log.Printf("analysis: finish job %s: %v", jobID, err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{
		"status": "started",
		"job_id": jobID,
		"kind":   kind,
		"bars":   len(bars),
		"config": body,
	})
}

// ListAnalysisJobs 列出当前用户的分析任务（?kind=lookahead|recursive 可选）。
func ListAnalysisJobs(c *gin.Context) {
	if store.GetDB() == nil {
		c.JSON(http.StatusOK, gin.H{"jobs": []any{}})
		return
	}
	jobs, err := store.NewAnalysisJobRepo().List(getUserID(c), c.Query("kind"), 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if jobs == nil {
		jobs = []*store.AnalysisJobRecord{}
	}
	c.JSON(http.StatusOK, gin.H{"jobs": jobs, "count": len(jobs)})
}

// GetAnalysisJob 返回单个任务详情（含结果）。
func GetAnalysisJob(c *gin.Context) {
	jobID := c.Param("id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job id required"})
		return
	}
	if store.GetDB() == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	job, err := store.NewAnalysisJobRepo().GetByID(jobID)
	if err != nil || job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	if !requireOwner(c, job.UserID) {
		return
	}
	resp := gin.H{
		"id":            job.ID,
		"kind":          job.Kind,
		"status":        job.Status,
		"symbol":        job.Symbol,
		"interval":      job.Interval,
		"strategy_type": job.StrategyType,
		"created_at":    job.CreatedAt,
		"updated_at":    job.UpdatedAt,
		"completed_at":  job.CompletedAt,
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(job.ParamsJSON), &params); err == nil {
		resp["config"] = params
	}
	if job.Error != "" {
		resp["error"] = job.Error
	}
	if job.ResultJSON != "" {
		var result map[string]any
		if err := json.Unmarshal([]byte(job.ResultJSON), &result); err == nil {
			resp["result"] = result
		}
	}
	c.JSON(http.StatusOK, resp)
}
