package handler

import (
	"fmt"
	"strings"
	"time"

	"github.com/xiaotian-quant/gateway/internal/agentmemory"
	"github.com/xiaotian-quant/gateway/internal/agentprofiles"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 学习闭环：自动记忆抽取（Hermes self-learning） ──
// 沉淀引导已静态化进 save_memory / save_skill 工具描述（见 agentmemory / agentskills
// 插件），不再按回合动态注入系统提示词——动态 nudge 每 5 轮改变前缀会破坏按会话的
// prompt 缓存。

// agentChatMemoryExtract 自动记忆抽取开关；测试置 false 避免 goroutine 与断言竞态。
var agentChatMemoryExtract = true

// agentChatMemoryExtractTimeout 抽取 LLM 调用的整体超时（超时静默放弃本轮沉淀）。
const agentChatMemoryExtractTimeout = 10 * time.Second

// agentChatMemoryAutoCap 每用户 auto 来源记忆上限，达到后不再自动沉淀。
const agentChatMemoryAutoCap = 200

// extractConversationMemories 自动记忆沉淀：每当会话 assistant 消息数为 3 的倍数时，
// 用本轮相同的 provider/model 从最近约 6 条消息中抽取最多 3 条耐久记忆
// （fact/preference/observation/market_note），exact 去重后以 origin='auto' 落库。
// 任何失败静默跳过（不影响对话主流程）。
func (r *agentChatRunner) extractConversationMemories(repo *store.AgentChatRepo) {
	if r.convID == "" || r.provider == nil || r.provider.APIKey == "" {
		return
	}
	// 回合节拍：每 3 个 assistant 回合触发一次
	turns, err := repo.CountAssistantMessages(r.convID)
	if err != nil || turns == 0 || turns%3 != 0 {
		return
	}
	memRepo := agentmemory.NewRepo()
	// 上限保护：auto 记忆过多时停止自动沉淀
	if n, err := memRepo.CountByOrigin(int64(r.userID), "auto"); err != nil || n >= agentChatMemoryAutoCap {
		return
	}
	msgs, err := repo.ListMessages(r.convID)
	if err != nil || len(msgs) == 0 {
		return
	}
	if len(msgs) > 6 {
		msgs = msgs[len(msgs)-6:]
	}

	var b strings.Builder
	b.WriteString("从以下对话中抽取最多 3 条值得长期记住的关于用户的信息（用户的偏好、事实、习惯或市场观察）。每条一行，格式：- kind: 内容（kind 取 fact/preference/observation/market_note）。没有值得记住的内容时只输出 NONE。\n")
	for _, m := range msgs {
		fmt.Fprintf(&b, "%s: %s\n", m.Role, truncateAgentChat(m.Content, 300))
	}

	// provider.ChatCompletion 内部自带超时且不收 ctx：结果走 channel，外层 select 兜底。
	type extractResult struct {
		text string
		err  error
	}
	ch := make(chan extractResult, 1)
	go func() {
		resp, err := r.provider.ChatCompletion(ai.CompletionRequest{
			Model: r.provider.Model,
			Messages: []ai.ChatMessage{
				{Role: ai.RoleSystem, Content: "你是记忆抽取器，只按要求的行格式输出，不要任何解释。"},
				{Role: ai.RoleUser, Content: b.String()},
			},
		})
		if err != nil || resp == nil || len(resp.Choices) == 0 {
			ch <- extractResult{err: fmt.Errorf("memory extraction failed: %v", err)}
			return
		}
		ch <- extractResult{text: resp.Choices[0].Message.Content}
	}()

	var out string
	select {
	case res := <-ch:
		if res.err != nil {
			return
		}
		out = res.text
	case <-time.After(agentChatMemoryExtractTimeout):
		return
	}

	for _, item := range parseMemoryExtraction(out) {
		// exact 去重：同文记忆（任意来源）不重复沉淀
		if exists, err := memRepo.ExistsContent(int64(r.userID), item.content); err != nil || exists {
			continue
		}
		_ = memRepo.Create(&agentmemory.Memory{
			ID:                   agentmemory.NewID(),
			UserID:               int64(r.userID),
			Kind:                 item.kind,
			Content:              item.content,
			SourceConversationID: r.convID,
			Importance:           3,
			Origin:               "auto",
			ProfileID:            agentprofiles.NewRepo().ActiveProfileID(int64(r.userID)), // 写入当前激活档案
		})
	}
}

// extractedMemory 一条解析出的待沉淀记忆。
type extractedMemory struct {
	kind    string
	content string
}

// parseMemoryExtraction 解析抽取输出："- kind: content" 行（兼容全角冒号），
// NONE 与无法识别的行跳过，kind 非法跳过，最多 3 条。
func parseMemoryExtraction(out string) []extractedMemory {
	items := []extractedMemory{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "NONE") {
			continue
		}
		if !strings.HasPrefix(line, "-") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		idx, sepLen := strings.Index(line, ":"), 1
		if i := strings.Index(line, "："); i >= 0 && (idx < 0 || i < idx) {
			idx, sepLen = i, len("：")
		}
		if idx <= 0 {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(line[:idx]))
		content := strings.TrimSpace(line[idx+sepLen:])
		if !agentmemory.ValidKind(kind) || content == "" {
			continue
		}
		items = append(items, extractedMemory{kind: kind, content: content})
		if len(items) >= 3 {
			break
		}
	}
	return items
}
