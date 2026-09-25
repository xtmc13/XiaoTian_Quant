package dataprovider

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ── 配置 ───────────────────────────────────────────────────────

// Config 数据源密钥与端点。密钥只来自环境变量；BaseURLs 供测试指向 httptest。
type Config struct {
	CoinglassAPIKey string // COINGLASS_API_KEY
	FredAPIKey      string // FRED_API_KEY
	HTTPTimeout     time.Duration
	BaseURLs        map[string]string // source name → base URL 覆盖（测试）
}

// LoadEnvConfig 从环境变量装配配置（绝不记录密钥值）。
func LoadEnvConfig() Config {
	return Config{
		CoinglassAPIKey: strings.TrimSpace(os.Getenv("COINGLASS_API_KEY")),
		FredAPIKey:      strings.TrimSpace(os.Getenv("FRED_API_KEY")),
		HTTPTimeout:     15 * time.Second,
	}
}

// Secrets 返回需脱敏的密钥值列表。
func (c Config) Secrets() []string {
	out := []string{}
	if c.CoinglassAPIKey != "" {
		out = append(out, c.CoinglassAPIKey)
	}
	if c.FredAPIKey != "" {
		out = append(out, c.FredAPIKey)
	}
	return out
}

func (c Config) baseURL(name, def string) string {
	if c.BaseURLs != nil {
		if u := c.BaseURLs[name]; u != "" {
			return strings.TrimRight(u, "/")
		}
	}
	return def
}

func (c Config) timeout() time.Duration {
	if c.HTTPTimeout > 0 {
		return c.HTTPTimeout
	}
	return 15 * time.Second
}

// BuildSources 装配全部数据源（顺序即 /sources 输出顺序）。
func BuildSources(cfg Config, httpClient *http.Client) []Source {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.timeout()}
	}
	return []Source{
		newFearGreedSource(cfg, httpClient),
		newCoinglassSource(cfg, httpClient),
		newFredSource(cfg, httpClient),
		newCryptoCompareNewsSource(cfg, httpClient),
		newHeatmapSource(cfg, httpClient),
		newCalendarSource(cfg, httpClient),
	}
}

// ── HTTP 助手 ─────────────────────────────────────────────────

func httpGetJSON(ctx context.Context, client *http.Client, reqURL string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "XiaoTianQuant-Gateway/3.0")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from upstream", resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("parse upstream json: %w", err)
	}
	return nil
}

// ══════════════════════════════════════════════════════════════
// 1. 情绪：Alternative.me Fear & Greed Index（免 key）
// ══════════════════════════════════════════════════════════════

type fearGreedSource struct {
	base   string
	client *http.Client
}

func newFearGreedSource(cfg Config, client *http.Client) *fearGreedSource {
	return &fearGreedSource{base: cfg.baseURL("fear_greed", "https://api.alternative.me"), client: client}
}

func (s *fearGreedSource) Name() string               { return "fear_greed" }
func (s *fearGreedSource) Description() string        { return "Alternative.me 恐惧贪婪指数（0-100）" }
func (s *fearGreedSource) TTL() time.Duration         { return 30 * time.Minute }
func (s *fearGreedSource) MinInterval() time.Duration { return 5 * time.Second }
func (s *fearGreedSource) RequiresKey() bool          { return false }
func (s *fearGreedSource) Configured() bool           { return true }

// FearGreedData 是 sentiment 端点的恐惧贪婪部分。
type FearGreedData struct {
	Value          int              `json:"value"`
	Classification string           `json:"classification"`
	UpdatedAt      int64            `json:"updated_at"`
	History        []FearGreedPoint `json:"history,omitempty"`
}

type FearGreedPoint struct {
	Value          int    `json:"value"`
	Classification string `json:"classification"`
	Timestamp      int64  `json:"timestamp"`
}

func (s *fearGreedSource) Fetch(ctx context.Context) (any, error) {
	var raw struct {
		Data []struct {
			Value               string `json:"value"`
			ValueClassification string `json:"value_classification"`
			Timestamp           string `json:"timestamp"`
		} `json:"data"`
	}
	if err := httpGetJSON(ctx, s.client, s.base+"/fng/?limit=7", nil, &raw); err != nil {
		return nil, err
	}
	if len(raw.Data) == 0 {
		return nil, fmt.Errorf("empty fear&greed payload")
	}
	points := make([]FearGreedPoint, 0, len(raw.Data))
	for _, item := range raw.Data {
		v, _ := strconv.Atoi(item.Value)
		ts, _ := strconv.ParseInt(item.Timestamp, 10, 64)
		points = append(points, FearGreedPoint{Value: v, Classification: item.ValueClassification, Timestamp: ts})
	}
	return &FearGreedData{
		Value:          points[0].Value,
		Classification: points[0].Classification,
		UpdatedAt:      points[0].Timestamp,
		History:        points,
	}, nil
}

// ══════════════════════════════════════════════════════════════
// 2. 情绪：Coinglass 衍生品（资金费率/多空比/爆仓，COINGLASS_API_KEY）
// ══════════════════════════════════════════════════════════════

type coinglassSource struct {
	base   string
	apiKey string
	client *http.Client
}

func newCoinglassSource(cfg Config, client *http.Client) *coinglassSource {
	return &coinglassSource{
		base:   cfg.baseURL("coinglass", "https://open-api.coinglass.com"),
		apiKey: cfg.CoinglassAPIKey,
		client: client,
	}
}

func (s *coinglassSource) Name() string { return "coinglass" }
func (s *coinglassSource) Description() string {
	return "Coinglass 衍生品情绪（资金费率/多空比/爆仓）"
}
func (s *coinglassSource) TTL() time.Duration         { return 5 * time.Minute }
func (s *coinglassSource) MinInterval() time.Duration { return 2 * time.Second }
func (s *coinglassSource) RequiresKey() bool          { return true }
func (s *coinglassSource) Configured() bool           { return s.apiKey != "" }

// CoinglassData 是 sentiment 端点的衍生品部分（未配置 key 时整块缺省）。
type CoinglassData struct {
	Symbol       string             `json:"symbol"`
	Funding      []FundingRateEntry `json:"funding"`
	LongShort    *LongShortRatio    `json:"long_short,omitempty"`
	Liquidations *LiquidationStats  `json:"liquidations,omitempty"`
	UpdatedAt    int64              `json:"updated_at"`
}

type FundingRateEntry struct {
	Exchange string  `json:"exchange"`
	Symbol   string  `json:"symbol"`
	Rate     float64 `json:"rate"`
	RatePct  float64 `json:"rate_pct"`
	Time     int64   `json:"time,omitempty"`
}

type LongShortRatio struct {
	LongPct  float64 `json:"long_pct"`
	ShortPct float64 `json:"short_pct"`
	Ratio    float64 `json:"ratio"`
	Time     int64   `json:"time,omitempty"`
}

type LiquidationStats struct {
	TotalUSD float64 `json:"total_usd"`
	LongUSD  float64 `json:"long_usd"`
	ShortUSD float64 `json:"short_usd"`
	Time     int64   `json:"time,omitempty"`
}

func (s *coinglassSource) Fetch(ctx context.Context) (any, error) {
	out := &CoinglassData{Symbol: "BTC", UpdatedAt: time.Now().Unix()}
	headers := map[string]string{"coinglassSecret": s.apiKey}

	// 资金费率（跨交易所）
	var fundingRaw struct {
		Code string `json:"code"`
		Data []struct {
			ExchangeName string `json:"exchangeName"`
			Symbol       string `json:"symbol"`
			FundingRate  string `json:"fundingRate"`
			Time         int64  `json:"time"`
		} `json:"data"`
	}
	fundingOK := httpGetJSON(ctx, s.client, s.base+"/public/v2/funding?symbol=BTC&time_type=all", headers, &fundingRaw) == nil
	if fundingOK {
		for _, e := range fundingRaw.Data {
			rate, _ := strconv.ParseFloat(e.FundingRate, 64)
			out.Funding = append(out.Funding, FundingRateEntry{
				Exchange: e.ExchangeName, Symbol: e.Symbol, Rate: rate, RatePct: rate * 100, Time: e.Time,
			})
			if len(out.Funding) >= 12 {
				break
			}
		}
	}

	// 多空比
	var lsRaw struct {
		Code string `json:"code"`
		Data []struct {
			LongRate  string `json:"longRate"`
			ShortRate string `json:"shortRate"`
			Timestamp int64  `json:"timestamp"`
		} `json:"data"`
	}
	lsOK := httpGetJSON(ctx, s.client, s.base+"/public/v2/long_short?symbol=BTC&time_type=h1", headers, &lsRaw) == nil
	if lsOK && len(lsRaw.Data) > 0 {
		last := lsRaw.Data[len(lsRaw.Data)-1]
		lp, _ := strconv.ParseFloat(last.LongRate, 64)
		sp, _ := strconv.ParseFloat(last.ShortRate, 64)
		ratio := 0.0
		if sp > 0 {
			ratio = lp / sp
		}
		out.LongShort = &LongShortRatio{LongPct: lp, ShortPct: sp, Ratio: ratio, Time: last.Timestamp}
	}

	// 爆仓
	var liqRaw struct {
		Code string `json:"code"`
		Data []struct {
			BuyVolUsd  string `json:"buyVolUsd"`
			SellVolUsd string `json:"sellVolUsd"`
			VolUsd     string `json:"volUsd"`
			Timestamp  int64  `json:"timestamp"`
		} `json:"data"`
	}
	liqOK := httpGetJSON(ctx, s.client, s.base+"/public/v2/liquidation?symbol=BTC&time_type=h1", headers, &liqRaw) == nil
	if liqOK && len(liqRaw.Data) > 0 {
		last := liqRaw.Data[len(liqRaw.Data)-1]
		buy, _ := strconv.ParseFloat(last.BuyVolUsd, 64)
		sell, _ := strconv.ParseFloat(last.SellVolUsd, 64)
		total, _ := strconv.ParseFloat(last.VolUsd, 64)
		if total == 0 {
			total = buy + sell
		}
		out.Liquidations = &LiquidationStats{TotalUSD: total, LongUSD: buy, ShortUSD: sell, Time: last.Timestamp}
	}

	if !fundingOK && !lsOK && !liqOK {
		return nil, fmt.Errorf("coinglass: all endpoints failed")
	}
	if out.Funding == nil {
		out.Funding = []FundingRateEntry{}
	}
	return out, nil
}

// ══════════════════════════════════════════════════════════════
// 3. 宏观：FRED（FRED_API_KEY，联邦基金利率/CPI/M2/失业率/10Y 国债）
// ══════════════════════════════════════════════════════════════

type fredSource struct {
	base   string
	apiKey string
	client *http.Client
}

func newFredSource(cfg Config, client *http.Client) *fredSource {
	return &fredSource{
		base:   cfg.baseURL("fred", "https://api.stlouisfed.org/fred"),
		apiKey: cfg.FredAPIKey,
		client: client,
	}
}

func (s *fredSource) Name() string { return "fred" }
func (s *fredSource) Description() string {
	return "FRED 美国宏观序列（利率/CPI/M2/失业率/国债收益率）"
}
func (s *fredSource) TTL() time.Duration         { return 6 * time.Hour }
func (s *fredSource) MinInterval() time.Duration { return time.Second }
func (s *fredSource) RequiresKey() bool          { return true }
func (s *fredSource) Configured() bool           { return s.apiKey != "" }

// FredSeriesMeta 内置跟踪的宏观序列。
var FredSeriesMeta = []struct {
	ID     string
	Name   string
	NameEN string
	Unit   string
}{
	{ID: "FEDFUNDS", Name: "联邦基金利率", NameEN: "Federal Funds Rate", Unit: "%"},
	{ID: "CPIAUCSL", Name: "消费者物价指数", NameEN: "CPI (All Urban)", Unit: "index"},
	{ID: "M2SL", Name: "M2 货币供应", NameEN: "M2 Money Stock", Unit: "B$"},
	{ID: "UNRATE", Name: "失业率", NameEN: "Unemployment Rate", Unit: "%"},
	{ID: "DGS10", Name: "10年期国债收益率", NameEN: "10Y Treasury Yield", Unit: "%"},
}

type MacroData struct {
	Series    []MacroSeries `json:"series"`
	UpdatedAt int64         `json:"updated_at"`
}

type MacroSeries struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	NameEN       string     `json:"name_en"`
	Unit         string     `json:"unit"`
	Observations []MacroObs `json:"observations"`
}

type MacroObs struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

func (s *fredSource) Fetch(ctx context.Context) (any, error) {
	out := &MacroData{Series: []MacroSeries{}, UpdatedAt: time.Now().Unix()}
	okCount := 0
	for _, meta := range FredSeriesMeta {
		q := url.Values{}
		q.Set("series_id", meta.ID)
		q.Set("api_key", s.apiKey)
		q.Set("file_type", "json")
		q.Set("sort_order", "desc")
		q.Set("limit", "12")
		var raw struct {
			Observations []struct {
				Date  string `json:"date"`
				Value string `json:"value"`
			} `json:"observations"`
		}
		if err := httpGetJSON(ctx, s.client, s.base+"/series/observations?"+q.Encode(), nil, &raw); err != nil {
			continue // 单序列失败不拖垮整块（错误不带 URL，避免泄露 api_key）
		}
		obs := make([]MacroObs, 0, len(raw.Observations))
		for _, o := range raw.Observations {
			if o.Value == "." || o.Value == "" {
				continue
			}
			v, err := strconv.ParseFloat(o.Value, 64)
			if err != nil {
				continue
			}
			obs = append(obs, MacroObs{Date: o.Date, Value: v})
		}
		// 翻转为时间升序，前端表格/图表直用
		for i, j := 0, len(obs)-1; i < j; i, j = i+1, j-1 {
			obs[i], obs[j] = obs[j], obs[i]
		}
		out.Series = append(out.Series, MacroSeries{
			ID: meta.ID, Name: meta.Name, NameEN: meta.NameEN, Unit: meta.Unit, Observations: obs,
		})
		okCount++
	}
	if okCount == 0 {
		return nil, fmt.Errorf("fred: all series failed")
	}
	return out, nil
}

// ══════════════════════════════════════════════════════════════
// 4. 新闻：CryptoCompare News（免 key）
// ══════════════════════════════════════════════════════════════

type cryptoCompareNewsSource struct {
	base   string
	client *http.Client
}

func newCryptoCompareNewsSource(cfg Config, client *http.Client) *cryptoCompareNewsSource {
	return &cryptoCompareNewsSource{base: cfg.baseURL("news", "https://min-api.cryptocompare.com"), client: client}
}

func (s *cryptoCompareNewsSource) Name() string { return "news" }
func (s *cryptoCompareNewsSource) Description() string {
	return "CryptoCompare 加密新闻流（可按币种过滤）"
}
func (s *cryptoCompareNewsSource) TTL() time.Duration         { return 10 * time.Minute }
func (s *cryptoCompareNewsSource) MinInterval() time.Duration { return 3 * time.Second }
func (s *cryptoCompareNewsSource) RequiresKey() bool          { return false }
func (s *cryptoCompareNewsSource) Configured() bool           { return true }

type NewsData struct {
	Items     []NewsItem `json:"items"`
	UpdatedAt int64      `json:"updated_at"`
}

type NewsItem struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	Source      string   `json:"source"`
	PublishedAt int64    `json:"published_at"`
	Categories  []string `json:"categories"`
	Summary     string   `json:"summary,omitempty"`
}

func (s *cryptoCompareNewsSource) Fetch(ctx context.Context) (any, error) {
	var raw struct {
		Data []struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			URL         string `json:"url"`
			Source      string `json:"source"`
			PublishedOn int64  `json:"published_on"`
			Categories  string `json:"categories"`
			Body        string `json:"body"`
		} `json:"data"`
	}
	if err := httpGetJSON(ctx, s.client, s.base+"/data/v2/news/?lang=EN", nil, &raw); err != nil {
		return nil, err
	}
	items := make([]NewsItem, 0, len(raw.Data))
	for _, n := range raw.Data {
		cats := []string{}
		for _, c := range strings.Split(n.Categories, "|") {
			if c = strings.TrimSpace(c); c != "" {
				cats = append(cats, c)
			}
		}
		summary := strings.TrimSpace(n.Body)
		if len([]rune(summary)) > 220 {
			summary = string([]rune(summary)[:220]) + "…"
		}
		items = append(items, NewsItem{
			ID: n.ID, Title: n.Title, URL: n.URL, Source: n.Source,
			PublishedAt: n.PublishedOn, Categories: cats, Summary: summary,
		})
		if len(items) >= 40 {
			break
		}
	}
	return &NewsData{Items: items, UpdatedAt: time.Now().Unix()}, nil
}

// FilterNewsBySymbol 按币种过滤新闻（categories 命中，大小写不敏感）。
func FilterNewsBySymbol(items []NewsItem, symbol string) []NewsItem {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return items
	}
	symbol = strings.TrimSuffix(symbol, "USDT")
	symbol = strings.TrimSuffix(symbol, "USD")
	out := []NewsItem{}
	for _, it := range items {
		for _, c := range it.Categories {
			if strings.EqualFold(c, symbol) {
				out = append(out, it)
				break
			}
		}
	}
	return out
}

// ══════════════════════════════════════════════════════════════
// 5. 热力图：自算（币安公开 24h ticker，免 key；大小=成交额）
// ══════════════════════════════════════════════════════════════

type heatmapSource struct {
	base    string
	client  *http.Client
	symbols []string
}

// HeatmapTopSymbols 市值/流动性靠前的 USDT 交易对（成交额排序在拉取后重排）。
var HeatmapTopSymbols = []string{
	"BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT",
	"ADAUSDT", "TRXUSDT", "AVAXUSDT", "LINKUSDT", "DOTUSDT", "LTCUSDT",
	"BCHUSDT", "NEARUSDT", "UNIUSDT", "ATOMUSDT", "FILUSDT", "APTUSDT",
	"ARBUSDT", "OPUSDT", "SUIUSDT", "INJUSDT", "TIAUSDT", "SEIUSDT",
}

func newHeatmapSource(cfg Config, client *http.Client) *heatmapSource {
	return &heatmapSource{base: cfg.baseURL("heatmap", "https://api.binance.com"), client: client, symbols: HeatmapTopSymbols}
}

func (s *heatmapSource) Name() string { return "heatmap" }
func (s *heatmapSource) Description() string {
	return "主流币种 24h 涨跌幅热力图（自算，成交额加权）"
}
func (s *heatmapSource) TTL() time.Duration         { return time.Minute }
func (s *heatmapSource) MinInterval() time.Duration { return 5 * time.Second }
func (s *heatmapSource) RequiresKey() bool          { return false }
func (s *heatmapSource) Configured() bool           { return true }

type HeatmapData struct {
	Entries   []HeatmapEntry `json:"entries"`
	UpdatedAt int64          `json:"updated_at"`
}

type HeatmapEntry struct {
	Symbol       string  `json:"symbol"`
	Base         string  `json:"base"`
	Price        float64 `json:"price"`
	ChangePct24h float64 `json:"change_pct_24h"`
	Volume24h    float64 `json:"volume_24h"` // 计价成交额（USDT）
	Weight       float64 `json:"weight"`     // 0-1，sqrt(成交额) 归一，前端定块大小
}

func (s *heatmapSource) Fetch(ctx context.Context) (any, error) {
	list, _ := json.Marshal(s.symbols)
	reqURL := s.base + "/api/v3/ticker/24hr?symbols=" + url.QueryEscape(string(list))
	var raw []struct {
		Symbol             string `json:"symbol"`
		LastPrice          string `json:"lastPrice"`
		PriceChangePercent string `json:"priceChangePercent"`
		QuoteVolume        string `json:"quoteVolume"`
	}
	if err := httpGetJSON(ctx, s.client, reqURL, nil, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty ticker payload")
	}
	entries := make([]HeatmapEntry, 0, len(raw))
	for _, t := range raw {
		price, _ := strconv.ParseFloat(t.LastPrice, 64)
		chg, _ := strconv.ParseFloat(t.PriceChangePercent, 64)
		vol, _ := strconv.ParseFloat(t.QuoteVolume, 64)
		if vol <= 0 {
			continue
		}
		entries = append(entries, HeatmapEntry{
			Symbol: t.Symbol, Base: strings.TrimSuffix(t.Symbol, "USDT"),
			Price: price, ChangePct24h: chg, Volume24h: vol,
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no usable ticker rows")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Volume24h > entries[j].Volume24h })
	// sqrt 归一权重（压缩数量级差异，保证小币块仍可见）
	maxSqrt := 0.0
	for _, e := range entries {
		if v := sqrt64(e.Volume24h); v > maxSqrt {
			maxSqrt = v
		}
	}
	for i := range entries {
		entries[i].Weight = sqrt64(entries[i].Volume24h) / maxSqrt
	}
	return &HeatmapData{Entries: entries, UpdatedAt: time.Now().Unix()}, nil
}

func sqrt64(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return math.Sqrt(v)
}

// ══════════════════════════════════════════════════════════════
// 6. 经济日历：ForexFactory 周历 XML 镜像（faireconomy，免 key）
//    取舍说明见 README/报告：无免费稳定官方源，该镜像覆盖本周、含
//    重要性/预期/前值，比 FRED 发布日历（需 key 且只有美国系列）更全。
// ══════════════════════════════════════════════════════════════

type calendarSource struct {
	base   string
	client *http.Client
}

func newCalendarSource(cfg Config, client *http.Client) *calendarSource {
	return &calendarSource{base: cfg.baseURL("calendar", "https://nfs.faireconomy.media"), client: client}
}

func (s *calendarSource) Name() string { return "calendar" }
func (s *calendarSource) Description() string {
	return "本周经济日历（ForexFactory 周历镜像，含重要性/预期/前值）"
}
func (s *calendarSource) TTL() time.Duration         { return 6 * time.Hour }
func (s *calendarSource) MinInterval() time.Duration { return 10 * time.Second }
func (s *calendarSource) RequiresKey() bool          { return false }
func (s *calendarSource) Configured() bool           { return true }

type CalendarData struct {
	Events    []CalendarEvent `json:"events"`
	UpdatedAt int64           `json:"updated_at"`
}

type CalendarEvent struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Currency string `json:"currency"` // USD/EUR/CNY...
	Date     string `json:"date"`     // YYYY-MM-DD
	Time     string `json:"time"`     // 原始时间串（ET），如 "8:30am"
	Impact   string `json:"impact"`   // high|medium|low|holiday
	Forecast string `json:"forecast"`
	Previous string `json:"previous"`
}

type ffWeeklyEvents struct {
	XMLName xml.Name `xml:"weeklyevents"`
	Events  []struct {
		Title    string `xml:"title"`
		Country  string `xml:"country"`
		Date     string `xml:"date"`
		Time     string `xml:"time"`
		Impact   string `xml:"impact"`
		Forecast string `xml:"forecast"`
		Previous string `xml:"previous"`
	} `xml:"event"`
}

func (s *calendarSource) Fetch(ctx context.Context) (any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/ff_calendar_thisweek.xml", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "XiaoTianQuant-Gateway/3.0")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from upstream", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var raw ffWeeklyEvents
	if err := xml.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse calendar xml: %w", err)
	}
	events := make([]CalendarEvent, 0, len(raw.Events))
	for i, e := range raw.Events {
		dateISO := normalizeFFDate(e.Date)
		events = append(events, CalendarEvent{
			ID:       fmt.Sprintf("ff-%s-%d", dateISO, i),
			Name:     strings.TrimSpace(e.Title),
			Currency: strings.TrimSpace(e.Country),
			Date:     dateISO,
			Time:     strings.TrimSpace(e.Time),
			Impact:   strings.ToLower(strings.TrimSpace(e.Impact)),
			Forecast: strings.TrimSpace(e.Forecast),
			Previous: strings.TrimSpace(e.Previous),
		})
	}
	return &CalendarData{Events: events, UpdatedAt: time.Now().Unix()}, nil
}

// normalizeFFDate 把 "01-15-2026"（MM-DD-YYYY）转为 "2026-01-15"。
func normalizeFFDate(s string) string {
	s = strings.TrimSpace(s)
	if t, err := time.Parse("01-02-2006", s); err == nil {
		return t.Format("2006-01-02")
	}
	return s
}
