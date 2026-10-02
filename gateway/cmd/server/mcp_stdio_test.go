package main

import "testing"

// TestMCPStdioMode 校验 --mcp-stdio flag / XT_MCP_STDIO env 的分支逻辑：
// 只有显式开启才进 MCP stdio 模式，默认行为（HTTP 网关）不受影响。
func TestMCPStdioMode(t *testing.T) {
	getenv := func(kv map[string]string) func(string) string {
		return func(k string) string { return kv[k] }
	}
	cases := []struct {
		name string
		args []string
		env  map[string]string
		want bool
	}{
		{"默认无 flag 无 env", nil, nil, false},
		{"长 flag", []string{"--mcp-stdio"}, nil, true},
		{"单横线 flag", []string{"-mcp-stdio"}, nil, true},
		{"flag 混在其他参数里", []string{"--foo", "--mcp-stdio"}, nil, true},
		{"无关 flag", []string{"--other"}, nil, false},
		{"env=1", nil, map[string]string{"XT_MCP_STDIO": "1"}, true},
		{"env=true", nil, map[string]string{"XT_MCP_STDIO": "true"}, true},
		{"env=yes 大小写", nil, map[string]string{"XT_MCP_STDIO": " YES "}, true},
		{"env=0", nil, map[string]string{"XT_MCP_STDIO": "0"}, false},
		{"env 空串", nil, map[string]string{"XT_MCP_STDIO": ""}, false},
	}
	for _, tc := range cases {
		if got := mcpStdioMode(tc.args, getenv(tc.env)); got != tc.want {
			t.Fatalf("%s: mcpStdioMode(%v) = %v, want %v", tc.name, tc.args, got, tc.want)
		}
	}
}
