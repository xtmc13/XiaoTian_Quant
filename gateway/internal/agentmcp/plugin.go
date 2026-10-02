package agentmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// ClientPlugin MCP 客户端插件：把配置的外部 MCP server 工具桥接进 agent。
// Clients 由装配侧（plugins/builtin）从 config.yaml 的 mcp_servers 解析注入；
// 空列表 = 惰性不工作。单个 server 拉起/握手失败只记日志，不拖垮整个装配。
type ClientPlugin struct {
	Clients []*Client
}

// Info 插件元信息。
func (p *ClientPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "mcp-client",
		Version:     "1.0.0",
		Kind:        plugin.KindTools,
		Description: "外部 MCP server 工具桥接（config.yaml mcp_servers，stdio JSON-RPC）",
		Builtin:     true,
	}
}

// UI 前端清单（无贡献）。
func (p *ClientPlugin) UI() plugin.UIContribution { return plugin.UIContribution{} }

// Register 拉起各 server、tools/list 后以 mcp_<server>_<tool> 注册工具。
func (p *ClientPlugin) Register(reg *plugin.Registry, _ plugin.Deps) error {
	for _, cli := range p.Clients {
		if cli == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), cli.handshakeTimeout())
		err := cli.Start(ctx)
		cancel()
		if err != nil {
			log.Printf("[mcp-client] server %s 启动失败，跳过: %v", cli.Name(), err)
			continue
		}
		ctx, cancel = context.WithTimeout(context.Background(), cli.handshakeTimeout())
		tools, err := cli.ListTools(ctx)
		cancel()
		if err != nil {
			log.Printf("[mcp-client] server %s tools/list 失败，跳过: %v", cli.Name(), err)
			continue
		}
		for _, t := range tools {
			p.registerTool(reg, cli, t)
		}
		log.Printf("[mcp-client] server %s 已接入 %d 个工具", cli.Name(), len(tools))
	}
	return nil
}

// registerTool 把单个远端工具注册进 Registry（scope 默认 ScopeWrite）。
func (p *ClientPlugin) registerTool(reg *plugin.Registry, cli *Client, t RemoteTool) {
	localName := toolName(cli.Name(), t.Name)
	desc := fmt.Sprintf("[MCP:%s] %s", cli.Name(), t.Description)
	schema := t.InputSchema
	if schema == nil {
		schema = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	remoteName := t.Name

	reg.AddTool(agent.Tool{
		Name:        localName,
		Description: desc,
		Scope:       agent.ScopeWrite,
		Schema:      schema,
	}, func(_ *agent.ToolContext, ctx context.Context, args map[string]any) (any, error) {
		ctx, cancel := context.WithTimeout(ctx, cli.callTimeout())
		defer cancel()
		raw, err := cli.CallTool(ctx, remoteName, args)
		if err != nil {
			return nil, err
		}
		return mcpResultText(raw), nil
	})
}

// mcpResultText 把 tools/call 的 result 转成模型可读文本：
// 标准 MCP content 数组取 text 拼接；非标准结构回 JSON 原文。
func mcpResultText(raw json.RawMessage) string {
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.Content == nil {
		return string(raw)
	}
	text := ""
	for i, c := range result.Content {
		if c.Type != "text" {
			continue
		}
		if i > 0 && text != "" {
			text += "\n"
		}
		text += c.Text
	}
	if result.IsError {
		return "远端工具错误: " + text
	}
	return text
}
