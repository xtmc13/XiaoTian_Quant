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
	"github.com/xiaotian-quant/gateway/internal/agentmemory"
	"github.com/xiaotian-quant/gateway/internal/agentskills"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/plugins/builtin"
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
//	event: compressed  data: {"compressed_count":int}（自动压缩触发时，delta 之前发一次）
//	event: tool_call   data: {"name","args_summary","status","result_summary"}
//	event: approval_request data: {"id","tool","args_summary"}（approval_mode=writes 时写工具执行前暂停，待 /agent/chat/approve 决议）
//	event: conversation data: {"id","title"}（自动/指定会话，done 之前发一次）
//	event: done        data: {"content","tool_calls":[...],"reasoning","conversation_id","usage":{"prompt_tokens","completion_tokens","llm_ms","first_token_ms","tok_per_s"}}
//	event: error       data: {"message"}
//	流结束            data: [DONE]

const (
	agentChatMaxIterations = 5                // tool-calling 最大迭代轮数
	agentChatToolTimeout   = 30 * time.Second // 单次工具执行超时
	agentChatResultLimit   = 2000             // 工具结果回传模型的截断长度
	agentChatSummaryLimit  = 80               // args_summary / result_summary 截断长度
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
	// Ephemeral：临时会话（/btw 旁路提问）——正常执行与流式输出，但不落库、
	// 不建会话、不触发标题/记忆后台任务；conversation_id 仅用于回显 conversation 事件。
	Ephemeral bool `json:"ephemeral"`
	// Moa：多厂商混合（Mixture of Agents）——正式循环前先把本轮提问扇出到
	// 其它持有凭证的厂商，成功回答拼成综合参考块注入主厂商上下文（见 agent_chat_moa.go）。
	Moa bool `json:"moa"`
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
	userID, tokenID, scopes, rateLimitRPS, usedToken, ok := resolveAgentChatIdentity(c)
	if !ok {
		return // 已回 401
	}

	// ── 限流：token 路径按 rate_limit_rps 滑动窗口（单进程内存实现）；JWT 路径不限 ──
	if usedToken && !agent.AllowAgentChatRequest(tokenID, rateLimitRPS) {
		agent.GetTokenManager().LogAccess(
			tokenID, "chat", "/agent/chat", "POST",
			"rate limit exceeded", http.StatusTooManyRequests, c.ClientIP(), c.Request.UserAgent(),
		)
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success": false,
			"error":   gin.H{"code": "RATE_LIMITED", "message": "rate limit exceeded, please slow down"},
		})
		return
	}

	// ── 会话解析：历史构造 + regenerate / replace_history（先于 provider 检查，
	// 保证请求校验 400/404 不因 provider 未配置而被掩盖）。
	// 指定会话时按 conversation_id 加互斥锁再读历史：防止并发对话历史交错与落库乱序
	// （自动新建会话无竞争，不加锁）。临时会话（ephemeral）完全绕过会话层。
	repo := store.DefaultAgentChatRepo()
	var plan *agentChatSessionPlan
	var history []ai.ChatMessage
	if reqBody.Ephemeral {
		plan, history, ok = resolveAgentChatEphemeral(c, repo, userID, &reqBody)
		if !ok {
			return // 已回 400
		}
	} else {
		if reqBody.ConversationID != "" {
			unlock := lockAgentConversation(reqBody.ConversationID)
			defer unlock()
		}
		plan, history, ok = resolveAgentChatSession(c, repo, userID, &reqBody)
		if !ok {
			return // 已回 404 / 400
		}
	}

	// ── Provider：统一解析链（请求覆盖 > 用户级 > agent.ai > 全局链）──
	provider, providerName := configuredAgentAIProviderForUser(int64(userID), reqBody.Model)
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

	// ── 工具：agent token 按 scope 过滤；再按角色做 RBAC 门控
	// （ScopeAdmin 文件工具仅管理员可见，普通用户工具集不变）──
	tools := mergedAgentTools()
	if usedToken {
		tools = agent.GetTokenManager().FilterTools(tools, scopes)
	}
	tools = agent.FilterToolsByRole(tools, agentRequestRole(c, userID, usedToken))

	if reqBody.Stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
	}

	// 浅拷贝全局 ToolContext：UserID 按请求覆盖，底层依赖仍共享生产单例。
	tc := *agent.GetToolContext()
	tc.UserID = userID
	tc.ConversationID = plan.convID // 文件工具检查点记录会话来源（自动建会话首轮为空）

	r := &agentChatRunner{
		c:            c,
		provider:     provider,
		stream:       reqBody.Stream,
		tokenID:      tokenID,
		userID:       userID,
		toolCtx:      &tc,
		history:      history,
		aiTools:      toAITools(tools),
		allowed:      allowedToolNames(tools),
		writeTools:   writeToolNames(tools),
		records:      []agentToolCallRecord{},
		convID:       plan.convID,
		convTitle:    plan.convTitle,
		userContent:  plan.userContent,
		skipUserMsg:  plan.skipUserMsg,
		dbHistoryLen: plan.dbHistoryLen,
		ephemeral:    reqBody.Ephemeral,
		moa:          reqBody.Moa,
		repo:         repo,
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

	// ── 落库：user + assistant（content / reasoning / tool_calls JSON + 用量）──
	// 临时会话跳过全部持久化与后台任务（标题升级 / 记忆抽取）。
	if !reqBody.Ephemeral {
		r.persistConversation(repo, finalContent)

		// 自动截断标题的会话：异步升级为 LLM 生成标题（不阻塞 SSE 事件流）
		if agentChatTitleUpgrade && r.autoTitled && r.convID != "" {
			go r.upgradeConversationTitle(repo)
		}

		// 自动记忆沉淀（Hermes 核心）：每 3 个助手回合异步抽取一次，不阻塞响应。
		if agentChatMemoryExtract && r.convID != "" {
			go r.extractConversationMemories(repo)
		}
	}

	if reqBody.Stream {
		// conversation 事件在 done 之前发一次（新建会话时前端据此更新当前会话 id）；
		// 临时会话仅在请求带了 conversation_id 时回显（不建会话）。
		if !reqBody.Ephemeral || r.convID != "" {
			r.emitEvent("conversation", gin.H{"id": r.convID, "title": r.convTitle})
		}
		r.emitEvent("done", gin.H{
			"content":         finalContent,
			"tool_calls":      r.records,
			"reasoning":       r.reasoning,
			"conversation_id": r.convID,
			"usage":           r.usagePayload(),
		})
		fmt.Fprint(c.Writer, "data: [DONE]\n\n")
		c.Writer.Flush()
		return
	}
	resp := gin.H{
		"content":         finalContent,
		"tool_calls":      r.records,
		"reasoning":       r.reasoning,
		"conversation_id": r.convID,
		"usage":           r.usagePayload(),
	}
	if reqBody.Moa {
		// 非流式路径不发 moa 事件，改为响应体带实际参与厂商列表（厂商不足为空数组）。
		proposers := r.moaProposers
		if proposers == nil {
			proposers = []string{}
		}
		resp["moa_proposers"] = proposers
	}
	c.JSON(http.StatusOK, resp)
}

// agentChatSessionPlan 会话解析结果：runner 落库所需的会话元信息。
type agentChatSessionPlan struct {
	convID      string // 会话 id（空 = 自动新建，落库时创建）
	convTitle   string // 新建会话标题（首条 user 消息截 20 字）
	userContent string // 本轮 user 消息内容（落库用）
	skipUserMsg bool   // replace_history：user 消息已整体入库，只补 assistant
	// dbHistoryLen 历史中来自库存的消息条数（自动压缩后用压缩后的历史原位替换该段，
	// 保留本轮追加的 user 消息）。
	dbHistoryLen int
}

// resolveAgentChatEphemeral 临时会话（/btw 旁路提问）：历史完全来自请求 messages，
// 不读库、不建会话；conversation_id 仅作 conversation 事件回显（标题尽力读取，
// 不校验属主、不存在也不报错）。
func resolveAgentChatEphemeral(c *gin.Context, repo *store.AgentChatRepo, userID uint64, req *agentChatRequest) (*agentChatSessionPlan, []ai.ChatMessage, bool) {
	plan := &agentChatSessionPlan{userContent: lastUserMessage(req.Messages)}
	history, ok := buildAgentChatHistory(req.Messages)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "messages must be non-empty and include at least one user message"})
		return nil, nil, false
	}
	if req.ConversationID != "" {
		plan.convID = req.ConversationID
		if rec, err := repo.GetConversation(req.ConversationID); err == nil && rec != nil && rec.UserID == int64(userID) {
			plan.convTitle = rec.Title
		}
	}
	return plan, history, true
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
	plan.dbHistoryLen = len(history)
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
// 已压缩消息跳过，改在头部注入最新一条压缩摘要（user 角色的前情提要）。
func listSessionHistory(repo *store.AgentChatRepo, convID string) []ai.ChatMessage {
	recs, err := repo.ListMessages(convID)
	if err != nil {
		return nil
	}
	history := make([]ai.ChatMessage, 0, len(recs)+1)
	if comp, err := repo.LatestCompaction(convID); err == nil && comp != nil && comp.Summary != "" {
		history = append(history, ai.ChatMessage{Role: ai.RoleUser, Content: "（前情摘要：" + comp.Summary + "）"})
	}
	for _, m := range recs {
		if m.Role == "tool" || m.Compressed {
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
	return configuredAgentAIProviderForUser(0, modelOverride)
}

// configuredAgentAIProviderForUser 完整解析链：
// 请求覆盖 > 用户级覆盖（xt_agent_user_ai，provider 非空生效）> agent.ai.provider >
// ai.defaults.provider > ai.provider > default_ai_provider > deepseek。
// 用户级 api_key 非空时覆盖该 provider 的 env/全局凭证（克隆，不动注册表）。
func configuredAgentAIProviderForUser(userID int64, modelOverride string) (*ai.Provider, string) {
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
			providerName = p // agent 专属配置高于全局链
		}
	}
	// 用户级覆盖：仅当请求未指定 provider 时插入（请求覆盖最高优先级）。
	var userCfg *store.AgentUserAIRecord
	if overrideName == "" && userID > 0 {
		if rec, err := store.NewAgentUserAIRepo().Get(userID); err == nil && rec != nil && rec.Provider != "" {
			userCfg = rec
			providerName = rec.Provider
		}
	}
	if overrideName != "" {
		providerName = overrideName // 请求级覆盖最高优先级
	}
	providerName = ai.NormalizeProviderName(providerName)

	// 设置页保存的 per-provider 配置（key/model/base_url）。
	providerCfg := agentAIProviderConfig(aiCfg, providerName)

	p := ai.GetProvider(providerName)
	if p == nil {
		return nil, providerName
	}
	// base：设置页 per-provider 配置（key/model/base_url）克隆优先，否则注册表默认
	// （env 注入的 key/base_url）。一律克隆覆盖，不动注册表。
	base := p
	if providerCfg != nil {
		if key := getString(providerCfg, "api_key", ""); key != "" {
			clone := *p
			clone.APIKey = key
			if m := getString(providerCfg, "model", ""); m != "" {
				clone.Model = m
			}
			if bu := getString(providerCfg, "base_url", ""); bu != "" {
				clone.BaseURL = bu
			}
			base = &clone
		}
	}
	// 用户级凭证覆盖：api_key / model 非空时覆盖该 provider 的全局/env 凭证。
	if userCfg != nil && (userCfg.APIKey != "" || userCfg.Model != "") {
		clone := *base
		if userCfg.APIKey != "" {
			clone.APIKey = userCfg.APIKey
		}
		if userCfg.Model != "" {
			clone.Model = userCfg.Model
		}
		base = &clone
	}
	if overrideModel != "" {
		// 请求级模型覆盖优先于一切配置模型
		if base == p {
			clone := *p
			clone.Model = overrideModel
			return &clone, providerName
		}
		base.Model = overrideModel
	}
	return base, providerName
}

// resolveAgentChatIdentity 双路径鉴权：X-Agent-Token 优先于 JWT。
// 返回 (userID, tokenID, scopes, rateLimitRPS, usedToken, ok)；!ok 时已写入 401 响应。
func resolveAgentChatIdentity(c *gin.Context) (uint64, int, []string, int, bool, bool) {
	if h := strings.TrimSpace(c.GetHeader("X-Agent-Token")); h != "" {
		rec, err := agent.GetTokenManager().ValidateToken(h)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid agent token"})
			return 0, 0, nil, 0, false, false
		}
		return uint64(rec.UserID), rec.ID, agent.ParseTokenScopes(rec.Scopes), rec.RateLimitRPS, true, true
	}
	return uint64(aiBotUserID(c)), 0, nil, 0, false, true
}

// agentRequestRole 解析当前请求的用户角色（RBAC 工具门控用）：
// Web JWT 路径取鉴权中间件写入的 claims；X-Agent-Token 路径按 token 属主查库。
// 取不到一律按普通用户处理（fail closed，ScopeAdmin 工具不下发）。
func agentRequestRole(c *gin.Context, userID uint64, usedToken bool) string {
	if !usedToken {
		return c.GetString("role")
	}
	return agentRoleFromStore(int64(userID))
}

// agentRoleFromStore 按 userID 查库取角色（headless / token 路径；无缓存，
// 库不可用或用户不存在返回空串 = 普通用户）。
func agentRoleFromStore(userID int64) string {
	if u := store.FindUserByID(int(userID)); u != nil {
		role, _ := u["role"].(string)
		return role
	}
	return ""
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

// writeToolNames 构建写类工具名集合（approval_mode=writes 审批门用）。
func writeToolNames(tools []agent.Tool) map[string]bool {
	out := make(map[string]bool, len(tools))
	for _, t := range tools {
		if t.Scope == agent.ScopeWrite {
			out[t.Name] = true
		}
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
	// writeTools 写类工具名集合（approval_mode=writes 时这些工具执行前需审批）。
	writeTools map[string]bool
	records    []agentToolCallRecord
	// 会话持久化（persistConversation 在成功后落库）
	convID      string // 会话 id（空 = 自动新建）
	convTitle   string // 新建会话标题（首条 user 消息截 20 字）
	userContent string // 本轮 user 消息内容（空 = 不插入 user 行）
	skipUserMsg bool   // replace_history：user 已整体入库，只补 assistant
	reasoning   string // 聚合的推理内容（流式回调 / 非流式解析累计）
	// headless 扩展（TG 入站等）：ctx 支持 /stop 中断；onDelta 文本分片回调（流式回复）
	ctx     context.Context
	onDelta func(string)
	// 用量统计（usage 事件字段与 assistant 消息落库共用）
	usage        ai.Usage  // 跨 tool-calling 迭代累计的 token 用量
	llmStart     time.Time // run 开始时间
	firstTokenAt time.Time // 首个正文/推理分片到达时间（零值 = 无分片，first_token_ms 记 0）
	autoTitled   bool      // 标题为截断自动生成（据此异步升级为 LLM 标题）
	// 临时会话（ephemeral）：不落库、不触发自动压缩与后台任务。
	ephemeral bool
	// moa：本轮启用多厂商混合（请求 moa:true）；moaProposers 记录实际参与厂商
	// （主厂商在前），供非流式 JSON 的 moa_proposers 字段回显。
	moa          bool
	moaProposers []string
	// onToolCall 工具实际派发时回调（headless 评测用；HTTP 路径为 nil）。
	onToolCall func(string)
	// repo 会话存储（自动压缩用；仅 convID 非空的路径设置）。
	repo *store.AgentChatRepo
	// dbHistoryLen 历史前缀中来自库存的消息条数（自动压缩后原位替换该段）。
	dbHistoryLen int
}

// run 执行 tool-calling 循环，返回最终答案文本。
func (r *agentChatRunner) run() (string, error) {
	r.llmStart = time.Now()
	// 自动上下文压缩：历史估算 token 超阈值时先同步压缩（保留最近 6 条），
	// 再继续本轮；任何失败按原样进行（绝不阻塞对话）。
	r.maybeAutoCompress()
	// 系统提示词放在这里拼装：首轮调用前工具集已确定。
	sysPrompt := buildAgentChatSystemPrompt(r.agentToolsDesc())
	// 记忆插件：把该用户的重要记忆注入系统提示词尾部（无记忆为空串）
	if mem := agentmemory.LoadPromptSection(int64(r.userID)); mem != "" {
		sysPrompt += "\n\n" + mem
	}
	// 技能插件：技能目录注入（agent 据此用 run_skill 调用）
	if skills := agentskills.LoadCatalogSection(int64(r.userID)); skills != "" {
		sysPrompt += "\n\n" + skills
	}
	r.history = append([]ai.ChatMessage{{Role: ai.RoleSystem, Content: sysPrompt}}, r.history...)

	// MoA：正式循环前扇出到其它持有凭证的厂商（同一系统提示词、不带工具），
	// 成功回答拼成综合参考块前置到最后一条 user 消息；moa 事件在任何 delta 之前发出。
	if r.moa {
		r.runMoaPropose(sysPrompt)
	}

	finalContent := ""
	completed := false
	for i := 0; i < agentChatMaxIterations; i++ {
		if r.aborted() {
			return "", errAgentChatAborted
		}
		fullText, reasoning, toolCalls, usage, err := r.callModel()
		if err != nil {
			return "", err
		}
		r.reasoning += reasoning
		// 跨迭代累计 token 用量（流式协议未上报时为零值，不影响累加）
		r.usage.PromptTokens += usage.PromptTokens
		r.usage.CompletionTokens += usage.CompletionTokens
		r.usage.TotalTokens += usage.TotalTokens
		if len(toolCalls) == 0 {
			finalContent = fullText
			completed = true
			break
		}
		// 本轮 assistant 消息（工具调用）入历史，再逐个执行并回传 tool 结果。
		r.history = append(r.history, ai.ChatMessage{Role: ai.RoleAssistant, Content: "", ToolCalls: toolCalls})
		for _, tc := range toolCalls {
			if r.aborted() {
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

// aborted 请求是否已中断；headless 无 HTTP 请求时看注入的 ctx（/stop 取消）。
func (r *agentChatRunner) aborted() bool {
	if r.c != nil && r.c.Request != nil && r.c.Request.Context().Err() != nil {
		return true
	}
	return r.ctx != nil && r.ctx.Err() != nil
}

// reqContext 工具执行用的上下文：HTTP 请求 > 注入 ctx > background。
func (r *agentChatRunner) reqContext() context.Context {
	if r.c != nil && r.c.Request != nil {
		return r.c.Request.Context()
	}
	if r.ctx != nil {
		return r.ctx
	}
	return context.Background()
}

// persistConversation 轮次成功后落库：自动建会话（标题=首条 user 截 20 字，
// model 存实际使用的 "provider:model"），插 user 行（如未入库）与 assistant 行
// （content / reasoning / tool_calls JSON + token 用量与整轮耗时）。
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
			r.autoTitled = rec.Title != "" // 截断自动标题：标记后可异步升级为 LLM 标题
		}
	} else {
		// 已有会话：补上标题（空标题的自动会话首次落库时命名为首条 user 截断）。
		if rec, err := repo.GetConversation(r.convID); err == nil && rec != nil {
			r.convTitle = rec.Title
			if rec.Title == "" && r.userContent != "" {
				title := truncateAgentChat(r.userContent, 20)
				_ = repo.RenameConversation(r.convID, title)
				r.convTitle = title
				r.autoTitled = true
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
		ConversationID:   r.convID,
		Role:             "assistant",
		Content:          finalContent,
		Reasoning:        r.reasoning,
		ToolCalls:        string(toolCallsJSON),
		PromptTokens:     int64(r.usage.PromptTokens),
		CompletionTokens: int64(r.usage.CompletionTokens),
		LLMMs:            time.Since(r.llmStart).Milliseconds(),
	})
}

// agentChatTitleTimeout LLM 标题生成的整体超时（超时保留截断标题）。
const agentChatTitleTimeout = 10 * time.Second

// agentChatTitleUpgrade 异步 LLM 标题升级开关；测试置 false 避免与标题断言竞态。
var agentChatTitleUpgrade = true

// upgradeConversationTitle 异步把截断自动标题升级为 LLM 生成标题（≤12 字，
// 与首条 user 消息同语言，无引号标点）。任何失败静默保留截断标题；
// 仅当标题仍是当时的截断标题时才改写（不覆盖用户手动改名）。
func (r *agentChatRunner) upgradeConversationTitle(repo *store.AgentChatRepo) {
	convID, autoTitle, userMsg := r.convID, r.convTitle, r.userContent
	if convID == "" || autoTitle == "" || userMsg == "" || r.provider == nil || r.provider.APIKey == "" {
		return
	}
	rec, err := repo.GetConversation(convID)
	if err != nil || rec == nil || rec.Title != autoTitle {
		return
	}

	// provider.ChatCompletion 内部自带超时且不收 ctx：结果走 channel，
	// 外层 select 兜底 10s，超时直接放弃（保留截断标题）。
	type titleResult struct {
		title string
		err   error
	}
	ch := make(chan titleResult, 1)
	go func() {
		resp, err := r.provider.ChatCompletion(ai.CompletionRequest{
			Model: r.provider.Model,
			Messages: []ai.ChatMessage{
				{Role: ai.RoleSystem, Content: "你是会话标题生成器，只输出标题本身。"},
				{Role: ai.RoleUser, Content: "把以下用户消息总结为一个不超过 12 个字的会话标题（与消息同语言，不要引号、标点或任何解释）。只输出标题。\n消息：" + userMsg},
			},
		})
		if err != nil || resp == nil || len(resp.Choices) == 0 {
			ch <- titleResult{err: fmt.Errorf("title generation failed: %v", err)}
			return
		}
		ch <- titleResult{title: resp.Choices[0].Message.Content}
	}()

	var title string
	select {
	case res := <-ch:
		if res.err != nil {
			return
		}
		title = sanitizeAgentChatTitle(res.title)
	case <-time.After(agentChatTitleTimeout):
		return
	}
	if title == "" {
		return
	}
	// 生成期间用户可能已手动改名：改写前再校验一次当前标题。
	rec, err = repo.GetConversation(convID)
	if err != nil || rec == nil || rec.Title != autoTitle {
		return
	}
	_ = repo.RenameConversation(convID, title)
}

// sanitizeAgentChatTitle 清洗 LLM 输出：去首尾空白/引号/标点，截 12 字。
func sanitizeAgentChatTitle(s string) string {
	s = strings.Trim(s, " \t\n\r\"'“”‘’`「」『』《》<>。，、！？!?.…")
	if s == "" {
		return ""
	}
	return truncateAgentChat(s, 12)
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

// callModel 单轮模型调用：流式走 ChatCompletionStreamUsage（回调转发 SSE delta 与
// reasoning 分片，并回填协议上报的用量），非流式走 ChatCompletion（解析
// message.reasoning_content）。返回 (正文, 推理内容, 工具调用, 用量, err)。
func (r *agentChatRunner) callModel() (string, string, []ai.ToolCall, ai.Usage, error) {
	req := ai.CompletionRequest{
		Model:      r.provider.Model,
		Messages:   r.history,
		Tools:      r.aiTools,
		ToolChoice: "auto",
	}
	if r.stream {
		return r.provider.ChatCompletionStreamUsage(req, func(delta, reasoningDelta string) {
			// 首个正文/推理分片到达即记 first-token 时间（仅此一次）
			if r.firstTokenAt.IsZero() && (delta != "" || reasoningDelta != "") {
				r.firstTokenAt = time.Now()
			}
			r.emitDelta(delta)
			r.emitReasoning(reasoningDelta)
		})
	}
	resp, err := r.provider.ChatCompletion(req)
	if err != nil {
		return "", "", nil, ai.Usage{}, err
	}
	fullText := ""
	reasoning := ""
	usage := ai.Usage{}
	if resp != nil {
		usage = resp.Usage
		if len(resp.Choices) > 0 {
			fullText = resp.Choices[0].Message.Content
			reasoning = resp.Choices[0].Message.ReasoningContent
		}
	}
	return fullText, reasoning, ai.ExtractToolCalls(resp), usage, nil
}

// agentChatUsagePayload done 事件 / 非流式 JSON 的 usage 字段（契约固定，
// 用量不可用时各字段为零值）。
type agentChatUsagePayload struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	LLMMs            int64   `json:"llm_ms"`
	FirstTokenMs     int64   `json:"first_token_ms"`
	TokPerS          float64 `json:"tok_per_s"`
}

// usagePayload 汇总整轮用量：llm_ms 为 run 总耗时，first_token_ms 为首分片耗时
// （无分片记 0），tok_per_s = completion_tokens / 总秒数（保留 1 位小数）。
func (r *agentChatRunner) usagePayload() agentChatUsagePayload {
	llmMs := time.Since(r.llmStart).Milliseconds()
	firstTokenMs := int64(0)
	if !r.firstTokenAt.IsZero() {
		firstTokenMs = r.firstTokenAt.Sub(r.llmStart).Milliseconds()
	}
	tokPerS := 0.0
	if llmMs > 0 && r.usage.CompletionTokens > 0 {
		tokPerS = float64(r.usage.CompletionTokens) / (float64(llmMs) / 1000)
		tokPerS = float64(int(tokPerS*10+0.5)) / 10
	}
	return agentChatUsagePayload{
		PromptTokens:     r.usage.PromptTokens,
		CompletionTokens: r.usage.CompletionTokens,
		LLMMs:            llmMs,
		FirstTokenMs:     firstTokenMs,
		TokPerS:          tokPerS,
	}
}

// executeTool 执行单个工具调用：发 running/done 事件、回传结果文本（截断）、写审计。
func (r *agentChatRunner) executeTool(tc ai.ToolCall) string {
	argsSummary := truncateAgentChat(tc.Arguments, agentChatSummaryLimit)
	rec := agentToolCallRecord{Name: tc.Name, ArgsSummary: argsSummary, Status: "running"}
	r.emitEvent("tool_call", rec)

	statusCode := http.StatusOK
	var content string

	handler, found := agentToolHandlers[tc.Name]
	if !found {
		// 插件贡献的工具（万物皆可插件：cron / 未来的 memory、skills…）
		handler, found = builtin.Manager().Registry().Handler(tc.Name)
	}
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
			// 审批门：approval_mode=writes 时写类工具执行前需用户确认；
			// 拒绝不终结回合——把拒绝结果回传模型继续对话。
			if r.writeTools[tc.Name] && !r.checkApproval(tc.Name, argsSummary) {
				statusCode = http.StatusForbidden
				content = "用户拒绝了该操作"
				break
			}
			// 工具可见性回调：评测等 headless 调用方据此统计实际使用的工具。
			if r.onToolCall != nil {
				r.onToolCall(tc.Name)
			}
			ctx, cancel := context.WithTimeout(r.reqContext(), agentChatToolTimeout)
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

// writeAudit 每次工具执行写 agent_audit_log（JWT 路径 tokenID=0；headless 无 IP/UA）。
func (r *agentChatRunner) writeAudit(tool, argsSummary string, statusCode int) {
	ip, ua := "", ""
	if r.c != nil && r.c.Request != nil {
		ip = r.c.ClientIP()
		ua = r.c.Request.UserAgent()
	}
	agent.GetTokenManager().LogAccess(
		r.tokenID, tool, "/agent/chat/tools/call", "TOOL",
		argsSummary, statusCode, ip, ua,
	)
}

// ── SSE 写出 ──

// emitDelta 默认事件：data 为文本分片原文（前端直接追加）；多行分片拆成多条 data 行。
// headless（c==nil）时转发给 onDelta 回调（TG 流式回复）。
func (r *agentChatRunner) emitDelta(delta string) {
	if !r.stream {
		return
	}
	if r.c == nil {
		if r.onDelta != nil && delta != "" {
			r.onDelta(delta)
		}
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
	if !r.stream || reasoningDelta == "" || r.c == nil {
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
	if !r.stream || r.c == nil {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(r.c.Writer, "event: %s\ndata: %s\n\n", event, data)
	r.c.Writer.Flush()
}

// ── 插件化工具装配 + headless 执行（cron 到点任务等无 HTTP 场景复用） ──

// mergedAgentTools 核心工具集 + 插件贡献工具（万物皆可插件）。
func mergedAgentTools() []agent.Tool {
	core := agent.AllTools()
	ext := builtin.Manager().Registry().Tools()
	out := make([]agent.Tool, 0, len(core)+len(ext))
	out = append(out, core...)
	out = append(out, ext...)
	return out
}

// RunAgentHeadless 以指定用户身份无界面执行一轮 agent 对话（工具按角色 RBAC 门控后全开，无审计 IP/UA）。
// cron 调度器到点执行与后端异步任务复用此入口。
func RunAgentHeadless(userID int64, prompt string) (string, error) {
	return runAgentHeadlessFiltered(userID, prompt, nil)
}

// RunAgentSub 子代理入口：仅放行只读 + 通知类工具（无下单/策略/回测写操作），
// 供 subagents 插件并行派生使用。
func RunAgentSub(userID int64, prompt string) (string, error) {
	return runAgentHeadlessFiltered(userID, prompt, func(t agent.Tool) bool {
		return t.Scope == agent.ScopeRead || t.Scope == agent.ScopeNotify
	})
}

// runAgentHeadlessFiltered headless 执行；keep 非 nil 时按 scope 过滤工具。
func runAgentHeadlessFiltered(userID int64, prompt string, keep func(agent.Tool) bool) (string, error) {
	return runAgentHeadlessCore(context.Background(), HeadlessOptions{UserID: userID, Prompt: prompt}, keep)
}

// HeadlessOptions headless 执行选项（TG 入站等平台投递场景）。
type HeadlessOptions struct {
	UserID int64
	Prompt string
	// ConversationID 绑定会话：加载其历史作为上下文，并把本轮 user+assistant 落库
	// （与网页端 SSE 路径同一套持久化，web UI 可见）；空 = 无会话（不落库）。
	ConversationID string
	// Model 覆盖："provider" 或 "provider:model"；空 = 默认解析链。
	Model string
	// OnDelta 文本分片回调：非 nil 时走流式协议并逐片回调（TG 流式回复）。
	OnDelta func(string)
	// OnToolCall 工具实际派发（找到且被允许、执行前）时回调工具名（评测统计 used_tools 用）。
	OnToolCall func(name string)
}

// RunAgentHeadlessCtx 带上下文与扩展选项的 headless 入口：ctx 取消即中断执行
// （TG /stop）；工具按角色 RBAC 门控后全开。errAgentChatAborted 表示被 ctx 中断（本轮不落库）。
func RunAgentHeadlessCtx(ctx context.Context, opts HeadlessOptions) (string, error) {
	return runAgentHeadlessCore(ctx, opts, nil)
}

// runAgentHeadlessCore headless 执行核心：keep 非 nil 时按 scope 过滤工具。
func runAgentHeadlessCore(ctx context.Context, opts HeadlessOptions, keep func(agent.Tool) bool) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if builtin.BuildError() != nil {
		return "", builtin.BuildError()
	}
	provider, providerName := configuredAgentAIProviderForUser(opts.UserID, opts.Model)
	if provider == nil || provider.APIKey == "" {
		return "", fmt.Errorf("AI provider '%s' 未配置 API Key", providerName)
	}
	tools := mergedAgentTools()
	// RBAC 门控：ScopeAdmin 文件工具仅管理员可见（cron/TG 等 headless 路径同样生效）。
	tools = agent.FilterToolsByRole(tools, agentRoleFromStore(opts.UserID))
	if keep != nil {
		filtered := make([]agent.Tool, 0, len(tools))
		for _, t := range tools {
			if keep(t) {
				filtered = append(filtered, t)
			}
		}
		tools = filtered
	}

	// 浅拷贝全局 ToolContext：UserID 覆盖，底层依赖共享生产单例。
	tc := *agent.GetToolContext()
	tc.UserID = uint64(opts.UserID)
	tc.ConversationID = opts.ConversationID // 文件工具检查点记录会话来源

	allowed := map[string]bool{}
	for _, t := range tools {
		allowed[t.Name] = true
	}

	r := &agentChatRunner{
		c:           nil, // headless：无 SSE
		ctx:         ctx,
		provider:    provider,
		stream:      opts.OnDelta != nil,
		onDelta:     opts.OnDelta,
		onToolCall:  opts.OnToolCall,
		userID:      uint64(opts.UserID),
		toolCtx:     &tc,
		aiTools:     toAITools(tools),
		allowed:     allowed,
		writeTools:  writeToolNames(tools),
		userContent: opts.Prompt,
	}

	// 会话绑定：与网页端同语义——服务端历史为准，追加本轮 user；按会话互斥防交错。
	var repo *store.AgentChatRepo
	if opts.ConversationID != "" {
		repo = store.DefaultAgentChatRepo()
		unlock := lockAgentConversation(opts.ConversationID)
		defer unlock()
		rec, err := repo.GetConversation(opts.ConversationID)
		if err != nil || rec == nil || rec.UserID != opts.UserID {
			return "", fmt.Errorf("conversation not found")
		}
		r.convID = rec.ID
		r.convTitle = rec.Title
		stored := listSessionHistory(repo, rec.ID)
		r.dbHistoryLen = len(stored)
		r.repo = repo
		r.history = append(stored, ai.ChatMessage{Role: ai.RoleUser, Content: opts.Prompt})
	} else {
		r.history = []ai.ChatMessage{{Role: ai.RoleUser, Content: opts.Prompt}}
	}

	finalContent, err := r.run()
	if err != nil {
		return "", err
	}
	// 绑定会话时落库本轮（user + assistant，tool_calls 摘要 JSON，与 web 路径一致）。
	if r.convID != "" {
		r.persistConversation(repo, finalContent)
	}
	return finalContent, nil
}

// ResolveScheduleNL 用配置的 LLM 把自然语言时间描述转换为 cron 表达式（供 cron 插件的 schedule 解析）。
func ResolveScheduleNL(text string) (string, error) {
	provider, providerName := configuredAgentAIProvider()
	if provider == nil || provider.APIKey == "" {
		return "", fmt.Errorf("AI provider '%s' 未配置 API Key，无法解析自然语言时间，请直接提供 cron 表达式", providerName)
	}
	prompt := "把以下时间描述转换为 5 段 cron 表达式（分 时 日 月 周，只用数字和 * , - /；周一=1…周日=0 或 7；北京时间）。只输出表达式本身，不要任何解释。\n描述：" + text
	resp, err := provider.ChatCompletion(ai.CompletionRequest{
		Model: provider.Model,
		Messages: []ai.ChatMessage{
			{Role: ai.RoleSystem, Content: "你是 cron 表达式转换器，只输出 5 段 cron 表达式。"},
			{Role: ai.RoleUser, Content: prompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("模型解析失败: %w", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		return "", fmt.Errorf("模型无响应")
	}
	return resp.Choices[0].Message.Content, nil
}
