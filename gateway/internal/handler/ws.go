package handler

import (
	"encoding/json"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/xiaotian-quant/gateway/internal/exchange"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/service"
	"github.com/xiaotian-quant/gateway/internal/store"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func init() {
	// Wire real Binance prices to frontend WS feed
	exchange.FrontendPriceFeed = UpdateRealPrice
}

var (
	pricesMu     sync.RWMutex
	prices       = map[string]float64{}   // symbol → last price (real data only)
	priceOHLCV   = map[string]*ohlcvCache{} // symbol → recent OHLCV tracking
	realPriceFed = map[string]bool{}       // true = price set externally (real exchange)
)

type ohlcvCache struct {
	high  float64
	low   float64
	vol   float64
	open  float64
	resetAt int64 // unix second for daily reset
}

// UpdateRealPrice sets a real market price from external feed (e.g., BinanceWSStream).
// It also tracks high/low/volume from tick data.
func UpdateRealPrice(symbol string, price float64) {
	pricesMu.Lock()
	defer pricesMu.Unlock()

	prev, existed := prices[symbol]
	prices[symbol] = price
	realPriceFed[symbol] = true

	nowSec := time.Now().Unix()
	cache, ok := priceOHLCV[symbol]
	if !ok || cache.resetAt < nowSec-86400 {
		cache = &ohlcvCache{high: price, low: price, vol: 0, open: price, resetAt: nowSec}
		priceOHLCV[symbol] = cache
	}
	if price > cache.high {
		cache.high = price
	}
	if price < cache.low {
		cache.low = price
	}
	if existed {
		cache.vol += math.Abs(price-prev) / prev * 100 // approx volume proxy
	}
}

func WSHandler(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	tick := 0

	// Fallback price feed: if the real-time exchange WebSocket has not pushed
	// prices for a symbol, poll Binance REST API every second to keep data real.
	symbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
	pricePollStop := make(chan struct{})
	go pollRealPrices(symbols, pricePollStop)
	defer close(pricePollStop)

	// Read messages in a goroutine (to detect disconnect)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			tick++

			// Send status (real portfolio data only, no random)
			statusMsg := buildStatusMsg(tick)
			if err := conn.WriteMessage(websocket.TextMessage, statusMsg); err != nil {
				return
			}

			// Send prices only for symbols with real exchange data
			pricesMu.RLock()
			var realSymbols []string
			for sym := range realPriceFed {
				if realPriceFed[sym] {
					realSymbols = append(realSymbols, sym)
				}
			}
			pricesMu.RUnlock()

			for _, sym := range realSymbols {
				priceMsg := buildPriceMsg(sym)
				if priceMsg == nil {
					continue
				}
				if err := conn.WriteMessage(websocket.TextMessage, priceMsg); err != nil {
					return
				}
			}

			// Orderbook: only if real prices available
			if tick%2 == 0 && len(realSymbols) > 0 {
				obMsg := buildOrderbookMsg(realSymbols[0])
				if obMsg != nil {
					if err := conn.WriteMessage(websocket.TextMessage, obMsg); err != nil {
						return
					}
				}
			}

			// Trades: fetch from store (real trades), every 3 ticks
			if tick%3 == 0 {
				tradesMsg := buildTradesMsg()
				if tradesMsg != nil {
					if err := conn.WriteMessage(websocket.TextMessage, tradesMsg); err != nil {
						return
					}
				}
			}
		}
	}
}

// pollRealPrices polls Binance REST API and feeds prices into UpdateRealPrice
// when the live WebSocket feed is not active for a symbol.
func pollRealPrices(symbols []string, stop chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			for _, sym := range symbols {
				pricesMu.RLock()
				_, fed := realPriceFed[sym]
				pricesMu.RUnlock()
				if fed {
					continue // live feed is active, don't overwrite
				}
				price := fetchBinancePriceWS(sym)
				if price > 0 {
					UpdateRealPrice(sym, price)
				}
			}
		}
	}
}

// fetchBinancePriceWS retrieves the latest price from Binance public API.
func fetchBinancePriceWS(symbol string) float64 {
	resp, err := http.Get("https://api.binance.com/api/v3/ticker/price?symbol=" + symbol)
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

func buildStatusMsg(tick int) []byte {
	var equity float64
	var availableBalance float64
	var marginUsed float64
	var curve []map[string]any
	var dailyOrders int
	var drawdownPct float64

	// Try real portfolio data
	if mgr := portfolio.GetManager(); mgr != nil {
		equity = mgr.TotalEquity()
		availableBalance = mgr.AvailableBalance()
		marginUsed = mgr.MarginUsed()
		drawdownPct = mgr.Drawdown()

		// Build real equity curve from snapshots (last 60)
		snapshots := mgr.GetSnapshots()
		start := 0
		if len(snapshots) > 60 {
			start = len(snapshots) - 60
		}
		for _, s := range snapshots[start:] {
			curve = append(curve, map[string]any{
				"time":   s.Timestamp,
				"equity": roundTo(s.TotalEquity, 2),
			})
		}

		// Count real positions
		positions := mgr.GetPositions()
		dailyOrders = len(positions)
	}

	// Fallback: return zero/empty values, not random fake data
	if curve == nil {
		curve = []map[string]any{}
	}

	msg, _ := json.Marshal(map[string]any{
		"type": "status",
		"data": map[string]any{
			"runtime_seconds": tick,
			"portfolio": map[string]any{
				"total_equity":      roundTo(equity, 2),
				"available_balance": roundTo(availableBalance, 2),
				"margin_used":       roundTo(marginUsed, 2),
			},
			"risk": map[string]any{
				"current_drawdown_pct": roundTo(drawdownPct, 2),
				"daily_orders":         dailyOrders,
				"consecutive_losses":   0,
			},
			"equity_curve": curve,
			"strategies":   map[string]any{},
		},
	})
	return msg
}

func buildPriceMsg(sym string) []byte {
	pricesMu.RLock()
	current, hasPrice := prices[sym]
	isReal := realPriceFed[sym]
	cache := priceOHLCV[sym]
	pricesMu.RUnlock()

	// Only push if we have real exchange data
	if !isReal || !hasPrice || current <= 0 {
		return nil
	}

	high := current
	low := current
	vol := 0.0
	open := current

	if cache != nil {
		high = cache.high
		low = cache.low
		vol = cache.vol
		open = cache.open
	}

	change := 0.0
	if open > 0 {
		change = (current - open) / open * 100
	}

	msg, _ := json.Marshal(map[string]any{
		"type":   "price",
		"symbol": sym,
		"data": map[string]any{
			"last":       roundTo(current, 6),
			"price":      roundTo(current, 6),
			"change_pct": roundTo(change, 4),
			"high":       roundTo(high, 6),
			"low":        roundTo(low, 6),
			"volume":     roundTo(vol, 2),
			"open":       roundTo(open, 6),
		},
	})
	return msg
}

func buildOrderbookMsg(sym string) []byte {
	pricesMu.RLock()
	mid, hasPrice := prices[sym]
	isReal := realPriceFed[sym]
	pricesMu.RUnlock()

	// Only push if real price available
	if !isReal || !hasPrice || mid <= 0 {
		return nil
	}

	// Try to use the matching engine order book first.
	if snap, err := service.GetMatchingService().GetOrderBook(sym, 15); err == nil {
		if bidsRaw, ok := snap["bids"].([][]any); ok && len(bidsRaw) > 0 {
			asksRaw, _ := snap["asks"].([][]any)
			msg, _ := json.Marshal(map[string]any{
				"type":   "orderbook",
				"symbol": sym,
				"data": map[string]any{
					"symbol": sym,
					"bids":   bidsRaw,
					"asks":   asksRaw,
					"source": "matching_engine",
				},
			})
			return msg
		}
	}

	// No real order book available: generate a synthetic book with zero
	// quantities and clearly mark the source so the UI can warn the user.
	bids := make([][]any, 15)
	asks := make([][]any, 15)
	baseSpread := mid * 0.0001 // 0.01% base spread
	for i := 0; i < 15; i++ {
		spread := baseSpread * float64(i+1)
		bids[i] = []any{roundTo(mid-spread, 6), float64(0)}
		asks[i] = []any{roundTo(mid+spread, 6), float64(0)}
	}
	msg, _ := json.Marshal(map[string]any{
		"type": "orderbook",
		"data": map[string]any{
			"symbol": sym,
			"bids":   bids,
			"asks":   asks,
			"source": "synthetic",
		},
	})
	return msg
}

func buildTradesMsg() []byte {
	// Fetch real trades from store (last 10 filled orders)
	orders := store.GetOrders("")
	if len(orders) == 0 {
		return nil
	}

	trades := make([]map[string]any, 0)
	count := 0
	// Iterate in reverse to get most recent
	for i := len(orders) - 1; i >= 0 && count < 10; i-- {
		o := orders[i]
		status := getString(o, "status", "")
		if status != "FILLED" {
			continue
		}
		trades = append(trades, map[string]any{
			"symbol": getString(o, "symbol", ""),
			"side":   strings.ToUpper(getString(o, "side", "")),
			"price":  getFloat(o, "price", 0),
			"qty":    getFloat(o, "quantity", 0),
			"time":   int64(getFloat(o, "created_at", float64(time.Now().UnixMilli()))),
		})
		count++
	}

	if len(trades) == 0 {
		return nil
	}

	msg, _ := json.Marshal(map[string]any{
		"type":   "trades",
		"data":   trades,
		"source": "store",
	})
	return msg
}

func roundTo(v float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(v*p) / p
}
