package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

var binanceClient = &http.Client{Timeout: 10 * time.Second}

func fetchBinanceKlines(symbol, interval string, limit int, fromMs, toMs int64) ([]map[string]any, error) {
	url := fmt.Sprintf("https://api.binance.com/api/v3/klines?symbol=%s&interval=%s&limit=%d",
		symbol, interval, limit)
	if fromMs > 0 && toMs > fromMs {
		url += fmt.Sprintf("&startTime=%d&endTime=%d", fromMs, toMs)
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := binanceClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var raw [][]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	klines := make([]map[string]any, 0, len(raw))
	for _, k := range raw {
		klines = append(klines, map[string]any{
			"time":   int64(k[0].(float64)),
			"open":   parseFloat(k[1]),
			"high":   parseFloat(k[2]),
			"low":    parseFloat(k[3]),
			"close":  parseFloat(k[4]),
			"volume": parseFloat(k[5]),
		})
	}
	return klines, nil
}

func parseFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	}
	return 0
}

func GetKlines(c *gin.Context) {
	symbol := c.Param("symbol")
	interval := c.DefaultQuery("interval", "1h")
	limitStr := c.DefaultQuery("limit", "200")
	limit, _ := strconv.Atoi(limitStr)

	// Accept both from_val/from and to_val/to
	fromVal := c.Query("from_val")
	if fromVal == "" {
		fromVal = c.Query("from")
	}
	toVal := c.Query("to_val")
	if toVal == "" {
		toVal = c.Query("to")
	}

	var fromMs, toMs int64
	if v, err := strconv.ParseInt(fromVal, 10, 64); err == nil {
		fromMs = v
	}
	if v, err := strconv.ParseInt(toVal, 10, 64); err == nil {
		toMs = v
	}

	// Fetch real Binance data only — no mock fallback
	klines, err := fetchBinanceKlines(symbol, interval, limit, fromMs, toMs)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch market data", "detail": err.Error()})
		return
	}
	if len(klines) == 0 {
		c.JSON(http.StatusOK, []map[string]any{})
		return
	}
	c.JSON(http.StatusOK, klines)
}

// MarketKlines returns klines with symbol from query param (for klinecharts-pro).
func MarketKlines(c *gin.Context) {
	symbol := c.DefaultQuery("symbol", "BTCUSDT")
	interval := c.DefaultQuery("interval", "1h")
	limitStr := c.DefaultQuery("limit", "200")
	limit, _ := strconv.Atoi(limitStr)

	var fromMs, toMs int64
	if v, err := strconv.ParseInt(c.Query("from"), 10, 64); err == nil {
		fromMs = v
	}
	if v, err := strconv.ParseInt(c.Query("to"), 10, 64); err == nil {
		toMs = v
	}

	klines, err := fetchBinanceKlines(symbol, interval, limit, fromMs, toMs)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch market data", "detail": err.Error()})
		return
	}
	if klines == nil {
		klines = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{"klines": klines, "symbol": symbol})
}

// OrderBook returns a mock order book for a symbol.
func OrderBook(c *gin.Context) {
	symbol := c.DefaultQuery("symbol", "BTCUSDT")
	depth := 15
	if d := c.Query("depth"); d != "" {
		if v, err := strconv.Atoi(d); err == nil && v > 0 && v <= 50 {
			depth = v
		}
	}
	basePrice := 68000.0
	if strings.Contains(symbol, "ETH") {
		basePrice = 3500.0
	}
	bids := make([][]float64, depth)
	asks := make([][]float64, depth)
	for i := 0; i < depth; i++ {
		bidPrice := basePrice - float64(i+1)*basePrice*0.0001
		askPrice := basePrice + float64(i+1)*basePrice*0.0001
		bidQty := 0.5 + float64(depth-i)*0.3
		askQty := 0.5 + float64(i+1)*0.3
		bids[i] = []float64{store.RoundFloat(bidPrice, 2), store.RoundFloat(bidQty, 4)}
		asks[i] = []float64{store.RoundFloat(askPrice, 2), store.RoundFloat(askQty, 4)}
	}
	c.JSON(http.StatusOK, gin.H{"bids": bids, "asks": asks, "symbol": symbol})
}

// MarketTrades returns recent public trades for a symbol.
func MarketTrades(c *gin.Context) {
	symbol := c.DefaultQuery("symbol", "BTCUSDT")
	limit := 30
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	basePrice := 68000.0
	if strings.Contains(symbol, "ETH") {
		basePrice = 3500.0
	}
	now := time.Now()
	trades := make([]map[string]any, limit)
	for i := 0; i < limit; i++ {
		side := "BUY"
		if i%2 == 0 {
			side = "SELL"
		}
		price := basePrice + (float64(i%7)-3.0)*basePrice*0.00015
		trades[i] = map[string]any{
			"price":          store.RoundFloat(price, 2),
			"qty":            store.RoundFloat(0.01+float64(i%5)*0.02, 4),
			"quantity":       store.RoundFloat(0.01+float64(i%5)*0.02, 4),
			"side":           side,
			"is_buyer_maker": side == "SELL",
			"time":           now.Add(-time.Duration(i) * time.Second).UnixMilli(),
			"timestamp":      now.Add(-time.Duration(i) * time.Second).UnixMilli(),
		}
	}
	c.JSON(http.StatusOK, gin.H{"trades": trades, "symbol": symbol})
}

func RunBacktest(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	symbol := getString(body, "symbol", "BTCUSDT")
	initialBalance := 100000.0
	if ib, ok := body["initial_balance"].(map[string]any); ok {
		if v, ok := ib["USDT"].(float64); ok {
			initialBalance = v
		}
	} else if v, ok := body["initial_balance"].(float64); ok {
		initialBalance = v
	}
	interval := getString(body, "interval", "1h")
	strategyType := getString(body, "strategy_type", "sma_cross")

	// Parse date range: "from" / "to" in ISO format (e.g. "2024-01-01")
	// If not provided, use num_bars to determine lookback
	var fromMs, toMs int64
	if fromStr := getString(body, "from", ""); fromStr != "" {
		if t, err := time.Parse("2006-01-02", fromStr); err == nil {
			fromMs = t.UnixMilli()
		}
	}
	if toStr := getString(body, "to", ""); toStr != "" {
		if t, err := time.Parse("2006-01-02", toStr); err == nil {
			toMs = t.UnixMilli()
		}
	}

	// Fallback: use num_bars if no date range specified
	numBars := int(getFloat(body, "num_bars", 500))
	if fromMs == 0 && toMs == 0 {
		numBars = min(max(numBars, 50), 1500)
	}

	// Fetch real historical klines from Binance
	klines, err := fetchBinanceKlines(symbol, interval, numBars, fromMs, toMs)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error":  "无法获取历史数据",
			"detail": fmt.Sprintf("从 Binance 获取 %s %s K线失败: %v", symbol, interval, err),
			"source": "Binance",
		})
		return
	}
	if len(klines) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "数据为空",
			"detail": fmt.Sprintf("Binance 未返回 %s %s 的K线数据，请检查交易对和时间范围是否正确", symbol, interval),
			"source": "Binance",
		})
		return
	}
	if len(klines) < 50 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "数据不足",
			"detail": fmt.Sprintf("仅获取到 %d 根K线，至少需要 50 根才能回测。请扩大日期范围", len(klines)),
			"source": "Binance",
		})
		return
	}

	// Convert to model.Bar
	bars := make([]model.Bar, 0, len(klines))
	for _, k := range klines {
		bars = append(bars, model.Bar{
			Symbol:   symbol,
			Open:     getFloat(k, "open", 0),
			High:     getFloat(k, "high", 0),
			Low:      getFloat(k, "low", 0),
			Close:    getFloat(k, "close", 0),
			Volume:   getFloat(k, "volume", 0),
			Interval: interval,
			Time:     int64(getFloat(k, "time", 0)),
		})
	}

	// Setup runner
	cfg := backtest.DefaultRunnerConfig()
	cfg.InitialBalance = initialBalance
	runner := backtest.NewRunner(cfg)
	runner.LoadBars(symbol, bars)

	// Select strategy
	var strategy backtest.BacktestStrategy
	switch strategyType {
	case "breakout":
		strategy = &breakoutBTStrategy{symbol: symbol, lookback: 20, bufferPct: 0.002, stopLossPct: 0.02, takeProfitPct: 0.04}
	default:
		strategy = &smaCrossStrategy{symbol: symbol, fastPeriod: 12, slowPeriod: 26}
	}

	result, err := runner.Run(strategy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "backtest execution failed", "detail": err.Error()})
		return
	}

	// Convert equity curve
	equityCurve := make([]map[string]any, 0, len(result.EquityCurve))
	for _, pt := range result.EquityCurve {
		equityCurve = append(equityCurve, map[string]any{
			"time":   pt.Timestamp,
			"equity": store.RoundFloat(pt.Equity, 2),
		})
	}

	// Convert trades
	trades := make([]map[string]any, 0, len(result.Trades))
	for _, t := range result.Trades {
		trades = append(trades, map[string]any{
			"entry_price": t.EntryPrice,
			"exit_price":  t.ExitPrice,
			"qty":         t.Quantity,
			"side":        t.Side,
			"pnl":         store.RoundFloat(t.RealizedPnL, 2),
			"time":        t.EntryTime,
			"exit_time":   t.ExitTime,
			"reason":      t.ExitReason,
		})
	}

	finalEquity := initialBalance
	if len(result.EquityCurve) > 0 {
		finalEquity = result.EquityCurve[len(result.EquityCurve)-1].Equity
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"source": "Binance",
		"params": gin.H{
			"symbol":          symbol,
			"interval":        interval,
			"strategy_type":   strategyType,
			"initial_balance": initialBalance,
			"bars_used":       len(klines),
			"from":            time.UnixMilli(klines[0]["time"].(int64)).Format("2006-01-02"),
			"to":              time.UnixMilli(klines[len(klines)-1]["time"].(int64)).Format("2006-01-02"),
		},
		"report": gin.H{
			"initial_balance":  initialBalance,
			"final_equity":     store.RoundFloat(finalEquity, 2),
			"total_return_pct": store.RoundFloat(result.TotalReturnPct, 2),
			"max_drawdown_pct": store.RoundFloat(result.MaxDrawdownPct, 2),
			"sharpe_ratio":     store.RoundFloat(result.SharpeRatio, 2),
			"win_rate_pct":     store.RoundFloat(result.WinRate, 1),
			"total_trades":     result.TotalTrades,
			"profit_factor":    store.RoundFloat(result.ProfitFactor, 2),
		},
		"equity_curve": equityCurve,
		"trades":       trades,
	})
}

func NativeBacktest(c *gin.Context) {
	RunBacktest(c)
}

func SymbolSearch(c *gin.Context) {
	q := strings.ToUpper(c.Query("q"))
	var results []string
	for _, s := range store.SymbolList {
		if strings.Contains(s, q) {
			results = append(results, s)
			if len(results) >= 30 {
				break
			}
		}
	}
	if results == nil {
		results = []string{}
	}
	c.JSON(http.StatusOK, results)
}

func Status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"runtime_seconds": int(time.Now().Unix()),
		"strategies":      gin.H{}, "exchanges": gin.H{},
	})
}

func Chart(c *gin.Context) {
	symbol := c.Query("symbol")
	if symbol == "" {
		symbol = "BTCUSDT"
	}
	c.JSON(http.StatusOK, gin.H{"symbol": symbol, "data": []any{}})
}

// ── Helpers ──

func getString(m map[string]any, key, def string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return def
}

func getFloat(m map[string]any, key string, def float64) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case string:
		var f float64
		fmt.Sscanf(v, "%f", &f)
		return f
	}
	return def
}

func safeLast(arr []map[string]any, n int) []map[string]any {
	if len(arr) <= n {
		return arr
	}
	return arr[len(arr)-n:]
}

// ── Backtest Strategies ──

type smaCrossStrategy struct {
	symbol     string
	fastPeriod int
	slowPeriod int
}

func (s *smaCrossStrategy) Name() string   { return "sma_cross" }
func (s *smaCrossStrategy) Symbol() string { return s.symbol }
func (s *smaCrossStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < s.slowPeriod+1 {
		return nil, nil
	}
	fast := sma(bars, s.fastPeriod)
	slow := sma(bars, s.slowPeriod)
	prevFast := sma(bars[:len(bars)-1], s.fastPeriod)
	prevSlow := sma(bars[:len(bars)-1], s.slowPeriod)

	if prevFast <= prevSlow && fast > slow && state.Position == nil {
		return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "sma golden cross"}, nil
	}
	if prevFast >= prevSlow && fast < slow && state.Position != nil {
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "sma death cross"}, nil
	}
	return nil, nil
}
func (s *smaCrossStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}

type breakoutBTStrategy struct {
	symbol        string
	lookback      int
	bufferPct     float64
	stopLossPct   float64
	takeProfitPct float64
}

func (s *breakoutBTStrategy) Name() string   { return "breakout" }
func (s *breakoutBTStrategy) Symbol() string { return s.symbol }
func (s *breakoutBTStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < s.lookback+2 {
		return nil, nil
	}
	if state.Position != nil {
		return s.checkExit(bar, state), nil
	}
	highest, lowest := rangeHighLow(bars[:len(bars)-1], s.lookback)
	if highest <= 0 || lowest <= 0 {
		return nil, nil
	}
	rangeSize := highest - lowest
	buffer := rangeSize * s.bufferPct
	if bar.Close > highest+buffer {
		return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "breakout above resistance"}, nil
	}
	if bar.Close < lowest-buffer {
		return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "breakdown below support"}, nil
	}
	return nil, nil
}
func (s *breakoutBTStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *breakoutBTStrategy) checkExit(bar model.Bar, state *backtest.StrategyState) *model.Signal {
	if state.Position == nil {
		return nil
	}
	entryPrice := state.Position.EntryPrice
	if state.Position.Side == model.SideBuy {
		if bar.Close <= entryPrice*(1-s.stopLossPct) {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "long stop loss"}
		}
		if bar.Close >= entryPrice*(1+s.takeProfitPct) {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "long take profit"}
		}
	} else {
		if bar.Close >= entryPrice*(1+s.stopLossPct) {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "short stop loss"}
		}
		if bar.Close <= entryPrice*(1-s.takeProfitPct) {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "short take profit"}
		}
	}
	return nil
}

// sma calculates simple moving average of closes.
func sma(bars []model.Bar, period int) float64 {
	if len(bars) < period {
		period = len(bars)
	}
	if period == 0 {
		return 0
	}
	sum := 0.0
	for i := len(bars) - period; i < len(bars); i++ {
		sum += bars[i].Close
	}
	return sum / float64(period)
}

// rangeHighLow calculates highest high and lowest low over the last N bars.
func rangeHighLow(bars []model.Bar, period int) (float64, float64) {
	if period > len(bars) {
		period = len(bars)
	}
	if period == 0 {
		return 0, 0
	}
	start := len(bars) - period
	if start < 0 {
		start = 0
	}
	highest := bars[start].High
	lowest := bars[start].Low
	for i := start + 1; i < len(bars); i++ {
		if bars[i].High > highest {
			highest = bars[i].High
		}
		if bars[i].Low < lowest {
			lowest = bars[i].Low
		}
	}
	return highest, lowest
}
