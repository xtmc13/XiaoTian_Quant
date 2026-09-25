package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/order"
)

// ── 阶梯智能单（Ladder Smart Orders）REST ──
// 语义见 order/ladder.go 包注释。归属校验复用高级订单同套口径
// （advancedOrderUID/advancedOrderOwned/advancedOrderRestricted）。

// ladderEngine 取全局阶梯引擎（生产在 main 接线 Start + RestoreFromStore）。
func ladderEngine() *order.LadderEngine { return order.GetLadderEngine() }

// PlaceLadder 创建阶梯单。
func PlaceLadder(c *gin.Context) {
	var body order.LadderSpec
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	l, err := ladderEngine().Create(&body, advancedOrderUID(c))
	if err != nil {
		resp := gin.H{"error": err.Error()}
		if l != nil {
			resp["ladder"] = l.Snapshot()
		}
		c.JSON(http.StatusBadRequest, resp)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "ladder": l.Snapshot()})
}

// ListLadders 列出阶梯单（登录非 admin 仅看自己的；?all=1 含终态，默认仅活动中）。
func ListLadders(c *gin.Context) {
	eng := ladderEngine()
	var uid uint64
	if advancedOrderRestricted(c) {
		uid = advancedOrderUID(c)
	}
	all := eng.List(uid)
	out := make([]*order.LadderOrder, 0, len(all))
	for _, l := range all {
		snap := l.Snapshot()
		if c.Query("all") != "1" && snap.IsTerminal() {
			continue
		}
		out = append(out, snap)
	}
	c.JSON(http.StatusOK, gin.H{"orders": out, "count": len(out)})
}

// GetLadder 返回单张阶梯单（含各档/各目标状态）。
func GetLadder(c *gin.Context) {
	l := ladderEngine().Get(c.Param("id"))
	if l == nil || !advancedOrderOwned(c, l.UserID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "ladder not found"})
		return
	}
	c.JSON(http.StatusOK, l.Snapshot())
}

// CancelLadder 撤销阶梯单：撤全部挂单，保留已建仓位。
func CancelLadder(c *gin.Context) {
	l := ladderEngine().Get(c.Param("id"))
	if l == nil || !advancedOrderOwned(c, l.UserID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "ladder not found"})
		return
	}
	updated, err := ladderEngine().Cancel(l.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "cancelled", "ladder": updated.Snapshot()})
}

// FlattenLadder 一键全平：撤全部挂单 + 市价平剩余仓位。
func FlattenLadder(c *gin.Context) {
	l := ladderEngine().Get(c.Param("id"))
	if l == nil || !advancedOrderOwned(c, l.UserID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "ladder not found"})
		return
	}
	updated, err := ladderEngine().Flatten(l.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "flattening", "ladder": updated.Snapshot()})
}

// UpdateLadder 拖动改价：修改未成交档位/目标价格、SL 与规则参数。
func UpdateLadder(c *gin.Context) {
	l := ladderEngine().Get(c.Param("id"))
	if l == nil || !advancedOrderOwned(c, l.UserID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "ladder not found"})
		return
	}
	var body order.LadderAmend
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, err := ladderEngine().Amend(l.ID, &body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "ladder": updated.Snapshot()})
}
