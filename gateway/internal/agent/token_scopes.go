package agent

// TokenScope defines capability scopes for agent tokens.
type TokenScope string

const (
	ScopeRead       TokenScope = "R" // Read: market data, klines, tickers
	ScopeWrite      TokenScope = "W" // Write: place orders, cancel orders
	ScopeBacktest   TokenScope = "B" // Backtest: run backtests, view results
	ScopeNotify     TokenScope = "N" // Notify: send notifications
	ScopeCommunity  TokenScope = "C" // Community: browse/purchase indicators
	ScopeTrade      TokenScope = "T" // Live Trading: full trading access
)

var AllScopes = []TokenScope{ScopeRead, ScopeWrite, ScopeBacktest, ScopeNotify, ScopeCommunity, ScopeTrade}

// ScopeDescriptions returns human-readable descriptions for each scope.
func ScopeDescriptions() map[TokenScope]string {
	return map[TokenScope]string{
		ScopeRead:      "Read: market data, klines, tickers, orderbook",
		ScopeWrite:     "Write: place/cancel orders in paper mode",
		ScopeBacktest:  "Backtest: run backtests, view history",
		ScopeNotify:    "Notify: send alerts and notifications",
		ScopeCommunity: "Community: browse, purchase, review indicators",
		ScopeTrade:     "Live Trading: place real orders on exchanges",
	}
}

// ValidateScopes checks if all given scopes are valid.
func ValidateScopes(scopes []TokenScope) bool {
	valid := map[TokenScope]bool{
		ScopeRead: true, ScopeWrite: true, ScopeBacktest: true,
		ScopeNotify: true, ScopeCommunity: true, ScopeTrade: true,
	}
	for _, s := range scopes {
		if !valid[s] {
			return false
		}
	}
	return true
}

// HasScope checks if the token has a specific scope.
func HasScope(tokenScopes []TokenScope, required TokenScope) bool {
	for _, s := range tokenScopes {
		if s == required {
			return true
		}
	}
	return false
}

// ScopesFromStrings converts string representations to TokenScope slice.
func ScopesFromStrings(s []string) []TokenScope {
	scopes := make([]TokenScope, 0, len(s))
	for _, v := range s {
		scopes = append(scopes, TokenScope(v))
	}
	return scopes
}
