// Package factors 实现因子研究框架（对标 QuantDinger 的研究能力）：
// 版本化因子注册表 + 内置因子库 + 因子评价（IC/RankIC/ICIR/分层回测）。
//
// 与 internal/factor（流式 ML 特征管线）不同：本包面向"研究"——输入一段
// 历史 K 线序列，输出与 K 线对齐的因子值序列，再对序列做截面/时序评价。
package factors

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ParamSchema 描述因子可调参数（前端据此渲染表单，评价时透传）。
type ParamSchema struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"` // int | float | string
	Default     any     `json:"default"`
	Min         float64 `json:"min,omitempty"`
	Max         float64 `json:"max,omitempty"`
	Description string  `json:"description,omitempty"`
}

// Def 是因子定义（某一版本）。
type Def struct {
	Name        string                                                  `json:"name"`
	Version     int                                                     `json:"version"`
	Category    string                                                  `json:"category"` // momentum|volatility|trend|volume|mean_reversion
	Description string                                                  `json:"description,omitempty"`
	Params      []ParamSchema                                           `json:"params,omitempty"`
	Defaults    map[string]any                                          `json:"-"`
	Calc        func(bars []model.Bar, params map[string]any) []float64 `json:"-"`

	// Versions 由 List 填充：该因子的全部已注册版本号（升序）。
	// Get 单取路径下保持 nil。
	Versions []int `json:"versions,omitempty"`
}

// Value 是因子在时间轴上的一个取值。
type Value struct {
	Time  int64   `json:"time"`
	Value float64 `json:"value"`
}

// registry 是进程内版本化因子注册表。
type registry struct {
	mu       sync.RWMutex
	versions map[string][]*Def // name -> defs sorted by version asc
}

var defaultRegistry = &registry{versions: make(map[string][]*Def)}

// Register 注册一个因子版本；同名可注册多个版本，List/Get 默认取最新。
func Register(d *Def) error {
	if d == nil || d.Name == "" {
		return fmt.Errorf("factor: name required")
	}
	if d.Calc == nil {
		return fmt.Errorf("factor %s: calc func required", d.Name)
	}
	if d.Version <= 0 {
		d.Version = 1
	}
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()
	for _, existing := range defaultRegistry.versions[d.Name] {
		if existing.Version == d.Version {
			return fmt.Errorf("factor %s version %d already registered", d.Name, d.Version)
		}
	}
	defaultRegistry.versions[d.Name] = append(defaultRegistry.versions[d.Name], d)
	sort.Slice(defaultRegistry.versions[d.Name], func(i, j int) bool {
		return defaultRegistry.versions[d.Name][i].Version < defaultRegistry.versions[d.Name][j].Version
	})
	return nil
}

// MustRegister 注册失败时 panic（仅用于内置因子初始化）。
func MustRegister(d *Def) {
	if err := Register(d); err != nil {
		panic(err)
	}
}

// List 返回全部因子的元信息（每个因子一行，附全部版本号，最新版打头）。
func List() []*Def {
	defaultRegistry.mu.RLock()
	defer defaultRegistry.mu.RUnlock()
	names := make([]string, 0, len(defaultRegistry.versions))
	for name := range defaultRegistry.versions {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*Def, 0, len(names))
	for _, name := range names {
		defs := defaultRegistry.versions[name]
		latest := *defs[len(defs)-1]
		latest.Versions = versionNumbers(defs)
		out = append(out, &latest)
	}
	return out
}

// Get 返回因子指定版本；version<=0 时取最新版本。
func Get(name string, version int) (*Def, error) {
	defaultRegistry.mu.RLock()
	defer defaultRegistry.mu.RUnlock()
	defs, ok := defaultRegistry.versions[strings.ToLower(strings.TrimSpace(name))]
	if !ok || len(defs) == 0 {
		return nil, fmt.Errorf("factor %q not found", name)
	}
	if version <= 0 {
		return defs[len(defs)-1], nil
	}
	for _, d := range defs {
		if d.Version == version {
			return d, nil
		}
	}
	return nil, fmt.Errorf("factor %q version %d not found", name, version)
}

// Categories 返回内置分类清单（前端分组展示用）。
func Categories() []string {
	return []string{"momentum", "volatility", "trend", "volume", "mean_reversion"}
}

// MergeParams 用默认值补齐未提供的参数。
func MergeParams(d *Def, params map[string]any) map[string]any {
	merged := make(map[string]any, len(d.Defaults)+len(params))
	for k, v := range d.Defaults {
		merged[k] = v
	}
	for k, v := range params {
		merged[k] = v
	}
	return merged
}

// IntParam / FloatParam 从参数表安全取数。
func IntParam(params map[string]any, key string, def int) int {
	if v, ok := params[key]; ok {
		switch t := v.(type) {
		case int:
			return t
		case int64:
			return int(t)
		case float64:
			if t == float64(int(t)) {
				return int(t)
			}
		case string:
			var n int
			if _, err := fmt.Sscanf(t, "%d", &n); err == nil {
				return n
			}
		}
	}
	return def
}

// FloatParam 从参数表安全取 float64。
func FloatParam(params map[string]any, key string, def float64) float64 {
	if v, ok := params[key]; ok {
		switch t := v.(type) {
		case int:
			return float64(t)
		case int64:
			return float64(t)
		case float64:
			return t
		case string:
			var n float64
			if _, err := fmt.Sscanf(t, "%g", &n); err == nil {
				return n
			}
		}
	}
	return def
}

// versionNumbers 提取版本号列表（升序）。
func versionNumbers(defs []*Def) []int {
	vs := make([]int, len(defs))
	for i, d := range defs {
		vs[i] = d.Version
	}
	return vs
}

// StringParam 从参数表安全取 string。
func StringParam(params map[string]any, key, def string) string {
	if v, ok := params[key]; ok {
		if s, ok2 := v.(string); ok2 && s != "" {
			return s
		}
	}
	return def
}

// StringSliceParam 从参数表安全取 []string（接受 []any / 逗号分隔串）。
func StringSliceParam(params map[string]any, key string) []string {
	if v, ok := params[key]; ok {
		switch t := v.(type) {
		case []string:
			return t
		case []any:
			out := make([]string, 0, len(t))
			for _, item := range t {
				if s, ok2 := item.(string); ok2 && strings.TrimSpace(s) != "" {
					out = append(out, strings.TrimSpace(s))
				}
			}
			return out
		case string:
			parts := strings.Split(t, ",")
			out := make([]string, 0, len(parts))
			for _, p := range parts {
				if p = strings.TrimSpace(p); p != "" {
					out = append(out, p)
				}
			}
			return out
		}
	}
	return nil
}
