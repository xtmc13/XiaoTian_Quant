package order

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── Advanced Order Engine ──
// Handles Iceberg, TWAP, and VWAP order execution.

// AdvancedOrder represents a parent order that will be sliced into child orders.
type AdvancedOrder struct {
	ID              string
	ParentOrder     *model.OrderData
	Type            model.OrderType
	TotalQuantity   float64
	FilledQuantity  float64
	RemainingQty    float64
	SliceSize       float64
	VisibleQty      float64 // For iceberg: qty visible in order book
	IntervalMs      int     // For TWAP: ms between slices
	DurationMs      int     // For TWAP: total duration
	StartTime       int64
	EndTime         int64
	Status          string // active, paused, completed, cancelled
	Children        []*model.OrderData
	mu              sync.RWMutex
}

// AdvancedOrderEngine manages execution of advanced order types.
type AdvancedOrderEngine struct {
	orders map[string]*AdvancedOrder
	mu     sync.RWMutex

	running bool
	stopCh  chan struct{}

	// Callbacks
	OnPlaceChild func(ord *model.OrderData) (*model.OrderData, error)
	OnCancelChild func(orderID string) error
}

var (
	advEngine     *AdvancedOrderEngine
	advEngineOnce sync.Once
)

// GetAdvancedOrderEngine returns the global advanced order engine.
func GetAdvancedOrderEngine() *AdvancedOrderEngine {
	advEngineOnce.Do(func() {
		advEngine = &AdvancedOrderEngine{
			orders: make(map[string]*AdvancedOrder),
			stopCh: make(chan struct{}),
		}
	})
	return advEngine
}

// Start begins the execution loop.
func (ae *AdvancedOrderEngine) Start() {
	ae.mu.Lock()
	if ae.running {
		ae.mu.Unlock()
		return
	}
	ae.running = true
	ae.mu.Unlock()

	go ae.executionLoop()
}

// Stop halts the execution loop.
func (ae *AdvancedOrderEngine) Stop() {
	ae.mu.Lock()
	if !ae.running {
		ae.mu.Unlock()
		return
	}
	ae.running = false
	ae.mu.Unlock()
	close(ae.stopCh)
}

// SubmitIceberg submits an iceberg order.
// totalQty: total order size
// visibleQty: amount visible in the order book at any time
// sliceSize: amount per child order
func (ae *AdvancedOrderEngine) SubmitIceberg(parent *model.OrderData, visibleQty, sliceSize float64) (*AdvancedOrder, error) {
	if sliceSize <= 0 {
		sliceSize = visibleQty
	}
	if sliceSize > parent.Quantity {
		sliceSize = parent.Quantity
	}

	ao := &AdvancedOrder{
		ID:           fmt.Sprintf("iceberg-%s", parent.ID),
		ParentOrder:  parent,
		Type:         model.TypeIceberg,
		TotalQuantity: parent.Quantity,
		RemainingQty:  parent.Quantity,
		SliceSize:     sliceSize,
		VisibleQty:    visibleQty,
		IntervalMs:    2000, // 2s between slices
		StartTime:     time.Now().UnixMilli(),
		Status:        "active",
		Children:      make([]*model.OrderData, 0),
	}

	ae.mu.Lock()
	ae.orders[ao.ID] = ao
	ae.mu.Unlock()

	// Place first slice immediately
	ae.placeNextSlice(ao)

	return ao, nil
}

// SubmitTWAP submits a TWAP order.
// durationMs: total execution time
// slices: number of slices
func (ae *AdvancedOrderEngine) SubmitTWAP(parent *model.OrderData, durationMs, slices int) (*AdvancedOrder, error) {
	if slices <= 0 {
		slices = int(math.Max(1, math.Min(float64(parent.Quantity), 10)))
	}
	if durationMs <= 0 {
		durationMs = 60000 // 1 minute default
	}

	sliceSize := parent.Quantity / float64(slices)
	intervalMs := durationMs / slices

	ao := &AdvancedOrder{
		ID:           fmt.Sprintf("twap-%s", parent.ID),
		ParentOrder:  parent,
		Type:         model.TypeTWAP,
		TotalQuantity: parent.Quantity,
		RemainingQty:  parent.Quantity,
		SliceSize:     sliceSize,
		IntervalMs:    intervalMs,
		DurationMs:    durationMs,
		StartTime:     time.Now().UnixMilli(),
		EndTime:       time.Now().UnixMilli() + int64(durationMs),
		Status:        "active",
		Children:      make([]*model.OrderData, 0),
	}

	ae.mu.Lock()
	ae.orders[ao.ID] = ao
	ae.mu.Unlock()

	// Place first slice immediately
	ae.placeNextSlice(ao)

	return ao, nil
}

// SubmitVWAP submits a VWAP order.
// Uses volume profile to determine slice sizes (simplified: equal slices).
func (ae *AdvancedOrderEngine) SubmitVWAP(parent *model.OrderData, durationMs, slices int) (*AdvancedOrder, error) {
	if slices <= 0 {
		slices = int(math.Max(1, math.Min(float64(parent.Quantity), 10)))
	}
	if durationMs <= 0 {
		durationMs = 60000
	}

	sliceSize := parent.Quantity / float64(slices)
	intervalMs := durationMs / slices

	ao := &AdvancedOrder{
		ID:           fmt.Sprintf("vwap-%s", parent.ID),
		ParentOrder:  parent,
		Type:         model.TypeVWAP,
		TotalQuantity: parent.Quantity,
		RemainingQty:  parent.Quantity,
		SliceSize:     sliceSize,
		IntervalMs:    intervalMs,
		DurationMs:    durationMs,
		StartTime:     time.Now().UnixMilli(),
		EndTime:       time.Now().UnixMilli() + int64(durationMs),
		Status:        "active",
		Children:      make([]*model.OrderData, 0),
	}

	ae.mu.Lock()
	ae.orders[ao.ID] = ao
	ae.mu.Unlock()

	ae.placeNextSlice(ao)

	return ao, nil
}

// Cancel cancels an advanced order and all its children.
func (ae *AdvancedOrderEngine) Cancel(advOrderID string) error {
	ae.mu.Lock()
	ao, ok := ae.orders[advOrderID]
	if !ok {
		ae.mu.Unlock()
		return fmt.Errorf("advanced order %s not found", advOrderID)
	}
	ao.Status = "cancelled"
	children := make([]*model.OrderData, len(ao.Children))
	copy(children, ao.Children)
	delete(ae.orders, advOrderID)
	ae.mu.Unlock()

	// Cancel all child orders
	if ae.OnCancelChild != nil {
		for _, child := range children {
			if child.IsActive() {
				ae.OnCancelChild(child.ID)
			}
		}
	}

	return nil
}

// Get returns an advanced order by ID.
func (ae *AdvancedOrderEngine) Get(advOrderID string) *AdvancedOrder {
	ae.mu.RLock()
	defer ae.mu.RUnlock()
	return ae.orders[advOrderID]
}

// List returns all active advanced orders.
func (ae *AdvancedOrderEngine) List() []*AdvancedOrder {
	ae.mu.RLock()
	defer ae.mu.RUnlock()
	result := make([]*AdvancedOrder, 0, len(ae.orders))
	for _, ao := range ae.orders {
		if ao.Status == "active" {
			result = append(result, ao)
		}
	}
	return result
}

// ── Internal ──

func (ae *AdvancedOrderEngine) executionLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ae.stopCh:
			return
		case <-ticker.C:
			ae.processActiveOrders()
		}
	}
}

func (ae *AdvancedOrderEngine) processActiveOrders() {
	ae.mu.RLock()
	orders := make([]*AdvancedOrder, 0, len(ae.orders))
	for _, ao := range ae.orders {
		if ao.Status == "active" {
			orders = append(orders, ao)
		}
	}
	ae.mu.RUnlock()

	now := time.Now().UnixMilli()
	for _, ao := range orders {
		// Check if completed
		if ao.RemainingQty <= 0.0000001 {
			ae.mu.Lock()
			ao.Status = "completed"
			ae.mu.Unlock()
			continue
		}

		// Check if TWAP/VWAP time expired
		if (ao.Type == model.TypeTWAP || ao.Type == model.TypeVWAP) && ao.EndTime > 0 && now > ao.EndTime {
			// Place remaining as market order
			if ae.OnPlaceChild != nil && ao.RemainingQty > 0 {
				child := ae.createChildOrder(ao, ao.RemainingQty)
				child.OrderType = model.TypeMarket
				ae.OnPlaceChild(child)
			}
			ae.mu.Lock()
			ao.Status = "completed"
			ae.mu.Unlock()
			continue
		}

		// Check if enough time passed since last slice
		lastSliceTime := ao.StartTime
		if len(ao.Children) > 0 {
			lastSliceTime = ao.Children[len(ao.Children)-1].CreatedAt
		}
		if now-lastSliceTime < int64(ao.IntervalMs) {
			continue
		}

		// Place next slice
		ae.placeNextSlice(ao)
	}
}

func (ae *AdvancedOrderEngine) placeNextSlice(ao *AdvancedOrder) {
	sliceQty := math.Min(ao.SliceSize, ao.RemainingQty)
	if sliceQty <= 0.0000001 {
		return
	}

	child := ae.createChildOrder(ao, sliceQty)

	if ae.OnPlaceChild != nil {
		placed, err := ae.OnPlaceChild(child)
		if err == nil && placed != nil {
			ae.mu.Lock()
			ao.Children = append(ao.Children, placed)
			ao.RemainingQty -= sliceQty
			if ao.RemainingQty <= 0.0000001 {
				ao.Status = "completed"
			}
			ae.mu.Unlock()
		}
	}
}

func (ae *AdvancedOrderEngine) createChildOrder(ao *AdvancedOrder, qty float64) *model.OrderData {
	parent := ao.ParentOrder
	return &model.OrderData{
		ID:           fmt.Sprintf("%s-child-%d", ao.ID, len(ao.Children)),
		Symbol:       parent.Symbol,
		Side:         parent.Side,
		OrderType:    parent.OrderType,
		Price:        parent.Price,
		Quantity:     qty,
		Exchange:     parent.Exchange,
		MarketType:   parent.MarketType,
		PositionSide: parent.PositionSide,
		Leverage:     parent.Leverage,
		MarginMode:   parent.MarginMode,
		CreatedAt:    time.Now().UnixMilli(),
		UpdatedAt:    time.Now().UnixMilli(),
	}
}
