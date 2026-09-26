package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
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

// analysisJobCancels 进程级取消句柄登记簿（对标 hyperopt 的 job.cancel 模式）：
// POST /analysis/jobs/:id/cancel 按 jobID 找到 context.CancelFunc 中止后台
// 变体回测。任务结束后 goroutine 自行注销；进程重启后遗留 running 任务无句柄，
// cancel 端点按孤儿任务直接落库 cancelled。
var (
	analysisJobCancels   = map[string]context.CancelFunc{}
	analysisJobCancelsMu sync.Mutex
)

func registerAnalysisJobCancel(jobID string, cancel context.CancelFunc) {
	analysisJobCancelsMu.Lock()
	analysisJobCancels[jobID] = cancel
	analysisJobCancelsMu.Unlock()
}

func unregisterAnalysisJobCancel(jobID string) {
	analysisJobCancelsMu.Lock()
	delete(analysisJobCancels, jobID)
	analysisJobCancelsMu.Unlock()
}

func takeAnalysisJobCancel(jobID string) context.CancelFunc {
	analysisJobCancelsMu.Lock()
	defer analysisJobCancelsMu.Unlock()
	return analysisJobCancels[jobID]
}

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

	jobCtx, cancelJob := context.WithCancel(context.Background())
	registerAnalysisJobCancel(jobID, cancelJob)

	go func() {
		defer unregisterAnalysisJobCancel(jobID)
		var resultJSON string
		var runErr error
		switch kind {
		case "lookahead":
			res, err := analysis.RunLookahead(jobCtx, cfg, factory, bars)
			if err != nil {
				runErr = err
			} else if data, err := json.Marshal(res); err != nil {
				runErr = err
			} else {
				resultJSON = string(data)
			}
		case "recursive":
			res, err := analysis.RunRecursive(jobCtx, cfg, factory, bars)
			if err != nil {
				runErr = err
			} else if data, err := json.Marshal(res); err != nil {
				runErr = err
			} else {
				resultJSON = string(data)
			}
		}
		status := "completed"
		errMsg := ""
		switch {
		case errors.Is(runErr, context.Canceled):
			status = "cancelled"
			errMsg = "任务已取消"
		case runErr != nil:
			status = "failed"
			errMsg = runErr.Error()
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

// analysisJobTerminal 判定终态（cancelled 也是终态：goroutine 已停写）。
func analysisJobTerminal(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled"
}

// loadOwnedAnalysisJob 取任务并做属主校验（cancel/delete 共用）；失败已写响应。
func loadOwnedAnalysisJob(c *gin.Context) *store.AnalysisJobRecord {
	jobID := c.Param("id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job id required"})
		return nil
	}
	if store.GetDB() == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return nil
	}
	job, err := store.NewAnalysisJobRepo().GetByID(jobID)
	if err != nil || job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return nil
	}
	if !requireOwner(c, job.UserID) {
		return nil
	}
	return job
}

// CancelAnalysisJob 取消运行中的分析任务（POST /api/analysis/jobs/:id/cancel）。
// 进程内任务经 context 取消（RunLookahead/RunRecursive 在逐变体边界检查）；
// 进程重启后遗留的 running 孤儿任务无句柄，直接落库 cancelled。
// 已终态任务返回 409（幂等语义：重复取消不算成功）。
func CancelAnalysisJob(c *gin.Context) {
	job := loadOwnedAnalysisJob(c)
	if job == nil {
		return
	}
	if analysisJobTerminal(job.Status) {
		c.JSON(http.StatusConflict, gin.H{"error": "任务已结束，无法取消", "status": job.Status})
		return
	}
	if cancelFn := takeAnalysisJobCancel(job.ID); cancelFn != nil {
		cancelFn() // goroutine 观察 ctx.Done 后自行落库 cancelled 并注销句柄
	} else {
		// 孤儿任务（重启遗留/句柄丢失）：无 goroutine 会写终态，直接落库。
		if err := store.NewAnalysisJobRepo().Finish(job.ID, "cancelled", "", "任务已取消（进程重启后标记）"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "取消失败: " + err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "cancelled", "job_id": job.ID})
}

// DeleteAnalysisJob 删除分析任务（DELETE /api/analysis/jobs/:id）。
// 仅终态（completed/failed/cancelled）可删；运行中任务须先取消。
func DeleteAnalysisJob(c *gin.Context) {
	job := loadOwnedAnalysisJob(c)
	if job == nil {
		return
	}
	if !analysisJobTerminal(job.Status) {
		c.JSON(http.StatusConflict, gin.H{"error": "任务运行中，请先取消", "status": job.Status})
		return
	}
	if err := store.NewAnalysisJobRepo().Delete(job.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted", "job_id": job.ID})
}
