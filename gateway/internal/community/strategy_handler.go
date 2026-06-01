package community

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
)

// ── Publish Strategy ──

type publishStrategyReq struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Code        string          `json:"code"`
	Symbol      string          `json:"symbol"`
	Interval    string          `json:"interval"`
	Backtest    *BacktestSummary `json:"backtest"`
}

func PublishStrategy(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	uid, _ := userID.(int)
	userName, _ := c.Get("username")
	uname, _ := userName.(string)

	var req publishStrategyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "invalid request: " + err.Error()})
		return
	}
	if req.Name == "" || req.Code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "name and code are required"})
		return
	}
	if req.Symbol == "" {
		req.Symbol = "BTCUSDT"
	}
	if req.Interval == "" {
		req.Interval = "1h"
	}

	id, err := svc.PublishStrategy(uid, req.Name, req.Description, req.Code, req.Symbol, req.Interval, uname, req.Backtest)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "published", "data": gin.H{"id": id}})
}

// ── List Strategies ──

func MarketStrategies(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	uid, _ := userID.(int)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "12"))
	keyword := c.Query("keyword")
	sortBy := c.DefaultQuery("sort_by", "newest")

	items, total, err := svc.GetMarketStrategies(uid, page, pageSize, keyword, sortBy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":      1,
		"msg":       "ok",
		"data":      items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ── Strategy Detail ──

func StrategyDetail(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "invalid id"})
		return
	}

	strategy, comments, err := svc.GetStrategyDetail(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":     1,
		"msg":      "ok",
		"data":     strategy,
		"comments": comments,
	})
}

// ── Add Comment ──

type addCommentReq struct {
	Content string `json:"content"`
}

func AddStrategyComment(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	uid, _ := userID.(int)
	userName, _ := c.Get("username")
	uname, _ := userName.(string)

	idStr := c.Param("id")
	strategyID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "invalid id"})
		return
	}

	var req addCommentReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "content is required"})
		return
	}

	comment, err := svc.AddStrategyComment(strategyID, uid, uname, req.Content)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "comment added", "data": comment})
}

// ── Rate Strategy ──

type rateReq struct {
	Rating int `json:"rating"`
}

func RateStrategy(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	uid, _ := userID.(int)

	idStr := c.Param("id")
	strategyID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "invalid id"})
		return
	}

	var req rateReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Rating < 1 || req.Rating > 5 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "rating must be 1-5"})
		return
	}

	if err := svc.RateStrategy(strategyID, uid, req.Rating); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "rated"})
}

// ── Leaderboard ──

func StrategyLeaderboard(c *gin.Context) {
	sortBy := c.DefaultQuery("sort_by", "sharpe")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if limit > 50 {
		limit = 50
	}

	items, _ := svc.GetStrategyLeaderboard(sortBy, limit)
	if items == nil {
		items = []StrategyItem{}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 1,
		"msg":  "ok",
		"data": items,
	})
}
