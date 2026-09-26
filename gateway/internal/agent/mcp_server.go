package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

// MCP Server implements JSON-RPC 2.0 over stdio for AI agent tool access.
// 16 tools, all backed by ToolContext real implementations (see tools.go).

// ── MCP Types ──

type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
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
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema MCPInputSchema `json:"inputSchema"`
}

type MCPInputSchema struct {
	Type       string             `json:"type"`
	Properties map[string]MCPProp `json:"properties,omitempty"`
	Required   []string           `json:"required,omitempty"`
}

type MCPProp struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// ── Tool Handler ──

type ToolHandler func(ctx context.Context, params map[string]any) (any, error)

// ── MCP Server ──

type MCPServer struct {
	name     string
	version  string
	tools    map[string]MCPTool
	handlers map[string]ToolHandler
	tc       *ToolContext
	reader   *bufio.Reader
	writer   io.Writer
	mu       sync.RWMutex
}

// NewMCPServer 创建 MCP server；tc 为工具依赖容器（可为 nil，随后用 SetToolContext 注入）。
func NewMCPServer(name, version string, tc *ToolContext) *MCPServer {
	server := &MCPServer{
		name:     name,
		version:  version,
		tools:    make(map[string]MCPTool),
		handlers: make(map[string]ToolHandler),
		tc:       tc,
		reader:   bufio.NewReader(os.Stdin),
		writer:   os.Stdout,
	}
	server.registerTools()
	return server
}

// SetToolContext 注入工具依赖上下文。
func (s *MCPServer) SetToolContext(tc *ToolContext) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tc = tc
}

// registerTools 从 AllTools() 注册 16 个工具，handler 全部走 ToolContext 真实实现。
func (s *MCPServer) registerTools() {
	methods := map[string]func(tc *ToolContext, ctx context.Context, args map[string]any) (any, error){
		"get_market_data":   (*ToolContext).GetMarketData,
		"get_klines":        (*ToolContext).GetKlines,
		"place_paper_order": (*ToolContext).PlacePaperOrder,
		"get_orders":        (*ToolContext).GetOrders,
		"cancel_order":      (*ToolContext).CancelOrder,
		"get_positions":     (*ToolContext).GetPositions,
		"get_balance":       (*ToolContext).GetBalance,
		"list_strategies":   (*ToolContext).ListStrategies,
		"deploy_strategy":   (*ToolContext).DeployStrategy,
		"start_strategy":    (*ToolContext).StartStrategy,
		"stop_strategy":     (*ToolContext).StopStrategy,
		"delete_strategy":   (*ToolContext).DeleteStrategy,
		"run_backtest":      (*ToolContext).RunBacktest,
		"list_backtests":    (*ToolContext).ListBacktests,
		"list_markets":      (*ToolContext).ListMarkets,
		"get_stats":         (*ToolContext).GetStats,
	}

	for _, t := range AllTools() {
		s.tools[t.Name] = toolToMCP(t)
		method := methods[t.Name]
		s.handlers[t.Name] = func(ctx context.Context, params map[string]any) (any, error) {
			s.mu.RLock()
			tc := s.tc
			s.mu.RUnlock()
			if tc == nil {
				return nil, fmt.Errorf("tool context not configured")
			}
			return method(tc, ctx, params)
		}
	}
}

// toolToMCP 把 Tool 的 JSON Schema 转成 MCP inputSchema。
func toolToMCP(t Tool) MCPTool {
	schema := MCPInputSchema{
		Type:       "object",
		Properties: map[string]MCPProp{},
	}
	if ty, ok := t.Schema["type"].(string); ok {
		schema.Type = ty
	}
	if props, ok := t.Schema["properties"].(map[string]any); ok {
		for name, pv := range props {
			p := MCPProp{Type: "string"}
			if pm, ok := pv.(map[string]any); ok {
				if ty, ok := pm["type"].(string); ok {
					p.Type = ty
				}
				if d, ok := pm["description"].(string); ok {
					p.Description = d
				}
			}
			schema.Properties[name] = p
		}
	}
	if req, ok := t.Schema["required"].([]string); ok {
		schema.Required = req
	}
	return MCPTool{Name: t.Name, Description: t.Description, InputSchema: schema}
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
		Name      string          `json:"name"`
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

	result, err := handler(context.Background(), args)
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

// Serve runs the MCP server in a goroutine and returns immediately.
func Serve() {
	server := NewMCPServer("xiaotian-quant", "2.0.0", GetToolContext())
	server.Run()
}
