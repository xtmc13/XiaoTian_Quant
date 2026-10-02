package agentmemory

import (
	"context"
	"fmt"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// MemoryPlugin 记忆插件：跨会话记住用户偏好、交易习惯与市场观察。
type MemoryPlugin struct {
	Repo *Repo
}

// Info 插件元信息。
func (p *MemoryPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "memory",
		Version:     "1.0.0",
		Kind:        plugin.KindMemory,
		Description: "跨会话记忆：偏好、交易习惯与市场观察，自动注入系统提示词",
		Builtin:     true,
	}
}

// UI 前端清单。
func (p *MemoryPlugin) UI() plugin.UIContribution {
	return plugin.UIContribution{
		Nav:   &plugin.NavItem{ID: "memory", Label: "记忆", Icon: "brain"},
		Slash: []plugin.SlashItem{{Name: "memory", Description: "打开记忆面板"}},
	}
}

// Register 注册 4 个记忆工具。
func (p *MemoryPlugin) Register(reg *plugin.Registry, _ plugin.Deps) error {
	strProp := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

	reg.AddTool(agent.Tool{
		Name:        "save_memory",
		Description: "把一条值得长期记住的信息存入记忆（用户偏好、交易习惯、市场观察等）。用户说「记住」时使用。kind: fact(事实)/preference(偏好)/observation(观察)/market_note(市场笔记)",
		Scope:       agent.ScopeNotify,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": strProp("记忆内容，一句话"),
				"kind":    strProp("fact / preference / observation / market_note，默认 fact"),
				"importance": map[string]any{"type": "integer", "description": "重要度 1-5，默认 1；核心偏好/风控纪律给 4-5"},
			},
			"required": []string{"content"},
		},
	}, p.save)

	reg.AddTool(agent.Tool{
		Name:        "search_memory",
		Description: "按关键词检索当前用户的记忆",
		Scope:       agent.ScopeNotify,
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"query": strProp("检索关键词，可空格分隔多个")},
			"required":   []string{"query"},
		},
	}, p.search)

	reg.AddTool(agent.Tool{
		Name:        "list_memories",
		Description: "列出当前用户的记忆（可按 kind 过滤）",
		Scope:       agent.ScopeNotify,
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"kind": strProp("fact/preference/observation/market_note，空 = 全部")},
		},
	}, p.list)

	reg.AddTool(agent.Tool{
		Name:        "delete_memory",
		Description: "删除一条记忆",
		Scope:       agent.ScopeNotify,
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": strProp("记忆 id")},
			"required":   []string{"id"},
		},
	}, p.delete)
	return nil
}

func argStr(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func (p *MemoryPlugin) save(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	content := argStr(args, "content")
	if content == "" {
		return nil, fmt.Errorf("content 不能为空")
	}
	importance := 1
	if f, ok := args["importance"].(float64); ok {
		importance = int(f)
	}
	m := &Memory{
		ID:                   NewID(),
		UserID:               int64(tc.UserID),
		Kind:                 argStr(args, "kind"),
		Content:              content,
		SourceConversationID: argStr(args, "conversation_id"),
		Importance:           importance,
	}
	if err := p.Repo.Create(m); err != nil {
		return nil, err
	}
	return map[string]any{"id": m.ID, "kind": m.Kind}, nil
}

func (p *MemoryPlugin) search(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	return p.Repo.Search(int64(tc.UserID), argStr(args, "query"), 10)
}

func (p *MemoryPlugin) list(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	return p.Repo.ListByUser(int64(tc.UserID), argStr(args, "kind"), 50)
}

func (p *MemoryPlugin) delete(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	if err := p.Repo.Delete(argStr(args, "id"), int64(tc.UserID)); err != nil {
		return nil, err
	}
	return map[string]any{"deleted": true}, nil
}
