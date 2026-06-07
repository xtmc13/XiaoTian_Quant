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
	c.JSON(http.StatusOK, gin.H{"success": true, "id": id})
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
		"items": items,
		"total": total,
		"page":  page,
		"page_size": pageSize,
	})
}

// ── Strategy Detail ──

func StrategyDetail(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	strategy, comments, err := svc.GetStrategyDetail(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":              strategy.ID,
		"name":            strategy.Name,
		"description":     strategy.Description,
		"code":            strategy.Code,
		"symbol":          strategy.Symbol,
		"interval":        strategy.Interval,
		"author_id":       strategy.AuthorID,
		"author_name":     strategy.AuthorName,
		"total_return":    strategy.TotalReturn,
		"sharpe_ratio":    strategy.SharpeRatio,
		"max_drawdown":    strategy.MaxDrawdown,
		"win_rate":        strategy.WinRate,
		"profit_factor":   strategy.ProfitFactor,
		"total_trades":    strategy.TotalTrades,
		"avg_trade":       strategy.TotalReturn / float64(max(strategy.TotalTrades, 1)),
		"rating":          strategy.AvgRating,
		"rating_count":    strategy.RatingCount,
		"purchase_count":  strategy.DownloadCount,
		"view_count":      strategy.ViewCount,
		"created_at":      strategy.CreatedAt,
		"updated_at":      strategy.UpdatedAt,
		"comments":        comments,
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
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req addCommentReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "content is required"})
		return
	}

	comment, err := svc.AddStrategyComment(strategyID, uid, uname, req.Content)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "comment_id": comment.ID})
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
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req rateReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Rating < 1 || req.Rating > 5 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "rating must be 1-5"})
		return
	}

	if err := svc.RateStrategy(strategyID, uid, req.Rating); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
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

	c.JSON(http.StatusOK, items)
}

// ── Trending Strategies ──

func TrendingStrategies(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if limit > 50 {
		limit = 50
	}

	items, _ := svc.GetTrendingStrategies(limit)
	if items == nil {
		items = []StrategyItem{}
	}

	c.JSON(http.StatusOK, items)
}

// ── Overfit Risk ──

func GetStrategyOverfitRisk(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	result, err := svc.GetOverfitRisk(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	if result == nil {
		// Real-time compute if not persisted
		strategy, _, err := svc.GetStrategyDetail(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
			return
		}
		result = ComputeOverfitRisk(strategy.TotalReturn, strategy.SharpeRatio, strategy.MaxDrawdown, strategy.TotalTrades, 0.7)
		_ = svc.SaveOverfitRisk(id, result)
	}

	c.JSON(http.StatusOK, result)
}
