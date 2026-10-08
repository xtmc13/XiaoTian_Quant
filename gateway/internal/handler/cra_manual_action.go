package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── G1：CRA 运行时手动操控（币富名词解释 #23/#24/#25/#28）──
//
// POST /api/strategies/configs/:id/manual-action
// body: {action: "close_all"|"add_position"|"toggle_add_position"|"reduce_position",
//        amount?: number, qty?: number, ratio?: number, enabled?: boolean}
//
// 四个动作对运行中的 CRA 实例做手动干预：清仓卖出（#23，市价全平+暂停新开仓）、
// 一键补仓（#24，按保证金金额市价买入，入档不推自动阶梯）、关闭/开启补仓
// （#25，运行时开关，止盈/止损照常）、自定义减仓（#28，按数量或比例市价减仓，
// FIFO 核销）。引擎消费口径与状态语义见 cra/manual_action.go 头注。
//
// 安全语义全部复用既有链路，不开新通道：
//   - 鉴权/属主校验与 Start/Stop 等策略写端点相同（requireOwner）；
//   - 仅 running 状态可操作（store 口径，与引擎实例双校验——引擎内查无实例
//     同样拒绝，防 store/引擎状态漂移后的幽灵操作）；
//   - 仅 CRA 兼容类型（cra_contract/cra_spot 及 mapCRAFactory 前端别名，与
//     启动映射口径同源）；
//   - 下单走引擎 emitSignal → OnSignal 既有管道：execution_mode 非 live 强制
//     paper（applyStrategyExecConfig 防线）、paper 余额锁/背书钳制
//     （app/context.go）、OMS 风控检查一个不少——手动信号与自动信号零差异。

// manualActions 端点支持的四个动作（请求体形校验）。
var manualActions = map[string]bool{
	"close_all":           true,
	"add_position":        true,
	"toggle_add_position": true,
	"reduce_position":     true,
}

// isCRAManualActionItem 判定配置是否映射到 CRA 引擎（cra_contract/cra_spot
// 及 mapCRAFactory 别名）——手动操控四件套只对有 CRA 分档持仓模型的实例
// 有意义（经典指标策略无补仓阶梯/分档概念）。与 isCRAContractItem 同源但
// 现货合约都收。
func isCRAManualActionItem(item map[string]any) bool {
	st := strings.ToLower(strings.TrimSpace(getString(item, "strategy_type", "")))
	if st == "" {
		st = strings.ToLower(strings.TrimSpace(getString(item, "bot_type", "")))
	}
	if st == "cra_contract" || st == "cra_spot" {
		return true
	}
	if st == "" {
		return false
	}
	_, ok := mapCRAFactory(st, item)
	return ok
}

// ManualStrategyAction 运行时手动操控端点。请求体形校验（动作枚举/参数范围）
// 在 handler（400）；状态语义校验（无持仓/在途平仓/无行情）在引擎（409）。
func ManualStrategyAction(c *gin.Context) {
	id := c.Param("id")
	item := store.GetStrategyConfig(id)
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	if !requireOwner(c, getInt64Of(item, "user_id")) {
		return
	}
	if !isCRAManualActionItem(item) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "仅 CRA 策略（现货/合约及 CRA 别名类型）支持运行时手动操控"})
		return
	}
	if getString(item, "status", "") != "running" {
		c.JSON(http.StatusConflict, gin.H{"detail": "仅运行中的策略可手动操控，请先启动"})
		return
	}

	var body struct {
		Action  string   `json:"action"`
		Amount  *float64 `json:"amount"`
		Qty     *float64 `json:"qty"`
		Ratio   *float64 `json:"ratio"`
		Enabled *bool    `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid body: " + err.Error()})
		return
	}
	action := strings.ToLower(strings.TrimSpace(body.Action))
	if !manualActions[action] {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "未知动作 " + body.Action + "（支持 close_all/add_position/toggle_add_position/reduce_position）"})
		return
	}

	req := map[string]any{"action": action}
	switch action {
	case "add_position":
		if body.Amount == nil || *body.Amount <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "一键补仓需填写补仓金额 amount（保证金 USDT，>0）"})
			return
		}
		req["amount"] = *body.Amount
	case "reduce_position":
		hasQty := body.Qty != nil && *body.Qty > 0
		hasRatio := body.Ratio != nil && *body.Ratio > 0
		if hasQty == hasRatio {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "自定义减仓需且只需填写数量 qty 或比例 ratio（0<ratio<1）之一"})
			return
		}
		if hasRatio && *body.Ratio >= 1 {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "减仓比例必须小于 1（全平请用清仓卖出）"})
			return
		}
		if hasQty {
			req["qty"] = *body.Qty
		} else {
			req["ratio"] = *body.Ratio
		}
	case "toggle_add_position":
		if body.Enabled != nil {
			req["enabled"] = *body.Enabled
		}
	}

	eng := strategy.GetEngine(nil)
	if eng == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "strategy engine not initialized"})
		return
	}
	detail, err := eng.ManualAction(id, req)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"detail": detail["message"],
		"result": detail,
	})
}
