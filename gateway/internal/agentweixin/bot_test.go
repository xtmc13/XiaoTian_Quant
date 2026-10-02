package agentweixin

import (
	"context"
	"encoding/base64"
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

const (
	testBotID = "bot_test_1"
	testToken = "tok_test_1"
	testWxid  = "user1@im.wechat"
)

// testQRImg 假 base64 PNG。
var testQRImg = base64.StdEncoding.EncodeToString([]byte("fake-png-bytes"))

// setupWXTestDB 初始化临时 SQLite（含 0055 迁移）。
func setupWXTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-agent-weixin")
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(store.CloseDB)
}

// inbound 一条排队待投递的入站消息。
type inbound struct {
	From        string
	Text        string
	ContextToken string
}

// wxSent 一条已发送的 sendmessage 记录。
type wxSent struct {
	ToUser       string
	Text         string
	ContextToken string
	MessageType  int
	MessageState int
	ItemType     int
}

// fakeILinkServer 模拟腾讯 iLink 服务端：QR 签发 / 状态轮询 / getupdates / sendmessage / getconfig / sendtyping。
type fakeILinkServer struct {
	mu           sync.Mutex
	qrStatusHits int
	confirmed    bool // 第二次轮询起返回 confirmed
	bufSeen      []string
	sent         []wxSent
	typings      []string
	inboundQueue []inbound
	srv          *httptest.Server
}

func newFakeILinkServer(t *testing.T) *fakeILinkServer {
	f := &fakeILinkServer{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/ilink/bot/get_bot_qrcode"):
			if got := r.URL.Query().Get("bot_type"); got != "3" {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"errmsg":"bad bot_type"}`)
				return
			}
			fmt.Fprintf(w, `{"qrcode":"qr-test-1","qrcode_img_content":%q}`, testQRImg)

		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/ilink/bot/get_qrcode_status"):
			f.mu.Lock()
			f.qrStatusHits++
			hits := f.qrStatusHits
			f.mu.Unlock()
			if hits < 2 {
				fmt.Fprint(w, `{"status":"wait"}`)
				return
			}
			// 第二轮起 confirmed；baseurl 指向 fake 自身供后续长轮询
			fmt.Fprint(w, fmt.Sprintf(`{"status":"confirmed","bot_token":%q,"ilink_bot_id":%q,"baseurl":%q,"ilink_user_id":"owner@im.wechat"}`,
				testToken, testBotID, f.srv.URL))

		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/ilink/bot/getupdates"):
			body, _ := io.ReadAll(r.Body)
			var req struct {
				GetUpdatesBuf string `json:"get_updates_buf"`
			}
			_ = json.Unmarshal(body, &req)
			f.mu.Lock()
			f.bufSeen = append(f.bufSeen, req.GetUpdatesBuf)
			msgs := []map[string]any{}
			for _, in := range f.inboundQueue {
				msgs = append(msgs, map[string]any{
					"from_user_id":   in.From,
					"to_user_id":     "bot@im.bot",
					"message_type":   1,
					"message_state":  2,
					"context_token":  in.ContextToken,
					"item_list":      []map[string]any{{"type": 1, "text_item": map[string]any{"text": in.Text}}},
				})
			}
			f.inboundQueue = nil
			buf := "buf-cursor-1"
			if req.GetUpdatesBuf != "" {
				buf = req.GetUpdatesBuf
			}
			f.mu.Unlock()
			resp, _ := json.Marshal(map[string]any{
				"ret":               0,
				"msgs":              msgs,
				"get_updates_buf":   buf,
			})
			time.Sleep(20 * time.Millisecond) // 避免轮询空转
			w.Write(resp)

		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/ilink/bot/sendmessage"):
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Msg struct {
					ToUserID     string `json:"to_user_id"`
					MessageType  int    `json:"message_type"`
					MessageState int    `json:"message_state"`
					ContextToken string `json:"context_token"`
					ItemList     []struct {
						Type     int `json:"type"`
						TextItem struct {
							Text string `json:"text"`
						} `json:"text_item"`
					} `json:"item_list"`
				} `json:"msg"`
			}
			_ = json.Unmarshal(body, &req)
			s := wxSent{
				ToUser:       req.Msg.ToUserID,
				Text:         "",
				ContextToken: req.Msg.ContextToken,
				MessageType:  req.Msg.MessageType,
				MessageState: req.Msg.MessageState,
			}
			if len(req.Msg.ItemList) > 0 {
				s.ItemType = req.Msg.ItemList[0].Type
				s.Text = req.Msg.ItemList[0].TextItem.Text
			}
			f.mu.Lock()
			f.sent = append(f.sent, s)
			f.mu.Unlock()
			fmt.Fprint(w, `{"ret":0,"message_id":"1"}`)

		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/ilink/bot/getconfig"):
			fmt.Fprint(w, `{"ret":0,"typing_ticket":"ticket-1"}`)

		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/ilink/bot/sendtyping"):
			body, _ := io.ReadAll(r.Body)
			var req struct {
				ILinkUserID string `json:"ilink_user_id"`
			}
			_ = json.Unmarshal(body, &req)
			f.mu.Lock()
			f.typings = append(f.typings, req.ILinkUserID)
			f.mu.Unlock()
			fmt.Fprint(w, `{"ret":0}`)

		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeILinkServer) allSent() []wxSent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]wxSent{}, f.sent...)
}

func (f *fakeILinkServer) allTypings() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.typings...)
}

func (f *fakeILinkServer) queueInbound(from, text, ctxToken string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inboundQueue = append(f.inboundQueue, inbound{From: from, Text: text, ContextToken: ctxToken})
}

// waitFor 轮询直到 cond 满足（3s 超时）。
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", msg)
}

// newTestBot 测试用 bot：fake API + 启动轮询。
func newTestBot(t *testing.T, fs *fakeILinkServer, exec Executor) *Bot {
	t.Helper()
	b := NewBot(NewRepo())
	b.SetAPIBase(fs.srv.URL)
	b.SetExecutor(exec)
	b.Start()
	t.Cleanup(b.Stop)
	return b
}

func TestQRLoginAndPairFlow(t *testing.T) {
	setupWXTestDB(t)
	fs := newFakeILinkServer(t)
	b := newTestBot(t, fs, nil)
	repo := NewRepo()

	// 未登录时 status
	if logged, _ := b.Status(); logged {
		t.Fatal("初始应为未登录")
	}

	// 生成二维码
	img, expiresIn, err := b.StartQRLogin(context.Background())
	if err != nil {
		t.Fatalf("StartQRLogin: %v", err)
	}
	if img != testQRImg || expiresIn <= 0 {
		t.Fatalf("二维码返回异常: img=%q expires=%d", img, expiresIn)
	}

	// 轮询直至 confirmed（fake 第二次轮询返回 confirmed）
	waitFor(t, func() bool { return b.QRStatus() == "confirmed" }, "QR 登录 confirmed")

	// 登录态落库
	st, err := repo.LoadLogin()
	if err != nil || st == nil || st.BotToken != testToken || st.BotID != testBotID {
		t.Fatalf("登录态落库错误: st=%+v err=%v", st, err)
	}
	if logged, botID := b.Status(); !logged || botID != testBotID {
		t.Fatalf("Status 异常: logged=%v botID=%q", logged, botID)
	}

	// 入站配对码消息 → 绑定成功回复（带 context_token）
	code, err := repo.CreatePairCode(42)
	if err != nil {
		t.Fatal(err)
	}
	fs.queueInbound(testWxid, code, "ctx-pair-1")
	waitFor(t, func() bool {
		for _, s := range fs.allSent() {
			if s.ToUser == testWxid && strings.Contains(s.Text, "绑定成功") {
				if s.ContextToken != "ctx-pair-1" {
					t.Fatalf("回复缺 context_token: %+v", s)
				}
				if s.MessageType != 2 || s.MessageState != 2 || s.ItemType != 1 {
					t.Fatalf("回复消息结构错误: %+v", s)
				}
				return true
			}
		}
		return false
	}, "配对绑定成功回复")

	link, err := repo.GetByWXID(testWxid)
	if err != nil || link.UserID != 42 {
		t.Fatalf("绑定落库错误: %+v err=%v", link, err)
	}
}

func TestInboundHeadlessRun(t *testing.T) {
	setupWXTestDB(t)
	fs := newFakeILinkServer(t)

	var gotReq *RunRequest
	b := newTestBot(t, fs, func(ctx context.Context, req *RunRequest) (string, error) {
		gotReq = req
		return "BTC 现价 100000", nil
	})
	repo := NewRepo()

	// 直接持久化登录态（模拟已登录重启恢复），并预绑定
	if err := repo.SaveLogin(&LoginState{BotToken: testToken, BaseURL: fs.srv.URL, BotID: testBotID, LoggedInAt: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	// 手动注入登录态并广播（不依赖 QR 流程）
	b.setLogin(&LoginState{BotToken: testToken, BaseURL: fs.srv.URL, BotID: testBotID})
	code, _ := repo.CreatePairCode(7)
	if _, err := repo.ConsumePairCode(code, "user2@im.wechat"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetModel("user2@im.wechat", "deepseek:deepseek-chat"); err != nil {
		t.Fatal(err)
	}

	fs.queueInbound("user2@im.wechat", "看看 BTC", "ctx-run-1")
	waitFor(t, func() bool {
		for _, s := range fs.allSent() {
			if s.ToUser == "user2@im.wechat" && s.Text == "BTC 现价 100000" {
				if s.ContextToken != "ctx-run-1" {
					t.Fatalf("回复缺 context_token: %+v", s)
				}
				return true
			}
		}
		return false
	}, "headless 执行回复")

	if gotReq == nil || gotReq.UserID != 7 || gotReq.Prompt != "看看 BTC" || gotReq.Model != "deepseek:deepseek-chat" {
		t.Fatalf("执行请求错误: %+v", gotReq)
	}

	// sendtyping：getconfig 取 ticket 后发送输入指示
	waitFor(t, func() bool {
		return len(fs.allTypings()) > 0 && fs.allTypings()[0] == "user2@im.wechat"
	}, "sendtyping 输入指示")

	// 游标持久化（fake 首轮返回 buf-cursor-1）
	st, err := repo.LoadLogin()
	if err != nil || st == nil || st.GetUpdatesBuf != "buf-cursor-1" {
		t.Fatalf("游标未持久化: %+v err=%v", st, err)
	}
}

func TestUnlinkClearsLogin(t *testing.T) {
	setupWXTestDB(t)
	fs := newFakeILinkServer(t)
	b := newTestBot(t, fs, nil)
	repo := NewRepo()

	if err := repo.SaveLogin(&LoginState{BotToken: testToken, BaseURL: fs.srv.URL, BotID: testBotID, LoggedInAt: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	b.setLogin(&LoginState{BotToken: testToken, BaseURL: fs.srv.URL, BotID: testBotID})
	// 未绑定用户：SendToUser 应 false
	if b.SendToUser(1, "x") {
		t.Fatal("未绑定用户 SendToUser 应返回 false")
	}

	if err := b.Unlink(); err != nil {
		t.Fatal(err)
	}
	st, err := repo.LoadLogin()
	if err != nil || st != nil {
		t.Fatalf("解绑后登录态应清除: st=%+v err=%v", st, err)
	}
	if logged, _ := b.Status(); logged {
		t.Fatal("解绑后 Status 应为未登录")
	}
	if b.Send("anyone@im.wechat", "hi", "") {
		t.Fatal("未登录 Send 应返回 false")
	}
}
