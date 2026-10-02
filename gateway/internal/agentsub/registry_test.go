package agentsub

import (
	"errors"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/agent"
)

// 登记状态流转：running → done / error；摘要截断；最新在前。
func TestRunRegistryTransitions(t *testing.T) {
	uid := int64(910001)
	rec := StartRun(uid, "分析 BTC 走势")
	runs := ListRuns(uid)
	if len(runs) == 0 {
		t.Fatal("StartRun 后应可见 running 记录")
	}
	got := runs[0]
	if !strings.HasPrefix(got.ID, "sa_") || got.Status != "running" || got.StartedAt <= 0 {
		t.Fatalf("running record = %+v", got)
	}
	if got.ID != rec.ID {
		t.Fatalf("id 不一致: %s vs %s", got.ID, rec.ID)
	}

	FinishRun(rec, strings.Repeat("好", 300), false)
	got = ListRuns(uid)[0]
	if got.Status != "done" || len([]rune(got.ResultSummary)) != 160 {
		t.Fatalf("done record = %+v", got)
	}

	// error 路径 + 最新在前
	rec2 := StartRun(uid, "失败任务")
	FinishRun(rec2, "ERROR: boom", true)
	runs = ListRuns(uid)
	if runs[0].ID != rec2.ID || runs[0].Status != "error" || runs[0].ResultSummary != "ERROR: boom" {
		t.Fatalf("error record = %+v", runs[0])
	}
	if runs[1].ID != rec.ID {
		t.Fatalf("应最新在前: %+v", runs)
	}
}

// task 截 80 rune；每用户上限 50（超 cap 丢最旧）；用户隔离。
func TestRunRegistryCapAndIsolation(t *testing.T) {
	uid := int64(910002)
	long := strings.Repeat("长", 200)
	rec := StartRun(uid, long)
	FinishRun(rec, "ok", false)
	if got := ListRuns(uid)[0]; len([]rune(got.Task)) != 80 {
		t.Fatalf("task 应截 80 rune: %d", len([]rune(got.Task)))
	}

	for i := 0; i < RunRegistryCap+10; i++ {
		r := StartRun(uid, "任务")
		FinishRun(r, "ok", false)
	}
	runs := ListRuns(uid)
	if len(runs) != RunRegistryCap {
		t.Fatalf("cap = %d, want %d", len(runs), RunRegistryCap)
	}
	// 最旧（长 task 那条）已被挤出
	for _, r := range runs {
		if len([]rune(r.Task)) == 80 {
			t.Fatal("超 cap 应丢最旧记录")
		}
	}

	// 用户隔离
	if got := ListRuns(910003); len(got) != 0 {
		t.Fatalf("其他用户应无记录: %v", got)
	}
}

// delegate 工具集成：并行子任务每次运行都登记（done / error）。
func TestDelegateRecordsRuns(t *testing.T) {
	uid := uint64(910004)
	p := newPlugin(func(userID int64, prompt string) (string, error) {
		if strings.Contains(prompt, "炸") {
			return "", errors.New("fake exec error")
		}
		return "结果:" + prompt, nil
	})
	_, err := p.delegate(&agent.ToolContext{UserID: uid}, nil, map[string]any{"tasks": []any{"查 BTC", "炸 ETH"}})
	if err != nil {
		t.Fatalf("delegate: %v", err)
	}
	runs := ListRuns(int64(uid))
	if len(runs) != 2 {
		t.Fatalf("runs = %v, want 2 条", runs)
	}
	var done, failed int
	for _, r := range runs {
		switch r.Status {
		case "done":
			done++
			if !strings.HasPrefix(r.ResultSummary, "结果:") {
				t.Fatalf("result_summary = %q", r.ResultSummary)
			}
		case "error":
			failed++
		default:
			t.Fatalf("status = %q", r.Status)
		}
	}
	if done != 1 || failed != 1 {
		t.Fatalf("done=%d error=%d, want 1/1", done, failed)
	}
}
