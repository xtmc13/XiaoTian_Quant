package agentdingtalk

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// setupDTTestDB 初始化临时 SQLite（含 0052 迁移）。
func setupDTTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-agent-dt")
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(store.CloseDB)
}

// fakeSessionWebhook 模拟钉钉 sessionWebhook 回发端点，记录收到的文本。
type fakeSessionWebhook struct {
	mu   sync.Mutex
	sent []string
	srv  *httptest.Server
}

func newFakeSessionWebhook(t *testing.T) *fakeSessionWebhook {
	f := &fakeSessionWebhook{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var m struct {
			Text struct {
				Content string `json:"content"`
			} `json:"text"`
		}
		_ = json.Unmarshal(body, &m)
		f.mu.Lock()
		f.sent = append(f.sent, m.Text.Content)
		f.mu.Unlock()
		fmt.Fprint(w, `{"errcode":0}`)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSessionWebhook) lastText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return ""
	}
	return f.sent[len(f.sent)-1]
}

func (f *fakeSessionWebhook) allSent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.sent...)
}

// newTestBot 测试用 bot：env 凭据 + 可选执行器。
func newTestBot(t *testing.T, exec Executor) *Bot {
	t.Helper()
	t.Setenv("DINGTALK_CLIENT_ID", "ding_test")
	t.Setenv("DINGTALK_CLIENT_SECRET", "ding_secret")
	b := NewBot(NewRepo())
	b.SetExecutor(exec)
	return b
}

// signFor 按钉钉 outgoing 规范计算签名（urlEncode(base64(hmac))）。
func signFor(timestamp, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "\n" + secret))
	return url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}

// incomingJSON 构造 outgoing 回调体。
func incomingJSON(webhook, staffID, nick, convID, text string) string {
	body, _ := json.Marshal(map[string]any{
		"msgtype":        "text",
		"conversationId": convID,
		"msgId":          "msg_1",
		"senderNick":     nick,
		"senderStaffId":  staffID,
		"sessionWebhook": webhook,
		"text":           map[string]string{"content": text},
	})
	return string(body)
}

func postWebhook(t *testing.T, b *Bot, body, timestamp, sign string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/agent/dingtalk/webhook", strings.NewReader(body))
	if timestamp != "" {
		req.Header.Set("timestamp", timestamp)
	}
	if sign != "" {
		req.Header.Set("sign", sign)
	}
	w := httptest.NewRecorder()
	b.HandleWebhook(w, req)
	return w
}

func TestVerifySign(t *testing.T) {
	ts := "1700000000000"
	secret := "sec123"
	// 原始 base64 与 url 编码形态都应通过
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "\n" + secret))
	raw := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if !verifySign(ts, raw, secret) {
		t.Error("原始 base64 签名应通过")
	}
	if !verifySign(ts, url.QueryEscape(raw), secret) {
		t.Error("url 编码签名应通过")
	}
	if verifySign(ts, "bogus", secret) {
		t.Error("错误签名不应通过")
	}
	if verifySign("", raw, secret) {
		t.Error("缺 timestamp 不应通过")
	}
	// secret 未配置 → 容忍缺签
	if !verifySign("", "", "") {
		t.Error("secret 未配置应容忍缺签")
	}
}

func TestWebhook_SignEnforced(t *testing.T) {
	setupDTTestDB(t)
	b := newTestBot(t, nil)
	body := incomingJSON("http://x", "staff_1", "张三", "cid_1", "hi")

	// 有效签名 → 200
	ts := fmt.Sprintf("%d", time.Now().UnixMilli())
	w := postWebhook(t, b, body, ts, signFor(ts, "ding_secret"))
	if w.Code != http.StatusOK {
		t.Errorf("有效签名 status = %d, want 200", w.Code)
	}
	// 无效签名 → 401
	w = postWebhook(t, b, body, ts, "bad-sign")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("无效签名 status = %d, want 401", w.Code)
	}
	// 缺签名 → 401（secret 已配置）
	w = postWebhook(t, b, body, ts, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("缺签名 status = %d, want 401", w.Code)
	}
}

func TestWebhook_UnconfiguredInert(t *testing.T) {
	setupDTTestDB(t)
	t.Setenv("DINGTALK_CLIENT_ID", "")
	t.Setenv("DINGTALK_CLIENT_SECRET", "")
	b := NewBot(NewRepo())
	w := postWebhook(t, b, `{"msgtype":"text"}`, "", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("未配置应 503, got %d", w.Code)
	}
	if Configured() {
		t.Error("未配置时 Configured 应为 false")
	}
}

func TestBot_PairFlowAndReply(t *testing.T) {
	setupDTTestDB(t)
	hook := newFakeSessionWebhook(t)
	b := newTestBot(t, nil)
	repo := NewRepo()
	code, err := repo.CreatePairCode(42)
	if err != nil {
		t.Fatal(err)
	}

	// 未绑定 → 指引
	b.handleMessage(mustIncoming(t, incomingJSON(hook.srv.URL, "staff_a", "李四", "cid_a", "帮我看持仓")))
	if text := hook.lastText(); !strings.Contains(text, "配对码") {
		t.Errorf("未绑定指引: %q", text)
	}
	// 错误码
	b.handleMessage(mustIncoming(t, incomingJSON(hook.srv.URL, "staff_a", "李四", "cid_a", "000000")))
	if text := hook.lastText(); !strings.Contains(text, "无效") {
		t.Errorf("错误码回复: %q", text)
	}
	// 正确码 → 绑定成功（经 sessionWebhook 回发）
	b.handleMessage(mustIncoming(t, incomingJSON(hook.srv.URL, "staff_a", "李四", "cid_a", code)))
	if text := hook.lastText(); !strings.Contains(text, "绑定成功") || !strings.Contains(text, "李四") {
		t.Errorf("配对回复: %q", text)
	}
	link, err := repo.GetByStaffID("staff_a")
	if err != nil || link.UserID != 42 {
		t.Fatalf("绑定落库错误: %+v err=%v", link, err)
	}
}

func TestBot_MessageHeadlessReply(t *testing.T) {
	setupDTTestDB(t)
	hook := newFakeSessionWebhook(t)
	var gotReq *RunRequest
	b := newTestBot(t, func(ctx context.Context, req *RunRequest) (string, error) {
		gotReq = req
		return "ETH 现价 5000", nil
	})
	repo := NewRepo()
	code, _ := repo.CreatePairCode(7)
	if _, err := repo.ConsumePairCode(code, "staff_m"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetModel("staff_m", "deepseek"); err != nil {
		t.Fatal(err)
	}

	b.handleMessage(mustIncoming(t, incomingJSON(hook.srv.URL, "staff_m", "王五", "cid_m", "看看 ETH")))
	if gotReq == nil || gotReq.UserID != 7 || gotReq.Prompt != "看看 ETH" || gotReq.Model != "deepseek" {
		t.Fatalf("执行请求错误: %+v", gotReq)
	}
	if text := hook.lastText(); text != "ETH 现价 5000" {
		t.Errorf("回发文本: %q", text)
	}
}

func TestBot_LongReplyChunked(t *testing.T) {
	setupDTTestDB(t)
	hook := newFakeSessionWebhook(t)
	long := strings.Repeat("跌", maxChunkRunes+50)
	b := newTestBot(t, func(ctx context.Context, req *RunRequest) (string, error) { return long, nil })
	repo := NewRepo()
	code, _ := repo.CreatePairCode(8)
	if _, err := repo.ConsumePairCode(code, "staff_l"); err != nil {
		t.Fatal(err)
	}
	b.handleMessage(mustIncoming(t, incomingJSON(hook.srv.URL, "staff_l", "赵六", "cid_l", "长文")))
	if n := len(hook.allSent()); n != 2 {
		t.Errorf("超长回复应分 2 条, got %d", n)
	}
}

func TestBot_Commands(t *testing.T) {
	setupDTTestDB(t)
	hook := newFakeSessionWebhook(t)
	b := newTestBot(t, func(ctx context.Context, req *RunRequest) (string, error) { return "ok", nil })
	repo := NewRepo()
	code, _ := repo.CreatePairCode(9)
	if _, err := repo.ConsumePairCode(code, "staff_c"); err != nil {
		t.Fatal(err)
	}

	send := func(text string) {
		b.handleMessage(mustIncoming(t, incomingJSON(hook.srv.URL, "staff_c", "孙七", "cid_c", text)))
	}
	send("/model openai:gpt-4o")
	link, _ := repo.GetByStaffID("staff_c")
	if link.Model != "openai:gpt-4o" {
		t.Errorf("/model 持久化: %q", link.Model)
	}
	send("/new")
	if text := hook.lastText(); !strings.Contains(text, "新会话") {
		t.Errorf("/new: %q", text)
	}
	send("/stop")
	if text := hook.lastText(); !strings.Contains(text, "没有正在执行") {
		t.Errorf("/stop 空闲: %q", text)
	}
	send("/status")
	if text := hook.lastText(); !strings.Contains(text, "user_id=9") || !strings.Contains(text, "gpt-4o") {
		t.Errorf("/status: %q", text)
	}
	send("/help")
	if text := hook.lastText(); !strings.Contains(text, "/model") {
		t.Errorf("/help: %q", text)
	}
}

func TestBot_SendToUserViaLastWebhook(t *testing.T) {
	setupDTTestDB(t)
	hook := newFakeSessionWebhook(t)
	b := newTestBot(t, nil)
	repo := NewRepo()
	code, _ := repo.CreatePairCode(11)
	if _, err := repo.ConsumePairCode(code, "staff_u"); err != nil {
		t.Fatal(err)
	}
	// 尚无消息往来 → 无 sessionWebhook → 回落
	if b.SendToUser(11, "cron 结果") {
		t.Error("无 sessionWebhook 时 SendToUser 应返回 false")
	}
	// 一次入站后记住回发通道 → 可直发
	b.handleMessage(mustIncoming(t, incomingJSON(hook.srv.URL, "staff_u", "周八", "cid_u", "/status")))
	if !b.SendToUser(11, "cron 结果") {
		t.Error("有 sessionWebhook 后 SendToUser 应成功")
	}
	if hook.lastText() != "cron 结果" {
		t.Errorf("cron 直发: %q", hook.lastText())
	}
	if b.SendToUser(999, "x") {
		t.Error("未绑定用户 SendToUser 应返回 false（回落）")
	}
}

// mustIncoming 解析回调体（测试辅助）。
func mustIncoming(t *testing.T, body string) *incoming {
	t.Helper()
	var in incoming
	if err := json.Unmarshal([]byte(body), &in); err != nil {
		t.Fatal(err)
	}
	return &in
}
