package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 工具调用审批流（对标 hermes approvals）──
// 配置门：agent.ai.approval_mode = off（默认，零行为变化）| writes（全部 ScopeWrite
// 工具执行前需用户确认）。交互式 web 回合暂停并等 /agent/chat/approve 决定；
// headless（cron / TG / 子代理）绝不暂停——自动批准并写审计备注。
//
// SSE 契约（仅新增，不改动既有事件）：
//
//	event: approval_request data: {"id":"ap_<rand>","tool":"place_paper_order","args_summary":"..."}

// agentChatApprovalMode 审批模式覆盖（测试缝，对齐 agentChatTitleUpgrade 模式）：
// "" = 读配置 agent.ai.approval_mode；测试可置 "writes" / "off"。
var agentChatApprovalMode = ""

// agentChatApprovalTimeout 审批等待超时（超时按拒绝处理；测试可调小）。
var agentChatApprovalTimeout = 120 * time.Second

// agentApprovalModeValue 当前生效的审批模式（off 之外的值目前仅认 writes）。
func agentApprovalModeValue() string {
	if agentChatApprovalMode != "" {
		return agentChatApprovalMode
	}
	cfg := store.GetConfig()
	agentCfg, _ := cfg["agent"].(map[string]any)
	aai, _ := agentCfg["ai"].(map[string]any)
	return strings.ToLower(strings.TrimSpace(getString(aai, "approval_mode", "")))
}

// agentApprovalRegistry 在途审批请求登记表（进程内存：单进程部署；重启后在途请求
// 随进程消失，等待侧按超时拒绝兜底）。id → 决定通道（缓冲 1， approve 端点投递）。
var agentApprovalRegistry sync.Map

// newApprovalID 生成审批请求 id（ap_<16 hex>）。
func newApprovalID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "ap_" + strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", "")
	}
	return "ap_" + hex.EncodeToString(buf)
}

// checkApproval 写类工具执行前的审批门；返回 true 放行。
//   - 模式非 writes：直接放行（默认 off = 零行为变化）；
//   - headless（cron / TG / 子代理，无 SSE 通道）：自动批准并写审计备注；
//   - 交互式 web：发 approval_request 事件并阻塞等决定——超时 / 客户端断开按拒绝。
func (r *agentChatRunner) checkApproval(tool, argsSummary string) bool {
	if agentApprovalModeValue() != "writes" {
		return true
	}
	if r.c == nil || r.c.Request == nil || !r.stream {
		// headless / 非流式（无审批交互通道）：自动批准，审计留痕。
		ip, ua := "", ""
		if r.c != nil && r.c.Request != nil {
			ip = r.c.ClientIP()
			ua = r.c.Request.UserAgent()
		}
		agent.GetTokenManager().LogAccess(
			r.tokenID, tool, "/agent/chat/approval", "AUTO",
			"自动批准（非交互通道）: "+argsSummary, http.StatusOK, ip, ua,
		)
		return true
	}

	id := newApprovalID()
	ch := make(chan bool, 1)
	agentApprovalRegistry.Store(id, ch)
	defer agentApprovalRegistry.Delete(id)

	r.emitEvent("approval_request", gin.H{"id": id, "tool": tool, "args_summary": argsSummary})

	select {
	case approve := <-ch:
		return approve
	case <-time.After(agentChatApprovalTimeout):
		return false // 超时按拒绝
	case <-r.c.Request.Context().Done():
		return false // 客户端断开按拒绝（run 循环随 aborted 静默收尾）
	}
}

// AgentChatApprove 处理 POST /api/agent/chat/approve：投递一次在途审批决定。
// body {"id":"ap_x","approve":true|false} → {"success":true}；未知/过期 id → 404。
func AgentChatApprove(c *gin.Context) {
	var req struct {
		ID      string `json:"id"`
		Approve bool   `json:"approve"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.ID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": gin.H{"code": "BAD_REQUEST", "message": "invalid request body"}})
		return
	}
	v, ok := agentApprovalRegistry.Load(req.ID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": gin.H{"code": "APPROVAL_NOT_FOUND", "message": "approval not found or expired"}})
		return
	}
	ch := v.(chan bool)
	select {
	case ch <- req.Approve:
	default: // 已超时/已投递：幂等成功
	}
	agentApprovalRegistry.Delete(req.ID)
	c.JSON(http.StatusOK, gin.H{"success": true})
}
