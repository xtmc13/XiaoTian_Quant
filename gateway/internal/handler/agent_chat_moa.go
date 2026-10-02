package handler

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── MoA（Mixture of Agents，对标 hermes /moa）──
// 请求带 "moa": true 时，在正式 tool-calling 循环之前，把用户最新提问（同一系统提示词、
// 不带工具）并行扇出到所有持有凭证的其它厂商，收集成功回答后拼成综合参考块，
// 临时前置到最后一条 user 消息上（仅注入主厂商本轮上下文，不落库、不改持久化内容）。
//
// SSE 契约（仅扩展，不影响既有事件）：
//
//	event: moa data: {"proposers":["kimi","deepseek"]}           任何 delta 之前发一次（主厂商在前）
//	event: moa data: {"proposers":[],"note":"厂商不足，按普通模式"}  可用厂商（含主厂商）<2 时
//
// 非流式 JSON 路径不发 moa 事件，改为响应体带 moa_proposers 字段。

var (
	// agentMoaMaxProposers 扇出厂商上限（不含主厂商）。
	agentMoaMaxProposers = 4
	// agentMoaProposeTimeout 单个厂商提问的整体超时（超时按失败容忍）。
	agentMoaProposeTimeout = 20 * time.Second
	// agentMoaAnswerLimit 单个厂商回答注入综合块的截断长度（rune）。
	agentMoaAnswerLimit = 1500

	// agentMoaDiscoverProposers 提案厂商发现（持有凭证的厂商集合）；测试可注入假实现。
	agentMoaDiscoverProposers = moaProposerProviders
	// agentMoaPropose 单个厂商提问；测试可注入假实现（不走 HTTP）。
	agentMoaPropose = moaProposeDefault
)

// moaProposerProviders 发现可参与 MoA 的提案厂商：注册表中持有凭证
// （env 注入的注册表 key、全局 ai.{name}.api_key 配置、或用户级 xt_agent_user_ai key）
// 的厂商，排除主厂商，按名称排序（结果确定），最多 agentMoaMaxProposers 个。
// 返回的均为克隆体（合并配置/用户级凭证），不动注册表。
func moaProposerProviders(userID int64, primaryName string) []*ai.Provider {
	cfg := store.GetConfig()
	aiCfg, _ := cfg["ai"].(map[string]any)
	// 用户级覆盖仅属于其指定厂商（与 configuredAgentAIProviderForUser 同语义）。
	var userRec *store.AgentUserAIRecord
	if userID > 0 {
		if rec, err := store.NewAgentUserAIRepo().Get(userID); err == nil {
			userRec = rec
		}
	}

	names := ai.ListProviders()
	sort.Strings(names)
	out := make([]*ai.Provider, 0, agentMoaMaxProposers)
	for _, name := range names {
		if name == primaryName {
			continue
		}
		p := ai.GetProvider(name)
		if p == nil {
			continue
		}
		clone := *p
		// 设置页保存的 per-provider 配置（key/model/base_url）覆盖注册表默认。
		if providerCfg := agentAIProviderConfig(aiCfg, name); providerCfg != nil {
			if k := getString(providerCfg, "api_key", ""); k != "" {
				clone.APIKey = k
				if m := getString(providerCfg, "model", ""); m != "" {
					clone.Model = m
				}
				if bu := getString(providerCfg, "base_url", ""); bu != "" {
					clone.BaseURL = bu
				}
			}
		}
		// 用户级凭证覆盖该厂商的全局/env 凭证。
		if userRec != nil && userRec.Provider == name {
			if userRec.APIKey != "" {
				clone.APIKey = userRec.APIKey
			}
			if userRec.Model != "" {
				clone.Model = userRec.Model
			}
		}
		if clone.APIKey == "" {
			continue // 无凭证的厂商不参与
		}
		out = append(out, &clone)
		if len(out) >= agentMoaMaxProposers {
			break
		}
	}
	return out
}

// agentAIProviderConfig 读设置页保存的 per-provider 配置：ai.{name}，
// 兼容 legacy 别名（ai.anthropic → claude）与嵌套形态 ai.providers.{name}。
// configuredAgentAIProviderForUser 与 MoA 厂商发现共用。
func agentAIProviderConfig(aiCfg map[string]any, providerName string) map[string]any {
	if aiCfg == nil {
		return nil
	}
	if providerCfg, ok := aiCfg[providerName].(map[string]any); ok {
		return providerCfg
	}
	if legacy := ai.LegacyProviderName(providerName); legacy != "" {
		if providerCfg, ok := aiCfg[legacy].(map[string]any); ok {
			return providerCfg
		}
	}
	if nested, ok := aiCfg["providers"].(map[string]any); ok {
		if providerCfg, ok := nested[providerName].(map[string]any); ok {
			return providerCfg
		}
	}
	return nil
}

// moaProposeDefault 单个厂商提问：同一系统提示词 + 用户最新提问，不带工具。
// ChatCompletion 内部自带超时但不收 ctx：结果走 channel，外层 select 兜底
// agentMoaProposeTimeout，超时按该厂商失败处理（其余厂商不受影响）。
func moaProposeDefault(ctx context.Context, p *ai.Provider, sysPrompt, prompt string) (string, error) {
	type result struct {
		text string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := p.ChatCompletion(ai.CompletionRequest{
			Model: p.Model,
			Messages: []ai.ChatMessage{
				{Role: ai.RoleSystem, Content: sysPrompt},
				{Role: ai.RoleUser, Content: prompt},
			},
		})
		if err != nil {
			ch <- result{err: err}
			return
		}
		if resp == nil || len(resp.Choices) == 0 {
			ch <- result{err: fmt.Errorf("空响应")}
			return
		}
		ch <- result{text: resp.Choices[0].Message.Content}
	}()
	select {
	case res := <-ch:
		return res.text, res.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// moaAnswer 一家厂商的成功回答（保持发现顺序注入综合块）。
type moaAnswer struct {
	name string
	text string
}

// runMoaPropose MoA 前置扇出：在 tool-calling 循环之前调用。
// 发 moa 事件（流式路径），并在拿到 ≥1 家成功回答时把综合参考块前置到
// 历史中最后一条 user 消息（瞬态，仅影响发给主厂商的上下文）。
func (r *agentChatRunner) runMoaPropose(sysPrompt string) {
	userPrompt := lastHistoryUser(r.history)
	if userPrompt == "" {
		return // 无用户提问：无从扇出（正常循环照旧，不发事件）
	}
	proposers := agentMoaDiscoverProposers(int64(r.userID), r.provider.Name)
	if len(proposers) == 0 {
		// 含主厂商在内不足 2 家：按普通模式进行
		r.moaProposers = []string{}
		r.emitEvent("moa", gin.H{"proposers": []string{}, "note": "厂商不足，按普通模式"})
		return
	}

	// moa 事件列出实际参与厂商（主厂商在前），在任何 delta 之前发一次。
	names := make([]string, 0, len(proposers)+1)
	names = append(names, r.provider.Name)
	for _, p := range proposers {
		names = append(names, p.Name)
	}
	r.moaProposers = names
	r.emitEvent("moa", gin.H{"proposers": names})

	// 并行扇出：单家失败/超时仅缺失该家，不阻塞其余。
	answers := make([]moaAnswer, len(proposers))
	var wg sync.WaitGroup
	for i, p := range proposers {
		wg.Add(1)
		go func(i int, p *ai.Provider) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), agentMoaProposeTimeout)
			defer cancel()
			text, err := agentMoaPropose(ctx, p, sysPrompt, userPrompt)
			if err != nil || strings.TrimSpace(text) == "" {
				return
			}
			answers[i] = moaAnswer{name: p.Name, text: text}
		}(i, p)
	}
	wg.Wait()

	// 拼综合参考块：全部失败则无块可注（本轮退化为普通模式）。
	var b strings.Builder
	n := 0
	for _, a := range answers {
		if a.text == "" {
			continue
		}
		if n == 0 {
			b.WriteString("（以下是对同一问题的多家模型回答，供你综合参考，取其精华、指出分歧：\n")
		}
		fmt.Fprintf(&b, "【%s】%s\n", a.name, truncateAgentChat(a.text, agentMoaAnswerLimit))
		n++
	}
	if n == 0 {
		return
	}
	block := strings.TrimRight(b.String(), "\n") + "）"
	for i := len(r.history) - 1; i >= 0; i-- {
		if r.history[i].Role == ai.RoleUser {
			r.history[i].Content = block + "\n\n" + r.history[i].Content
			break
		}
	}
}
