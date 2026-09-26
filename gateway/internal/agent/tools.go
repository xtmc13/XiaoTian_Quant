package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/service"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── 工具层 ──
// 16 个 MCP 工具的真实实现。读类工具返回真实数据（币安行情、portfolio 余额/持仓、
// store 订单/策略/机器人/回测记录），写类工具走真实撮合与策略引擎。
// 实盘下单类工具本期不做（仅 paper）。

// ── 依赖抽象（main.go 注入真实实现，测试注入 mock）──

// MarketDataSource 行情数据源（生产为币安公共 REST）。
type MarketDataSource interface {
	// Ticker24h 返回币安 24hr 统计（原始字段，字符串数值）。
	Ticker24h(ctx context.Context, symbol string) (map[string]any, error)
	// BookTicker 返回最优买卖价。
	BookTicker(ctx context.Context, symbol string) (bid, ask float64, err error)
	// Klines 返回历史 K 线，元素键：timestamp/open/high/low/close/volume。
	Klines(ctx context.Context, symbol, interval string, limit int, fromMs, toMs int64) ([]map[string]any, error)
	// ExchangeSymbols 返回全部交易中的现货交易对。
	ExchangeSymbols(ctx context.Context) ([]string, error)
}

// OrderMatcher 模拟撮合下单（生产为 *service.MatchingService）。
type OrderMatcher interface {
	PlaceOrder(symbol, side, orderType string, price, quantity float64, userID uint64, storeOrderID string) (map[string]any, error)
	CancelOrder(symbol string, storeOrderID string) error
}

// PortfolioReader 组合账本读取（生产为 *portfolio.Manager 适配器）。
type PortfolioReader interface {
	TotalEquity() float64
	TotalPnL() float64
	// Balances 返回账户余额（accountID 通常为 "default"）。
	Balances(accountID string) []*model.Balance
	// Positions 返回当前持仓。
	Positions() []*model.PositionData
}

// StrategyRuntime 策略引擎运行时（生产为 *strategy.Engine）。
type StrategyRuntime interface {
	List() []string
	Get(name string) strategy.Strategy
	Register(s strategy.Strategy) error
	Unregister(name string) error
	Start(name string, params map[string]any) error
	Stop(name string) error
}

// ── ToolContext ──

// ToolContext 工具依赖容器，main.go 启动时注入一次。
type ToolContext struct {
	UserID    uint64           // 当前请求用户（Web JWT 或 agent token 解析出）
	Market    MarketDataSource // 行情
	Matcher   OrderMatcher     // paper 撮合下单/撤单
	Portfolio PortfolioReader  // 余额/持仓/权益
	Engine    StrategyRuntime  // 策略引擎启停
}

// ToolDeps 供 main.go 用真实单例覆盖默认装配；零值字段回落到包内默认实现。
type ToolDeps struct {
	Market    MarketDataSource
	Matcher   OrderMatcher
	Portfolio PortfolioReader
	Engine    StrategyRuntime
}

// NewDefaultToolContext 装配工具上下文：deps 中的非零字段优先，
// 零值字段使用全局单例（币安 REST / MatchingService / Portfolio / 策略引擎）。
func NewDefaultToolContext(deps ToolDeps) *ToolContext {
	tc := &ToolContext{
		Market:    deps.Market,
		Matcher:   deps.Matcher,
		Portfolio: deps.Portfolio,
		Engine:    deps.Engine,
	}
	if tc.Market == nil {
		tc.Market = &binanceLiveData{client: &http.Client{Timeout: 10 * time.Second}}
	}
	if tc.Matcher == nil {
		tc.Matcher = service.GetMatchingService()
	}
	if tc.Portfolio == nil {
		tc.Portfolio = portfolioAdapter{mgr: portfolio.GetManager()}
	}
	if tc.Engine == nil {
		tc.Engine = safeEngine{eng: strategy.GetEngine(nil)}
	}
	return tc
}

// ── 全局工具上下文（HTTP 侧调用方按请求覆盖 UserID）──

var (
	stdToolCtx     *ToolContext
	stdToolCtxOnce sync.Once
)

// SetToolContext 注入全局工具上下文（main.go 启动时调用一次）。
func SetToolContext(tc *ToolContext) {
	stdToolCtx = tc
}

// GetToolContext 返回全局工具上下文；未注入时用默认单例懒装配。
func GetToolContext() *ToolContext {
	stdToolCtxOnce.Do(func() {
		if stdToolCtx == nil {
			stdToolCtx = NewDefaultToolContext(ToolDeps{})
		}
	})
	return stdToolCtx
}

// ── 默认实现 ──

// binanceLiveData 币安公共 REST 行情（复用 handler/ai.go、handler/market.go 的真实调用）。
type binanceLiveData struct {
	client *http.Client
}

const binanceBaseURL = "https://api.binance.com"

func (b *binanceLiveData) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance %s: %s", resp.Status, truncateStr(string(body), 200))
	}
	return body, nil
}

func (b *binanceLiveData) Ticker24h(ctx context.Context, symbol string) (map[string]any, error) {
	body, err := b.get(ctx, fmt.Sprintf("%s/api/v3/ticker/24hr?symbol=%s", binanceBaseURL, symbol))
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (b *binanceLiveData) BookTicker(ctx context.Context, symbol string) (float64, float64, error) {
	body, err := b.get(ctx, fmt.Sprintf("%s/api/v3/ticker/bookTicker?symbol=%s", binanceBaseURL, symbol))
	if err != nil {
		return 0, 0, err
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return 0, 0, err
	}
	return jsonFloat(raw["bidPrice"]), jsonFloat(raw["askPrice"]), nil
}

func (b *binanceLiveData) Klines(ctx context.Context, symbol, interval string, limit int, fromMs, toMs int64) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 100
	}
	url := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&limit=%d", binanceBaseURL, symbol, interval, limit)
	if fromMs > 0 && toMs > fromMs {
		url += fmt.Sprintf("&startTime=%d&endTime=%d", fromMs, toMs)
	}
	body, err := b.get(ctx, url)
	if err != nil {
		return nil, err
	}
	var raw [][]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	klines := make([]map[string]any, 0, len(raw))
	for _, k := range raw {
		if len(k) < 6 {
			continue
		}
		ts, _ := k[0].(float64)
		klines = append(klines, map[string]any{
			"timestamp": int64(ts),
			"open":      jsonFloat(k[1]),
			"high":      jsonFloat(k[2]),
			"low":       jsonFloat(k[3]),
			"close":     jsonFloat(k[4]),
			"volume":    jsonFloat(k[5]),
		})
	}
	return klines, nil
}

func (b *binanceLiveData) ExchangeSymbols(ctx context.Context) ([]string, error) {
	body, err := b.get(ctx, binanceBaseURL+"/api/v3/exchangeInfo")
	if err != nil {
		return nil, err
	}
	var raw struct {
		Symbols []struct {
			Symbol     string `json:"symbol"`
			Status     string `json:"status"`
			IsSpot     bool   `json:"isSpotTradingAllowed"`
			QuoteAsset string `json:"quoteAsset"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	symbols := make([]string, 0, len(raw.Symbols))
	for _, s := range raw.Symbols {
		if s.Status == "TRADING" && s.IsSpot && s.QuoteAsset == "USDT" {
			symbols = append(symbols, s.Symbol)
		}
	}
	sort.Strings(symbols)
	return symbols, nil
}

// portfolioAdapter 把 *portfolio.Manager 适配为 PortfolioReader。
type portfolioAdapter struct {
	mgr *portfolio.Manager
}

func (a portfolioAdapter) TotalEquity() float64 { return a.mgr.TotalEquity() }
func (a portfolioAdapter) TotalPnL() float64    { return a.mgr.TotalPnL() }

func (a portfolioAdapter) Balances(accountID string) []*model.Balance {
	acct := a.mgr.GetAccount(accountID)
	if acct == nil {
		return []*model.Balance{}
	}
	out := make([]*model.Balance, 0, len(acct.Balances))
	for _, b := range acct.Balances {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })
	return out
}

func (a portfolioAdapter) Positions() []*model.PositionData { return a.mgr.GetPositions() }

// safeEngine 包裹策略引擎：引擎以 nil 事件总线初始化时（独立 MCP 进程），
// Register/Start 会因 nil bus panic，这里转成普通错误。
type safeEngine struct {
	eng *strategy.Engine
}

func (e safeEngine) List() []string { return e.eng.List() }
func (e safeEngine) Get(name string) strategy.Strategy {
	return e.eng.Get(name)
}
func (e safeEngine) Register(s strategy.Strategy) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("strategy engine unavailable: %v", r)
		}
	}()
	return e.eng.Register(s)
}
func (e safeEngine) Unregister(name string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("strategy engine unavailable: %v", r)
		}
	}()
	return e.eng.Unregister(name)
}
func (e safeEngine) Start(name string, params map[string]any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("strategy engine unavailable: %v", r)
		}
	}()
	return e.eng.Start(name, params)
}
func (e safeEngine) Stop(name string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("strategy engine unavailable: %v", r)
		}
	}()
	return e.eng.Stop(name)
}

// ── Tool 统一描述 ──

// Tool 统一描述。
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any // JSON Schema parameters（type/properties/required）
	Scope       TokenScope     // 调用所需 token scope
}

// prop 构造 JSON Schema 属性。
func prop(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}

// AllTools 返回全部 16 个工具描述（顺序稳定）。
func AllTools() []Tool {
	return []Tool{
		{
			Name:        "get_market_data",
			Description: "Get current market data (price, spread, 24h stats) for a symbol",
			Scope:       ScopeRead,
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"symbol": prop("string", "Trading symbol (e.g., BTCUSDT)")},
				"required":   []string{"symbol"},
			},
		},
		{
			Name:        "get_klines",
			Description: "Get historical kline/candlestick data",
			Scope:       ScopeRead,
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"symbol":     prop("string", "Trading symbol"),
					"interval":   prop("string", "Kline interval: 1m, 5m, 15m, 1h, 4h, 1d"),
					"limit":      prop("integer", "Number of klines to return (default 100, max 1000)"),
					"start_time": prop("number", "Start timestamp (unix ms, optional)"),
					"end_time":   prop("number", "End timestamp (unix ms, optional)"),
				},
				"required": []string{"symbol", "interval"},
			},
		},
		{
			Name:        "place_paper_order",
			Description: "Place a simulated/paper trading order via the matching engine",
			Scope:       ScopeWrite,
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"symbol":     prop("string", "Trading symbol"),
					"side":       prop("string", "BUY or SELL"),
					"order_type": prop("string", "MARKET or LIMIT"),
					"price":      prop("number", "Order price (required for LIMIT)"),
					"quantity":   prop("number", "Order quantity"),
				},
				"required": []string{"symbol", "side", "order_type", "quantity"},
			},
		},
		{
			Name:        "get_orders",
			Description: "Get all orders visible to the current user, optionally filtered by symbol",
			Scope:       ScopeRead,
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"symbol": prop("string", "Filter by symbol (empty for all)")},
			},
		},
		{
			Name:        "cancel_order",
			Description: "Cancel an active order by ID",
			Scope:       ScopeWrite,
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"order_id": prop("string", "Order ID to cancel")},
				"required":   []string{"order_id"},
			},
		},
		{
			Name:        "get_positions",
			Description: "Get current open positions",
			Scope:       ScopeRead,
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"symbol": prop("string", "Filter by symbol (empty for all)")},
			},
		},
		{
			Name:        "get_balance",
			Description: "Get account balance summary and total equity",
			Scope:       ScopeRead,
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "list_strategies",
			Description: "List all deployed strategies, running engine strategies and AI bot instances",
			Scope:       ScopeRead,
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "deploy_strategy",
			Description: "Deploy a new trading strategy from JSON config",
			Scope:       ScopeWrite,
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"config_json": prop("string", "Strategy configuration in JSON format")},
				"required":   []string{"config_json"},
			},
		},
		{
			Name:        "start_strategy",
			Description: "Start a deployed strategy by name in the strategy engine",
			Scope:       ScopeWrite,
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": prop("string", "Strategy name (config id)")},
				"required":   []string{"name"},
			},
		},
		{
			Name:        "stop_strategy",
			Description: "Stop a running strategy by name",
			Scope:       ScopeWrite,
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": prop("string", "Strategy name")},
				"required":   []string{"name"},
			},
		},
		{
			Name:        "delete_strategy",
			Description: "Delete a strategy by name",
			Scope:       ScopeWrite,
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": prop("string", "Strategy name")},
				"required":   []string{"name"},
			},
		},
		{
			Name:        "run_backtest",
			Description: "Run a real backtest for a builtin strategy and return metric summary",
			Scope:       ScopeBacktest,
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"strategy_name":   prop("string", "Builtin strategy name (sma_cross, breakout, martin_trend, macd_golden_long, macd_death_short, ...)"),
					"symbol":          prop("string", "Trading symbol"),
					"interval":        prop("string", "Kline interval (default 1h)"),
					"start_time":      prop("number", "Start timestamp (unix ms, optional)"),
					"end_time":        prop("number", "End timestamp (unix ms, optional)"),
					"initial_balance": prop("number", "Initial balance (default 100000)"),
				},
				"required": []string{"strategy_name", "symbol"},
			},
		},
		{
			Name:        "list_backtests",
			Description: "List completed backtest results",
			Scope:       ScopeBacktest,
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "list_markets",
			Description: "List available trading symbols (spot USDT pairs from Binance)",
			Scope:       ScopeRead,
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "get_stats",
			Description: "Get platform statistics (orders, trades, P&L, backtest aggregates)",
			Scope:       ScopeRead,
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

// ── 参数解析 ──

func strArg(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", fmt.Errorf("missing required parameter: %s", key)
	}
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("parameter %s must be a non-empty string", key)
	}
	return strings.TrimSpace(s), nil
}

func optStrArg(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return strings.TrimSpace(s)
}

func numArg(args map[string]any, key string) (float64, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, fmt.Errorf("missing required parameter: %s", key)
	}
	return toFloat(v)
}

func optNumArg(args map[string]any, key string, def float64) float64 {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	f, err := toFloat(v)
	if err != nil {
		return def
	}
	return f
}

func toFloat(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case json.Number:
		f, err := val.Float64()
		return f, err
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(val), "%g", &f); err != nil {
			return 0, fmt.Errorf("not a number: %q", val)
		}
		return f, nil
	}
	return 0, fmt.Errorf("not a number: %v", v)
}

// jsonFloat 解析币安返回的字符串/数值字段。
func jsonFloat(v any) float64 {
	f, _ := toFloat(v)
	return f
}

// roundSafe 四舍五入；非有限值（+Inf/-Inf/NaN，如无亏损交易时的 profit_factor）
// 回落为 0，保证结果可 JSON 序列化落库。
func roundSafe(v float64, places int) float64 {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return 0
	}
	return store.RoundFloat(v, places)
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

var allowedIntervals = map[string]bool{
	"1m": true, "3m": true, "5m": true, "15m": true, "30m": true,
	"1h": true, "2h": true, "4h": true, "6h": true, "8h": true, "12h": true,
	"1d": true, "3d": true, "1w": true,
}

// ── 工具方法 ──

// GetMarketData 真实行情：币安 24hr ticker + 最优买卖价。
func (tc *ToolContext) GetMarketData(ctx context.Context, args map[string]any) (any, error) {
	symbol, err := strArg(args, "symbol")
	if err != nil {
		return nil, err
	}
	symbol = strings.ToUpper(symbol)

	ticker, err := tc.Market.Ticker24h(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("fetch ticker: %w", err)
	}
	bid, ask, err := tc.Market.BookTicker(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("fetch book ticker: %w", err)
	}

	return map[string]any{
		"symbol":     symbol,
		"price":      jsonFloat(ticker["lastPrice"]),
		"bid":        bid,
		"ask":        ask,
		"spread":     ask - bid,
		"change_24h": jsonFloat(ticker["priceChangePercent"]),
		"high_24h":   jsonFloat(ticker["highPrice"]),
		"low_24h":    jsonFloat(ticker["lowPrice"]),
		"volume_24h": jsonFloat(ticker["quoteVolume"]),
		"timestamp":  time.Now().UnixMilli(),
	}, nil
}

// GetKlines 真实历史 K 线（币安）。
func (tc *ToolContext) GetKlines(ctx context.Context, args map[string]any) (any, error) {
	symbol, err := strArg(args, "symbol")
	if err != nil {
		return nil, err
	}
	interval, err := strArg(args, "interval")
	if err != nil {
		return nil, err
	}
	if !allowedIntervals[interval] {
		return nil, fmt.Errorf("invalid interval %q (allowed: 1m 5m 15m 30m 1h 2h 4h 6h 8h 12h 1d 3d 1w)", interval)
	}
	limit := int(optNumArg(args, "limit", 100))
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	startMs := int64(optNumArg(args, "start_time", 0))
	endMs := int64(optNumArg(args, "end_time", 0))

	klines, err := tc.Market.Klines(ctx, strings.ToUpper(symbol), interval, limit, startMs, endMs)
	if err != nil {
		return nil, fmt.Errorf("fetch klines: %w", err)
	}
	return map[string]any{
		"symbol":   strings.ToUpper(symbol),
		"interval": interval,
		"count":    len(klines),
		"klines":   klines,
	}, nil
}

// PlacePaperOrder 走模拟撮合引擎真实下单并落库。
func (tc *ToolContext) PlacePaperOrder(ctx context.Context, args map[string]any) (any, error) {
	symbol, err := strArg(args, "symbol")
	if err != nil {
		return nil, err
	}
	side, err := strArg(args, "side")
	if err != nil {
		return nil, err
	}
	orderType, err := strArg(args, "order_type")
	if err != nil {
		return nil, err
	}
	quantity, err := numArg(args, "quantity")
	if err != nil {
		return nil, err
	}
	price := optNumArg(args, "price", 0)

	side = strings.ToUpper(side)
	if side != "BUY" && side != "SELL" {
		return nil, fmt.Errorf("side must be BUY or SELL, got %q", side)
	}
	orderType = strings.ToUpper(orderType)
	if orderType != "MARKET" && orderType != "LIMIT" {
		return nil, fmt.Errorf("order_type must be MARKET or LIMIT, got %q", orderType)
	}
	if quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive, got %v", quantity)
	}
	if orderType == "LIMIT" && price <= 0 {
		return nil, fmt.Errorf("price is required for LIMIT orders")
	}
	symbol = strings.ToUpper(symbol)

	// 撮合引擎约定小写 side/order_type（参照 app.Context.simulatePaperFill）。
	result, err := tc.Matcher.PlaceOrder(symbol, strings.ToLower(side), strings.ToLower(orderType), price, quantity, tc.UserID, "")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"order_id":   result["store_order_id"],
		"symbol":     symbol,
		"side":       side,
		"order_type": orderType,
		"price":      price,
		"quantity":   quantity,
		"filled":     result["filled"],
		"status":     result["status"],
		"engine":     "paper_matching",
	}, nil
}

// GetOrders 真实订单列表（store，按属主可见性过滤）。
func (tc *ToolContext) GetOrders(_ context.Context, args map[string]any) (any, error) {
	symbol := strings.ToUpper(optStrArg(args, "symbol"))
	orders := store.GetOrdersForUser(symbol, int64(tc.UserID), false)
	return map[string]any{
		"symbol": symbol,
		"count":  len(orders),
		"orders": orders,
	}, nil
}

// CancelOrder 撤单：属主校验后走撮合引擎 + store。
func (tc *ToolContext) CancelOrder(_ context.Context, args map[string]any) (any, error) {
	orderID, err := strArg(args, "order_id")
	if err != nil {
		return nil, err
	}
	order := store.GetOrderByID(orderID)
	if order == nil {
		return nil, fmt.Errorf("order %s not found", orderID)
	}
	if owner, found := store.GetOrderOwnerID(orderID); found && owner != 0 && uint64(owner) != tc.UserID {
		return nil, fmt.Errorf("order %s belongs to another user", orderID)
	}
	symbol, _ := order["symbol"].(string)
	if err := tc.Matcher.CancelOrder(symbol, orderID); err != nil {
		return nil, err
	}
	return map[string]any{
		"order_id": orderID,
		"status":   "cancelled",
	}, nil
}

// GetPositions 真实持仓（portfolio manager，可按 symbol 过滤）。
func (tc *ToolContext) GetPositions(_ context.Context, args map[string]any) (any, error) {
	symbol := strings.ToUpper(optStrArg(args, "symbol"))
	positions := tc.Portfolio.Positions()
	filtered := make([]*model.PositionData, 0, len(positions))
	for _, p := range positions {
		if symbol == "" || p.Symbol == symbol {
			filtered = append(filtered, p)
		}
	}
	return map[string]any{
		"symbol":    symbol,
		"count":     len(filtered),
		"positions": filtered,
	}, nil
}

// GetBalance 真实余额与权益（portfolio manager）。
func (tc *ToolContext) GetBalance(_ context.Context, _ map[string]any) (any, error) {
	balances := tc.Portfolio.Balances("default")
	return map[string]any{
		"balances":     balances,
		"total_equity": store.RoundFloat(tc.Portfolio.TotalEquity(), 2),
		"total_pnl":    store.RoundFloat(tc.Portfolio.TotalPnL(), 2),
	}, nil
}

// ListStrategies 真实列表：已部署策略配置 + 引擎在跑策略 + AI 机器人实例。
func (tc *ToolContext) ListStrategies(_ context.Context, _ map[string]any) (any, error) {
	running := map[string]bool{}
	if tc.Engine != nil {
		for _, name := range tc.Engine.List() {
			running[name] = true
		}
	}

	strategies := make([]map[string]any, 0)
	for id, cfg := range store.GetStrategyConfigs() {
		item := map[string]any{
			"id":      id,
			"name":    cfgName(cfg, id),
			"status":  cfg["status"],
			"running": running[id],
		}
		for _, k := range []string{"strategy_type", "symbol", "timeframe", "market_type", "created_at", "updated_at"} {
			if v, ok := cfg[k]; ok {
				item[k] = v
			}
		}
		strategies = append(strategies, item)
	}
	sort.Slice(strategies, func(i, j int) bool { return strategies[i]["id"].(string) < strategies[j]["id"].(string) })

	bots := store.GetAIBotInstances(int(tc.UserID))
	if bots == nil {
		bots = []map[string]any{}
	}

	return map[string]any{
		"count":      len(strategies),
		"strategies": strategies,
		"bots":       bots,
	}, nil
}

func cfgName(cfg map[string]any, id string) string {
	for _, k := range []string{"strategy_name", "name", "bot_type"} {
		if v, ok := cfg[k].(string); ok && v != "" {
			return v
		}
	}
	return id
}

// DeployStrategy 解析 JSON 配置并真实落库（store strategy configs）。
func (tc *ToolContext) DeployStrategy(_ context.Context, args map[string]any) (any, error) {
	configJSON, err := strArg(args, "config_json")
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("config_json is not valid JSON: %w", err)
	}
	strategyType := ""
	for _, k := range []string{"strategy_type", "bot_type"} {
		if v, ok := cfg[k].(string); ok && v != "" {
			strategyType = v
			break
		}
	}
	if strategyType == "" {
		return nil, fmt.Errorf("config_json must include strategy_type")
	}

	id, _ := cfg["id"].(string)
	if id == "" {
		id = fmt.Sprintf("agent-%d", time.Now().UnixMilli())
	}
	cfg["strategy_type"] = strategyType
	cfg["status"] = "stopped"
	cfg["user_id"] = tc.UserID
	cfg["created_at"] = time.Now().UnixMilli()
	cfg["updated_at"] = time.Now().UnixMilli()

	store.SetStrategyConfig(id, cfg)
	store.PersistStrategyConfigs()

	return map[string]any{
		"id":            id,
		"name":          cfgName(cfg, id),
		"strategy_type": strategyType,
		"symbol":        cfg["symbol"],
		"status":        "deployed",
	}, nil
}

// StartStrategy 在策略引擎真实注册并启动已部署策略。
// 复用 handler/ai_bot.go startAIBotInEngine 的逻辑（agent 不能 import handler，
// 依赖经 StrategyRuntime 接口注入；参数过滤与 ai_bot 版一致）。
func (tc *ToolContext) StartStrategy(_ context.Context, args map[string]any) (any, error) {
	name, err := strArg(args, "name")
	if err != nil {
		return nil, err
	}
	item := store.GetStrategyConfig(name)
	if item == nil {
		return nil, fmt.Errorf("strategy %s not found", name)
	}
	if err := tc.startInEngine(name, item); err != nil {
		return nil, err
	}
	item["status"] = "running"
	item["updated_at"] = time.Now().UnixMilli()
	store.SetStrategyConfig(name, item)
	store.PersistStrategyConfigs()
	return map[string]any{
		"name":   name,
		"status": "started",
	}, nil
}

func (tc *ToolContext) startInEngine(id string, item map[string]any) error {
	if tc.Engine == nil {
		return fmt.Errorf("strategy engine not configured")
	}
	strategyType, _ := item["strategy_type"].(string)
	if strategyType == "" {
		strategyType, _ = item["bot_type"].(string)
	}
	if strategyType == "" {
		return fmt.Errorf("strategy_type not set")
	}

	if existing := tc.Engine.Get(id); existing != nil {
		_ = tc.Engine.Stop(id)
		_ = tc.Engine.Unregister(id)
	}

	s := strategy.StrategyFactory(strategyType)
	if s == nil {
		return fmt.Errorf("unknown strategy type: %s", strategyType)
	}

	// 只传策略声明的参数，避免 "parameter not found"（同 ai_bot 版）。
	allowed := map[string]bool{}
	if defs := s.ParamDefs(); defs != nil {
		for _, def := range defs {
			if n, ok := def["name"].(string); ok {
				allowed[n] = true
			}
		}
	}
	params := map[string]any{}
	for k, v := range item {
		if allowed[k] {
			params[k] = v
		}
	}
	if sym, ok := item["symbol"].(string); ok && sym != "" {
		params["symbol"] = strings.ToUpper(strings.TrimSpace(sym))
	}
	if tf, ok := item["timeframe"].(string); ok && tf != "" {
		params["timeframe"] = tf
	}

	wrapped := strategy.WrapStrategy(id, s)
	if err := tc.Engine.Register(wrapped); err != nil {
		return fmt.Errorf("register strategy: %w", err)
	}
	if err := tc.Engine.Start(id, params); err != nil {
		_ = tc.Engine.Unregister(id)
		return fmt.Errorf("start strategy: %w", err)
	}
	return nil
}

// StopStrategy 停止引擎中的策略并更新配置状态。
func (tc *ToolContext) StopStrategy(_ context.Context, args map[string]any) (any, error) {
	name, err := strArg(args, "name")
	if err != nil {
		return nil, err
	}
	stopped := false
	if tc.Engine != nil && tc.Engine.Get(name) != nil {
		if err := tc.Engine.Stop(name); err != nil {
			return nil, fmt.Errorf("stop strategy: %w", err)
		}
		stopped = true
	}
	item := store.GetStrategyConfig(name)
	if item == nil && !stopped {
		return nil, fmt.Errorf("strategy %s not found", name)
	}
	if item != nil {
		item["status"] = "stopped"
		item["updated_at"] = time.Now().UnixMilli()
		store.SetStrategyConfig(name, item)
		store.PersistStrategyConfigs()
	}
	return map[string]any{
		"name":   name,
		"status": "stopped",
	}, nil
}

// DeleteStrategy 停掉引擎实例并删除配置。
func (tc *ToolContext) DeleteStrategy(_ context.Context, args map[string]any) (any, error) {
	name, err := strArg(args, "name")
	if err != nil {
		return nil, err
	}
	if tc.Engine != nil && tc.Engine.Get(name) != nil {
		_ = tc.Engine.Stop(name)
		if err := tc.Engine.Unregister(name); err != nil {
			return nil, fmt.Errorf("unregister strategy: %w", err)
		}
	}
	if !store.DeleteStrategyConfig(name) {
		return nil, fmt.Errorf("strategy %s not found", name)
	}
	store.PersistStrategyConfigs()
	return map[string]any{
		"name":   name,
		"status": "deleted",
	}, nil
}

// RunBacktest 真实跑回测：币安 K 线 + backtest.Runner + 内置策略，
// 结果落库 xt_backtests 并返回指标摘要。
func (tc *ToolContext) RunBacktest(ctx context.Context, args map[string]any) (any, error) {
	strategyName, err := strArg(args, "strategy_name")
	if err != nil {
		return nil, err
	}
	symbol, err := strArg(args, "symbol")
	if err != nil {
		return nil, err
	}
	symbol = strings.ToUpper(symbol)
	interval := optStrArg(args, "interval")
	if interval == "" {
		interval = "1h"
	}
	if !allowedIntervals[interval] {
		return nil, fmt.Errorf("invalid interval %q", interval)
	}
	startMs := int64(optNumArg(args, "start_time", 0))
	endMs := int64(optNumArg(args, "end_time", 0))
	initialBalance := optNumArg(args, "initial_balance", 100000)
	if initialBalance <= 0 {
		initialBalance = 100000
	}

	btStrategy := backtest.NewBuiltinStrategy(strategyName, symbol)
	if btStrategy == nil {
		return nil, fmt.Errorf("unknown strategy %q (available: %s)", strategyName, strings.Join(backtest.BuiltinStrategyNames(), ", "))
	}

	klines, err := tc.Market.Klines(ctx, symbol, interval, 1000, startMs, endMs)
	if err != nil {
		return nil, fmt.Errorf("fetch klines: %w", err)
	}
	if len(klines) < 50 {
		return nil, fmt.Errorf("insufficient kline data (%d < 50)", len(klines))
	}
	bars := make([]model.Bar, 0, len(klines))
	for _, k := range klines {
		bars = append(bars, model.Bar{
			Symbol:   symbol,
			Open:     jsonFloat(k["open"]),
			High:     jsonFloat(k["high"]),
			Low:      jsonFloat(k["low"]),
			Close:    jsonFloat(k["close"]),
			Volume:   jsonFloat(k["volume"]),
			Interval: interval,
			Time:     int64(jsonFloat(k["timestamp"])),
		})
	}

	cfg := backtest.DefaultRunnerConfig()
	cfg.InitialBalance = initialBalance
	cfg.StartTime = startMs
	cfg.EndTime = endMs
	runner := backtest.NewRunner(cfg)
	runner.LoadBars(symbol, bars)

	started := time.Now()
	result, err := runner.Run(btStrategy)
	if err != nil {
		return nil, fmt.Errorf("backtest run: %w", err)
	}

	finalEquity := initialBalance
	if len(result.EquityCurve) > 0 {
		finalEquity = result.EquityCurve[len(result.EquityCurve)-1].Equity
	}
	summary := map[string]any{
		"strategy":         strategyName,
		"symbol":           symbol,
		"interval":         interval,
		"start_date":       time.UnixMilli(bars[0].Time).Format("2006-01-02"),
		"end_date":         time.UnixMilli(bars[len(bars)-1].Time).Format("2006-01-02"),
		"bars":             len(bars),
		"initial_balance":  initialBalance,
		"final_equity":     roundSafe(finalEquity, 2),
		"total_return_pct": roundSafe(result.TotalReturnPct, 2),
		"max_drawdown_pct": roundSafe(result.MaxDrawdownPct, 2),
		"sharpe_ratio":     roundSafe(result.SharpeRatio, 2),
		"win_rate":         roundSafe(result.WinRate, 1),
		"profit_factor":    roundSafe(result.ProfitFactor, 2),
		"total_trades":     result.TotalTrades,
		"duration_ms":      time.Since(started).Milliseconds(),
	}

	// 结果落库 xt_backtests（幂等失败不阻断返回）。
	if report, err := json.Marshal(summary); err == nil {
		rec := &store.BacktestRecord{
			ID:          fmt.Sprintf("agent-bt-%d", time.Now().UnixMilli()),
			UserID:      int64(tc.UserID),
			Name:        fmt.Sprintf("%s %s %s", strategyName, symbol, interval),
			Strategy:    strategyName,
			Symbol:      symbol,
			StartTime:   bars[0].Time,
			EndTime:     bars[len(bars)-1].Time,
			DurationMs:  result.DurationMs,
			Status:      "COMPLETED",
			ReportJSON:  string(report),
			CreatedAt:   time.Now().UnixMilli(),
			CompletedAt: time.Now().UnixMilli(),
		}
		if err := store.NewBacktestRepo().Create(rec); err == nil {
			summary["id"] = rec.ID
		}
	}
	return summary, nil
}

// ListBacktests 真实回测记录（xt_backtests，按属主过滤）。
func (tc *ToolContext) ListBacktests(_ context.Context, _ map[string]any) (any, error) {
	filter := map[string]any{}
	if tc.UserID > 0 {
		filter["user_id"] = int64(tc.UserID)
	}
	records, err := store.NewBacktestRepo().List(filter, 50)
	if err != nil {
		return nil, err
	}
	backtests := make([]map[string]any, 0, len(records))
	for _, r := range records {
		item := map[string]any{
			"id":           r.ID,
			"name":         r.Name,
			"strategy":     r.Strategy,
			"symbol":       r.Symbol,
			"status":       r.Status,
			"start_time":   r.StartTime,
			"end_time":     r.EndTime,
			"duration_ms":  r.DurationMs,
			"created_at":   r.CreatedAt,
			"completed_at": r.CompletedAt,
		}
		if r.ReportJSON != "" {
			var report map[string]any
			if json.Unmarshal([]byte(r.ReportJSON), &report) == nil {
				item["report"] = report
			}
		}
		backtests = append(backtests, item)
	}
	return map[string]any{
		"count":     len(backtests),
		"backtests": backtests,
	}, nil
}

// ListMarkets 真实交易对列表（币安 exchangeInfo，交易中现货 USDT 区）。
func (tc *ToolContext) ListMarkets(ctx context.Context, _ map[string]any) (any, error) {
	symbols, err := tc.Market.ExchangeSymbols(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch exchange info: %w", err)
	}
	return map[string]any{
		"count":   len(symbols),
		"symbols": symbols,
	}, nil
}

// GetStats 平台统计：订单/成交/盈亏/回测聚合，全部来自真实存储。
func (tc *ToolContext) GetStats(_ context.Context, _ map[string]any) (any, error) {
	totalOrders := 0
	if n, err := store.GetOrderCount(); err == nil {
		totalOrders = n
	}

	totalTrades := 0
	if trades, err := store.NewTradeRepo().List(nil, 100000); err == nil {
		totalTrades = len(trades)
	}

	stats := map[string]any{
		"total_orders": totalOrders,
		"total_trades": totalTrades,
		"total_pnl":    store.RoundFloat(tc.Portfolio.TotalPnL(), 2),
		"total_equity": store.RoundFloat(tc.Portfolio.TotalEquity(), 2),
	}

	// 回测聚合（已完成记录的均值）。
	filter := map[string]any{"status": "COMPLETED"}
	if tc.UserID > 0 {
		filter["user_id"] = int64(tc.UserID)
	}
	if records, err := store.NewBacktestRepo().List(filter, 100); err == nil && len(records) > 0 {
		var sharpeSum, winRateSum, ddSum, retSum float64
		for _, r := range records {
			var report map[string]any
			if json.Unmarshal([]byte(r.ReportJSON), &report) != nil {
				continue
			}
			sharpeSum += jsonFloat(report["sharpe_ratio"])
			winRateSum += jsonFloat(report["win_rate"])
			ddSum += jsonFloat(report["max_drawdown_pct"])
			retSum += jsonFloat(report["total_return_pct"])
		}
		n := float64(len(records))
		stats["backtests_completed"] = len(records)
		stats["sharpe_ratio"] = store.RoundFloat(sharpeSum/n, 2)
		stats["win_rate"] = store.RoundFloat(winRateSum/n, 1)
		stats["max_drawdown"] = store.RoundFloat(ddSum/n, 2)
		stats["total_return_pct"] = store.RoundFloat(retSum/n, 2)
	} else {
		stats["backtests_completed"] = 0
		stats["sharpe_ratio"] = 0.0
		stats["win_rate"] = 0.0
		stats["max_drawdown"] = 0.0
		stats["total_return_pct"] = 0.0
	}
	return stats, nil
}

// ── Token 鉴权 ──

// scopeAliases 兼容单词式 scope（历史 token 里存过 "read"/"write" 等）。
var scopeAliases = map[string]TokenScope{
	"READ": ScopeRead, "WRITE": ScopeWrite, "BACKTEST": ScopeBacktest,
	"NOTIFY": ScopeNotify, "COMMUNITY": ScopeCommunity,
	"TRADE": ScopeTrade, "LIVE": ScopeTrade,
}

// normalizeScopes 把字符串 scope 列表规范化为单字母 scope 集合。
func normalizeScopes(scopes []string) map[TokenScope]bool {
	out := map[TokenScope]bool{}
	for _, s := range scopes {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if len(s) == 1 {
			if sc := TokenScope(s); ValidateScopes([]TokenScope{sc}) {
				out[sc] = true
			}
			continue
		}
		if sc, ok := scopeAliases[s]; ok {
			out[sc] = true
		}
	}
	return out
}

// FilterTools 按 token scope 过滤工具（如 "W" 才能给写类工具）。
func (tm *TokenManager) FilterTools(tools []Tool, scopes []string) []Tool {
	owned := normalizeScopes(scopes)
	out := make([]Tool, 0, len(tools))
	for _, t := range tools {
		if t.Scope == "" || owned[t.Scope] {
			out = append(out, t)
		}
	}
	return out
}

// ParseTokenScopes 解析逗号分隔 scope 字符串（供 FilterTools 调用侧复用）。
func ParseTokenScopes(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
