package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Types ──

type executorStatusResp struct {
	Status          string  `json:"status"`
	ActivePositions int     `json:"active_positions"`
	PendingSignals  int     `json:"pending_signals"`
	TodayExecuted   int     `json:"today_executed"`
	TodayPnL        float64 `json:"today_pnl"`
	Tp1Executed     int     `json:"tp1_executed"`
	Tp2Executed     int     `json:"tp2_executed"`
	Tp3Executed     int     `json:"tp3_executed"`
	SlTriggered     int     `json:"sl_triggered"`
	UpdatedAt       int64   `json:"updated_at"`
}

type executorPosition struct {
	ID             string  `json:"id"`
	Symbol         string  `json:"symbol"`
	Side           string  `json:"side"`
	EntryPrice     float64 `json:"entry_price"`
	CurrentPrice   float64 `json:"current_price"`
	Quantity       float64 `json:"quantity"`
	RealizedPnL    float64 `json:"realized_pnl"`
	UnrealizedPnL  float64 `json:"unrealized_pnl"`
	MarginMode     string  `json:"margin_mode,omitempty"`
	PositionSide   string  `json:"position_side,omitempty"`
	Tp1Price       float64 `json:"tp1_price,omitempty"`
	Tp2Price       float64 `json:"tp2_price,omitempty"`
	Tp3Price       float64 `json:"tp3_price,omitempty"`
	SlPrice        float64 `json:"sl_price,omitempty"`
	Tp1Hit         bool    `json:"tp1_hit"`
	Tp2Hit         bool    `json:"tp2_hit"`
	Tp3Hit         bool    `json:"tp3_hit"`
	TrailingActive bool    `json:"trailing_active"`
	CurrentTP      float64 `json:"current_tp"`
	CurrentSL      float64 `json:"current_sl"`
}

type executionRecord struct {
	ID          string  `json:"id"`
	SignalID    int     `json:"signal_id"`
	SourceID    string  `json:"source_id"`
	Symbol      string  `json:"symbol"`
	Type        string  `json:"type"`   // entry | tp1 | tp2 | tp3 | sl | trailing
	Side        string  `json:"side"`   // LONG | SHORT
	Status      string  `json:"status"` // active | closed
	MarginMode  string  `json:"margin_mode,omitempty"`
	Price       float64 `json:"price"`
	Quantity    float64 `json:"quantity"`
	PnL         float64 `json:"pnl"`
	CloseReason string  `json:"close_reason,omitempty"`
	Trailing    bool    `json:"trailing_active"`
	CurrentTP   float64 `json:"current_tp"`
	CurrentSL   float64 `json:"current_sl"`
	ExecutedAt  int64   `json:"executed_at"`
}

type signalSource struct {
	ID               string      `json:"id"`
	Name             string      `json:"name"`
	Type             string      `json:"type"`
	Enabled          bool        `json:"enabled"`
	OwnerUserID      int         `json:"owner_user_id"`
	FeeModel         string      `json:"fee_model"`
	FeePercent       float64     `json:"fee_percent"`
	MonthlyFee       float64     `json:"monthly_fee"`
	WebhookURL       string      `json:"webhook_url,omitempty"`
	SignalCountToday int         `json:"signal_count_today"`
	SignalCountTotal int         `json:"signal_count_total"`
	TPSLConfig       *tpslConfig `json:"tp_sl_config,omitempty"`
}

type tpslConfig struct {
	Tp1Pct float64 `json:"tp1_pct"`
	Tp2Pct float64 `json:"tp2_pct"`
	Tp3Pct float64 `json:"tp3_pct"`
	SlPct  float64 `json:"sl_pct"`
}

// todayStartMs 返回本地时区今日 0 点的毫秒时间戳。
func todayStartMs() int64 {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()
}

// executionToRecord 执行记录 → 前端成交流水结构。
func executionToRecord(e *store.SignalExecution) executionRecord {
	recType := "entry"
	price := e.EntryPrice
	qty := e.EntryQty
	at := e.EntryTime
	if e.Status == "closed" {
		recType = e.CloseReason
		if recType == "" {
			recType = "closed"
		}
		price = 0
		at = e.ClosedAt
	}
	return executionRecord{
		ID:          e.ID,
		SignalID:    e.SignalID,
		SourceID:    e.SourceID,
		Symbol:      e.Symbol,
		Type:        recType,
		Side:        e.Direction,
		Status:      e.Status,
		MarginMode:  e.MarginMode,
		Price:       price,
		Quantity:    qty,
		PnL:         e.RealizedPnL,
		CloseReason: e.CloseReason,
		Trailing:    e.TrailingActive,
		CurrentTP:   e.CurrentTP,
		CurrentSL:   e.CurrentSL,
		ExecutedAt:  at,
	}
}

// ExecutorStatus godoc
// GET /executor/status
func ExecutorStatus(c *gin.Context) {
	startTPSLLoop()
	now := time.Now()
	dayStart := todayStartMs()
	sigRepo := store.NewSignalRepo()
	execRepo := store.NewSignalExecutionRepo()

	// 活跃持仓：组合持仓源（含 hedge 双腿 symbol-LONG/SHORT 双键）
	activePositions := 0
	if mgr := portfolio.GetManager(); mgr != nil {
		for _, p := range mgr.GetPositions() {
			if p.Quantity > 0 {
				activePositions++
			}
		}
	}

	// 今日成交的执行记录与盈亏
	todayPnL := 0.0
	todayExecuted := 0
	if todayExecs, err := execRepo.ListSince(dayStart, 0); err == nil {
		for _, e := range todayExecs {
			if e.Status == "closed" {
				todayExecuted++
				todayPnL += e.RealizedPnL
			}
		}
	}

	// 阶梯 TP/SL 累计触发计数（执行记录表聚合）
	tp1, tp2, tp3, sl := 0, 0, 0, 0
	if execs, err := execRepo.ListSince(0, 0); err == nil {
		for _, e := range execs {
			if e.TP1Filled {
				tp1++
			}
			if e.TP2Filled {
				tp2++
			}
			if e.TP3Filled {
				tp3++
			}
			if e.SLTriggered {
				sl++
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": executorStatusResp{
			Status:          "running",
			ActivePositions: activePositions,
			PendingSignals:  sigRepo.CountByStatusSince("PENDING", 0),
			TodayExecuted:   todayExecuted,
			TodayPnL:        todayPnL,
			Tp1Executed:     tp1,
			Tp2Executed:     tp2,
			Tp3Executed:     tp3,
			SlTriggered:     sl,
			UpdatedAt:       now.Unix(),
		},
	})
}

// ExecutorPositions godoc
// GET /executor/positions
// 真实持仓来自组合持仓管理器；阶梯 TP/SL 状态从活跃执行记录合并
// （CurrentTP/CurrentSL/tpN_hit/trailing）。
func ExecutorPositions(c *gin.Context) {
	startTPSLLoop()
	positions := []executorPosition{}

	// 活跃执行记录按持仓键索引（hedge 双腿各自的 symbol-LONG/SHORT）
	execByPos := map[string]*store.SignalExecution{}
	for _, e := range sigTPSLManager.activeExecutions() {
		execByPos[e.PositionID] = e
	}

	if mgr := portfolio.GetManager(); mgr != nil {
		for _, p := range mgr.GetPositions() {
			if p.Quantity <= 0 {
				continue
			}
			pos := executorPosition{
				ID:            p.ID,
				Symbol:        p.Symbol,
				Side:          p.Side,
				EntryPrice:    p.AvgEntryPrice,
				CurrentPrice:  p.CurrentPrice,
				Quantity:      p.Quantity,
				RealizedPnL:   p.RealizedPnL,
				UnrealizedPnL: p.UnrealizedPnL,
			}
			if p.MarginMode != "" {
				pos.MarginMode = string(p.MarginMode)
			}
			if p.PositionSide != "" {
				pos.PositionSide = string(p.PositionSide)
			}
			if exec, ok := execByPos[p.ID]; ok {
				pos.Side = exec.Direction
				pos.Tp1Price = exec.TP1Price
				pos.Tp2Price = exec.TP2Price
				pos.Tp3Price = exec.TP3Price
				pos.SlPrice = exec.SLPrice
				pos.Tp1Hit = exec.TP1Filled
				pos.Tp2Hit = exec.TP2Filled
				pos.Tp3Hit = exec.TP3Filled
				pos.TrailingActive = exec.TrailingActive
				pos.CurrentTP = exec.CurrentTP
				pos.CurrentSL = exec.CurrentSL
			}
			positions = append(positions, pos)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"positions": positions,
	})
}

// ExecutionRecords godoc
// GET /executor/records
// 真实成交流水：信号执行记录（入场 + 每步 TP/SL 推进的终态）。
func ExecutionRecords(c *gin.Context) {
	records := []executionRecord{}
	execs, err := store.NewSignalExecutionRepo().List(nil, 200)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "records": records})
		return
	}
	for _, e := range execs {
		records = append(records, executionToRecord(e))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"records": records,
	})
}

// ExecutorSignalSources godoc
// GET /executor/signal-sources
// 真实信号源列表（xt_signal_sources）+ 今日/累计信号计数（SignalRepo 聚合）。
func ExecutorSignalSources(c *gin.Context) {
	ensureExecutorSources()
	sources := []signalSource{}
	dayStart := todayStartMs()
	sigRepo := store.NewSignalRepo()

	srcs, err := store.NewSignalSourceRepo().List(false)
	if err != nil {
		srcs = nil
	}
	for _, s := range srcs {
		item := signalSource{
			ID:               s.ID,
			Name:             s.Name,
			Type:             s.Type,
			Enabled:          s.Enabled,
			OwnerUserID:      s.OwnerUserID,
			FeeModel:         s.FeeModel,
			FeePercent:       s.FeePercent,
			MonthlyFee:       s.MonthlyFee,
			WebhookURL:       "/api/webhook/generic",
			SignalCountToday: sigRepo.CountBySourceSince(s.ID, dayStart),
			SignalCountTotal: sigRepo.CountBySourceSince(s.ID, 0),
		}
		var cfg tpslConfig
		if err := json.Unmarshal([]byte(s.TPSLJSON), &cfg); err == nil && (cfg.Tp1Pct > 0 || cfg.SlPct > 0) {
			item.TPSLConfig = &cfg
		}
		sources = append(sources, item)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"sources": sources,
	})
}
