package agentsub

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// SubagentExecutor 子代理执行器（main 注入 handler.RunAgentSub：只读 scope）。
type SubagentExecutor func(userID int64, prompt string) (string, error)

const (
	// MaxParallel 最大并行子代理数。
	MaxParallel = 3
	// MaxTasks 单次委派任务数上限。
	MaxTasks = 5
	// TaskTimeout 单个子任务超时。
	TaskTimeout = 3 * time.Minute
)

// SubagentsPlugin 子代理插件：主 agent 并行派发只读子任务并汇总结果。
type SubagentsPlugin struct {
	Exec SubagentExecutor
}

// Info 插件元信息。
func (p *SubagentsPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "subagents",
		Version:     "1.0.0",
		Kind:        plugin.KindTools,
		Description: "子代理：把可拆分的分析任务并行派给只读子代理（无下单/写权限），结果汇总",
		Builtin:     true,
	}
}

// UI 前端清单。
func (p *SubagentsPlugin) UI() plugin.UIContribution {
	return plugin.UIContribution{}
}

// Register 注册 delegate_task 工具。
func (p *SubagentsPlugin) Register(reg *plugin.Registry, _ plugin.Deps) error {
	reg.AddTool(agent.Tool{
		Name:        "delegate_task",
		Description: "把一组可独立完成的分析/查询子任务并行委派给只读子代理（无交易写权限），返回每个子任务的结果汇总。适合多标的对比、多维度排查等可拆分场景；最多 5 个任务",
		Scope:       agent.ScopeRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description": map[string]any{"type": "string", "description": "本次委派的总体目标（一句话）"},
				"tasks": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "1-5 个子任务，每个自包含上下文（子代理看不到当前对话历史）",
				},
			},
			"required": []string{"tasks"},
		},
	}, p.delegate)
	return nil
}

// delegate 并行执行子任务并汇总。
func (p *SubagentsPlugin) delegate(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	raw, ok := args["tasks"].([]any)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("tasks 不能为空")
	}
	if len(raw) > MaxTasks {
		return nil, fmt.Errorf("tasks 最多 %d 个", MaxTasks)
	}
	tasks := make([]string, 0, len(raw))
	for _, t := range raw {
		s, _ := t.(string)
		if strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("子任务不能为空串")
		}
		tasks = append(tasks, s)
	}

	if p.Exec == nil {
		return nil, fmt.Errorf("子代理执行器未配置")
	}

	results := make([]string, len(tasks))
	sem := make(chan struct{}, MaxParallel)
	var wg sync.WaitGroup
	for i, task := range tasks {
		wg.Add(1)
		go func(i int, task string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = p.runOne(tc.UserID, task)
		}(i, task)
	}
	wg.Wait()

	var b strings.Builder
	if desc := argStr(args, "description"); desc != "" {
		fmt.Fprintf(&b, "【委派目标】%s\n\n", desc)
	}
	for i, r := range results {
		fmt.Fprintf(&b, "【子任务 %d/%d】%s\n%s\n\n", i+1, len(tasks), tasks[i], r)
	}
	return b.String(), nil
}

// runOne 执行单个子任务（带超时与 panic 防护；panic 在执行 goroutine 内捕获）。
func (p *SubagentsPlugin) runOne(userID uint64, task string) (result string) {
	type out struct {
		reply string
		err   error
	}
	ch := make(chan out, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- out{"", fmt.Errorf("子代理 panic: %v", r)}
			}
		}()
		reply, err := p.Exec(int64(userID), task)
		ch <- out{reply, err}
	}()
	select {
	case o := <-ch:
		if o.err != nil {
			return "ERROR: " + truncate(o.err.Error(), 300)
		}
		return truncate(o.reply, 1500)
	case <-time.After(TaskTimeout):
		return fmt.Sprintf("ERROR: 子任务超时（%d 分钟）", int(TaskTimeout.Minutes()))
	}
}

func argStr(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…[truncated]"
}
