package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── AI Agent 对话端点：POST /api/agent/chat ──
// tool-calling 循环（最多 maxIterations 轮）+ 可选 SSE 流式输出。
// 鉴权双路径：Web JWT（aiBotUserID）或 X-Agent-Token（agent token，带 scope 过滤 + 审计）。
// SSE 事件契约与 web/src/lib/api.ts 的 agentChatApi 逐字对应：
//
//	默认事件 data: <文本分片原文（前端直接追加）>
//	event: tool_call  data: {"name","args_summary","status","result_summary"}
//	event: done        data: {"content","tool_calls":[...]}
//	event: error       data: {"message"}
//	流结束            data: [DONE]

const (
	agentChatMaxIterations = 5                 // tool-calling 最大迭代轮数
	agentChatToolTimeout   = 30 * time.Second  // 单次工具执行超时
	agentChatResultLimit   = 2000              // 工具结果回传模型的截断长度
	agentChatSummaryLimit  = 80                // args_summary / result_summary 截断长度
)

// errAgentChatAborted 客户端中途断开，循环静默终止（不发送 done/error 事件）。
var errAgentChatAborted = errors.New("client connection closed")

// agentChatSystemPromptHead/Tail 系统提示词骨架；可用工具清单按请求（过滤后）动态拼入。
const agentChatSystemPromptHead = `你是"小天量化助手"，小天量化交易平台的 AI 助手。你的职责：
1. 帮助用户进行交易分析（行情、K 线、策略、回测结果）；
2. 帮助用户管理交易机器人与策略（查看、部署、启停、删除）；
3. 执行模拟盘操作（模拟下单、撤单、查看持仓与余额）。

你可以使用以下工具来完成任务：
`

const agentChatSystemPromptTail = `
铁律：
- 本期所有交易类工具均为模拟盘（paper）与配置类操作，不会动用真实资金；即便如此，执行下单类操作前也应先向用户说明其影响。
- 若未来接入实盘资金操作，必须先充分说明风险并征得用户明确确认后方可执行。
- 不确定的事情直接说不知道，不要编造数据。
- 回答使用中文，简洁清晰，可使用 markdown 格式。`

// agentChatRequest 对话请求体。
type agentChatRequest struct {
	Messages []agentChatMessage `json:"messages"`
	Stream   bool               `json:"stream"`
}

type agentChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// agentToolCallRecord 工具调用记录（tool_call / done 事件与最终 JSON 共用）。
type agentToolCallRecord struct {
	Name          string `json:"name"`
	ArgsSummary   string `json:"args_summary,omitempty"`
	Status        string `json:"status"` // running | done
	ResultSummary string `json:"result_summary,omitempty"`
}

// agentToolHandlers 工具名 → ToolContext 方法映射（与 agent.MCPServer 注册表保持一致）。
var agentToolHandlers = map[string]func(tc *agent.ToolContext, ctx context.Context, args map[string]any) (any, error){
	"get_market_data":   (*agent.ToolContext).GetMarketData,
	"get_klines":        (*agent.ToolContext).GetKlines,
	"place_paper_order": (*agent.ToolContext).PlacePaperOrder,
	"get_orders":        (*agent.ToolContext).GetOrders,
	"cancel_order":      (*agent.ToolContext).CancelOrder,
	"get_positions":     (*agent.ToolContext).GetPositions,
	"get_balance":       (*agent.ToolContext).GetBalance,
	"list_strategies":   (*agent.ToolContext).ListStrategies,
	"deploy_strategy":   (*agent.ToolContext).DeployStrategy,
	"start_strategy":    (*agent.ToolContext).StartStrategy,
	"stop_strategy":     (*agent.ToolContext).StopStrategy,
	"delete_strategy":   (*agent.ToolContext).DeleteStrategy,
	"run_backtest":      (*agent.ToolContext).RunBacktest,
	"list_backtests":    (*agent.ToolContext).ListBacktests,
	"list_markets":      (*agent.ToolContext).ListMarkets,
	"get_stats":         (*agent.ToolContext).GetStats,
}

// AgentChatStream 处理 POST /api/agent/chat。
func AgentChatStream(c *gin.Context) {
	var reqBody agentChatRequest
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	history, ok := buildAgentChatHistory(reqBody.Messages)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "messages must be non-empty and include at least one user message"})
		return
	}

	// ── 鉴权：X-Agent-Token 优先，其次 JWT 用户 ──
	userID, tokenID, scopes, usedToken, ok := resolveAgentChatIdentity(c)
	if !ok {
		return // 已回 401
	}

	// ── Provider：agent.ai.provider，默认 deepseek ──
	provider, providerName := configuredAgentAIProvider()
	if provider == nil || provider.APIKey == "" {
		c.JSON(http.StatusOK, gin.H{
			"status": "error",
			"reply":  fmt.Sprintf("AI provider '%s' not configured. Please set API key in Settings → AI.", providerName),
		})
		return
	}

	// ── 工具：agent token 按 scope 过滤；Web JWT 使用全部工具 ──
	tools := agent.AllTools()
	if usedToken {
		tools = agent.GetTokenManager().FilterTools(tools, scopes)
	}

	if reqBody.Stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
	}

	// 浅拷贝全局 ToolContext：UserID 按请求覆盖，底层依赖仍共享生产单例。
	tc := *agent.GetToolContext()
	tc.UserID = userID

	r := &agentChatRunner{
		c:        c,
		provider: provider,
		stream:   reqBody.Stream,
		tokenID:  tokenID,
		toolCtx:  &tc,
		history:  history,
		aiTools:  toAITools(tools),
		allowed:  allowedToolNames(tools),
		records:  []agentToolCallRecord{},
	}

	finalContent, err := r.run()
	if err == errAgentChatAborted {
		return // 客户端断开，静默结束
	}
	if err != nil {
		if reqBody.Stream {
			r.emitEvent("error", gin.H{"message": err.Error()})
			fmt.Fprint(c.Writer, "data: [DONE]\n\n")
			c.Writer.Flush()
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "error", "reply": "AI service error: " + err.Error()})
		return
	}

	if reqBody.Stream {
		r.emitEvent("done", gin.H{"content": finalContent, "tool_calls": r.records})
		fmt.Fprint(c.Writer, "data: [DONE]\n\n")
		c.Writer.Flush()
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": finalContent, "tool_calls": r.records})
}

// configuredAgentAIProvider 读取 store 配置 agent.ai.provider（默认 deepseek）。
func configuredAgentAIProvider() (*ai.Provider, string) {
	cfg := store.GetConfig()
	agentCfg, _ := cfg["agent"].(map[string]any)
	aiCfg, _ := agentCfg["ai"].(map[string]any)
	providerName := "deepseek"
	if aiCfg != nil {
		if p, ok := aiCfg["provider"].(string); ok && p != "" {
			providerName = p
		}
	}
	return ai.GetProvider(providerName), providerName
}

// resolveAgentChatIdentity 双路径鉴权：X-Agent-Token 优先于 JWT。
// 返回 (userID, tokenID, scopes, usedToken, ok)；!ok 时已写入 401 响应。
func resolveAgentChatIdentity(c *gin.Context) (uint64, int, []string, bool, bool) {
	if h := strings.TrimSpace(c.GetHeader("X-Agent-Token")); h != "" {
		rec, err := agent.GetTokenManager().ValidateToken(h)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid agent token"})
			return 0, 0, nil, false, false
		}
		return uint64(rec.UserID), rec.ID, agent.ParseTokenScopes(rec.Scopes), true, true
	}
	return uint64(aiBotUserID(c)), 0, nil, false, true
}

// buildAgentChatHistory 构造模型历史：[system] + 客户端消息（角色白名单校验）。
func buildAgentChatHistory(messages []agentChatMessage) ([]ai.ChatMessage, bool) {
	hasUser := false
	for _, m := range messages {
		switch m.Role {
		case "user":
			hasUser = true
		case "assistant", "system":
		default:
			return nil, false
		}
	}
	if !hasUser {
		return nil, false
	}
	history := make([]ai.ChatMessage, 0, len(messages)+1)
	for _, m := range messages {
		history = append(history, ai.ChatMessage{Role: ai.Role(m.Role), Content: m.Content})
	}
	return history, true
}

// toAITools 把 agent.Tool 转为 ai.Tool（OpenAI function 描述格式）。
func toAITools(tools []agent.Tool) []ai.Tool {
	out := make([]ai.Tool, 0, len(tools))
	for _, t := range tools {
		out = append(out, ai.Tool{
			Type: "function",
			Function: ai.ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Schema,
			},
		})
	}
	return out
}

// allowedToolNames 构建允许执行的工具名集合（agent token 按 scope 过滤后的白名单；
// 模型可能请求未下发的工具，执行前必须再校验一道）。
func allowedToolNames(tools []agent.Tool) map[string]bool {
	out := make(map[string]bool, len(tools))
	for _, t := range tools {
		out[t.Name] = true
	}
	return out
}

// buildAgentChatSystemPrompt 拼系统提示词（工具清单按过滤后结果动态生成）。
func buildAgentChatSystemPrompt(tools []agent.Tool) string {
	var b strings.Builder
	b.WriteString(agentChatSystemPromptHead)
	for _, t := range tools {
		fmt.Fprintf(&b, "- %s：%s\n", t.Name, t.Description)
	}
	b.WriteString(agentChatSystemPromptTail)
	return b.String()
}

// truncateAgentChat 按 rune 截断（避免切断多字节字符）。
func truncateAgentChat(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ── tool-calling 循环执行器 ──

type agentChatRunner struct {
	c        *gin.Context
	provider *ai.Provider
	stream   bool
	tokenID  int
	toolCtx  *agent.ToolContext
	history  []ai.ChatMessage
	aiTools  []ai.Tool
	allowed  map[string]bool
	records  []agentToolCallRecord
}

// run 执行 tool-calling 循环，返回最终答案文本。
func (r *agentChatRunner) run() (string, error) {
	// 系统提示词放在这里拼装：首轮调用前工具集已确定。
	r.history = append([]ai.ChatMessage{{Role: ai.RoleSystem, Content: buildAgentChatSystemPrompt(r.agentToolsDesc())}}, r.history...)

	finalContent := ""
	completed := false
	for i := 0; i < agentChatMaxIterations; i++ {
		if r.c.Request.Context().Err() != nil {
			return "", errAgentChatAborted
		}
		fullText, toolCalls, err := r.callModel()
		if err != nil {
			return "", err
		}
		if len(toolCalls) == 0 {
			finalContent = fullText
			completed = true
			break
		}
		// 本轮 assistant 消息（工具调用）入历史，再逐个执行并回传 tool 结果。
		r.history = append(r.history, ai.ChatMessage{Role: ai.RoleAssistant, Content: "", ToolCalls: toolCalls})
		for _, tc := range toolCalls {
			if r.c.Request.Context().Err() != nil {
				return "", errAgentChatAborted
			}
			content := r.executeTool(tc)
			r.history = append(r.history, ai.ChatMessage{
				Role:       ai.RoleTool,
				Content:    content,
				ToolCallID: tc.ID,
				ToolResult: &ai.ToolResult{ToolCallID: tc.ID, Name: tc.Name, Content: content},
			})
		}
	}
	if !completed {
		finalContent = "任务较复杂，已达最大执行步数，请把需求拆小后重试"
	}
	return finalContent, nil
}

// agentToolsDesc 还原当前 aiTools 对应的 agent.Tool 描述（仅用于拼系统提示词）。
func (r *agentChatRunner) agentToolsDesc() []agent.Tool {
	out := make([]agent.Tool, 0, len(r.aiTools))
	for _, t := range r.aiTools {
		out = append(out, agent.Tool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
		})
	}
	return out
}

// callModel 单轮模型调用：流式走 ChatCompletionStreamEx（回调转发 SSE delta），
// 非流式走 ChatCompletion。
func (r *agentChatRunner) callModel() (string, []ai.ToolCall, error) {
	req := ai.CompletionRequest{
		Model:      r.provider.Model,
		Messages:   r.history,
		Tools:      r.aiTools,
		ToolChoice: "auto",
	}
	if r.stream {
		fullText, toolCalls, err := r.provider.ChatCompletionStreamEx(req, func(delta string) {
			r.emitDelta(delta)
		})
		return fullText, toolCalls, err
	}
	resp, err := r.provider.ChatCompletion(req)
	if err != nil {
		return "", nil, err
	}
	fullText := ""
	if resp != nil && len(resp.Choices) > 0 {
		fullText = resp.Choices[0].Message.Content
	}
	return fullText, ai.ExtractToolCalls(resp), nil
}

// executeTool 执行单个工具调用：发 running/done 事件、回传结果文本（截断）、写审计。
func (r *agentChatRunner) executeTool(tc ai.ToolCall) string {
	argsSummary := truncateAgentChat(tc.Arguments, agentChatSummaryLimit)
	rec := agentToolCallRecord{Name: tc.Name, ArgsSummary: argsSummary, Status: "running"}
	r.emitEvent("tool_call", rec)

	statusCode := http.StatusOK
	var content string

	handler, found := agentToolHandlers[tc.Name]
	switch {
	case !found:
		statusCode = http.StatusNotImplemented
		content = "ERROR: unknown tool: " + tc.Name
	case !r.allowed[tc.Name]:
		// scope 过滤是双保险：即使模型请求了未下发的工具也拒绝执行
		statusCode = http.StatusForbidden
		content = "ERROR: tool " + tc.Name + " not permitted for this token"
	default:
		var args map[string]any
		if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
			statusCode = http.StatusBadRequest
			content = "ERROR: invalid tool arguments: " + err.Error()
		} else {
			if args == nil {
				args = map[string]any{}
			}
			ctx, cancel := context.WithTimeout(r.c.Request.Context(), agentChatToolTimeout)
			defer cancel()
			result, err := handler(r.toolCtx, ctx, args)
			if err != nil {
				statusCode = http.StatusInternalServerError
				content = "ERROR: " + err.Error()
			} else {
				b, mErr := json.Marshal(result)
				if mErr != nil {
					statusCode = http.StatusInternalServerError
					content = "ERROR: " + mErr.Error()
				} else {
					content = string(b)
					if len(content) > agentChatResultLimit {
						content = content[:agentChatResultLimit] + "…[truncated]"
					}
				}
			}
		}
	}

	rec.Status = "done"
	rec.ResultSummary = truncateAgentChat(content, agentChatSummaryLimit)
	r.emitEvent("tool_call", rec)
	r.records = append(r.records, rec)
	r.writeAudit(tc.Name, argsSummary, statusCode)
	return content
}

// writeAudit 每次工具执行写 agent_audit_log（JWT 路径 tokenID=0）。
func (r *agentChatRunner) writeAudit(tool, argsSummary string, statusCode int) {
	agent.GetTokenManager().LogAccess(
		r.tokenID, tool, "/agent/chat/tools/call", "TOOL",
		argsSummary, statusCode, r.c.ClientIP(), r.c.Request.UserAgent(),
	)
}

// ── SSE 写出 ──

// emitDelta 默认事件：data 为文本分片原文（前端直接追加）；多行分片拆成多条 data 行。
func (r *agentChatRunner) emitDelta(delta string) {
	if !r.stream {
		return
	}
	for _, line := range strings.Split(delta, "\n") {
		fmt.Fprintf(r.c.Writer, "data: %s\n", line)
	}
	fmt.Fprint(r.c.Writer, "\n")
	r.c.Writer.Flush()
}

// emitEvent 具名事件（tool_call / done / error），payload 序列化为单行 JSON。
func (r *agentChatRunner) emitEvent(event string, payload any) {
	if !r.stream {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(r.c.Writer, "event: %s\ndata: %s\n\n", event, data)
	r.c.Writer.Flush()
}
