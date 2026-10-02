// Package agentwebsearch 联网搜索插件：web_search（实时资讯检索）与
// web_extract（网页正文抽取）两个只读工具，零外部依赖（stdlib only）。
//
// 搜索提供方优先级：
//  1. config.yaml 的 agent.ai.brave_api_key（或 ai.brave_api_key）→ Brave Web Search API；
//  2. 未配置 key 或 Brave 调用失败 → DuckDuckGo HTML 轻量抓取（无需 key）。
//
// 全部失败时返回「搜索暂不可用」软错误字符串，不以 error 中断对话轮次。
package agentwebsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// SearchResult 单条搜索结果。
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// realisticUA 抓取用的真实浏览器 UA（DuckDuckGo HTML 端点对脚本 UA 会 403）。
const realisticUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// BraveKeyFromConfig 从 store 配置取 Brave API key：
// 优先 agent.ai.brave_api_key，兼容顶层 ai.brave_api_key；空串 = 未配置。
func BraveKeyFromConfig(cfg map[string]any) string {
	if agent, ok := cfg["agent"].(map[string]any); ok {
		if ai, ok := agent["ai"].(map[string]any); ok {
			if k, ok := ai["brave_api_key"].(string); ok && strings.TrimSpace(k) != "" {
				return strings.TrimSpace(k)
			}
		}
	}
	if ai, ok := cfg["ai"].(map[string]any); ok {
		if k, ok := ai["brave_api_key"].(string); ok {
			return strings.TrimSpace(k)
		}
	}
	return ""
}

// ── Brave Web Search API ──

type braveResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

// braveSearch 调 Brave Web Search API（X-Subscription-Token 头鉴权）。
func (p *Plugin) braveSearch(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	base := p.BraveBaseURL
	if base == "" {
		base = "https://api.search.brave.com/res/v1/web/search"
	}
	u := fmt.Sprintf("%s?q=%s&count=%d", base, url.QueryEscape(query), limit)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", p.APIKey)
	body, err := p.do(req)
	if err != nil {
		return nil, err
	}
	var resp braveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("brave 响应解析失败: %w", err)
	}
	out := make([]SearchResult, 0, len(resp.Web.Results))
	for _, r := range resp.Web.Results {
		out = append(out, SearchResult{Title: r.Title, URL: r.URL, Snippet: r.Description})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("brave 返回空结果")
	}
	return out, nil
}

// ── DuckDuckGo HTML 兜底 ──

var (
	// 结果链接：<a rel="nofollow" class="result__a" href="...">标题</a>
	ddgLinkRe = regexp.MustCompile(`(?s)<a[^>]*class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	// 摘要：<a class="result__snippet" ...>摘要</a>
	ddgSnippetRe = regexp.MustCompile(`(?s)class="result__snippet"[^>]*>(.*?)</a>`)
	tagRe        = regexp.MustCompile(`(?s)<[^>]+>`)
	wsRe         = regexp.MustCompile(`\s+`)
)

// ddgSearch 抓 DuckDuckGo HTML 端点并解析结果链接/摘要（无 key 兜底）。
func (p *Plugin) ddgSearch(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	base := p.DDGBaseURL
	if base == "" {
		base = "https://html.duckduckgo.com/html/"
	}
	u := fmt.Sprintf("%s?q=%s", base, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", realisticUA)
	req.Header.Set("Accept", "text/html")
	body, err := p.do(req)
	if err != nil {
		return nil, err
	}
	return parseDDGHTML(string(body), limit), nil
}

// parseDDGHTML 解析 DDG HTML 结果页（链接与摘要按出现顺序配对）。
func parseDDGHTML(page string, limit int) []SearchResult {
	links := ddgLinkRe.FindAllStringSubmatch(page, limit)
	snippets := ddgSnippetRe.FindAllStringSubmatch(page, limit)
	out := make([]SearchResult, 0, len(links))
	for i, m := range links {
		snippet := ""
		if i < len(snippets) {
			snippet = cleanText(snippets[i][1])
		}
		out = append(out, SearchResult{
			Title:   cleanText(m[2]),
			URL:     decodeDDGLink(m[1]),
			Snippet: snippet,
		})
	}
	return out
}

// decodeDDGLink DDG 跳转链接（//duckduckgo.com/l/?uddg=<urlencoded>）还原真实 URL。
func decodeDDGLink(href string) string {
	href = html.UnescapeString(href)
	if u, err := url.Parse(href); err == nil {
		if uddg := u.Query().Get("uddg"); uddg != "" {
			return uddg
		}
	}
	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	return href
}

// cleanText 去标签 + 反转义 + 折叠空白。
func cleanText(s string) string {
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
}

// ── 网页正文抽取 ──

var (
	scriptRe  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe   = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	commentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
)

// maxExtractBody 抓取上限 2MB；maxExtractChars 正文截断 4000 字符。
const (
	maxExtractBody  = 2 << 20
	maxExtractChars = 4000
)

// extractText GET 网页并抽取可读正文（去脚本/样式/标签，截断 4000 字符）。
func (p *Plugin) extractText(ctx context.Context, rawURL string) (string, error) {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return "", fmt.Errorf("url 必须是 http(s) 链接")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", realisticUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("抓取失败: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxExtractBody))
	if err != nil {
		return "", err
	}
	text := string(body)
	text = commentRe.ReplaceAllString(text, " ")
	text = scriptRe.ReplaceAllString(text, " ")
	text = styleRe.ReplaceAllString(text, " ")
	text = cleanText(text)
	runes := []rune(text)
	if len(runes) > maxExtractChars {
		text = string(runes[:maxExtractChars]) + "…"
	}
	if text == "" {
		return "", fmt.Errorf("页面无可抽取正文")
	}
	return text, nil
}

// ── 公共小工具 ──

// do 发请求并读全量 body（上限 2MB），非 200 视为错误。
func (p *Plugin) do(req *http.Request) ([]byte, error) {
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxExtractBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateRunes(string(body), 200))
	}
	return body, nil
}

// httpClient 延迟构造 10s 超时的共享 client。
func (p *Plugin) httpClient() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
