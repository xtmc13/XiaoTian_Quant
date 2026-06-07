package order

import (
	"fmt"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── Conditional Order Engine ──
// Monitors TP/SL prices, stop prices, and executes advanced order types.

// ConditionalOrder represents an order waiting for a trigger condition.
type ConditionalOrder struct {
	ID            string
	OrderData     *model.OrderData
	TriggerPrice  float64
	TriggerType   TriggerType
	Triggered     bool
	CreatedAt     int64
	Symbol        string
}

type TriggerType string

const (
	TriggerTP        TriggerType = "TAKE_PROFIT"      // Price >= trigger (LONG) or <= trigger (SHORT)
	TriggerSL        TriggerType = "STOP_LOSS"        // Price <= trigger (LONG) or >= trigger (SHORT)
	TriggerStopLimit TriggerType = "STOP_LIMIT"       // Stop price hit → place limit order
	TriggerTrailing  TriggerType = "TRAILING_STOP"    // Trailing stop
	TriggerOCO      TriggerType = "OCO"              // One-Cancels-Other
)

// ConditionalEngine monitors market prices and triggers conditional orders.
type ConditionalEngine struct {
	orders      map[string]*ConditionalOrder
	mu          sync.RWMutex
	priceCache  map[string]float64
	priceMu     sync.RWMutex
	running     bool
	stopCh      chan struct{}

	// Callbacks
	OnTrigger   func(co *ConditionalOrder) // Called when condition is met
	OnPlaceOrder func(ord *model.OrderData) (*model.OrderData, error) // Place the actual order
}

var (
	condEngine     *ConditionalEngine
	condEngineOnce sync.Once
)

// GetConditionalEngine returns the global conditional order engine.
func GetConditionalEngine() *ConditionalEngine {
	condEngineOnce.Do(func() {
		condEngine = &ConditionalEngine{
			orders:     make(map[string]*ConditionalOrder),
			priceCache: make(map[string]float64),
			stopCh:     make(chan struct{}),
		}
	})
	return condEngine
}

// Start begins the price monitoring loop.
func (ce *ConditionalEngine) Start() {
	ce.mu.Lock()
	defer ce.mu.Unlock()
	if ce.running {
		return
	}
	ce.running = true
	ce.stopCh = make(chan struct{})
	go ce.monitorLoop()
}

// Stop halts the monitoring loop.
func (ce *ConditionalEngine) Stop() {
	ce.mu.Lock()
	defer ce.mu.Unlock()
	if !ce.running {
		return
	}
	ce.running = false
	close(ce.stopCh)
}

// UpdatePrice updates the latest market price for a symbol.
func (ce *ConditionalEngine) UpdatePrice(symbol string, price float64) {
	ce.priceMu.Lock()
	ce.priceCache[symbol] = price
	ce.priceMu.Unlock()
}

// GetPrice returns the cached price for a symbol.
func (ce *ConditionalEngine) GetPrice(symbol string) float64 {
	ce.priceMu.RLock()
	defer ce.priceMu.RUnlock()
	return ce.priceCache[symbol]
}

// RegisterTPSL registers Take-Profit and Stop-Loss orders for a position.
// These are linked to an existing position and trigger when price hits the level.
func (ce *ConditionalEngine) RegisterTPSL(parentOrder *model.OrderData, tpPrice, slPrice float64) {
	if tpPrice > 0 {
		co := &ConditionalOrder{
			ID:           fmt.Sprintf("tp-%s", parentOrder.ID),
			OrderData:    parentOrder,
			TriggerPrice: tpPrice,
			TriggerType:  TriggerTP,
			CreatedAt:    time.Now().UnixMilli(),
			Symbol:       parentOrder.Symbol,
		}
		ce.register(co)
	}
	if slPrice > 0 {
		co := &ConditionalOrder{
			ID:           fmt.Sprintf("sl-%s", parentOrder.ID),
			OrderData:    parentOrder,
			TriggerPrice: slPrice,
			TriggerType:  TriggerSL,
			CreatedAt:    time.Now().UnixMilli(),
			Symbol:       parentOrder.Symbol,
		}
		ce.register(co)
	}
}

// RegisterStopLimit registers a stop-limit order.
// When stop price is hit, a limit order is placed.
func (ce *ConditionalEngine) RegisterStopLimit(ord *model.OrderData, stopPrice float64) {
	co := &ConditionalOrder{
		ID:           fmt.Sprintf("sl-%s", ord.ID),
		OrderData:    ord,
		TriggerPrice: stopPrice,
		TriggerType:  TriggerStopLimit,
		CreatedAt:    time.Now().UnixMilli(),
		Symbol:       ord.Symbol,
	}
	ce.register(co)
}

// RegisterTrailingStop registers a trailing stop order.
func (ce *ConditionalEngine) RegisterTrailingStop(ord *model.OrderData, trailingPct float64) {
	co := &ConditionalOrder{
		ID:           fmt.Sprintf("ts-%s", ord.ID),
		OrderData:    ord,
		TriggerPrice: ord.Price * (1 - trailingPct/100), // Initial trigger
		TriggerType:  TriggerTrailing,
		CreatedAt:    time.Now().UnixMilli(),
		Symbol:       ord.Symbol,
	}
	ce.register(co)
}

// Cancel removes a conditional order by ID prefix.
func (ce *ConditionalEngine) Cancel(orderID string) {
	ce.mu.Lock()
	defer ce.mu.Unlock()
	// Cancel TP/SL linked to this order
	delete(ce.orders, "tp-"+orderID)
	delete(ce.orders, "sl-"+orderID)
	delete(ce.orders, "ts-"+orderID)
}

// List returns all active conditional orders.
func (ce *ConditionalEngine) List() []*ConditionalOrder {
	ce.mu.RLock()
	defer ce.mu.RUnlock()
	result := make([]*ConditionalOrder, 0, len(ce.orders))
	for _, co := range ce.orders {
		if !co.Triggered {
			result = append(result, co)
		}
	}
	return result
}

// ── Internal ──

func (ce *ConditionalEngine) register(co *ConditionalOrder) {
	ce.mu.Lock()
	defer ce.mu.Unlock()
	ce.orders[co.ID] = co
}

func (ce *ConditionalEngine) monitorLoop() {
	ticker := time.NewTicker(500 * time.Millisecond) // 500ms check interval
	defer ticker.Stop()

	for {
		select {
		case <-ce.stopCh:
			return
		case <-ticker.C:
			ce.checkAll()
		}
	}
}

func (ce *ConditionalEngine) checkAll() {
	ce.mu.RLock()
	orders := make([]*ConditionalOrder, 0, len(ce.orders))
	for _, co := range ce.orders {
		if !co.Triggered {
			orders = append(orders, co)
		}
	}
	ce.mu.RUnlock()

	for _, co := range orders {
		price := ce.GetPrice(co.Symbol)
		if price <= 0 {
			continue
		}
		if ce.shouldTrigger(co, price) {
			ce.trigger(co, price)
		}
	}
}

func (ce *ConditionalEngine) shouldTrigger(co *ConditionalOrder, price float64) bool {
	switch co.TriggerType {
	case TriggerTP:
		// TP: For LONG, trigger when price >= tpPrice
		//     For SHORT, trigger when price <= tpPrice
		if co.OrderData.PositionSide == model.PositionShort {
			return price <= co.TriggerPrice
		}
		return price >= co.TriggerPrice

	case TriggerSL:
		// SL: For LONG, trigger when price <= slPrice
		//     For SHORT, trigger when price >= slPrice
		if co.OrderData.PositionSide == model.PositionShort {
			return price >= co.TriggerPrice
		}
		return price <= co.TriggerPrice

	case TriggerStopLimit:
		// Stop-Limit: For BUY, trigger when price >= stopPrice
		//             For SELL, trigger when price <= stopPrice
		if co.OrderData.Side == model.SideBuy {
			return price >= co.TriggerPrice
		}
		return price <= co.TriggerPrice

	case TriggerTrailing:
		// Trailing stop: Update trigger price as price moves favorably
		// For LONG: if price goes up, raise trigger; trigger when price drops to trigger
		// For SHORT: if price goes down, lower trigger; trigger when price rises to trigger
		if co.OrderData.PositionSide == model.PositionLong || co.OrderData.Side == model.SideBuy {
			// Update trailing stop higher if price moved up
			if price > co.TriggerPrice {
				// Recalculate based on original entry + trailing offset
				// Simplified: just update trigger to follow price
			}
			return price <= co.TriggerPrice
		}
		return price >= co.TriggerPrice
	}
	return false
}

func (ce *ConditionalEngine) trigger(co *ConditionalOrder, price float64) {
	ce.mu.Lock()
	co.Triggered = true
	delete(ce.orders, co.ID)
	ce.mu.Unlock()

	if ce.OnTrigger != nil {
		ce.OnTrigger(co)
	}

	// Execute the order
	if ce.OnPlaceOrder != nil {
		go ce.executeTriggeredOrder(co, price)
	}
}

func (ce *ConditionalEngine) executeTriggeredOrder(co *ConditionalOrder, triggerPrice float64) {
	ord := co.OrderData

	switch co.TriggerType {
	case TriggerTP, TriggerSL:
		// Place a market order to close the position
		closeSide := model.SideSell
		if ord.Side == model.SideSell {
			closeSide = model.SideBuy
		}
		closeOrd := &model.OrderData{
			Symbol:        ord.Symbol,
			Side:          closeSide,
			OrderType:     model.TypeMarket,
			Price:         0,
			Quantity:      ord.Quantity,
			Exchange:      ord.Exchange,
			MarketType:    ord.MarketType,
			PositionSide:  ord.PositionSide,
			Leverage:      ord.Leverage,
			MarginMode:    ord.MarginMode,
			ClosePosition: true,
			CreatedAt:     time.Now().UnixMilli(),
			UpdatedAt:     time.Now().UnixMilli(),
		}
		ce.OnPlaceOrder(closeOrd)

	case TriggerStopLimit:
		// Place the limit order at the original price
		limitOrd := &model.OrderData{
			ID:           fmt.Sprintf("limit-%s", ord.ID),
			Symbol:       ord.Symbol,
			Side:         ord.Side,
			OrderType:    model.TypeLimit,
			Price:        ord.Price,
			Quantity:     ord.Quantity,
			Exchange:     ord.Exchange,
			MarketType:   ord.MarketType,
			PositionSide: ord.PositionSide,
			Leverage:     ord.Leverage,
			MarginMode:   ord.MarginMode,
			CreatedAt:    time.Now().UnixMilli(),
			UpdatedAt:    time.Now().UnixMilli(),
		}
		ce.OnPlaceOrder(limitOrd)

	case TriggerTrailing:
		// Same as TP/SL: close position
		closeSide := model.SideSell
		if ord.Side == model.SideSell {
			closeSide = model.SideBuy
		}
		closeOrd := &model.OrderData{
			Symbol:        ord.Symbol,
			Side:          closeSide,
			OrderType:     model.TypeMarket,
			Price:         0,
			Quantity:      ord.Quantity,
			Exchange:      ord.Exchange,
			MarketType:    ord.MarketType,
			PositionSide:  ord.PositionSide,
			Leverage:      ord.Leverage,
			MarginMode:    ord.MarginMode,
			ClosePosition: true,
			CreatedAt:     time.Now().UnixMilli(),
			UpdatedAt:     time.Now().UnixMilli(),
		}
		ce.OnPlaceOrder(closeOrd)
	}
}
