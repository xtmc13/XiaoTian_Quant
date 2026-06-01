package service

import (
	"sync"
	"time"
)

// Account represents a trading account with balances and positions.
type Account struct {
	ID        string
	Exchange  string
	Balances  map[string]*Balance
	Positions map[string]*Position
	CreatedAt time.Time
	mu        sync.RWMutex
}

// Balance represents a currency balance.
type Balance struct {
	Currency string
	Total    float64
	Free     float64
	Used     float64
}

// Position represents an open position.
type Position struct {
	ID             string
	Symbol         string
	Side           string
	Quantity       float64
	AvgEntryPrice  float64
	CurrentPrice   float64
	UnrealizedPnL  float64
	CostBasis      float64
	OpenedAt       time.Time
}

// AccountService manages multiple exchange accounts.
type AccountService struct {
	accounts map[string]*Account
	mu       sync.RWMutex
}

var (
	accountSvc     *AccountService
	accountSvcOnce sync.Once
)

// GetAccountService returns the singleton account service.
func GetAccountService() *AccountService {
	accountSvcOnce.Do(func() {
		svc := &AccountService{
			accounts: make(map[string]*Account),
		}
		// Create default account
		svc.CreateAccount("default", "BINANCE", map[string]float64{
			"USDT": 100000.0,
			"BTC":  0.0,
			"ETH":  0.0,
		})
		accountSvc = svc
	})
	return accountSvc
}

// CreateAccount creates a new trading account with initial balances.
func (as *AccountService) CreateAccount(id, exchange string, initialBalances map[string]float64) *Account {
	as.mu.Lock()
	defer as.mu.Unlock()

	acc := &Account{
		ID:        id,
		Exchange:  exchange,
		Balances:  make(map[string]*Balance),
		Positions: make(map[string]*Position),
		CreatedAt: time.Now(),
	}

	for currency, amount := range initialBalances {
		acc.Balances[currency] = &Balance{
			Currency: currency,
			Total:    amount,
			Free:     amount,
			Used:     0,
		}
	}

	as.accounts[id] = acc
	return acc
}

// GetAccount returns an account by ID.
func (as *AccountService) GetAccount(id string) *Account {
	as.mu.RLock()
	defer as.mu.RUnlock()
	return as.accounts[id]
}

// GetBalance returns the balance for a currency.
func (acc *Account) GetBalance(currency string) *Balance {
	acc.mu.RLock()
	defer acc.mu.RUnlock()
	b, ok := acc.Balances[currency]
	if !ok {
		return &Balance{Currency: currency, Total: 0, Free: 0, Used: 0}
	}
	return &Balance{Currency: b.Currency, Total: b.Total, Free: b.Free, Used: b.Used}
}

// LockFunds moves funds from free to used.
func (acc *Account) LockFunds(currency string, amount float64) bool {
	acc.mu.Lock()
	defer acc.mu.Unlock()

	b, ok := acc.Balances[currency]
	if !ok {
		return false
	}
	if b.Free < amount {
		return false
	}
	b.Free -= amount
	b.Used += amount
	return true
}

// UnlockFunds moves funds from used back to free.
func (acc *Account) UnlockFunds(currency string, amount float64) {
	acc.mu.Lock()
	defer acc.mu.Unlock()

	b, ok := acc.Balances[currency]
	if !ok {
		return
	}
	b.Used -= amount
	b.Free += amount
}

// UpdateTotal recalculates total balance.
func (acc *Account) UpdateTotal(currency string) {
	acc.mu.Lock()
	defer acc.mu.Unlock()

	b, ok := acc.Balances[currency]
	if !ok {
		return
	}
	b.Total = b.Free + b.Used
}

// OpenPosition adds a new position.
func (acc *Account) OpenPosition(pos *Position) {
	acc.mu.Lock()
	defer acc.mu.Unlock()

	pos.OpenedAt = time.Now()
	pos.CostBasis = pos.AvgEntryPrice * pos.Quantity
	acc.Positions[pos.ID] = pos
}

// ClosePosition removes a position by ID.
func (acc *Account) ClosePosition(id string) *Position {
	acc.mu.Lock()
	defer acc.mu.Unlock()

	pos, ok := acc.Positions[id]
	if ok {
		delete(acc.Positions, id)
	}
	return pos
}

// GetPositions returns all open positions.
func (acc *Account) GetPositions() []*Position {
	acc.mu.RLock()
	defer acc.mu.RUnlock()

	result := make([]*Position, 0, len(acc.Positions))
	for _, pos := range acc.Positions {
		cp := *pos
		result = append(result, &cp)
	}
	return result
}

// GetTotalEquity calculates total equity across all currencies.
func (acc *Account) GetTotalEquity(prices map[string]float64) float64 {
	acc.mu.RLock()
	defer acc.mu.RUnlock()

	equity := 0.0
	for _, b := range acc.Balances {
		if b.Currency == "USDT" {
			equity += b.Total
		} else {
			equity += b.Total * prices[b.Currency+"USDT"]
		}
	}
	return equity
}

// GetStats returns account statistics.
func (acc *Account) GetStats() map[string]any {
	acc.mu.RLock()
	defer acc.mu.RUnlock()

	totalEquity := 0.0
	availableBalance := 0.0
	marginUsed := 0.0

	for _, b := range acc.Balances {
		if b.Currency == "USDT" {
			totalEquity += b.Total
			availableBalance += b.Free
			marginUsed += b.Used
		}
	}

	positions := acc.GetPositions()
	unrealizedPnL := 0.0
	for _, p := range positions {
		unrealizedPnL += p.UnrealizedPnL
	}

	return map[string]any{
		"total_equity":      totalEquity + unrealizedPnL,
		"available_balance": availableBalance,
		"margin_used":       marginUsed,
		"unrealized_pnl":    unrealizedPnL,
		"position_count":    len(positions),
	}
}
