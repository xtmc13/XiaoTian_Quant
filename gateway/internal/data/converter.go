package data

import (
	"fmt"
	"math"
	"sort"
)

// ── Timeframe Conversion ──────────────────────────────────────

// intervalToMs converts interval string to milliseconds.
func intervalToMs(interval string) int64 {
	switch interval {
	case "1m":
		return 60000
	case "3m":
		return 180000
	case "5m":
		return 300000
	case "15m":
		return 900000
	case "30m":
		return 1800000
	case "1h":
		return 3600000
	case "4h":
		return 14400000
	case "1d":
		return 86400000
	case "1w":
		return 604800000
	default:
		return 3600000
	}
}

// ConvertTimeframe resamples OHLCV bars from one interval to another.
// Only supports converting to LARGER intervals (e.g., 1m → 5m, 1h → 4h).
func ConvertTimeframe(bars []OHLCV, targetInterval string) ([]OHLCV, error) {
	if len(bars) == 0 {
		return nil, nil
	}

	sourceInterval := bars[0].Interval
	sourceMs := intervalToMs(sourceInterval)
	targetMs := intervalToMs(targetInterval)

	if targetMs < sourceMs {
		return nil, fmt.Errorf("target interval %s is smaller than source %s — upsampling not supported", targetInterval, sourceInterval)
	}
	if targetMs%sourceMs != 0 {
		return nil, fmt.Errorf("target interval %s is not a multiple of source %s", targetInterval, sourceInterval)
	}

	ratio := int(targetMs / sourceMs)
	grouped := make(map[int64][]OHLCV)
	for _, bar := range bars {
		bucket := (bar.Time / targetMs) * targetMs
		grouped[bucket] = append(grouped[bucket], bar)
	}

	// Sort bucket keys
	keys := make([]int64, 0, len(grouped))
	for k := range grouped {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	result := make([]OHLCV, 0, len(keys))
	for _, bucketTime := range keys {
		group := grouped[bucketTime]
		if len(group) < ratio {
			// Incomplete bucket — skip (or keep if we want partial bars)
			// For strict conversion, skip partial buckets
			if len(group) < ratio/2 {
				continue
			}
		}

		bar := OHLCV{
			Symbol:   bars[0].Symbol,
			Interval: targetInterval,
			Time:     bucketTime,
			Open:     group[0].Open,
			Close:    group[len(group)-1].Close,
			High:     group[0].High,
			Low:      group[0].Low,
			Volume:   0,
		}

		for _, b := range group {
			if b.High > bar.High {
				bar.High = b.High
			}
			if b.Low < bar.Low {
				bar.Low = b.Low
			}
			bar.Volume += b.Volume
		}

		result = append(result, bar)
	}

	return result, nil
}

// ── Trade → OHLCV Conversion ──────────────────────────────────────

// TradeTick represents a single trade (for trade-to-OHLCV conversion).
type TradeTick struct {
	Symbol    string
	Price     float64
	Quantity  float64
	Timestamp int64 // unix milliseconds
}

// TradesToOHLCV converts a list of trades into OHLCV bars at the given interval.
func TradesToOHLCV(trades []TradeTick, interval string) []OHLCV {
	if len(trades) == 0 {
		return nil
	}

	intervalMs := intervalToMs(interval)
	buckets := make(map[int64]*OHLCV)
	var bucketKeys []int64

	for _, t := range trades {
		bucket := (t.Timestamp / intervalMs) * intervalMs
		bar, exists := buckets[bucket]
		if !exists {
			bucketKeys = append(bucketKeys, bucket)
			bar = &OHLCV{
				Symbol:   t.Symbol,
				Interval: interval,
				Time:     bucket,
				Open:     t.Price,
				High:     t.Price,
				Low:      t.Price,
				Close:    t.Price,
			}
			buckets[bucket] = bar
		}
		bar.Close = t.Price
		if t.Price > bar.High {
			bar.High = t.Price
		}
		if t.Price < bar.Low {
			bar.Low = t.Price
		}
		bar.Volume += t.Quantity
	}

	sort.Slice(bucketKeys, func(i, j int) bool { return bucketKeys[i] < bucketKeys[j] })

	result := make([]OHLCV, len(bucketKeys))
	for i, key := range bucketKeys {
		result[i] = *buckets[key]
	}
	return result
}

// ── Utility ────────────────────────────────────────────────────

// BarDuration returns a human-readable duration for an interval.
func BarDuration(interval string) string {
	ms := intervalToMs(interval)
	hours := ms / 3600000
	if hours >= 24 {
		days := hours / 24
		return fmt.Sprintf("%d天", days)
	}
	if hours >= 1 {
		return fmt.Sprintf("%d小时", hours)
	}
	minutes := ms / 60000
	return fmt.Sprintf("%d分钟", minutes)
}

// AlignToBarBoundary aligns a timestamp to the start of its bar.
func AlignToBarBoundary(ts int64, interval string) int64 {
	ms := intervalToMs(interval)
	return (ts / ms) * ms
}

// RoundFloat rounds a float to the specified number of decimal places.
func RoundFloat(v float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(v*p) / p
}
