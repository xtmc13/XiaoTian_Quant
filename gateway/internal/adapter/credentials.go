package adapter

import (
	"os"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// GetCredential returns API credentials for an exchange.
// First checks the saved config (from Settings page), then falls back to environment variables.
func GetCredential(exchangeName string) (apiKey, secret, passphrase string) {
	name := strings.ToLower(exchangeName)

	// 1. Try saved config from Settings page
	cfg := store.GetConfig()
	if exchanges, ok := cfg["exchanges"].(map[string]any); ok {
		if ex, ok := exchanges[name].(map[string]any); ok {
			if k, ok := ex["api_key"].(string); ok && k != "" {
				apiKey = k
			}
			if s, ok := ex["secret"].(string); ok && s != "" {
				secret = s
			}
			if p, ok := ex["passphrase"].(string); ok && p != "" {
				passphrase = p
			}
		}
	}

	// 2. Fallback to environment variables
	if apiKey == "" {
		apiKey = os.Getenv(strings.ToUpper(name) + "_API_KEY")
	}
	if secret == "" {
		secret = os.Getenv(strings.ToUpper(name) + "_API_SECRET")
	}
	if passphrase == "" {
		passphrase = os.Getenv(strings.ToUpper(name) + "_PASSPHRASE")
	}

	return
}
