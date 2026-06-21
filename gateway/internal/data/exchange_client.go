package data

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/xiaotian-quant/gateway/internal/adapter"
)

// KlineClient defines a common interface for fetching historical OHLCV data.
type KlineClient interface {
	// GetKlinesRange fetches OHLCV bars in the given time range.
	// startMs and endMs are unix milliseconds. limit caps bars per request.
	GetKlinesRange(symbol, interval string, startMs, endMs int64, limit int) ([]OHLCV, error)
	// Name returns the exchange name.
	Name() string
}

// clientFactory creates a KlineClient for the given exchange.
// API credentials are read from environment variables.
func clientFactory(exchange string) (KlineClient, error) {
	switch exchange {
	case "binance":
		apiKey := os.Getenv("BINANCE_API_KEY")
		secret := os.Getenv("BINANCE_API_SECRET")
		testnet := os.Getenv("BINANCE_TESTNET") == "true"
		return &binanceClient{adapter.NewBinanceAdapter(apiKey, secret, testnet)}, nil
	default:
		return nil, fmt.Errorf("data download for exchange %s is not yet implemented; only binance is supported", exchange)
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
