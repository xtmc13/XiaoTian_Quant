// Package aigate 实现 AI 交易决策门（对标 QuantDinger 的 JEV 决策门）：
// 入场订单在风控检查之后、实际下单之前强制经过结构化 AI 审批
// （approve/reject/abstain + 置信度 + 理由 + 决策时间线落库可审计）；
// 出场/平仓/止损单永远绕过——AI 不能拦出场，防止 AI 故障把仓位锁死；
// provider 故障/超时/解析失败一律 fail-open 放行并记录 degrade 事件。
package aigate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Config ──

// Config 是决策门运行配置。默认关闭（AI_GATE_ENABLED）；store 配置 "ai_gate"
// 覆盖环境变量（PUT /api/ai/gate/config 写入），实现运行时热调。
type Config struct {
	Enabled         bool     `json:"enabled"`          // 总开关，默认 false
	MinConfidence   float64  `json:"min_confidence"`   // 置信度阈值，默认 0.6；低于阈值视为 abstain
	AbstainAction   string   `json:"abstain_action"`   // abstain 处置：allow（默认放行）| block（拦截）
	PaperOnly       bool     `json:"paper_only"`       // 仅 paper 单生效，live 单直接跳过
	TimeoutSeconds  int      `json:"timeout_seconds"`  // 单 provider 超时，默认 8（1-30）
	Provider        string   `json:"provider"`         // 指定 provider 名，空=自动链
	ContextBars     int      `json:"context_bars"`     // 上下文 K 线根数，默认 30（<=120，控制 token）
	ExcludedSources []string `json:"excluded_sources"` // 来源豁免（高频机械策略），默认 grid/dca/lmartin
}

// DefaultConfig 返回默认配置（关闭、保守阈值、fail-open 语义）。
func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		MinConfidence:   0.6,
		AbstainAction:   "allow",
		PaperOnly:       false,
		TimeoutSeconds:  8,
		ContextBars:     30,
		ExcludedSources: []string{"grid", "dca", "lmartin"},
	}
}

func (c *Config) normalize() {
	if c.MinConfidence <= 0 || c.MinConfidence > 1 {
		c.MinConfidence = 0.6
	}
	if c.AbstainAction != "block" {
		c.AbstainAction = "allow"
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = 8
	}
	if c.TimeoutSeconds > 30 {
		c.TimeoutSeconds = 30
	}
	if c.ContextBars <= 0 {
		c.ContextBars = 30
	}
	if c.ContextBars > 120 {
		c.ContextBars = 120
	}
}

func envBool(key string, cur bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return cur
	}
	return v == "1" || strings.EqualFold(v, "true")
}

func envInt(key string, cur int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return cur
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	return cur
}

func envFloat(key string, cur float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return cur
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return f
	}
	return cur
}

// LoadConfig 环境变量为底、store 配置 "ai_gate" 覆盖（与 auto_trade 同一模式）。
func LoadConfig() Config {
	cfg := DefaultConfig()
	cfg.Enabled = envBool("AI_GATE_ENABLED", cfg.Enabled)
	cfg.PaperOnly = envBool("AI_GATE_PAPER_ONLY", cfg.PaperOnly)
	cfg.MinConfidence = envFloat("AI_GATE_MIN_CONFIDENCE", cfg.MinConfidence)
	cfg.TimeoutSeconds = envInt("AI_GATE_TIMEOUT_SECONDS", cfg.TimeoutSeconds)
	if v := strings.TrimSpace(os.Getenv("AI_GATE_ABSTAIN_ACTION")); v != "" {
		cfg.AbstainAction = strings.ToLower(v)
	}
	if v := strings.TrimSpace(os.Getenv("AI_GATE_PROVIDER")); v != "" {
		cfg.Provider = v
	}
	cfg.ApplyMap(readStoreGateConfig())
	cfg.normalize()
	return cfg
}

func readStoreGateConfig() map[string]any {
	defer func() { _ = recover() }() // store 未初始化时按无配置处理
	cfg := store.GetConfig()
	if m, ok := cfg["ai_gate"].(map[string]any); ok {
		return m
	}
	return nil
}

// ApplyMap 把 API/配置文件里的宽松 map 合并进 Config（只覆盖出现的键）。
func (c *Config) ApplyMap(m map[string]any) {
	if m == nil {
		return
	}
	if v, ok := m["enabled"].(bool); ok {
		c.Enabled = v
	}
	if v, ok := m["paper_only"].(bool); ok {
		c.PaperOnly = v
	}
	if v, ok := mapFloat(m, "min_confidence"); ok {
		c.MinConfidence = v
	}
	if v, ok := mapFloat(m, "timeout_seconds"); ok {
		c.TimeoutSeconds = int(v)
	}
	if v, ok := mapFloat(m, "context_bars"); ok {
		c.ContextBars = int(v)
	}
	if v, ok := m["abstain_action"].(string); ok && v != "" {
		c.AbstainAction = strings.ToLower(strings.TrimSpace(v))
	}
	if v, ok := m["provider"].(string); ok {
		c.Provider = strings.TrimSpace(v)
	}
	if v, ok := m["excluded_sources"].([]any); ok {
		var sources []string
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				sources = append(sources, strings.ToLower(strings.TrimSpace(s)))
			}
		}
		c.ExcludedSources = sources
	}
	c.normalize()
}

func mapFloat(m map[string]any, key string) (float64, bool) {
	switch v := m[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// ToMap 导出为 API 响应用的 map。
func (c Config) ToMap() map[string]any {
	return map[string]any{
		"enabled":          c.Enabled,
		"min_confidence":   c.MinConfidence,
		"abstain_action":   c.AbstainAction,
		"paper_only":       c.PaperOnly,
		"timeout_seconds":  c.TimeoutSeconds,
		"provider":         c.Provider,
		"context_bars":     c.ContextBars,
		"excluded_sources": c.ExcludedSources,
	}
}

// ── 入场/出场分类 ──

// ResolveSource 解析订单来源：显式 Source 字段优先，其次 client_oid
// （"dca:botID" 等，见 bot_executor），最后兜底 manual（手动下单/旧信号通道）。
func ResolveSource(req *order.Request) string {
	if s := strings.TrimSpace(req.Source); s != "" {
		return s
	}
	if s := strings.TrimSpace(req.ClientOID); s != "" {
		return s
	}
	return "manual"
}

// SourceKind 取来源前缀（":" 之前），用于豁免列表匹配。
func SourceKind(source string) string {
	if i := strings.Index(source, ":"); i > 0 {
		return strings.ToLower(source[:i])
	}
	return strings.ToLower(source)
}

// ClassifyEntry 判定订单是否为入场单。出场/平仓/止损/引擎子单永远返回
// false（附带原因）——决策门绝不评估它们，保证 AI 故障不可能锁死仓位。
func ClassifyEntry(req *order.Request) (entry bool, bypassReason string) {
	if req.AIGateBypass {
		return false, "engine_child_or_trigger"
	}
	if req.ClosePosition {
		return false, "close_position"
	}
	switch req.OrderType {
	case model.TypeStopLoss, model.TypeTakeProfit, model.TypeStopLossLimit,
		model.TypeTakeProfitLimit, model.TypeTrailingStop:
		return false, "protective_order_type"
	}
	if req.MarketType == model.MarketSwap {
		// 双向持仓语义：与持仓方向相反的合约为减仓/平仓。
		if req.PositionSide == model.PositionLong && req.Side == model.SideSell {
			return false, "reduce_long"
		}
		if req.PositionSide == model.PositionShort && req.Side == model.SideBuy {
			return false, "reduce_short"
		}
		return true, ""
	}
	// 现货无做空：卖出即减持/出场。
	if req.Side == model.SideSell {
		return false, "spot_sell_reduces"
	}
	return true, ""
}

// ── LLM 调用抽象 ──

// LLMCaller 抽象 ai.Provider 的调用面，单测注入 fake。
type LLMCaller interface {
	ChatCompletion(req ai.CompletionRequest) (*ai.CompletionResponse, error)
}

type ProviderCaller struct {
	Name   string
	Model  string
	Caller LLMCaller
}

// llmDecision 是结构化输出契约：{decision, confidence, reasons}。
type llmDecision struct {
	Decision   string   `json:"decision"`
	Confidence float64  `json:"confidence"`
	Reasons    []string `json:"reasons"`
}

// ── Result ──

// Result 是一次门评估的完整结果（同时是落库与 API 响应的事实源）。
type Result struct {
	Allowed       bool     `json:"allowed"`
	Decision      string   `json:"decision"` // approve|reject|abstain|bypassed_exit|skipped|fail_open
	Confidence    float64  `json:"confidence"`
	Reasons       []string `json:"reasons"`
	Provider      string   `json:"provider"`
	Model         string   `json:"model"`
	LatencyMs     int64    `json:"latency_ms"`
	FailOpen      bool     `json:"fail_open"`
	DegradeReason string   `json:"degrade_reason"`
	DecisionID    string   `json:"decision_id"`
	RequestHash   string   `json:"request_hash"`
	ContextJSON   string   `json:"context_json"`
}

// ── Gate ──

// Gate 是决策门本体。所有外部依赖（provider 链、上下文源、落库、配置加载）
// 均为可注入字段，生产用 NewGate() 默认值，单测整体替换。
type Gate struct {
	Sources       ContextSources
	LoadConfigFn  func() Config
	ProviderChain func(cfg Config) []ProviderCaller
	Persist       func(rec *store.AIGateDecisionRecord) error
	MarkOutcome   func(id, orderID string, executed bool) error
	NewID         func() string
	Logger        *log.Logger
}

// NewGate 创建生产决策门（真实 provider 链 + store 落库）。
func NewGate() *Gate {
	repo := store.GetAIGateDecisionRepo()
	return &Gate{
		Sources:       DefaultContextSources(),
		LoadConfigFn:  LoadConfig,
		ProviderChain: defaultProviderChain,
		Persist:       repo.Create,
		MarkOutcome:   repo.MarkOutcome,
		NewID:         newDecisionID,
		Logger:        log.New(os.Stderr, "[aigate] ", log.LstdFlags),
	}
}

var globalGate = NewGate()

// Global 返回进程级决策门单例。
func Global() *Gate { return globalGate }

// SetGlobal 替换全局决策门（测试用）。
func SetGlobal(g *Gate) { globalGate = g }

func newDecisionID() string {
	return fmt.Sprintf("aig-%d-%d", time.Now().UnixNano(), time.Now().UnixMilli()%1000)
}

// CheckOrder 是 OMS 钩子（order.OrderManager.AIGateCheck 的签名）：
// 返回 (decisionID, error)；error 非空表示拦截。decisionID 用于订单终态后
// 回写成交结果。未启用时零开销直返（无 LLM 调用、无落库、无上下文构建）。
func (g *Gate) CheckOrder(req *order.Request) (string, error) {
	cfg := DefaultConfig()
	if g.LoadConfigFn != nil {
		cfg = g.LoadConfigFn()
	}
	if !cfg.Enabled {
		return "", nil
	}
	res := g.Evaluate(req, cfg)
	// 引擎子单/条件触发是机械执行（父单已审批或属保护动作），不产审计行；
	// 真实出场绕过（close_position/止损类型/减仓）必须落库，证明"AI 未拦出场"。
	if res.DecisionID != "" && !req.AIGateBypass {
		g.persistRecord(req, res)
	}
	if !res.Allowed {
		reason := "rejected"
		if len(res.Reasons) > 0 {
			reason = res.Reasons[0]
		}
		return res.DecisionID, fmt.Errorf("AI 决策门拦截（%s, 置信度 %.2f）: %s",
			res.Decision, res.Confidence, reason)
	}
	return res.DecisionID, nil
}

// Evaluate 执行完整评估流程（不落库——由调用方决定）。cfg 需已 normalize。
// 语义总表：
//   - 出场/止损/引擎子单        → bypassed_exit，放行（绝不评估、绝不拦截）
//   - paper_only 且 live 单     → skipped，放行
//   - 来源在豁免列表            → skipped，放行
//   - 无可用 provider           → fail_open，放行（degrade: ai_not_configured）
//   - 全部 provider 超时/错误   → fail_open，放行（degrade: provider_unavailable）
//   - 响应解析失败              → fail_open，放行（degrade: parse_error）
//   - approve + 置信度达标      → 放行
//   - reject  + 置信度达标      → 拦截
//   - abstain 或置信度不足      → abstain_action=allow 放行（默认）/ block 拦截
func (g *Gate) Evaluate(req *order.Request, cfg Config) *Result {
	started := time.Now()
	res := &Result{Allowed: true, Decision: "approve", Reasons: []string{}, DecisionID: g.newID()}

	source := ResolveSource(req)

	// 1. 出场/止损/引擎子单：永远绕过（AI 不能拦出场）。
	if entry, why := ClassifyEntry(req); !entry {
		res.Decision = "bypassed_exit"
		res.Reasons = []string{"出场/保护单不经 AI 决策门: " + why}
		res.LatencyMs = elapsedMs(started)
		return res
	}

	// 2. 仅 paper 生效：live 单跳过。
	if cfg.PaperOnly && !isPaperExchange(req.Exchange) {
		res.Decision = "skipped"
		res.Reasons = []string{"paper_only 配置：live 单不经决策门"}
		res.LatencyMs = elapsedMs(started)
		return res
	}

	// 3. 来源豁免（高频机械策略逐单过 LLM 无意义，对标 QuantDinger）。
	kind := SourceKind(source)
	for _, ex := range cfg.ExcludedSources {
		if kind == strings.ToLower(strings.TrimSpace(ex)) {
			res.Decision = "skipped"
			res.Reasons = []string{"来源在豁免列表: " + kind}
			res.LatencyMs = elapsedMs(started)
			return res
		}
	}

	// 4. 构建决策上下文（best-effort：任何数据源失败只降级不失败）。
	dctx := g.Sources.Build(req, source, cfg.ContextBars)
	contextJSON := dctx.JSON()
	res.ContextJSON = truncate(contextJSON, 4096)
	sum := sha256.Sum256([]byte(contextJSON))
	res.RequestHash = hex.EncodeToString(sum[:])

	// 5. provider 链故障转移。
	var chain []ProviderCaller
	if g.ProviderChain != nil {
		chain = g.ProviderChain(cfg)
	}
	if len(chain) == 0 {
		res.Decision = "fail_open"
		res.FailOpen = true
		res.DegradeReason = "ai_not_configured: 无可用 LLM provider（未配置 API key）"
		res.Reasons = []string{"AI 未配置，fail-open 放行"}
		res.LatencyMs = elapsedMs(started)
		return res
	}

	var failures []string
	for _, p := range chain {
		content, err := g.callProvider(p, contextJSON, time.Duration(cfg.TimeoutSeconds)*time.Second)
		if err != nil {
			failures = append(failures, p.Name+": "+safeErr(err))
			continue
		}
		dec, err := parseDecisionJSON(content)
		if err != nil {
			failures = append(failures, p.Name+": parse: "+safeErr(err))
			continue
		}
		// 成功拿到结构化决策。
		res.Provider = p.Name
		res.Model = p.Model
		res.Confidence = dec.Confidence
		res.Reasons = dec.Reasons
		res.Decision = dec.Decision
		res.LatencyMs = elapsedMs(started)
		if len(failures) > 0 {
			res.DegradeReason = "前置 provider 失败: " + truncate(strings.Join(failures, "; "), 280)
		}

		// 置信度不足 → 视为 abstain（可配放行/拦截）。
		if res.Confidence < cfg.MinConfidence && (dec.Decision == "approve" || dec.Decision == "reject") {
			res.Reasons = append([]string{fmt.Sprintf(
				"置信度 %.2f 低于阈值 %.2f，原决策 %s 降级为 abstain", res.Confidence, cfg.MinConfidence, dec.Decision)},
				res.Reasons...)
			res.Decision = "abstain"
		}

		switch res.Decision {
		case "approve":
			res.Allowed = true
		case "reject":
			res.Allowed = false
		default: // abstain
			res.Allowed = cfg.AbstainAction != "block"
		}
		return res
	}

	// 6. 全部 provider 失败 → fail-open 放行。
	res.Decision = "fail_open"
	res.FailOpen = true
	res.DegradeReason = "provider_unavailable: " + truncate(strings.Join(failures, "; "), 280)
	res.Reasons = []string{"LLM provider 全部不可用，fail-open 放行"}
	res.LatencyMs = elapsedMs(started)
	return res
}

// callProvider 带超时调用单个 provider（超时判定后底层请求被遗弃，
// provider 自身 http client 会兜底回收连接）。
func (g *Gate) callProvider(p ProviderCaller, contextJSON string, timeout time.Duration) (string, error) {
	type outcome struct {
		content string
		err     error
	}
	ch := make(chan outcome, 1)
	go func() {
		resp, err := p.Caller.ChatCompletion(ai.CompletionRequest{
			Messages: []ai.ChatMessage{
				{Role: ai.RoleSystem, Content: gateSystemPrompt},
				{Role: ai.RoleUser, Content: contextJSON},
			},
			MaxTokens:   512,
			Temperature: 0,
		})
		if err != nil {
			ch <- outcome{err: err}
			return
		}
		if resp == nil || len(resp.Choices) == 0 {
			ch <- outcome{err: fmt.Errorf("empty response")}
			return
		}
		ch <- outcome{content: resp.Choices[0].Message.Content}
	}()

	select {
	case out := <-ch:
		return out.content, out.err
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout after %s", timeout)
	}
}

// persistRecord 落库决策时间线；失败只记日志，绝不影响交易路径。
func (g *Gate) persistRecord(req *order.Request, res *Result) {
	if g.Persist == nil {
		return
	}
	refPrice := req.Price
	notional := refPrice * req.Quantity
	defer func() {
		if r := recover(); r != nil {
			g.logf("决策落库 panic 已吞没: %v", r)
		}
	}()
	err := g.Persist(&store.AIGateDecisionRecord{
		ID:            res.DecisionID,
		UserID:        int64(req.UserID),
		Source:        ResolveSource(req),
		Symbol:        req.Symbol,
		Side:          string(req.Side),
		OrderType:     string(req.OrderType),
		MarketType:    string(req.MarketType),
		PositionSide:  string(req.PositionSide),
		Quantity:      req.Quantity,
		RefPrice:      refPrice,
		Notional:      notional,
		Decision:      res.Decision,
		Allowed:       res.Allowed,
		Confidence:    res.Confidence,
		Reasons:       res.Reasons,
		Provider:      res.Provider,
		Model:         res.Model,
		LatencyMs:     res.LatencyMs,
		FailOpen:      res.FailOpen,
		DegradeReason: res.DegradeReason,
		RequestHash:   res.RequestHash,
		ContextJSON:   res.ContextJSON,
	})
	if err != nil {
		g.logf("决策落库失败（不影响下单）: %v", err)
	}
}

// RecordOutcome 是 OMS 的 AIGateOutcome 钩子：订单终态后回写成交结果。
func (g *Gate) RecordOutcome(decisionID, orderID string, executed bool) {
	if decisionID == "" || g.MarkOutcome == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			g.logf("成交回写 panic 已吞没: %v", r)
		}
	}()
	if err := g.MarkOutcome(decisionID, orderID, executed); err != nil {
		g.logf("成交回写失败: %v", err)
	}
}

func (g *Gate) newID() string {
	if g.NewID != nil {
		return g.NewID()
	}
	return newDecisionID()
}

func (g *Gate) logf(format string, args ...any) {
	if g.Logger != nil {
		g.Logger.Printf(format, args...)
	}
}

func isPaperExchange(exchange string) bool {
	return exchange == "" || strings.EqualFold(exchange, "paper")
}

func elapsedMs(start time.Time) int64 {
	ms := time.Since(start).Milliseconds()
	if ms < 0 {
		return 0
	}
	return ms
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// safeErr 压缩错误串（错误可能含 provider 响应体，截断防泄密/防爆日志）。
func safeErr(err error) string {
	return truncate(strings.Join(strings.Fields(fmt.Sprint(err)), " "), 200)
}

// ── 结构化输出契约解析 ──

// parseDecisionJSON 从 LLM 响应里提取严格 JSON 契约：
// {decision: "approve"|"reject"|"abstain", confidence: 0-1, reasons: []}。
// 容忍 ```json 围栏与前缀噪声；decision 不在枚举内即失败（fail-open 由调用方决定）。
func parseDecisionJSON(content string) (*llmDecision, error) {
	text := strings.TrimSpace(content)
	if text == "" {
		return nil, fmt.Errorf("empty content")
	}
	// 去 markdown 围栏
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```JSON")
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}
	// 定位首个 JSON 对象
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object in response")
	}
	var dec llmDecision
	if err := json.Unmarshal([]byte(text[start:end+1]), &dec); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	dec.Decision = strings.ToLower(strings.TrimSpace(dec.Decision))
	switch dec.Decision {
	case "approve", "reject", "abstain":
	default:
		return nil, fmt.Errorf("decision %q not in {approve,reject,abstain}", dec.Decision)
	}
	if dec.Confidence < 0 {
		dec.Confidence = 0
	}
	if dec.Confidence > 1 {
		dec.Confidence = 1
	}
	// reasons 清洗：最多 8 条，每条 <=300 字符
	clean := make([]string, 0, len(dec.Reasons))
	for _, r := range dec.Reasons {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		clean = append(clean, truncate(r, 300))
		if len(clean) >= 8 {
			break
		}
	}
	dec.Reasons = clean
	return &dec, nil
}

const gateSystemPrompt = `You are a conservative, evidence-based pre-trade ENTRY filter for a quantitative trading system.
You receive a JSON context: order details, strategy source, recent market bars summary, account risk state, and recent performance.
Decide whether the entry order should be allowed.
Rules:
- Reject ONLY for concrete evidence in the supplied context: directional contradiction, material account/portfolio risk, or unsafe execution conditions.
- Missing or stale evidence alone is NOT a reason to reject; use "abstain" when evidence is insufficient.
- You never see exit/stop-loss orders; every request you receive is an entry.
Respond with STRICT JSON only (no markdown, no prose):
{"decision": "approve" | "reject" | "abstain", "confidence": <number 0-1>, "reasons": [<short evidence-based reasons, max 8>]}`

// ── Provider 链（含故障转移） ──

// defaultProviderChain 按优先级构建 provider 调用链：
//  1. 配置指定的 provider（cfg.Provider，支持 store 里的密钥覆盖）；
//  2. store 配置的激活 provider（ai.defaults.provider / ai.provider）；
//  3. 环境变量里配有 key 的 provider（与 strategy_ai.go 同一兜底顺序）。
func defaultProviderChain(cfg Config) []ProviderCaller {
	var chain []ProviderCaller
	seen := map[string]bool{}
	add := func(p *ai.Provider) {
		if p == nil || p.APIKey == "" || seen[p.Name] {
			return
		}
		seen[p.Name] = true
		chain = append(chain, ProviderCaller{Name: p.Name, Model: p.Model, Caller: p})
	}
	if cfg.Provider != "" {
		add(resolveStoreProvider(cfg.Provider))
		add(ai.GetProvider(cfg.Provider))
	}
	add(activeStoreProvider())
	for _, name := range []string{"deepseek", "openai", "qwen", "hunyuan", "glm", "kimi", "claude", "gemini"} {
		add(ai.GetProvider(name))
	}
	return chain
}

// activeStoreProvider 复刻 handler.getActiveAIProvider 的 store 配置解析
// （ai.defaults.provider / ai.provider + ai.<name> 的 api_key/model/base_url 覆盖），
// 保持 handler 包不可导入时的同一语义。
func activeStoreProvider() *ai.Provider {
	defer func() { _ = recover() }()
	cfg := store.GetConfig()
	aiCfg, ok := cfg["ai"].(map[string]any)
	if !ok {
		return nil
	}
	name := ""
	if defaults, ok := aiCfg["defaults"].(map[string]any); ok {
		name, _ = defaults["provider"].(string)
	}
	if name == "" {
		name, _ = aiCfg["provider"].(string)
	}
	if name == "" {
		return nil
	}
	// legacy 名归一：旧配置可能写 anthropic，注册表为 claude。
	return resolveStoreProvider(ai.NormalizeProviderName(name))
}

// resolveStoreProvider 取注册 provider 并应用 store 中的凭证/模型覆盖（克隆，不改全局）。
func resolveStoreProvider(name string) *ai.Provider {
	p := ai.GetProvider(name)
	if p == nil {
		return nil
	}
	defer func() { _ = recover() }()
	cfg := store.GetConfig()
	aiCfg, ok := cfg["ai"].(map[string]any)
	if !ok {
		if p.APIKey != "" {
			return p
		}
		return nil
	}
	pc, _ := aiCfg[name].(map[string]any)
	if pc == nil {
		// 磁盘上的老配置可能仍存于 legacy key（如 ai.anthropic），回退查找。
		if legacy := ai.LegacyProviderName(name); legacy != "" {
			pc, _ = aiCfg[legacy].(map[string]any)
		}
	}
	if pc == nil {
		if nested, ok := aiCfg["providers"].(map[string]any); ok {
			pc, _ = nested[name].(map[string]any)
		}
	}
	if pc != nil {
		if key, _ := pc["api_key"].(string); key != "" {
			clone := *p
			clone.APIKey = key
			if m, _ := pc["model"].(string); m != "" {
				clone.Model = m
			}
			if u, _ := pc["base_url"].(string); u != "" {
				clone.BaseURL = u
			}
			return &clone
		}
	}
	if p.APIKey != "" {
		return p
	}
	return nil
}
