package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 审批流测试基建 ──

// withApprovalMode 设置审批模式测试缝（"" = 读配置），用例结束恢复。
func withApprovalMode(t *testing.T, mode string) {
	t.Helper()
	prev := agentChatApprovalMode
	agentChatApprovalMode = mode
	t.Cleanup(func() { agentChatApprovalMode = prev })
}

// callApprove 直接调用审批端点（注册表进程全局，独立于 chat 路由）。
func callApprove(t *testing.T, id string, approve bool) *httptest.ResponseRecorder {
	t.Helper()
	r := setupRouter()
	r.POST("/agent/chat/approve", AgentChatApprove)
	body := fmt.Sprintf(`{"id":%q,"approve":%v}`, id, approve)
	req := httptest.NewRequest(http.MethodPost, "/agent/chat/approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// waitPendingApproval 轮询在途审批注册表，返回首个审批 id（chat 在 goroutine 中阻塞等待）。
func waitPendingApproval(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		found := ""
		agentApprovalRegistry.Range(func(k, _ any) bool { found, _ = k.(string); return false })
		if found != "" {
			return found
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("approval_request 未出现（注册表为空）")
	return ""
}

// pendingChat 异步 chat 的句柄：done 关闭后 w 可读（channel 关闭建立 happens-before）。
type pendingChat struct {
	w    *httptest.ResponseRecorder
	done chan struct{}
}

// doAgentChatAsync 在 goroutine 中发起流式 chat（审批等待期间主测试协程去决议）。
func doAgentChatAsync(t *testing.T, body string, jwtUID int) *pendingChat {
	t.Helper()
	p := &pendingChat{done: make(chan struct{})}
	go func() {
		defer close(p.done)
		p.w = doAgentChat(t, body, nil, jwtUID)
	}()
	return p
}

// approvalEventFrom 解析 SSE 中的 approval_request 事件 payload（无则 nil）。
func approvalEventFrom(t *testing.T, raw string) map[string]any {
	t.Helper()
	for _, ev := range parseSSE(t, raw) {
		if ev.Event == "approval_request" {
			var p map[string]any
			if err := json.Unmarshal([]byte(ev.Data), &p); err != nil {
				t.Fatalf("approval_request data not JSON: %v", err)
			}
			return p
		}
	}
	return nil
}

// ── 用例 ──

// 模式 off（默认）：写工具直接执行，无 approval_request 事件，注册表为空。
func TestAgentApproval_OffNoPause(t *testing.T) {
	withApprovalMode(t, "off")
	matcher := &chatMockMatcher{}
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{}, matcher)
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		if !reqHasToolMessage(req) {
			return "", []ai.ToolCall{{
				Name:      "place_paper_order",
				Arguments: `{"symbol":"BTCUSDT","side":"BUY","order_type":"MARKET","quantity":0.1}`,
			}}
		}
		return "已提交模拟买单。", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	w := doAgentChat(t, `{"messages":[{"role":"user","content":"买 0.1 个 BTC"}],"stream":true}`, nil, 7)
	assertEq(t, w.Code, http.StatusOK, "status code")
	assertEq(t, int(matcher.LastUser()), 7, "tool executed immediately")
	if ev := approvalEventFrom(t, w.Body.String()); ev != nil {
		t.Fatalf("mode off 不应有 approval_request: %v", ev)
	}
	empty := true
	agentApprovalRegistry.Range(func(_, _ any) bool { empty = false; return false })
	assertTrue(t, empty, "注册表应为空")
}

// 模式 writes：写工具执行前暂停并发 approval_request；approve 后执行并继续回合。
func TestAgentApproval_ApproveExecutes(t *testing.T) {
	withApprovalMode(t, "writes")
	matcher := &chatMockMatcher{}
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{}, matcher)
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		if !reqHasToolMessage(req) {
			return "", []ai.ToolCall{{
				Name:      "place_paper_order",
				Arguments: `{"symbol":"BTCUSDT","side":"BUY","order_type":"MARKET","quantity":0.1}`,
			}}
		}
		return "已提交模拟买单。", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	pc := doAgentChatAsync(t, `{"messages":[{"role":"user","content":"买 0.1 个 BTC"}],"stream":true}`, 7)

	id := waitPendingApproval(t)
	if !strings.HasPrefix(id, "ap_") {
		t.Fatalf("approval id = %q, want ap_ 前缀", id)
	}
	// 等待期间工具尚未执行
	assertEq(t, int(matcher.LastUser()), 0, "审批前工具不应执行")

	aw := callApprove(t, id, true)
	assertEq(t, aw.Code, http.StatusOK, "approve status")
	var abody map[string]any
	if err := json.Unmarshal(aw.Body.Bytes(), &abody); err != nil || abody["success"] != true {
		t.Fatalf("approve body = %q", aw.Body.String())
	}
	<-pc.done
	w := pc.w

	assertEq(t, w.Code, http.StatusOK, "chat status")
	assertEq(t, int(matcher.LastUser()), 7, "批准后工具执行")
	ev := approvalEventFrom(t, w.Body.String())
	if ev == nil {
		t.Fatalf("缺 approval_request 事件: %q", w.Body.String())
	}
	if ev["tool"] != "place_paper_order" || ev["id"] != id {
		t.Fatalf("approval_request = %v", ev)
	}
	if summary, _ := ev["args_summary"].(string); summary == "" || len([]rune(summary)) > agentChatSummaryLimit {
		t.Fatalf("args_summary = %q", summary)
	}
	// 已决议的 id 再从注册表消失：二次决议 404
	aw2 := callApprove(t, id, true)
	assertEq(t, aw2.Code, http.StatusNotFound, "重复决议应 404")
}

// 模式 writes + deny：不执行工具，把「用户拒绝了该操作」回传模型继续回合（不终结）。
func TestAgentApproval_DenyFeedsModel(t *testing.T) {
	withApprovalMode(t, "writes")
	matcher := &chatMockMatcher{}
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{}, matcher)
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		if !reqHasToolMessage(req) {
			return "", []ai.ToolCall{{
				Name:      "place_paper_order",
				Arguments: `{"symbol":"BTCUSDT","side":"BUY","order_type":"MARKET","quantity":0.1}`,
			}}
		}
		return "好的，已取消该操作。", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	pc := doAgentChatAsync(t, `{"messages":[{"role":"user","content":"买 0.1 个 BTC"}],"stream":true}`, 7)
	id := waitPendingApproval(t)
	callApprove(t, id, false)
	<-pc.done
	w := pc.w

	assertEq(t, w.Code, http.StatusOK, "chat status")
	assertEq(t, int(matcher.LastUser()), 0, "拒绝后工具不应执行")

	// 拒绝结果作为 tool 消息回传模型，模型据此继续回答
	reqs := llm.Requests()
	assertEq(t, len(reqs), 2, "provider calls")
	msgs, _ := reqs[1]["messages"].([]any)
	lastMsg, _ := msgs[len(msgs)-1].(map[string]any)
	if lastMsg["role"] != "tool" || !strings.Contains(fmt.Sprint(lastMsg["content"]), "用户拒绝了该操作") {
		t.Fatalf("拒绝结果未回传: %v", lastMsg)
	}
	var donePayload map[string]any
	for _, ev := range parseSSE(t, w.Body.String()) {
		if ev.Event == "done" {
			_ = json.Unmarshal([]byte(ev.Data), &donePayload)
		}
	}
	if donePayload == nil || donePayload["content"] != "好的，已取消该操作。" {
		t.Fatalf("done payload = %v", donePayload)
	}
}

// 模式 writes + 超时：按拒绝处理（回传拒绝文案，回合继续）。
func TestAgentApproval_TimeoutDenies(t *testing.T) {
	withApprovalMode(t, "writes")
	prevTimeout := agentChatApprovalTimeout
	agentChatApprovalTimeout = 60 * time.Millisecond
	t.Cleanup(func() { agentChatApprovalTimeout = prevTimeout })

	matcher := &chatMockMatcher{}
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{}, matcher)
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		if !reqHasToolMessage(req) {
			return "", []ai.ToolCall{{
				Name:      "cancel_order",
				Arguments: `{"symbol":"BTCUSDT","order_id":"x1"}`,
			}}
		}
		return "操作已超时取消。", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	// 不决议，直接等超时
	w := doAgentChat(t, `{"messages":[{"role":"user","content":"撤单"}],"stream":true}`, nil, 7)
	assertEq(t, w.Code, http.StatusOK, "status code")

	reqs := llm.Requests()
	assertEq(t, len(reqs), 2, "provider calls")
	msgs, _ := reqs[1]["messages"].([]any)
	lastMsg, _ := msgs[len(msgs)-1].(map[string]any)
	if !strings.Contains(fmt.Sprint(lastMsg["content"]), "用户拒绝了该操作") {
		t.Fatalf("超时应按拒绝回传: %v", lastMsg)
	}
	if ev := approvalEventFrom(t, w.Body.String()); ev == nil || ev["tool"] != "cancel_order" {
		t.Fatalf("approval_request = %v", ev)
	}
}

// headless（cron / TG / 子代理）绝不暂停：写工具自动批准执行，审计留痕。
func TestAgentApproval_HeadlessAutoApproves(t *testing.T) {
	withApprovalMode(t, "writes")
	matcher := &chatMockMatcher{}
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{}, matcher)
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		if !reqHasToolMessage(req) {
			return "", []ai.ToolCall{{
				Name:      "place_paper_order",
				Arguments: `{"symbol":"BTCUSDT","side":"BUY","order_type":"MARKET","quantity":0.1}`,
			}}
		}
		return "定时买单已提交。", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	reply, err := RunAgentHeadless(7, "定时买 0.1 个 BTC")
	if err != nil {
		t.Fatalf("headless run: %v", err)
	}
	if reply != "定时买单已提交。" {
		t.Fatalf("reply = %q", reply)
	}
	assertEq(t, int(matcher.LastUser()), 7, "headless 自动批准应执行写工具")

	// 审计：自动批准留痕（endpoint=/agent/chat/approval）
	recs, err := store.NewAuditRepo().GetRecent(100)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	found := false
	for _, r := range recs {
		if r.Endpoint == "/agent/chat/approval" && r.Name == "place_paper_order" {
			found = true
			break
		}
	}
	assertTrue(t, found, "headless 自动批准应写审计备注")
}

// 未知/过期 id → 404；空 id → 400。
func TestAgentChatApprove_UnknownID(t *testing.T) {
	w := callApprove(t, "ap_nonexistent", true)
	assertEq(t, w.Code, http.StatusNotFound, "未知 id 应 404")

	r := setupRouter()
	r.POST("/agent/chat/approve", AgentChatApprove)
	req := httptest.NewRequest(http.MethodPost, "/agent/chat/approve", strings.NewReader(`{"approve":true}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	assertEq(t, rw.Code, http.StatusBadRequest, "空 id 应 400")
}
