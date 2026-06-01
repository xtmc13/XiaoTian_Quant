package indicator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Sandbox HTTP Handlers ─────────────────────────────────────────

// SandboxExecute godoc
// POST /api/indicator/execute
// Executes indicator code in the Python sandbox.
func SandboxExecute(c *gin.Context) {
	var req struct {
		Code   string         `json:"code" binding:"required"`
		Params map[string]any `json:"params"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "code is required", "data": nil})
		return
	}

	sandbox := DefaultSandboxConfig()
	result, err := sandbox.Execute(req.Code, req.Params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 0,
			"msg":  err.Error(),
			"data": gin.H{"success": false, "msg": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 1,
		"msg":  result.Msg,
		"data": result,
	})
}

// SandboxAnalyze godoc
// POST /api/indicator/analyze
// Performs deep static analysis via Python sandbox + Go-side checks.
func SandboxAnalyze(c *gin.Context) {
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "code is required", "data": nil})
		return
	}

	// First, run Go-side fast static analysis
	goResult := ValidateCode(req.Code)
	var allHints []ValidationHint
	allHints = append(allHints, goResult.Hints...)

	// Then, run Python sandbox analyzer for deeper checks
	sandbox := DefaultSandboxConfig()
	pyResult, err := sandbox.Analyze(req.Code)
	if err == nil && pyResult != nil {
		// Deduplicate by code
		seen := make(map[string]bool)
		for _, h := range allHints {
			seen[h.Code] = true
		}
		for _, h := range pyResult.Hints {
			if !seen[h.Code] {
				allHints = append(allHints, h)
				seen[h.Code] = true
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 1,
		"msg":  "analysis complete",
		"data": gin.H{
			"success":      len(filterErrors(allHints)) == 0,
			"msg":          "",
			"hints":        allHints,
			"plotsCount":   0,
			"signalsCount": 0,
		},
	})
}

func filterErrors(hints []ValidationHint) []ValidationHint {
	var errs []ValidationHint
	for _, h := range hints {
		if h.Severity == "error" {
			errs = append(errs, h)
		}
	}
	return errs
}

// ── AI SSE Stream Generation ──────────────────────────────────────

// IndicatorAIGenerate godoc
// POST /api/indicator/ai-generate
// SSE stream for AI indicator code generation with auto-fix loop.
//
// Events:
//   - code_chunk: streaming code tokens from LLM
//   - status:     "generating" | "validating" | "auto_fixing" | "completed"
//   - validation: validation result JSON
//   - code:       full replacement code after auto-fix
//   - debug:      debug info JSON
//   - done:       stream end marker
func IndicatorAIGenerate(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	_, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized"})
		return
	}

	var req struct {
		Prompt       string `json:"prompt" binding:"required"`
		ExistingCode string `json:"existingCode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "prompt is required"})
		return
	}

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Header("Connection", "keep-alive")

	sendEvent := func(event, data string) {
		c.SSEvent(event, data)
		c.Writer.Flush()
	}

	// ── 1. Generate code (streaming if supported) ──
	sendEvent("status", "generating")

	provider := getActiveAIProvider()
	var codeText string

	if provider != nil && provider.SupportsStream() {
		// True streaming from LLM to client
		var sb strings.Builder
		err := streamCodeViaLLM(provider, req.Prompt, req.ExistingCode, func(chunk string) {
			sb.WriteString(chunk)
			sendEvent("code_chunk", chunk)
		})
		if err != nil {
			sendEvent("status", "error")
			sendEvent("debug", fmt.Sprintf(`{"error": "%s"}`, jsonEscape(err.Error())))
			sendEvent("done", "")
			return
		}
		codeText = cleanMarkdownFences(sb.String())
	} else {
		// Non-streaming fallback
		codeText = generateCodeViaLLM(req.Prompt, req.ExistingCode)
		// Simulate streaming by chunking
		chunkSize := 80
		for i := 0; i < len(codeText); i += chunkSize {
			end := i + chunkSize
			if end > len(codeText) {
				end = len(codeText)
			}
			sendEvent("code_chunk", codeText[i:end])
		}
	}

	if codeText == "" {
		codeText = fallbackTemplateCode(req.Prompt)
		for _, chunk := range chunkString(codeText, 80) {
			sendEvent("code_chunk", chunk)
		}
	}

	// ── 2. Validation + Auto-fix loop ──
	sendEvent("status", "validating")

	debugInfo := map[string]any{
		"auto_fix_applied":   false,
		"auto_fix_succeeded": false,
		"rounds":             0,
	}

	// Initial validation: Go static + Python sandbox execution
	validation, execResult := validateAndExec(codeText)
	validationJSON, _ := json.Marshal(validation)
	sendEvent("validation", string(validationJSON))

	if validation.Success && execResult.Success {
		debugInfo["auto_fix_succeeded"] = true
		debugInfo["returned_candidate"] = "initial"
		debugInfo["hint_count"] = len(validation.Hints)
		sendDebugAndDone(sendEvent, debugInfo)
		return
	}

	// ── 3. Auto-fix loop (max 3 rounds) ──
	debugInfo["auto_fix_applied"] = true

	for round := 1; round <= 3; round++ {
		debugInfo["rounds"] = round
		sendEvent("status", fmt.Sprintf("auto_fixing_round_%d", round))

		repaired := repairCodeViaLLM(req.Prompt, codeText, validation.Hints, execResult)
		if repaired == codeText || repaired == "" {
			break // No change or empty, stop
		}

		// Validate repaired code
		validation, execResult = validateAndExec(repaired)
		validationJSON, _ := json.Marshal(validation)
		sendEvent("validation", string(validationJSON))

		if validation.Success && execResult.Success {
			debugInfo["auto_fix_succeeded"] = true
			debugInfo["returned_candidate"] = "repaired"
			debugInfo["hint_count"] = len(validation.Hints)
			// Send the final repaired code as a single replacement event
			codeJSON, _ := json.Marshal(map[string]string{"code": repaired})
			sendEvent("code", string(codeJSON))
			sendDebugAndDone(sendEvent, debugInfo)
			return
		}

		codeText = repaired
	}

	// 4. Return best attempt (initial or last repaired)
	debugInfo["auto_fix_succeeded"] = false
	debugInfo["returned_candidate"] = "best_attempt"
	debugInfo["hint_count"] = len(validation.Hints)
	sendDebugAndDone(sendEvent, debugInfo)
}

// validateAndExec runs both Go static validation and Python sandbox execution.
func validateAndExec(code string) (ValidateResult, *SandboxExecuteResponse) {
	// Go static validation
	validation := ValidateCode(code)

	// Python sandbox execution (only if static passes or no hard errors)
	var execResult *SandboxExecuteResponse
	if validation.Success || len(filterErrors(validation.Hints)) == 0 {
		sandbox := DefaultSandboxConfig()
		res, err := sandbox.Execute(code, nil)
		if err == nil && res != nil {
			execResult = res
			if !res.Success {
				validation.Success = false
				validation.Msg = res.Msg
				validation.ErrorType = res.ErrorType
				validation.Hints = append(validation.Hints, ValidationHint{
					Severity: "error",
					Code:     "SANDBOX_EXECUTION_ERROR",
					Params:   map[string]any{"error": res.Error, "type": res.ErrorType},
				})
			}
		}
	}

	return validation, execResult
}

func sendDebugAndDone(sendEvent func(string, string), debugInfo map[string]any) {
	debugJSON, _ := json.Marshal(debugInfo)
	sendEvent("debug", string(debugJSON))
	sendEvent("done", "")
}

func chunkString(s string, size int) []string {
	var chunks []string
	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
	}
	return chunks
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

// ── LLM Helpers ───────────────────────────────────────────────────

// getActiveAIProvider returns the first available AI provider with an API key.
func getActiveAIProvider() *ai.Provider {
	cfg := store.GetConfig()
	providerName := ""

	if aiCfg, ok := cfg["ai"].(map[string]any); ok {
		if defaults, ok := aiCfg["defaults"].(map[string]any); ok {
			providerName = getString(defaults, "provider", "")
		}
		if providerName == "" {
			providerName = getString(aiCfg, "provider", "")
		}
	}

	if providerName != "" {
		if p := ai.GetProvider(providerName); p != nil && p.APIKey != "" {
			return p
		}
	}

	for _, name := range []string{"deepseek", "openai", "qwen", "hunyuan", "glm", "kimi", "claude", "gemini"} {
		if p := ai.GetProvider(name); p != nil && p.APIKey != "" {
			return p
		}
	}
	return nil
}

func getString(m map[string]any, key, def string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return def
}

// streamCodeViaLLM uses the provider's streaming API to generate code token by token.
func streamCodeViaLLM(provider *ai.Provider, prompt, existingCode string, onChunk func(string)) error {
	systemPrompt := buildSystemPrompt()
	userPrompt := prompt
	if existingCode != "" {
		userPrompt = fmt.Sprintf("# Existing code:\n```python\n%s\n```\n\n# Request: %s", existingCode, prompt)
	}

	return provider.ChatCompletionStream(ai.CompletionRequest{
		Messages: []ai.ChatMessage{
			{Role: ai.RoleSystem, Content: systemPrompt},
			{Role: ai.RoleUser, Content: userPrompt},
		},
		Temperature: 0.7,
	}, onChunk)
}

// generateCodeViaLLM calls the LLM provider to generate indicator code (non-streaming).
func generateCodeViaLLM(prompt, existingCode string) string {
	provider := getActiveAIProvider()
	if provider == nil {
		return fallbackTemplateCode(prompt)
	}

	systemPrompt := buildSystemPrompt()
	userPrompt := prompt
	if existingCode != "" {
		userPrompt = fmt.Sprintf("# Existing code:\n```python\n%s\n```\n\n# Request: %s", existingCode, prompt)
	}

	resp, err := provider.ChatCompletion(ai.CompletionRequest{
		Messages: []ai.ChatMessage{
			{Role: ai.RoleSystem, Content: systemPrompt},
			{Role: ai.RoleUser, Content: userPrompt},
		},
		Temperature: 0.7,
	})
	if err != nil || resp == nil || len(resp.Choices) == 0 {
		return fallbackTemplateCode(prompt)
	}

	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	content = cleanMarkdownFences(content)
	if content == "" {
		return fallbackTemplateCode(prompt)
	}
	return content
}

// repairCodeViaLLM asks LLM to fix validation issues.
func repairCodeViaLLM(prompt, badCode string, hints []ValidationHint, execResult *SandboxExecuteResponse) string {
	provider := getActiveAIProvider()
	if provider == nil {
		return badCode
	}

	issuesText := formatHintsForRepair(hints)
	if execResult != nil && !execResult.Success {
		issuesText += fmt.Sprintf("\n- SANDBOX_EXECUTION_ERROR: %s (%s)", execResult.Msg, execResult.ErrorType)
		if execResult.Error != "" {
			issuesText += fmt.Sprintf("\n  Details: %s", truncate(execResult.Error, 500))
		}
	}

	repairPrompt := fmt.Sprintf(
		"Fix the following QuantDinger indicator code while preserving the user's trading idea.\n\n"+
			"# User request\n%s\n\n"+
			"# Validation issues to fix\n%s\n\n"+
			"# Current code\n```python\n%s\n```\n\n"+
			"Return one full replacement script only, no markdown, no explanation.",
		prompt, issuesText, badCode,
	)

	resp, err := provider.ChatCompletion(ai.CompletionRequest{
		Messages: []ai.ChatMessage{
			{Role: ai.RoleSystem, Content: buildSystemPrompt()},
			{Role: ai.RoleUser, Content: repairPrompt},
		},
		Temperature: 0.2,
	})
	if err != nil || resp == nil || len(resp.Choices) == 0 {
		return badCode
	}

	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	content = cleanMarkdownFences(content)
	if content == "" {
		return badCode
	}
	return content
}

func cleanMarkdownFences(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```python") {
		content = strings.TrimPrefix(content, "```python")
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
	}
	content = strings.TrimSuffix(content, "```")
	return strings.TrimSpace(content)
}

func formatHintsForRepair(hints []ValidationHint) string {
	var lines []string
	for _, h := range hints {
		line := fmt.Sprintf("- %s", h.Code)
		if len(h.Params) > 0 {
			paramsJSON, _ := json.Marshal(h.Params)
			line += fmt.Sprintf(": %s", string(paramsJSON))
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func fallbackTemplateCode(prompt string) string {
	return fmt.Sprintf(`my_indicator_name = "Custom Indicator"
my_indicator_description = "%s"

# @strategy stopLossPct 0.03
# @strategy takeProfitPct 0.06
# @strategy tradeDirection long

import talib

df = df.copy()
close = df['close'].values
rsi = talib.RSI(close, timeperiod=14)

raw_buy = pd.Series(rsi < 30, index=df.index)
raw_sell = pd.Series(rsi > 70, index=df.index)

df['buy'] = raw_buy.fillna(False).astype(bool)
df['sell'] = raw_sell.fillna(False).astype(bool)

output = {
    'name': my_indicator_name,
    'plots': [
        {'name': 'RSI(14)', 'data': rsi.tolist(), 'color': '#faad14', 'overlay': False}
    ],
    'signals': [
        {'type': 'buy', 'text': 'B', 'data': [df['low'].iloc[i]*0.995 if df['buy'].iloc[i] else None for i in range(len(df))], 'color': '#00E676'},
        {'type': 'sell', 'text': 'S', 'data': [df['high'].iloc[i]*1.005 if df['sell'].iloc[i] else None for i in range(len(df))], 'color': '#FF5252'}
    ]
}
`, strings.ReplaceAll(prompt, "\n", " ")[:200])
}

// buildSystemPrompt returns the QuantDinger indicator system prompt.
func buildSystemPrompt() string {
	return `You write production-ready QuantDinger indicator scripts in Python.

Rules:
- Environment: sandboxed, no network, no file I/O, no subprocess.
- pd and np are already available. Do not import pandas or numpy. Avoid any import unless unavoidable; never import os, sys, requests, socket, subprocess, threading, sqlite3, multiprocessing.
- Do not use: eval, exec, compile, open, __import__, getattr/setattr/delattr on untrusted names, globals, vars, dir.
- Work vectorized with pandas on df where possible; avoid O(n) Python loops over every row for core series.

Series vs ndarray contract (critical):
- np.where(...), np.maximum(...), np.minimum(...), np.abs(...) on a Series may return either a Series or an ndarray. Never chain pandas methods on their result without coercing. Use: pd.Series(arr, index=df.index).
- Prefer pandas-native operators: np.where(cond,a,b) -> a.where(cond,b); np.maximum(s,0) -> s.clip(lower=0); np.minimum(s,k) -> s.clip(upper=k); np.abs(s) -> s.abs().

Input: df
- df is a pandas DataFrame aligned to K-line bars.
- You must start mutating with: df = df.copy()
- Expected columns: open, high, low, close, volume.

Required globals:
1. my_indicator_name = "..."
2. my_indicator_description = "..."

Backtest contract:
- df['buy'] — True on bars where a new long entry signal is allowed (edge-triggered).
- df['sell'] — True on bars where a new exit / short entry signal is allowed.
- Edge-trigger: raw_buy = (...condition...); buy = raw_buy.fillna(False) & (~raw_buy.shift(1).fillna(False)). Same for sell.

Chart output: output dict
output = { 'name': ..., 'plots': [...], 'signals': [...] }
- plots: list of dicts with name, data (list len(df)), color, overlay (bool).
- signals: list of dicts with type ('buy'|'sell'), text, data (list len(df), None or float price), color.

Optional parameters: # @param name type default description
Example: # @param rsi_len int 14 RSI period
You must read declared params via: param_name = params.get('param_name', default_value)

Strategy defaults: # @strategy key value
Supported: stopLossPct, takeProfitPct, entryPct, trailingEnabled, trailingStopPct, trailingActivationPct, tradeDirection (long|short|both).

Return only valid Python source: no markdown fences, no explanation before or after.`
}
