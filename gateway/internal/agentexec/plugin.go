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
	AdminRoot  string // 管理员开放根（沙箱内路径，如 /workspace）；管理员请求的 cwd 落到此目录
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

// Register 注册 run_python / run_shell / fetch_url 工具（沙箱执行三件套，对标 Kimi Code Bash/FetchURL）。
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
	reg.AddTool(agent.Tool{
		Name: "run_shell",
		Description: "在沙箱中执行 shell 命令并返回输出（对标 Kimi Code 的 Bash）。工作目录为文件沙箱根目录，" +
			"支持 ls/grep/curl/pip/git 等任意命令；默认 30s 超时（最多 300s），stdout/stderr 限量回传；" +
			"执行期间新建/修改的文件会列入返回的 files 清单。",
		Scope: agent.ScopeWrite, // 写类审批门：approval_mode=writes 时需用户确认（同 Kimi Bash 需批准）
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "要执行的 shell 命令（bash -lc 执行）",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "超时秒数（1-300，默认 30）",
				},
			},
			"required": []string{"command"},
		},
	}, p.runShell)
	reg.AddTool(agent.Tool{
		Name: "fetch_url",
		Description: "抓取网页/文本资源并返回正文（对标 Kimi Code 的 FetchURL）。仅支持 http/https；" +
			"HTML 会提取正文文本（去脚本样式标签），文本/JSON/XML 原样返回，上限 2MB。",
		Scope: agent.ScopeRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "要抓取的 http/https URL",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "超时秒数（1-60，默认 15）",
				},
			},
			"required": []string{"url"},
		},
	}, p.fetchURL)
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

// postSandbox 向沙箱指定端点发 JSON 请求并解码响应。
func (p *Plugin) postSandbox(endpoint string, payload map[string]any, out any) error {
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 320 * time.Second}
	resp, err := client.Post(p.url()+endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("沙箱不可达：%v", err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("沙箱响应解析失败：%v", err)
	}
	return nil
}

// cwdFor 按请求角色定沙箱工作目录：管理员→开放根，其余→空串（沙箱默认 /data/agent_files）。
// 返回沙箱容器内路径（B 方案：/workspace 由 compose 同时挂进网关与沙箱）。
func (p *Plugin) cwdFor(tc *agent.ToolContext) string {
	if tc != nil && tc.Role == "admin" && p.AdminRoot != "" {
		return p.AdminRoot
	}
	return ""
}

func (p *Plugin) runPython(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	code := argStr(args, "code")
	if code == "" {
		return nil, fmt.Errorf("code 不能为空")
	}
	payload := map[string]any{
		"code":    code,
		"timeout": argInt(args, "timeout", 30),
	}
	if cwd := p.cwdFor(tc); cwd != "" {
		payload["cwd"] = cwd
	}
	var rr runResponse
	if err := p.postSandbox("/run", payload, &rr); err != nil {
		return nil, err
	}
	return runOutput(rr), nil
}

func (p *Plugin) runShell(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	command := argStr(args, "command")
	// 容错：模型常用 cmd/shell/script 等别名
	if command == "" {
		for _, k := range []string{"cmd", "shell", "script", "input"} {
			if v := argStr(args, k); v != "" {
				command = v
				break
			}
		}
	}
	if command == "" {
		return nil, fmt.Errorf("command 不能为空")
	}
	payload := map[string]any{
		"command": command,
		"timeout": argInt(args, "timeout", 30),
	}
	if cwd := p.cwdFor(tc); cwd != "" {
		payload["cwd"] = cwd
	}
	var rr runResponse
	if err := p.postSandbox("/run-shell", payload, &rr); err != nil {
		return nil, err
	}
	return runOutput(rr), nil
}

// runOutput 统一构造执行类工具输出（含失败提示）。
func runOutput(rr runResponse) map[string]any {
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
	return out
}

// fetchURL 抓取网页正文（沙箱 /fetch，仅 http/https）。
func (p *Plugin) fetchURL(_ *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	url := argStr(args, "url")
	if url == "" {
		for _, k := range []string{"link", "href", "address"} {
			if v := argStr(args, k); v != "" {
				url = v
				break
			}
		}
	}
	if url == "" {
		return nil, fmt.Errorf("url 不能为空")
	}
	var fr struct {
		Success     bool   `json:"success"`
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
		Text        string `json:"text"`
		Error       string `json:"error"`
	}
	if err := p.postSandbox("/fetch", map[string]any{
		"url":     url,
		"timeout": argInt(args, "timeout", 15),
	}, &fr); err != nil {
		return nil, err
	}
	if !fr.Success {
		return nil, fmt.Errorf("%s", fr.Error)
	}
	return map[string]any{
		"url":          fr.URL,
		"content_type": fr.ContentType,
		"text":         fr.Text,
	}, nil
}
