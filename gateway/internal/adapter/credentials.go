package adapter

import (
	"os"
	"strings"
)

// GetCredential returns API credentials for an exchange from environment variables only.
// Credentials are never read from persisted config files to prevent plaintext secret storage.
// Expected env vars: <EXCHANGE>_API_KEY, <EXCHANGE>_API_SECRET, <EXCHANGE>_PASSPHRASE.
func GetCredential(exchangeName string) (apiKey, secret, passphrase string) {
	name := strings.ToUpper(exchangeName)

	apiKey = os.Getenv(name + "_API_KEY")
	secret = os.Getenv(name + "_API_SECRET")
	passphrase = os.Getenv(name + "_PASSPHRASE")

	return
}

// HasCredential reports whether an exchange has non-empty credentials in the environment.
func HasCredential(exchangeName string) bool {
	name := strings.ToUpper(exchangeName)
	return os.Getenv(name+"_API_KEY") != "" && os.Getenv(name+"_API_SECRET") != ""
}
