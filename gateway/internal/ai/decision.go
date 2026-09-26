package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ── AI 决策信号引擎 ─────────────────────────────────────────────
// DecisionEngine 用真实 LLM 为单品种生成交易决策信号：
//   - fast 模式（默认）：单次 LLM 调用，prompt 要求严格 JSON 输出
//     {signal,confidence,reason,market_condition}，正则/JSON 双重解析提取。
//   - deep 模式：走 7-agent 三阶段 Pipeline（分析→辩论→决策），
//     从 trader 结论解析方向与置信度。
// 行情数据来自币安公共接口（无需 key），可用 DecisionConfig.MarketData 注入替换（测试）。
// market_filter 开启时，波动率过大 / 成交量过低会把信号降级为 neutral 并记录原因。

// AIDecision 是一次 AI 决策的归一化输出。
type AIDecision struct {
	Symbol          string   `json:"symbol"`
	Signal          string   `json:"signal"` // long / short / neutral
	Confidence      float64  `json:"confidence"`
	Reason          string   `json:"reason"`
	Filters         []string `json:"filters"`
	MarketCondition string   `json:"market_condition"` // trending / ranging / volatile
	Mode            string   `json:"mode"`             // fast / deep
	Provider        string   `json:"provider"`
	CreatedAt       int64    `json:"created_at"`
}

// DecisionConfig 决策引擎配置。
type DecisionConfig struct {
	Provider string // LLM provider 名（GetProvider 查找），默认 deepseek
	Model    string // 可选：覆盖 provider 默认 model
	Mode     string // "fast"（默认）或 "deep"

	// MarketFilter 开启后按波动率/成交量降级 neutral。
	MarketFilter  bool
	MaxVolatility float64 // 24h 高低价差百分比上限，默认 10
	MinVolume24h  float64 // 24h 成交额下限（USDT），默认 1,000,000

	// MarketData 可注入行情源（测试用）；nil 时走币安公共接口。
	MarketData func(ctx context.Context, symbol string) (*MarketSnapshot, error)
	// HTTPClient 可覆盖默认行情 HTTP client。
	HTTPClient *http.Client
}

// MarketSnapshot 是决策所需的最小行情摘要。
type MarketSnapshot struct {
	Symbol     string
	Price      float64
	Change24h  float64 // 百分比
	High24h    float64
	Low24h     float64
	Volume24h  float64 // 成交额（quote volume）
	Volatility float64 // (high-low)/low*100
	Closes     []float64
}

// DecisionEngine 是 AI 决策信号引擎。
type DecisionEngine struct {
	cfg    DecisionConfig
	client *http.Client
}

// NewDecisionEngine 构造决策引擎；补默认配置。
func NewDecisionEngine(cfg DecisionConfig) *DecisionEngine {
	if cfg.Provider == "" {
		cfg.Provider = "deepseek"
	}
	if cfg.Mode == "" {
		cfg.Mode = "fast"
	}
	if cfg.MaxVolatility <= 0 {
		cfg.MaxVolatility = 10
	}
	if cfg.MinVolume24h <= 0 {
		cfg.MinVolume24h = 1_000_000
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &DecisionEngine{cfg: cfg, client: cfg.HTTPClient}
}

// Decide 生成一次决策信号；出错返回 error（调用方决定是否落库）。
func (e *DecisionEngine) Decide(ctx context.Context, symbol string) (*AIDecision, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol required")
	}
	fetch := e.cfg.MarketData
	if fetch == nil {
		fetch = fetchBinanceSnapshot(e.client)
	}
	snap, err := fetch(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("market data: %w", err)
	}
	if snap == nil || snap.Price <= 0 {
		return nil, fmt.Errorf("market data unavailable for %s", symbol)
	}

	var dec *AIDecision
	switch strings.ToLower(e.cfg.Mode) {
	case "deep":
		dec, err = e.deepDecide(snap)
	default:
		dec, err = e.fastDecide(ctx, snap)
	}
	if err != nil {
		return nil, err
	}

	dec.Symbol = symbol
	dec.Mode = e.cfg.Mode
	dec.Provider = e.cfg.Provider
	if dec.Filters == nil {
		dec.Filters = []string{}
	}
	if dec.Signal == "" {
		dec.Signal = "neutral"
	}
	dec.Signal = normalizeSignal(dec.Signal)
	dec.Confidence = normalizeConfidence(dec.Confidence)
	if dec.CreatedAt == 0 {
		dec.CreatedAt = time.Now().Unix()
	}

	// market_filter：波动率过大 / 成交量过低 → 降级 neutral 并记录原因。
	if e.cfg.MarketFilter {
		if snap.Volatility > e.cfg.MaxVolatility {
			dec.Filters = append(dec.Filters,
				fmt.Sprintf("high_volatility:%.2f%%>%.2f%%", snap.Volatility, e.cfg.MaxVolatility))
			dec.Signal = "neutral"
		}
		if snap.Volume24h > 0 && snap.Volume24h < e.cfg.MinVolume24h {
			dec.Filters = append(dec.Filters,
				fmt.Sprintf("low_volume:%.0f<%.0f", snap.Volume24h, e.cfg.MinVolume24h))
			dec.Signal = "neutral"
		}
		if dec.MarketCondition == "" && len(dec.Filters) > 0 {
			dec.MarketCondition = "volatile"
		}
	}
	return dec, nil
}

// ── fast 模式：单次 LLM 调用 ───────────────────────────────────

var decisionJSONRe = regexp.MustCompile(`\{[^{}]*"signal"[^{}]*\}`)

func (e *DecisionEngine) fastDecide(ctx context.Context, snap *MarketSnapshot) (*AIDecision, error) {
	provider := GetProvider(e.cfg.Provider)
	if provider == nil {
		return nil, fmt.Errorf("provider %q not registered", e.cfg.Provider)
	}
	if provider.APIKey == "" {
		return nil, fmt.Errorf("provider %q api key not configured", e.cfg.Provider)
	}

	prompt := buildFastDecisionPrompt(snap)
	resp, err := provider.ChatCompletion(CompletionRequest{
		Messages: []ChatMessage{
			{Role: RoleSystem, Content: "You are a quantitative trading decision engine. Respond with strict JSON only, no markdown, no extra text."},
			{Role: RoleUser, Content: prompt},
		},
		MaxTokens:   512,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("llm: empty response")
	}
	content := resp.Choices[0].Message.Content

	var parsed struct {
		Signal          string  `json:"signal"`
		Confidence      float64 `json:"confidence"`
		Reason          string  `json:"reason"`
		MarketCondition string  `json:"market_condition"`
	}
	raw := extractDecisionJSON(content)
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("parse decision json: %w (raw=%q)", err, truncate(content, 200))
	}
	return &AIDecision{
		Signal:          parsed.Signal,
		Confidence:      parsed.Confidence,
		Reason:          parsed.Reason,
		MarketCondition: parsed.MarketCondition,
	}, nil
}

// buildFastDecisionPrompt 把行情摘要拼成 fast 模式的决策 prompt（要求严格 JSON）。
func buildFastDecisionPrompt(snap *MarketSnapshot) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Symbol: %s\n", snap.Symbol))
	sb.WriteString(fmt.Sprintf("Current Price: %.4f\n", snap.Price))
	sb.WriteString(fmt.Sprintf("24h Change: %.2f%%\n", snap.Change24h))
	sb.WriteString(fmt.Sprintf("24h Range: %.4f - %.4f (volatility %.2f%%)\n", snap.Low24h, snap.High24h, snap.Volatility))
	sb.WriteString(fmt.Sprintf("24h Quote Volume: %.0f\n", snap.Volume24h))
	if n := len(snap.Closes); n >= 10 {
		window := snap.Closes
		if n > 20 {
			window = window[n-20:]
		}
		parts := make([]string, len(window))
		for i, c := range window {
			parts[i] = strconv.FormatFloat(c, 'f', 4, 64)
		}
		sb.WriteString(fmt.Sprintf("Recent %d closes: %s\n", len(window), strings.Join(parts, ", ")))
	}
	sb.WriteString(`
Decide the trading bias for the next few hours. Respond ONLY with a JSON object:
{"signal":"long|short|neutral","confidence":0-100,"reason":"one or two sentences","market_condition":"trending|ranging|volatile"}
Rules: confidence is an integer 0-100. Use neutral when there is no clear edge.`)
	return sb.String()
}

// extractDecisionJSON 从 LLM 输出中提取 JSON：优先完整对象解析，退化到正则抓
// {"signal":...} 片段，兼容模型输出 markdown 代码块或前后附带文字的情况。
func extractDecisionJSON(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		var inner []string
		for _, ln := range lines {
			if strings.HasPrefix(strings.TrimSpace(ln), "```") {
				continue
			}
			inner = append(inner, ln)
		}
		trimmed = strings.TrimSpace(strings.Join(inner, "\n"))
	}
	if json.Valid([]byte(trimmed)) {
		return trimmed
	}
	if idx := strings.Index(trimmed, "{"); idx != -1 {
		if end := strings.LastIndex(trimmed, "}"); end > idx {
			candidate := trimmed[idx : end+1]
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
	}
	if m := decisionJSONRe.FindString(trimmed); m != "" {
		return m
	}
	return trimmed
}

// ── deep 模式：7-agent Pipeline ────────────────────────────────

func (e *DecisionEngine) deepDecide(snap *MarketSnapshot) (*AIDecision, error) {
	rsi := computeRSI(snap.Closes, 14)
	macdTrend := "unknown"
	if len(snap.Closes) >= 26 {
		fast := ema(snap.Closes, 12)
		slow := ema(snap.Closes, 26)
		switch {
		case fast > slow*1.001:
			macdTrend = "bullish"
		case fast < slow*0.999:
			macdTrend = "bearish"
		default:
			macdTrend = "neutral"
		}
	}
	condition := "ranging"
	if snap.Volatility > e.cfg.MaxVolatility {
		condition = "volatile"
	} else if math.Abs(snap.Change24h) > 3 {
		condition = "trending"
	}

	input := MarketInput{
		Symbol:       snap.Symbol,
		CurrentPrice: snap.Price,
		Change24h:    snap.Change24h,
		Volume24h:    snap.Volume24h,
		High24h:      snap.High24h,
		Low24h:       snap.Low24h,
		RSI:          rsi,
		MACD:         macdTrend,
		Volatility:   snap.Volatility,
	}
	pipe := NewPipeline()
	pipe.SetProvider(e.cfg.Provider)
	decision, err := pipe.Run(input)
	if err != nil {
		return nil, fmt.Errorf("pipeline: %w", err)
	}

	signal := "neutral"
	switch strings.ToUpper(decision.Direction) {
	case "LONG":
		signal = "long"
	case "SHORT":
		signal = "short"
	}
	reason := strings.TrimSpace(decision.Reason)
	if reason == "" {
		reason = fmt.Sprintf("multi-agent consensus=%.2f has_consensus=%v", decision.Consensus, decision.HasConsensus)
	}
	return &AIDecision{
		Signal:          signal,
		Confidence:      decision.Confidence * 100,
		Reason:          reason,
		MarketCondition: condition,
	}, nil
}

// ── 行情数据（币安公共接口，无需 key）──────────────────────────

// fetchBinanceSnapshot 拉取 24h ticker + 近 100 根 1h K 线收盘价。
func fetchBinanceSnapshot(client *http.Client) func(ctx context.Context, symbol string) (*MarketSnapshot, error) {
	return func(ctx context.Context, symbol string) (*MarketSnapshot, error) {
		type tickerResp struct {
			LastPrice      string `json:"lastPrice"`
			PriceChangePct string `json:"priceChangePercent"`
			HighPrice      string `json:"highPrice"`
			LowPrice       string `json:"lowPrice"`
			QuoteVolume    string `json:"quoteVolume"`
		}
		url := fmt.Sprintf("https://api.binance.com/api/v3/ticker/24hr?symbol=%s", symbol)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("ticker HTTP %d: %s", resp.StatusCode, truncate(string(body), 120))
		}
		var ticker tickerResp
		if err := json.NewDecoder(resp.Body).Decode(&ticker); err != nil {
			return nil, err
		}

		snap := &MarketSnapshot{
			Symbol:    symbol,
			Price:     parseFloatStr(ticker.LastPrice),
			Change24h: parseFloatStr(ticker.PriceChangePct),
			High24h:   parseFloatStr(ticker.HighPrice),
			Low24h:    parseFloatStr(ticker.LowPrice),
			Volume24h: parseFloatStr(ticker.QuoteVolume),
		}
		if snap.Low24h > 0 {
			snap.Volatility = (snap.High24h - snap.Low24h) / snap.Low24h * 100
		}

		// 近 100 根 1h K 线收盘价（供 prompt 摘要 + RSI/MACD 计算）。
		klineURL := fmt.Sprintf("https://api.binance.com/api/v3/klines?symbol=%s&interval=1h&limit=100", symbol)
		kreq, err := http.NewRequestWithContext(ctx, http.MethodGet, klineURL, nil)
		if err == nil {
			if kresp, err := client.Do(kreq); err == nil {
				defer kresp.Body.Close()
				if kresp.StatusCode == http.StatusOK {
					var raw [][]any
					if err := json.NewDecoder(kresp.Body).Decode(&raw); err == nil {
						for _, k := range raw {
							if len(k) > 4 {
								snap.Closes = append(snap.Closes, parseFloatStr(fmt.Sprint(k[4])))
							}
						}
					}
				}
			}
		}
		return snap, nil
	}
}

// ── 指标小工具 ────────────────────────────────────────────────

// normalizeSignal 把 LLM 的各种写法归一到 long/short/neutral。
func normalizeSignal(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "long", "buy", "bullish", "做多", "看多":
		return "long"
	case "short", "sell", "bearish", "做空", "看空":
		return "short"
	default:
		return "neutral"
	}
}

// normalizeConfidence 归一化到 0-100：<=1 视为 0-1 比例，超出截断。
func normalizeConfidence(c float64) float64 {
	if c > 0 && c <= 1 {
		c *= 100
	}
	if c < 0 {
		return 0
	}
	if c > 100 {
		return 100
	}
	return math.Round(c*10) / 10
}

func parseFloatStr(s string) float64 {
	var f float64
	fmt.Sscanf(strings.TrimSpace(s), "%f", &f)
	return f
}

// computeRSI 计算 RSI（Wilder 平滑）。
func computeRSI(closes []float64, period int) float64 {
	if len(closes) < period+1 {
		return 50
	}
	gains, losses := 0.0, 0.0
	for i := len(closes) - period; i < len(closes); i++ {
		diff := closes[i] - closes[i-1]
		if diff > 0 {
			gains += diff
		} else {
			losses -= diff
		}
	}
	if losses == 0 {
		return 100
	}
	rs := gains / losses
	return 100 - 100/(1+rs)
}

// ema 计算收盘序列的指数移动平均（末值）。
func ema(closes []float64, period int) float64 {
	if len(closes) == 0 || period <= 0 {
		return 0
	}
	k := 2.0 / float64(period+1)
	e := closes[0]
	for _, c := range closes[1:] {
		e = c*k + e*(1-k)
	}
	return e
}
