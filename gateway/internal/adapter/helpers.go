package adapter

import (
	"fmt"
	"strings"
)

// stubError returns a friendly error indicating that the exchange adapter
// is a placeholder and real trading is not yet implemented.
func stubError(exchangeName, method string) error {
	return fmt.Errorf(
		"%s adapter is a stub (%s): real trading not yet implemented. "+
		"Only Binance/OKX/Bybit have live trading support.",
		exchangeName, method,
	)
}

// parseFloat converts a string or float64 value to float64.
func parseFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	default:
		var f float64
		fmt.Sscanf(fmt.Sprint(v), "%f", &f)
		return f
	}
}

// toOKXInstID converts a standard symbol like "BTCUSDT" to OKX format "BTC-USDT".
func toOKXInstID(symbol string) string {
	if strings.Contains(symbol, "-") {
		return symbol
	}
	if len(symbol) > 4 && (strings.HasSuffix(symbol, "USDT") || strings.HasSuffix(symbol, "USDC")) {
		idx := len(symbol) - 4
		if strings.HasSuffix(symbol, "USDT") {
			idx = len(symbol) - 4
		}
		return symbol[:idx] + "-" + symbol[idx:]
	}
	// Generic fallback: split at middle for known quote currencies
	for _, quote := range []string{"USDT", "USDC", "BTC", "ETH", "BNB"} {
		if strings.HasSuffix(symbol, quote) && len(symbol) > len(quote) {
			idx := len(symbol) - len(quote)
			return symbol[:idx] + "-" + symbol[idx:]
		}
	}
	return symbol
}

// fromOKXInstID converts OKX format "BTC-USDT" back to "BTCUSDT".
func fromOKXInstID(instID string) string {
	return strings.Replace(instID, "-", "", 1)
}

// parseFloatSafe converts a value to float64 safely.
func parseFloatSafe(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	default:
		var f float64
		fmt.Sscanf(fmt.Sprint(v), "%f", &f)
		return f
	}
}

// getString extracts a string value from a map with a default fallback.
func getString(m map[string]any, key string, defaultVal string) string {
	if v, ok := m[key]; ok {
		switch s := v.(type) {
		case string:
			return s
		default:
			return fmt.Sprint(v)
		}
	}
	return defaultVal
}
