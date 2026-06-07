package indicator

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// BacktestIndicator godoc
// POST /api/indicator/backtest
// Runs a single backtest for an indicator with given parameters and kline data.
func BacktestIndicator(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	_, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized", "data": nil})
		return
	}

	var req struct {
		Code          string            `json:"code" binding:"required"`
		Symbol        string            `json:"symbol"`
		Interval      string            `json:"interval"`
		Klines        []map[string]any  `json:"klines"`
		Params        map[string]any    `json:"params"`
		BacktestConfig backtest.RunnerConfig `json:"backtest_config"`
		StartDate     string            `json:"start_date"`
		EndDate       string            `json:"end_date"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": err.Error(), "data": nil})
		return
	}

	if req.Symbol == "" {
		req.Symbol = "BTCUSDT"
	}
	if req.Interval == "" {
		req.Interval = "1h"
	}

	// Parse klines to bars
	bars := klinesToBars(req.Klines, req.Symbol, req.Interval)
	if len(bars) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "no bar data provided", "data": nil})
		return
	}

	// Apply date filters if provided
	if req.StartDate != "" || req.EndDate != "" {
		bars = filterBarsByDate(bars, req.StartDate, req.EndDate)
	}
	if len(bars) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "insufficient bars after date filter", "data": nil})
		return
	}

	// Default backtest config
	btCfg := req.BacktestConfig
	if btCfg.InitialBalance <= 0 {
		btCfg.InitialBalance = 10000
	}
	if btCfg.Commission <= 0 {
		btCfg.Commission = 0.001
	}
	if btCfg.Slippage <= 0 {
		btCfg.Slippage = 0.0005
	}

	// Build strategy from indicator code
	strategy := NewIndicatorStrategy(req.Code, req.Symbol, req.Params)
	if err := strategy.Precompute(bars); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "indicator execution failed: " + err.Error(), "data": nil})
		return
	}

	// Run backtest
	runner := backtest.NewRunner(btCfg)
	runner.LoadBars(req.Symbol, bars)
	result, err := runner.Run(strategy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "backtest failed: " + err.Error(), "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 1,
		"msg":  "backtest completed",
		"data": result,
	})
}

// klinesToBars converts frontend kline format to model.Bar.
func klinesToBars(klines []map[string]any, symbol, interval string) []model.Bar {
	var bars []model.Bar
	for _, k := range klines {
		b := model.Bar{Symbol: symbol, Interval: interval}
		if v, ok := k["open"].(float64); ok {
			b.Open = v
		}
		if v, ok := k["high"].(float64); ok {
			b.High = v
		}
		if v, ok := k["low"].(float64); ok {
			b.Low = v
		}
		if v, ok := k["close"].(float64); ok {
			b.Close = v
		}
		if v, ok := k["volume"].(float64); ok {
			b.Volume = v
		}
		if v, ok := k["time"].(float64); ok {
			b.Time = int64(v)
		} else if v, ok := k["timestamp"].(float64); ok {
			b.Time = int64(v)
		} else if v, ok := k["time"].(int64); ok {
			b.Time = v
		} else if v, ok := k["timestamp"].(int64); ok {
			b.Time = v
		}
		bars = append(bars, b)
	}
	return bars
}

// filterBarsByDate filters bars by date range (YYYY-MM-DD format).
func filterBarsByDate(bars []model.Bar, startDate, endDate string) []model.Bar {
	var startMs, endMs int64
	if startDate != "" {
		t, _ := time.Parse("2006-01-02", startDate)
		startMs = t.UnixMilli()
	}
	if endDate != "" {
		t, _ := time.Parse("2006-01-02", endDate)
		endMs = t.UnixMilli() + 86400000 // include full day
	}

	var filtered []model.Bar
	for _, b := range bars {
		if startMs > 0 && b.Time < startMs {
			continue
		}
		if endMs > 0 && b.Time > endMs {
			continue
		}
		filtered = append(filtered, b)
	}
	return filtered
}

// NewIndicatorStrategy creates a strategy from indicator code (re-export from experiment package).
// We duplicate the minimal logic here to avoid circular imports.
type indicatorStrategy struct {
	code        string
	symbol      string
	params      map[string]any
	client      *SandboxConfig
	buySignals  []bool
	sellSignals []bool
}

func NewIndicatorStrategy(code, symbol string, params map[string]any) *indicatorStrategy {
	return &indicatorStrategy{
		code:   code,
		symbol: symbol,
		params: params,
		client: DefaultSandboxConfig(),
	}
}

func (s *indicatorStrategy) Precompute(bars []model.Bar) error {
	dfJSON := make([]map[string]any, len(bars))
	for i, b := range bars {
		dfJSON[i] = map[string]any{
			"open":   b.Open,
			"high":   b.High,
			"low":    b.Low,
			"close":  b.Close,
			"volume": b.Volume,
		}
	}

	resp, err := s.client.Execute(s.code, s.params, dfJSON)
	if err != nil {
		return err
	}
	if resp == nil || !resp.Success {
		return err
	}

	// Extract signals from output
	outputJSON, _ := json.Marshal(resp.Output)
	output, _ := ValidateOutputJSON(string(outputJSON))

	s.buySignals = make([]bool, len(bars))
	s.sellSignals = make([]bool, len(bars))

	for _, sig := range output.Signals {
		if len(sig.Data) != len(bars) {
			continue
		}
		for i, v := range sig.Data {
			if v == nil {
				continue
			}
			if sig.Type == "buy" {
				s.buySignals[i] = true
			} else if sig.Type == "sell" {
				s.sellSignals[i] = true
			}
		}
	}

	return nil
}

func (s *indicatorStrategy) Name() string { return "indicator" }
func (s *indicatorStrategy) Symbol() string { return s.symbol }

func (s *indicatorStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	idx := state.BarIndex
	if idx < 0 || idx >= len(s.buySignals) {
		return nil, nil
	}

	if s.buySignals[idx] {
		if state.Position == nil || state.Position.IsClosed {
			return &model.Signal{
				Symbol:    s.symbol,
				Direction: "LONG",
				Reason:    "indicator_buy",
			}, nil
		}
	}

	if s.sellSignals[idx] {
		if state.Position != nil && !state.Position.IsClosed {
			return &model.Signal{
				Symbol:    s.symbol,
				Direction: "CLOSE",
				Reason:    "indicator_sell",
			}, nil
		}
	}

	return nil, nil
}

func (s *indicatorStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}
