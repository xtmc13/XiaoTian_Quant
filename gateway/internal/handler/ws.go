package handler

import (
	"encoding/json"
	"log"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/exchange"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func init() {
	// Wire real Binance prices to frontend WS feed
	exchange.FrontendPriceFeed = UpdateRealPrice
}

var (
	basePrices = map[string]float64{
		"BTCUSDT": 68000.0,
		"ETHUSDT": 3500.0,
		"BNBUSDT": 600.0,
	}
	pricesMu     sync.RWMutex
	prices       = map[string]float64{"BTCUSDT": 68000.0, "ETHUSDT": 3500.0, "BNBUSDT": 600.0}
	realPriceFed = map[string]bool{} // true = price set externally (real Binance)
)

// UpdatePrice sets a real market price from external feed (e.g., BinanceWSStream).
// Once set, ws.go stops random-walking for that symbol.
func UpdateRealPrice(symbol string, price float64) {
	pricesMu.Lock()
	prices[symbol] = price
	realPriceFed[symbol] = true
	pricesMu.Unlock()
}

func WSHandler(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	symbols := []string{"BTCUSDT", "ETHUSDT", "BNBUSDT"}
	tick := 0

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
			// Send status
			statusMsg := buildStatusMsg(tick, rng)
			if err := conn.WriteMessage(websocket.TextMessage, statusMsg); err != nil {
				return
			}

			// Send prices for each symbol
			for _, sym := range symbols {
				priceMsg := buildPriceMsg(sym, rng)
				if err := conn.WriteMessage(websocket.TextMessage, priceMsg); err != nil {
					return
				}
			}

			// Orderbook every 2 ticks
			if tick%2 == 0 {
				obMsg := buildOrderbookMsg("BTCUSDT", rng)
				if err := conn.WriteMessage(websocket.TextMessage, obMsg); err != nil {
					return
				}
			}

			// Trades every 3 ticks
			if tick%3 == 0 {
				tradesMsg := buildTradesMsg(symbols, rng)
				if err := conn.WriteMessage(websocket.TextMessage, tradesMsg); err != nil {
					return
				}
			}
		}
	}
}

func buildStatusMsg(tick int, rng *rand.Rand) []byte {
	// Try real portfolio data first
	equity := 100000.0
	if mgr := portfolio.GetManager(); mgr != nil {
		if acct := mgr.GetAccount("default"); acct != nil {
			for _, bal := range acct.Balances {
				equity += bal.Total
			}
			if b, ok := acct.Balances["USDT"]; ok {
				equity = b.Total
			}
		}
	}
	// Fallback random if no real data
	if equity == 0 {
		equity = 100000.0 + rng.Float64()*7000 - 2000
	}

	curve := make([]map[string]any, 0)
	for i := 0; i < 60; i++ {
		val := equity * (0.95 + float64(i)*0.001 + rng.Float64()*0.02)
		curve = append(curve, map[string]any{
			"time":   time.Now().Unix() - 60 + int64(i),
			"equity": roundTo(val, 2),
		})
	}
	msg, _ := json.Marshal(map[string]any{
		"type": "status",
		"data": map[string]any{
			"runtime_seconds": tick,
			"portfolio": map[string]any{
				"total_equity":      roundTo(equity, 2),
				"available_balance": roundTo(equity*0.8, 2),
				"margin_used":       roundTo(equity*0.2, 2),
			},
			"risk": map[string]any{
				"current_drawdown_pct":  roundTo(rng.Float64()*4.5+0.5, 2),
				"daily_orders":          rng.Intn(28) + 3,
				"consecutive_losses":    rng.Intn(3),
			},
			"equity_curve": curve,
			"strategies":   map[string]any{},
		},
	})
	return msg
}

func buildPriceMsg(sym string, rng *rand.Rand) []byte {
	pricesMu.RLock()
	base := basePrices[sym]
	current := prices[sym]
	isReal := realPriceFed[sym]
	pricesMu.RUnlock()

	if !isReal {
		// Simulated random walk (fallback)
		current = math.Max(base*0.9, math.Min(base*1.1, current+rng.NormFloat64()*base*0.002))
		pricesMu.Lock()
		prices[sym] = current
		pricesMu.Unlock()
	}

	change := (current - base) / base * 100
	msg, _ := json.Marshal(map[string]any{
		"type":   "price",
		"symbol": sym,
		"data": map[string]any{
			"last":       roundTo(current, 2),
			"price":      roundTo(current, 2),
			"change_pct": roundTo(change, 4),
			"high":       roundTo(current*(1+math.Abs(rng.NormFloat64()*0.005)), 2),
			"low":        roundTo(current*(1-math.Abs(rng.NormFloat64()*0.005)), 2),
			"volume":     roundTo(rng.Float64()*4900+100, 1),
		},
	})
	return msg
}

func buildOrderbookMsg(sym string, rng *rand.Rand) []byte {
	pricesMu.Lock()
	mid := prices[sym]
	pricesMu.Unlock()

	bids := make([][]any, 15)
	asks := make([][]any, 15)
	for i := 0; i < 15; i++ {
		bidPrice := mid * (1 - float64(i+1)*0.0008)
		askPrice := mid * (1 + float64(i+1)*0.0008)
		bids[i] = []any{roundTo(bidPrice, 2), roundTo(rng.Float64()*4.9+0.1, 4)}
		asks[i] = []any{roundTo(askPrice, 2), roundTo(rng.Float64()*4.9+0.1, 4)}
	}
	msg, _ := json.Marshal(map[string]any{
		"type": "orderbook",
		"data": map[string]any{
			"symbol": sym,
			"bids":   bids,
			"asks":   asks,
		},
	})
	return msg
}

func buildTradesMsg(symbols []string, rng *rand.Rand) []byte {
	trades := make([]map[string]any, 10)
	for i := 0; i < 10; i++ {
		sym := symbols[rng.Intn(len(symbols))]
		side := []string{"BUY", "SELL"}[rng.Intn(2)]
		pricesMu.Lock()
		price := prices[sym] * (1 + (rng.Float64()-0.5)*0.004)
		pricesMu.Unlock()
		trades[i] = map[string]any{
			"symbol": sym,
			"side":   side,
			"price":  roundTo(price, 2),
			"qty":    roundTo(rng.Float64()*1.999+0.001, 4),
			"time":   time.Now().UnixMilli(),
		}
	}
	msg, _ := json.Marshal(map[string]any{
		"type": "trades",
		"data": trades,
	})
	return msg
}

func roundTo(v float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(v*p) / p
}
