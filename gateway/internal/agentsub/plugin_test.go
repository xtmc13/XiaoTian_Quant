package agentsub

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

func newPlugin(exec SubagentExecutor) *SubagentsPlugin { return &SubagentsPlugin{Exec: exec} }

func TestDelegate_ParallelAndAggregate(t *testing.T) {
	var mu sync.Mutex
	execCount := 0
	p := newPlugin(func(userID int64, prompt string) (string, error) {
		mu.Lock()
		execCount++
		mu.Unlock()
		time.Sleep(20 * time.Millisecond) // 模拟耗时，验证并行（3 个任务 < 3×串行时间）
		return "结果-" + prompt, nil
	})

	start := time.Now()
	out, err := p.delegate(&agent.ToolContext{UserID: 1}, nil, map[string]any{
		"description": "对比三个币",
		"tasks":       []any{"分析 BTC", "分析 ETH", "分析 SOL"},
	})
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	// 3 任务、并发 3：总耗时应接近 1 倍任务耗时而非 3 倍
	if elapsed > 150*time.Millisecond {
		t.Errorf("疑似串行执行: %v", elapsed)
	}

	text, _ := out.(string)
	if !strings.Contains(text, "【委派目标】对比三个币") {
		t.Errorf("missing description: %q", text)
	}
	for _, want := range []string{"结果-分析 BTC", "结果-分析 ETH", "结果-分析 SOL"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if execCount != 3 {
		t.Errorf("execCount = %d, want 3", execCount)
	}
}

func TestDelegate_Validation(t *testing.T) {
	p := newPlugin(func(int64, string) (string, error) { return "", nil })
	tc := &agent.ToolContext{UserID: 1}

	if _, err := p.delegate(tc, nil, map[string]any{}); err == nil {
		t.Error("empty tasks should error")
	}
	if _, err := p.delegate(tc, nil, map[string]any{"tasks": []any{"a", "b", "c", "d", "e", "f"}}); err == nil {
		t.Error(">5 tasks should error")
	}
	if _, err := p.delegate(tc, nil, map[string]any{"tasks": []any{"a", ""}}); err == nil {
		t.Error("empty task item should error")
	}
	// 未注入执行器
	nilExec := newPlugin(nil)
	if _, err := nilExec.delegate(tc, nil, map[string]any{"tasks": []any{"a"}}); err == nil {
		t.Error("nil executor should error")
	}
}

func TestDelegate_ErrorAndPanicCaptured(t *testing.T) {
	p := newPlugin(func(_ int64, prompt string) (string, error) {
		if prompt == "err" {
			return "", errTest
		}
		if prompt == "panic" {
			panic("boom")
		}
		return "ok", nil
	})
	tc := &agent.ToolContext{UserID: 1}
	out, err := p.delegate(tc, nil, map[string]any{"tasks": []any{"err", "panic", "fine"}})
	if err != nil {
		t.Fatal(err)
	}
	text, _ := out.(string)
	if !strings.Contains(text, "ERROR: 测试错误") {
		t.Errorf("error result missing: %q", text)
	}
	if !strings.Contains(text, "子代理 panic") {
		t.Errorf("panic result missing: %q", text)
	}
	if !strings.Contains(text, "ok") {
		t.Errorf("success result missing: %q", text)
	}
}

var errTest = &testError{}

type testError struct{}

func (e *testError) Error() string { return "测试错误" }

func TestPlugin_Register(t *testing.T) {
	p := newPlugin(nil)
	reg := plugin.NewRegistry()
	if err := p.Register(reg, plugin.Deps{}); err != nil {
		t.Fatal(err)
	}
	tools := reg.Tools()
	if len(tools) != 1 || tools[0].Name != "delegate_task" {
		t.Fatalf("tools: %+v", tools)
	}
	// 子代理工具只读 scope，主 agent 的写工具不会被它带上
	if tools[0].Scope != agent.ScopeRead {
		t.Errorf("scope = %v, want R", tools[0].Scope)
	}
}
