package agent

import "testing"

// ── RBAC 工具门控：ScopeAdmin 仅管理员可见（含 token 属主角色过滤）──

func rbacTestTools() []Tool {
	return []Tool{
		{Name: "get_market_data", Scope: ScopeRead},
		{Name: "place_paper_order", Scope: ScopeWrite},
		{Name: "read_file", Scope: ScopeAdmin},
		{Name: "write_file", Scope: ScopeAdmin},
	}
}

func toolNames(tools []Tool) map[string]bool {
	out := map[string]bool{}
	for _, t := range tools {
		out[t.Name] = true
	}
	return out
}

func TestFilterToolsByRole_AdminSeesFileTools(t *testing.T) {
	got := toolNames(FilterToolsByRole(rbacTestTools(), "admin"))
	for _, name := range []string{"get_market_data", "place_paper_order", "read_file", "write_file"} {
		if !got[name] {
			t.Errorf("admin 工具集缺少 %s", name)
		}
	}
}

func TestFilterToolsByRole_OrdinaryUserNoFileTools(t *testing.T) {
	for _, role := range []string{"user", "", "moderator"} {
		got := toolNames(FilterToolsByRole(rbacTestTools(), role))
		if got["read_file"] || got["write_file"] {
			t.Errorf("role=%q 不应看到 ScopeAdmin 工具: %v", role, got)
		}
		if !got["get_market_data"] || !got["place_paper_order"] {
			t.Errorf("role=%q 普通工具集应保持不变: %v", role, got)
		}
	}
}

func TestFilterTools_AdminScopeToken(t *testing.T) {
	tm := GetTokenManager()
	// 带 "A" scope 的 token 能过 scope 过滤（角色门控在调用侧另行叠加）
	got := toolNames(tm.FilterTools(rbacTestTools(), []string{"R", "A"}))
	if !got["read_file"] || !got["get_market_data"] {
		t.Fatalf("scope R,A 应放行读 + 管理员工具: %v", got)
	}
	if got["place_paper_order"] {
		t.Errorf("scope R,A 不应放行写工具: %v", got)
	}
	// 不带 "A" 的 token 拿不到 ScopeAdmin 工具（即使属主是 admin，也要过 scope 这道）
	got = toolNames(tm.FilterTools(rbacTestTools(), []string{"R", "W"}))
	if got["read_file"] || got["write_file"] {
		t.Errorf("scope R,W 不应放行管理员工具: %v", got)
	}
	// 单词式别名
	got = toolNames(tm.FilterTools(rbacTestTools(), []string{"ADMIN"}))
	if !got["read_file"] {
		t.Errorf("别名 ADMIN 应规范化为 ScopeAdmin: %v", got)
	}
}
