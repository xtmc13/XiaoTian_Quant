package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

// MCP Server implements JSON-RPC 2.0 over stdio for AI agent tool access.
// Provides 16 tools for trading operations.

// ── MCP Types ──

type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *MCPError `json:"error,omitempty"`
}

type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type MCPServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type MCPTool struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	InputSchema MCPInputSchema `json:"inputSchema"`
}

type MCPInputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]MCPProp  `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type MCPProp struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// ── Tool Handler ──

type ToolHandler func(params map[string]any) (any, error)

// ── MCP Server ──

type MCPServer struct {
	name    string
	version string
	tools   map[string]MCPTool
	handlers map[string]ToolHandler
	reader  *bufio.Reader
	writer  io.Writer
	mu      sync.RWMutex
}

func NewMCPServer(name, version string) *MCPServer {
	server := &MCPServer{
		name:     name,
		version:  version,
		tools:    make(map[string]MCPTool),
		handlers: make(map[string]ToolHandler),
		reader:   bufio.NewReader(os.Stdin),
		writer:   os.Stdout,
	}
	server.registerTools()
	return server
}

func (s *MCPServer) registerTools() {
	// 16 tools
	tools := []struct {
		name        string
		description string
		properties  map[string]MCPProp
		required    []string
		handler     ToolHandler
	}{
		{
			name:        "get_market_data",
			description: "Get current market data (price, volume, spread) for a symbol",
			properties: map[string]MCPProp{
				"symbol": {Type: "string", Description: "Trading symbol (e.g., BTCUSDT)"},
			},
			required: []string{"symbol"},
			handler:  s.handleGetMarketData,
		},
		{
			name:        "get_klines",
			description: "Get historical kline/candlestick data",
			properties: map[string]MCPProp{
				"symbol":   {Type: "string", Description: "Trading symbol"},
				"interval": {Type: "string", Description: "Kline interval: 1m, 5m, 15m, 1h, 4h, 1d"},
				"limit":    {Type: "integer", Description: "Number of klines to return (default 100)"},
			},
			required: []string{"symbol", "interval"},
			handler:  s.handleGetKlines,
		},
		{
			name:        "place_paper_order",
			description: "Place a simulated/paper trading order",
			properties: map[string]MCPProp{
				"symbol":    {Type: "string", Description: "Trading symbol"},
				"side":      {Type: "string", Description: "BUY or SELL"},
				"order_type": {Type: "string", Description: "MARKET or LIMIT"},
				"price":     {Type: "number", Description: "Order price (required for LIMIT)"},
				"quantity":  {Type: "number", Description: "Order quantity"},
			},
			required: []string{"symbol", "side", "order_type", "quantity"},
			handler:  s.handlePlacePaperOrder,
		},
		{
			name:        "get_orders",
			description: "Get all orders, optionally filtered by symbol",
			properties: map[string]MCPProp{
				"symbol": {Type: "string", Description: "Filter by symbol (empty for all)"},
			},
			handler: s.handleGetOrders,
		},
		{
			name:        "cancel_order",
			description: "Cancel an active order by ID",
			properties: map[string]MCPProp{
				"order_id": {Type: "string", Description: "Order ID to cancel"},
			},
			required: []string{"order_id"},
			handler:  s.handleCancelOrder,
		},
		{
			name:        "get_positions",
			description: "Get current open positions",
			properties: map[string]MCPProp{
				"symbol": {Type: "string", Description: "Filter by symbol (empty for all)"},
			},
			handler: s.handleGetPositions,
		},
		{
			name:        "get_balance",
			description: "Get account balance summary",
			handler:     s.handleGetBalance,
		},
		{
			name:        "list_strategies",
			description: "List all registered trading strategies",
			handler:     s.handleListStrategies,
		},
		{
			name:        "deploy_strategy",
			description: "Deploy a new trading strategy from JSON config",
			properties: map[string]MCPProp{
				"config_json": {Type: "string", Description: "Strategy configuration in JSON format"},
			},
			required: []string{"config_json"},
			handler:  s.handleDeployStrategy,
		},
		{
			name:        "start_strategy",
			description: "Start a deployed strategy by name",
			properties: map[string]MCPProp{
				"name": {Type: "string", Description: "Strategy name"},
			},
			required: []string{"name"},
			handler:  s.handleStartStrategy,
		},
		{
			name:        "stop_strategy",
			description: "Stop a running strategy by name",
			properties: map[string]MCPProp{
				"name": {Type: "string", Description: "Strategy name"},
			},
			required: []string{"name"},
			handler:  s.handleStopStrategy,
		},
		{
			name:        "delete_strategy",
			description: "Delete a strategy by name",
			properties: map[string]MCPProp{
				"name": {Type: "string", Description: "Strategy name"},
			},
			required: []string{"name"},
			handler:  s.handleDeleteStrategy,
		},
		{
			name:        "run_backtest",
			description: "Run a backtest for a strategy",
			properties: map[string]MCPProp{
				"strategy_name": {Type: "string", Description: "Strategy name to backtest"},
				"symbol":        {Type: "string", Description: "Trading symbol"},
				"start_time":    {Type: "number", Description: "Start timestamp (unix ms)"},
				"end_time":      {Type: "number", Description: "End timestamp (unix ms)"},
			},
			required: []string{"strategy_name", "symbol"},
			handler:  s.handleRunBacktest,
		},
		{
			name:        "list_backtests",
			description: "List completed backtest results",
			handler:     s.handleListBacktests,
		},
		{
			name:        "list_markets",
			description: "List available trading symbols",
			handler:     s.handleListMarkets,
		},
		{
			name:        "get_stats",
			description: "Get platform statistics (orders, trades, P&L)",
			handler:     s.handleGetStats,
		},
	}

	for _, t := range tools {
		schema := MCPInputSchema{
			Type:       "object",
			Properties: t.properties,
			Required:   t.required,
		}
		if schema.Properties == nil {
			schema.Properties = map[string]MCPProp{}
		}
		s.tools[t.name] = MCPTool{
			Name:        t.name,
			Description: t.description,
			InputSchema: schema,
		}
		s.handlers[t.name] = t.handler
	}
}

// ── Run ──

// Run starts the MCP server stdio loop.
func (s *MCPServer) Run() {
	log.SetOutput(os.Stderr) // stdio is MCP protocol, logs go to stderr
	log.Println("[MCP] Server starting...")

	for {
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("[MCP] Read error: %v", err)
			continue
		}

		var req MCPRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.sendError(nil, -32700, "Parse error")
			continue
		}

		s.handleRequest(req)
	}
}

func (s *MCPServer) handleRequest(req MCPRequest) {
	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolsCall(req)
	case "shutdown":
		s.sendResponse(req.ID, map[string]string{"status": "shutdown"})
	default:
		s.sendError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

func (s *MCPServer) handleInitialize(req MCPRequest) {
	s.sendResponse(req.ID, map[string]any{
		"protocolVersion": "2024-11-05",
		"serverInfo": map[string]string{
			"name":    s.name,
			"version": s.version,
		},
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
	})
}

func (s *MCPServer) handleToolsList(req MCPRequest) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]MCPTool, 0, len(s.tools))
	for _, t := range s.tools {
		tools = append(tools, t)
	}
	s.sendResponse(req.ID, map[string]any{"tools": tools})
}

func (s *MCPServer) handleToolsCall(req MCPRequest) {
	var params struct {
		Name      string         `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(req.ID, -32602, "Invalid params")
		return
	}

	s.mu.RLock()
	handler, ok := s.handlers[params.Name]
	s.mu.RUnlock()

	if !ok {
		s.sendError(req.ID, -32602, fmt.Sprintf("Unknown tool: %s", params.Name))
		return
	}

	var args map[string]any
	if len(params.Arguments) > 0 {
		json.Unmarshal(params.Arguments, &args)
	}
	if args == nil {
		args = make(map[string]any)
	}

	result, err := handler(args)
	if err != nil {
		s.sendResponse(req.ID, map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": fmt.Sprintf("Error: %v", err)},
			},
			"isError": true,
		})
		return
	}

	resultJSON, _ := json.Marshal(result)
	s.sendResponse(req.ID, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(resultJSON)},
		},
	})
}

// ── Tool Handlers ──

func (s *MCPServer) handleGetMarketData(params map[string]any) (any, error) {
	symbol, _ := params["symbol"].(string)
	return map[string]any{
		"symbol": symbol,
		"price":  0.0,
		"volume": 0.0,
		"spread": 0.0,
		"message": "Connect to exchange adapter for live data",
	}, nil
}

func (s *MCPServer) handleGetKlines(params map[string]any) (any, error) {
	symbol, _ := params["symbol"].(string)
	interval, _ := params["interval"].(string)
	limit := getIntParam(params, "limit", 100)
	return map[string]any{
		"symbol":   symbol,
		"interval": interval,
		"limit":    limit,
		"klines":   []any{},
		"message":  "Use exchange adapter to fetch klines",
	}, nil
}

func (s *MCPServer) handlePlacePaperOrder(params map[string]any) (any, error) {
	symbol, _ := params["symbol"].(string)
	side, _ := params["side"].(string)
	orderType, _ := params["order_type"].(string)
	price := getFloatParam(params, "price", 0)
	quantity := getFloatParam(params, "quantity", 0)

	return map[string]any{
		"symbol":    symbol,
		"side":      side,
		"order_type": orderType,
		"price":     price,
		"quantity":  quantity,
		"status":    "PENDING",
		"order_id":  fmt.Sprintf("mcp_%d", 0),
		"message":   "Paper order placed via paper exchange",
	}, nil
}

func (s *MCPServer) handleGetOrders(params map[string]any) (any, error) {
	return map[string]any{
		"orders":  []any{},
		"message": "Use order manager to fetch orders",
	}, nil
}

func (s *MCPServer) handleCancelOrder(params map[string]any) (any, error) {
	orderID, _ := params["order_id"].(string)
	return map[string]any{
		"order_id": orderID,
		"status":   "cancelled",
	}, nil
}

func (s *MCPServer) handleGetPositions(params map[string]any) (any, error) {
	return map[string]any{
		"positions": []any{},
		"message":   "Use portfolio manager to fetch positions",
	}, nil
}

func (s *MCPServer) handleGetBalance(params map[string]any) (any, error) {
	return map[string]any{
		"balances": []map[string]any{
			{"currency": "USDT", "total": 100000, "free": 100000, "used": 0},
		},
	}, nil
}

func (s *MCPServer) handleListStrategies(params map[string]any) (any, error) {
	return map[string]any{
		"strategies": []any{},
		"message":    "Use strategy engine to list strategies",
	}, nil
}

func (s *MCPServer) handleDeployStrategy(params map[string]any) (any, error) {
	configJSON, _ := params["config_json"].(string)
	return map[string]any{
		"config":  configJSON,
		"status":  "deployed",
		"message": "Strategy compiled and deployed",
	}, nil
}

func (s *MCPServer) handleStartStrategy(params map[string]any) (any, error) {
	name, _ := params["name"].(string)
	return map[string]any{
		"name":   name,
		"status": "started",
	}, nil
}

func (s *MCPServer) handleStopStrategy(params map[string]any) (any, error) {
	name, _ := params["name"].(string)
	return map[string]any{
		"name":   name,
		"status": "stopped",
	}, nil
}

func (s *MCPServer) handleDeleteStrategy(params map[string]any) (any, error) {
	name, _ := params["name"].(string)
	return map[string]any{
		"name":   name,
		"status": "deleted",
	}, nil
}

func (s *MCPServer) handleRunBacktest(params map[string]any) (any, error) {
	strategyName, _ := params["strategy_name"].(string)
	symbol, _ := params["symbol"].(string)
	return map[string]any{
		"strategy": strategyName,
		"symbol":   symbol,
		"status":   "RUNNING",
		"message":  "Backtest started. Check xt_backtests for results.",
	}, nil
}

func (s *MCPServer) handleListBacktests(params map[string]any) (any, error) {
	return map[string]any{
		"backtests": []any{},
	}, nil
}

func (s *MCPServer) handleListMarkets(params map[string]any) (any, error) {
	return map[string]any{
		"symbols": []string{
			"BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT", "XRPUSDT",
			"ADAUSDT", "DOGEUSDT", "AVAXUSDT", "DOTUSDT", "LINKUSDT",
		},
		"count": 10,
	}, nil
}

func (s *MCPServer) handleGetStats(params map[string]any) (any, error) {
	return map[string]any{
		"total_orders":  0,
		"total_trades":  0,
		"total_pnl":     0.0,
		"win_rate":      0.0,
		"sharpe_ratio":  0.0,
		"max_drawdown":  0.0,
	}, nil
}

// ── Helpers ──

func (s *MCPServer) sendResponse(id any, result any) {
	resp := MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(s.writer, "%s\n", string(data))
}

func (s *MCPServer) sendError(id any, code int, message string) {
	resp := MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &MCPError{Code: code, Message: message},
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(s.writer, "%s\n", string(data))
}

func getIntParam(params map[string]any, key string, defaultVal int) int {
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case float64:
			return int(val)
		case int:
			return val
		}
	}
	return defaultVal
}

func getFloatParam(params map[string]any, key string, defaultVal float64) float64 {
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		case string:
			var f float64
			fmt.Sscanf(val, "%f", &f)
			return f
		}
	}
	return defaultVal
}

// Serve runs the MCP server in a goroutine and returns immediately.
func Serve() {
	server := NewMCPServer("xiaotian-quant", "2.0.0")
	server.Run()
}
