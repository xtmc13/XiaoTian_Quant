package agentwecom

import (
	"context"
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

const (
	testCorpID = "ww_test_corp"
	testToken  = "tok_test"
	testSecret = "sec_test"
)

// testAESKey 生成 43 字符 EncodingAESKey（32 字节密钥 base64 去 "="）。
func testAESKey() (envKey string, key []byte) {
	key = make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	full := base64.StdEncoding.EncodeToString(key) // 44 字符含 "="
	return strings.TrimSuffix(full, "="), key
}

// setupWCTestDB 初始化临时 SQLite（含 0054 迁移）。
func setupWCTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-agent-wecom")
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(store.CloseDB)
}

// setWCCredentials 注入测试凭据。
func setWCCredentials(t *testing.T) (key []byte) {
	t.Helper()
	envKey, rawKey := testAESKey()
	t.Setenv("WECOM_CORP_ID", testCorpID)
	t.Setenv("WECOM_SECRET", testSecret)
	t.Setenv("WECOM_TOKEN", testToken)
	t.Setenv("WECOM_AES_KEY", envKey)
	t.Setenv("WECOM_AGENT_ID", "1000002")
	return rawKey
}

// wecomSent 一条主动推送记录。
type wecomSent struct {
	ToUser string
	Text   string
}

// fakeWecomServer 模拟企业微信服务端：gettoken 签发 + message/send 记录。
type fakeWecomServer struct {
	mu       sync.Mutex
	tokenHit int
	sent     []wecomSent
	srv      *httptest.Server
}

func newFakeWecomServer(t *testing.T) *fakeWecomServer {
	f := &fakeWecomServer{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/cgi-bin/gettoken"):
			f.mu.Lock()
			f.tokenHit++
			f.mu.Unlock()
			fmt.Fprint(w, `{"errcode":0,"errmsg":"ok","access_token":"t-wx","expires_in":7200}`)
		case strings.Contains(r.URL.Path, "/cgi-bin/message/send"):
			if r.URL.Query().Get("access_token") != "t-wx" {
				fmt.Fprint(w, `{"errcode":40014,"errmsg":"invalid access_token"}`)
				return
			}
			body, _ := io.ReadAll(r.Body)
			var m struct {
				ToUser string `json:"touser"`
				Text   struct {
					Content string `json:"content"`
				} `json:"text"`
				AgentID int `json:"agentid"`
			}
			_ = json.Unmarshal(body, &m)
			if m.AgentID != 1000002 {
				fmt.Fprint(w, `{"errcode":40056,"errmsg":"invalid agentid"}`)
				return
			}
			f.mu.Lock()
			f.sent = append(f.sent, wecomSent{ToUser: m.ToUser, Text: m.Text.Content})
			f.mu.Unlock()
			fmt.Fprint(w, `{"errcode":0,"errmsg":"ok"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeWecomServer) allSent() []wecomSent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]wecomSent{}, f.sent...)
}

// newTestBot 测试用 bot：env 凭据 + fake API。
func newTestBot(t *testing.T, fs *fakeWecomServer, exec Executor) *Bot {
	t.Helper()
	b := NewBot(NewRepo())
	b.SetAPIBase(fs.srv.URL)
	b.SetExecutor(exec)
	return b
}

// buildTextEnvelope 构造加密的消息回调体与签名查询串。
func buildTextEnvelope(t *testing.T, key []byte, from, content string) (body, query string) {
	t.Helper()
	inner := fmt.Sprintf(`<xml><ToUserName><![CDATA[%s]]></ToUserName><FromUserName><![CDATA[%s]]></FromUserName><CreateTime>1</CreateTime><MsgType><![CDATA[text]]></MsgType><Content><![CDATA[%s]]></Content><MsgId>1</MsgId><AgentID>1000002</AgentID></xml>`,
		testCorpID, from, content)
	enc, err := encryptMsg(key, []byte(inner), testCorpID)
	if err != nil {
		t.Fatal(err)
	}
	body = fmt.Sprintf(`<xml><ToUserName><![CDATA[%s]]></ToUserName><Encrypt><![CDATA[%s]]></Encrypt><AgentID><![CDATA[1000002]]></AgentID></xml>`,
		testCorpID, enc)
	ts, nonce := "1700000000", "nonce1"
	v := url.Values{}
	v.Set("msg_signature", msgSignature(testToken, ts, nonce, enc))
	v.Set("timestamp", ts)
	v.Set("nonce", nonce)
	return body, v.Encode()
}

func TestCrypto_SignDecryptRoundtrip(t *testing.T) {
	envKey, key := testAESKey()
	gotKey, err := decodeAESKey(envKey)
	if err != nil || len(gotKey) != 32 {
		t.Fatalf("decodeAESKey: %v len=%d", err, len(gotKey))
	}
	enc, err := encryptMsg(key, []byte("你好，企业微信"), testCorpID)
	if err != nil {
		t.Fatal(err)
	}
	// 签名确定性：四串排序拼接 sha1
	sig := msgSignature(testToken, "1", "n", enc)
	if len(sig) != 40 || sig != msgSignature(testToken, "1", "n", enc) {
		t.Errorf("签名异常: %q", sig)
	}
	msg, corp, err := decryptMsg(key, enc)
	if err != nil {
		t.Fatal(err)
	}
	if string(msg) != "你好，企业微信" || corp != testCorpID {
		t.Errorf("回环失败: msg=%q corp=%q", msg, corp)
	}
}

func TestWebhook_EchostrGET(t *testing.T) {
	setupWCTestDB(t)
	key := setWCCredentials(t)
	fs := newFakeWecomServer(t)
	b := newTestBot(t, fs, nil)

	enc, err := encryptMsg(key, []byte("echo-123"), testCorpID)
	if err != nil {
		t.Fatal(err)
	}
	ts, nonce := "1700000001", "nonce2"
	v := url.Values{}
	v.Set("msg_signature", msgSignature(testToken, ts, nonce, enc))
	v.Set("timestamp", ts)
	v.Set("nonce", nonce)
	v.Set("echostr", enc)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/wecom/webhook?"+v.Encode(), nil)
	w := httptest.NewRecorder()
	b.HandleWebhook(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "echo-123" {
		t.Errorf("echostr 验证: code=%d body=%q", w.Code, w.Body.String())
	}

	// 坏签名 → 401
	v.Set("msg_signature", "deadbeef")
	req = httptest.NewRequest(http.MethodGet, "/api/agent/wecom/webhook?"+v.Encode(), nil)
	w = httptest.NewRecorder()
	b.HandleWebhook(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("坏签名应 401, got %d", w.Code)
	}
}

func TestWebhook_BadSignaturePOST(t *testing.T) {
	setupWCTestDB(t)
	key := setWCCredentials(t)
	fs := newFakeWecomServer(t)
	b := newTestBot(t, fs, nil)

	body, _ := buildTextEnvelope(t, key, "staff1", "hello")
	// 重建查询串但使用错误签名
	v := url.Values{}
	v.Set("msg_signature", "0000000000000000000000000000000000000000")
	v.Set("timestamp", "1700000000")
	v.Set("nonce", "nonce1")
	req := httptest.NewRequest(http.MethodPost, "/api/agent/wecom/webhook?"+v.Encode(), strings.NewReader(body))
	w := httptest.NewRecorder()
	b.HandleWebhook(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("坏签名应 401, got %d", w.Code)
	}
}

func TestWebhook_PairFlow(t *testing.T) {
	setupWCTestDB(t)
	key := setWCCredentials(t)
	fs := newFakeWecomServer(t)
	b := newTestBot(t, fs, nil)
	repo := NewRepo()
	code, err := repo.CreatePairCode(42)
	if err != nil {
		t.Fatal(err)
	}

	body, query := buildTextEnvelope(t, key, "staff1", code)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/wecom/webhook?"+query, strings.NewReader(body))
	w := httptest.NewRecorder()
	b.HandleWebhook(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("回调应立即 200, got %d", w.Code)
	}

	// 异步处理：轮询推送记录
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range fs.allSent() {
			if s.ToUser == "staff1" && strings.Contains(s.Text, "绑定成功") {
				link, err := repo.GetByStaffID("staff1")
				if err != nil || link.UserID != 42 {
					t.Fatalf("绑定落库错误: %+v err=%v", link, err)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("未收到绑定成功推送: %+v", fs.allSent())
}

func TestWebhook_MessageHeadlessPush(t *testing.T) {
	setupWCTestDB(t)
	key := setWCCredentials(t)
	fs := newFakeWecomServer(t)
	var gotReq *RunRequest
	b := newTestBot(t, fs, func(ctx context.Context, req *RunRequest) (string, error) {
		gotReq = req
		return "BTC 现价 100000", nil
	})
	repo := NewRepo()
	code, _ := repo.CreatePairCode(7)
	if _, err := repo.ConsumePairCode(code, "staff2"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetModel("staff2", "deepseek:deepseek-chat"); err != nil {
		t.Fatal(err)
	}

	body, query := buildTextEnvelope(t, key, "staff2", "看看 BTC")
	req := httptest.NewRequest(http.MethodPost, "/api/agent/wecom/webhook?"+query, strings.NewReader(body))
	w := httptest.NewRecorder()
	b.HandleWebhook(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("回调应立即 200, got %d", w.Code)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range fs.allSent() {
			if s.ToUser == "staff2" && s.Text == "BTC 现价 100000" {
				if gotReq == nil || gotReq.UserID != 7 || gotReq.Prompt != "看看 BTC" || gotReq.Model != "deepseek:deepseek-chat" {
					t.Fatalf("执行请求错误: %+v", gotReq)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("未收到 headless 推送: %+v", fs.allSent())
}

func TestWebhook_UnconfiguredInert(t *testing.T) {
	setupWCTestDB(t)
	t.Setenv("WECOM_CORP_ID", "")
	t.Setenv("WECOM_SECRET", "")
	t.Setenv("WECOM_TOKEN", "")
	t.Setenv("WECOM_AES_KEY", "")
	t.Setenv("WECOM_AGENT_ID", "")
	b := NewBot(NewRepo())

	req := httptest.NewRequest(http.MethodGet, "/api/agent/wecom/webhook?echostr=x", nil)
	w := httptest.NewRecorder()
	b.HandleWebhook(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("未配置应 503, got %d", w.Code)
	}
	if Configured() {
		t.Error("未配置时 Configured 应为 false")
	}
	if b.SendToUser(1, "x") {
		t.Error("未配置 SendToUser 应返回 false")
	}
}
