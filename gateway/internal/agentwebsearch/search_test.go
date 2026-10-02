package agentwebsearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ddgFixture DDG HTML 结果页样例（含 uddg 跳转链接与摘要）。
const ddgFixture = `<html><body>
<div class="result results_links">
  <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fnews-1&amp;rut=abc">示例新闻 &amp; 标题</a>
  <a class="result__snippet" href="#">第一条 <b>摘要</b>内容</a>
</div>
<div class="result results_links">
  <a rel="nofollow" class="result__a" href="https://direct.example.com/a">直达链接标题</a>
  <a class="result__snippet" href="#">第二条摘要</a>
</div>
</body></html>`

func TestParseDDGHTML(t *testing.T) {
	results := parseDDGHTML(ddgFixture, 10)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].URL != "https://example.com/news-1" {
		t.Fatalf("uddg decode failed: %q", results[0].URL)
	}
	if results[0].Title != "示例新闻 & 标题" {
		t.Fatalf("title cleanup failed: %q", results[0].Title)
	}
	if results[0].Snippet != "第一条 摘要内容" {
		t.Fatalf("snippet cleanup failed: %q", results[0].Snippet)
	}
	if results[1].URL != "https://direct.example.com/a" {
		t.Fatalf("direct link failed: %q", results[1].URL)
	}
}

func TestBraveKeyFromConfig(t *testing.T) {
	// agent.ai 优先
	cfg := map[string]any{
		"agent": map[string]any{"ai": map[string]any{"brave_api_key": "agent-key"}},
		"ai":    map[string]any{"brave_api_key": "top-key"},
	}
	if got := BraveKeyFromConfig(cfg); got != "agent-key" {
		t.Fatalf("agent.ai priority failed: %q", got)
	}
	// 顶层 ai 兜底
	cfg = map[string]any{"ai": map[string]any{"brave_api_key": "top-key"}}
	if got := BraveKeyFromConfig(cfg); got != "top-key" {
		t.Fatalf("top-level ai fallback failed: %q", got)
	}
	if got := BraveKeyFromConfig(map[string]any{}); got != "" {
		t.Fatalf("empty config should be empty key, got %q", got)
	}
}

func TestBraveSearchRequestShape(t *testing.T) {
	var gotToken, gotQuery, gotCount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Subscription-Token")
		gotQuery = r.URL.Query().Get("q")
		gotCount = r.URL.Query().Get("count")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"web":{"results":[{"title":"T1","url":"https://a.com","description":"D1"}]}}`))
	}))
	defer srv.Close()

	p := &Plugin{APIKey: "test-key", BraveBaseURL: srv.URL}
	results, err := p.braveSearch(context.Background(), "比特币 新闻", 7)
	if err != nil {
		t.Fatalf("braveSearch: %v", err)
	}
	if gotToken != "test-key" {
		t.Fatalf("missing subscription token: %q", gotToken)
	}
	if gotQuery != "比特币 新闻" || gotCount != "7" {
		t.Fatalf("query params wrong: q=%q count=%q", gotQuery, gotCount)
	}
	if len(results) != 1 || results[0].URL != "https://a.com" || results[0].Snippet != "D1" {
		t.Fatalf("result mapping wrong: %+v", results)
	}
}

func TestSearchProviderSelection(t *testing.T) {
	braveHit, ddgHit := false, false
	braveSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		braveHit = true
		_, _ = w.Write([]byte(`{"web":{"results":[{"title":"B","url":"https://b.com","description":"bd"}]}}`))
	}))
	defer braveSrv.Close()
	ddgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ddgHit = true
		_, _ = w.Write([]byte(ddgFixture))
	}))
	defer ddgSrv.Close()

	// 有 key → 走 Brave，不碰 DDG
	p := &Plugin{APIKey: "k", BraveBaseURL: braveSrv.URL, DDGBaseURL: ddgSrv.URL}
	out, err := p.search(nil, context.Background(), map[string]any{"query": "x"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	m := out.(map[string]any)
	if m["provider"] != "brave" || !braveHit || ddgHit {
		t.Fatalf("provider selection wrong: %+v braveHit=%v ddgHit=%v", m, braveHit, ddgHit)
	}

	// 无 key → 直接走 DDG
	braveHit, ddgHit = false, false
	p = &Plugin{BraveBaseURL: braveSrv.URL, DDGBaseURL: ddgSrv.URL}
	out, _ = p.search(nil, context.Background(), map[string]any{"query": "x", "limit": float64(99)})
	m = out.(map[string]any)
	if m["provider"] != "duckduckgo" || braveHit || !ddgHit {
		t.Fatalf("ddg fallback wrong: %+v braveHit=%v ddgHit=%v", m, braveHit, ddgHit)
	}
	if m["count"] != 2 { // limit 99 被夹到 10，fixture 只有 2 条
		t.Fatalf("count wrong: %v", m["count"])
	}
}

func TestSearchBraveFailureFallsBackToDDG(t *testing.T) {
	braveSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer braveSrv.Close()
	ddgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(ddgFixture))
	}))
	defer ddgSrv.Close()

	p := &Plugin{APIKey: "bad-key", BraveBaseURL: braveSrv.URL, DDGBaseURL: ddgSrv.URL}
	out, err := p.search(nil, context.Background(), map[string]any{"query": "x"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if m := out.(map[string]any); m["provider"] != "duckduckgo" {
		t.Fatalf("should fall back to ddg: %+v", m)
	}
}

func TestSearchTotalFailureSoftError(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()

	p := &Plugin{BraveBaseURL: bad.URL, DDGBaseURL: bad.URL}
	out, err := p.search(nil, context.Background(), map[string]any{"query": "x"})
	if err != nil {
		t.Fatalf("soft error must not be a Go error: %v", err)
	}
	s, ok := out.(string)
	if !ok || !strings.Contains(s, "搜索暂不可用") {
		t.Fatalf("expected soft error string, got %+v", out)
	}
}

func TestExtractTextCleanup(t *testing.T) {
	page := `<html><head><style>body{color:red}</style><script>var x=1;</script></head>
<body><!-- 注释 --><h1>标题 &amp; 正文</h1><p>第一段<strong>加粗</strong></p><p>第二段</p></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()

	p := &Plugin{}
	text, err := p.extractText(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("extractText: %v", err)
	}
	for _, bad := range []string{"var x=1", "color:red", "注释", "<p>", "<strong>"} {
		if strings.Contains(text, bad) {
			t.Fatalf("cleanup left %q in %q", bad, text)
		}
	}
	if !strings.Contains(text, "标题 & 正文") || !strings.Contains(text, "第二段") {
		t.Fatalf("content missing: %q", text)
	}
}

func TestExtractTextTruncation(t *testing.T) {
	long := strings.Repeat("正", 5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<p>" + long + "</p>"))
	}))
	defer srv.Close()

	p := &Plugin{}
	text, err := p.extractText(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("extractText: %v", err)
	}
	if n := len([]rune(text)); n > maxExtractChars+1 { // +1 为省略号
		t.Fatalf("not truncated: %d runes", n)
	}
}

func TestExtractRejectsNonHTTP(t *testing.T) {
	p := &Plugin{}
	if _, err := p.extractText(context.Background(), "ftp://x"); err == nil {
		t.Fatal("should reject non-http url")
	}
}
