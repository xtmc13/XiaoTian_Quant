package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/paper"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// GET /api/paper/account — 模拟盘账户状态（开关 + USDT 余额）
func PaperAccountGet(c *gin.Context) {
	c.JSON(http.StatusOK, paper.GetPaperExchange().GetAccount())
}

// POST /api/paper/account — body: {"enabled":bool?, "balance":float64?, "reset":bool?}
// balance 修改会连带清空全部持仓/挂单/订单簿（全新起点）；reset=true 仅按当前余额重置。
func PaperAccountSet(c *gin.Context) {
	var body struct {
		Enabled *bool    `json:"enabled"`
		Balance *float64 `json:"balance"`
		Reset   bool     `json:"reset"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body: " + err.Error()})
		return
	}
	pe := paper.GetPaperExchange()
	if body.Enabled != nil {
		pe.SetEnabled(*body.Enabled)
	}
	if body.Balance != nil {
		if *body.Balance < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "balance 不能为负数"})
			return
		}
		pe.SetBalance(*body.Balance)
	} else if body.Reset {
		// 仅重置：按当前余额清空持仓/挂单
		if b, ok := pe.GetAccount()["balance"].(float64); ok {
			pe.SetBalance(b)
		}
	}
	acc := pe.GetAccount()
	if b, err := json.Marshal(acc); err == nil {
		store.SavePaperAccountJSON(string(b))
	}
	c.JSON(http.StatusOK, acc)
}

// RestorePaperAccount 启动时恢复模拟盘账户状态（须在 store.InitDB 之后调用）。
func RestorePaperAccount() {
	s := store.GetPaperAccountJSON()
	if s == "" {
		return
	}
	var acc struct {
		Enabled bool    `json:"enabled"`
		Balance float64 `json:"balance"`
	}
	if json.Unmarshal([]byte(s), &acc) != nil {
		return
	}
	pe := paper.GetPaperExchange()
	pe.SetEnabled(acc.Enabled)
	if acc.Balance > 0 {
		pe.SetBalance(acc.Balance)
	}
}
