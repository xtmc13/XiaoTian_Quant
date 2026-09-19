package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/factors"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 因子研究（A6.1） ──
// REST 层很薄：取数（本地存储 → Binance REST 兜底）→ factors 包计算 →
// 结果落库 factors_evaluations（含 user_id 属主）。评价全部在无网络依赖的
// factors 包内完成，handler 只做参数解析与持久化。

// loadResearchBars 为因子研究/组合回测加载 K 线：优先本地存储，
// 不足时从 Binance REST 拉取并落本地（与 RunBacktest 同一口径）。
// 返回 bars 与数据来源标识。
func loadResearchBars(symbol, interval string, numBars int, fromMs, toMs int64) ([]model.Bar, string, error) {
	if DataDownloader != nil {
		if bars := DataDownloader.LoadBarsForBacktest(symbol, interval, fromMs, toMs); len(bars) >= 50 {
			return bars, "local_storage", nil
		}
	}
	klines, err := fetchBinanceKlines(symbol, interval, numBars, fromMs, toMs)
	if err != nil {
		return nil, "", fmt.Errorf("从 Binance 获取 %s %s K线失败: %w", symbol, interval, err)
	}
	if len(klines) == 0 {
		return nil, "", fmt.Errorf("Binance 未返回 %s %s 的K线数据", symbol, interval)
	}
	bars := make([]model.Bar, 0, len(klines))
	for _, k := range klines {
		bars = append(bars, model.Bar{
			Symbol:   symbol,
			Open:     getFloat(k, "open", 0),
			High:     getFloat(k, "high", 0),
			Low:      getFloat(k, "low", 0),
			Close:    getFloat(k, "close", 0),
			Volume:   getFloat(k, "volume", 0),
			Interval: interval,
			Time:     int64(getFloat(k, "time", 0)),
		})
	}
	if DataDownloader != nil && len(bars) >= 50 {
		go func() { _ = DataDownloader.SaveBars(bars) }()
	}
	return bars, "Binance", nil
}

// parseResearchWindow 解析通用研究请求参数：symbol/tf/from/to/limit。
func parseResearchWindow(c *gin.Context) (symbol, tf string, fromMs, toMs int64, limit int) {
	symbol = strings.ToUpper(strings.TrimSpace(c.Query("symbol")))
	if symbol == "" {
		symbol = "BTCUSDT"
	}
	tf = strings.ToLower(strings.TrimSpace(c.Query("tf")))
	if tf == "" {
		tf = strings.ToLower(strings.TrimSpace(c.Query("interval")))
	}
	if tf == "" {
		tf = "1h"
	}
	if fromStr := c.Query("from"); fromStr != "" {
		if t, err := time.Parse("2006-01-02", fromStr); err == nil {
			fromMs = t.UnixMilli()
		}
	}
	if toStr := c.Query("to"); toStr != "" {
		if t, err := time.Parse("2006-01-02", toStr); err == nil {
			toMs = t.UnixMilli()
		}
	}
	limit = 1000
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = min(max(n, 100), 1500)
		}
	}
	return symbol, tf, fromMs, toMs, limit
}

// resolveFactorDef 按 name[/version] 解析因子定义。
// version 取值优先级：query 参数 > body 字段 > name 后缀 "roc@2" > 最新版。
func resolveFactorDef(c *gin.Context, name string, bodyVersion int) (*factors.Def, int, error) {
	version := bodyVersion
	if v := c.Query("version"); v != "" {
		fmt.Sscanf(v, "%d", &version)
	}
	// 允许 name 形如 roc@2
	if i := strings.LastIndex(name, "@"); i > 0 {
		if n, err := strconv.Atoi(name[i+1:]); err == nil {
			version = n
			name = name[:i]
		}
	}
	def, err := factors.Get(name, version)
	if err != nil {
		return nil, 0, err
	}
	return def, version, nil
}

// GetFactors 返回内置因子列表（含版本）。
func GetFactors(c *gin.Context) {
	defs := factors.List()
	c.JSON(http.StatusOK, gin.H{
		"factors":    defs,
		"categories": factors.Categories(),
		"count":      len(defs),
	})
}

// factorValuesRequest 是 /values 的查询参数集合。
type factorValuesRequest struct {
	Symbol  string         `json:"symbol"`
	TF      string         `json:"tf"`
	From    string         `json:"from"`
	To      string         `json:"to"`
	Limit   int            `json:"limit"`
	Params  map[string]any `json:"params"`
	Version int            `json:"version"`
}

// GetFactorValues 计算并返回因子值序列（供前端叠加到 K 线）。
// GET /api/factors/:name/values?symbol=&tf=&limit=&from=&to=
func GetFactorValues(c *gin.Context) {
	name := c.Param("name")
	def, _, err := resolveFactorDef(c, name, 0)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": err.Error()})
		return
	}
	symbol, tf, fromMs, toMs, limit := parseResearchWindow(c)

	params := map[string]any{}
	if raw := c.Query("params"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &params)
	}

	bars, source, err := loadResearchBars(symbol, tf, limit, fromMs, toMs)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "无法获取历史数据", "detail": err.Error()})
		return
	}
	if len(bars) < 50 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "数据不足", "detail": fmt.Sprintf("仅 %d 根K线，至少需要 50 根", len(bars))})
		return
	}

	series := factors.ComputeSeries(def, bars, params)
	values := factors.ToValues(bars, series, limit)

	// 同步输出收盘价供前端对齐展示
	closes := make([]map[string]any, 0, len(bars))
	for _, b := range bars {
		closes = append(closes, map[string]any{"time": b.Time, "close": store.RoundFloat(b.Close, 8)})
	}

	c.JSON(http.StatusOK, gin.H{
		"factor":         def.Name,
		"version":        def.Version,
		"category":       def.Category,
		"symbol":         symbol,
		"tf":             tf,
		"bars_used":      len(bars),
		"source":         source,
		"values":         values,
		"closes":         closes,
		"default_params": def.Defaults,
	})
}

// factorEvaluateRequest 是 /evaluate 与 /layers 的请求体。
type factorEvaluateRequest struct {
	Name        string         `json:"name" binding:"required"`
	Symbol      string         `json:"symbol"`
	TF          string         `json:"tf"`
	From        string         `json:"from"`
	To          string         `json:"to"`
	Limit       int            `json:"limit"`
	ForwardBars int            `json:"forward_bars"`
	ICWindow    int            `json:"ic_window"`
	LayerCount  int            `json:"layer_count"`
	Lookback    int            `json:"lookback"`
	Params      map[string]any `json:"params"`
	Version     int            `json:"version"`
	Save        bool           `json:"save"`
}

func (r *factorEvaluateRequest) normalize() {
	r.Symbol = strings.ToUpper(strings.TrimSpace(r.Symbol))
	if r.Symbol == "" {
		r.Symbol = "BTCUSDT"
	}
	r.TF = strings.ToLower(strings.TrimSpace(r.TF))
	if r.TF == "" {
		r.TF = "1h"
	}
	if r.ForwardBars <= 0 {
		r.ForwardBars = 5
	}
	if r.ForwardBars > 100 {
		r.ForwardBars = 100
	}
	if r.Limit <= 0 {
		r.Limit = 1000
	}
	if r.Limit > 1500 {
		r.Limit = 1500
	}
}

func (r *factorEvaluateRequest) window() (fromMs, toMs int64) {
	if r.From != "" {
		if t, err := time.Parse("2006-01-02", r.From); err == nil {
			fromMs = t.UnixMilli()
		}
	}
	if r.To != "" {
		if t, err := time.Parse("2006-01-02", r.To); err == nil {
			toMs = t.UnixMilli()
		}
	}
	return
}

// saveFactorEvaluation 落库评价结果；失败仅记日志不阻塞响应。
func saveFactorEvaluation(c *gin.Context, kind string, def *factors.Def, req *factorEvaluateRequest, resultJSON string, samples int) {
	paramsJSON, _ := json.Marshal(req.Params)
	rec := &store.FactorEvaluationRecord{
		UserID:        getUserID(c),
		Kind:          kind,
		FactorName:    def.Name,
		FactorVersion: def.Version,
		Category:      def.Category,
		Symbol:        req.Symbol,
		TF:            req.TF,
		ForwardBars:   req.ForwardBars,
		ParamsJSON:    string(paramsJSON),
		Samples:       samples,
		ResultJSON:    resultJSON,
	}
	if err := store.NewFactorEvaluationRepo().Create(rec); err != nil {
		fmt.Printf("[factors] persist evaluation failed: %v\n", err)
	}
}

// EvaluateFactor 计算 IC/RankIC/ICIR 并落库。
// POST /api/factors/evaluate
func EvaluateFactor(c *gin.Context) {
	var req factorEvaluateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json: " + err.Error()})
		return
	}
	req.normalize()

	def, _, err := resolveFactorDef(c, req.Name, req.Version)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": err.Error()})
		return
	}
	fromMs, toMs := req.window()
	bars, source, err := loadResearchBars(req.Symbol, req.TF, req.Limit, fromMs, toMs)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "无法获取历史数据", "detail": err.Error()})
		return
	}
	if len(bars) < 50 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "数据不足", "detail": fmt.Sprintf("仅 %d 根K线，至少需要 50 根", len(bars))})
		return
	}

	ev, err := factors.Evaluate(def, bars, req.Params, factors.EvaluationConfig{
		ForwardBars: req.ForwardBars,
		ICWindow:    req.ICWindow,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "evaluation failed", "detail": err.Error()})
		return
	}
	if ev == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "样本不足", "detail": "有效样本不足以计算滚动 IC，请扩大数据范围或减小 ic_window/forward_bars"})
		return
	}
	ev.Symbol = req.Symbol
	ev.TF = req.TF

	resultJSON, _ := json.Marshal(ev)
	if req.Save {
		saveFactorEvaluation(c, "ic", def, &req, string(resultJSON), ev.Samples)
	}

	c.JSON(http.StatusOK, gin.H{
		"evaluation": ev,
		"bars_used":  len(bars),
		"source":     source,
	})
}

// FactorLayers 计算分层回测并落库。
// POST /api/factors/layers
func FactorLayers(c *gin.Context) {
	var req factorEvaluateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json: " + err.Error()})
		return
	}
	req.normalize()
	if req.LayerCount < 2 {
		req.LayerCount = 5
	}
	if req.LayerCount > 10 {
		req.LayerCount = 10
	}
	if req.Lookback < 20 {
		req.Lookback = 120
	}

	def, _, err := resolveFactorDef(c, req.Name, req.Version)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": err.Error()})
		return
	}
	fromMs, toMs := req.window()
	bars, source, err := loadResearchBars(req.Symbol, req.TF, req.Limit, fromMs, toMs)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "无法获取历史数据", "detail": err.Error()})
		return
	}
	if len(bars) < 50 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "数据不足", "detail": fmt.Sprintf("仅 %d 根K线，至少需要 50 根", len(bars))})
		return
	}

	res, err := factors.LayeredBacktest(def, bars, req.Params, factors.EvaluationConfig{
		ForwardBars: req.ForwardBars,
	}, req.LayerCount, req.Lookback)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "layered backtest failed", "detail": err.Error()})
		return
	}
	res.Symbol = req.Symbol
	res.TF = req.TF

	resultJSON, _ := json.Marshal(res)
	if req.Save {
		saveFactorEvaluation(c, "layers", def, &req, string(resultJSON), res.Samples)
	}

	c.JSON(http.StatusOK, gin.H{
		"result":    res,
		"bars_used": len(bars),
		"source":    source,
	})
}

// ListFactorEvaluations 返回当前用户的评价历史。
// GET /api/factors/evaluations?factor=&limit=
func ListFactorEvaluations(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		// 未注入用户（单用户模式）：不限属主
		userID = 0
	}
	limit := 100
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = min(n, 500)
		}
	}
	recs, err := store.NewFactorEvaluationRepo().ListByUser(userID, c.Query("factor"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to list evaluations"})
		return
	}
	if recs == nil {
		recs = []*store.FactorEvaluationRecord{}
	}
	c.JSON(http.StatusOK, gin.H{"evaluations": recs})
}
