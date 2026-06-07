package handler

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/adapter"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/store"
)

func GetOrders(c *gin.Context) {
	symbol := c.Query("symbol")
	allOrders := store.GetOrders(symbol)
	var active []map[string]any
	for _, o := range allOrders {
		status := getString(o, "status", "")
		if status != "CANCELLED" && status != "FILLED" && status != "REJECTED" {
			active = append(active, normalizeOrder(o))
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

	req := &order.Request{
		Symbol:        getString(body, "symbol", "BTCUSDT"),
		Side:          model.OrderSide(getString(body, "side", "BUY")),
		OrderType:     model.OrderType(getString(body, "order_type", "LIMIT")),
		Price:         getFloat(body, "price", 0),
		Quantity:      getFloat(body, "quantity", 0),
		Exchange:      getString(body, "exchange", "paper"),

		// ── Contract fields ──
		MarketType:    model.MarketType(getString(body, "market_type", "spot")),
		PositionSide:  model.PositionSide(getString(body, "position_side", "")),
		Leverage:      getFloat(body, "leverage", 0),
		MarginMode:    model.MarginMode(getString(body, "margin_mode", "cross")),
		TPPrice:       getFloat(body, "tp_price", 0),
		SLPrice:       getFloat(body, "sl_price", 0),
		ClosePosition: getBool(body, "close_position", false),
	}

	ord, err := order.GetOrderManager().PlaceOrder(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "detail": err.Error()})
		return
	}

	// Store in the shared store so GetOrders / OrderHistory can find it
	storeOrder := map[string]any{
		"symbol":         ord.Symbol,
		"side":           string(ord.Side),
		"order_type":     string(ord.OrderType),
		"price":          ord.Price,
		"quantity":       ord.Quantity,
		"filled":         ord.Filled,
		"status":         string(ord.Status),
		"exchange":       ord.Exchange,
		"market_type":    string(ord.MarketType),
		"position_side":  string(ord.PositionSide),
		"leverage":       ord.Leverage,
		"margin_mode":    string(ord.MarginMode),
		"tp_price":       ord.TPPrice,
		"sl_price":       ord.SLPrice,
		"close_position": ord.ClosePosition,
	}
	store.PlaceOrder(storeOrder)

	c.JSON(http.StatusOK, normalizeOrder(storeOrder))
}

// normalizeOrder converts a raw store order map into the frontend-expected format.
// It maps backend field names (order_type → type, filled → filled_quantity)
// and converts Unix timestamps to ISO-8601 strings.
func normalizeOrder(o map[string]any) map[string]any {
	result := make(map[string]any)

	// Basic fields
	result["id"] = getString(o, "order_id", getString(o, "id", ""))
	result["symbol"] = getString(o, "symbol", "")
	result["side"] = strings.ToUpper(getString(o, "side", ""))

	// Type mapping: backend uses "order_type", frontend uses "type"
	if ot, ok := o["order_type"].(string); ok && ot != "" {
		result["type"] = ot
	} else if t, ok := o["type"].(string); ok {
		result["type"] = t
	} else {
		result["type"] = "LIMIT"
	}

	result["price"] = getFloat(o, "price", 0)
	result["quantity"] = getFloat(o, "quantity", 0)

	// Filled quantity mapping
	if filled, ok := o["filled"].(float64); ok {
		result["filled_quantity"] = filled
	} else {
		result["filled_quantity"] = 0
	}

	result["status"] = getString(o, "status", "NEW")

	// Time conversion: backend stores float64 Unix timestamp, frontend expects ISO string
	if created, ok := o["created_at"].(float64); ok && created > 0 {
		result["created_at"] = time.Unix(int64(created), 0).Format(time.RFC3339)
	} else if createdStr, ok := o["created_at"].(string); ok {
		result["created_at"] = createdStr
	} else {
		result["created_at"] = time.Now().Format(time.RFC3339)
	}

	// Contract fields
	result["market_type"] = getString(o, "market_type", "spot")
	result["position_side"] = getString(o, "position_side", "")
	result["leverage"] = getFloat(o, "leverage", 0)
	result["margin_mode"] = getString(o, "margin_mode", "cross")
	result["tp_price"] = getFloat(o, "tp_price", 0)
	result["sl_price"] = getFloat(o, "sl_price", 0)
	result["close_position"] = getBool(o, "close_position", false)

	return result
}

// fillOrderAndUpdatePortfolio simulates immediate execution for market orders
// and updates the portfolio manager balances and positions.
// Supports both spot and contract (swap) trading.
// NOTE: This is called from webhooks — basic sanity checks are applied.
func fillOrderAndUpdatePortfolio(order map[string]any) {
	symbol := getString(order, "symbol", "BTCUSDT")
	side := strings.ToUpper(getString(order, "side", "BUY"))
	price := getFloat(order, "price", 0)
	qty := getFloat(order, "quantity", 0)
	if price <= 0 || qty <= 0 {
		return
	}

	// Basic sanity checks (bypass OrderManager risk checks — webhook path)
	if qty > 1000000 {
		log.Printf("[risk] Webhook order quantity too large: %.4f %s", qty, symbol)
		return
	}
	if price > 10000000 || price < 1e-8 {
		log.Printf("[risk] Webhook order price suspicious: %.8f %s", price, symbol)
		return
	}

	marketType := model.MarketType(getString(order, "market_type", "spot"))
	leverage := getFloat(order, "leverage", 1)
	if leverage <= 0 {
		leverage = 1
	}
	marginMode := model.MarginMode(getString(order, "margin_mode", "cross"))
	positionSide := model.PositionSide(getString(order, "position_side", ""))
	closePosition := getBool(order, "close_position", false)

	base, quote := parseSymbolPair(symbol)
	cost := price * qty

	mgr := portfolio.GetManager()
	if mgr == nil {
		return
	}

	// Mark order as filled
	order["status"] = "FILLED"
	order["filled"] = qty

	// Get account
	acct := mgr.GetAccount("default")
	if acct == nil {
		return
	}

	if marketType == model.MarketSwap {
		// ── CONTRACT (SWAP) LOGIC ──
		margin := cost / leverage

		// Determine position side if not explicitly set
		if positionSide == "" {
			if closePosition {
				// Closing: infer from existing position
				if side == "SELL" {
					positionSide = model.PositionLong // SELL closes LONG
				} else {
					positionSide = model.PositionShort // BUY closes SHORT
				}
			} else {
				// Opening
				if side == "BUY" {
					positionSide = model.PositionLong
				} else {
					positionSide = model.PositionShort
				}
			}
		}

		posID := symbol + "-" + string(positionSide)
		existing := acct.Positions[posID]

		if closePosition || (existing != nil && existing.Quantity > 0) {
			// ── CLOSE / REDUCE position ──
			if existing == nil || existing.Quantity <= 0 {
				return // Nothing to close
			}
			closeQty := qty
			if closeQty > existing.Quantity {
				closeQty = existing.Quantity
			}

			// Realized PnL
			var realizedPnL float64
			if positionSide == model.PositionLong {
				realizedPnL = (price - existing.AvgEntryPrice) * closeQty
			} else {
				realizedPnL = (existing.AvgEntryPrice - price) * closeQty
			}
			order["realized_pnl"] = realizedPnL

			// Release margin proportionally
			releasedMargin := margin
			if closeQty < existing.Quantity {
				releasedMargin = (existing.Margin * closeQty) / existing.Quantity
			}

			// Update balance (release margin + realized PnL)
			quoteBal := acct.Balances[quote]
			if quoteBal == nil {
				quoteBal = &model.Balance{Currency: quote, Total: 0, Free: 0, Used: 0}
			}
			quoteBal.Free += releasedMargin + realizedPnL
			quoteBal.Total += releasedMargin + realizedPnL
			if quoteBal.Free < 0 {
				quoteBal.Free = 0
			}
			if quoteBal.Total < 0 {
				quoteBal.Total = 0
			}
			mgr.UpdateBalance("default", quote, quoteBal.Total, quoteBal.Free, quoteBal.Used)

			// Update position
			existing.Quantity -= closeQty
			existing.RealizedPnL += realizedPnL
			existing.Margin -= releasedMargin
			if existing.Quantity <= 0 {
				existing.Quantity = 0
				existing.Margin = 0
				existing.UnrealizedPnL = 0
			} else {
				existing.CurrentPrice = price
				if positionSide == model.PositionLong {
					existing.UnrealizedPnL = (existing.CurrentPrice - existing.AvgEntryPrice) * existing.Quantity
				} else {
					existing.UnrealizedPnL = (existing.AvgEntryPrice - existing.CurrentPrice) * existing.Quantity
				}
			}
			mgr.UpdatePosition(*existing)
		} else {
			// ── OPEN / INCREASE position ──
			quoteBal := acct.Balances[quote]
			if quoteBal == nil {
				quoteBal = &model.Balance{Currency: quote, Total: 0, Free: 0, Used: 0}
			}
			// Deduct margin
			quoteBal.Free -= margin
			quoteBal.Used += margin
			if quoteBal.Free < 0 {
				quoteBal.Free = 0
			}
			mgr.UpdateBalance("default", quote, quoteBal.Total, quoteBal.Free, quoteBal.Used)

			if existing != nil && existing.Quantity > 0 {
				// Average down/up
				totalQty := existing.Quantity + qty
				totalCost := existing.AvgEntryPrice*existing.Quantity + price*qty
				avgPrice := totalCost / totalQty
				existing.Quantity = totalQty
				existing.AvgEntryPrice = avgPrice
				existing.CurrentPrice = price
				existing.Margin += margin
				existing.Leverage = leverage
				existing.MarginMode = marginMode
				if positionSide == model.PositionLong {
					existing.UnrealizedPnL = (existing.CurrentPrice - existing.AvgEntryPrice) * existing.Quantity
				} else {
					existing.UnrealizedPnL = (existing.AvgEntryPrice - existing.CurrentPrice) * existing.Quantity
				}
				mgr.UpdatePosition(*existing)
			} else {
				// New position
				newPos := model.PositionData{
					ID:               posID,
					Symbol:           symbol,
					Side:             string(side),
					Quantity:         qty,
					AvgEntryPrice:    price,
					CurrentPrice:     price,
					UnrealizedPnL:    0,
					RealizedPnL:      0,
					OpenedAt:         time.Now().UnixMilli(),
					PositionSide:     positionSide,
					Leverage:         leverage,
					MarginMode:       marginMode,
					Margin:           margin,
					MarketType:       marketType,
					LiquidationPrice: calcLiquidationPrice(price, leverage, positionSide),
				}
				mgr.UpdatePosition(newPos)
			}
		}
	} else {
		// ── SPOT LOGIC (original) ──
		// Calculate realized PnL: if this trade closes or reduces an opposite position
		var realizedPnL float64
		var closedQty float64
		oppositeSide := "SELL"
		if side == "SELL" {
			oppositeSide = "BUY"
		}
		oppositePos := acct.Positions[symbol+"-"+oppositeSide]
		if oppositePos != nil && oppositePos.Quantity > 0 {
			closedQty = qty
			if closedQty > oppositePos.Quantity {
				closedQty = oppositePos.Quantity
			}
			if side == "SELL" {
				// SELL 卖出平仓 LONG 持仓
				// 利润 = (卖出价 - 成本价) * 平仓数量
				realizedPnL = (price - oppositePos.AvgEntryPrice) * closedQty
			} else {
				// BUY 买入平仓 SHORT 持仓
				// 利润 = (成本价 - 买入价) * 平仓数量
				realizedPnL = (oppositePos.AvgEntryPrice - price) * closedQty
			}

			// 更新反向持仓（减少数量或删除）
			oppositePos.Quantity -= closedQty
			if oppositePos.Quantity <= 0 {
				delete(acct.Positions, symbol+"-"+oppositeSide)
			} else {
				mgr.UpdatePosition(*oppositePos)
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

			// 如果是平仓操作，将 PnL 加到 quote 余额中
			if closedQty > 0 {
				quoteBal.Free += realizedPnL
				quoteBal.Total += realizedPnL
			}
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

			// 如果是平仓操作，将 PnL 加到 quote 余额中
			if closedQty > 0 {
				quoteBal.Free += realizedPnL
				quoteBal.Total += realizedPnL
			}
		}

		mgr.UpdateBalance("default", quote, quoteBal.Total, quoteBal.Free, quoteBal.Used)
		mgr.UpdateBalance("default", base, baseBal.Total, baseBal.Free, baseBal.Used)

		// Update position only if this is NOT a closing trade (or only partially closing)
		remainingQty := qty - closedQty
		if remainingQty > 0 {
			posID := symbol + "-" + side
			existing := acct.Positions[posID]
			if existing != nil {
				// Average down/up
				totalQty := existing.Quantity + remainingQty
				totalCost := existing.AvgEntryPrice*existing.Quantity + price*remainingQty
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
					Quantity:      remainingQty,
					AvgEntryPrice: price,
					CurrentPrice:  price,
					UnrealizedPnL: 0,
					OpenedAt:      time.Now().UnixMilli(),
				}
				mgr.UpdatePosition(newPos)
			}
		}
	}

	// Record snapshot after trade
	mgr.Snapshot()
}

// calcLiquidationPrice estimates the liquidation price for a contract position.
// Simplified: assumes maintenance margin rate of 0.5%.
func calcLiquidationPrice(entryPrice, leverage float64, side model.PositionSide) float64 {
	mmr := 0.005 // maintenance margin rate 0.5%
	if side == model.PositionLong {
		return entryPrice * (1 - 1/leverage + mmr)
	}
	return entryPrice * (1 + 1/leverage - mmr)
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
	var rawHistory []map[string]any
	for _, o := range allOrders {
		status := getString(o, "status", "")
		if status == "CANCELLED" || status == "FILLED" || status == "REJECTED" {
			if symbol == "" || getString(o, "symbol", "") == symbol {
				rawHistory = append(rawHistory, o)
			}
		}
	}

	sort.Slice(rawHistory, func(i, j int) bool {
		a := getFloat(rawHistory[i], "created_at", 0)
		b := getFloat(rawHistory[j], "created_at", 0)
		return a > b
	})

	if len(rawHistory) > limit {
		rawHistory = rawHistory[:limit]
	}

	history := make([]map[string]any, 0, len(rawHistory))
	for _, o := range rawHistory {
		history = append(history, normalizeOrder(o))
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

// GetAccountBalance fetches real account balances from configured exchanges.
func GetAccountBalance(c *gin.Context) {
	apiKey, secret, _ := adapter.GetCredential("binance")
	if apiKey == "" || secret == "" {
		// No credentials configured — return empty balances with clear source
		c.JSON(http.StatusOK, gin.H{
			"balances":        []map[string]any{},
			"currencies":      []map[string]any{},
			"estimated_usdt":  0,
			"source":          "unconfigured",
			"message":         "请在「设置 → 交易所」中配置 Binance API Key 以获取真实余额",
		})
		return
	}

	// Use Binance adapter with real credentials
	binance := adapter.NewBinanceAdapter(apiKey, secret, false)
	rawBalances, err := binance.GetBalance()
	if err != nil {
		log.Printf("[Binance] GetBalance error: %v", err)
		c.JSON(http.StatusOK, gin.H{"error": "Failed to fetch balance from Binance: " + err.Error()})
		return
	}

	balances := make([]map[string]any, 0)
	totalUSDT := 0.0
	for _, b := range rawBalances {
		free := parseFloatFromAny(b["free"])
		locked := parseFloatFromAny(b["locked"])
		total := free + locked
		if total <= 0 {
			continue
		}
		asset := ""
		if a, ok := b["asset"].(string); ok {
			asset = a
		}
		balances = append(balances, map[string]any{
			"asset":     asset,
			"currency":  asset,
			"free":      free,
			"available": free,
			"locked":    locked,
			"total":     total,
		})

		switch asset {
		case "USDT", "BUSD", "USDC":
			totalUSDT += total
		case "BTC":
			if price := getLastPrice("BTCUSDT"); price > 0 {
				totalUSDT += total * price
			}
		case "ETH":
			if price := getLastPrice("ETHUSDT"); price > 0 {
				totalUSDT += total * price
			}
		case "BNB":
			if price := getLastPrice("BNBUSDT"); price > 0 {
				totalUSDT += total * price
			}
		case "SOL":
			if price := getLastPrice("SOLUSDT"); price > 0 {
				totalUSDT += total * price
			}
		}
	}

	mgr := portfolio.GetManager()
	if mgr != nil && totalUSDT > 0 {
		mgr.UpdateBalance("binance", "USDT", totalUSDT, totalUSDT, 0)
	}

	c.JSON(http.StatusOK, gin.H{
		"balances":        balances,
		"currencies":      balances,
		"estimated_usdt":  totalUSDT,
		"source":          "binance",
	})
}

// parseFloatFromAny safely converts various numeric types to float64.
func parseFloatFromAny(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	}
	return 0
}

// getLastPrice fetches the latest price for a symbol from Binance public API.
func getLastPrice(symbol string) float64 {
	var result map[string]any
	resp, err := http.Get("https://api.binance.com/api/v3/ticker/price?symbol=" + symbol)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	json.Unmarshal(raw, &result)
	if priceStr, ok := result["price"].(string); ok {
		f, _ := strconv.ParseFloat(priceStr, 64)
		return f
	}
	return 0
}

// GetTradeHistory fetches executed trade history from the exchange.
func GetTradeHistory(c *gin.Context) {
	limit := 50
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}

	apiKey, secret, _ := adapter.GetCredential("binance")
	if apiKey == "" || secret == "" {
		c.JSON(http.StatusOK, gin.H{
			"trades": []map[string]any{},
			"source": "unconfigured",
			"message": "请在「设置 → 交易所」中配置 Binance API Key 以获取成交记录",
		})
		return
	}

	binance := adapter.NewBinanceAdapter(apiKey, secret, false)
	rawTrades, err := binance.GetAccountTradeHistory(c.Query("symbol"), limit)
	if err != nil {
		log.Printf("[Binance] GetTradeHistory error: %v", err)
		c.JSON(http.StatusOK, gin.H{"trades": []map[string]any{}, "error": err.Error()})
		return
	}

	trades := make([]map[string]any, 0, len(rawTrades))
	for _, t := range rawTrades {
		trades = append(trades, map[string]any{
			"symbol":    t.Symbol,
			"side":      t.Side,
			"price":     t.Price,
			"qty":       t.Quantity,
			"quantity":  t.Quantity,
			"pnl":       t.RealizedPnl,
			"time":      t.Time,
			"timestamp": t.Time,
			"id":        t.ID,
			"order_id":  t.OrderID,
			"commission": t.Commission,
			"commission_asset": t.CommissionAsset,
		})
	}

	c.JSON(http.StatusOK, gin.H{"trades": trades, "source": "binance"})
}

func fmtScan(s string, v *int) {
	if i, err := strconv.Atoi(s); err == nil {
		*v = i
	}
}
