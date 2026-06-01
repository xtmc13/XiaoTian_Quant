package community

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
)

var svc = NewService()

// MarketIndicators godoc
// GET /api/community/indicators
func MarketIndicators(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	uid, _ := userID.(int)

	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	if pageSize < 1 {
		pageSize = 12
	}

	items, total, err := svc.GetMarketIndicators(
		uid,
		page,
		pageSize,
		c.Query("keyword"),
		c.Query("pricing_type"),
		c.Query("sort_by"),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error(), "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 1,
		"msg":  "success",
		"data": gin.H{
			"items":      items,
			"total":      total,
			"page":       page,
			"page_size":  pageSize,
			"total_pages": (total + pageSize - 1) / pageSize,
		},
	})
}

// PublishIndicator godoc
// POST /api/community/publish
func PublishIndicator(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	uid, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized"})
		return
	}

	var req struct {
		IndicatorID  int     `json:"indicatorId" binding:"required"`
		PricingType  string  `json:"pricingType"`
		Price        float64 `json:"price"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	if req.PricingType == "" {
		req.PricingType = "free"
	}

	if err := svc.PublishIndicator(uid, req.IndicatorID, req.PricingType, req.Price); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "published", "data": nil})
}

// PurchaseIndicator godoc
// POST /api/community/purchase/:id
func PurchaseIndicator(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	uid, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized"})
		return
	}

	idStr := c.Param("id")
	indicatorID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "invalid id"})
		return
	}

	success, msg, err := svc.PurchaseIndicator(uid, indicatorID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	if !success {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": msg})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": msg, "data": nil})
}

// AddComment godoc
// POST /api/community/comments/:id
func AddComment(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	uid, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized"})
		return
	}

	idStr := c.Param("id")
	indicatorID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "invalid id"})
		return
	}

	var req struct {
		Rating  int    `json:"rating"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	success, msg, err := svc.AddComment(uid, indicatorID, req.Rating, req.Content)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	if !success {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": msg})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": msg})
}

// GetComments godoc
// GET /api/community/comments/:id
func GetComments(c *gin.Context) {
	idStr := c.Param("id")
	indicatorID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "invalid id"})
		return
	}

	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	if pageSize < 1 {
		pageSize = 20
	}

	items, total, err := svc.GetComments(indicatorID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 1,
		"msg":  "success",
		"data": gin.H{
			"items":       items,
			"total":       total,
			"page":        page,
			"page_size":   pageSize,
			"total_pages": (total + pageSize - 1) / pageSize,
		},
	})
}

func OverfitCheck(c *gin.Context) {
	totalReturn, _ := strconv.ParseFloat(c.DefaultQuery("total_return", "0"), 64)
	sharpe, _ := strconv.ParseFloat(c.DefaultQuery("sharpe", "0"), 64)
	maxDD, _ := strconv.ParseFloat(c.DefaultQuery("max_drawdown", "0"), 64)
	trades, _ := strconv.Atoi(c.DefaultQuery("total_trades", "0"))
	sampleRatio, _ := strconv.ParseFloat(c.DefaultQuery("sample_ratio", "0.7"), 64)
	result := ComputeOverfitRisk(totalReturn, sharpe, maxDD, trades, sampleRatio)
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "ok", "data": gin.H{"overfit": result}})
}
