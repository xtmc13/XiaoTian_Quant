package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 组合回测（A6.2） ──
// POST /api/backtests/portfolio 同步执行组合回测并落库 xt_portfolio_backtests。
// 每个 leg 用既有的单策略事件驱动回测（newBacktestStrategy 目录），
// 组合层面按权重合成权益曲线（backtest.RunPortfolio）。

type portfolioBacktestRequest struct {
	Name           string  `json:"name"`
	Timeframe      string  `json:"timeframe"`
	Start          string  `json:"start"`
	End            string  `json:"end"`
	InitialCapital float64 `json:"initial_capital"`
	Rebalance      string  `json:"rebalance"`
	Legs           []struct {
		StrategyType string         `json:"strategy_type"`
		Symbol       string         `json:"symbol"`
		Weight       float64        `json:"weight"`
		Params       map[string]any `json:"params"`
	} `json:"legs"`
}

func (r *portfolioBacktestRequest) validate() error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		r.Name = "组合回测"
	}
	if r.Timeframe == "" {
		r.Timeframe = "1h"
	}
	if r.InitialCapital <= 0 {
		r.InitialCapital = 100000
	}
	switch r.Rebalance {
	case "", "none", "daily", "weekly", "monthly":
	default:
		return fmt.Errorf("rebalance 必须是 none|daily|weekly|monthly")
	}
	if len(r.Legs) == 0 {
		return fmt.Errorf("至少需要一条 leg")
	}
	if len(r.Legs) > 20 {
		return fmt.Errorf("最多支持 20 条 leg")
	}
	for i := range r.Legs {
		leg := &r.Legs[i]
		leg.Symbol = strings.ToUpper(strings.TrimSpace(leg.Symbol))
		if leg.Symbol == "" {
			return fmt.Errorf("leg %d: symbol 不能为空", i+1)
		}
		if leg.StrategyType == "" {
			return fmt.Errorf("leg %d: strategy_type 不能为空", i+1)
		}
		if leg.Weight <= 0 {
			leg.Weight = 1
		}
	}
	return nil
}

func (r *portfolioBacktestRequest) window() (fromMs, toMs int64, err error) {
	if r.Start != "" {
		t, err2 := time.Parse("2006-01-02", r.Start)
		if err2 != nil {
			return 0, 0, fmt.Errorf("start 格式应为 YYYY-MM-DD")
		}
		fromMs = t.UnixMilli()
	}
	if r.End != "" {
		t, err2 := time.Parse("2006-01-02", r.End)
		if err2 != nil {
			return 0, 0, fmt.Errorf("end 格式应为 YYYY-MM-DD")
		}
		toMs = t.UnixMilli()
	}
	return
}

// RunPortfolioBacktest 执行组合回测并落库（AuthRequired + user_id 属主）。
func RunPortfolioBacktest(c *gin.Context) {
	var req portfolioBacktestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json: " + err.Error()})
		return
	}
	if err := req.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	fromMs, toMs, err := req.window()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	legs := make([]backtest.PortfolioLeg, len(req.Legs))
	for i, l := range req.Legs {
		legs[i] = backtest.PortfolioLeg{
			StrategyType: l.StrategyType,
			Symbol:       l.Symbol,
			Weight:       l.Weight,
			Params:       l.Params,
		}
	}

	result, err := backtest.RunPortfolio(backtest.PortfolioConfig{
		Name:           req.Name,
		Timeframe:      req.Timeframe,
		Start:          fromMs,
		End:            toMs,
		InitialCapital: req.InitialCapital,
		Rebalance:      req.Rebalance,
		Legs:           legs,
	}, func(leg backtest.PortfolioLeg) (backtest.BacktestStrategy, error) {
		return newBacktestStrategy(leg.StrategyType, leg.Symbol), nil
	}, func(symbol, interval string, from, to int64) ([]model.Bar, error) {
		bars, _, err := loadResearchBars(symbol, interval, 1500, from, to)
		return bars, err
	})
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "至少需要 50 根") || strings.Contains(err.Error(), "未返回") {
			status = http.StatusBadRequest
		} else if strings.Contains(err.Error(), "Binance") {
			status = http.StatusBadGateway
		}
		c.JSON(status, gin.H{"error": "组合回测执行失败", "detail": err.Error()})
		return
	}

	// 权益曲线降采样到 ≤600 点再入库（完整曲线在响应里给前端）
	equity := result.EquityCurve
	if len(equity) > 600 {
		equity = samplePortfolioEquity(equity, 600)
	}
	report := backtest.GenerateReport(result.Metrics, "portfolio", result.Name)
	report.EquitySampled = equity

	legsJSON, _ := json.Marshal(result.Legs)
	resultJSON, _ := json.Marshal(gin.H{
		"report":       report,
		"equity_curve": equity,
		"drift":        result.Drift,
		"legs":         result.Legs,
		"duration_ms":  result.DurationMs,
	})

	userID := getUserID(c)
	rec := &store.PortfolioBacktestRecord{
		ID:             result.ID,
		UserID:         userID,
		Name:           result.Name,
		Timeframe:      req.Timeframe,
		Rebalance:      req.Rebalance,
		StartTime:      fromMs,
		EndTime:        toMs,
		InitialCapital: req.InitialCapital,
		FinalEquity:    report.FinalEquity,
		TotalReturnPct: round2(report.TotalReturnPct),
		MaxDrawdownPct: round2(report.MaxDrawdownPct),
		SharpeRatio:    round2(report.SharpeRatio),
		SortinoRatio:   round2(report.SortinoRatio),
		CalmarRatio:    round2(report.CalmarRatio),
		WinRate:        round2(report.WinRate),
		ProfitFactor:   round2(report.ProfitFactor),
		TotalTrades:    report.TotalTrades,
		LegsJSON:       string(legsJSON),
		ResultJSON:     string(resultJSON),
		DurationMs:     result.DurationMs,
	}
	if err := store.NewPortfolioBacktestRepo().Create(rec); err != nil {
		fmt.Printf("[portfolio-backtest] persist failed: %v\n", err)
	}

	// 通知
	if broadcaster := notify.NewBroadcaster(); broadcaster != nil {
		go broadcaster.Backtest(result.Name, "portfolio", map[string]any{
			"total_return_pct": report.TotalReturnPct,
			"max_drawdown_pct": report.MaxDrawdownPct,
			"sharpe_ratio":     report.SharpeRatio,
		}, 0)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":           result.ID,
		"name":         result.Name,
		"created_at":   rec.CreatedAt,
		"report":       report,
		"equity_curve": result.EquityCurve,
		"drift":        result.Drift,
		"legs":         result.Legs,
		"duration_ms":  result.DurationMs,
	})
}

// samplePortfolioEquity 等间隔降采样权益曲线。
func samplePortfolioEquity(curve []backtest.EquityPoint, maxPoints int) []backtest.EquityPoint {
	if len(curve) <= maxPoints {
		return curve
	}
	step := float64(len(curve)-1) / float64(maxPoints-1)
	out := make([]backtest.EquityPoint, maxPoints)
	for i := 0; i < maxPoints; i++ {
		idx := int(float64(i) * step)
		if idx >= len(curve) {
			idx = len(curve) - 1
		}
		out[i] = curve[idx]
	}
	return out
}

// ListPortfolioBacktests 返回当前用户的组合回测历史。
func ListPortfolioBacktests(c *gin.Context) {
	userID := getUserID(c)
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = min(n, 200)
		}
	}
	recs, err := store.NewPortfolioBacktestRepo().ListByUser(userID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to list portfolio backtests"})
		return
	}
	if recs == nil {
		recs = []*store.PortfolioBacktestRecord{}
	}
	c.JSON(http.StatusOK, gin.H{"backtests": recs})
}

// GetPortfolioBacktest 返回单条组合回测详情（属主校验）。
func GetPortfolioBacktest(c *gin.Context) {
	id := c.Param("id")
	rec, err := store.NewPortfolioBacktestRepo().GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	if !requireOwner(c, rec.UserID) {
		return
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(rec.ResultJSON), &result); err != nil {
		result = map[string]any{}
	}
	var legs []map[string]any
	if err := json.Unmarshal([]byte(rec.LegsJSON), &legs); err != nil {
		legs = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{
		"id":              rec.ID,
		"name":            rec.Name,
		"timeframe":       rec.Timeframe,
		"rebalance":       rec.Rebalance,
		"start_time":      rec.StartTime,
		"end_time":        rec.EndTime,
		"initial_capital": rec.InitialCapital,
		"final_equity":    rec.FinalEquity,
		"metrics": gin.H{
			"total_return_pct": rec.TotalReturnPct,
			"max_drawdown_pct": rec.MaxDrawdownPct,
			"sharpe_ratio":     rec.SharpeRatio,
			"sortino_ratio":    rec.SortinoRatio,
			"calmar_ratio":     rec.CalmarRatio,
			"win_rate":         rec.WinRate,
			"profit_factor":    rec.ProfitFactor,
			"total_trades":     rec.TotalTrades,
		},
		"legs":        legs,
		"result":      result,
		"duration_ms": rec.DurationMs,
		"created_at":  rec.CreatedAt,
	})
}

// DeletePortfolioBacktest 删除单条（属主校验）。
func DeletePortfolioBacktest(c *gin.Context) {
	id := c.Param("id")
	repo := store.NewPortfolioBacktestRepo()
	rec, err := repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	if !requireOwner(c, rec.UserID) {
		return
	}
	if err := repo.DeleteByID(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to delete"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func round2(v float64) float64 { return store.RoundFloat(v, 2) }
