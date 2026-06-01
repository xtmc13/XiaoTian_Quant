package handler

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/store"
)

func GetOrders(c *gin.Context) {
	symbol := c.Query("symbol")
	allOrders := store.GetOrders(symbol)
	var active []map[string]any
	for _, o := range allOrders {
		status := o["status"].(string)
		if status != "CANCELLED" && status != "FILLED" && status != "REJECTED" {
			active = append(active, o)
		}
	}
	if active == nil {
		active = []map[string]any{}
	}
	c.JSON(http.StatusOK, active)
}

func PlaceOrder(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	order := map[string]any{
		"symbol":     getString(body, "symbol", "BTCUSDT"),
		"side":       getString(body, "side", "BUY"),
		"order_type": getString(body, "order_type", "LIMIT"),
		"price":      getFloat(body, "price", 0),
		"quantity":   getFloat(body, "quantity", 0),
		"exchange":   getString(body, "exchange", "BINANCE"),
	}
	id := store.PlaceOrder(order)

	// ── Auto-fill MARKET orders for paper trading ──
	if strings.EqualFold(order["order_type"].(string), "MARKET") {
		fillOrderAndUpdatePortfolio(order)
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "order_id": id})
}

// fillOrderAndUpdatePortfolio simulates immediate execution for market orders
// and updates the portfolio manager balances and positions.
func fillOrderAndUpdatePortfolio(order map[string]any) {
	symbol := getString(order, "symbol", "BTCUSDT")
	side := strings.ToUpper(getString(order, "side", "BUY"))
	price := getFloat(order, "price", 0)
	qty := getFloat(order, "quantity", 0)
	if price <= 0 || qty <= 0 {
		return
	}

	base, quote := parseSymbolPair(symbol)
	cost := price * qty

	mgr := portfolio.GetManager()
	if mgr == nil {
		return
	}

	// Mark order as filled
	order["status"] = "FILLED"
	order["filled"] = qty

	// Get account for PnL calculation
	acct := mgr.GetAccount("default")
	if acct == nil {
		return
	}

	// Calculate realized PnL: if this trade closes or reduces an opposite position
	var realizedPnL float64
	oppositeSide := "SELL"
	if side == "SELL" {
		oppositeSide = "BUY"
	}
	oppositePos := acct.Positions[symbol+"-"+oppositeSide]
	if oppositePos != nil && oppositePos.Quantity > 0 {
		closeQty := qty
		if closeQty > oppositePos.Quantity {
			closeQty = oppositePos.Quantity
		}
		if side == "SELL" {
			realizedPnL = (price - oppositePos.AvgEntryPrice) * closeQty
		} else {
			realizedPnL = (oppositePos.AvgEntryPrice - price) * closeQty
		}
	}
	order["realized_pnl"] = realizedPnL

	// Update balances
	quoteBal := acct.Balances[quote]
	if quoteBal == nil {
		quoteBal = &model.Balance{Currency: quote, Total: 0, Free: 0, Used: 0}
	}
	baseBal := acct.Balances[base]
	if baseBal == nil {
		baseBal = &model.Balance{Currency: base, Total: 0, Free: 0, Used: 0}
	}

	if side == "BUY" {
		// Deduct quote, add base
		quoteBal.Free -= cost
		quoteBal.Total -= cost
		if quoteBal.Free < 0 {
			quoteBal.Free = 0
		}
		if quoteBal.Total < 0 {
			quoteBal.Total = 0
		}
		baseBal.Free += qty
		baseBal.Total += qty
	} else {
		// Deduct base, add quote
		baseBal.Free -= qty
		baseBal.Total -= qty
		if baseBal.Free < 0 {
			baseBal.Free = 0
		}
		if baseBal.Total < 0 {
			baseBal.Total = 0
		}
		quoteBal.Free += cost
		quoteBal.Total += cost
	}

	mgr.UpdateBalance("default", quote, quoteBal.Total, quoteBal.Free, quoteBal.Used)
	mgr.UpdateBalance("default", base, baseBal.Total, baseBal.Free, baseBal.Used)

	// Update position
	posID := symbol + "-" + side
	existing := acct.Positions[posID]
	if existing != nil {
		// Average down/up
		totalQty := existing.Quantity + qty
		totalCost := existing.AvgEntryPrice*existing.Quantity + price*qty
		avgPrice := totalCost / totalQty
		existing.Quantity = totalQty
		existing.AvgEntryPrice = avgPrice
		existing.CurrentPrice = price
		existing.UnrealizedPnL = (existing.CurrentPrice - existing.AvgEntryPrice) * existing.Quantity
		if side == "SELL" {
			existing.UnrealizedPnL = (existing.AvgEntryPrice - existing.CurrentPrice) * existing.Quantity
		}
		mgr.UpdatePosition(*existing)
	} else {
		newPos := model.PositionData{
			ID:            posID,
			Symbol:        symbol,
			Side:          side,
			Quantity:      qty,
			AvgEntryPrice: price,
			CurrentPrice:  price,
			UnrealizedPnL: 0,
			OpenedAt:      time.Now().UnixMilli(),
		}
		mgr.UpdatePosition(newPos)
	}

	// Record snapshot after trade
	mgr.Snapshot()
}

// parseSymbolPair extracts base and quote assets from a symbol like "BTCUSDT".
func parseSymbolPair(symbol string) (base, quote string) {
	symbol = strings.ToUpper(symbol)
	for _, q := range []string{"USDT", "USDC", "USD", "BTC", "ETH", "BNB", "SOL"} {
		if strings.HasSuffix(symbol, q) && len(symbol) > len(q) {
			return strings.TrimSuffix(symbol, q), q
		}
	}
	return symbol, "USDT"
}

func CancelOrder(c *gin.Context) {
	orderID := c.Param("order_id")
	if err := store.CancelOrder(orderID); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func OrderHistory(c *gin.Context) {
	symbol := c.Query("symbol")
	limit := 50
	if l := c.Query("limit"); l != "" {
		fmtScan(l, &limit)
	}

	allOrders := store.GetOrders("")
	var history []map[string]any
	for _, o := range allOrders {
		status := o["status"].(string)
		if status == "CANCELLED" || status == "FILLED" || status == "REJECTED" {
			if symbol == "" || o["symbol"] == symbol {
				history = append(history, o)
			}
		}
	}

	sort.Slice(history, func(i, j int) bool {
		a := getFloat(history[i], "created_at", 0)
		b := getFloat(history[j], "created_at", 0)
		return a > b
	})

	if len(history) > limit {
		history = history[:limit]
	}
	if history == nil {
		history = []map[string]any{}
	}
	c.JSON(http.StatusOK, history)
}

func CancelAllOrders(c *gin.Context) {
	allOrders := store.GetOrders("")
	for _, o := range allOrders {
		status := o["status"].(string)
		if status != "CANCELLED" && status != "FILLED" && status != "REJECTED" {
			id := o["id"].(string)
			store.CancelOrder(id)
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// GetAccountBalance returns mock account balances.
func GetAccountBalance(c *gin.Context) {
	symbol := c.DefaultQuery("symbol", "BTCUSDT")
	base := strings.Replace(symbol, "USDT", "", 1)
	balances := []map[string]any{
		{"asset": "USDT", "currency": "USDT", "free": 50000.0, "available": 50000.0, "total": 52000.0},
		{"asset": base, "currency": base, "free": 0.5, "available": 0.5, "total": 0.55},
		{"asset": "ETH", "currency": "ETH", "free": 3.2, "available": 3.2, "total": 3.5},
		{"asset": "SOL", "currency": "SOL", "free": 25.0, "available": 25.0, "total": 28.0},
	}
	c.JSON(http.StatusOK, gin.H{"balances": balances, "currencies": balances})
}

// GetTradeHistory returns mock executed trade history.
func GetTradeHistory(c *gin.Context) {
	limit := 50
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}
	now := time.Now()
	trades := make([]map[string]any, limit)
	for i := 0; i < limit; i++ {
		side := "BUY"
		if i%2 == 0 {
			side = "SELL"
		}
		trades[i] = map[string]any{
			"symbol":    "BTCUSDT",
			"side":      side,
			"price":     67200.0 + float64(i)*10.0,
			"qty":       0.01,
			"quantity":  0.01,
			"pnl":       50.0 - float64(i%5)*25.0,
			"time":      now.Add(-time.Duration(i*30) * time.Minute).UnixMilli(),
			"timestamp": now.Add(-time.Duration(i*30) * time.Minute).UnixMilli(),
		}
	}
	c.JSON(http.StatusOK, gin.H{"trades": trades})
}

func fmtScan(s string, v *int) {
	if i, err := strconv.Atoi(s); err == nil {
		*v = i
	}
}
