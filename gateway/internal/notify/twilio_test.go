package notify

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

func TestTwilioChannelSend(t *testing.T) {
	var gotForm url.Values
	var gotAuth string
	var calls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if !strings.HasSuffix(r.URL.Path, "/2010-04-01/Accounts/AC_test/Messages.json") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"sid":"SM_test","status":"queued"}`))
	}))
	defer srv.Close()

	twilioAPIBase = srv.URL
	defer func() { twilioAPIBase = "" }()

	ch := &TwilioChannel{
		accountSID: "AC_test",
		authToken:  "auth_token_test",
		fromNumber: "+15551234567",
		defaultTo:  "+8613900000000",
		enabled:    true,
		httpClient: &http.Client{Timeout: 2 * time.Second},
	}
	if ch.Name() != "sms" {
		t.Fatalf("channel name should be sms, got %s", ch.Name())
	}
	if !ch.IsEnabled() {
		t.Fatal("channel should be enabled")
	}

	msg := Message{
		Title:     "**Risk Alert**: drawdown",
		Content:   "Daily loss 12% exceeds limit 10%",
		Level:     "CRITICAL",
		Timestamp: time.Date(2026, time.September, 19, 10, 0, 0, 0, time.UTC).UnixMilli(),
	}
	if err := ch.Send(msg); err != nil {
		t.Fatalf("send failed: %v", err)
	}

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected 1 upstream call, got %d", calls)
	}
	// Basic auth: base64(sid:token)
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("AC_test:auth_token_test"))
	if gotAuth != wantAuth {
		t.Fatalf("bad basic auth header: %q", gotAuth)
	}
	if gotForm.Get("To") != "+8613900000000" || gotForm.Get("From") != "+15551234567" {
		t.Fatalf("bad to/from: %+v", gotForm)
	}
	body := gotForm.Get("Body")
	if strings.Contains(body, "**") {
		t.Fatalf("markdown should be stripped: %q", body)
	}
	if !strings.Contains(body, "Risk Alert: drawdown") {
		t.Fatalf("title missing: %q", body)
	}
	if !strings.Contains(body, "[CRITICAL]") || !strings.Contains(body, "2026-09-19") {
		t.Fatalf("level/time trailer missing: %q", body)
	}
}

func TestTwilioChannelSMSToOverride(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(b))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"sid":"SM_x"}`))
	}))
	defer srv.Close()
	twilioAPIBase = srv.URL
	defer func() { twilioAPIBase = "" }()

	ch := &TwilioChannel{accountSID: "AC", authToken: "tk", fromNumber: "+1", defaultTo: "+8613900000000", enabled: true}
	msg := Message{Title: "T", Content: "C", Level: "INFO", Tags: map[string]string{"sms_to": "+8613800000001"}}
	if err := ch.Send(msg); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if gotForm.Get("To") != "+8613800000001" {
		t.Fatalf("sms_to tag should override default recipient, got %q", gotForm.Get("To"))
	}
}

func TestTwilioChannelAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		payload, _ := json.Marshal(map[string]any{"code": 21211, "message": "Invalid 'To' Phone Number"})
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	twilioAPIBase = srv.URL
	defer func() { twilioAPIBase = "" }()

	ch := &TwilioChannel{accountSID: "AC", authToken: "tk", fromNumber: "+1", defaultTo: "+bad", enabled: true}
	err := ch.Send(Message{Title: "t", Content: "c", Level: "INFO"})
	if err == nil || !strings.Contains(err.Error(), "21211") {
		t.Fatalf("expected twilio api error with code, got %v", err)
	}
}

func TestTwilioChannelTimeoutAndDisable(t *testing.T) {
	// 未配置 recipient → 明确报错
	ch := &TwilioChannel{accountSID: "AC", authToken: "tk", fromNumber: "+1", enabled: true}
	if err := ch.Send(Message{Title: "t", Content: "c", Level: "INFO"}); err == nil ||
		!strings.Contains(err.Error(), "no recipient") {
		t.Fatalf("expected no-recipient error, got %v", err)
	}

	// env 构建：缺 from_number → 禁用
	t.Setenv("TWILIO_ACCOUNT_SID", "AC")
	t.Setenv("TWILIO_AUTH_TOKEN", "tk")
	t.Setenv("TWILIO_FROM_NUMBER", "")
	t.Setenv("TWILIO_TO_NUMBER", "+8613900000000")
	if NewTwilioChannelFromEnv().IsEnabled() {
		t.Fatal("channel must be disabled without TWILIO_FROM_NUMBER")
	}
}

func TestSMSTextTruncation(t *testing.T) {
	long := strings.Repeat("测", 2000) // 2000 字符 > 1600 上限
	msg := Message{Title: "T", Content: long, Level: "INFO", Timestamp: time.Now().UnixMilli()}
	out := SMSText(msg)
	if n := utf8.RuneCountInString(out); n != smsBodyLimit {
		t.Fatalf("truncated SMS should be exactly %d chars, got %d", smsBodyLimit, n)
	}
	if !utf8.ValidString(out) {
		t.Fatal("truncated SMS must be valid UTF-8")
	}
	if !strings.HasSuffix(out, "…") {
		t.Fatal("truncated SMS should end with ellipsis")
	}

	short := SMSText(Message{Title: "Hi", Content: "short", Level: "INFO", Timestamp: time.Now().UnixMilli()})
	if strings.HasSuffix(short, "…") || utf8.RuneCountInString(short) > smsBodyLimit {
		t.Fatalf("short message must not be truncated: %q", short)
	}
}

// TestTwilioChannelRequestConstruction 补强请求构造断言：POST 方法、表单
// Content-Type、完整 endpoint（含 account SID）、Basic Auth 与表单字段。
func TestTwilioChannelRequestConstruction(t *testing.T) {
	var gotMethod, gotCT, gotPath, gotAuth string
	var gotForm url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"sid":"SM_construction"}`))
	}))
	defer srv.Close()
	twilioAPIBase = srv.URL
	defer func() { twilioAPIBase = "" }()

	ch := &TwilioChannel{accountSID: "AC_construction", authToken: "tk_secret",
		fromNumber: "+15550001111", defaultTo: "+8613900000000", enabled: true}
	if err := ch.Send(Message{Title: "Order Paid", Content: "plan monthly activated", Level: "INFO"}); err != nil {
		t.Fatalf("send failed: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("must be POST, got %s", gotMethod)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Fatalf("bad content-type: %q", gotCT)
	}
	wantPath := "/2010-04-01/Accounts/AC_construction/Messages.json"
	if gotPath != wantPath {
		t.Fatalf("bad endpoint: %q, want %q", gotPath, wantPath)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("AC_construction:tk_secret"))
	if gotAuth != wantAuth {
		t.Fatalf("bad basic auth header: %q", gotAuth)
	}
	if gotForm.Get("To") != "+8613900000000" || gotForm.Get("From") != "+15550001111" {
		t.Fatalf("bad to/from form: %+v", gotForm)
	}
	if body := gotForm.Get("Body"); !strings.Contains(body, "Order Paid") {
		t.Fatalf("body form missing content: %q", body)
	}
}

// TestTwilioChannelNonJSONError 非 JSON 错误体（网关 5xx/HTML）时错误信息
// 应回退为 status + 原始 body 片段，而不是静默成功或空错误。
func TestTwilioChannelNonJSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`<html>upstream down</html>`))
	}))
	defer srv.Close()
	twilioAPIBase = srv.URL
	defer func() { twilioAPIBase = "" }()

	ch := &TwilioChannel{accountSID: "AC", authToken: "tk", fromNumber: "+1", defaultTo: "+8613900000000", enabled: true}
	err := ch.Send(Message{Title: "t", Content: "c", Level: "INFO"})
	if err == nil {
		t.Fatal("non-2xx must return error")
	}
	if !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "upstream down") {
		t.Fatalf("error should carry status and raw body, got %v", err)
	}
	if strings.Contains(err.Error(), "(code 0)") {
		t.Fatalf("must not append empty twilio code: %v", err)
	}
}

// TestTwilioChannelTransportError 上游不可达（连接拒绝）时必须包装并上报错误。
func TestTwilioChannelTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // 立即关闭 → 连接被拒绝
	twilioAPIBase = url
	defer func() { twilioAPIBase = "" }()

	ch := &TwilioChannel{accountSID: "AC", authToken: "tk", fromNumber: "+1", defaultTo: "+8613900000000", enabled: true,
		httpClient: &http.Client{Timeout: 2 * time.Second}}
	if err := ch.Send(Message{Title: "t", Content: "c", Level: "INFO"}); err == nil ||
		!strings.HasPrefix(err.Error(), "twilio: ") {
		t.Fatalf("transport error must be wrapped with twilio prefix, got %v", err)
	}
}
