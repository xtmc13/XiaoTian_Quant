package adapter

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/config"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── IBKR Client Portal Web API ─────────────────────────────────
//
// IBKR（Interactive Brokers）通过本地 Client Portal Web API 网关接入：
// 用户自行运行 IB Gateway 或 Client Portal（默认 https://localhost:5000），
// 网关用会话 cookie 认证——浏览器登录一次后本会话内有效。认证过期时尝试
// POST /iserver/reauthenticate 恢复一次，不行则提示重新浏览器登录。
//
// 合约以 conid（数字）标识，symbol → conid 通过 /iserver/secdef/search
// 解析并缓存（股票/外汇/加密货币都走这一个搜索端点）。

const (
	ibkrDefaultHost    = "localhost"
	ibkrDefaultPort    = 5000
	ibkrRequestTimeout = 15 * time.Second
	ibkrAuthCacheTTL   = 30 * time.Second
	ibkrKeepAliveEvery = 60 * time.Second
	ibkrMaxRetries     = 3 // 429 额外重试次数（指数退避）
)

// IBKRConfig IBKR Client Portal 连接配置。
type IBKRConfig struct {
	Host       string        // 网关地址，默认 localhost
	Port       int           // 网关端口，默认 5000
	AccountID  string        // 账户 id（如 DU123456）；留空自动取 /portfolio/accounts 第一个
	PaperHost  string        // paper 网关地址，默认同 Host（指向 paper 账户端口）
	PaperPort  int           // paper 网关端口，默认同 Port
	Paper      bool          // 下单/撤单/订单查询走 paper 网关
	CACertPath string        // 自签根证书 PEM 路径；为空则 InsecureSkipVerify（Client Portal 默认自签）
	Timeout    time.Duration // 单请求超时，默认 15s
}

// IBKRAdapter provides stock/forex/crypto trading via Interactive
// Brokers Client Portal Web API (local gateway, session-cookie auth).
type IBKRAdapter struct {
	cfg        IBKRConfig
	baseURL    string // 主网关 https://host:port/v1/api
	paperURL   string // paper 网关 https://paperHost:paperPort/v1/api
	httpClient *http.Client

	authMu        sync.RWMutex
	authenticated bool
	lastAuthCheck time.Time
	reauthMu      sync.Mutex // 重认证单飞，避免并发请求风暴

	acctMu    sync.Mutex
	accountID string

	conidMu  sync.Mutex
	conids   map[string]int64 // 上层 symbol（大写）→ conid
	conidSym map[int64]string // conid → 上层 symbol（反向显示用）

	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewIBKRAdapter creates an IBKR adapter for the given Client Portal config.
func NewIBKRAdapter(cfg IBKRConfig) *IBKRAdapter {
	if cfg.Host == "" {
		cfg.Host = ibkrDefaultHost
	}
	if cfg.Port <= 0 {
		cfg.Port = ibkrDefaultPort
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = ibkrRequestTimeout
	}
	tlsCfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // 本地自签网关，可按 CACertPath 收紧
	if cfg.CACertPath != "" {
		if pem, err := os.ReadFile(cfg.CACertPath); err == nil {
			pool := x509.NewCertPool()
			if pool.AppendCertsFromPEM(pem) {
				tlsCfg = &tls.Config{RootCAs: pool}
			} else {
				log.Printf("[IBKR] ca_cert %s 无有效证书，回退 InsecureSkipVerify", cfg.CACertPath)
			}
		} else {
			log.Printf("[IBKR] ca_cert %s 读取失败: %v，回退 InsecureSkipVerify", cfg.CACertPath, err)
		}
	}
	jar, _ := cookiejar.New(nil)
	a := &IBKRAdapter{
		cfg:        cfg,
		baseURL:    fmt.Sprintf("https://%s:%d/v1/api", cfg.Host, cfg.Port),
		paperURL:   fmt.Sprintf("https://%s:%d/v1/api", firstNonEmpty(cfg.PaperHost, cfg.Host), firstNonZero(cfg.PaperPort, cfg.Port)),
		httpClient: &http.Client{Timeout: cfg.Timeout, Jar: jar, Transport: &http.Transport{TLSClientConfig: tlsCfg}},
		conids:     make(map[string]int64),
		conidSym:   make(map[int64]string),
		stopCh:     make(chan struct{}),
	}
	return a
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonZero(vals ...int) int {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

func (a *IBKRAdapter) Name() string { return "ibkr" }

// Start 启动会话维持协程：周期性 GET /iserver/auth/status 保持会话活跃。
func (a *IBKRAdapter) Start() error {
	go func() {
		ticker := time.NewTicker(ibkrKeepAliveEvery)
		defer ticker.Stop()
		for {
			select {
			case <-a.stopCh:
				return
			case <-ticker.C:
				st, err := a.authStatus()
				if err != nil {
					log.Printf("[IBKR] auth/status keep-alive 失败: %v", err)
					continue
				}
				a.setAuthState(st.Authenticated)
			}
		}
	}()
	return nil
}

// Stop 停止会话维持协程（IBKR 会话由 Client Portal 管理，无需登出调用）。
func (a *IBKRAdapter) Stop() error {
	a.stopOnce.Do(func() { close(a.stopCh) })
	return nil
}

// IsConnected 报告最近一次 auth/status 的认证状态。
func (a *IBKRAdapter) IsConnected() bool {
	a.authMu.RLock()
	defer a.authMu.RUnlock()
	return a.authenticated
}

func (a *IBKRAdapter) setAuthState(ok bool) {
	a.authMu.Lock()
	a.authenticated = ok
	a.lastAuthCheck = time.Now()
	a.authMu.Unlock()
}

// Ping 连通性测试：只探活 auth/status，不触发重认证（供设置页连接测试用）。
func (a *IBKRAdapter) Ping() error {
	_, err := a.authStatus()
	return err
}

// ── 认证会话管理 ────────────────────────────────────────────────

type ibkrAuthStatus struct {
	Authenticated bool   `json:"authenticated"`
	Connected     bool   `json:"connected"`
	Competing     bool   `json:"competing"`
	Message       string `json:"message"`
}

// authStatus GET /iserver/auth/status，返回原始认证状态（不触发重认证）。
func (a *IBKRAdapter) authStatus() (ibkrAuthStatus, error) {
	raw, status, err := a.do("GET", a.baseURL+"/iserver/auth/status", nil, nil)
	if err != nil {
		return ibkrAuthStatus{}, err
	}
	if status != http.StatusOK {
		return ibkrAuthStatus{}, fmt.Errorf("ibkr auth/status: HTTP %d: %s", status, truncateStr(string(raw), 200))
	}
	var st ibkrAuthStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		return ibkrAuthStatus{}, fmt.Errorf("ibkr auth/status parse: %w", err)
	}
	a.setAuthState(st.Authenticated)
	return st, nil
}

// ensureAuth 私有端点调用前的懒认证检查（30s 缓存）。
func (a *IBKRAdapter) ensureAuth() error {
	a.authMu.RLock()
	ok := a.authenticated && time.Since(a.lastAuthCheck) < ibkrAuthCacheTTL
	a.authMu.RUnlock()
	if ok {
		return nil
	}
	return a.reauthenticate()
}

// reauthenticate 会话失效时尝试 POST /iserver/reauthenticate 恢复一次。
// 单飞：并发请求共享一次重认证。仍失败则提示浏览器重新登录。
func (a *IBKRAdapter) reauthenticate() error {
	a.reauthMu.Lock()
	defer a.reauthMu.Unlock()

	// 别的请求可能刚重认证成功，先看缓存。
	a.authMu.RLock()
	ok := a.authenticated && time.Since(a.lastAuthCheck) < ibkrAuthCacheTTL
	a.authMu.RUnlock()
	if ok {
		return nil
	}

	if st, err := a.authStatus(); err == nil && st.Authenticated {
		return nil
	}

	raw, status, err := a.do("POST", a.baseURL+"/iserver/reauthenticate", nil, map[string]any{})
	if err != nil {
		return fmt.Errorf("ibkr reauthenticate: %w", err)
	}
	if status != http.StatusOK {
		return a.loginHintErr(fmt.Errorf("HTTP %d: %s", status, truncateStr(string(raw), 200)))
	}
	st, err := a.authStatus()
	if err != nil {
		return a.loginHintErr(err)
	}
	if !st.Authenticated {
		return a.loginHintErr(fmt.Errorf("authenticated=false (%s)", firstNonEmpty(st.Message, "no message")))
	}
	return nil
}

func (a *IBKRAdapter) loginHintErr(cause error) error {
	return fmt.Errorf("IBKR Client Portal 会话未认证（%v）。请在浏览器打开 %s 完成 IB 登录后重试",
		cause, fmt.Sprintf("https://%s:%d", a.cfg.Host, a.cfg.Port))
}

// isAuthFailure 判断响应是否认证类失败（可经重认证恢复）。
func isAuthFailure(status int, raw []byte) bool {
	if status == http.StatusUnauthorized {
		return true
	}
	msg := strings.ToLower(string(raw))
	return strings.Contains(msg, "not_auth") ||
		strings.Contains(msg, "not authenticated") ||
		strings.Contains(msg, "please login") ||
		strings.Contains(msg, "access denied")
}

// ── HTTP 底层 ─────────────────────────────────────────────────

// do 发起 HTTP 请求：429 最多重试 3 次（指数退避 300ms 起步），返回原始响应。
func (a *IBKRAdapter) do(method, urlStr string, query url.Values, body any) ([]byte, int, error) {
	var bodyBytes []byte
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("ibkr encode body: %w", err)
		}
		bodyBytes = data
	}
	if query != nil {
		urlStr += "?" + query.Encode()
	}

	var lastErr error
	for attempt := 0; attempt <= ibkrMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(300*(1<<(attempt-1))) * time.Millisecond
			time.Sleep(backoff)
		}
		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = strings.NewReader(string(bodyBytes)) // 每次重试重建，reader 只能读一次
		}
		req, err := http.NewRequest(method, urlStr, bodyReader)
		if err != nil {
			return nil, 0, err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")

		resp, err := a.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("ibkr %s %s: %w", method, urlStr, err)
			// 网络错误不视为限流，直接失败
			return nil, 0, lastErr
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			return nil, resp.StatusCode, fmt.Errorf("ibkr read body: %w", err)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("ibkr %s %s: HTTP 429 throttled (attempt %d)", method, urlStr, attempt+1)
			continue
		}
		return raw, resp.StatusCode, nil
	}
	return nil, http.StatusTooManyRequests, lastErr
}

// ibkrAPIError 从 IBKR 错误响应 {"error": "..."} 提取信息。
func ibkrAPIError(raw []byte) string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	if e, ok := m["error"].(string); ok && e != "" {
		return e
	}
	if e, ok := m["errorMsg"].(string); ok && e != "" {
		return e
	}
	if msgs, ok := m["message"].([]any); ok && len(msgs) > 0 {
		parts := make([]string, 0, len(msgs))
		for _, msg := range msgs {
			if s, ok := msg.(string); ok {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}
	return ""
}

// api 业务层请求：认证失败时重认证一次并重试；其余错误按 IBKR 格式解析。
// paper=true 时走 paper 网关（下单类端点）。
func (a *IBKRAdapter) api(method, path string, query url.Values, body any, paper bool) ([]byte, error) {
	base := a.baseURL
	if paper && a.cfg.Paper {
		base = a.paperURL
	}
	urlStr := base + path

	raw, status, err := a.do(method, urlStr, query, body)
	if err == nil && isAuthFailure(status, raw) {
		// 会话过期：重认证一次后重试
		if rerr := a.reauthenticate(); rerr != nil {
			return nil, rerr
		}
		raw, status, err = a.do(method, urlStr, query, body)
	}
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		if apiErr := ibkrAPIError(raw); apiErr != "" {
			return nil, fmt.Errorf("ibkr %s %s: HTTP %d: %s", method, path, status, apiErr)
		}
		return nil, fmt.Errorf("ibkr %s %s: HTTP %d: %s", method, path, status, truncateStr(string(raw), 200))
	}
	if apiErr := ibkrAPIError(raw); apiErr != "" {
		return nil, fmt.Errorf("ibkr %s %s: %s", method, path, apiErr)
	}
	return raw, nil
}

// ── 账户 / 合约解析 ───────────────────────────────────────────

// account 返回账户 id：配置的 AccountID 优先，否则取 /portfolio/accounts 第一个并缓存。
func (a *IBKRAdapter) account() (string, error) {
	a.acctMu.Lock()
	defer a.acctMu.Unlock()
	if a.accountID != "" {
		return a.accountID, nil
	}
	if a.cfg.AccountID != "" {
		a.accountID = a.cfg.AccountID
		return a.accountID, nil
	}
	raw, err := a.api("GET", "/portfolio/accounts", nil, nil, false)
	if err != nil {
		return "", err
	}
	var res struct {
		Accounts []string `json:"accounts"`
		Error    string   `json:"error"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("ibkr parse accounts: %w", err)
	}
	if len(res.Accounts) == 0 {
		if res.Error != "" {
			return "", fmt.Errorf("ibkr accounts: %s", res.Error)
		}
		return "", fmt.Errorf("ibkr: no accounts in /portfolio/accounts")
	}
	a.accountID = res.Accounts[0]
	return a.accountID, nil
}

// ibkrFiatCurrencies 常见法币代码（三位），用于区分外汇对与加密货币。
var ibkrFiatCurrencies = map[string]bool{
	"EUR": true, "GBP": true, "USD": true, "JPY": true, "CHF": true,
	"AUD": true, "NZD": true, "CAD": true, "SEK": true, "NOK": true,
	"HKD": true, "SGD": true, "MXN": true, "ZAR": true, "DKK": true,
}

// ibkrSearchSymbol 把上层 symbol 转成 secdef 搜索词：
// BTCUSDT/BTCUSD → BTC（IBKR 加密货币以 USD 报价），EURUSD → EUR.USD（外汇），
// 股票代码原样传递。
func ibkrSearchSymbol(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	s = strings.NewReplacer("-", "", "/", "", "_", "").Replace(s)
	// 外汇式六位字母代码 EURUSD → EUR.USD（前后都是法币）
	if len(s) == 6 && isAllLetters(s) && ibkrFiatCurrencies[s[:3]] && ibkrFiatCurrencies[s[3:]] {
		return s[:3] + "." + s[3:]
	}
	for _, q := range []string{"USDT", "USDC", "USD"} {
		if strings.HasSuffix(s, q) && len(s) > len(q) {
			return strings.TrimSuffix(s, q)
		}
	}
	return s
}

func isAllLetters(s string) bool {
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return len(s) > 0
}

// SearchSymbols 合约搜索：/iserver/secdef/search。让任意 symbol（股票/外汇/币）
// 可解析成 conid。secType 可选过滤（STK/CASH/CRYPTO/FUT/CFD/OPT）。
func (a *IBKRAdapter) SearchSymbols(symbol, secType string) ([]map[string]any, error) {
	q := url.Values{}
	q.Set("symbol", ibkrSearchSymbol(symbol))
	if secType != "" {
		q.Set("secType", secType)
	}
	raw, err := a.api("GET", "/iserver/secdef/search", q, nil, false)
	if err != nil {
		return nil, err
	}
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("ibkr parse secdef search: %w", err)
	}
	out := make([]map[string]any, 0, len(arr))
	for _, m := range arr {
		out = append(out, map[string]any{
			"conid":       m["conid"],
			"symbol":      m["symbol"],
			"name":        firstAnyStr(m, "companyName", "companyShortName", "name"),
			"sec_type":    firstAnyStr(m, "securityType", "secType"),
			"exchange":    firstAnyStr(m, "listingExchange", "exchange"),
			"currency":    m["currency"],
			"asset_class": m["assetClass"],
		})
	}
	return out, nil
}

func firstAnyStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// conid 解析并缓存 symbol → conid。优先级：精确代码匹配 > 偏好 secType > 主流交易所。
func (a *IBKRAdapter) conid(symbol string) (int64, error) {
	key := strings.ToUpper(strings.TrimSpace(symbol))
	a.conidMu.Lock()
	if c, ok := a.conids[key]; ok {
		a.conidMu.Unlock()
		return c, nil
	}
	a.conidMu.Unlock()

	matches, err := a.SearchSymbols(symbol, "")
	if err != nil {
		return 0, err
	}
	if len(matches) == 0 {
		return 0, fmt.Errorf("ibkr: symbol %s 未找到合约（secdef/search 无结果）", symbol)
	}

	want := ibkrSearchSymbol(symbol)
	prefTypes := map[string]int{"STK": 0, "CRYPTO": 1, "CFD": 2, "FUT": 3, "CASH": 4}
	prefExch := map[string]int{"NASDAQ": 0, "NYSE": 1, "ARCA": 2, "AMEX": 3, "SMART": 4, "PAXOS": 5, "IDEALPRO": 6}
	bestIdx, bestScore := -1, 1<<30
	for i, m := range matches {
		score := 100
		if strings.EqualFold(getString(m, "symbol", ""), want) {
			score = 0
		}
		if t, ok := m["sec_type"].(string); ok {
			if p, ok2 := prefTypes[strings.ToUpper(t)]; ok2 {
				score += p
			} else {
				score += 50
			}
		}
		if e, ok := m["exchange"].(string); ok {
			if p, ok2 := prefExch[strings.ToUpper(e)]; ok2 {
				score += p * 10
			} else {
				score += 200
			}
		}
		if bestIdx < 0 || score < bestScore {
			bestIdx, bestScore = i, score
		}
	}

	cf, ok := matches[bestIdx]["conid"].(float64)
	if !ok {
		return 0, fmt.Errorf("ibkr: secdef/search 结果缺 conid (symbol=%s)", symbol)
	}
	c := int64(cf)

	a.conidMu.Lock()
	a.conids[key] = c
	a.conidSym[c] = key
	a.conidMu.Unlock()
	return c, nil
}

// symbolForConid 反向把 conid 显示回上层 symbol（缓存未命中时原样转字符串）。
func (a *IBKRAdapter) symbolForConid(conid any) string {
	var cf float64
	switch v := conid.(type) {
	case float64:
		cf = v
	case string:
		cf = parseFloatSafe(v)
	default:
		return ""
	}
	if cf == 0 {
		return ""
	}
	a.conidMu.Lock()
	defer a.conidMu.Unlock()
	if s, ok := a.conidSym[int64(cf)]; ok {
		return s
	}
	return fmt.Sprintf("%d", int64(cf))
}

// ── 行情 ─────────────────────────────────────────────────────

// snapshotFields IBKR 行情字段 id：31=最新价 84=买价 85=卖价 86=成交量 88=开盘价 7295=标记价。
const ibkrSnapshotFields = "31,84,85,86,88,7295"

// snapshot GET /iserver/marketdata/snapshot，返回指定 conid 的原始快照 map。
func (a *IBKRAdapter) snapshot(conid int64) (map[string]any, error) {
	q := url.Values{}
	q.Set("conids", strconv.FormatInt(conid, 10))
	q.Set("fields", ibkrSnapshotFields)
	raw, err := a.api("GET", "/iserver/marketdata/snapshot", q, nil, false)
	if err != nil {
		return nil, err
	}
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("ibkr parse snapshot: %w", err)
	}
	for _, m := range arr {
		if parseFloatSafe(m["conid"]) == float64(conid) {
			return m, nil
		}
	}
	if len(arr) > 0 {
		return arr[0], nil
	}
	return nil, fmt.Errorf("ibkr: snapshot 无数据 (conid=%d)", conid)
}

// GetTicker 最新价/买卖价/成交量（symbol → conid → snapshot）。
func (a *IBKRAdapter) GetTicker(symbol string) (map[string]any, error) {
	c, err := a.conid(symbol)
	if err != nil {
		return nil, err
	}
	snap, err := a.snapshot(c)
	if err != nil {
		return nil, err
	}
	last := parseFloatSafe(snap["31"])
	if last == 0 {
		last = parseFloatSafe(snap["7295"]) // 标记价兜底
	}
	return map[string]any{
		"symbol": symbol,
		"last":   last,
		"bid":    parseFloatSafe(snap["84"]),
		"ask":    parseFloatSafe(snap["85"]),
		"volume": parseFloatSafe(snap["86"]),
		"open":   parseFloatSafe(snap["88"]),
		"conid":  c,
	}, nil
}

// ibkrBarPeriod 把上层 K 线周期映射成 Client Portal history 端点的 period/bar。
func ibkrBarPeriod(interval string) (period, bar string) {
	switch strings.ToLower(interval) {
	case "1m":
		return "1d", "1min"
	case "3m":
		return "1d", "3min"
	case "5m":
		return "1d", "5min"
	case "15m":
		return "1w", "15min"
	case "30m":
		return "1w", "30min"
	case "1h":
		return "1m", "1h"
	case "2h":
		return "1m", "2h"
	case "4h":
		return "1m", "4h"
	case "6h":
		return "1m", "6h"
	case "8h":
		return "3m", "8h"
	case "1d":
		return "1y", "1d"
	case "1w":
		return "1y", "1d"
	default:
		return "1d", "5min"
	}
}

func ibkrHistoryPeriodForSpan(span time.Duration) string {
	switch {
	case span <= 24*time.Hour:
		return "1d"
	case span <= 7*24*time.Hour:
		return "1w"
	case span <= 31*24*time.Hour:
		return "1m"
	case span <= 93*24*time.Hour:
		return "3m"
	case span <= 186*24*time.Hour:
		return "6m"
	default:
		return "1y"
	}
}

// ibkrHistory GET /iserver/marketdata/history → [t, o, h, l, c, v]。
func (a *IBKRAdapter) ibkrHistory(symbol, period, bar string) ([][]any, error) {
	c, err := a.conid(symbol)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("conid", strconv.FormatInt(c, 10))
	q.Set("period", period)
	q.Set("bar", bar)
	q.Set("outsideRth", "false")
	q.Set("barType", "Last")
	raw, err := a.api("GET", "/iserver/marketdata/history", q, nil, false)
	if err != nil {
		return nil, err
	}
	var res struct {
		Data  []map[string]any `json:"data"`
		Error string           `json:"error"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("ibkr parse history: %w", err)
	}
	if res.Error != "" {
		return nil, fmt.Errorf("ibkr history: %s", res.Error)
	}
	out := make([][]any, 0, len(res.Data))
	for _, d := range res.Data {
		out = append(out, []any{
			ibkrEpochMs(parseFloatSafe(d["t"])),
			parseFloatSafe(d["o"]),
			parseFloatSafe(d["h"]),
			parseFloatSafe(d["l"]),
			parseFloatSafe(d["c"]),
			parseFloatSafe(d["v"]),
		})
	}
	return out, nil
}

// ibkrEpochMs IBKR 历史 K 线 t 字段归一到毫秒（秒级时间戳 ×1000）。
func ibkrEpochMs(t float64) int64 {
	if t <= 0 {
		return 0
	}
	if t > 1e11 { // 已是毫秒
		return int64(t)
	}
	return int64(t * 1000)
}

// GetKlines 历史 K 线（symbol → conid → marketdata/history）。
func (a *IBKRAdapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	period, bar := ibkrBarPeriod(interval)
	rows, err := a.ibkrHistory(symbol, period, bar)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	return rows, nil
}

// GetKlinesRange 区间历史 K 线（按区间跨度选 period，再过滤 [startMs, endMs]）。
func (a *IBKRAdapter) GetKlinesRange(symbol, interval string, startMs, endMs int64, limit int) ([][]any, error) {
	_, bar := ibkrBarPeriod(interval)
	period := "1y"
	if startMs > 0 && endMs > startMs {
		period = ibkrHistoryPeriodForSpan(time.Duration(endMs-startMs) * time.Millisecond)
	}
	rows, err := a.ibkrHistory(symbol, period, bar)
	if err != nil {
		return nil, err
	}
	filtered := make([][]any, 0, len(rows))
	for _, r := range rows {
		t := parseFloatSafe(r[0])
		if startMs > 0 && t < float64(startMs) {
			continue
		}
		if endMs > 0 && t > float64(endMs) {
			continue
		}
		filtered = append(filtered, r)
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return filtered, nil
}

// ── 交易 ─────────────────────────────────────────────────────

// ibkrOrderType 上层 orderType → IBKR 订单类型。STOP 用传入价作辅助价（触发价）。
func ibkrOrderType(orderType string) string {
	switch strings.ToUpper(orderType) {
	case "LIMIT", "LMT":
		return "LMT"
	case "STOP", "STP", "STOP_LOSS":
		return "STP"
	default:
		return "MKT"
	}
}

// PlaceOrder 下单：/iserver/account/{accountId}/orders（paper 配置时走 paper 网关）。
// 支持 LIMIT/MARKET/STOP，side 接受 BUY/SELL（大小写不敏感）。
func (a *IBKRAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	if err := a.ensureAuth(); err != nil {
		return nil, err
	}
	c, err := a.conid(symbol)
	if err != nil {
		return nil, err
	}
	acct, err := a.account()
	if err != nil {
		return nil, err
	}

	ot := ibkrOrderType(orderType)
	// 下单前本地规整精度（IBKR 无公开 symbol 规则 REST，保守近似见
	// ibkr_precision.go）；碎尘量/非法精度本地拒绝，不发注定被拒的单。
	qtyStr, priceStr, perr := normalizeIBKROrder(symbol, orderType, price, quantity)
	if perr != nil {
		return nil, perr
	}
	order := map[string]any{
		"conid":                     c,
		"side":                      ibkrSide(side),
		"quantity":                  qtyStr,
		"orderType":                 ot,
		"tif":                       "DAY",
		"outsideRegularTradingHour": true,
	}
	switch ot {
	case "LMT":
		order["price"] = priceStr
	case "STP":
		order["auxPrice"] = priceStr
	}

	body := map[string]any{"orders": []any{order}}
	raw, err := a.api("POST", "/iserver/account/"+acct+"/orders", nil, body, true)
	if err != nil {
		return nil, err
	}

	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil || len(arr) == 0 {
		// 部分错误是对象格式 {"error": "..."}
		if apiErr := ibkrAPIError(raw); apiErr != "" {
			return nil, fmt.Errorf("ibkr place order: %s", apiErr)
		}
		return nil, fmt.Errorf("ibkr place order: unexpected response: %.200s", string(raw))
	}

	first := arr[0]
	if apiErr := getString(first, "error", ""); apiErr != "" {
		return nil, fmt.Errorf("ibkr place order: %s", apiErr)
	}
	orderID := firstAnyStr(first, "id", "order_id")
	if orderID == "" {
		// IBKR 返回 message（订单确认问题）但无 id：视为拒绝，不自动确认
		if msg := firstAnyStr(first, "message"); msg != "" {
			return nil, fmt.Errorf("ibkr place order rejected: %s", msg)
		}
		if msgs, ok := first["message"].([]any); ok && len(msgs) > 0 {
			parts := make([]string, 0, len(msgs))
			for _, m := range msgs {
				if s, ok := m.(string); ok {
					parts = append(parts, s)
				}
			}
			if len(parts) > 0 {
				return nil, fmt.Errorf("ibkr place order rejected: %s", strings.Join(parts, "; "))
			}
		}
		return nil, fmt.Errorf("ibkr place order: response missing order id: %.200s", string(raw))
	}

	return map[string]any{
		"order_id": orderID,
		"symbol":   strings.ToUpper(symbol),
		"side":     strings.ToUpper(side),
		"type":     strings.ToUpper(orderType),
		"qty":      quantity,
		"price":    price,
		"conid":    c,
		"status":   "SUBMITTED",
	}, nil
}

func ibkrSide(side string) string {
	if strings.EqualFold(strings.TrimSpace(side), "SELL") {
		return "SELL"
	}
	return "BUY"
}

// CancelOrder 撤单：DELETE /iserver/account/{accountId}/order/{orderId}。
func (a *IBKRAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	if err := a.ensureAuth(); err != nil {
		return nil, err
	}
	acct, err := a.account()
	if err != nil {
		return nil, err
	}
	raw, err := a.api("DELETE", "/iserver/account/"+acct+"/order/"+url.PathEscape(orderID), nil, nil, true)
	if err != nil {
		return nil, err
	}
	res := map[string]any{"order_id": orderID, "status": "CANCELLED"}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err == nil {
		if v := firstAnyStr(m, "order_id", "orderId"); v != "" {
			res["order_id"] = v
		}
		if v := firstAnyStr(m, "msg", "message"); v != "" {
			res["msg"] = v
		}
	}
	return res, nil
}

// ibkrStatusNormalize IBKR 订单状态 → 归一化状态。
func ibkrStatusNormalize(s string) string {
	switch strings.ToUpper(s) {
	case "SUBMITTED", "PRESUBMITTED", "PENDINGSUBMIT":
		return "NEW"
	case "FILLED":
		return "FILLED"
	case "CANCELLED", "PENDINGCANCEL", "APICANCEL", "APICANCELLED":
		return "CANCELLED"
	case "INACTIVE", "EXPIRED":
		return "EXPIRED"
	default:
		return strings.ToUpper(s)
	}
}

// normalizeIBKROrder 归一化 /iserver/account/orders 的订单对象。
func (a *IBKRAdapter) normalizeIBKROrder(o map[string]any) map[string]any {
	qty := parseFloatSafe(o["quantity"])
	filled := parseFloatSafe(o["filledQuantity"])
	if filled == 0 {
		if cf, ok := o["cumFill"].(map[string]any); ok {
			filled = parseFloatSafe(cf["qty"])
		}
	}
	symbol := firstAnyStr(o, "ticker", "contractDesc", "symbol")
	if symbol == "" {
		symbol = a.symbolForConid(o["conid"])
	}
	return map[string]any{
		"order_id":   firstAnyStr(o, "orderId", "order_id", "id"),
		"symbol":     symbol,
		"conid":      o["conid"],
		"side":       strings.ToUpper(getString(o, "side", "")),
		"type":       getString(o, "orderType", ""),
		"qty":        qty,
		"filled":     filled,
		"price":      parseFloatSafe(o["price"]),
		"aux_price":  parseFloatSafe(o["auxPrice"]),
		"status":     ibkrStatusNormalize(getString(o, "status", "")),
		"raw_status": getString(o, "status", ""),
		"time":       ibkrEpochMs(parseFloatSafe(o["lastExecutionTime"])),
	}
}

// GetOpenOrders 订单查询：GET /iserver/account/orders（全量活动订单，按需按 symbol 过滤）。
func (a *IBKRAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	if err := a.ensureAuth(); err != nil {
		return nil, err
	}
	raw, err := a.api("GET", "/iserver/account/orders", nil, nil, true)
	if err != nil {
		return nil, err
	}

	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		// 可能是 {"orders": [...]} 包装
		var wrapped struct {
			Orders []map[string]any `json:"orders"`
		}
		if werr := json.Unmarshal(raw, &wrapped); werr != nil || len(wrapped.Orders) == 0 {
			return nil, fmt.Errorf("ibkr parse orders: %w (body: %.200s)", err, string(raw))
		}
		arr = wrapped.Orders
	}

	orders := make([]map[string]any, 0, len(arr))
	wantSym := strings.ToUpper(strings.TrimSpace(symbol))
	for _, o := range arr {
		n := a.normalizeIBKROrder(o)
		if wantSym != "" && !strings.EqualFold(getString(n, "symbol", ""), wantSym) {
			continue
		}
		orders = append(orders, n)
	}
	return orders, nil
}

// ── 成交 ─────────────────────────────────────────────────────

// GetTrades 成交查询：GET /iserver/account/trades（paper 配置时走 paper 网关）。
// IBKR 成交 side 为 BOT/SLD，归一化为 BUY/SELL。
func (a *IBKRAdapter) GetTrades() ([]map[string]any, error) {
	if err := a.ensureAuth(); err != nil {
		return nil, err
	}
	raw, err := a.api("GET", "/iserver/account/trades", nil, nil, true)
	if err != nil {
		return nil, err
	}
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("ibkr parse trades: %w (body: %.200s)", err, string(raw))
	}
	trades := make([]map[string]any, 0, len(arr))
	for _, m := range arr {
		side := strings.ToUpper(getString(m, "side", ""))
		switch side {
		case "BOT":
			side = "BUY"
		case "SLD":
			side = "SELL"
		}
		symbol := firstAnyStr(m, "symbol", "contractDesc")
		if symbol == "" {
			symbol = a.symbolForConid(m["conid"])
		}
		ts := int64Of(m["trade_time_r"])
		if ts == 0 {
			ts = ibkrEpochMs(parseFloatSafe(m["trade_time"]))
		}
		trades = append(trades, map[string]any{
			"trade_id": firstAnyStr(m, "execution_id", "exec_id", "id"),
			"order_id": firstAnyStr(m, "order_ref", "order_id"),
			"symbol":   symbol,
			"conid":    m["conid"],
			"side":     side,
			"price":    parseFloatSafe(m["price"]),
			"qty":      parseFloatSafe(m["quantity"]),
			"time":     ts,
			"exchange": getString(m, "exchange", ""),
		})
	}
	return trades, nil
}

// GetOrderTrades 指定订单的成交明细（/iserver/account/trades 按 order_ref 过滤）。
// 签名与 adapter 层惯例对齐（同 binance_reconcile.go），供对账窄接口使用。
func (a *IBKRAdapter) GetOrderTrades(symbol, orderID string) ([]AccountTrade, error) {
	all, err := a.GetTrades()
	if err != nil {
		return nil, err
	}
	out := make([]AccountTrade, 0, len(all))
	for _, t := range all {
		if !strings.EqualFold(getString(t, "order_id", ""), orderID) {
			continue
		}
		out = append(out, AccountTrade{
			ID:       getString(t, "trade_id", ""),
			OrderID:  orderID,
			Symbol:   getString(t, "symbol", symbol),
			Side:     getString(t, "side", ""),
			Price:    parseFloatSafe(t["price"]),
			Quantity: parseFloatSafe(t["qty"]),
			Time:     int64Of(t["time"]),
		})
	}
	return out, nil
}

// QueryOrderStatus 订单状态查询：GET /iserver/account/order/status/{orderId}。
// 返回归一化状态（NEW/PARTIALLY_FILLED/FILLED/CANCELLED/EXPIRED）。
func (a *IBKRAdapter) QueryOrderStatus(symbol, orderID string) (OrderStatusInfo, error) {
	if err := a.ensureAuth(); err != nil {
		return OrderStatusInfo{}, err
	}
	raw, err := a.api("GET", "/iserver/account/order/status/"+url.PathEscape(orderID), nil, nil, true)
	if err != nil {
		return OrderStatusInfo{}, err
	}
	var r struct {
		OrderID   string `json:"order_id"`
		Status    string `json:"status"`
		FilledQty string `json:"filled_quantity"`
		AvgPrice  string `json:"avg_price"`
		Remaining string `json:"remaining_quantity"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return OrderStatusInfo{}, fmt.Errorf("ibkr parse order status: %w", err)
	}
	if r.Error != "" {
		return OrderStatusInfo{}, fmt.Errorf("ibkr order status: %s", r.Error)
	}
	status := ibkrStatusNormalize(r.Status)
	filled := parseQty(r.FilledQty)
	if status == "NEW" && filled > 0 {
		status = "PARTIALLY_FILLED"
	}
	return OrderStatusInfo{
		Status:    status,
		FilledQty: filled,
		AvgPrice:  parseQty(r.AvgPrice),
	}, nil
}

// ── 持仓 / 余额 ──────────────────────────────────────────────

// GetPositions 持仓：GET /portfolio/{accountId}/positions。
func (a *IBKRAdapter) GetPositions() ([]map[string]any, error) {
	if err := a.ensureAuth(); err != nil {
		return nil, err
	}
	acct, err := a.account()
	if err != nil {
		return nil, err
	}
	raw, err := a.api("GET", "/portfolio/"+acct+"/positions", nil, nil, false)
	if err != nil {
		return nil, err
	}
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		var wrapped struct {
			Positions []map[string]any `json:"positions"`
		}
		if werr := json.Unmarshal(raw, &wrapped); werr != nil || len(wrapped.Positions) == 0 {
			return nil, fmt.Errorf("ibkr parse positions: %w (body: %.200s)", err, string(raw))
		}
		arr = wrapped.Positions
	}
	positions := make([]map[string]any, 0, len(arr))
	for _, p := range arr {
		pos := parseFloatSafe(p["position"])
		symbol := firstAnyStr(p, "contractDesc", "ticker", "symbol")
		if symbol == "" {
			symbol = a.symbolForConid(p["conid"])
		}
		side := "LONG"
		if pos < 0 {
			side = "SHORT"
		}
		positions = append(positions, map[string]any{
			"symbol":        symbol,
			"conid":         p["conid"],
			"qty":           pos,
			"entry_price":   parseFloatSafe(p["avgCost"]),
			"current_price": parseFloatSafe(p["marketPrice"]),
			"market_value":  parseFloatSafe(p["marketValue"]),
			"unrealized_pl": parseFloatSafe(p["unrealizedPnl"]),
			"realized_pl":   parseFloatSafe(p["realizedPnl"]),
			"currency":      getString(p, "currency", ""),
			"asset_class":   getString(p, "assetClass", ""),
			"side":          side,
		})
	}
	return positions, nil
}

// ibkrMetricValue /portfolio/{acct}/summary 的指标可能是
// {"value": "...", "asset": "USD"} 对象，也可能直接是字符串/数字。
func ibkrMetricValue(m map[string]any, key string) (float64, string) {
	v, ok := m[key]
	if !ok {
		return 0, ""
	}
	if obj, ok := v.(map[string]any); ok {
		return parseFloatSafe(obj["value"]), getString(obj, "asset", "")
	}
	return parseFloatSafe(v), ""
}

// GetBalance 账户余额/净值：GET /portfolio/{accountId}/summary。
// 返回 [{asset, free, locked, equity}]：free=可用资金，equity=净清算价值。
func (a *IBKRAdapter) GetBalance() ([]map[string]any, error) {
	if err := a.ensureAuth(); err != nil {
		return nil, err
	}
	acct, err := a.account()
	if err != nil {
		return nil, err
	}
	raw, err := a.api("GET", "/portfolio/"+acct+"/summary", nil, nil, false)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("ibkr parse summary: %w", err)
	}

	equity, asset := ibkrMetricValue(m, "netliquidation")
	free, freeAsset := ibkrMetricValue(m, "availablefunds")
	if asset == "" {
		asset = freeAsset
	}
	if asset == "" {
		asset = "USD"
	}
	locked := equity - free
	if locked < 0 {
		locked = 0
	}
	return []map[string]any{
		{"asset": asset, "free": free, "locked": locked, "equity": equity},
	}, nil
}

// ── 配置加载 ─────────────────────────────────────────────────

// LoadIBKRConfig 按 gateway 配置惯例组装 IBKRConfig：
// 默认值 ← config.yaml exchange.ibkr（类型化 schema）← config.yaml exchanges.ibkr
// （store 配置）← 环境变量（IBKR_HOST/IBKR_PORT/IBKR_ACCOUNT_ID/IBKR_PAPER_HOST/
// IBKR_PAPER_PORT/IBKR_PAPER/IBKR_CA_CERT）。
func LoadIBKRConfig() IBKRConfig {
	cfg := IBKRConfig{
		Host:    ibkrDefaultHost,
		Port:    ibkrDefaultPort,
		Timeout: ibkrRequestTimeout,
	}

	// 类型化 schema（internal/config，env 覆盖已在 config.Load 内处理）。
	if g := config.Get(); g != nil && g.Exchange.IBKR.Host != "" {
		typed := g.Exchange.IBKR
		cfg.Host = typed.Host
		if typed.Port > 0 {
			cfg.Port = typed.Port
		}
		cfg.AccountID = typed.AccountID
		cfg.PaperHost = typed.PaperHost
		cfg.PaperPort = typed.PaperPort
		cfg.CACertPath = typed.CACert
	}

	// store 配置（config.yaml exchanges.ibkr，设置页可写）。
	if sc := storeGetStringMap("exchanges", "ibkr"); sc != nil {
		if v := getString(sc, "host", ""); v != "" {
			cfg.Host = v
		}
		if v := sc["port"]; v != nil {
			if n, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(v))); err == nil && n > 0 {
				cfg.Port = n
			}
		}
		if v := getString(sc, "account_id", ""); v != "" {
			cfg.AccountID = v
		}
		if v := getString(sc, "paper_host", ""); v != "" {
			cfg.PaperHost = v
		}
		if v := sc["paper_port"]; v != nil {
			if n, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(v))); err == nil && n > 0 {
				cfg.PaperPort = n
			}
		}
		if v := sc["paper"]; v != nil {
			if b, ok := v.(bool); ok {
				cfg.Paper = b
			}
		}
		if v := getString(sc, "ca_cert", ""); v != "" {
			cfg.CACertPath = v
		}
	}

	// 环境变量最终覆盖。
	if v := os.Getenv("IBKR_HOST"); v != "" {
		cfg.Host = v
	}
	if v := os.Getenv("IBKR_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Port = n
		}
	}
	if v := os.Getenv("IBKR_ACCOUNT_ID"); v != "" {
		cfg.AccountID = v
	}
	if v := os.Getenv("IBKR_PAPER_HOST"); v != "" {
		cfg.PaperHost = v
	}
	if v := os.Getenv("IBKR_PAPER_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.PaperPort = n
		}
	}
	if v := os.Getenv("IBKR_PAPER"); v == "true" {
		cfg.Paper = true
	}
	if v := os.Getenv("IBKR_CA_CERT"); v != "" {
		cfg.CACertPath = v
	}

	return cfg
}

// IBKRConfigured 报告 IBKR 是否已启用（类型化 schema 或 store 配置 enabled=true，
// 或任一 host/account 环境变量显式配置）。IBKR 用本地网关会话认证，无需 API key。
func IBKRConfigured() bool {
	if g := config.Get(); g != nil && g.Exchange.IBKR.Enabled {
		return true
	}
	if sc := storeGetStringMap("exchanges", "ibkr"); sc != nil {
		if b, ok := sc["enabled"].(bool); ok && b {
			return true
		}
	}
	return os.Getenv("IBKR_HOST") != "" || os.Getenv("IBKR_ACCOUNT_ID") != ""
}

// storeGetStringMap 从 store 配置取 exchanges.<name> 子 map（store 未初始化时返回 nil）。
func storeGetStringMap(section, name string) map[string]any {
	cfg := store.GetConfig()
	if cfg == nil {
		return nil
	}
	sec, _ := cfg[section].(map[string]any)
	if sec == nil {
		return nil
	}
	m, _ := sec[name].(map[string]any)
	return m
}
