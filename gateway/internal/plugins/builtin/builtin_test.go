package builtin

import "testing"

// TestBuiltinWebSearchAndMCPClientWired 校验 P3-C 装配：websearch 插件
// 贡献 web_search/web_extract 工具；mcp-client 插件在无 mcp_servers
// 配置时惰性（注册但不贡献工具）。
func TestBuiltinWebSearchAndMCPClientWired(t *testing.T) {
	mgr := Manager()
	if err := BuildError(); err != nil {
		t.Fatalf("BuildError: %v", err)
	}

	foundWebsearch, foundMCP := false, false
	for _, m := range mgr.Manifest() {
		switch m.Name {
		case "websearch":
			foundWebsearch = true
		case "mcp-client":
			foundMCP = true
		}
	}
	if !foundWebsearch || !foundMCP {
		t.Fatalf("plugins missing: websearch=%v mcp-client=%v", foundWebsearch, foundMCP)
	}

	reg := mgr.Registry()
	for _, name := range []string{"web_search", "web_extract"} {
		if _, ok := reg.Handler(name); !ok {
			t.Fatalf("tool %s not registered", name)
		}
	}
	// 无 mcp_servers 配置：不应出现 mcp_ 前缀工具
	for _, tool := range reg.Tools() {
		if len(tool.Name) > 4 && tool.Name[:4] == "mcp_" {
			t.Fatalf("unexpected mcp tool without config: %s", tool.Name)
		}
	}
}
