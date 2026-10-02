package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/agentmemory"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 学习闭环：nudge 静态化 + 自动记忆抽取 ──

// nudge 已移除：系统提示词跨 6 个回合保持稳定（不破坏 prompt 缓存），
// 全程不出现"学习提示"；沉淀引导静态存在于 save_memory/save_skill 工具描述中。
func TestAgentChat_NudgeRemoved(t *testing.T) {
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return "好的。", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	buildBody := func(pairs int) string {
		msgs := make([]map[string]string, 0, pairs*2+1)
		for i := 0; i < pairs; i++ {
			msgs = append(msgs,
				map[string]string{"role": "user", "content": fmt.Sprintf("问题%d", i+1)},
				map[string]string{"role": "assistant", "content": fmt.Sprintf("回答%d", i+1)},
			)
		}
		msgs = append(msgs, map[string]string{"role": "user", "content": "继续"})
		body, _ := json.Marshal(map[string]any{"messages": msgs})
		return string(body)
	}
	sysPromptOf := func(t *testing.T) string {
		t.Helper()
		reqs := llm.Requests()
		msgs, _ := reqs[len(reqs)-1]["messages"].([]any)
		sys, _ := msgs[0].(map[string]any)
		return fmt.Sprint(sys["content"])
	}

	var prev string
	for pairs := 0; pairs <= 5; pairs++ {
		w := doAgentChat(t, buildBody(pairs), nil, 88)
		assertEq(t, w.Code, http.StatusOK, "status")
		sys := sysPromptOf(t)
		if strings.Contains(sys, "学习提示") {
			t.Fatalf("回合 %d：系统提示词不应出现学习提示", pairs+1)
		}
		if pairs > 0 && sys != prev {
			t.Fatalf("回合 %d：系统提示词应跨回合保持稳定（不注入动态 nudge）", pairs+1)
		}
		prev = sys
	}

	// 沉淀引导静态化进工具描述：save_memory / save_skill 描述含"主动"措辞。
	if !strings.Contains(prev, "主动调用") || !strings.Contains(prev, "主动沉淀为技能") {
		t.Fatal("系统提示词应含静态沉淀引导（save_memory/save_skill 工具描述）")
	}
}

// parseMemoryExtraction：行格式解析、NONE/非法 kind 跳过、全角冒号、最多 3 条。
func TestParseMemoryExtraction(t *testing.T) {
	out := "- preference: 偏好低杠杆短线\n" +
		"NONE\n" +
		"- fact: 主要交易 BTC/USDT\n" +
		"- weird: 非法类别跳过\n" +
		"没有前缀的行跳过\n" +
		"- observation：全角冒号也支持\n" +
		"- market_note: 超过三条被截断"
	items := parseMemoryExtraction(out)
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3: %+v", len(items), items)
	}
	if items[0].kind != "preference" || items[0].content != "偏好低杠杆短线" {
		t.Errorf("items[0] = %+v", items[0])
	}
	if items[1].kind != "fact" || items[1].content != "主要交易 BTC/USDT" {
		t.Errorf("items[1] = %+v", items[1])
	}
	if items[2].kind != "observation" || items[2].content != "全角冒号也支持" {
		t.Errorf("items[2] = %+v", items[2])
	}

	if got := parseMemoryExtraction("NONE"); len(got) != 0 {
		t.Errorf("NONE should yield 0 items, got %+v", got)
	}
	if got := parseMemoryExtraction("无可抽取内容"); len(got) != 0 {
		t.Errorf("no-dash line should yield 0 items, got %+v", got)
	}
}

// 自动记忆抽取：每 3 个 assistant 回合触发；解析落库 origin=auto/importance=3/
// source_conversation_id；exact 去重。
func TestExtractConversationMemories(t *testing.T) {
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return "- preference: 偏好低杠杆短线\n- fact: 主要交易 BTC\n- weird: 非法类别跳过", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	repo := store.DefaultAgentChatRepo()
	provider, _ := configuredAgentAIProvider()
	memRepo := agentmemory.NewRepo()

	rec := &store.AgentConversationRecord{UserID: 77, Title: "抽取测试"}
	if err := repo.CreateConversation(rec); err != nil {
		t.Fatalf("create conv: %v", err)
	}
	insert := func(role, content string) {
		if err := repo.InsertMessage(&store.AgentMessageRecord{ConversationID: rec.ID, Role: role, Content: content}); err != nil {
			t.Fatalf("insert %s: %v", role, err)
		}
	}
	insert("user", "我平时做 BTC 短线")
	insert("assistant", "了解，可以留意杠杆。")
	insert("user", "我一般用几倍？")
	insert("assistant", "建议低杠杆。")

	r := &agentChatRunner{provider: provider, convID: rec.ID, userID: 77}

	// 节拍未到（2 个 assistant 回合）：不抽取
	r.extractConversationMemories(repo)
	if mems, _ := memRepo.ListByUser(77, "", 10); len(mems) != 0 {
		t.Fatalf("turns=2 不应抽取, got %d memories", len(mems))
	}

	// 第 3 个 assistant 回合：触发抽取，非法 kind 跳过
	insert("assistant", "控制在 2-3 倍较稳妥。")
	r.extractConversationMemories(repo)
	mems, err := memRepo.ListByUser(77, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 2 {
		t.Fatalf("got %d memories, want 2: %+v", len(mems), mems)
	}
	for _, m := range mems {
		if m.Origin != "auto" || m.Importance != 3 || m.SourceConversationID != rec.ID {
			t.Errorf("memory = %+v, want origin=auto importance=3 source=%s", m, rec.ID)
		}
	}

	// exact 去重：同内容再次抽取不新增
	r.extractConversationMemories(repo)
	if mems, _ := memRepo.ListByUser(77, "", 10); len(mems) != 2 {
		t.Fatalf("重复抽取应去重, got %d memories", len(mems))
	}

	// 他人同文不受影响
	other := &store.AgentConversationRecord{UserID: 78, Title: "他人"}
	repo.CreateConversation(other)
	for i := 0; i < 3; i++ {
		if err := repo.InsertMessage(&store.AgentMessageRecord{ConversationID: other.ID, Role: "assistant", Content: "a"}); err != nil {
			t.Fatal(err)
		}
	}
	r2 := &agentChatRunner{provider: provider, convID: other.ID, userID: 78}
	r2.extractConversationMemories(repo)
	if mems, _ := memRepo.ListByUser(78, "", 10); len(mems) != 2 {
		t.Fatalf("用户隔离失败, got %d memories", len(mems))
	}
}
