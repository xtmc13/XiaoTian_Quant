package data

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/xiaotian-quant/gateway/internal/adapter"
	"github.com/xiaotian-quant/gateway/internal/ccxt"
)

// KlineClient defines a common interface for fetching historical OHLCV data.
type KlineClient interface {
	// GetKlinesRange fetches OHLCV bars in the given time range.
	// startMs and endMs are unix milliseconds. limit caps bars per request.
	GetKlinesRange(symbol, interval string, startMs, endMs int64, limit int) ([]OHLCV, error)
	// Name returns the exchange name.
	Name() string
}

// nativeExchanges lists exchanges with native Go adapters.
var nativeExchanges = map[string]bool{
	"binance":  true,
	"okx":      true,
	"bybit":    true,
	"gateio":   true,
	"mexc":     true,
	"bitget":   true,
	"coinbase": true,
	"kraken":   true,
	"alpaca":   true,
}

// clientFactory creates a KlineClient for the given exchange.
// API credentials are read from environment variables.
// If the exchange has no native adapter, it falls back to the CCXT bridge when available.
func clientFactory(exchange string) (KlineClient, error) {
	switch exchange {
	case "binance":
		apiKey := os.Getenv("BINANCE_API_KEY")
		secret := os.Getenv("BINANCE_API_SECRET")
		testnet := os.Getenv("BINANCE_TESTNET") == "true"
		return &binanceClient{adapter.NewBinanceAdapter(apiKey, secret, testnet)}, nil
	default:
		// Try CCXT bridge for unsupported exchanges.
		bridge := ccxt.NewClient("")
		if !bridge.IsAvailable() {
			return nil, fmt.Errorf("data download for exchange %s is not yet implemented natively and CCXT bridge is unavailable", exchange)
		}
		return &ccxtKlineClient{bridge, exchange}, nil
	}
}

// binanceClient wraps BinanceAdapter as a KlineClient.
type binanceClient struct {
	*adapter.BinanceAdapter
}

func (c *binanceClient) Name() string { return "binance" }

func (c *binanceClient) GetKlinesRange(symbol, interval string, startMs, endMs int64, limit int) ([]OHLCV, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := c.BinanceAdapter.GetKlinesRange(symbol, interval, startMs, endMs, limit)
	if err != nil {
		return nil, err
	}
	return parseStandardKlines(rows, symbol, interval), nil
}

// ccxtKlineClient wraps the CCXT bridge as a KlineClient.
type ccxtKlineClient struct {
	client   *ccxt.Client
	exchange string
}

func (c *ccxtKlineClient) Name() string { return c.exchange }

func (c *ccxtKlineClient) GetKlinesRange(symbol, interval string, startMs, endMs int64, limit int) ([]OHLCV, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	ccxtSymbol := toCCXTSymbol(symbol)
	ccxtTimeframe := toCCXTTimeframe(interval)
	bars, err := c.client.FetchOHLCV(c.exchange, ccxtSymbol, ccxtTimeframe, limit, startMs)
	if err != nil {
		return nil, err
	}
	result := make([]OHLCV, 0, len(bars))
	for _, b := range bars {
		if endMs > 0 && b.Time > endMs {
			continue
		}
		result = append(result, OHLCV{
			Symbol:   symbol,
			Interval: interval,
			Time:     b.Time,
			Open:     b.Open,
			High:     b.High,
			Low:      b.Low,
			Close:    b.Close,
			Volume:   b.Volume,
		})
	}
	return result, nil
}

// toCCXTSymbol converts "BTCUSDT" to "BTC/USDT" for CCXT.
func toCCXTSymbol(symbol string) string {
	// Common stablecoins and quote currencies.
	quotes := []string{"USDT", "USDC", "BUSD", "BTC", "ETH", "USD", "EUR", "GBP", "JPY", "KRW"}
	upper := strings.ToUpper(symbol)
	for _, q := range quotes {
		if strings.HasSuffix(upper, q) && len(upper) > len(q) {
			return upper[:len(upper)-len(q)] + "/" + q
		}
	}
	// Fallback: if no known quote found, leave as-is and hope CCXT recognizes it.
	return symbol
}

// toCCXTTimeframe ensures interval is CCXT-compatible (e.g. "1h").
func toCCXTTimeframe(interval string) string {
	if interval == "" {
		return "1h"
	}
	return interval
}

// parseStandardKlines converts [][]any with [time, open, high, low, close, volume] to []OHLCV.
func parseStandardKlines(rows [][]any, symbol, interval string) []OHLCV {
	bars := make([]OHLCV, 0, len(rows))
	for _, r := range rows {
		if len(r) < 6 {
			continue
		}
		t := parseInt(r[0])
		if t == 0 {
			continue
		}
		bars = append(bars, OHLCV{
			Symbol:   symbol,
			Interval: interval,
			Time:     t,
			Open:     parseFloat(r[1]),
			High:     parseFloat(r[2]),
			Low:      parseFloat(r[3]),
			Close:    parseFloat(r[4]),
			Volume:   parseFloat(r[5]),
		})
	}
	return bars
}

func parseInt(v any) int64 {
	switch val := v.(type) {
	case float64:
		return int64(val)
	case int64:
		return val
	case int:
		return int64(val)
	case string:
		i, _ := strconv.ParseInt(val, 10, 64)
		return i
	}
	return 0
}

// parseDate parses "YYYY-MM-DD" to unix millis.
func parseDate(s string) int64 {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}
