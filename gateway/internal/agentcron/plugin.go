package agentcron

import (
	"context"
	"fmt"
	"time"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// CronPlugin 定时任务插件：向助手贡献 cron 管理工具 + 前端面板入口。
type CronPlugin struct {
	Repo  *Repo
	Sched *Scheduler
	// ScheduleResolver 自然语言 → cron 转换器（main 注入 handler 的 LLM 解析，可为 nil）
	ScheduleResolver func(string) (string, error)
}

// Info 插件元信息。
func (p *CronPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "cron",
		Version:     "1.0.0",
		Kind:        plugin.KindCron,
		Description: "定时任务：到点自动执行一段指令，结果投递到面板或消息通道",
		Builtin:     true,
	}
}

// UI 前端清单：侧栏导航 + 斜杠命令。
func (p *CronPlugin) UI() plugin.UIContribution {
	return plugin.UIContribution{
		Nav:   &plugin.NavItem{ID: "cron", Label: "定时任务", Icon: "clock"},
		Slash: []plugin.SlashItem{{Name: "cron", Description: "打开定时任务面板"}},
	}
}

// Register 注册 5 个 cron 管理工具。
func (p *CronPlugin) Register(reg *plugin.Registry, _ plugin.Deps) error {
	strProp := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

	reg.AddTool(agent.Tool{
		Name:        "create_scheduled_job",
		Description: "创建一个定时任务（到点自动执行 prompt 并把结果投递到面板或消息通道）。schedule 支持 cron 表达式（\"0 8 * * *\"=每天 8:00，\"*/30 * * * *\"=每 30 分钟）或自然语言（\"每天早上八点\"，将由模型转换）",
		Scope:       agent.ScopeNotify,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":     strProp("任务名，如「每日持仓汇报」"),
				"prompt":   strProp("到点后交给助手执行的指令"),
				"schedule": strProp("5 段 cron 表达式：分 时 日 月 周"),
				"channel":  strProp("结果投递：web(默认,面板可见) / telegram / lark / dingtalk / email"),
				"timezone": strProp("IANA 时区，默认 Asia/Shanghai"),
			},
			"required": []string{"name", "prompt", "schedule"},
		},
	}, p.createJob)

	reg.AddTool(agent.Tool{
		Name:        "list_scheduled_jobs",
		Description: "列出当前用户全部定时任务（名称、表达式、下次执行、上次结果）",
		Scope:       agent.ScopeNotify,
		Schema:      map[string]any{"type": "object", "properties": map[string]any{}},
	}, p.listJobs)

	reg.AddTool(agent.Tool{
		Name:        "toggle_scheduled_job",
		Description: "启用或停用定时任务",
		Scope:       agent.ScopeNotify,
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": strProp("任务 id"), "enabled": map[string]any{"type": "boolean", "description": "true 启用 / false 停用"}},
			"required":   []string{"id", "enabled"},
		},
	}, p.toggleJob)

	reg.AddTool(agent.Tool{
		Name:        "delete_scheduled_job",
		Description: "删除定时任务",
		Scope:       agent.ScopeNotify,
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": strProp("任务 id")},
			"required":   []string{"id"},
		},
	}, p.deleteJob)

	reg.AddTool(agent.Tool{
		Name:        "run_scheduled_job",
		Description: "立即执行一次定时任务（不影响正常调度）",
		Scope:       agent.ScopeNotify,
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": strProp("任务 id")},
			"required":   []string{"id"},
		},
	}, p.runJobNow)
	return nil
}

func argStr(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func (p *CronPlugin) createJob(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	name, prompt, schedule := argStr(args, "name"), argStr(args, "prompt"), argStr(args, "schedule")
	if name == "" || prompt == "" || schedule == "" {
		return nil, fmt.Errorf("name/prompt/schedule 均不能为空")
	}
	channel := argStr(args, "channel")
	if channel == "" {
		channel = "web"
	}
	resolved, err := ResolveSchedule(schedule, p.ScheduleResolver)
	if err != nil {
		return nil, err
	}
	next, err := NextRun(resolved, argStr(args, "timezone"), time.Now())
	if err != nil {
		return nil, fmt.Errorf("schedule 无效：%w", err)
	}
	j := &Job{ID: NewID(), UserID: int64(tc.UserID), Name: name, Prompt: prompt, Schedule: resolved,
		Timezone: argStr(args, "timezone"), Channel: channel, Enabled: true, NextRunAt: next}
	if err := p.Repo.Create(j); err != nil {
		return nil, err
	}
	return map[string]any{"id": j.ID, "name": j.Name, "schedule": resolved, "next_run_at": j.NextRunAt}, nil
}

func (p *CronPlugin) listJobs(tc *agent.ToolContext, _ context.Context, _ map[string]any) (any, error) {
	jobs, err := p.Repo.ListByUser(int64(tc.UserID))
	if err != nil {
		return nil, err
	}
	type slim struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Schedule   string `json:"schedule"`
		Enabled    bool   `json:"enabled"`
		NextRunAt  int64  `json:"next_run_at"`
		LastStatus string `json:"last_status"`
	}
	out := []slim{}
	for _, j := range jobs {
		out = append(out, slim{j.ID, j.Name, j.Schedule, j.Enabled, j.NextRunAt, j.LastStatus})
	}
	return out, nil
}

func (p *CronPlugin) toggleJob(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	id := argStr(args, "id")
	enabled, _ := args["enabled"].(bool)
	if err := p.Repo.SetEnabled(id, int64(tc.UserID), enabled); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "enabled": enabled}, nil
}

func (p *CronPlugin) deleteJob(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	id := argStr(args, "id")
	if err := p.Repo.Delete(id, int64(tc.UserID)); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "deleted": true}, nil
}

func (p *CronPlugin) runJobNow(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	id := argStr(args, "id")
	j, err := p.Sched.RunNow(id, int64(tc.UserID))
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": j.ID, "started": true}, nil
}
