// Package agentfiles 本地文件工具插件（管理员专属，ScopeAdmin）：
// 在沙箱根目录内读写/检索文件，写入前自动建检查点，支持回滚。
package agentfiles

import (
	"context"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// checkpointsDir 备份目录名（沙箱根下，检索工具跳过）。
const checkpointsDir = ".checkpoints"

// readFileMaxBytes read_file 单次返回上限（超出截断并标注）。
const readFileMaxBytes = 200 * 1024

// searchMaxResults search_files 返回上限。
const searchMaxResults = 50

// Plugin 本地文件工具插件。
type Plugin struct {
	Root      string // 沙箱根（已 Abs+EvalSymlinks 规范化，构造时 MkdirAll）——普通用户/默认根
	AdminRoot string // 管理员开放根（B 方案：file_root_admin，默认 /workspace；仅角色=admin 的请求使用）
	Repo      *Repo
}

// DefaultAdminRootFallback 未配置 file_root_admin 时的默认开放根：
// 与 docker-compose 里挂进网关/沙箱两容器的工作区挂载点保持一致。
const DefaultAdminRootFallback = "/workspace"

// AdminRootFromConfig 解析管理员开放根：config agent.ai.file_root_admin，缺省 /workspace。
// 与 DefaultRoot 不同：目录不存在时不强行创建（工作区卷由 compose 负责挂载），
// 但存在时仍做 Abs+EvalSymlinks 规范化供前缀校验。
func AdminRootFromConfig(cfg map[string]any) string {
	root := ""
	if agentCfg, ok := cfg["agent"].(map[string]any); ok {
		if aai, ok := agentCfg["ai"].(map[string]any); ok {
			root, _ = aai["file_root_admin"].(string)
		}
	}
	if root == "" {
		root = DefaultAdminRootFallback
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	return root
}

// rootFor 按请求角色选根：管理员→开放根，其余→沙箱根（用户隔离的 B 方案核心）。
func (p *Plugin) rootFor(tc *agent.ToolContext) string {
	if tc != nil && tc.Role == "admin" && p.AdminRoot != "" {
		return p.AdminRoot
	}
	return p.Root
}

// rootForRole AgentFileContent（HTTP 直读，只有 gin role 字符串）同款选根。
func (p *Plugin) rootForRole(role string) string {
	if role == "admin" && p.AdminRoot != "" {
		return p.AdminRoot
	}
	return p.Root
}

// DefaultRoot 解析沙箱根：config agent.ai.file_root 优先，默认 <cwd>/runtime/agent_files；
// 不存在则创建，并规范化为绝对真实路径（符号链接解引用，供前缀校验基准）。
func DefaultRoot(cfg map[string]any) (string, error) {
	root := ""
	if agentCfg, ok := cfg["agent"].(map[string]any); ok {
		if aai, ok := agentCfg["ai"].(map[string]any); ok {
			root, _ = aai["file_root"].(string)
		}
	}
	if root == "" {
		// 默认落 DB 目录（/app/data 卷），重建容器不丢沙箱文件。
		runtimeDir := "runtime"
		if db := os.Getenv("DB_PATH"); db != "" {
			if dir := filepath.Dir(db); dir != "" && dir != "." {
				runtimeDir = dir
			}
		}
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		if filepath.IsAbs(runtimeDir) {
			root = filepath.Join(runtimeDir, "agent_files")
		} else {
			root = filepath.Join(cwd, runtimeDir, "agent_files")
		}
	}
	return CanonicalRoot(root)
}

// CanonicalRoot 规范化沙箱根：MkdirAll + Abs + EvalSymlinks。
func CanonicalRoot(root string) (string, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return "", fmt.Errorf("创建文件沙箱根目录失败: %w", err)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return abs, nil
}

// resolve 把用户给的相对路径解析为沙箱内绝对路径（默认根；测试兼容入口）。
func (p *Plugin) resolve(path string) (string, string, error) {
	return Resolve(p.Root, path)
}

// resolveRoot 按指定根解析相对路径（生产工具按请求角色选根后走这里）。
func (p *Plugin) resolveRoot(root, path string) (string, string, error) {
	return Resolve(root, path)
}

// Resolve 包级路径解析（HTTP 文件读取端点与插件工具共用同一套安全校验）：
// 拒绝 .. 逃逸（不是钳制），已存在路径解引用符号链接后同样不得越界；
// 符号链接防逃逸覆盖"目标不存在时校验最近存在的祖先目录"的情形。
// 返回 (绝对路径, 根内相对路径, error)。
func Resolve(root, path string) (string, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", "", fmt.Errorf("path 不能为空")
	}
	// 前导 / 视为根内绝对写法；Clean 后仍以 .. 开头即试图逃逸，直接拒绝。
	raw := strings.TrimPrefix(filepath.ToSlash(path), "/")
	cleaned := pathpkg.Clean(raw)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", "", fmt.Errorf("路径越出沙箱根目录: %s", path)
	}
	rel := filepath.FromSlash(cleaned)
	abs := filepath.Join(root, rel)
	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return "", "", fmt.Errorf("路径越出沙箱根目录: %s", path)
	}
	// 符号链接防逃逸：目标存在时校验真实路径；不存在时校验最近存在的祖先目录。
	target := abs
	for {
		if real, err := filepath.EvalSymlinks(target); err == nil {
			if real != root && !strings.HasPrefix(real, root+string(filepath.Separator)) {
				return "", "", fmt.Errorf("路径经符号链接越出沙箱根目录: %s", path)
			}
			break
		}
		parent := filepath.Dir(target)
		if parent == target || !strings.HasPrefix(parent, root) {
			break
		}
		target = parent
	}
	return abs, rel, nil
}

// Info 插件元信息。
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "files",
		Version:     "1.0.0",
		Kind:        plugin.KindTools,
		Description: "本地文件工具：沙箱内读写/补丁/检索，写入自动备份并支持检查点回滚（管理员）",
		Builtin:     true,
	}
}

// UI 前端清单：文件回滚面板仅管理员可见（AdminOnly，清单按角色过滤）。
func (p *Plugin) UI() plugin.UIContribution {
	return plugin.UIContribution{
		Nav: &plugin.NavItem{ID: "files", Label: "文件回滚", Icon: "rollback", AdminOnly: true},
	}
}

// Register 注册 5 个文件工具（全部 ScopeAdmin，仅管理员角色可见可用）。
func (p *Plugin) Register(reg *plugin.Registry, _ plugin.Deps) error {
	strProp := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	intProp := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }

	reg.AddTool(agent.Tool{
		Name: "read_file",
		Description: "读取沙箱内文件内容（路径相对于文件根目录）。大文件用 offset/limit 分段读：" +
			"offset 为起始行号（1 起，负数从尾部数，如 -50 读最后 50 行），limit 为行数（默认 2000 行）；" +
			"返回带行号，并附总行数与 next_offset 提示续读位置",
		Scope: agent.ScopeAdmin,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":   strProp("文件路径（相对文件根目录）"),
				"offset": intProp("起始行号（1 起；负数=从尾部数，默认 1）"),
				"limit":  intProp("读取行数（默认 2000，单次最多 2000）"),
			},
			"required": []string{"path"},
		},
	}, p.readFile)

	reg.AddTool(agent.Tool{
		Name:        "write_file",
		Description: "写入沙箱内文件（自动创建父目录；覆盖已有文件前自动备份，可用检查点回滚）。mode=append 为追加模式",
		Scope:       agent.ScopeAdmin,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    strProp("文件路径（相对文件根目录）"),
				"content": strProp("要写入的完整内容"),
				"mode":    strProp("overwrite（默认，覆盖整文件）或 append（追加到文件末尾，不自动加换行）"),
			},
			"required": []string{"path", "content"},
		},
	}, p.writeFile)

	reg.AddTool(agent.Tool{
		Name:        "list_files",
		Description: "列出沙箱目录内容（对标 Kimi Code 的 Glob）：path 留空列根目录；recursive=true 递归子目录（最多 200 条）；返回相对路径、类型（file/dir）、大小，按修改时间倒序",
		Scope:       agent.ScopeAdmin,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":      strProp("目录路径（相对文件根目录，留空=根目录）"),
				"recursive": map[string]any{"type": "boolean", "description": "是否递归子目录（默认 false，最多 200 条）"},
				"limit":     intProp("返回条数上限（默认 100，最多 200）"),
			},
		},
	}, p.listFiles)

	reg.AddTool(agent.Tool{
		Name:        "patch",
		Description: "对沙箱内文件做精确文本替换：find 必须在文件中恰好出现一次，替换为 replace（修改前自动备份，可用检查点回滚）",
		Scope:       agent.ScopeAdmin,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    strProp("文件路径（相对文件根目录）"),
				"find":    strProp("要替换的原文（必须恰好出现一次）"),
				"replace": strProp("替换后的文本"),
			},
			"required": []string{"path", "find", "replace"},
		},
	}, p.patch)

	reg.AddTool(agent.Tool{
		Name:        "search_files",
		Description: "在沙箱内检索文件，两种用法：①按文件名找文件：{\"pattern\":\"config\"}；②在代码/文本里找内容（全文检索）：{\"content\":\"模拟账户\"}（pattern 留空即全文件扫描）；组合：{\"pattern\":\".\", \"content\":\"api_key\"} 限定 py 文件再按内容过滤。最多 50 条命中，内容命中返回 文件:行号 与行文本",
		Scope:       agent.ScopeAdmin,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": strProp("可选：文件名包含的关键字（留空则不限文件名，配合 content 做全文检索）"),
				"content": strProp("可选：文件内容需包含的关键字（命中时返回行号与行文本）；pattern 与 content 至少填一个"),
			},
		},
	}, p.searchFiles)
	return nil
}

func argStr(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// argPath 取文件路径参数：兼容模型爱用的别名（file/filename/file_path/filepath），
// 主参数 path 为空时按序回退，减少"参数名猜错即硬失败"的无效回合。
func argPath(args map[string]any) string {
	if v := argStr(args, "path"); v != "" {
		return v
	}
	for _, k := range []string{"file", "filename", "file_path", "filepath", "target"} {
		if v := argStr(args, k); v != "" {
			return v
		}
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

func argBool(args map[string]any, key string) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	}
	return false
}

// readFile 支持行分页（对标 Kimi Code Read）：offset 1 起、负数从尾部数；
// 返回带行号内容与 total_lines/next_offset，大文件可循环续读。
func (p *Plugin) readFile(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	abs, rel, err := p.resolveRoot(p.rootFor(tc), argPath(args))
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("文件不存在: %s", rel)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("路径是目录: %s", rel)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	truncated := false
	if len(data) > readFileMaxBytes {
		data = data[:readFileMaxBytes]
		truncated = true
	}
	lines := strings.Split(string(data), "\n")
	total := len(lines)
	offset := argInt(args, "offset", 1)
	if offset < 0 {
		offset = total + offset + 1 // -N = 最后 N 行
	}
	if offset < 1 {
		offset = 1
	}
	limit := argInt(args, "limit", 2000)
	if limit < 1 {
		limit = 2000
	}
	if limit > 2000 {
		limit = 2000
	}
	end := offset + limit - 1
	if end > total {
		end = total
	}
	var sb strings.Builder
	for i := offset - 1; i < end; i++ {
		fmt.Fprintf(&sb, "%6d\t%s\n", i+1, lines[i])
	}
	if truncated {
		sb.WriteString("\n…[内容超过 200KB，已截断；分页基于截断后的内容]\n")
	}
	out := map[string]any{
		"path":        rel,
		"size":        info.Size(),
		"total_lines": total,
		"truncated":   truncated,
		"content":     sb.String(),
	}
	if end < total {
		out["next_offset"] = end + 1
	}
	return out, nil
}

func (p *Plugin) writeFile(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	abs, rel, err := p.resolveRoot(p.rootFor(tc), argPath(args))
	if err != nil {
		return nil, err
	}
	content := argStr(args, "content")
	cp, err := p.createCheckpoint(int64(tc.UserID), rel, abs, tc.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("创建检查点失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return nil, err
	}
	appendMode := strings.ToLower(strings.TrimSpace(argStr(args, "mode"))) == "append"
	if appendMode {
		f, err := os.OpenFile(abs, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		if _, err := f.WriteString(content); err != nil {
			f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
	} else if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		return nil, err
	}
	out := map[string]any{"path": rel, "size": len(content), "written": true, "append": appendMode}
	if cp != nil {
		out["checkpoint_id"] = cp.ID
	}
	return out, nil
}

// listFiles 列目录（对标 Kimi Code Glob）：非递归或递归（上限 200 条），按修改时间倒序。
func (p *Plugin) listFiles(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	dirArg := strings.TrimSpace(argPath(args))
	root := p.rootFor(tc)
	abs, rel, err := p.resolveRoot(root, func() string {
		if dirArg == "" {
			return "."
		}
		return dirArg
	}())
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("目录不存在: %s", rel)
	}
	recursive := argBool(args, "recursive")
	limit := argInt(args, "limit", 100)
	if limit < 1 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	type entry struct {
		Path string `json:"path"`
		Type string `json:"type"`
		Size int64  `json:"size"`
		Mtime int64 `json:"mtime"`
	}
	entries := []entry{}
	walkErr := filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == checkpointsDir {
				return filepath.SkipDir // 备份目录不参与检索
			}
			if path != abs {
				if len(entries) < limit {
					if rel, rerr := filepath.Rel(root, path); rerr == nil {
						entries = append(entries, entry{Path: filepath.ToSlash(rel) + "/", Type: "dir"})
					}
				}
				if !recursive {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if len(entries) >= limit {
			return filepath.SkipAll
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		e := entry{Path: filepath.ToSlash(rel), Type: "file"}
		if fi, ferr := d.Info(); ferr == nil {
			e.Size = fi.Size()
			e.Mtime = fi.ModTime().Unix()
		}
		entries = append(entries, e)
		return nil
	})
	if walkErr != nil && walkErr != filepath.SkipAll {
		return nil, walkErr
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Mtime > entries[j].Mtime })
	return map[string]any{"count": len(entries), "entries": entries}, nil
}

func (p *Plugin) patch(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	abs, rel, err := p.resolveRoot(p.rootFor(tc), argPath(args))
	if err != nil {
		return nil, err
	}
	find := argStr(args, "find")
	if find == "" {
		return nil, fmt.Errorf("find 不能为空")
	}
	replace := argStr(args, "replace")
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("文件不存在: %s", rel)
	}
	content := string(data)
	n := strings.Count(content, find)
	if n == 0 {
		return nil, fmt.Errorf("find 在文件中未出现，未做修改")
	}
	if n > 1 {
		return nil, fmt.Errorf("find 在文件中出现 %d 次（要求恰好一次），未做修改", n)
	}
	cp, err := p.createCheckpoint(int64(tc.UserID), rel, abs, tc.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("创建检查点失败: %w", err)
	}
	newContent := strings.Replace(content, find, replace, 1)
	if err := os.WriteFile(abs, []byte(newContent), 0644); err != nil {
		return nil, err
	}
	out := map[string]any{"path": rel, "patched": true}
	if cp != nil {
		out["checkpoint_id"] = cp.ID
	}
	return out, nil
}

// searchHit search_files 的单条命中（content 为空时仅 path/size）。
type searchHit struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
	Text string `json:"text,omitempty"`
	Size int64  `json:"size"`
}

func (p *Plugin) searchFiles(tc *agent.ToolContext, _ context.Context, args map[string]any) (any, error) {
	pattern := strings.ToLower(strings.TrimSpace(argStr(args, "pattern")))
	needle := strings.TrimSpace(argStr(args, "content"))
	// 容错：模型常把搜索词写进 query/text/q/search 等别名参数，或只填其一
	if pattern == "" {
		for _, k := range []string{"query", "text", "q", "search", "keyword", "filename", "name", "path"} {
			if v := strings.TrimSpace(argStr(args, k)); v != "" {
				pattern = strings.ToLower(v)
				break
			}
		}
	}
	if needle == "" {
		for _, k := range []string{"grep", "contains", "body"} {
			if v := strings.TrimSpace(argStr(args, k)); v != "" {
				needle = v
				break
			}
		}
	}
	if pattern == "" && needle == "" {
		return nil, fmt.Errorf("pattern 与 content 至少填一个：按文件名找填 {\"pattern\":\"config\"}；在代码里找内容填 {\"content\":\"模拟账户\"}")
	}
	root := p.rootFor(tc)
	hits := []searchHit{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 跳过不可读条目
		}
		if d.IsDir() {
			if d.Name() == checkpointsDir {
				return filepath.SkipDir // 备份目录不参与检索
			}
			return nil
		}
		if len(hits) >= searchMaxResults {
			return filepath.SkipAll
		}
		// 文件名过滤：pattern 留空 = 不限文件名（全文检索模式）
		if pattern != "" && !strings.Contains(strings.ToLower(d.Name()), pattern) {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		info, _ := d.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		if needle == "" {
			hits = append(hits, searchHit{Path: filepath.ToSlash(rel), Size: size})
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil || len(data) > readFileMaxBytes {
			return nil // 跳过不可读/超大文件的内容匹配
		}
		matched := false
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, needle) {
				matched = true
				if len(hits) < searchMaxResults {
					hits = append(hits, searchHit{Path: filepath.ToSlash(rel), Line: i + 1, Text: strings.TrimSpace(line), Size: size})
				}
			}
		}
		if !matched {
			return nil
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"count": len(hits), "matches": hits}, nil
}
