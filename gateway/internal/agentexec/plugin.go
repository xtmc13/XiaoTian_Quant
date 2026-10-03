// Package agentexec 代码执行插件：把 agent 在文件沙箱里写好的 Python 代码
// 扔进沙箱容器执行（/run），并回传 stdout/stderr。与文件工具（read_file/
// write_file/patch）配合即构成最小"写代码→跑代码"闭环。
package agentexec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// Plugin 代码执行工具插件。
type Plugin struct {
	SandboxURL string // 沙箱服务地址，默认 http://sandbox:9000
}

func (p *Plugin) url() string {
	if p.SandboxURL != "" {
		return p.SandboxURL
	}
	if v := os.Getenv("SANDBOX_URL"); v != "" {
		return v
	}
	return "http://sandbox:9000"
}

// Info 插件清单。
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "code-exec",
		Version:     "1.0.0",
		Kind:        plugin.KindTools,
		Description: "代码执行：在沙箱容器中运行 Python 代码（工作目录=文件沙箱，可配合文件工具写码→跑码）（管理员）",
		Builtin:     true,
	}
}

// UI 无独立面板（工具随会话暴露）。
func (p *Plugin) UI() plugin.UIContribution { return plugin.UIContribution{} }

// Register 注册 run_python 工具（ScopeAdmin）。
func (p *Plugin) Register(reg *plugin.Registry, _ plugin.Deps) error {
	reg.AddTool(agent.Tool{
		Name: "run_python",
		Description: "在沙箱中执行 Python 代码并返回输出。工作目录就是文件沙箱根目录——" +
			"用 write_file 写的脚本可直接以相对路径读写。可用 pandas/numpy 等预装库；" +
			"默认 30s 超时（最多 120s），stdout 最多回传 8000 字符。",
		Scope: agent.ScopeAdmin,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"code": map[string]any{
					"type":        "string",
					"description": "要执行的 Python 代码（完整脚本）",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "超时秒数（1-120，默认 30）",
				},
			},
			"required": []string{"code"},
		},
	}, p.runPython)
	return nil
}

func argStr(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func argInt(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return def
}

type runResponse struct {
	Success  bool   `json:"success"`
	ExitCode *int   `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

func (p *Plugin) runPython(_ *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	code := argStr(args, "code")
	if code == "" {
		return nil, fmt.Errorf("code 不能为空")
	}
	payload, _ := json.Marshal(map[string]any{
		"code":    code,
		"timeout": argInt(args, "timeout", 30),
	})
	client := &http.Client{Timeout: 150 * time.Second}
	resp, err := client.Post(p.url()+"/run", "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("沙箱不可达：%v", err)
	}
	defer resp.Body.Close()
	var rr runResponse
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return nil, fmt.Errorf("沙箱响应解析失败：%v", err)
	}
	out := map[string]any{
		"success": rr.Success,
		"stdout":  rr.Stdout,
	}
	if rr.ExitCode != nil {
		out["exit_code"] = *rr.ExitCode
	}
	if rr.Stderr != "" {
		out["stderr"] = rr.Stderr
	}
	if !rr.Success {
		out["hint"] = "执行失败：先读 stderr 定位错误，修复后用 write_file/patch 改文件再重跑"
	}
	return out, nil
}
