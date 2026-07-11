package adapter

import (
	"os"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// exchangeAliases maps common exchange name variants to the canonical name
// used in config and environment variables.
var exchangeAliases = map[string]string{
	"gateio": "gate",
}

// normalizeExchangeName returns the canonical exchange name.
func normalizeExchangeName(name string) string {
	lower := strings.ToLower(name)
	if canonical, ok := exchangeAliases[lower]; ok {
		return canonical
	}
	return lower
}

// GetCredential returns API credentials for an exchange.
// Credentials are read from environment variables first to allow secret managers
// and deployment tooling to override persisted values. If environment variables
// are absent, credentials are read from the encrypted in-memory config store
// (populated from config.yaml).
// Expected env vars: <EXCHANGE>_API_KEY, <EXCHANGE>_API_SECRET, <EXCHANGE>_PASSPHRASE.
func GetCredential(exchangeName string) (apiKey, secret, passphrase string) {
	name := strings.ToUpper(normalizeExchangeName(exchangeName))

	apiKey = os.Getenv(name + "_API_KEY")
	secret = os.Getenv(name + "_API_SECRET")
	passphrase = os.Getenv(name + "_PASSPHRASE")

	if apiKey != "" && secret != "" {
		return
	}

	// Fallback: read from persisted config (decrypted in-memory cache).
	cfg := store.GetConfig()
	if cfg == nil {
		return
	}
	exchanges, _ := cfg["exchanges"].(map[string]any)
	if exchanges == nil {
		return
	}

	canonical := normalizeExchangeName(exchangeName)
	if ex, ok := exchanges[canonical].(map[string]any); ok {
		if v := getString(ex, "api_key", ""); v != "" && apiKey == "" {
			apiKey = v
		}
		if v := getString(ex, "secret", ""); v != "" && secret == "" {
			secret = v
		}
		if v := getString(ex, "passphrase", ""); v != "" && passphrase == "" {
			passphrase = v
		}
	}
	return
}

// HasCredential reports whether an exchange has non-empty API key and secret.
func HasCredential(exchangeName string) bool {
	apiKey, secret, _ := GetCredential(exchangeName)
	return apiKey != "" && secret != ""
}
