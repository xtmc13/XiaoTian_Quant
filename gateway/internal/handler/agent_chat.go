package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── AI Agent 对话端点：POST /api/agent/chat ──
// tool-calling 循环（最多 maxIterations 轮）+ 可选 SSE 流式输出。
// 鉴权双路径：Web JWT（aiBotUserID）或 X-Agent-Token（agent token，带 scope 过滤 + 审计）。
// 支持多会话持久化（conversation_id / 自动建会话）、reasoning_content 透传与按会话模型覆盖。
// SSE 事件契约与 web/src/lib/api.ts 的 agentChatApi 逐字对应：
//
//	默认事件 data: <文本分片原文（前端直接追加）>
//	event: reasoning   data: <JSON 编码字符串（前端 JSON.parse 后追加）>
//	event: tool_call   data: {"name","args_summary","status","result_summary"}
//	event: conversation data: {"id","title"}（自动/指定会话，done 之前发一次）
//	event: done        data: {"content","tool_calls":[...],"reasoning","conversation_id"}
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

// agentChatRequest 对话请求体。conversation_id / model / regenerate / replace_history
// 全部可选（向后兼容）：空 conversation_id 表示自动新建会话。
type agentChatRequest struct {
	Messages       []agentChatMessage `json:"messages"`
	Stream         bool               `json:"stream"`
	ConversationID string             `json:"conversation_id"`
	// Model 覆盖："provider" 或 "provider:model"；空=configuredAgentAIProvider 默认链。
	Model string `json:"model"`
	// Regenerate：删除该会话最后一条 assistant 消息，用 messages 最后一条 user 重跑。
	Regenerate bool `json:"regenerate"`
	// ReplaceHistory：用请求 messages 整体替换该会话历史（编辑消息场景），再跑最后一轮。
	ReplaceHistory bool `json:"replace_history"`
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

	// ── 鉴权：X-Agent-Token 优先，其次 JWT 用户 ──
	userID, tokenID, scopes, usedToken, ok := resolveAgentChatIdentity(c)
	if !ok {
		return // 已回 401
	}

	// ── 会话解析：历史构造 + regenerate / replace_history（先于 provider 检查，
	// 保证请求校验 400/404 不因 provider 未配置而被掩盖）。
	// 指定会话时按 conversation_id 加互斥锁再读历史：防止并发对话历史交错与落库乱序
	// （自动新建会话无竞争，不加锁）。
	repo := store.DefaultAgentChatRepo()
	if reqBody.ConversationID != "" {
		unlock := lockAgentConversation(reqBody.ConversationID)
		defer unlock()
	}
	plan, history, ok := resolveAgentChatSession(c, repo, userID, &reqBody)
	if !ok {
		return // 已回 404 / 400
	}

	// ── Provider：统一解析链（支持 "provider" / "provider:model" 覆盖）──
	provider, providerName := configuredAgentAIProviderWithOverride(reqBody.Model)
	if provider == nil || provider.APIKey == "" {
		msg := fmt.Sprintf("AI provider '%s' 未配置 API Key，请到 设置 → AI 模型 填写并保存", providerName)
		if reqBody.Stream {
			// 流式请求必须回 SSE error 事件：前端按事件协议解析，回 JSON 会静默落空。
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("Connection", "keep-alive")
			c.Header("X-Accel-Buffering", "no")
			fmt.Fprintf(c.Writer, "event: error\ndata: {\"message\":%q}\n\ndata: [DONE]\n\n", msg)
			c.Writer.Flush()
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "error", "reply": msg})
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
		c:           c,
		provider:    provider,
		stream:      reqBody.Stream,
		tokenID:     tokenID,
		userID:      userID,
		toolCtx:     &tc,
		history:     history,
		aiTools:     toAITools(tools),
		allowed:     allowedToolNames(tools),
		records:     []agentToolCallRecord{},
		convID:      plan.convID,
		convTitle:   plan.convTitle,
		userContent: plan.userContent,
		skipUserMsg: plan.skipUserMsg,
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

	// ── 落库：user + assistant（content / reasoning / tool_calls JSON）──
	r.persistConversation(repo, finalContent)

	if reqBody.Stream {
		// conversation 事件在 done 之前发一次（新建会话时前端据此更新当前会话 id）。
		r.emitEvent("conversation", gin.H{"id": r.convID, "title": r.convTitle})
		r.emitEvent("done", gin.H{
			"content":         finalContent,
			"tool_calls":      r.records,
			"reasoning":       r.reasoning,
			"conversation_id": r.convID,
		})
		fmt.Fprint(c.Writer, "data: [DONE]\n\n")
		c.Writer.Flush()
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"content":         finalContent,
		"tool_calls":      r.records,
		"reasoning":       r.reasoning,
		"conversation_id": r.convID,
	})
}

// agentChatSessionPlan 会话解析结果：runner 落库所需的会话元信息。
type agentChatSessionPlan struct {
	convID      string // 会话 id（空 = 自动新建，落库时创建）
	convTitle   string // 新建会话标题（首条 user 消息截 20 字）
	userContent string // 本轮 user 消息内容（落库用）
	skipUserMsg bool   // replace_history：user 消息已整体入库，只补 assistant
}

// resolveAgentChatSession 按请求构造会话历史：
//   - 无 conversation_id：旧行为（请求 messages 全量，校验至少一条 user），自动建会话；
//   - 有 conversation_id：服务端历史为准，追加请求最后一条 user 作为本轮输入；
//     regenerate 先删最后一条 assistant 再追加；replace_history 用请求 messages 整体替换。
func resolveAgentChatSession(c *gin.Context, repo *store.AgentChatRepo, userID uint64, req *agentChatRequest) (*agentChatSessionPlan, []ai.ChatMessage, bool) {
	plan := &agentChatSessionPlan{}
	userMsg := lastUserMessage(req.Messages)
	plan.userContent = userMsg

	if req.ConversationID == "" {
		history, ok := buildAgentChatHistory(req.Messages)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "messages must be non-empty and include at least one user message"})
			return nil, nil, false
		}
		if len([]rune(userMsg)) > 20 {
			plan.convTitle = truncateAgentChat(userMsg, 20)
		} else {
			plan.convTitle = userMsg
		}
		return plan, history, true
	}

	rec, err := repo.GetConversation(req.ConversationID)
	if err != nil || rec == nil || rec.UserID != int64(userID) {
		agentChatSessionError(c, req.Stream, http.StatusNotFound, "conversation not found")
		return nil, nil, false
	}
	plan.convID = rec.ID

	switch {
	case req.ReplaceHistory:
		// 编辑消息场景：请求 messages 整体替换会话历史（含最后一条 user），再跑最后一轮。
		stored := make([]*store.AgentMessageRecord, 0, len(req.Messages))
		for _, m := range req.Messages {
			stored = append(stored, &store.AgentMessageRecord{Role: m.Role, Content: m.Content})
		}
		if err := repo.ReplaceMessages(rec.ID, stored); err != nil {
			agentChatSessionError(c, req.Stream, http.StatusInternalServerError, "replace history failed: "+err.Error())
			return nil, nil, false
		}
		plan.skipUserMsg = true // user 消息已入库，落库阶段只补 assistant
		history := make([]ai.ChatMessage, 0, len(req.Messages))
		for _, m := range req.Messages {
			history = append(history, ai.ChatMessage{Role: ai.Role(m.Role), Content: m.Content})
		}
		return plan, history, true

	case req.Regenerate:
		// 删除该会话最后一条 assistant 消息，用 messages 最后一条 user 重跑。
		if err := repo.DeleteLastAssistantMessage(rec.ID); err != nil {
			agentChatSessionError(c, req.Stream, http.StatusInternalServerError, "regenerate failed: "+err.Error())
			return nil, nil, false
		}
	}

	history := listSessionHistory(repo, rec.ID)
	if userMsg != "" {
		// 会话历史为准，追加本轮 user；regenerate 时若库尾已有同内容 user 则不重复。
		if !req.Regenerate || lastHistoryUser(history) != userMsg {
			history = append(history, ai.ChatMessage{Role: ai.RoleUser, Content: userMsg})
		} else {
			plan.userContent = "" // 已存在于历史，落库不再重复插入
		}
	}
	return plan, history, true
}

// agentChatSessionError 会话级错误：非流式回 JSON（带状态码），流式回 SSE error 事件。
func agentChatSessionError(c *gin.Context, stream bool, status int, msg string) {
	if stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
		fmt.Fprintf(c.Writer, "event: error\ndata: {\"message\":%q}\n\ndata: [DONE]\n\n", msg)
		c.Writer.Flush()
		return
	}
	c.JSON(status, gin.H{"error": msg})
}

// lastUserMessage 取 messages 中最后一条 user 消息内容（无则空串）。
func lastUserMessage(messages []agentChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

// listSessionHistory 读会话历史并转为模型输入（system 不入库故无需过滤；
// tool 消息不落库——tool_calls 仅存摘要 JSON，无法重放为模型可用历史）。
func listSessionHistory(repo *store.AgentChatRepo, convID string) []ai.ChatMessage {
	recs, err := repo.ListMessages(convID)
	if err != nil {
		return nil
	}
	history := make([]ai.ChatMessage, 0, len(recs))
	for _, m := range recs {
		if m.Role == "tool" {
			continue
		}
		history = append(history, ai.ChatMessage{Role: ai.Role(m.Role), Content: m.Content})
	}
	return history
}

// lastHistoryUser 取历史中最后一条 user 消息内容（无则空串）。
func lastHistoryUser(history []ai.ChatMessage) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == ai.RoleUser {
			return history[i].Content
		}
	}
	return ""
}

// ── 会话并发互斥 ──

// agentChatConvLocks 按 conversation_id 互斥（同一会话的并发对话串行执行，
// 防止历史交错与落库乱序；条目随进程生命周期保留，不做清理）。
var agentChatConvLocks sync.Map // conversation_id → *sync.Mutex

// lockAgentConversation 加锁并返回解锁函数。
func lockAgentConversation(convID string) func() {
	v, _ := agentChatConvLocks.LoadOrStore(convID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// configuredAgentAIProvider 解析 agent 对话实际使用的 provider。
// 名称优先级：agent.ai.provider（agent 专属覆盖）> ai.defaults.provider >
// ai.provider > 顶层 default_ai_provider（设置页"默认 AI 提供商"写入处）> deepseek。
// 凭证优先级：ai.{name} 配置中的 api_key/model/base_url（设置页保存）> env 注入的注册表默认。
func configuredAgentAIProvider() (*ai.Provider, string) {
	return configuredAgentAIProviderWithOverride("")
}

// parseAgentModelOverride 解析请求 model 覆盖："provider" 或 "provider:model"。
// provider 名归一（NormalizeProviderName）；空输入返回 ("", "")。
func parseAgentModelOverride(s string) (providerName, model string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if i := strings.Index(s, ":"); i >= 0 {
		return ai.NormalizeProviderName(s[:i]), s[i+1:]
	}
	return ai.NormalizeProviderName(s), ""
}

// configuredAgentAIProviderWithOverride 在默认链之上支持模型覆盖：
// 覆盖指定 provider 时替换整条名称链（含 agent.ai.provider），凭证仍按
// ai.{name}.api_key > env 注册表默认回落；带 ":" 时 model 一并覆盖
// （优先于 ai.{name}.model 配置），实现方式为克隆 provider，不动注册表。
func configuredAgentAIProviderWithOverride(modelOverride string) (*ai.Provider, string) {
	cfg := store.GetConfig()
	overrideName, overrideModel := parseAgentModelOverride(modelOverride)
	providerName := "deepseek"
	if v, ok := cfg["default_ai_provider"].(string); ok && v != "" {
		providerName = v
	}
	var aiCfg map[string]any
	if ac, ok := cfg["ai"].(map[string]any); ok {
		aiCfg = ac
		if p := getString(aiCfg, "provider", ""); p != "" {
			providerName = p
		}
		if defaults, ok := aiCfg["defaults"].(map[string]any); ok {
			if p := getString(defaults, "provider", ""); p != "" {
				providerName = p
			}
		}
	}
	agentCfg, _ := cfg["agent"].(map[string]any)
	if aai, ok := agentCfg["ai"].(map[string]any); ok {
		if p := getString(aai, "provider", ""); p != "" {
			providerName = p // agent 专属配置最高优先级
		}
	}
	if overrideName != "" {
		providerName = overrideName // 请求级覆盖最高优先级
	}
	providerName = ai.NormalizeProviderName(providerName)

	// 设置页保存的 per-provider 配置（key/model/base_url）。
	var providerCfg map[string]any
	if aiCfg != nil {
		providerCfg, _ = aiCfg[providerName].(map[string]any)
		if providerCfg == nil {
			if legacy := ai.LegacyProviderName(providerName); legacy != "" {
				providerCfg, _ = aiCfg[legacy].(map[string]any)
			}
		}
		if providerCfg == nil {
			if nested, ok := aiCfg["providers"].(map[string]any); ok {
				providerCfg, _ = nested[providerName].(map[string]any)
			}
		}
	}

	p := ai.GetProvider(providerName)
	if p == nil {
		return nil, providerName
	}
	// 配置里有 key → 克隆覆盖（不动注册表，避免影响其他调用方）。
	cloneForOverride := overrideModel != ""
	if providerCfg != nil {
		key := getString(providerCfg, "api_key", "")
		if key != "" {
			clone := *p
			clone.APIKey = key
			if m := getString(providerCfg, "model", ""); m != "" {
				clone.Model = m
			}
			if bu := getString(providerCfg, "base_url", ""); bu != "" {
				clone.BaseURL = bu
			}
			if overrideModel != "" {
				clone.Model = overrideModel // 请求级模型覆盖优先于配置模型
			}
			return &clone, providerName
		}
	}
	if cloneForOverride {
		// 该 provider 无设置页配置：克隆注册表默认（env 注入的 key/base_url），仅覆盖模型。
		clone := *p
		clone.Model = overrideModel
		return &clone, providerName
	}
	return p, providerName
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
	userID   uint64
	toolCtx  *agent.ToolContext
	history  []ai.ChatMessage
	aiTools  []ai.Tool
	allowed  map[string]bool
	records  []agentToolCallRecord
	// 会话持久化（persistConversation 在成功后落库）
	convID      string // 会话 id（空 = 自动新建）
	convTitle   string // 新建会话标题（首条 user 消息截 20 字）
	userContent string // 本轮 user 消息内容（空 = 不插入 user 行）
	skipUserMsg bool   // replace_history：user 已整体入库，只补 assistant
	reasoning   string // 聚合的推理内容（流式回调 / 非流式解析累计）
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
		fullText, reasoning, toolCalls, err := r.callModel()
		if err != nil {
			return "", err
		}
		r.reasoning += reasoning
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

// persistConversation 轮次成功后落库：自动建会话（标题=首条 user 截 20 字，
// model 存实际使用的 "provider:model"），插 user 行（如未入库）与 assistant 行
// （content / reasoning / tool_calls JSON）。
func (r *agentChatRunner) persistConversation(repo *store.AgentChatRepo, finalContent string) {
	usedModel := r.provider.Name + ":" + r.provider.Model
	if r.convID == "" {
		rec := &store.AgentConversationRecord{
			UserID: int64(r.userID),
			Title:  r.convTitle,
			Model:  usedModel,
		}
		if err := repo.CreateConversation(rec); err == nil {
			r.convID = rec.ID
			r.convTitle = rec.Title
		}
	} else {
		// 已有会话：补上标题（空标题的自动会话首次落库时命名为首条 user 截断）。
		if rec, err := repo.GetConversation(r.convID); err == nil && rec != nil {
			r.convTitle = rec.Title
			if rec.Title == "" && r.userContent != "" {
				title := truncateAgentChat(r.userContent, 20)
				_ = repo.RenameConversation(r.convID, title)
				r.convTitle = title
			}
		}
	}
	if r.convID == "" {
		return // 建会话失败：跳过落库（对话结果仍正常返回）
	}
	if !r.skipUserMsg && r.userContent != "" {
		_ = repo.InsertMessage(&store.AgentMessageRecord{
			ConversationID: r.convID,
			Role:           "user",
			Content:        r.userContent,
		})
	}
	toolCallsJSON, _ := json.Marshal(r.records)
	_ = repo.InsertMessage(&store.AgentMessageRecord{
		ConversationID: r.convID,
		Role:           "assistant",
		Content:        finalContent,
		Reasoning:      r.reasoning,
		ToolCalls:      string(toolCallsJSON),
	})
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

// callModel 单轮模型调用：流式走 ChatCompletionStreamEx（回调转发 SSE delta 与
// reasoning 分片），非流式走 ChatCompletion（解析 message.reasoning_content）。
// 返回 (正文, 推理内容, 工具调用, err)。
func (r *agentChatRunner) callModel() (string, string, []ai.ToolCall, error) {
	req := ai.CompletionRequest{
		Model:      r.provider.Model,
		Messages:   r.history,
		Tools:      r.aiTools,
		ToolChoice: "auto",
	}
	if r.stream {
		fullText, reasoning, toolCalls, err := r.provider.ChatCompletionStreamEx(req, func(delta, reasoningDelta string) {
			r.emitDelta(delta)
			r.emitReasoning(reasoningDelta)
		})
		return fullText, reasoning, toolCalls, err
	}
	resp, err := r.provider.ChatCompletion(req)
	if err != nil {
		return "", "", nil, err
	}
	fullText := ""
	reasoning := ""
	if resp != nil && len(resp.Choices) > 0 {
		fullText = resp.Choices[0].Message.Content
		reasoning = resp.Choices[0].Message.ReasoningContent
	}
	return fullText, reasoning, ai.ExtractToolCalls(resp), nil
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

// emitReasoning 推理内容分片：data 为 JSON 编码字符串（前端 JSON.parse 后追加）。
func (r *agentChatRunner) emitReasoning(reasoningDelta string) {
	if !r.stream || reasoningDelta == "" {
		return
	}
	data, err := json.Marshal(reasoningDelta)
	if err != nil {
		return
	}
	fmt.Fprintf(r.c.Writer, "event: reasoning\ndata: %s\n\n", data)
	r.c.Writer.Flush()
}

// emitEvent 具名事件（reasoning / tool_call / conversation / done / error），payload 序列化为单行 JSON。
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
