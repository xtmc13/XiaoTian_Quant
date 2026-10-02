package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 上下文压缩：手动 /compress 端点 + runner 内自动压缩 ──
// 已压缩消息标记 xt_agent_messages.compressed=1，历史加载时跳过并由
// xt_agent_compactions 最新摘要（前情提要）替代；token 估算与前端约定一致
// （estTokens = 历史总字符数 / 2，CJK 安全）。

const (
	// agentChatCompressKeepLast 压缩时保留的最近未压缩消息条数（不参与摘要）。
	agentChatCompressKeepLast = 6
	// agentChatCompressMinBatch 自动压缩的最小候选批量：刚压缩完只剩 6 条库存，
	// 2 个回合（4 条消息）内不再重复压缩。
	agentChatCompressMinBatch = 4
	// agentChatCompressTimeout 压缩摘要 LLM 调用的整体超时（超时按未压缩继续）。
	agentChatCompressTimeout = 15 * time.Second
)

// agentChatCompressThreshold 自动压缩的 estTokens 阈值；测试可置小触发压缩。
var agentChatCompressThreshold = 96000

// agentChatCompressThresholdEffective 生效阈值：config agent.ai.compress_threshold 优先。
func agentChatCompressThresholdEffective() int {
	cfg := store.GetConfig()
	if agentCfg, ok := cfg["agent"].(map[string]any); ok {
		if aai, ok := agentCfg["ai"].(map[string]any); ok {
			if v, ok := aai["compress_threshold"].(float64); ok && v > 0 {
				return int(v)
			}
		}
	}
	return agentChatCompressThreshold
}

// estimateChatHistoryTokens 估算历史 token 数：总 rune 数 / 2（CJK 安全，
// 与前端 1tok≈2字符 的约定一致）。
func estimateChatHistoryTokens(history []ai.ChatMessage) int {
	runes := 0
	for _, m := range history {
		runes += len([]rune(m.Content)) + len([]rune(m.ReasoningContent))
		for _, tc := range m.ToolCalls {
			runes += len([]rune(tc.Arguments))
		}
	}
	return runes / 2
}

// compressAgentConversation 压缩会话历史：LLM 把全部未压缩消息（保留最近
// agentChatCompressKeepLast 条）总结为 ≤500 字摘要，写入压缩存档并标记
// 候选消息 compressed=1。候选不足 minBatch 时不动作（返回 n=0, err=nil）。
func compressAgentConversation(repo *store.AgentChatRepo, provider *ai.Provider, convID string, minBatch int) (string, int, error) {
	msgs, err := repo.ListMessages(convID)
	if err != nil {
		return "", 0, err
	}
	uncomp := make([]*store.AgentMessageRecord, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "tool" || m.Compressed {
			continue
		}
		uncomp = append(uncomp, m)
	}
	if len(uncomp) <= agentChatCompressKeepLast {
		return "", 0, nil
	}
	candidates := uncomp[:len(uncomp)-agentChatCompressKeepLast]
	if len(candidates) < minBatch {
		return "", 0, nil
	}
	summary, err := summarizeAgentMessages(repo, provider, convID, candidates)
	if err != nil {
		return "", 0, err
	}
	ids := make([]int64, 0, len(candidates))
	for _, m := range candidates {
		ids = append(ids, m.ID)
	}
	rec, err := repo.ApplyCompaction(convID, summary, ids)
	if err != nil {
		return "", 0, err
	}
	return rec.Summary, rec.CompressedCount, nil
}

// summarizeAgentMessages 用当前解析链 provider 生成 ≤500 字摘要；已有旧摘要时并入，
// 保证多轮压缩后上下文仍累计。channel + select 兜底超时（与标题/记忆抽取同模式）。
func summarizeAgentMessages(repo *store.AgentChatRepo, provider *ai.Provider, convID string, msgs []*store.AgentMessageRecord) (string, error) {
	var b strings.Builder
	b.WriteString("把以下对话历史压缩为不超过 500 字的中文摘要，保留关键事实、用户偏好、已得出的结论与待办事项；不要逐条复述，只输出摘要本身。\n")
	if prev, err := repo.LatestCompaction(convID); err == nil && prev != nil && prev.Summary != "" {
		b.WriteString("此前已有摘要（请并入新摘要）：\n" + prev.Summary + "\n")
	}
	b.WriteString("对话历史：\n")
	for _, m := range msgs {
		fmt.Fprintf(&b, "%s: %s\n", m.Role, truncateAgentChat(m.Content, 500))
	}

	type sumResult struct {
		text string
		err  error
	}
	ch := make(chan sumResult, 1)
	go func() {
		resp, err := provider.ChatCompletion(ai.CompletionRequest{
			Model: provider.Model,
			Messages: []ai.ChatMessage{
				{Role: ai.RoleSystem, Content: "你是对话历史压缩器，只输出不超过 500 字的中文摘要。"},
				{Role: ai.RoleUser, Content: b.String()},
			},
		})
		if err != nil || resp == nil || len(resp.Choices) == 0 {
			ch <- sumResult{err: fmt.Errorf("compress summary failed: %v", err)}
			return
		}
		ch <- sumResult{text: strings.TrimSpace(resp.Choices[0].Message.Content)}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			return "", res.err
		}
		if res.text == "" {
			return "", fmt.Errorf("compress summary empty")
		}
		return res.text, nil
	case <-time.After(agentChatCompressTimeout):
		return "", fmt.Errorf("compress summary timeout")
	}
}

// maybeAutoCompress runner 自动压缩：历史 estTokens 超阈值时同步压缩一次
// （保留最近 6 条），压缩后原位替换历史中的库存段并发 compressed SSE 事件；
// 任何失败静默跳过，绝不阻塞本轮对话。
func (r *agentChatRunner) maybeAutoCompress() {
	if r.ephemeral || r.convID == "" || r.repo == nil || r.provider == nil || r.provider.APIKey == "" {
		return
	}
	if estimateChatHistoryTokens(r.history) <= agentChatCompressThresholdEffective() {
		return
	}
	_, n, err := compressAgentConversation(r.repo, r.provider, r.convID, agentChatCompressMinBatch)
	if err != nil || n == 0 {
		return
	}
	// 用压缩后的库存历史原位替换前 dbHistoryLen 条，保留本轮追加的 user 消息。
	fresh := listSessionHistory(r.repo, r.convID)
	r.history = append(fresh, r.history[r.dbHistoryLen:]...)
	r.dbHistoryLen = len(fresh)
	r.emitEvent("compressed", gin.H{"compressed_count": n})
}

// AgentConversationCompress 处理 POST /agent/conversations/:id/compress：
// 手动压缩会话历史（LLM 摘要全部未压缩消息，保留最近 6 条）。
// 响应契约：{"success":true,"summary":"...","compressed_count":int}。
func AgentConversationCompress(c *gin.Context) {
	uid := int64(aiBotUserID(c))
	repo := store.DefaultAgentChatRepo()
	rec, err := repo.GetConversation(c.Param("id"))
	if err != nil || rec == nil || rec.UserID != uid {
		c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
		return
	}
	provider, providerName := configuredAgentAIProvider()
	if provider == nil || provider.APIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("AI provider '%s' 未配置 API Key", providerName)})
		return
	}
	// 手动压缩不批量下限（minBatch=1）：用户点了就压，哪怕只多一条候选。
	summary, n, err := compressAgentConversation(repo, provider, rec.ID, 1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "compress failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "summary": summary, "compressed_count": n})
}

// AgentConversationBranch 处理 POST /agent/conversations/:id/branch：
// 从指定消息处分叉——把 user+assistant 消息 [0..message_index] 复制进新会话
// （标题=原标题（分支），沿用原模型；保留 content/reasoning/tool_calls/compressed，
// 不带用量）。响应契约：{"success":true,"id":"c_new","title":"..."}。
func AgentConversationBranch(c *gin.Context) {
	var body struct {
		MessageIndex int `json:"message_index"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	uid := int64(aiBotUserID(c))
	repo := store.DefaultAgentChatRepo()
	rec, err := repo.GetConversation(c.Param("id"))
	if err != nil || rec == nil || rec.UserID != uid {
		c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
		return
	}
	msgs, err := repo.ListMessages(rec.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list messages failed: " + err.Error()})
		return
	}
	ua := make([]*store.AgentMessageRecord, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "user" || m.Role == "assistant" {
			ua = append(ua, m)
		}
	}
	if body.MessageIndex < 0 || body.MessageIndex >= len(ua) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message_index out of range"})
		return
	}
	branch := &store.AgentConversationRecord{
		UserID: uid,
		Title:  rec.Title + "（分支）",
		Model:  rec.Model,
	}
	if err := repo.CreateConversation(branch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create branch failed: " + err.Error()})
		return
	}
	for _, m := range ua[:body.MessageIndex+1] {
		if err := repo.InsertMessage(&store.AgentMessageRecord{
			ConversationID: branch.ID,
			Role:           m.Role,
			Content:        m.Content,
			Reasoning:      m.Reasoning,
			ToolCalls:      m.ToolCalls,
			Compressed:     m.Compressed,
		}); err != nil {
			_ = repo.DeleteConversation(branch.ID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "copy messages failed: " + err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "id": branch.ID, "title": branch.Title})
}
