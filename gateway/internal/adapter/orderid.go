package adapter

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

// ExchangeClientOID derives a stable, venue-safe client order id from the
// gateway order id. The same order therefore maps to the same id on every
// retry, which is what makes the exchange deduplicate a re-sent order.
// Format: "xt" + first 30 hex chars of SHA-1 (32 chars total, within
// Binance's 36, OKX's 32 and Bybit's 36 character limits).
func ExchangeClientOID(orderID string) string {
	trimmed := strings.TrimSpace(orderID)
	if trimmed == "" {
		return ""
	}
	sum := sha1.Sum([]byte(trimmed))
	return "xt" + hex.EncodeToString(sum[:])[:30]
}

// firstClientOrderID returns the first non-empty optional client id.
func firstClientOrderID(ids []string) string {
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id)
		}
	}
	return ""
}
