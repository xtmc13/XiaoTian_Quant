package agentskills

import (
	"context"
	"fmt"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/agentprofiles"
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// SkillsPlugin 技能插件：可复用的程序性指令，斜杠 /技能名 或 run_skill 调用。
type SkillsPlugin struct {
	Repo *Repo
}

// Info 插件元信息。
func (p *SkillsPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "skills",
		Version:     "1.0.0",
		Kind:        plugin.KindSkills,
		Description: "技能：把常用流程沉淀为可复用指令，支持对话中自动创建",
		Builtin:     true,
	}
}

// UI 前端清单。
func (p *SkillsPlugin) UI() plugin.UIContribution {
	return plugin.UIContribution{
		Nav:   &plugin.NavItem{ID: "skills", Label: "技能", Icon: "zap"},
		Slash: []plugin.SlashItem{{Name: "skills", Description: "打开技能面板"}},
	}
}

// Register 注册 4 个技能工具。
func (p *SkillsPlugin) Register(reg *plugin.Registry, _ plugin.Deps) error {
	strProp := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

	reg.AddTool(agent.Tool{
		Name:        "save_skill",
		Description: "把一段可复用的流程保存为技能（用户说「把刚才的流程存成技能」「记住这个套路」时使用；对话中出现值得复用的操作流程时，也应主动沉淀为技能）。同名覆盖更新。body 写清步骤与产出要求。",
		Scope:       agent.ScopeNotify,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":        strProp("技能名，简短可斜杠调用，如 每日复盘"),
				"description": strProp("一句话说明适用场景"),
				"body":        strProp("程序性指令正文：步骤、检查项、产出格式"),
			},
			"required": []string{"name", "body"},
		},
	}, p.save)

	reg.AddTool(agent.Tool{
		Name:        "run_skill",
		Description: "执行一个已保存的技能：把该技能的程序性指令载入当前上下文并按步骤执行。用户输入 /技能名 或明确要求按某流程办理时使用。",
		Scope:       agent.ScopeNotify,
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"name": strProp("技能名")},
			"required":   []string{"name"},
		},
	}, p.run)

	reg.AddTool(agent.Tool{
		Name:        "list_skills",
		Description: "列出当前用户已保存的技能（名称、说明、使用次数）",
		Scope:       agent.ScopeNotify,
		Schema:      map[string]any{"type": "object", "properties": map[string]any{}},
	}, p.list)

	reg.AddTool(agent.Tool{
		Name:        "delete_skill",
		Description: "删除一个技能",
		Scope:       agent.ScopeNotify,
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"name": strProp("技能名")},
			"required":   []string{"name"},
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

func (p *SkillsPlugin) save(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	name, body := argStr(args, "name"), argStr(args, "body")
	if name == "" || body == "" {
		return nil, fmt.Errorf("name/body 均不能为空")
	}
	s := &Skill{ID: NewID(), UserID: int64(tc.UserID), Name: name, Description: argStr(args, "description"), Body: body, Source: "agent", ProfileID: agentprofiles.NewRepo().ActiveProfileID(int64(tc.UserID))}
	if err := p.Repo.Upsert(s); err != nil {
		return nil, err
	}
	return map[string]any{"name": s.Name, "saved": true}, nil
}

// run 取技能正文并计数；正文作为工具结果回到模型上下文，由模型继续执行步骤。
func (p *SkillsPlugin) run(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	name := argStr(args, "name")
	s, err := p.Repo.GetByName(int64(tc.UserID), name)
	if err != nil {
		return nil, fmt.Errorf("技能 %q 不存在", name)
	}
	_ = p.Repo.TouchUsage(s.ID, s.UserID)
	return "【技能：" + s.Name + "】请严格按以下流程执行：\n" + s.Body, nil
}

func (p *SkillsPlugin) list(tc *agent.ToolContext, _ context.Context, _ map[string]any) (any, error) {
	return p.Repo.ListByUser(int64(tc.UserID), 50)
}

func (p *SkillsPlugin) delete(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	name := argStr(args, "name")
	s, err := p.Repo.GetByName(int64(tc.UserID), name)
	if err != nil {
		return nil, fmt.Errorf("技能 %q 不存在", name)
	}
	if err := p.Repo.Delete(s.ID, s.UserID); err != nil {
		return nil, err
	}
	return map[string]any{"name": name, "deleted": true}, nil
}
