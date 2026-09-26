package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/xiaotian-quant/gateway/internal/config"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/metrics"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// verifyWebhookSignature checks HMAC-SHA256 signature from X-Webhook-Signature header.
// If WEBHOOK_SECRET is not set, authentication is skipped (development mode).
func verifyWebhookSignature(c *gin.Context) bool {
	secret := os.Getenv("WEBHOOK_SECRET")
	if secret == "" {
		return true // no secret configured — allow (dev mode)
	}
	signature := c.GetHeader("X-Webhook-Signature")
	if signature == "" {
		return false
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return false
	}
	// Restore body for re-reading
	c.Request.Body = io.NopCloser(strings.NewReader(string(body)))

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expected))
}

var webhookSecretWarnOnce sync.Once

// WarnIfWebhookSecretMissing 在未配置 WEBHOOK_SECRET 时打印一次启动警告（M8）：
// webhook 下单通道将对互联网上所有人开放，生产环境必须配置。
func WarnIfWebhookSecretMissing() {
	if os.Getenv("WEBHOOK_SECRET") == "" {
		webhookSecretWarnOnce.Do(func() {
			log.Printf("[WARN] WEBHOOK_SECRET 未配置：/api/webhook/* 下单通道无签名校验，任何人均可触发下单。生产环境请务必配置 WEBHOOK_SECRET。")
		})
	}
}

// TradingViewWebhook receives alerts from TradingView Pine Script strategies.
//
// TradingView setup:
//
//	Alert → Webhook URL: http://your-server:8080/api/webhook/tv
//	Message format (JSON):
//	  {"symbol":"BTCUSDT","action":"buy","price":"50000","quantity":"0.1","strategy":"TV_Strategy"}
//
// The webhook automatically places paper/real orders through the order manager.
func TradingViewWebhook(c *gin.Context) {
	if !verifyWebhookSignature(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing signature"})
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}

	symbol := getString(body, "symbol", getString(body, "ticker", ""))
	action := strings.ToLower(getString(body, "action", getString(body, "side", "")))
	quantity := resolveWebhookQuantity(body)
	if quantity < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity required"})
		return
	}

	if symbol == "" || action == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol and action required"})
		return
	}

	// Map TradingView action to order side
	var side string
	switch action {
	case "buy", "long", "enter_long":
		side = "BUY"
	case "sell", "short", "enter_short":
		side = "SELL"
	case "exit", "close", "exit_long", "exit_short", "flatten":
		// 出场单保持直发（AI 不能拦出场、风控不拦平仓，与 OMS 门内口径一致），
		// 但记审计留痕——收口后不再是无痕通道。
		closeAllOrdersForSymbol(symbol)
		closeOrder := map[string]any{
			"symbol":   strings.ToUpper(symbol),
			"side":     "SELL",
			"type":     "MARKET",
			"price":    0,
			"quantity": quantity,
			"source":   "tradingview",
			"strategy": getString(body, "strategy", "TV_Strategy"),
			"action":   "exit",
		}
		fillOrderAndUpdatePortfolio(closeOrder)
		store.AddAuditLog("webhook", "tv_exit_direct",
			fmt.Sprintf("symbol=%s quantity=%.8f strategy=%s（出场直发，不过风控/AI决策门）",
				strings.ToUpper(symbol), quantity, getString(body, "strategy", "TV_Strategy")))
		log.Printf("[webhook] TV signal: CLOSE %s (orders cancelled + exit submitted)", symbol)
		c.JSON(http.StatusOK, gin.H{"status": "ok", "action": "close", "symbol": symbol, "msg": "close signal processed"})
		return
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown action: %s", action)})
		return
	}

	// Get price from alert or use 0 for market order
	price := getFloat(body, "price", getFloat(body, "limit", 0))
	orderType := model.OrderType("MARKET")
	if price > 0 {
		orderType = model.TypeLimit
	}

	// 入场单改道 OMS 全管线（安全收口）：风控 15 维 → AI 决策门 → 余额锁 → 下单，
	// 与界面/策略下单同一口径，不再直发绕过。
	req := &order.Request{
		Symbol:    strings.ToUpper(symbol),
		Side:      model.OrderSide(side),
		OrderType: orderType,
		Price:     price,
		Quantity:  quantity,
		Exchange:  strings.ToLower(getString(body, "exchange", "paper")),
		Source:    "webhook",
		ClientOID: "webhook:tv",
		// 合约字段转发（旧直发路径支持 market_type=swap，保持能力平价）
		MarketType:   model.MarketType(getString(body, "market_type", "spot")),
		PositionSide: model.PositionSide(getString(body, "position_side", "")),
		Leverage:     getFloat(body, "leverage", 0),
		MarginMode:   model.MarginMode(getString(body, "margin_mode", "cross")),
	}
	ord, ok := placeWebhookEntryViaOMS(c, req, getBool(body, "confirmed", false))
	if !ok {
		return
	}

	log.Printf("[webhook] TV signal: %s %s %s qty=%.4f price=%.2f → OMS %s (%s)",
		getString(body, "strategy", "TV_Strategy"), side, symbol, quantity, price, ord.ID, ord.Status)

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"order":  normalizeOrder(omsOrderStoreMap(ord, "webhook")),
	})
}

// placeWebhookEntryViaOMS 把 webhook 入场单送进 OMS 管线（risk/manager 15 维检查
// + aigate 决策门 + 余额锁 + 交易所/paper 撮合）。Source="webhook"：决策门默认
// 豁免列表（grid/dca/lmartin）不含 webhook，门对该源生效。
// 被拒/失败时写好错误响应并返回 ok=false；成功时落展示层 store 并返回订单。
func placeWebhookEntryViaOMS(c *gin.Context, req *order.Request, confirmed bool) (*model.OrderData, bool) {
	if err := canPlaceLiveOrder(req.Exchange, confirmed); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"status": "error", "detail": err.Error()})
		return nil, false
	}

	ord, err := order.GetOrderManager().PlaceOrder(req)
	if err != nil {
		// 风控/决策门拒绝的订单在 OMS 里以 REJECTED 落库（审计口径），但不成交、
		// 不进撮合/交易所——webhook 返回错误，调用方必须感知被拒。
		resp := gin.H{"status": "error", "detail": err.Error()}
		if ord != nil {
			resp["order_id"] = ord.ID
		}
		c.JSON(http.StatusBadRequest, resp)
		return ord, false
	}

	storeOrder := omsOrderStoreMap(ord, req.Source)
	store.PlaceOrder(storeOrder)
	metrics.RecordOrder(string(ord.Side), string(ord.Status))
	if ord.Filled > 0 {
		metrics.RecordFill(string(ord.Side), ord.Filled)
	}
	return ord, true
}

// omsOrderStoreMap 把 OMS 订单转成展示层 store map（与 PlaceOrder handler 同口径），
// 使 webhook 单出现在订单列表/历史里。
func omsOrderStoreMap(ord *model.OrderData, source string) map[string]any {
	return map[string]any{
		"id":             ord.ID,
		"order_id":       ord.ID,
		"symbol":         ord.Symbol,
		"side":           string(ord.Side),
		"order_type":     string(ord.OrderType),
		"price":          ord.Price,
		"quantity":       ord.Quantity,
		"filled":         ord.Filled,
		"status":         string(ord.Status),
		"exchange":       ord.Exchange,
		"user_id":        ord.UserID,
		"client_oid":     ord.ClientOID,
		"avg_fill_price": ord.AvgFillPrice,
		"created_at":     ord.CreatedAt,
		"updated_at":     ord.UpdatedAt,
		"market_type":    string(ord.MarketType),
		"position_side":  string(ord.PositionSide),
		"leverage":       ord.Leverage,
		"margin_mode":    string(ord.MarginMode),
		"tp_price":       ord.TPPrice,
		"sl_price":       ord.SLPrice,
		"close_position": ord.ClosePosition,
		"source":         source,
	}
}

// GenericWebhook receives signals from any external source (3Commas, custom bots, etc).
//
// POST /api/webhook/generic
// Body: {"symbol":"BTCUSDT","side":"BUY","type":"MARKET","quantity":0.1,"price":0}
// Requires X-Webhook-Signature header with HMAC-SHA256 of the body (set WEBHOOK_SECRET env var).
func GenericWebhook(c *gin.Context) {
	if !verifyWebhookSignature(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing signature"})
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}

	symbol := strings.ToUpper(getString(body, "symbol", ""))
	side := strings.ToUpper(getString(body, "side", "BUY"))
	orderType := strings.ToUpper(getString(body, "type", "MARKET"))
	price := getFloat(body, "price", 0)
	quantity := resolveWebhookQuantity(body)
	if quantity < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity required"})
		return
	}

	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol required"})
		return
	}

	// 显式出场标记（action=exit/close/flatten 或 close_position=true）保持直发 + 审计；
	// 其余一律按入场口径改道 OMS（风控 + AI 决策门）。
	action := strings.ToLower(getString(body, "action", ""))
	isExit := action == "exit" || action == "close" || action == "flatten" ||
		action == "exit_long" || action == "exit_short" || getBool(body, "close_position", false)
	if isExit {
		exitOrder := map[string]any{
			"symbol":         symbol,
			"side":           side,
			"type":           orderType,
			"price":          price,
			"quantity":       quantity,
			"source":         getString(body, "source", "webhook"),
			"action":         "exit",
			"market_type":    getString(body, "market_type", "spot"),
			"position_side":  getString(body, "position_side", ""),
			"leverage":       getFloat(body, "leverage", 1),
			"margin_mode":    getString(body, "margin_mode", "cross"),
			"close_position": true,
		}
		fillOrderAndUpdatePortfolio(exitOrder)
		store.AddAuditLog("webhook", "generic_exit_direct",
			fmt.Sprintf("symbol=%s side=%s quantity=%.8f（出场直发，不过风控/AI决策门）", symbol, side, quantity))
		log.Printf("[webhook] generic exit: %s %s %s qty=%.4f (direct + audited)", side, orderType, symbol, quantity)
		c.JSON(http.StatusOK, gin.H{"status": "ok", "action": "close", "order": exitOrder})
		return
	}

	req := &order.Request{
		Symbol:    symbol,
		Side:      model.OrderSide(side),
		OrderType: model.OrderType(orderType),
		Price:     price,
		Quantity:  quantity,
		Exchange:  strings.ToLower(getString(body, "exchange", "paper")),
		Source:    "webhook",
		ClientOID: "webhook:generic",
		// 合约字段转发（与旧直发路径能力平价）
		MarketType:   model.MarketType(getString(body, "market_type", "spot")),
		PositionSide: model.PositionSide(getString(body, "position_side", "")),
		Leverage:     getFloat(body, "leverage", 0),
		MarginMode:   model.MarginMode(getString(body, "margin_mode", "cross")),
	}
	ord, ok := placeWebhookEntryViaOMS(c, req, getBool(body, "confirmed", false))
	if !ok {
		return
	}

	log.Printf("[webhook] generic: %s %s %s qty=%.4f → OMS %s (%s)", side, orderType, symbol, quantity, ord.ID, ord.Status)

	c.JSON(http.StatusOK, gin.H{"status": "ok", "order": normalizeOrder(omsOrderStoreMap(ord, "webhook"))})
}

// closeAllOrdersForSymbol cancels all open orders for a given symbol.
func closeAllOrdersForSymbol(symbol string) {
	symbol = strings.ToUpper(symbol)
	allOrders := store.GetOrders("")
	for _, o := range allOrders {
		if s, ok := o["symbol"].(string); ok && strings.ToUpper(s) == symbol {
			status, _ := o["status"].(string)
			if status != "CANCELLED" && status != "FILLED" && status != "REJECTED" {
				id, _ := o["id"].(string)
				store.CancelOrder(id)
				log.Printf("[webhook] Cancelled order %s for %s", id, symbol)
			}
		}
	}
}

// resolveWebhookQuantity 解析 webhook 请求的数量：显式 quantity/qty 优先；
// 缺失或 <=0 时读 config.yaml webhook.default_quantity（默认 0=必须显式传），
// 无有效值返回 -1（调用方 400）。不再硬编码 0.01 默认（P0-5）。
func resolveWebhookQuantity(body map[string]any) float64 {
	q := getFloat(body, "quantity", getFloat(body, "qty", 0))
	if q > 0 {
		return q
	}
	if def := config.Get().Webhook.DefaultQuantity; def > 0 {
		return def
	}
	return -1
}
