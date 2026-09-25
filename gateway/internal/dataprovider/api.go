package dataprovider

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes 挂载数据生态端点（调用方负责外层 /dataproviders 前缀与鉴权）。
//
//	GET /sentiment      恐惧贪婪指数 + Coinglass 衍生品情绪（未配置自动降级）
//	GET /macro          FRED 宏观序列表
//	GET /news?symbol=   最新加密新闻（可选按币种过滤）
//	GET /heatmap        主流币 24h 涨跌幅热力图（自算，免 key）
//	GET /calendar       本周经济日历
//	GET /sources        各源健康状态
func RegisterRoutes(r *gin.RouterGroup, svc *Service) {
	h := &apiHandler{svc: svc}
	r.GET("/sentiment", h.getSentiment)
	r.GET("/macro", h.getMacro)
	r.GET("/news", h.getNews)
	r.GET("/heatmap", h.getHeatmap)
	r.GET("/calendar", h.getCalendar)
	r.GET("/sources", h.getSources)
}

type apiHandler struct{ svc *Service }

func (h *apiHandler) getSentiment(c *gin.Context) {
	fg := h.svc.Get(c.Request.Context(), "fear_greed")
	deriv := h.svc.Get(c.Request.Context(), "coinglass")
	c.JSON(http.StatusOK, gin.H{
		"fear_greed":  fg,
		"derivatives": deriv,
	})
}

func (h *apiHandler) getMacro(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.Get(c.Request.Context(), "fred"))
}

func (h *apiHandler) getNews(c *gin.Context) {
	res := h.svc.Get(c.Request.Context(), "news")
	if symbol := strings.TrimSpace(c.Query("symbol")); symbol != "" && res.Data != nil {
		// Data 可能是 *NewsData（内存缓存）或 json.RawMessage（DB 回灌），统一走 JSON 归一。
		if raw, err := json.Marshal(res.Data); err == nil {
			var nd NewsData
			if json.Unmarshal(raw, &nd) == nil && nd.Items != nil {
				nd.Items = FilterNewsBySymbol(nd.Items, symbol)
				res.Data = &nd
			}
		}
	}
	c.JSON(http.StatusOK, res)
}

func (h *apiHandler) getHeatmap(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.Get(c.Request.Context(), "heatmap"))
}

func (h *apiHandler) getCalendar(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.Get(c.Request.Context(), "calendar"))
}

func (h *apiHandler) getSources(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"sources": h.svc.Health()})
}
