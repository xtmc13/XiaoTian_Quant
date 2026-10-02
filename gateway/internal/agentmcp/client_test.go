package agentmcp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// ── 假 MCP stdio server（TestHelperProcess 模式）──

// TestHelperProcess 作为子进程运行：按行读 JSON-RPC 请求并应答。
// 支持 initialize / tools/list / tools/call；tools/call 的 slow 工具
// 睡眠 3s 用于客户端超时测试；kill_after=N 应答 N 次后自行退出用于重启测试。
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	answered := 0
	for scanner.Scan() {
		var req struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil || req.Method == "" {
			continue
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]string{"name": "fake", "version": "0.1"},
				"capabilities":    map[string]any{"tools": map[string]any{}},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{
				{
					"name":        "echo",
					"description": "回声测试",
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
						"text": map[string]any{"type": "string"},
					}},
				},
				{"name": "slow", "description": "慢工具", "inputSchema": map[string]any{"type": "object"}},
			}}
		case "tools/call":
			var p struct {
				Name string         `json:"name"`
				Args map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.Name == "slow" {
				time.Sleep(3 * time.Second)
			}
			if p.Name == "" {
				writeRPCError(t, req.ID, -32602, "missing name")
				continue
			}
			text, _ := p.Args["text"].(string)
			result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": "echo:" + p.Name + ":" + text}},
			}
		default:
			writeRPCError(t, req.ID, -32601, "method not found")
			continue
		}
		answered++
		writeRPCResult(t, req.ID, result)
		if os.Getenv("HELPER_KILL_AFTER") == "1" && answered >= 2 {
			os.Exit(1) // 模拟意外退出（握手 + 第一次调用应答后挂掉）
		}
	}
	os.Exit(0)
}

func writeRPCResult(t *testing.T, id any, result any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	_, _ = os.Stdout.Write(append(body, '\n'))
}

func writeRPCError(t *testing.T, id any, code int, msg string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": msg},
	})
	_, _ = os.Stdout.Write(append(body, '\n'))
}

// helperConfig 拉起测试二进制自身作为 MCP server 子进程。
func helperConfig(extraEnv ...string) ServerConfig {
	return ServerConfig{
		Name:    "fake server", // 故意带空格，验证名字净化
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env:     append([]string{"GO_WANT_HELPER_PROCESS=1"}, extraEnv...),
	}
}

func TestParseConfigs(t *testing.T) {
	v := []any{
		map[string]any{"name": "fs", "command": "npx", "args": []any{"-y", "srv"}, "env": []any{"A=1"}},
		map[string]any{"name": "", "command": "x"},     // 缺 name → 跳过
		map[string]any{"name": "bad name!", "command": "y"}, // 名字净化
		"not-a-map",                                     // 非法 → 跳过
	}
	cfgs := ParseConfigs(v)
	if len(cfgs) != 2 {
		t.Fatalf("expected 2 valid configs, got %d", len(cfgs))
	}
	if cfgs[0].Name != "fs" || cfgs[0].Command != "npx" || len(cfgs[0].Args) != 2 || cfgs[0].Env[0] != "A=1" {
		t.Fatalf("parse wrong: %+v", cfgs[0])
	}
	if cfgs[1].Name != "bad_name_" {
		t.Fatalf("sanitize wrong: %q", cfgs[1].Name)
	}
	if ParseConfigs(nil) != nil || ParseConfigs(map[string]any{}) != nil {
		t.Fatal("missing config should be nil")
	}
}

func TestClientHandshakeListCall(t *testing.T) {
	cli := NewClient(helperConfig())
	ctx := context.Background()
	if err := cli.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	tools, err := cli.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "echo" {
		t.Fatalf("tools wrong: %+v", tools)
	}
	raw, err := cli.CallTool(ctx, "echo", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if got := mcpResultText(raw); got != "echo:echo:hello" {
		t.Fatalf("call result wrong: %q", got)
	}
}

func TestClientPluginRegistersPrefixedTools(t *testing.T) {
	p := &ClientPlugin{Clients: []*Client{NewClient(helperConfig())}}
	reg := plugin.NewRegistry()
	if err := p.Register(reg, plugin.Deps{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	tools := reg.Tools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 registered tools, got %d", len(tools))
	}
	var echo *agent.Tool
	for i := range tools {
		if tools[i].Name == "mcp_fake_server_echo" {
			echo = &tools[i]
		}
	}
	if echo == nil {
		t.Fatalf("prefixed tool missing: %+v", tools)
	}
	if echo.Scope != agent.ScopeWrite {
		t.Fatalf("scope should default ScopeWrite, got %q", echo.Scope)
	}
	if !strings.HasPrefix(echo.Description, "[MCP:fake server]") {
		t.Fatalf("description prefix wrong: %q", echo.Description)
	}

	// 经 Registry handler 转发调用
	h, ok := reg.Handler("mcp_fake_server_echo")
	if !ok {
		t.Fatal("handler missing")
	}
	out, err := h(nil, context.Background(), map[string]any{"text": "hi"})
	if err != nil {
		t.Fatalf("handler call: %v", err)
	}
	if s, _ := out.(string); s != "echo:echo:hi" {
		t.Fatalf("forwarded result wrong: %+v", out)
	}
}

func TestClientCallTimeout(t *testing.T) {
	cli := NewClient(helperConfig())
	cli.CallTimeout = 200 * time.Millisecond
	cli.HandshakeTimeout = 2 * time.Second
	ctx := context.Background()
	if err := cli.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_, err := cli.CallTool(ctx, "slow", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestClientRestartOnceAfterUnexpectedExit(t *testing.T) {
	cli := NewClient(helperConfig("HELPER_KILL_AFTER=1"))
	cli.CallTimeout = 2 * time.Second
	ctx := context.Background()
	if err := cli.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// 第一次调用成功（此后子进程按 HELPER_KILL_AFTER 自行退出）
	if _, err := cli.CallTool(ctx, "echo", map[string]any{"text": "a"}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // 留时间让子进程退出
	// 第二次调用：读到 EOF 判定连接已死 → 重启一次并成功
	raw, err := cli.CallTool(ctx, "echo", map[string]any{"text": "b"})
	if err != nil {
		t.Fatalf("restart call: %v", err)
	}
	if got := mcpResultText(raw); got != "echo:echo:b" {
		t.Fatalf("restart result wrong: %q", got)
	}
}

func TestClientPluginSkipsDeadServer(t *testing.T) {
	p := &ClientPlugin{Clients: []*Client{
		NewClient(ServerConfig{Name: "dead", Command: "/nonexistent/binary-xyz"}),
		NewClient(helperConfig()),
	}}
	reg := plugin.NewRegistry()
	if err := p.Register(reg, plugin.Deps{}); err != nil {
		t.Fatalf("Register should tolerate dead server: %v", err)
	}
	if len(reg.Tools()) != 2 {
		t.Fatalf("live server tools should still register, got %d", len(reg.Tools()))
	}
}
