// Package agentmcp 最小可用 MCP stdio 客户端：按 config.yaml 的 mcp_servers
// 配置拉起外部 MCP server 子进程，initialize → tools/list 后把远端工具
// 以 mcp_<server>_<tool> 命名注册进 agent 工具表，调用经 tools/call 转发。
//
// 生命周期说明：进程意外退出时按需重启一次（下次调用前重建连接并重放
// initialize）；网关进程退出时子进程随管道 EOF 自行结束，不做显式
// clean shutdown（MCP stdio server 约定 stdin 关闭即退出）。
package agentmcp

import (
	"fmt"
	"strings"
)

// ServerConfig 单个 MCP server 配置（config.yaml mcp_servers 数组元素）。
type ServerConfig struct {
	Name    string   // 服务名（用于工具名前缀 mcp_<name>_*）
	Command string   // 可执行文件
	Args    []string // 启动参数
	Env     []string // 追加环境变量（KEY=VALUE，可选）
}

// ParseConfigs 从 store 配置 map 的 mcp_servers 值解析 server 列表；
// 缺失/非法条目静默跳过（空配置 = 插件惰性不工作）。
func ParseConfigs(v any) []ServerConfig {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]ServerConfig, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		cfg := ServerConfig{
			Name:    strOf(m["name"]),
			Command: strOf(m["command"]),
			Args:    strSlice(m["args"]),
			Env:     strSlice(m["env"]),
		}
		if cfg.Name == "" || cfg.Command == "" {
			continue
		}
		cfg.Name = SanitizeName(cfg.Name)
		out = append(out, cfg)
	}
	return out
}

// SanitizeName 服务名只保留字母数字与 _ -，其余归一为 _（进工具名前缀）。
func SanitizeName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func strOf(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func strSlice(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// toolName 远端工具的本地注册名：mcp_<server>_<tool>。
func toolName(server, tool string) string {
	return fmt.Sprintf("mcp_%s_%s", SanitizeName(server), SanitizeName(tool))
}
