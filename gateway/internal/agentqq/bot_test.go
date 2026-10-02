package agentqq

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

	"github.com/gorilla/websocket"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// setupQQTestDB 初始化临时 SQLite（含 0053 迁移）。
func setupQQTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-agent-qq")
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(store.CloseDB)
}

// qqSent 一条 REST 回发记录。
type qqSent struct {
	Path  string
	MsgID string
	Text  string
	Auth  string
}

// fakeQQServer 模拟 QQ 开放平台：token 签发 + 消息回发记录 + WSS 网关（每连接
// 一个脚本：hello → 读首帧（identify/resume）→ 心跳收集；事件由测试主动下发）。
type fakeQQServer struct {
	mu       sync.Mutex
	tokenHit int
	sent     []qqSent
	srv      *httptest.Server

	conns    chan *websocket.Conn // 每次新 WSS 连接
	first    chan wsPayload       // 每连接首帧（identify/resume）
	hbFrames chan wsPayload       // 心跳帧
}

func newFakeQQServer(t *testing.T) *fakeQQServer {
	f := &fakeQQServer{
		conns:    make(chan *websocket.Conn, 8),
		first:    make(chan wsPayload, 8),
		hbFrames: make(chan wsPayload, 64),
	}
	up := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/app/getAppAccessToken", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.tokenHit++
		f.mu.Unlock()
		fmt.Fprint(w, `{"access_token":"t-qq","expires_in":"7200"}`)
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		f.conns <- c
		// hello（心跳间隔 50ms，加速测试）
		_ = c.WriteJSON(map[string]any{"op": 10, "d": map[string]any{"heartbeat_interval": 50}})
		go func() {
			first := true
			for {
				var p wsPayload
				if err := c.ReadJSON(&p); err != nil {
					return
				}
				if p.Op == 1 {
					f.hbFrames <- p
					continue
				}
				if first {
					first = false
					f.first <- p
				}
			}
		}()
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/messages") {
			w.WriteHeader(404)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var m struct {
			Content string `json:"content"`
			MsgID   string `json:"msg_id"`
		}
		_ = json.Unmarshal(body, &m)
		f.mu.Lock()
		f.sent = append(f.sent, qqSent{Path: r.URL.Path, MsgID: m.MsgID, Text: m.Content, Auth: r.Header.Get("Authorization")})
		f.mu.Unlock()
		fmt.Fprint(w, `{"id":"reply-1"}`)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeQQServer) allSent() []qqSent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]qqSent{}, f.sent...)
}

func (f *fakeQQServer) tokenCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenHit
}

// wsURL WSS 拨号地址。
func (f *fakeQQServer) wsURL() string {
	return "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/ws"
}

// newTestBot 测试用 bot：env 凭据 + fake 端点。
func newTestBot(t *testing.T, f *fakeQQServer, exec Executor) *Bot {
	t.Helper()
	t.Setenv("QQ_APP_ID", "app_test")
	t.Setenv("QQ_APP_SECRET", "secret_test")
	b := NewBot(NewRepo())
	b.SetAPIBase(f.srv.URL)
	b.SetTokenURL(f.srv.URL + "/app/getAppAccessToken")
	b.SetWSURL(f.wsURL())
	b.SetExecutor(exec)
	t.Cleanup(b.Stop)
	return b
}

// sendEvent 经服务端连接下发一个 dispatch 事件帧。
func sendEvent(t *testing.T, c *websocket.Conn, seq int64, eventType string, d map[string]any) {
	t.Helper()
	if err := c.WriteJSON(map[string]any{"op": 0, "s": seq, "t": eventType, "d": d}); err != nil {
		t.Fatalf("下发事件失败: %v", err)
	}
}

// waitSent 轮询直至出现满足条件的回发（超时失败）。
func waitSent(t *testing.T, f *fakeQQServer, match func(qqSent) bool) qqSent {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range f.allSent() {
			if match(s) {
				return s
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待回发超时, 已回发: %+v", f.allSent())
	return qqSent{}
}

func TestWS_IdentifyAndHeartbeat(t *testing.T) {
	setupQQTestDB(t)
	f := newFakeQQServer(t)
	b := newTestBot(t, f, nil)
	b.Start()

	<-f.conns
	var p wsPayload
	select {
	case p = <-f.first:
	case <-time.After(3 * time.Second):
		t.Fatal("未收到 identify")
	}
	if p.Op != 2 {
		t.Fatalf("首帧应为 identify(op2), got op=%d", p.Op)
	}
	var d struct {
		Token   string `json:"token"`
		Intents int64  `json:"intents"`
	}
	if err := json.Unmarshal(p.D, &d); err != nil {
		t.Fatal(err)
	}
	if d.Token != "QQBot t-qq" {
		t.Errorf("identify token: %q", d.Token)
	}
	if d.Intents != intents {
		t.Errorf("intents = %d, want %d", d.Intents, intents)
	}
	select {
	case hb := <-f.hbFrames:
		if hb.Op != 1 {
			t.Errorf("心跳帧 op=%d", hb.Op)
		}
	case <-time.After(2 * time.Second):
		t.Error("未收到心跳")
	}
}

func TestWS_C2CPairFlow(t *testing.T) {
	setupQQTestDB(t)
	f := newFakeQQServer(t)
	b := newTestBot(t, f, nil)
	repo := NewRepo()
	code, err := repo.CreatePairCode(42)
	if err != nil {
		t.Fatal(err)
	}
	b.Start()
	c := <-f.conns
	<-f.first
	sendEvent(t, c, 1, "READY", map[string]any{"session_id": "sess-1"})
	sendEvent(t, c, 2, "C2C_MESSAGE_CREATE", map[string]any{
		"id": "msg-1", "content": code, "author": map[string]any{"id": "u-open-1"},
	})

	got := waitSent(t, f, func(s qqSent) bool {
		return s.Path == "/v2/users/u-open-1/messages" && strings.Contains(s.Text, "绑定成功")
	})
	if got.MsgID != "msg-1" {
		t.Errorf("被动回复应携带 msg_id=msg-1, got %q", got.MsgID)
	}
	if got.Auth != "QQBot t-qq" {
		t.Errorf("回发鉴权头: %q", got.Auth)
	}
	link, err := repo.GetByOpenID("u-open-1")
	if err != nil || link.UserID != 42 || link.ChatType != "c2c" || link.ChatID != "u-open-1" {
		t.Fatalf("绑定落库错误: %+v err=%v", link, err)
	}
}

func TestWS_GroupAtHeadlessReply(t *testing.T) {
	setupQQTestDB(t)
	f := newFakeQQServer(t)
	var gotReq *RunRequest
	b := newTestBot(t, f, func(ctx context.Context, req *RunRequest) (string, error) {
		gotReq = req
		return "BTC 现价 100000", nil
	})
	repo := NewRepo()
	code, _ := repo.CreatePairCode(7)
	if _, err := repo.ConsumePairCode(code, "member-1", "group", "g-open-1"); err != nil {
		t.Fatal(err)
	}
	b.Start()
	c := <-f.conns
	<-f.first
	sendEvent(t, c, 1, "READY", map[string]any{"session_id": "sess-1"})
	sendEvent(t, c, 2, "GROUP_AT_MESSAGE_CREATE", map[string]any{
		"id": "msg-9", "content": " 看看 BTC ", "group_openid": "g-open-1",
		"author": map[string]any{"member_openid": "member-1"},
	})

	got := waitSent(t, f, func(s qqSent) bool {
		return s.Path == "/v2/groups/g-open-1/messages" && s.Text == "BTC 现价 100000"
	})
	if got.MsgID != "msg-9" {
		t.Errorf("群回复应携带 msg_id=msg-9, got %q", got.MsgID)
	}
	if gotReq == nil || gotReq.UserID != 7 || gotReq.Prompt != "看看 BTC" {
		t.Fatalf("执行请求错误: %+v", gotReq)
	}
}

func TestWS_ResumeOnServerClose(t *testing.T) {
	setupQQTestDB(t)
	f := newFakeQQServer(t)
	b := newTestBot(t, f, nil)
	b.Start()

	c1 := <-f.conns
	<-f.first // identify
	sendEvent(t, c1, 1, "READY", map[string]any{"session_id": "sess-xyz"})
	// 等 bot 记录 session 后由服务端主动断开
	time.Sleep(100 * time.Millisecond)
	_ = c1.Close()

	select {
	case <-f.conns: // 第二次连接（1s 退避后）
	case <-time.After(5 * time.Second):
		t.Fatal("断线后未重连")
	}
	var p wsPayload
	select {
	case p = <-f.first:
	case <-time.After(3 * time.Second):
		t.Fatal("重连后未收到 resume")
	}
	if p.Op != 6 {
		t.Fatalf("重连首帧应为 resume(op6), got op=%d", p.Op)
	}
	var d struct {
		Token     string `json:"token"`
		SessionID string `json:"session_id"`
		Seq       int64  `json:"seq"`
	}
	if err := json.Unmarshal(p.D, &d); err != nil {
		t.Fatal(err)
	}
	if d.SessionID != "sess-xyz" || d.Token != "QQBot t-qq" {
		t.Errorf("resume 帧错误: %+v", d)
	}
	if d.Seq < 1 {
		t.Errorf("resume seq 应 >= 1, got %d", d.Seq)
	}
}

func TestUnconfigured_Inert(t *testing.T) {
	setupQQTestDB(t)
	t.Setenv("QQ_APP_ID", "")
	t.Setenv("QQ_APP_SECRET", "")
	f := newFakeQQServer(t)
	b := NewBot(NewRepo())
	b.SetAPIBase(f.srv.URL)
	b.SetTokenURL(f.srv.URL + "/app/getAppAccessToken")
	b.SetWSURL(f.wsURL())

	if Configured() {
		t.Error("未配置时 Configured 应为 false")
	}
	b.Start() // 惰性：不应拨号
	select {
	case <-f.conns:
		t.Error("未配置不应建立 WSS 连接")
	case <-time.After(300 * time.Millisecond):
	}
	if b.SendToUser(1, "x") {
		t.Error("未配置 SendToUser 应返回 false")
	}
}

func TestSendToUser_UsesStoredChatContext(t *testing.T) {
	setupQQTestDB(t)
	f := newFakeQQServer(t)
	b := newTestBot(t, f, nil)
	repo := NewRepo()
	code, _ := repo.CreatePairCode(11)
	if _, err := repo.ConsumePairCode(code, "member-cron", "group", "g-cron"); err != nil {
		t.Fatal(err)
	}
	if !b.SendToUser(11, "cron 结果") {
		t.Error("已绑定用户 SendToUser 应成功")
	}
	waitSent(t, f, func(s qqSent) bool {
		return s.Path == "/v2/groups/g-cron/messages" && s.Text == "cron 结果"
	})
	if b.SendToUser(999, "x") {
		t.Error("未绑定用户 SendToUser 应返回 false（回落）")
	}
}

func TestTokenCached(t *testing.T) {
	setupQQTestDB(t)
	f := newFakeQQServer(t)
	b := newTestBot(t, f, nil)
	repo := NewRepo()
	code, _ := repo.CreatePairCode(12)
	if _, err := repo.ConsumePairCode(code, "u-cache", "c2c", "u-cache"); err != nil {
		t.Fatal(err)
	}
	b.SendToUser(12, "a")
	b.SendToUser(12, "b")
	if n := f.tokenCalls(); n != 1 {
		t.Errorf("token 应缓存，调用 %d 次", n)
	}
}
