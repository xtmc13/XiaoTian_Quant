package handler

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Strategy AI Generate ──

type strategyAIRequest struct {
	Prompt      string         `json:"prompt"`
	UserID      any            `json:"user_id"`
	Intent      string         `json:"intent"` // "adjust_params" | "generate_code"
	TemplateKey string         `json:"template_key"`
	Params      map[string]any `json:"params"`
	Code        string         `json:"code"`
}

// StrategyAIGenerate handles AI-powered strategy generation and parameter optimization.
// POST /api/strategies/ai-generate
func StrategyAIGenerate(c *gin.Context) {
	var body strategyAIRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid json"})
		return
	}

	provider := getActiveAIProvider()
	if provider == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"msg":     "No AI provider configured. Please set an API key in Settings → AI.",
		})
		return
	}

	var systemPrompt, userPrompt string
	if body.Intent == "adjust_params" {
		systemPrompt = adjustParamsSystemPrompt
		userPrompt = buildAdjustParamsPrompt(body)
	} else {
		systemPrompt = generateCodeSystemPrompt
		userPrompt = buildGenerateCodePrompt(body)
	}

	resp, err := provider.ChatCompletion(ai.CompletionRequest{
		Messages: []ai.ChatMessage{
			{Role: ai.RoleSystem, Content: systemPrompt},
			{Role: ai.RoleUser, Content: userPrompt},
		},
		MaxTokens:   4096,
		Temperature: 0.3,
	})

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"msg":     "AI service error: " + err.Error(),
		})
		return
	}

	if len(resp.Choices) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "msg": "Empty response from AI"})
		return
	}

	response := resp.Choices[0].Message.Content

	if body.Intent == "adjust_params" {
		c.JSON(http.StatusOK, buildAdjustParamsResult(response, body.Params))
	} else {
		c.JSON(http.StatusOK, buildGenerateCodeResult(response, body.Code))
	}
}

// ── Provider resolution ──

func getActiveAIProvider() *ai.Provider {
	cfg := store.GetConfig()
	providerName := ""

	// Try the configured AI provider from store
	if aiCfg, ok := cfg["ai"].(map[string]any); ok {
		if defaults, ok := aiCfg["defaults"].(map[string]any); ok {
			providerName = getStringFromMap(defaults, "provider", "")
		}
		if providerName == "" {
			providerName = getStringFromMap(aiCfg, "provider", "")
		}
	}

	// Try to get the named provider
	if providerName != "" {
		if p := ai.GetProvider(providerName); p != nil && p.APIKey != "" {
			return p
		}
	}

	// Fallback: first provider with an API key set
	// Check env-based providers (deepseek, openai, etc.)
	for _, name := range []string{"deepseek", "openai", "qwen", "hunyuan", "glm", "kimi", "claude", "gemini"} {
		if p := ai.GetProvider(name); p != nil && p.APIKey != "" {
			return p
		}
	}

	return nil
}

func getStringFromMap(m map[string]any, key, def string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return def
}

// ── System Prompts ──

const adjustParamsSystemPrompt = `You are a quantitative trading parameter optimizer. You adjust grid/contract trading strategy parameters based on user requests.

Input: you receive the current strategy parameters as a JSON object and the user's optimization request.
Output: respond ONLY with a JSON object containing:
{
  "params": { <updated parameters — only include parameters that should change> },
  "explanation": "<brief explanation of what was changed and why>"
}

Parameter types:
- "grid_count" (integer, 2-200): number of grid levels
- "upper_price" / "lower_price" (number): price bounds
- "order_qty" (number): quantity per grid order
- "grid_mode" (string): "arithmetic" or "geometric"
- "direction" (string): "neutral", "long", or "short"
- "adaptive_bounds" (boolean): enable ATR-based adaptive bounds
- "atr_period" (integer, 1-200): ATR lookback period
- "atr_mult" (number, 0.5-5): ATR multiplier
- "min_width_pct" (number): minimum grid width percentage
- "max_shift_pct" (number): maximum shift percentage
- "edge_pct" (number): edge trigger percentage
- "waterfall_protection" (boolean): enable crash protection
- "waterfall_drop_pct" (number, 0.5-30): trigger drop percentage
- "waterfall_window" (integer, 1-200): detection window bars
- "waterfall_cooldown" (integer, 1-500): cooldown bars
- "take_profit_pct" (number, 0-100): take profit percentage
- "stop_loss_pct" (number, 0-100): stop loss percentage
- "trailing_tp" (boolean): enable trailing take profit
- "total_investment" (number): total investment in USDT

CRITICAL: Return ONLY valid JSON, no markdown, no extra text.`

const generateCodeSystemPrompt = `You are a quantitative trading strategy developer specializing in grid trading bots. Generate complete, production-ready Python strategy code.

The code must use the on_init(ctx) / on_bar(ctx, bar) event-driven API:
- ctx.param(name, default) — read/write persistent parameters
- ctx._params[name] = value — write parameter directly
- ctx.bars(n) — get last n bars [{open, high, low, close, volume}]
- ctx.buy(price=, amount=) — place buy order (amount in quote currency)
- ctx.sell(price=, amount=) — place sell order
- ctx.close_position() — close current position
- ctx.log(msg) — write log message
- ctx.balance — current available balance
- ctx.equity — total equity
- ctx.position — current position dict {side, size, entry_price} or None

Output format — respond with:
1. Brief explanation of the strategy
2. A Python code block in ` + "```python" + ` ... ` + "```" + `

The code must include:
- Grid level calculation (arithmetic or geometric)
- Buy/sell logic at grid crossings
- Budget/exposure tracking (long_exposure, short_exposure)
- Waterfall/crash protection if enabled
- Adaptive bounds using ATR if enabled
- Take profit / stop loss logic
- Proper initialization in on_init()
- Clear logging for debugging

IMPORTANT:
- Do NOT use external libraries except basic math functions
- All parameters must be read via ctx.param()
- Handle edge cases (price=0, empty bars, etc.)
- Return float-safe calculations (avoid division by zero)`

// ── Prompt Builders ──

func buildAdjustParamsPrompt(req strategyAIRequest) string {
	paramsJSON, _ := json.MarshalIndent(req.Params, "", "  ")
	var sb strings.Builder
	sb.WriteString("Current grid strategy parameters:\n```json\n")
	sb.Write(paramsJSON)
	sb.WriteString("\n```\n\n")

	if req.Code != "" {
		sb.WriteString("Current strategy code:\n```python\n")
		sb.WriteString(truncate(req.Code, 3000))
		sb.WriteString("\n```\n\n")
	}

	if req.TemplateKey != "" {
		sb.WriteString(fmt.Sprintf("Template: %s\n\n", req.TemplateKey))
	}

	if req.Prompt != "" {
		sb.WriteString(fmt.Sprintf("User optimization request: %s\n\n", req.Prompt))
	} else {
		sb.WriteString("Please optimize the parameters to improve risk-adjusted returns while maintaining reasonable drawdown.\n\n")
	}

	sb.WriteString("Return ONLY a JSON object with updated parameters and explanation.")
	return sb.String()
}

func buildGenerateCodePrompt(req strategyAIRequest) string {
	var sb strings.Builder

	if req.TemplateKey != "" {
		sb.WriteString(fmt.Sprintf("Template: %s\n\n", req.TemplateKey))
	}

	if len(req.Params) > 0 {
		paramsJSON, _ := json.MarshalIndent(req.Params, "", "  ")
		sb.WriteString("Strategy parameters:\n```json\n")
		sb.Write(paramsJSON)
		sb.WriteString("\n```\n\n")
	}

	if req.Code != "" {
		sb.WriteString("Current code to improve:\n```python\n")
		sb.WriteString(truncate(req.Code, 3000))
		sb.WriteString("\n```\n\n")
	}

	if req.Prompt != "" {
		sb.WriteString(fmt.Sprintf("User request: %s\n\n", req.Prompt))
	}

	sb.WriteString("Generate a complete, production-ready grid trading strategy in Python. Include the code in a ```python block.")
	return sb.String()
}

// ── Response Parsers ──

func buildAdjustParamsResult(response string, currentParams map[string]any) gin.H {
	// Extract JSON from response
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		// Try to parse the whole response as JSON
		jsonStr = response
	}

	var parsed struct {
		Params      map[string]any `json:"params"`
		Explanation string         `json:"explanation"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return gin.H{
			"success": false,
			"msg":     "Failed to parse AI response as JSON: " + err.Error(),
			"params":  nil,
		}
	}

	if parsed.Params == nil {
		parsed.Params = map[string]any{}
	}

	// Coerce types and validate
	coerced := coerceParamTypes(parsed.Params, currentParams)
	fixed, remaining := validateParams(coerced)

	debug := gin.H{
		"title":             "AI 参数优化完成",
		"returned_text":     parsed.Explanation,
		"fixed_messages":    fixed,
		"remaining_messages": remaining,
	}

	return gin.H{
		"success": true,
		"params":  coerced,
		"debug": gin.H{
			"human_summary": debug,
		},
		"explanation": parsed.Explanation,
	}
}

func buildGenerateCodeResult(response string, originalCode string) gin.H {
	code := extractPythonCode(response)
	if code == "" {
		code = response // use raw response if no code block found
	}

	// Run QC
	fixed, remaining := qcGeneratedCode(code, originalCode)

	explanation := extractExplanation(response)

	debug := gin.H{
		"title":              "AI 策略生成完成",
		"returned_text":      explanation,
		"fixed_messages":     fixed,
		"remaining_messages": remaining,
	}

	return gin.H{
		"success": true,
		"code":    code,
		"debug": gin.H{
			"human_summary": debug,
		},
	}
}

// ── Parameter Type Coercion ──

// paramDef defines the expected type and constraints for a parameter.
type paramDef struct {
	Type    string  // "integer", "number", "boolean", "select", "string"
	Min     float64
	Max     float64
	Options []string
}

var gridParamDefs = map[string]paramDef{
	"grid_count":           {Type: "integer", Min: 2, Max: 200},
	"upper_price":          {Type: "number", Min: 0},
	"lower_price":          {Type: "number", Min: 0},
	"order_qty":            {Type: "number", Min: 0},
	"grid_mode":            {Type: "select", Options: []string{"arithmetic", "geometric"}},
	"direction":            {Type: "select", Options: []string{"neutral", "long", "short"}},
	"atr_period":           {Type: "integer", Min: 1, Max: 200},
	"atr_mult":             {Type: "number", Min: 0.5, Max: 5},
	"min_width_pct":        {Type: "number", Min: 0},
	"max_shift_pct":        {Type: "number", Min: 0},
	"edge_pct":             {Type: "number", Min: 0},
	"adaptive_bounds":      {Type: "boolean"},
	"waterfall_protection": {Type: "boolean"},
	"waterfall_drop_pct":   {Type: "number", Min: 0.5, Max: 30},
	"waterfall_window":     {Type: "integer", Min: 1, Max: 200},
	"waterfall_cooldown":   {Type: "integer", Min: 1, Max: 500},
	"take_profit_pct":      {Type: "number", Min: 0, Max: 100},
	"stop_loss_pct":        {Type: "number", Min: 0, Max: 100},
	"trailing_tp":          {Type: "boolean"},
	"total_investment":     {Type: "number", Min: 0},
	"auto_start":           {Type: "boolean"},
}

func coerceParamTypes(aiParams map[string]any, currentParams map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range currentParams {
		result[k] = v
	}
	for key, raw := range aiParams {
		def, ok := gridParamDefs[key]
		if !ok {
			result[key] = raw // pass through unknown params
			continue
		}
		coerced := coerceValue(raw, def)
		if coerced != nil {
			result[key] = coerced
		}
	}
	return result
}

func coerceValue(raw any, def paramDef) any {
	switch def.Type {
	case "boolean":
		switch v := raw.(type) {
		case bool:
			return v
		case string:
			return v == "true" || v == "True" || v == "1"
		case float64:
			return v != 0
		}
	case "integer":
		switch v := raw.(type) {
		case float64:
			n := int(math.Round(v))
			if def.Min != 0 || def.Max != 0 {
				if float64(n) < def.Min {
					n = int(def.Min)
				}
				if float64(n) > def.Max {
					n = int(def.Max)
				}
			}
			return n
		case string:
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil
			}
			return n
		}
	case "number":
		switch v := raw.(type) {
		case float64:
			if def.Min != 0 || def.Max != 0 {
				if v < def.Min {
					v = def.Min
				}
				if v > def.Max {
					v = def.Max
				}
			}
			return math.Round(v*1000) / 1000
		case string:
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil
			}
			return n
		}
	case "select":
		switch v := raw.(type) {
		case string:
			for _, opt := range def.Options {
				if strings.EqualFold(v, opt) {
					return opt
				}
			}
		}
	}
	return raw
}

// validateParams checks coerced parameters for issues and returns (fixed, remaining) messages.
func validateParams(params map[string]any) ([]string, []string) {
	var fixed, remaining []string

	// Check price bounds
	upper, _ := toFloat(params["upper_price"])
	lower, _ := toFloat(params["lower_price"])
	if upper > 0 && lower > 0 && upper <= lower {
		if upper > 0 && lower > 0 {
			params["upper_price"] = lower * 1.2
			fixed = append(fixed, "价格上限不能低于下限，已自动调整上限 = 下限 × 1.2")
		}
	}

	// Check grid count
	if count, ok := toInt(params["grid_count"]); ok {
		if count < 2 {
			params["grid_count"] = 10
			fixed = append(fixed, "网格数量最少为2，已调整为10")
		}
		if count > 200 {
			params["grid_count"] = 200
			fixed = append(fixed, "网格数量最大为200，已调整为200")
		}
	}

	// Check waterfall params consistency
	if wp, ok := params["waterfall_protection"].(bool); ok && wp {
		cooldown, _ := toInt(params["waterfall_cooldown"])
		window, _ := toInt(params["waterfall_window"])
		if cooldown > 0 && window > 0 && cooldown < window {
			remaining = append(remaining, "冷却时间("+itoa(cooldown)+") 小于检测窗口("+itoa(window)+")，建议冷却时间 ≥ 检测窗口")
		}
	}

	// Check investment vs grid sizing
	investment, _ := toFloat(params["total_investment"])
	qty, _ := toFloat(params["order_qty"])
	count, _ := toInt(params["grid_count"])
	if investment > 0 && qty > 0 && count > 0 {
		totalNeeded := qty * float64(count)
		if totalNeeded > investment {
			remaining = append(remaining, fmt.Sprintf("总资金(%.0f)不足以覆盖%d格×%.4f = %.2f，可能只有部分网格会成交",
				investment, count, qty, totalNeeded))
		}
	}

	return fixed, remaining
}

// ── Code QC ──

func qcGeneratedCode(code, original string) ([]string, []string) {
	var fixed, remaining []string

	if code == "" {
		return fixed, []string{"生成的代码为空"}
	}

	// Auto-fix: remove markdown wrapper if present
	cleaned := strings.TrimSpace(code)

	// Check for required functions
	if !strings.Contains(cleaned, "on_init") {
		remaining = append(remaining, "缺少 on_init(ctx) 函数，策略无法初始化参数")
	}
	if !strings.Contains(cleaned, "on_bar") {
		remaining = append(remaining, "缺少 on_bar(ctx, bar) 函数，策略无法接收K线数据")
	}

	// Check for forbidden patterns (security)
	forbidden := []string{
		"import os", "import subprocess", "import socket",
		"os.system", "subprocess.", "exec(", "eval(",
		"__import__", "open(", "file(",
	}
	for _, pattern := range forbidden {
		if strings.Contains(cleaned, pattern) {
			cleaned = strings.ReplaceAll(cleaned, pattern, "# "+pattern+"  # removed for safety")
			fixed = append(fixed, "移除了不安全的调用: "+pattern)
		}
	}

	// Check for basic safety
	if !strings.Contains(cleaned, "stop_loss") && !strings.Contains(cleaned, "stoploss") &&
		!strings.Contains(cleaned, "stop loss") {
		remaining = append(remaining, "建议添加止损逻辑以控制风险")
	}
	if !strings.Contains(cleaned, "ctx.param") {
		remaining = append(remaining, "未使用 ctx.param() 管理参数，策略重启后会丢失状态")
	}

	// Check division by zero
	if strings.Contains(cleaned, "/ price") || strings.Contains(cleaned, "/price") {
		if !strings.Contains(cleaned, "price > 0") && !strings.Contains(cleaned, "price != 0") {
			remaining = append(remaining, "存在除以 price 的操作，建议添加 price > 0 检查")
		}
	}

	return fixed, remaining
}

// ── Extractors ──

func extractJSON(text string) string {
	// Try ```json ... ``` block first
	re := regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(.*?)\\n?```")
	matches := re.FindStringSubmatch(text)
	if len(matches) >= 2 && strings.TrimSpace(matches[1]) != "" {
		return strings.TrimSpace(matches[1])
	}

	// Try to find a bare JSON object
	start := strings.Index(text, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return ""
}

func extractPythonCode(text string) string {
	// Try ```python ... ``` block first
	re := regexp.MustCompile("(?s)```(?:python|py)?\\s*\\n?(.*?)\\n?```")
	matches := re.FindStringSubmatch(text)
	if len(matches) >= 2 && strings.TrimSpace(matches[1]) != "" {
		return strings.TrimSpace(matches[1])
	}

	// Try bare ``` ... ``` block
	re2 := regexp.MustCompile("(?s)```\\s*\\n?(.*?)\\n?```")
	matches2 := re2.FindStringSubmatch(text)
	if len(matches2) >= 2 && strings.TrimSpace(matches2[1]) != "" {
		return strings.TrimSpace(matches2[1])
	}

	return ""
}

func extractExplanation(text string) string {
	// Remove code blocks and return the first meaningful paragraph
	text = regexp.MustCompile("(?s)```.*?```").ReplaceAllString(text, "")
	lines := strings.Split(strings.TrimSpace(text), "\n")
	var meaningful []string
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t != "" && !strings.HasPrefix(t, "#") {
			meaningful = append(meaningful, t)
		}
	}
	if len(meaningful) > 0 {
		result := strings.Join(meaningful, "\n")
		if len(result) > 500 {
			result = result[:500] + "..."
		}
		return result
	}
	return ""
}

// ── Helpers ──

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n# ... (truncated)"
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	}
	return 0, false
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	}
	return 0, false
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
