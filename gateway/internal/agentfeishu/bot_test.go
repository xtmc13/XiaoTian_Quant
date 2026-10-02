package agentfeishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// setupFSTestDB 初始化临时 SQLite（含 0051 迁移）。
func setupFSTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-agent-fs")
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(store.CloseDB)
}

// fakeFeishuServer 模拟飞书开放平台：tenant_access_token 签发 + im/v1/messages 回发记录。
type fakeFeishuServer struct {
	mu       sync.Mutex
	sent     []string // 已回发文本（分条后）
	tokenHit int      // token 接口调用次数
	srv      *httptest.Server
}

func newFakeFeishuServer(t *testing.T) *fakeFeishuServer {
	f := &fakeFeishuServer{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "tenant_access_token"):
			f.mu.Lock()
			f.tokenHit++
			f.mu.Unlock()
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"t-test","expire":7200}`)
		case strings.Contains(r.URL.Path, "/open-apis/im/v1/messages"):
			if r.Header.Get("Authorization") != "Bearer t-test" {
				w.WriteHeader(401)
				return
			}
			body, _ := io.ReadAll(r.Body)
			var m struct {
				Content string `json:"content"`
			}
			_ = json.Unmarshal(body, &m)
			var c struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal([]byte(m.Content), &c)
			f.mu.Lock()
			f.sent = append(f.sent, c.Text)
			f.mu.Unlock()
			fmt.Fprint(w, `{"code":0,"msg":"ok"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeFeishuServer) lastText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return ""
	}
	return f.sent[len(f.sent)-1]
}

func (f *fakeFeishuServer) allSent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.sent...)
}

func (f *fakeFeishuServer) tokenCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenHit
}

// newTestBot 测试用 bot：env 凭据 + fake API。
func newTestBot(t *testing.T, fs *fakeFeishuServer, exec Executor) *Bot {
	t.Helper()
	t.Setenv("FEISHU_APP_ID", "cli_test")
	t.Setenv("FEISHU_APP_SECRET", "secret_test")
	b := NewBot(NewRepo())
	b.SetAPIBase(fs.srv.URL)
	b.SetExecutor(exec)
	return b
}

// makeEvent 构造 im.message.receive_v1 事件。
func makeEvent(eventID, openID, chatID, text string) *webhookBody {
	content, _ := json.Marshal(map[string]string{"text": text})
	var in webhookBody
	in.Schema = "2.0"
	in.Header.EventID = eventID
	in.Header.EventType = "im.message.receive_v1"
	in.Event.Sender.SenderID.OpenID = openID
	in.Event.Message.MessageID = "om_1"
	in.Event.Message.ChatID = chatID
	in.Event.Message.ChatType = "p2p"
	in.Event.Message.MessageType = "text"
	in.Event.Message.Content = string(content)
	return &in
}

func postJSON(t *testing.T, b *Bot, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/agent/feishu/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	b.HandleWebhook(w, req)
	return w
}

func TestWebhook_ChallengeHandshake(t *testing.T) {
	setupFSTestDB(t)
	fs := newFakeFeishuServer(t)
	b := newTestBot(t, fs, nil)

	w := postJSON(t, b, `{"type":"url_verification","challenge":"abc123"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var out map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out["challenge"] != "abc123" {
		t.Errorf("challenge 握手响应错误: %s", w.Body.String())
	}
}

func TestWebhook_UnconfiguredInert(t *testing.T) {
	setupFSTestDB(t)
	t.Setenv("FEISHU_APP_ID", "")
	t.Setenv("FEISHU_APP_SECRET", "")
	fs := newFakeFeishuServer(t)
	b := NewBot(NewRepo())
	b.SetAPIBase(fs.srv.URL)

	w := postJSON(t, b, `{"type":"url_verification","challenge":"x"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("未配置应 503, got %d", w.Code)
	}
	if Configured() {
		t.Error("未配置时 Configured 应为 false")
	}
}

func TestWebhook_EventDedupe(t *testing.T) {
	setupFSTestDB(t)
	fs := newFakeFeishuServer(t)
	done := make(chan struct{}, 4)
	b := newTestBot(t, fs, func(ctx context.Context, req *RunRequest) (string, error) {
		done <- struct{}{}
		return "ok", nil
	})
	// 先绑定，保证消息进入执行路径
	repo := NewRepo()
	code, _ := repo.CreatePairCode(1)
	if _, err := repo.ConsumePairCode(code, "ou_1", "oc_1"); err != nil {
		t.Fatal(err)
	}

	ev, _ := json.Marshal(makeEvent("ev_dup", "ou_1", "oc_1", "你好"))
	// 同一 event_id 连发两次：第二次去重不处理
	w1 := postJSON(t, b, string(ev))
	w2 := postJSON(t, b, string(ev))
	if w1.Code != http.StatusOK || w2.Code != http.StatusOK {
		t.Fatalf("status = %d/%d", w1.Code, w2.Code)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("首次事件应异步执行")
	}
	select {
	case <-done:
		t.Error("重复 event_id 不应再次执行")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestBot_PairFlow(t *testing.T) {
	setupFSTestDB(t)
	fs := newFakeFeishuServer(t)
	b := newTestBot(t, fs, nil)
	repo := NewRepo()
	code, err := repo.CreatePairCode(42)
	if err != nil {
		t.Fatal(err)
	}

	// 错误码 → 提示
	b.handleMessage(&makeEvent("e1", "ou_a", "oc_a", "000000").Event)
	if text := fs.lastText(); !strings.Contains(text, "无效") {
		t.Errorf("错误码回复: %q", text)
	}
	// 正确码 → 绑定成功
	b.handleMessage(&makeEvent("e2", "ou_a", "oc_a", code).Event)
	if text := fs.lastText(); !strings.Contains(text, "绑定成功") {
		t.Errorf("配对回复: %q", text)
	}
	link, err := repo.GetByOpenID("ou_a")
	if err != nil || link.UserID != 42 || link.ChatID != "oc_a" {
		t.Fatalf("绑定落库错误: %+v err=%v", link, err)
	}
}

func TestBot_UnboundHint(t *testing.T) {
	setupFSTestDB(t)
	fs := newFakeFeishuServer(t)
	b := newTestBot(t, fs, nil)

	b.handleMessage(&makeEvent("e1", "ou_stranger", "oc_s", "帮我看持仓").Event)
	if text := fs.lastText(); !strings.Contains(text, "配对码") {
		t.Errorf("未绑定指引: %q", text)
	}
}

func TestBot_MessageHeadlessReply(t *testing.T) {
	setupFSTestDB(t)
	fs := newFakeFeishuServer(t)
	var gotReq *RunRequest
	b := newTestBot(t, fs, func(ctx context.Context, req *RunRequest) (string, error) {
		gotReq = req
		return "BTC 现价 100000", nil
	})
	repo := NewRepo()
	code, _ := repo.CreatePairCode(7)
	if _, err := repo.ConsumePairCode(code, "ou_m", "oc_m"); err != nil {
		t.Fatal(err)
	}
	// 模型覆盖持久化后应透传到执行器
	if err := repo.SetModel("ou_m", "deepseek:deepseek-chat"); err != nil {
		t.Fatal(err)
	}

	b.handleMessage(&makeEvent("e1", "ou_m", "oc_m", "看看 BTC").Event)
	if gotReq == nil || gotReq.UserID != 7 || gotReq.Prompt != "看看 BTC" || gotReq.Model != "deepseek:deepseek-chat" {
		t.Fatalf("执行请求错误: %+v", gotReq)
	}
	if text := fs.lastText(); text != "BTC 现价 100000" {
		t.Errorf("回发文本: %q", text)
	}
	// token 缓存：再次发送不应重复取 token
	b.handleMessage(&makeEvent("e2", "ou_m", "oc_m", "再看一眼").Event)
	if fs.tokenCalls() != 1 {
		t.Errorf("tenant_token 应缓存，调用 %d 次", fs.tokenCalls())
	}
}

func TestBot_LongReplyChunked(t *testing.T) {
	setupFSTestDB(t)
	fs := newFakeFeishuServer(t)
	long := strings.Repeat("涨", maxChunkRunes+100)
	b := newTestBot(t, fs, func(ctx context.Context, req *RunRequest) (string, error) { return long, nil })
	repo := NewRepo()
	code, _ := repo.CreatePairCode(8)
	if _, err := repo.ConsumePairCode(code, "ou_l", "oc_l"); err != nil {
		t.Fatal(err)
	}
	b.handleMessage(&makeEvent("e1", "ou_l", "oc_l", "长文").Event)
	if n := len(fs.allSent()); n != 2 {
		t.Errorf("超长回复应分 2 条, got %d", n)
	}
}

func TestBot_Commands(t *testing.T) {
	setupFSTestDB(t)
	fs := newFakeFeishuServer(t)
	b := newTestBot(t, fs, func(ctx context.Context, req *RunRequest) (string, error) { return "ok", nil })
	repo := NewRepo()
	code, _ := repo.CreatePairCode(9)
	if _, err := repo.ConsumePairCode(code, "ou_c", "oc_c"); err != nil {
		t.Fatal(err)
	}

	b.handleMessage(&makeEvent("c1", "ou_c", "oc_c", "/model").Event)
	if text := fs.lastText(); !strings.Contains(text, "默认") {
		t.Errorf("/model 查看: %q", text)
	}
	b.handleMessage(&makeEvent("c2", "ou_c", "oc_c", "/model openai:gpt-4o").Event)
	link, _ := repo.GetByOpenID("ou_c")
	if link.Model != "openai:gpt-4o" {
		t.Errorf("/model 持久化: %q", link.Model)
	}
	b.handleMessage(&makeEvent("c3", "ou_c", "oc_c", "/new").Event)
	if text := fs.lastText(); !strings.Contains(text, "新会话") {
		t.Errorf("/new: %q", text)
	}
	b.handleMessage(&makeEvent("c4", "ou_c", "oc_c", "/stop").Event)
	if text := fs.lastText(); !strings.Contains(text, "没有正在执行") {
		t.Errorf("/stop 空闲: %q", text)
	}
	b.handleMessage(&makeEvent("c5", "ou_c", "oc_c", "/status").Event)
	if text := fs.lastText(); !strings.Contains(text, "user_id=9") || !strings.Contains(text, "gpt-4o") {
		t.Errorf("/status: %q", text)
	}
	b.handleMessage(&makeEvent("c6", "ou_c", "oc_c", "/unknown").Event)
	if text := fs.lastText(); !strings.Contains(text, "未知命令") {
		t.Errorf("未知命令: %q", text)
	}
}

func TestBot_StopCancelsRun(t *testing.T) {
	setupFSTestDB(t)
	fs := newFakeFeishuServer(t)
	started := make(chan struct{})
	b := newTestBot(t, fs, func(ctx context.Context, req *RunRequest) (string, error) {
		close(started)
		<-ctx.Done() // 阻塞直到 /stop
		return "", ctx.Err()
	})
	repo := NewRepo()
	code, _ := repo.CreatePairCode(10)
	if _, err := repo.ConsumePairCode(code, "ou_s", "oc_s"); err != nil {
		t.Fatal(err)
	}
	go b.handleMessage(&makeEvent("s1", "ou_s", "oc_s", "跑个长任务").Event)
	<-started
	b.handleMessage(&makeEvent("s2", "ou_s", "oc_s", "/stop").Event)
	// 中断回复与 /stop 应答并发回发，顺序不定：等任意一条含"已中断"
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, text := range fs.allSent() {
			if strings.Contains(text, "已中断") {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("/stop 应中断在途执行, 回发: %v", fs.allSent())
}

func TestBot_SendToUser(t *testing.T) {
	setupFSTestDB(t)
	fs := newFakeFeishuServer(t)
	b := newTestBot(t, fs, nil)
	repo := NewRepo()
	code, _ := repo.CreatePairCode(11)
	if _, err := repo.ConsumePairCode(code, "ou_u", "oc_u"); err != nil {
		t.Fatal(err)
	}
	if !b.SendToUser(11, "cron 结果") {
		t.Error("已绑定用户 SendToUser 应成功")
	}
	if fs.lastText() != "cron 结果" {
		t.Errorf("cron 直发: %q", fs.lastText())
	}
	if b.SendToUser(999, "x") {
		t.Error("未绑定用户 SendToUser 应返回 false（回落）")
	}
}
