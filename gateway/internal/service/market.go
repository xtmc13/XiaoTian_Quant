package service

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// MarketDataService provides real-time and historical market data.
type MarketDataService struct {
	httpClient *http.Client
	cache      map[string]*CachedKlines
	mu         sync.RWMutex
}

type CachedKlines struct {
	Symbol   string
	Interval string
	Data     []map[string]any
	Fetched  time.Time
}

var (
	marketSvc     *MarketDataService
	marketSvcOnce sync.Once
)

// GetMarketService returns the singleton market data service.
func GetMarketService() *MarketDataService {
	marketSvcOnce.Do(func() {
		marketSvc = &MarketDataService{
			httpClient: &http.Client{Timeout: 30 * time.Second},
			cache:      make(map[string]*CachedKlines),
		}
	})
	return marketSvc
}

// FetchKlines fetches OHLCV data from Binance with caching.
func (m *MarketDataService) FetchKlines(symbol, interval string, limit int) ([]map[string]any, error) {
	cacheKey := fmt.Sprintf("%s:%s:%d", symbol, interval, limit)

	m.mu.RLock()
	cached, ok := m.cache[cacheKey]
	m.mu.RUnlock()

	// Cache for 30 seconds
	if ok && time.Since(cached.Fetched) < 30*time.Second {
		return cached.Data, nil
	}

	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", interval)
	params.Set("limit", fmt.Sprintf("%d", limit))

	u, _ := url.Parse("https://api.binance.com/api/v3/klines")
	u.RawQuery = params.Encode()

	resp, err := m.httpClient.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var raw [][]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	klines := make([]map[string]any, 0, len(raw))
	for _, k := range raw {
		if len(k) >= 6 {
			klines = append(klines, map[string]any{
				"time":   int64(toFloat(k[0])),
				"open":   toFloat(k[1]),
				"high":   toFloat(k[2]),
				"low":    toFloat(k[3]),
				"close":  toFloat(k[4]),
				"volume": toFloat(k[5]),
			})
		}
	}

	m.mu.Lock()
	m.cache[cacheKey] = &CachedKlines{
		Symbol: symbol, Interval: interval,
		Data: klines, Fetched: time.Now(),
	}
	m.mu.Unlock()

	return klines, nil
}

// GetTicker fetches 24hr ticker for a symbol.
func (m *MarketDataService) GetTicker(symbol string) (map[string]any, error) {
	u := fmt.Sprintf("https://api.binance.com/api/v3/ticker/24hr?symbol=%s", symbol)
	resp, err := m.httpClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var ticker map[string]any
	json.Unmarshal(body, &ticker)
	return ticker, nil
}

// GetMultipleTickers fetches tickers for multiple symbols.
func (m *MarketDataService) GetMultipleTickers(symbols []string) ([]map[string]any, error) {
	var results []map[string]any
	for _, sym := range symbols {
		ticker, err := m.GetTicker(sym)
		if err != nil {
			log.Printf("[Market] Ticker error for %s: %v", sym, err)
			continue
		}
		results = append(results, ticker)
	}
	return results, nil
}

// ComputeIndicators computes basic technical indicators from klines.
func (m *MarketDataService) ComputeIndicators(klines []map[string]any) map[string]float64 {
	if len(klines) < 20 {
		return nil
	}

	closes := make([]float64, len(klines))
	for i, k := range klines {
		closes[i] = k["close"].(float64)
	}

	indicators := make(map[string]float64)

	// MA5, MA20, MA60
	indicators["ma5"] = average(closes[len(closes)-5:])
	if len(closes) >= 20 {
		indicators["ma20"] = average(closes[len(closes)-20:])
	}
	if len(closes) >= 60 {
		indicators["ma60"] = average(closes[len(closes)-60:])
	}

	// RSI 14
	indicators["rsi"] = computeRSI(closes, 14)

	// ATR 14
	indicators["atr"] = computeATR(klines, 14)

	// Bollinger Bands
	if len(closes) >= 20 {
		ma20 := indicators["ma20"]
		stddev := standardDeviation(closes[len(closes)-20:], ma20)
		indicators["bb_upper"] = ma20 + 2*stddev
		indicators["bb_mid"] = ma20
		indicators["bb_lower"] = ma20 - 2*stddev
		lastPrice := closes[len(closes)-1]
		if stddev > 0 {
			indicators["bb_position"] = (lastPrice - ma20) / (2 * stddev)
		}
	}

	return indicators
}

// SubscribeSymbols starts real-time ticker polling.
func (m *MarketDataService) SubscribeSymbols(symbols []string) {
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for range ticker.C {
			for _, sym := range symbols {
				ticker, err := m.GetTicker(sym)
				if err != nil {
					continue
				}
				// Update store cache
				price := toFloat(ticker["lastPrice"])
				_ = price
			}
		}
	}()
}

// ── Helpers ──

func toFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	}
	return 0
}

func average(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func computeRSI(closes []float64, period int) float64 {
	if len(closes) < period+1 {
		return 50
	}
	gains, losses := 0.0, 0.0
	for i := len(closes) - period; i < len(closes); i++ {
		diff := closes[i] - closes[i-1]
		if diff > 0 {
			gains += diff
		} else {
			losses -= diff
		}
	}
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

func computeATR(klines []map[string]any, period int) float64 {
	if len(klines) < period+1 {
		return 0
	}
	trSum := 0.0
	recentKlines := klines[len(klines)-period:]
	for i, k := range recentKlines {
		high := k["high"].(float64)
		low := k["low"].(float64)
		var prevClose float64
		if i > 0 {
			prevClose = recentKlines[i-1]["close"].(float64)
		} else {
			prevClose = recentKlines[0]["close"].(float64)
		}
		tr := max(high-low, max(abs(high-prevClose), abs(low-prevClose)))
		trSum += tr
	}
	return trSum / float64(period)
}

func standardDeviation(vals []float64, mean float64) float64 {
	sumSq := 0.0
	for _, v := range vals {
		diff := v - mean
		sumSq += diff * diff
	}
	variance := sumSq / float64(len(vals))
	return sqrt(variance)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func sqrt(x float64) float64 {
	// Newton's method
	if x <= 0 {
		return 0
	}
	r := x
	for i := 0; i < 20; i++ {
		r = (r + x/r) / 2
	}
	return r
}

