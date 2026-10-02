package agenttelegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTelegramServer 模拟 Telegram Bot API，记录 sendMessage/editMessageText。
type fakeTelegramServer struct {
	mu      sync.Mutex
	sent    []sentMsg
	edits   []editMsg
	nextID  int
	updates string // 下一个 getUpdates 返回的 result JSON
	srv     *httptest.Server
}

type sentMsg struct {
	ChatID int64  `json:"chat_id"`
	Text   string `json:"text"`
}

type editMsg struct {
	ChatID    int64  `json:"chat_id"`
	MessageID int    `json:"message_id"`
	Text      string `json:"text"`
}

func newFakeTelegramServer(t *testing.T, updates string) *fakeTelegramServer {
	f := &fakeTelegramServer{updates: updates}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "getUpdates"):
			fmt.Fprintf(w, `{"ok":true,"result":%s}`, f.updates)
		case strings.Contains(r.URL.Path, "sendMessage"):
			body, _ := io.ReadAll(r.Body)
			var m sentMsg
			_ = json.Unmarshal(body, &m)
			f.mu.Lock()
			f.nextID++
			id := f.nextID
			f.sent = append(f.sent, m)
			f.mu.Unlock()
			fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d}}`, id)
		case strings.Contains(r.URL.Path, "editMessageText"):
			body, _ := io.ReadAll(r.Body)
			var m editMsg
			_ = json.Unmarshal(body, &m)
			f.mu.Lock()
			f.edits = append(f.edits, m)
			f.mu.Unlock()
			fmt.Fprint(w, `{"ok":true}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTelegramServer) lastText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return ""
	}
	return f.sent[len(f.sent)-1].Text
}

func (f *fakeTelegramServer) allSent() []sentMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentMsg{}, f.sent...)
}

func (f *fakeTelegramServer) allEdits() []editMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]editMsg{}, f.edits...)
}

// lastEditText 最后一次编辑的文本（无编辑时回退到最后一条 sendMessage）。
func (f *fakeTelegramServer) lastEditText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.edits) == 0 {
		return ""
	}
	return f.edits[len(f.edits)-1].Text
}

func newTestBot(t *testing.T, fs *fakeTelegramServer, exec Executor) *Bot {
	t.Helper()
	b := NewBot("TEST_TOKEN", NewRepo(), exec)
	b.SetAPIBase(fs.srv.URL)
	return b
}

func TestBot_StartReply(t *testing.T) {
	setupTGTestDB(t)
	fs := newFakeTelegramServer(t, `[]`)
	b := newTestBot(t, fs, nil)

	b.handleMessage(tgMessage{Chat: tgChat{ID: 1}, Text: "/start"})
	if text := fs.lastText(); !strings.Contains(text, "配对码") {
		t.Errorf("start reply: %q", text)
	}
}

func TestBot_PairingFlow(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	code, _ := repo.CreatePairCode(42)

	fs := newFakeTelegramServer(t, `[]`)
	b := newTestBot(t, fs, nil)

	// 错误码
	b.handleMessage(tgMessage{Chat: tgChat{ID: 9}, Text: "000000"})
	if !strings.Contains(fs.lastText(), "无效") {
		t.Errorf("invalid code reply: %q", fs.lastText())
	}

	// 正确码绑定
	b.handleMessage(tgMessage{Chat: tgChat{ID: 9}, From: tgFrom{Username: "trader"}, Text: code})
	if !strings.Contains(fs.lastText(), "绑定成功") {
		t.Errorf("pair reply: %q", fs.lastText())
	}
}

func TestBot_InboundRunsExecutor(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	code, _ := repo.CreatePairCode(7)
	repo.ConsumePairCode(code, 55, "trader")

	var gotUser int64
	var gotPrompt string
	fs := newFakeTelegramServer(t, `[]`)
	b := newTestBot(t, fs, func(userID int64, prompt string) (string, error) {
		gotUser, gotPrompt = userID, prompt
		return "持仓一切正常", nil
	})

	b.handleMessage(tgMessage{Chat: tgChat{ID: 55}, Text: "看看我的持仓"})
	if gotUser != 7 || gotPrompt != "看看我的持仓" {
		t.Errorf("exec args: %d %q", gotUser, gotPrompt)
	}
	if fs.lastText() != "持仓一切正常" {
		t.Errorf("reply: %q", fs.lastText())
	}
}

func TestBot_UnlinkedHint(t *testing.T) {
	setupTGTestDB(t)
	fs := newFakeTelegramServer(t, `[]`)
	b := newTestBot(t, fs, func(int64, string) (string, error) { return "x", nil })

	b.handleMessage(tgMessage{Chat: tgChat{ID: 77}, Text: "任意消息"})
	if !strings.Contains(fs.lastText(), "还没有绑定") {
		t.Errorf("unlinked hint: %q", fs.lastText())
	}
}

func TestBot_GetUpdatesLoop(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	code, _ := repo.CreatePairCode(3)

	// 轮询返回一条携带配对码的消息
	updates := fmt.Sprintf(`[{"update_id":10,"message":{"chat":{"id":66},"from":{"username":"loop"},"text":%q}}]`, code)
	fs := newFakeTelegramServer(t, updates)

	var execCalled bool
	b := newTestBot(t, fs, func(int64, string) (string, error) {
		execCalled = true
		return "", nil
	})

	got, err := b.getUpdates()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].UpdateID != 10 {
		t.Fatalf("updates: %+v", got)
	}
	_ = execCalled

	// 消费消息 → 绑定成功（通过 handleMessage 模拟 loop 分支）
	b.handleMessage(got[0].Message)
	if !strings.Contains(fs.lastText(), "绑定成功") {
		t.Errorf("loop pair reply: %q", fs.lastText())
	}
	if _, err := repo.GetByChatID(66); err != nil {
		t.Error("loop 消息未建立绑定")
	}
}

// ── 斜杠命令 ──

func TestBot_HelpAndUnknownCommand(t *testing.T) {
	setupTGTestDB(t)
	fs := newFakeTelegramServer(t, `[]`)
	b := newTestBot(t, fs, nil)

	b.handleMessage(tgMessage{Chat: tgChat{ID: 1}, Text: "/help"})
	if text := fs.lastText(); !strings.Contains(text, "/handoff") || !strings.Contains(text, "/model") {
		t.Errorf("help reply: %q", text)
	}

	// 未知命令需先绑定
	repo := NewRepo()
	linkChat(t, repo, 1, 1, "trader")
	b.handleMessage(tgMessage{Chat: tgChat{ID: 1}, Text: "/foobar"})
	if text := fs.lastText(); !strings.Contains(text, "未知命令") || !strings.Contains(text, "/help") {
		t.Errorf("unknown command reply: %q", text)
	}
}

func TestBot_ModelCommand(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	linkChat(t, repo, 1, 1, "trader")
	fs := newFakeTelegramServer(t, `[]`)
	b := newTestBot(t, fs, nil)

	// 默认
	b.handleMessage(tgMessage{Chat: tgChat{ID: 1}, Text: "/model"})
	if text := fs.lastText(); !strings.Contains(text, "默认") {
		t.Errorf("model show default: %q", text)
	}
	// 设置覆盖
	b.handleMessage(tgMessage{Chat: tgChat{ID: 1}, Text: "/model deepseek:deepseek-chat"})
	if got := b.modelFor(1); got != "deepseek:deepseek-chat" {
		t.Errorf("modelFor = %q", got)
	}
	if text := fs.lastText(); !strings.Contains(text, "deepseek:deepseek-chat") {
		t.Errorf("model set reply: %q", text)
	}
	// 恢复默认
	b.handleMessage(tgMessage{Chat: tgChat{ID: 1}, Text: "/model default"})
	if got := b.modelFor(1); got != "" {
		t.Errorf("modelFor after default = %q", got)
	}
}

func TestBot_StatusCommand(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	linkChat(t, repo, 7, 55, "trader")
	fs := newFakeTelegramServer(t, `[]`)
	b := newTestBot(t, fs, nil)
	b.SetHealthProvider(func() string { return "网关运行中 · AI: mock/mock-model" })

	b.handleMessage(tgMessage{Chat: tgChat{ID: 55}, Text: "/status"})
	text := fs.lastText()
	for _, want := range []string{"@trader", "user_id=7", "未绑定", "网关运行中 · AI: mock/mock-model"} {
		if !strings.Contains(text, want) {
			t.Errorf("status 缺 %q: %q", want, text)
		}
	}
}

func TestBot_StopWithoutRun(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	linkChat(t, repo, 1, 1, "trader")
	fs := newFakeTelegramServer(t, `[]`)
	b := newTestBot(t, fs, nil)

	b.handleMessage(tgMessage{Chat: tgChat{ID: 1}, Text: "/stop"})
	if text := fs.lastText(); !strings.Contains(text, "没有正在执行") {
		t.Errorf("stop idle reply: %q", text)
	}
}

func TestBot_NewClearsConversation(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	linkChat(t, repo, 1, 1, "trader")
	if err := repo.SetConversationID(1, "conv-1"); err != nil {
		t.Fatal(err)
	}
	fs := newFakeTelegramServer(t, `[]`)
	b := newTestBot(t, fs, nil)

	b.handleMessage(tgMessage{Chat: tgChat{ID: 1}, Text: "/new"})
	if text := fs.lastText(); !strings.Contains(text, "新会话") {
		t.Errorf("new reply: %q", text)
	}
	link, _ := repo.GetByChatID(1)
	if link.ConversationID != "" {
		t.Errorf("/new 后会话绑定应为空: %q", link.ConversationID)
	}
}

func TestBot_HandoffCommand(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	linkChat(t, repo, 1, 1, "trader")
	fs := newFakeTelegramServer(t, `[]`)
	b := newTestBot(t, fs, nil)

	// 用法错误
	b.handleMessage(tgMessage{Chat: tgChat{ID: 1}, Text: "/handoff abc"})
	if text := fs.lastText(); !strings.Contains(text, "用法") {
		t.Errorf("handoff usage reply: %q", text)
	}
	// 错码
	b.handleMessage(tgMessage{Chat: tgChat{ID: 1}, Text: "/handoff 000000"})
	if text := fs.lastText(); !strings.Contains(text, "无效") {
		t.Errorf("handoff invalid reply: %q", text)
	}
	// 正确码 → 绑定会话
	code, err := repo.CreateHandoffCode(1, "conv-7")
	if err != nil {
		t.Fatal(err)
	}
	b.handleMessage(tgMessage{Chat: tgChat{ID: 1}, Text: "/handoff " + code})
	if text := fs.lastText(); !strings.Contains(text, "已接管会话") {
		t.Errorf("handoff ok reply: %q", text)
	}
	link, _ := repo.GetByChatID(1)
	if link.ConversationID != "conv-7" {
		t.Errorf("handoff 后绑定: %q", link.ConversationID)
	}
}

// ── 流式回复 / 单飞 / 中断 ──

func TestBot_StreamingReply(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	linkChat(t, repo, 7, 55, "trader")

	fs := newFakeTelegramServer(t, `[]`)
	b := newTestBot(t, fs, nil)
	b.SetEditThrottle(5 * time.Millisecond)
	var gotReq *RunRequest
	b.SetStreamExecutor(func(ctx context.Context, req *RunRequest) (string, error) {
		gotReq = req
		req.OnDelta("持仓")
		time.Sleep(20 * time.Millisecond) // 让节流编辑有机会触发
		req.OnDelta("一切正常")
		return "持仓一切正常", nil
	})

	b.handleMessage(tgMessage{Chat: tgChat{ID: 55}, Text: "看看我的持仓"})

	if gotReq == nil || gotReq.UserID != 7 || gotReq.Prompt != "看看我的持仓" {
		t.Fatalf("req: %+v", gotReq)
	}
	sent := fs.allSent()
	if len(sent) == 0 || sent[0].Text != "思考中…" {
		t.Fatalf("首条应为占位消息: %+v", sent)
	}
	// 至少发生过一次中间编辑，且最终编辑为完整回复
	edits := fs.allEdits()
	if len(edits) == 0 {
		t.Fatal("应有编辑发生")
	}
	if last := edits[len(edits)-1].Text; last != "持仓一切正常" {
		t.Errorf("最终编辑 = %q", last)
	}
}

func TestBot_StreamingLongReplyChunked(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	linkChat(t, repo, 7, 55, "trader")

	long := strings.Repeat("字", maxChunkRunes+100)
	fs := newFakeTelegramServer(t, `[]`)
	b := newTestBot(t, fs, nil)
	b.SetEditThrottle(5*time.Millisecond)
	b.SetStreamExecutor(func(ctx context.Context, req *RunRequest) (string, error) {
		return long, nil
	})

	b.handleMessage(tgMessage{Chat: tgChat{ID: 55}, Text: "长回复"})
	// 最终编辑首段 ≤3900 rune，剩余段走 sendMessage
	if last := fs.lastEditText(); len([]rune(last)) != maxChunkRunes {
		t.Errorf("首段长度 = %d, want %d", len([]rune(last)), maxChunkRunes)
	}
	sent := fs.allSent()
	if len(sent) < 2 {
		t.Fatalf("应有占位 + 续段: %d 条", len(sent))
	}
	if tail := sent[len(sent)-1].Text; len([]rune(tail)) != 100 {
		t.Errorf("续段长度 = %d, want 100", len([]rune(tail)))
	}
}

func TestBot_SingleFlightBusyHint(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	linkChat(t, repo, 7, 55, "trader")

	fs := newFakeTelegramServer(t, `[]`)
	b := newTestBot(t, fs, nil)
	release := make(chan struct{})
	b.SetStreamExecutor(func(ctx context.Context, req *RunRequest) (string, error) {
		<-release
		return "done", nil
	})

	first := make(chan struct{})
	go func() {
		b.handleMessage(tgMessage{Chat: tgChat{ID: 55}, Text: "第一条"})
		close(first)
	}()
	// 等第一条进入执行
	deadline := time.Now().Add(2 * time.Second)
	for {
		b.mu.Lock()
		busy := len(b.runs) == 1
		b.mu.Unlock()
		if busy || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	b.handleMessage(tgMessage{Chat: tgChat{ID: 55}, Text: "第二条"})
	close(release)
	<-first

	found := false
	for _, m := range fs.allSent() {
		if strings.Contains(m.Text, "正在处理上一条") && strings.Contains(m.Text, "/stop") {
			found = true
		}
	}
	if !found {
		t.Errorf("应有单飞提示: %+v", fs.allSent())
	}
}

func TestBot_StopAbortsRun(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	linkChat(t, repo, 7, 55, "trader")

	fs := newFakeTelegramServer(t, `[]`)
	b := newTestBot(t, fs, nil)
	b.SetEditThrottle(5*time.Millisecond)
	started := make(chan struct{})
	b.SetStreamExecutor(func(ctx context.Context, req *RunRequest) (string, error) {
		close(started)
		<-ctx.Done() // 等 /stop 取消
		return "", ctx.Err()
	})

	done := make(chan struct{})
	go func() {
		b.handleMessage(tgMessage{Chat: tgChat{ID: 55}, Text: "跑一个长任务"})
		close(done)
	}()
	<-started
	b.handleMessage(tgMessage{Chat: tgChat{ID: 55}, Text: "/stop"})
	<-done

	if last := fs.lastEditText(); !strings.Contains(last, "已中断") {
		t.Errorf("中断后最终编辑 = %q, edits=%+v", last, fs.allEdits())
	}
}

func TestBot_RunRequestCarriesConversationAndModel(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	linkChat(t, repo, 7, 55, "trader")
	if err := repo.SetConversationID(55, "conv-3"); err != nil {
		t.Fatal(err)
	}

	fs := newFakeTelegramServer(t, `[]`)
	b := newTestBot(t, fs, nil)
	b.handleMessage(tgMessage{Chat: tgChat{ID: 55}, Text: "/model kimi:k2"})
	var gotReq *RunRequest
	b.SetStreamExecutor(func(ctx context.Context, req *RunRequest) (string, error) {
		gotReq = req
		return "ok", nil
	})
	b.handleMessage(tgMessage{Chat: tgChat{ID: 55}, Text: "hi"})

	if gotReq == nil || gotReq.ConversationID != "conv-3" || gotReq.Model != "kimi:k2" {
		t.Errorf("req 应携带会话绑定与模型覆盖: %+v", gotReq)
	}
}
