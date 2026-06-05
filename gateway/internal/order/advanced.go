package order

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── OCO Order ──────────────────────────────────────────────────

// OCOOrder combines a limit order (take-profit) and a stop-loss order.
// When one fills, the other is automatically cancelled.
type OCOOrder struct {
	ID          string
	Symbol      string
	Side        model.OrderSide
	Quantity    float64
	LimitPrice  float64 // take-profit price
	StopPrice   float64 // stop-loss trigger price
	StopLimit   float64 // stop-loss limit price (optional)
	Status      string
	ParentID    string // reference to entry order

	// Child orders
	LimitOrderID string
	StopOrderID  string

	// Callbacks
	OnFill   func(side string, price float64)
	OnCancel func()
}

// Validate checks OCO parameters.
func (o *OCOOrder) Validate() error {
	if o.Symbol == "" {
		return fmt.Errorf("symbol required")
	}
	if o.Quantity <= 0 {
		return fmt.Errorf("quantity must be positive")
	}
	if o.LimitPrice <= 0 || o.StopPrice <= 0 {
		return fmt.Errorf("both limit and stop prices required")
	}
	if o.Side == model.SideBuy {
		if o.LimitPrice >= o.StopPrice {
			return fmt.Errorf("for BUY OCO, limit price must be below stop price")
		}
	} else {
		if o.LimitPrice <= o.StopPrice {
			return fmt.Errorf("for SELL OCO, limit price must be above stop price")
		}
	}
	return nil
}

// ── Bracket Order ──────────────────────────────────────────────

// BracketOrder combines entry + take-profit + stop-loss.
type BracketOrder struct {
	ID           string
	Symbol       string
	Side         model.OrderSide
	Quantity     float64
	EntryPrice   float64 // limit or market
	TakeProfit   float64
	StopLoss     float64
	EntryType    model.OrderType // MARKET or LIMIT

	// Child order IDs
	EntryOrderID string
	TPOrderID    string
	SLOrderID    string

	Status       string
	PlacedAt     int64
}

// Validate checks bracket parameters.
func (b *BracketOrder) Validate() error {
	if b.Symbol == "" || b.Quantity <= 0 {
		return fmt.Errorf("symbol and quantity required")
	}
	if b.EntryType == model.TypeLimit && b.EntryPrice <= 0 {
		return fmt.Errorf("limit entry requires entry price")
	}
	if b.TakeProfit <= 0 || b.StopLoss <= 0 {
		return fmt.Errorf("take-profit and stop-loss prices required")
	}
	if b.Side == model.SideBuy {
		if b.TakeProfit <= b.EntryPrice {
			return fmt.Errorf("take-profit must be above entry for BUY")
		}
		if b.StopLoss >= b.EntryPrice {
			return fmt.Errorf("stop-loss must be below entry for BUY")
		}
	} else {
		if b.TakeProfit >= b.EntryPrice {
			return fmt.Errorf("take-profit must be below entry for SELL")
		}
		if b.StopLoss <= b.EntryPrice {
			return fmt.Errorf("stop-loss must be above entry for SELL")
		}
	}
	return nil
}

// ── Iceberg Order ──────────────────────────────────────────────

// IcebergOrder splits a large order into smaller visible chunks.
type IcebergOrder struct {
	ID              string
	Symbol          string
	Side            model.OrderSide
	TotalQuantity   float64
	VisibleQuantity float64 // amount shown on orderbook per slice
	Price           float64 // limit price
	Variance        float64 // random variance for slice sizes (0-1)

	// State
	FilledTotal     float64
	SlicesPlaced    int
	SlicesFilled    int
	Status          string

	// Callback
	PlaceSlice func(qty float64) (string, error) // returns order ID
}

// Validate checks iceberg parameters.
func (i *IcebergOrder) Validate() error {
	if i.Symbol == "" || i.TotalQuantity <= 0 {
		return fmt.Errorf("symbol and total quantity required")
	}
	if i.VisibleQuantity <= 0 || i.VisibleQuantity >= i.TotalQuantity {
		return fmt.Errorf("visible quantity must be positive and less than total")
	}
	if i.Price <= 0 {
		return fmt.Errorf("price required")
	}
	if i.Variance < 0 || i.Variance > 1 {
		return fmt.Errorf("variance must be between 0 and 1")
	}
	return nil
}

// NextSlice returns the quantity for the next slice.
func (i *IcebergOrder) NextSlice() float64 {
	remaining := i.TotalQuantity - i.FilledTotal
	if remaining <= 0 {
		return 0
	}

	base := i.VisibleQuantity
	if i.Variance > 0 {
		// Apply random variance
		variance := base * i.Variance
		base = base + (randFloat()*2-1)*variance
		if base < i.VisibleQuantity*0.5 {
			base = i.VisibleQuantity * 0.5
		}
	}

	if base > remaining {
		base = remaining
	}
	return base
}

// IsComplete returns true when all slices are filled.
func (i *IcebergOrder) IsComplete() bool {
	return i.FilledTotal >= i.TotalQuantity
}

// ── Advanced Order Manager ─────────────────────────────────────

// AdvancedManager handles OCO, Bracket, and Iceberg orders.
type AdvancedManager struct {
	ocoOrders     map[string]*OCOOrder
	bracketOrders map[string]*BracketOrder
	icebergOrders map[string]*IcebergOrder
	mu            sync.RWMutex
	seq           int64

	// Integration hooks
	PlaceOrder    func(req *Request) (*model.OrderData, error)
	CancelOrder   func(orderID string) error
	GetOrder      func(orderID string) *model.OrderData
}

// NewAdvancedManager creates an advanced order manager.
func NewAdvancedManager() *AdvancedManager {
	return &AdvancedManager{
		ocoOrders:     make(map[string]*OCOOrder),
		bracketOrders: make(map[string]*BracketOrder),
		icebergOrders: make(map[string]*IcebergOrder),
	}
}

// PlaceOCO creates an OCO order.
func (am *AdvancedManager) PlaceOCO(oco *OCOOrder) (*OCOOrder, error) {
	if err := oco.Validate(); err != nil {
		return nil, err
	}

	am.mu.Lock()
	am.seq++
	oco.ID = fmt.Sprintf("oco-%d-%d", time.Now().UnixMilli(), am.seq)
	oco.Status = "ACTIVE"
	am.ocoOrders[oco.ID] = oco
	am.mu.Unlock()

	// Place both child orders
	if am.PlaceOrder != nil {
		// Limit order (take-profit)
		limitReq := &Request{
			Symbol:    oco.Symbol,
			Side:      oco.Side,
			OrderType: model.TypeLimit,
			Price:     oco.LimitPrice,
			Quantity:  oco.Quantity,
		}
		limitOrder, err := am.PlaceOrder(limitReq)
		if err != nil {
			return oco, fmt.Errorf("limit order failed: %w", err)
		}
		oco.LimitOrderID = limitOrder.ID

		// Stop-loss order
		stopType := model.TypeStopLoss
		if oco.StopLimit > 0 {
			stopType = model.TypeStopLossLimit
		}
		stopReq := &Request{
			Symbol:    oco.Symbol,
			Side:      oco.Side,
			OrderType: stopType,
			Price:     oco.StopLimit,
			StopPrice: oco.StopPrice,
			Quantity:  oco.Quantity,
		}
		stopOrder, err := am.PlaceOrder(stopReq)
		if err != nil {
			// Cancel limit order if stop fails
			am.CancelOrder(limitOrder.ID)
			return oco, fmt.Errorf("stop order failed: %w", err)
		}
		oco.StopOrderID = stopOrder.ID
	}

	return oco, nil
}

// HandleOCOUpdate processes child order updates for OCO logic.
func (am *AdvancedManager) HandleOCOUpdate(ocoID, childOrderID string, status model.OrderStatus) {
	am.mu.Lock()
	oco, ok := am.ocoOrders[ocoID]
	if !ok {
		am.mu.Unlock()
		return
	}

	if status == model.StatusFilled {
		oco.Status = "FILLED"
		// Cancel the other child
		var otherID string
		if childOrderID == oco.LimitOrderID {
			otherID = oco.StopOrderID
		} else {
			otherID = oco.LimitOrderID
		}
		am.mu.Unlock()

		if otherID != "" && am.CancelOrder != nil {
			am.CancelOrder(otherID)
		}
		if oco.OnFill != nil {
			oco.OnFill(string(oco.Side), 0)
		}
		return
	}

	if status == model.StatusCancelled || status == model.StatusRejected {
		// Check if both children are dead
		bothDead := true
		for _, id := range []string{oco.LimitOrderID, oco.StopOrderID} {
			if id == "" {
				continue
			}
			if id == childOrderID {
				continue
			}
			if am.GetOrder != nil {
				order := am.GetOrder(id)
				if order != nil && !order.IsDone() {
					bothDead = false
					break
				}
			}
		}
		if bothDead {
			oco.Status = "CANCELLED"
			if oco.OnCancel != nil {
				oco.OnCancel()
			}
		}
	}
	am.mu.Unlock()
}

// CancelOCO cancels an OCO order and both children.
func (am *AdvancedManager) CancelOCO(ocoID string) error {
	am.mu.Lock()
	oco, ok := am.ocoOrders[ocoID]
	if !ok {
		am.mu.Unlock()
		return fmt.Errorf("OCO order %s not found", ocoID)
	}
	am.mu.Unlock()

	if am.CancelOrder != nil {
		if oco.LimitOrderID != "" {
			am.CancelOrder(oco.LimitOrderID)
		}
		if oco.StopOrderID != "" {
			am.CancelOrder(oco.StopOrderID)
		}
	}

	am.mu.Lock()
	oco.Status = "CANCELLED"
	am.mu.Unlock()
	return nil
}

// PlaceBracket creates a bracket order (entry + TP + SL).
func (am *AdvancedManager) PlaceBracket(bracket *BracketOrder) (*BracketOrder, error) {
	if err := bracket.Validate(); err != nil {
		return nil, err
	}

	am.mu.Lock()
	am.seq++
	bracket.ID = fmt.Sprintf("bracket-%d-%d", time.Now().UnixMilli(), am.seq)
	bracket.Status = "PENDING_ENTRY"
	bracket.PlacedAt = time.Now().UnixMilli()
	am.bracketOrders[bracket.ID] = bracket
	am.mu.Unlock()

	if am.PlaceOrder != nil {
		// Place entry order
		entryReq := &Request{
			Symbol:    bracket.Symbol,
			Side:      bracket.Side,
			OrderType: bracket.EntryType,
			Price:     bracket.EntryPrice,
			Quantity:  bracket.Quantity,
		}
		entryOrder, err := am.PlaceOrder(entryReq)
		if err != nil {
			return bracket, fmt.Errorf("entry order failed: %w", err)
		}
		bracket.EntryOrderID = entryOrder.ID
	}

	return bracket, nil
}

// HandleBracketEntry processes entry fill and places TP/SL.
func (am *AdvancedManager) HandleBracketEntry(bracketID string, fillPrice float64) {
	am.mu.Lock()
	bracket, ok := am.bracketOrders[bracketID]
	if !ok || bracket.Status != "PENDING_ENTRY" {
		am.mu.Unlock()
		return
	}
	bracket.Status = "ENTRY_FILLED"
	am.mu.Unlock()

	if am.PlaceOrder == nil {
		return
	}

	// Determine TP/SL sides (opposite of entry)
	tpSide := model.SideSell
	slSide := model.SideSell
	if bracket.Side == model.SideSell {
		tpSide = model.SideBuy
		slSide = model.SideBuy
	}

	// Place take-profit
	tpReq := &Request{
		Symbol:    bracket.Symbol,
		Side:      tpSide,
		OrderType: model.TypeLimit,
		Price:     bracket.TakeProfit,
		Quantity:  bracket.Quantity,
	}
	tpOrder, err := am.PlaceOrder(tpReq)
	if err == nil {
		bracket.TPOrderID = tpOrder.ID
	}

	// Place stop-loss
	slReq := &Request{
		Symbol:    bracket.Symbol,
		Side:      slSide,
		OrderType: model.TypeStopLoss,
		StopPrice: bracket.StopLoss,
		Quantity:  bracket.Quantity,
	}
	slOrder, err := am.PlaceOrder(slReq)
	if err == nil {
		bracket.SLOrderID = slOrder.ID
	}

	am.mu.Lock()
	bracket.Status = "ACTIVE"
	am.mu.Unlock()
}

// HandleBracketChildUpdate processes TP/SL updates.
func (am *AdvancedManager) HandleBracketChildUpdate(bracketID, childID string, status model.OrderStatus) {
	am.mu.Lock()
	bracket, ok := am.bracketOrders[bracketID]
	if !ok {
		am.mu.Unlock()
		return
	}

	if status == model.StatusFilled {
		// Cancel remaining child
		var otherID string
		if childID == bracket.TPOrderID {
			otherID = bracket.SLOrderID
			bracket.Status = "TP_FILLED"
		} else {
			otherID = bracket.TPOrderID
			bracket.Status = "SL_FILLED"
		}
		am.mu.Unlock()

		if otherID != "" && am.CancelOrder != nil {
			am.CancelOrder(otherID)
		}
		return
	}

	if status == model.StatusCancelled || status == model.StatusRejected {
		// Check if both TP and SL are dead
		bothDead := true
		for _, id := range []string{bracket.TPOrderID, bracket.SLOrderID} {
			if id == "" || id == childID {
				continue
			}
			if am.GetOrder != nil {
				order := am.GetOrder(id)
				if order != nil && !order.IsDone() {
					bothDead = false
					break
				}
			}
		}
		if bothDead {
			bracket.Status = "CANCELLED"
		}
	}
	am.mu.Unlock()
}

// CancelBracket cancels a bracket order and all children.
func (am *AdvancedManager) CancelBracket(bracketID string) error {
	am.mu.Lock()
	bracket, ok := am.bracketOrders[bracketID]
	if !ok {
		am.mu.Unlock()
		return fmt.Errorf("bracket order %s not found", bracketID)
	}
	am.mu.Unlock()

	if am.CancelOrder != nil {
		for _, id := range []string{bracket.EntryOrderID, bracket.TPOrderID, bracket.SLOrderID} {
			if id != "" {
				am.CancelOrder(id)
			}
		}
	}

	am.mu.Lock()
	bracket.Status = "CANCELLED"
	am.mu.Unlock()
	return nil
}

// PlaceIceberg creates an iceberg order.
func (am *AdvancedManager) PlaceIceberg(iceberg *IcebergOrder) (*IcebergOrder, error) {
	if err := iceberg.Validate(); err != nil {
		return nil, err
	}

	am.mu.Lock()
	am.seq++
	iceberg.ID = fmt.Sprintf("iceberg-%d-%d", time.Now().UnixMilli(), am.seq)
	iceberg.Status = "ACTIVE"
	iceberg.FilledTotal = 0
	iceberg.SlicesPlaced = 0
	iceberg.SlicesFilled = 0
	am.icebergOrders[iceberg.ID] = iceberg
	am.mu.Unlock()

	// Place first slice
	am.placeNextIcebergSlice(iceberg)

	return iceberg, nil
}

// placeNextIcebergSlice places the next visible slice.
func (am *AdvancedManager) placeNextIcebergSlice(iceberg *IcebergOrder) {
	if iceberg.IsComplete() {
		iceberg.Status = "FILLED"
		return
	}

	sliceQty := iceberg.NextSlice()
	if sliceQty <= 0 {
		return
	}

	if iceberg.PlaceSlice != nil {
		orderID, err := iceberg.PlaceSlice(sliceQty)
		if err != nil {
			iceberg.Status = "ERROR"
			return
		}
		_ = orderID
	}

	iceberg.SlicesPlaced++
}

// HandleIcebergFill processes a slice fill and places the next slice.
func (am *AdvancedManager) HandleIcebergFill(icebergID string, filledQty float64) {
	am.mu.Lock()
	iceberg, ok := am.icebergOrders[icebergID]
	if !ok {
		am.mu.Unlock()
		return
	}

	iceberg.FilledTotal += filledQty
	iceberg.SlicesFilled++

	if iceberg.IsComplete() {
		iceberg.Status = "FILLED"
		am.mu.Unlock()
		return
	}
	am.mu.Unlock()

	// Place next slice
	am.placeNextIcebergSlice(iceberg)
}

// CancelIceberg cancels an iceberg order.
func (am *AdvancedManager) CancelIceberg(icebergID string) error {
	am.mu.Lock()
	iceberg, ok := am.icebergOrders[icebergID]
	if !ok {
		am.mu.Unlock()
		return fmt.Errorf("iceberg order %s not found", icebergID)
	}
	iceberg.Status = "CANCELLED"
	am.mu.Unlock()
	return nil
}

// GetOCO returns an OCO order by ID.
func (am *AdvancedManager) GetOCO(id string) *OCOOrder {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.ocoOrders[id]
}

// GetBracket returns a bracket order by ID.
func (am *AdvancedManager) GetBracket(id string) *BracketOrder {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.bracketOrders[id]
}

// GetIceberg returns an iceberg order by ID.
func (am *AdvancedManager) GetIceberg(id string) *IcebergOrder {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.icebergOrders[id]
}

// ListActiveOCO returns active OCO orders.
func (am *AdvancedManager) ListActiveOCO() []*OCOOrder {
	am.mu.RLock()
	defer am.mu.RUnlock()
	var result []*OCOOrder
	for _, o := range am.ocoOrders {
		if o.Status == "ACTIVE" {
			result = append(result, o)
		}
	}
	return result
}

// ListActiveBracket returns active bracket orders.
func (am *AdvancedManager) ListActiveBracket() []*BracketOrder {
	am.mu.RLock()
	defer am.mu.RUnlock()
	var result []*BracketOrder
	for _, b := range am.bracketOrders {
		if b.Status == "ACTIVE" || b.Status == "PENDING_ENTRY" {
			result = append(result, b)
		}
	}
	return result
}

// ListActiveIceberg returns active iceberg orders.
func (am *AdvancedManager) ListActiveIceberg() []*IcebergOrder {
	am.mu.RLock()
	defer am.mu.RUnlock()
	var result []*IcebergOrder
	for _, i := range am.icebergOrders {
		if i.Status == "ACTIVE" {
			result = append(result, i)
		}
	}
	return result
}

// ── Helpers ────────────────────────────────────────────────────

func randFloat() float64 {
	return float64(time.Now().UnixNano()%1000) / 1000.0
}

// CalculateBracketPrices computes TP/SL prices from entry and risk/reward.
func CalculateBracketPrices(entryPrice float64, side model.OrderSide, stopLossPct, takeProfitPct float64) (tp, sl float64) {
	if side == model.SideBuy {
		tp = entryPrice * (1 + takeProfitPct)
		sl = entryPrice * (1 - stopLossPct)
	} else {
		tp = entryPrice * (1 - takeProfitPct)
		sl = entryPrice * (1 + stopLossPct)
	}
	return
}

// CalculatePositionSize computes quantity from risk amount and stop distance.
func CalculatePositionSize(balance, riskPct, entryPrice, stopPrice float64) float64 {
	riskAmount := balance * riskPct
	stopDistance := math.Abs(entryPrice - stopPrice)
	if stopDistance <= 0 {
		return 0
	}
	qty := riskAmount / stopDistance
	return math.Floor(qty*1e6) / 1e6 // round to 6 decimals
}
