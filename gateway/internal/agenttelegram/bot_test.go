package agenttelegram

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeTelegramServer 模拟 Telegram Bot API，记录 sendMessage 的聊天与文本。
type fakeTelegramServer struct {
	mu      sync.Mutex
	sent    []sentMsg
	updates string // 下一个 getUpdates 返回的 result JSON
	srv     *httptest.Server
}

type sentMsg struct {
	ChatID int64  `json:"chat_id"`
	Text   string `json:"text"`
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
			f.sent = append(f.sent, m)
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
