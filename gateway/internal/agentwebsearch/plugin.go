package agentwebsearch

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// Plugin 联网搜索插件（websearch）：web_search / web_extract 两个只读工具。
type Plugin struct {
	APIKey       string       // Brave API key（装配时从 config 注入）；空 = 直接走 DuckDuckGo
	BraveBaseURL string       // 可注入覆盖（测试指向 httptest）
	DDGBaseURL   string       // 同上
	Client       *http.Client // 可注入覆盖（默认 10s 超时）
}

// Info 插件元信息。
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "websearch",
		Version:     "1.0.0",
		Kind:        plugin.KindTools,
		Description: "联网搜索与网页正文抽取（Brave API / DuckDuckGo 兜底）",
		Builtin:     true,
	}
}

// UI 前端清单（无贡献）。
func (p *Plugin) UI() plugin.UIContribution { return plugin.UIContribution{} }

// Register 注册 web_search / web_extract 工具。
func (p *Plugin) Register(reg *plugin.Registry, _ plugin.Deps) error {
	strProp := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

	reg.AddTool(agent.Tool{
		Name:        "web_search",
		Description: "联网搜索实时资讯（新闻、公告、市场动态），返回标题/链接/摘要列表。需要最新信息、项目动态、公告原文时使用。",
		Scope:       agent.ScopeRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": strProp("搜索关键词，可空格分隔多个"),
				"limit": map[string]any{"type": "integer", "description": "返回条数，默认 5，最多 10"},
			},
			"required": []string{"query"},
		},
	}, p.search)

	reg.AddTool(agent.Tool{
		Name:        "web_extract",
		Description: "抓取指定网页并抽取可读正文（去脚本/样式/标签，截断 4000 字符）。拿到搜索结果的链接后深入阅读时使用。",
		Scope:       agent.ScopeRead,
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"url": strProp("要抓取的 http(s) 链接")},
			"required":   []string{"url"},
		},
	}, p.extract)
	return nil
}

// search 执行搜索：Brave（有 key）→ DuckDuckGo 兜底；全失败回软错误字符串。
func (p *Plugin) search(_ *agent.ToolContext, ctx context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query 不能为空")
	}
	limit := 5
	if f, ok := args["limit"].(float64); ok && f > 0 {
		limit = int(f)
	}
	if limit > 10 {
		limit = 10
	}

	var lastErr error
	if p.APIKey != "" {
		results, err := p.braveSearch(ctx, query, limit)
		if err == nil {
			return map[string]any{"provider": "brave", "count": len(results), "results": results}, nil
		}
		lastErr = err // Brave 失败回落 DDG
	}
	results, err := p.ddgSearch(ctx, query, limit)
	if err == nil && len(results) > 0 {
		return map[string]any{"provider": "duckduckgo", "count": len(results), "results": results}, nil
	}
	if err != nil {
		lastErr = err
	}
	// 软错误：以正常结果返回提示，不用 error 中断对话轮次。
	return fmt.Sprintf("搜索暂不可用：%v", lastErr), nil
}

// extract 抓取网页正文；失败同样回软错误字符串。
func (p *Plugin) extract(_ *agent.ToolContext, ctx context.Context, args map[string]any) (any, error) {
	rawURL, _ := args["url"].(string)
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url 不能为空")
	}
	text, err := p.extractText(ctx, rawURL)
	if err != nil {
		return fmt.Sprintf("网页抓取暂不可用：%v", err), nil
	}
	return map[string]any{"url": rawURL, "chars": len([]rune(text)), "text": text}, nil
}
