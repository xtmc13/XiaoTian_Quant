package main

import (
	"log"
	"os"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── MCP stdio 模式 ──
//
// 供 Claude Code / Cursor 等 MCP 客户端直接挂载本网关的 16 个量化工具：
//
//	./server --mcp-stdio            # 或 XT_MCP_STDIO=1 ./server
//
// 客户端配置示例（Claude Code）：
//
//	claude mcp add xiaotian -- /path/to/server --mcp-stdio
//
// 该模式下不启动 HTTP 网关：协议走 stdin/stdout（JSON-RPC 2.0），日志一律
// 写 stderr，stdio 保持纯净；stdin 关闭（客户端退出）即进程结束。
// 无 flag/env 时行为与之前完全一致。

// mcpStdioMode 判断是否以 MCP stdio 模式启动（flag 或环境变量）。
func mcpStdioMode(args []string, getenv func(string) string) bool {
	for _, a := range args {
		if a == "--mcp-stdio" || a == "-mcp-stdio" {
			return true
		}
	}
	switch strings.ToLower(strings.TrimSpace(getenv("XT_MCP_STDIO"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// runMCPStdio 最小初始化（store + 工具上下文）后阻塞运行 MCP stdio server。
func runMCPStdio() {
	log.SetOutput(os.Stderr) // 协议走 stdout，日志一律 stderr
	if err := store.InitDB(); err != nil {
		log.Printf("[mcp-stdio] WARNING: SQLite init skipped: %v", err)
	}
	store.LoadConfig()
	store.LoadStrategyConfigs()
	// 默认装配工具上下文（币安行情/撮合/组合/策略引擎单例）；写类工具在
	// stdio 模式无 token 过滤，由 MCP 客户端自行约束（同本地 CLI 语义）。
	agent.SetToolContext(agent.NewDefaultToolContext(agent.ToolDeps{}))
	agent.Serve() // 阻塞直到 stdin EOF
}
