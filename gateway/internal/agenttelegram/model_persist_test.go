package agenttelegram

import (
	"context"
	"strings"
	"testing"
)

// /model 持久化：写入绑定表；新 Bot 实例（模拟进程重启）从绑定表恢复覆盖并透传给执行器；
// /model default 清除；/status 展示持久化值。
func TestBot_ModelPersisted(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	linkChat(t, repo, 7, 55, "trader")
	fs := newFakeTelegramServer(t, `[]`)

	b := newTestBot(t, fs, nil)
	b.handleMessage(tgMessage{Chat: tgChat{ID: 55}, Text: "/model kimi:k2.5"})

	// 已写入绑定表
	link, err := repo.GetByChatID(55)
	if err != nil {
		t.Fatal(err)
	}
	if link.Model != "kimi:k2.5" {
		t.Fatalf("persisted model = %q", link.Model)
	}

	// 模拟重启：新 Bot 实例内存为空，/status 与入站执行都应拿到持久化模型
	b2 := newTestBot(t, fs, nil)
	b2.handleMessage(tgMessage{Chat: tgChat{ID: 55}, Text: "/status"})
	if text := fs.lastText(); !strings.Contains(text, "kimi:k2.5") {
		t.Errorf("status 应展示持久化模型: %q", text)
	}

	var gotReq *RunRequest
	b2.SetStreamExecutor(func(ctx context.Context, req *RunRequest) (string, error) {
		gotReq = req
		return "ok", nil
	})
	b2.handleMessage(tgMessage{Chat: tgChat{ID: 55}, Text: "hi"})
	if gotReq == nil || gotReq.Model != "kimi:k2.5" {
		t.Errorf("重启后入站执行应透传持久化模型: %+v", gotReq)
	}

	// /model default 清除（库 + 内存）
	b2.handleMessage(tgMessage{Chat: tgChat{ID: 55}, Text: "/model default"})
	link, _ = repo.GetByChatID(55)
	if link.Model != "" {
		t.Fatalf("default 后 persisted model = %q", link.Model)
	}
	if got := b2.modelFor(55); got != "" {
		t.Fatalf("default 后内存覆盖 = %q", got)
	}

	// 清除后入站执行不带模型覆盖
	gotReq = nil
	b2.handleMessage(tgMessage{Chat: tgChat{ID: 55}, Text: "hi again"})
	if gotReq == nil || gotReq.Model != "" {
		t.Errorf("清除后不应带模型覆盖: %+v", gotReq)
	}
}
