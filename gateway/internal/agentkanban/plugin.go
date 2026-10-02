package agentkanban

import (
	"context"
	"fmt"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/agentprofiles"
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// KanbanPlugin 看板插件：助手与用户共用任务看板，跟踪多步任务/计划进度。
type KanbanPlugin struct {
	Repo *Repo
}

// Info 插件元信息。
func (p *KanbanPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "kanban",
		Version:     "1.0.0",
		Kind:        plugin.KindTools,
		Description: "任务看板：todo/doing/done 三列任务卡，跟踪多步任务与计划进度",
		Builtin:     true,
	}
}

// UI 前端清单：侧栏导航 + 斜杠命令。
func (p *KanbanPlugin) UI() plugin.UIContribution {
	return plugin.UIContribution{
		Nav:   &plugin.NavItem{ID: "kanban", Label: "看板", Icon: "kanban"},
		Slash: []plugin.SlashItem{{Name: "kanban", Description: "打开任务看板"}},
	}
}

// Register 注册 4 个看板工具。
func (p *KanbanPlugin) Register(reg *plugin.Registry, _ plugin.Deps) error {
	strProp := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

	reg.AddTool(agent.Tool{
		Name:        "kanban_add",
		Description: "在看板上创建任务卡（跟踪多步任务/计划）。开始一项需要多个步骤的任务前先建卡，完成后用 kanban_complete 收尾",
		Scope:       agent.ScopeWrite,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":       strProp("任务标题，一句话"),
				"description": strProp("任务详情/验收标准，可空"),
			},
			"required": []string{"title"},
		},
	}, p.add)

	reg.AddTool(agent.Tool{
		Name:        "kanban_list",
		Description: "查看当前看板上的任务卡（可按列过滤）",
		Scope:       agent.ScopeRead,
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"column": strProp("todo/doing/done，空 = 全部")},
		},
	}, p.list)

	reg.AddTool(agent.Tool{
		Name:        "kanban_move",
		Description: "把任务卡移动到另一列（开始处理移到 doing，暂停移回 todo）",
		Scope:       agent.ScopeWrite,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":     strProp("任务卡 id"),
				"column": strProp("目标列：todo/doing/done"),
			},
			"required": []string{"id", "column"},
		},
	}, p.move)

	reg.AddTool(agent.Tool{
		Name:        "kanban_complete",
		Description: "完成任务：把任务卡移到 done 并可附一句总结备注",
		Scope:       agent.ScopeWrite,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":      strProp("任务卡 id"),
				"comment": strProp("完成总结，可空"),
			},
			"required": []string{"id"},
		},
	}, p.complete)
	return nil
}

func argStr(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func (p *KanbanPlugin) add(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	title := argStr(args, "title")
	if title == "" {
		return nil, fmt.Errorf("title 不能为空")
	}
	c := &Card{
		ID:          NewID(),
		UserID:      int64(tc.UserID),
		Title:       title,
		Description: argStr(args, "description"),
		Column:      "todo",
		Assignee:    "agent", // 助手建卡 = 助手在跟进
		CreatedBy:   "agent",
		ProfileID:   agentprofiles.NewRepo().ActiveProfileID(int64(tc.UserID)), // 写入当前激活档案
	}
	if err := p.Repo.Create(c); err != nil {
		return nil, err
	}
	return map[string]any{"id": c.ID, "column": c.Column}, nil
}

func (p *KanbanPlugin) list(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	return p.Repo.ListByUser(int64(tc.UserID), argStr(args, "column"))
}

// loadOwned 取卡并校验归属（不存在/他人卡统一报错，不泄露存在性）。
func (p *KanbanPlugin) loadOwned(tc *agent.ToolContext, id string) (*Card, error) {
	if id == "" {
		return nil, fmt.Errorf("id 不能为空")
	}
	c, err := p.Repo.GetByID(id, int64(tc.UserID))
	if err != nil {
		return nil, fmt.Errorf("任务卡不存在: %s", id)
	}
	return c, nil
}

func (p *KanbanPlugin) move(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	column := argStr(args, "column")
	if !ValidColumn(column) {
		return nil, fmt.Errorf("column 必须是 todo/doing/done")
	}
	c, err := p.loadOwned(tc, argStr(args, "id"))
	if err != nil {
		return nil, err
	}
	c.Column = column
	if err := p.Repo.Update(c); err != nil {
		return nil, err
	}
	return map[string]any{"id": c.ID, "column": c.Column}, nil
}

func (p *KanbanPlugin) complete(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	c, err := p.loadOwned(tc, argStr(args, "id"))
	if err != nil {
		return nil, err
	}
	c.Column = "done"
	if comment := argStr(args, "comment"); comment != "" {
		c.Comment = comment
	}
	if err := p.Repo.Update(c); err != nil {
		return nil, err
	}
	return map[string]any{"id": c.ID, "column": "done"}, nil
}
