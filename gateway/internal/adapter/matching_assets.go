package adapter

import (
	"fmt"
	"math"
	"strings"
)

var quoteAssets = []string{"USDT", "USDC", "FDUSD", "TUSD", "BUSD", "DAI", "EUR", "GBP", "ETH", "BTC", "BNB"}

func splitSymbolAssets(symbol string) (base, quote string) {
	s := strings.ToUpper(symbol)
	for _, q := range quoteAssets {
		if strings.HasSuffix(s, q) && len(s) > len(q) {
			return s[:len(s)-len(q)], q
		}
	}
	return s, "USDT"
}

func validateAvailableBalance(asset string, available float64) error {
	if math.IsNaN(available) {
		return fmt.Errorf("invalid available %s balance", asset)
	}
	return nil
}
