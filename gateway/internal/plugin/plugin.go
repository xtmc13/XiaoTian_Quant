// Package plugin 定义小天助手的插件契约与注册表（万物皆可插件）。
//
// 插件可贡献：工具（Tool+Handler）、UI 清单（侧栏导航、斜杠命令）。
// 后续能力（memory/skills/channel）在同一契约上扩展 Kind 与注册方法。
package plugin

import (
	"context"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/agent"
)

// Kind 插件能力类别。
type Kind string

const (
	KindTools   Kind = "tools"   // 交易/查询类工具
	KindCron    Kind = "cron"    // 定时任务
	KindMemory  Kind = "memory"  // 记忆
	KindSkills  Kind = "skills"  // 技能（可复用流程）
	KindChannel Kind = "channel" // 消息通道（预留）
)

// Info 插件元信息。
type Info struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Kind        Kind   `json:"kind"`
	Description string `json:"description"`
	Builtin     bool   `json:"builtin"`
}

// NavItem 侧栏导航贡献。
type NavItem struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Icon  string `json:"icon"`
}

// SlashItem 斜杠命令贡献（追加进前端命令面板）。
type SlashItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// UIContribution 前端可消费的能力清单；各字段可选。
type UIContribution struct {
	Nav   *NavItem    `json:"nav,omitempty"`
	Slash []SlashItem `json:"slash,omitempty"`
}

// Manifest 单个插件对外清单（GET /api/agent/plugins 的数组元素）。
type Manifest struct {
	Info `json:",inline"`
	UI   UIContribution `json:"ui"`
}

// ToolHandler 工具执行函数（与 handler 包中 agentToolHandlers 同型）。
type ToolHandler = func(tc *agent.ToolContext, ctx context.Context, args map[string]any) (any, error)

// Plugin 插件契约。
type Plugin interface {
	Info() Info
	// UI 返回前端清单贡献（可为零值）。
	UI() UIContribution
	// Register 向注册表贡献能力；deps 为网关侧注入的依赖集合。
	Register(reg *Registry, deps Deps) error
}

// Deps 插件注册时可用的网关依赖（按需取，可为 nil）。
type Deps struct {
	// 预留：DB、通知、LLM 执行器等按插件需要逐步加入。
}

// Registry 能力注册表：所有插件贡献汇聚于此，handler 统一消费。
type Registry struct {
	mu       sync.RWMutex
	tools    []agent.Tool
	handlers map[string]ToolHandler
	uis      map[string]UIContribution
}

// NewRegistry 空注册表。
func NewRegistry() *Registry {
	return &Registry{handlers: map[string]ToolHandler{}, uis: map[string]UIContribution{}}
}

// AddTool 注册工具及其处理器（同名覆盖）。
func (r *Registry) AddTool(t agent.Tool, h ToolHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools = append(r.tools, t)
	r.handlers[t.Name] = h
}

// Tools 全部插件工具（快照）。
func (r *Registry) Tools() []agent.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]agent.Tool, len(r.tools))
	copy(out, r.tools)
	return out
}

// Handler 按名取工具处理器。
func (r *Registry) Handler(name string) (ToolHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[name]
	return h, ok
}

// AddUI 记录插件 UI 贡献。
func (r *Registry) AddUI(pluginName string, ui UIContribution) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.uis[pluginName] = ui
}

// Manager 管理所有插件：注册 + 清单。
type Manager struct {
	reg     *Registry
	plugins []Plugin
	infos   map[string]Info
}

// NewManager 建管理器并立即注册全部内置插件。
func NewManager(deps Deps, plugins ...Plugin) (*Manager, error) {
	m := &Manager{reg: NewRegistry(), infos: map[string]Info{}}
	for _, p := range plugins {
		if p == nil {
			continue
		}
		info := p.Info()
		if err := p.Register(m.reg, deps); err != nil {
			return nil, err
		}
		m.reg.AddUI(info.Name, p.UI())
		m.infos[info.Name] = info
		m.plugins = append(m.plugins, p)
	}
	return m, nil
}

// Registry 能力注册表。
func (m *Manager) Registry() *Registry { return m.reg }

// Plugins 已装配插件列表（装配后只读）。
func (m *Manager) Plugins() []Plugin {
	out := make([]Plugin, len(m.plugins))
	copy(out, m.plugins)
	return out
}

// Manifest 全部插件清单（含 UI 贡献），供 /api/agent/plugins。
func (m *Manager) Manifest() []Manifest {
	out := make([]Manifest, 0, len(m.plugins))
	for _, p := range m.plugins {
		info := p.Info()
		out = append(out, Manifest{Info: info, UI: m.reg.uis[info.Name]})
	}
	return out
}
