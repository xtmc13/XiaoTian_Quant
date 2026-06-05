package handler

import (
	"math"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
)

var advancedOrderMgr = order.NewAdvancedManager()

func init() {
	// Wire advanced manager to base order manager
	om := order.GetOrderManager()
	advancedOrderMgr.PlaceOrder = func(req *order.Request) (*model.OrderData, error) {
		return om.PlaceOrder(req)
	}
	advancedOrderMgr.CancelOrder = func(orderID string) error {
		_, err := om.CancelOrder(orderID, "")
		return err
	}
	advancedOrderMgr.GetOrder = func(orderID string) *model.OrderData {
		return om.GetOrder(orderID)
	}
}

// ── OCO Orders ─────────────────────────────────────────────────

// PlaceOCO creates an OCO order.
func PlaceOCO(c *gin.Context) {
	var body struct {
		Symbol     string  `json:"symbol"`
		Side       string  `json:"side"`
		Quantity   float64 `json:"quantity"`
		LimitPrice float64 `json:"limit_price"`  // take-profit
		StopPrice  float64 `json:"stop_price"`   // stop trigger
		StopLimit  float64 `json:"stop_limit"`   // optional stop limit
		ParentID   string  `json:"parent_id,omitempty"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	oco := &order.OCOOrder{
		Symbol:     body.Symbol,
		Side:       model.OrderSide(body.Side),
		Quantity:   body.Quantity,
		LimitPrice: body.LimitPrice,
		StopPrice:  body.StopPrice,
		StopLimit:  body.StopLimit,
		ParentID:   body.ParentID,
	}

	result, err := advancedOrderMgr.PlaceOCO(oco)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":         "ok",
		"oco_id":         result.ID,
		"limit_order_id": result.LimitOrderID,
		"stop_order_id":  result.StopOrderID,
	})
}

// CancelOCO cancels an OCO order.
func CancelOCO(c *gin.Context) {
	id := c.Param("id")
	if err := advancedOrderMgr.CancelOCO(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

// GetOCO returns an OCO order.
func GetOCO(c *gin.Context) {
	id := c.Param("id")
	oco := advancedOrderMgr.GetOCO(id)
	if oco == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "OCO not found"})
		return
	}
	c.JSON(http.StatusOK, oco)
}

// ListOCO returns active OCO orders.
func ListOCO(c *gin.Context) {
	orders := advancedOrderMgr.ListActiveOCO()
	if orders == nil {
		orders = []*order.OCOOrder{}
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders, "count": len(orders)})
}

// ── Bracket Orders ─────────────────────────────────────────────

// PlaceBracket creates a bracket order.
func PlaceBracket(c *gin.Context) {
	var body struct {
		Symbol     string  `json:"symbol"`
		Side       string  `json:"side"`
		Quantity   float64 `json:"quantity"`
		EntryPrice float64 `json:"entry_price"`
		TakeProfit float64 `json:"take_profit"`
		StopLoss   float64 `json:"stop_loss"`
		EntryType  string  `json:"entry_type"` // MARKET or LIMIT
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	entryType := model.TypeLimit
	if body.EntryType == "MARKET" {
		entryType = model.TypeMarket
	}

	bracket := &order.BracketOrder{
		Symbol:     body.Symbol,
		Side:       model.OrderSide(body.Side),
		Quantity:   body.Quantity,
		EntryPrice: body.EntryPrice,
		TakeProfit: body.TakeProfit,
		StopLoss:   body.StopLoss,
		EntryType:  entryType,
	}

	result, err := advancedOrderMgr.PlaceBracket(bracket)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":         "ok",
		"bracket_id":     result.ID,
		"entry_order_id": result.EntryOrderID,
	})
}

// CancelBracket cancels a bracket order.
func CancelBracket(c *gin.Context) {
	id := c.Param("id")
	if err := advancedOrderMgr.CancelBracket(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

// GetBracket returns a bracket order.
func GetBracket(c *gin.Context) {
	id := c.Param("id")
	bracket := advancedOrderMgr.GetBracket(id)
	if bracket == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bracket not found"})
		return
	}
	c.JSON(http.StatusOK, bracket)
}

// ListBracket returns active bracket orders.
func ListBracket(c *gin.Context) {
	orders := advancedOrderMgr.ListActiveBracket()
	if orders == nil {
		orders = []*order.BracketOrder{}
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders, "count": len(orders)})
}

// ── Iceberg Orders ─────────────────────────────────────────────

// PlaceIceberg creates an iceberg order.
func PlaceIceberg(c *gin.Context) {
	var body struct {
		Symbol          string  `json:"symbol"`
		Side            string  `json:"side"`
		TotalQuantity   float64 `json:"total_quantity"`
		VisibleQuantity float64 `json:"visible_quantity"`
		Price           float64 `json:"price"`
		Variance        float64 `json:"variance"` // 0-1
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	iceberg := &order.IcebergOrder{
		Symbol:          body.Symbol,
		Side:            model.OrderSide(body.Side),
		TotalQuantity:   body.TotalQuantity,
		VisibleQuantity: body.VisibleQuantity,
		Price:           body.Price,
		Variance:        body.Variance,
	}

	result, err := advancedOrderMgr.PlaceIceberg(iceberg)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"iceberg_id": result.ID,
	})
}

// CancelIceberg cancels an iceberg order.
func CancelIceberg(c *gin.Context) {
	id := c.Param("id")
	if err := advancedOrderMgr.CancelIceberg(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

// GetIceberg returns an iceberg order.
func GetIceberg(c *gin.Context) {
	id := c.Param("id")
	iceberg := advancedOrderMgr.GetIceberg(id)
	if iceberg == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Iceberg not found"})
		return
	}
	c.JSON(http.StatusOK, iceberg)
}

// ListIceberg returns active iceberg orders.
func ListIceberg(c *gin.Context) {
	orders := advancedOrderMgr.ListActiveIceberg()
	if orders == nil {
		orders = []*order.IcebergOrder{}
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders, "count": len(orders)})
}

// ── Utility ────────────────────────────────────────────────────

// CalculateBracket computes TP/SL prices and position size.
func CalculateBracket(c *gin.Context) {
	var body struct {
		EntryPrice      float64 `json:"entry_price"`
		Side            string  `json:"side"`
		StopLossPct     float64 `json:"stop_loss_pct"`
		TakeProfitPct   float64 `json:"take_profit_pct"`
		Balance         float64 `json:"balance"`
		RiskPct         float64 `json:"risk_pct"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tp, sl := order.CalculateBracketPrices(
		body.EntryPrice,
		model.OrderSide(body.Side),
		body.StopLossPct,
		body.TakeProfitPct,
	)

	qty := order.CalculatePositionSize(body.Balance, body.RiskPct, body.EntryPrice, sl)

	c.JSON(http.StatusOK, gin.H{
		"entry_price":     body.EntryPrice,
		"take_profit":     tp,
		"stop_loss":       sl,
		"position_size":   qty,
		"risk_amount":     body.Balance * body.RiskPct,
		"reward_amount":   qty * math.Abs(tp-body.EntryPrice),
		"risk_reward":     body.TakeProfitPct / body.StopLossPct,
	})
}
