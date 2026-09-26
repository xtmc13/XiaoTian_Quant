package service

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/adapter"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// MatchingService provides order matching across multiple symbols.
// Uses the Rust matching engine via CGo when built with cgo tags,
// otherwise falls back to an in-memory Go implementation.
type MatchingService struct {
	engines map[string]*adapter.MatchingEngine
	mu      sync.RWMutex

	// simOrderIDs 记录每个 symbol 最近一轮模拟做市单的引擎订单 id。
	// SimulateTrading 每轮先撤旧单再挂新单——修复旧实现每 3s 叠加 4 张
	// 限价单导致盘口（及 WS 订阅）无限堆积重复展示的问题。
	simOrderIDs   map[string][]uint64
	simOrderIDsMu sync.Mutex

	// orderIDs 是 store 订单号 → 引擎订单号的内部登记簿（C2.2）：
	// OMS 是订单唯一事实源，撮合镜像不再回写展示层（legacy store），
	// 撤单时按登记簿找到引擎单号撤引擎单。
	orderIDs   map[string]uint64
	orderIDsMu sync.Mutex
	orderSeq   uint64

	// engineToStoreIDs 是反向登记簿（C2.3）：symbol → 引擎订单号 → OMS 订单号。
	// 挂单（maker）被后续对手单/模拟做市单撮合成交时，按它找到 OMS 事实源订单
	// 回写 FILLED/均价/数量——否则 paper LIMIT 单成交后 OMS 里仍挂 NEW。
	engineToStoreIDs map[string]map[uint64]string

	balanceProvider adapter.BalanceProvider
}

var (
	matchSvc     *MatchingService
	matchSvcOnce sync.Once
)

// dataFeedHTTPClient has a short timeout so the background simulated-trading
// feed skips a cycle quickly when the network is unreachable.
var dataFeedHTTPClient = &http.Client{Timeout: 3 * time.Second}

// GetMatchingService returns the singleton matching service.
func GetMatchingService() *MatchingService {
	matchSvcOnce.Do(func() {
		matchSvc = &MatchingService{
			engines:          make(map[string]*adapter.MatchingEngine),
			simOrderIDs:      make(map[string][]uint64),
			orderIDs:         make(map[string]uint64),
			engineToStoreIDs: make(map[string]map[uint64]string),
		}
	})
	return matchSvc
}

// SetBalanceProvider 给引擎注入资金校验（生产由 app 上下文接上层账本）。
// 已创建的引擎同步注入；引擎在构建时也会继承（见 GetEngine）。
func (ms *MatchingService) SetBalanceProvider(p adapter.BalanceProvider) {
	ms.mu.Lock()
	ms.balanceProvider = p
	engs := make([]*adapter.MatchingEngine, 0, len(ms.engines))
	for _, e := range ms.engines {
		engs = append(engs, e)
	}
	ms.mu.Unlock()
	for _, e := range engs {
		e.SetBalanceProvider(p)
	}
}

// GetEngine returns or creates an engine for a symbol.
func (ms *MatchingService) GetEngine(symbol string) *adapter.MatchingEngine {
	ms.mu.RLock()
	eng, ok := ms.engines[symbol]
	ms.mu.RUnlock()
	if ok {
		return eng
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	// Double-check
	if eng, ok = ms.engines[symbol]; ok {
		return eng
	}
	eng = adapter.NewMatchingEngine(symbol)
	if ms.balanceProvider != nil {
		eng.SetBalanceProvider(ms.balanceProvider)
	}
	// C2.3：成交回写钩子，挂单成交后回写 OMS 事实源（handler 闭包捕获 symbol）。
	eng.SetOnFill(func(orderID uint64, filledQty, avgPrice float64) {
		ms.handleEngineFill(symbol, orderID, filledQty, avgPrice)
	})
	ms.engines[symbol] = eng
	return eng
}

// handleEngineFill 引擎成交回调：把 maker 侧的累计成交回写 OMS。
// taker 侧在下单当下已由 OMS 提交结果回写（且此刻反向登记簿尚未登记 taker，
// 天然跳过），这里只处理引擎里已登记的挂单。引擎锁内触发，OMS 写库/事件较重，
// 异步化防引擎锁长占与重入死锁；累计量语义 + RecordFill 幂等，乱序收敛。
func (ms *MatchingService) handleEngineFill(symbol string, engineOrderID uint64, filledQty, avgPrice float64) {
	ms.orderIDsMu.Lock()
	storeOrderID := ms.engineToStoreIDs[symbol][engineOrderID]
	ms.orderIDsMu.Unlock()
	if storeOrderID == "" {
		return // 模拟做市单/未登记引擎单：不回写
	}
	om := order.GetOrderManager()
	if om.GetOrder(storeOrderID) == nil {
		return // 非 OMS 订单（内部铸造的 mord- 号）：无事实源可回写
	}
	go func() {
		if err := om.RecordFill(storeOrderID, filledQty, avgPrice); err != nil {
			log.Printf("[Matching] fill write-back to OMS failed: order=%s err=%v", storeOrderID, err)
		}
	}()
}

// PlaceOrder places an order and matches it against the book.
// storeOrderID 为展示层事实源（OMS）订单号；为空时内部铸造 "mord-" 前缀号。
// C2.2：撮合镜像不再回写 legacy store（修复 paper LIMIT 单重复展示），
// store 订单号 → 引擎订单号 的映射由内存登记簿维护，供撤单查找。
func (ms *MatchingService) PlaceOrder(symbol, side, orderType string, price, quantity float64, userID uint64, storeOrderID string) (map[string]any, error) {
	eng := ms.GetEngine(symbol)
	result, err := eng.SubmitOrder(side, orderType, price, quantity, userID)
	if err != nil {
		return nil, err
	}

	engineOrderID, _ := result["order_id"].(uint64)
	if storeOrderID == "" {
		ms.orderIDsMu.Lock()
		ms.orderSeq++
		storeOrderID = fmt.Sprintf("mord-%d-%d", time.Now().UnixMilli(), ms.orderSeq)
		ms.orderIDsMu.Unlock()
	}
	ms.orderIDsMu.Lock()
	ms.orderIDs[storeOrderID] = engineOrderID
	if ms.engineToStoreIDs[symbol] == nil {
		ms.engineToStoreIDs[symbol] = make(map[uint64]string)
	}
	ms.engineToStoreIDs[symbol][engineOrderID] = storeOrderID
	ms.orderIDsMu.Unlock()

	result["store_order_id"] = storeOrderID
	return result, nil
}

// CancelOrder cancels an order by store order ID.
// 引擎里没有该单（重启后登记簿丢失/纯展示层单）时不算错误：
// 继续把展示层事实源置 CANCELLED，找不到记录则容忍。
func (ms *MatchingService) CancelOrder(symbol string, storeOrderID string) error {
	ms.orderIDsMu.Lock()
	engineID, ok := ms.orderIDs[storeOrderID]
	if ok {
		delete(ms.orderIDs, storeOrderID)
		delete(ms.engineToStoreIDs[symbol], engineID)
	}
	ms.orderIDsMu.Unlock()

	if !ok {
		// 兼容修复前落库的镜像记录（engine_order_id 在 legacy store 里）。
		if order := store.GetOrderByID(storeOrderID); order != nil {
			if id, ok2 := order["engine_order_id"].(uint64); ok2 && id > 0 {
				engineID, ok = id, true
			}
		}
	}

	if ok {
		eng := ms.GetEngine(symbol)
		if err := eng.CancelOrder(engineID); err != nil {
			return err
		}
	}

	// 展示层事实源（OMS/xt_orders）同步置 CANCELLED；镜像已不落库，
	// 纯引擎单/已清理记录查不到不算错误。
	_ = store.CancelOrder(storeOrderID)
	return nil
}

// GetOrderBook returns the order book snapshot for a symbol.
func (ms *MatchingService) GetOrderBook(symbol string, depth int) (map[string]any, error) {
	eng := ms.GetEngine(symbol)
	return eng.Snapshot(depth)
}

// SimulateTrading places a fresh set of simulated market-making orders around
// the current price. Previous simulated orders for the symbol are cancelled
// first so the book (and any WS orderbook consumers) shows exactly one set of
// simulated levels instead of accumulating stale duplicates every tick.
func (ms *MatchingService) SimulateTrading(symbol string, price float64) {
	eng := ms.GetEngine(symbol)

	// Cancel the previous round (ignore errors: filled or gone orders are fine).
	ms.simOrderIDsMu.Lock()
	prev := ms.simOrderIDs[symbol]
	ms.simOrderIDsMu.Unlock()
	for _, id := range prev {
		_ = eng.CancelOrder(id)
	}

	go func() {
		// Place simulated buy/sell orders around the current price
		levels := []struct {
			side      string
			offsetPct float64
			qty       float64
		}{
			{"buy", -0.001, 0.1},
			{"buy", -0.002, 0.2},
			{"sell", 0.001, 0.1},
			{"sell", 0.002, 0.2},
		}

		var newIDs []uint64
		for _, level := range levels {
			px := price * (1 + level.offsetPct)
			res, err := eng.SubmitOrder(level.side, "limit", px, level.qty, 0)
			if err != nil {
				log.Printf("[Matching] Simulated order error for %s: %v", symbol, err)
				continue
			}
			if id, ok := res["order_id"].(uint64); ok {
				newIDs = append(newIDs, id)
			}
		}
		ms.simOrderIDsMu.Lock()
		ms.simOrderIDs[symbol] = newIDs
		ms.simOrderIDsMu.Unlock()
	}()
}

// StartDataFeed begins periodic simulated trading for configured symbols.
// Prices are fetched from Binance public API; hard-coded fake prices are no
// longer used. Symbols without a real price are skipped.
func (ms *MatchingService) StartDataFeed() {
	symbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
	lastPrices := make(map[string]float64)
	var priceMu sync.Mutex

	ticker := time.NewTicker(3 * time.Second)
	go func() {
		for range ticker.C {
			for _, sym := range symbols {
				price := fetchLastPrice(sym)
				if price <= 0 {
					// Fallback to last known price if available.
					priceMu.Lock()
					price = lastPrices[sym]
					priceMu.Unlock()
					if price <= 0 {
						log.Printf("[Matching] No price available for %s, skipping simulation", sym)
						continue
					}
				}
				priceMu.Lock()
				lastPrices[sym] = price
				priceMu.Unlock()
				ms.SimulateTrading(sym, price)
			}
		}
	}()
}

// fetchLastPrice retrieves the latest price from Binance public API.
// Short timeout so a dead network skips a cycle instead of wedging the feed.
func fetchLastPrice(symbol string) float64 {
	resp, err := dataFeedHTTPClient.Get("https://api.binance.com/api/v3/ticker/price?symbol=" + symbol)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return 0
	}
	if priceStr, ok := result["price"].(string); ok {
		f, _ := strconv.ParseFloat(priceStr, 64)
		return f
	}
	return 0
}
