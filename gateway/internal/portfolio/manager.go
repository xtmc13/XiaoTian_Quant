package portfolio

import (
	"encoding/json"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/adapter"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── Portfolio Manager ──

// Manager manages multi-account portfolio state.
type Manager struct {
	accounts  map[string]*model.AccountData
	positions map[string]*model.PositionData // positionID -> position
	snapshots []model.PortfolioSnapshot
	peak      float64
	mu        sync.RWMutex

	// Binance-specific totals
	spotTotalUSDT        float64
	futuresTotalUSDT     float64 // totalMarginBalance
	futuresUnrealizedPnL float64
	futuresWalletBalance float64
	fundingTotalUSDT     float64 // funding wallet
	earnTotalUSDT         float64 // earn (flexible + locked)

	// Other exchange totals (exchange name -> USDT value)
	otherExcTotals map[string]float64
	otherExcMu     sync.RWMutex

	// Callbacks
	OnPositionUpdate func(pos model.PositionData)
}

var (
	pmgr     *Manager
	pmgrOnce sync.Once
)

// GetManager returns the global portfolio manager.
func GetManager() *Manager {
	pmgrOnce.Do(func() {
		pmgr = NewManager()
	})
	return pmgr
}

// NewManager creates a new portfolio manager.
func NewManager() *Manager {
	m := &Manager{
		accounts:        make(map[string]*model.AccountData),
		positions:       make(map[string]*model.PositionData),
		otherExcTotals:  make(map[string]float64),
	}
	m.accounts["default"] = &model.AccountData{
		ID:       "default",
		Exchange: "paper",
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 100000, Free: 100000, Used: 0},
		},
		Positions: make(map[string]*model.PositionData),
		CreatedAt: time.Now().UnixMilli(),
	}
	m.peak = 100000
	return m
}

// SyncFromBinance fetches real balance from Binance API and updates the portfolio manager.
func (m *Manager) SyncFromBinance() {
	apiKey, secret, _ := adapter.GetCredential("binance")
	if apiKey == "" || secret == "" {
		log.Println("[Portfolio] No Binance API credentials configured, skipping sync")
		return
	}

	binance := adapter.NewBinanceAdapter(apiKey, secret, false)

	// ── Spot ──
	rawBalances, err := binance.GetBalance()
	if err != nil {
		log.Printf("[Portfolio] Binance spot sync error: %v", err)
	} else {
		m.syncSpotBalances(binance, rawBalances)
	}

	// ── Futures ──
	futuresAcct, err := binance.GetFuturesAccount()
	if err != nil {
		log.Printf("[Portfolio] Binance futures sync error: %v", err)
	} else {
		m.syncFuturesBalances(binance, futuresAcct)
	}

	// ── Funding ──
	fundingBalances, err := binance.GetFundingWallet()
	if err != nil {
		log.Printf("[Portfolio] Binance funding wallet error: %v", err)
	} else {
		m.syncFundingBalances(binance, fundingBalances)
	}

	// ── Earn ──
	flexibleEarn, err := binance.GetFlexibleEarn()
	if err != nil {
		log.Printf("[Portfolio] Binance flexible earn error: %v", err)
	}
	lockedEarn, err := binance.GetLockedEarn()
	if err != nil {
		log.Printf("[Portfolio] Binance locked earn error: %v", err)
	}
	if flexibleEarn != nil || lockedEarn != nil {
		m.syncEarnBalances(binance, flexibleEarn, lockedEarn)
	}

	// Remove paper account when real exchange data is available
	m.mu.Lock()
	if m.spotTotalUSDT > 0 || m.futuresTotalUSDT > 0 {
		delete(m.accounts, "paper")
		// Reset peak to actual current equity (paper account was 100k)
		if m.peak > m.spotTotalUSDT+m.futuresTotalUSDT*10 {
			m.peak = m.spotTotalUSDT + m.futuresTotalUSDT + m.fundingTotalUSDT + m.earnTotalUSDT
		}
	}
	m.mu.Unlock()
}

func (m *Manager) syncSpotBalances(binance *adapter.BinanceAdapter, rawBalances []map[string]any) {
	balances := make(map[string]*model.Balance)
	totalUSDT := 0.0

	for _, b := range rawBalances {
		free := parseAnyFloat(b["free"])
		locked := parseAnyFloat(b["locked"])
		total := free + locked
		if total <= 0 {
			continue
		}

		asset := ""
		if a, ok := b["asset"].(string); ok {
			asset = a
		}

		balances[asset] = &model.Balance{
			Currency: asset,
			Total:    total,
			Free:     free,
			Used:     locked,
		}

		estimated := estimateUSDT(asset, total)
		totalUSDT += estimated
	}

	m.mu.Lock()
	m.accounts["binance_spot"] = &model.AccountData{
		ID:         "binance_spot",
		Exchange:   "binance",
		Balances:   balances,
		Positions:  make(map[string]*model.PositionData),
		CreatedAt:  time.Now().UnixMilli(),
	}
	m.mu.Unlock()

	// Store spot USDT total for summary
	if totalUSDT > 0 {
		m.spotTotalUSDT = totalUSDT
		log.Printf("[Portfolio] Spot synced: ~%.2f USDT (%d assets)", totalUSDT, len(balances))
	}

	// Update peak
	m.mu.Lock()
	if m.spotTotalUSDT + m.futuresTotalUSDT > m.peak {
		m.peak = m.spotTotalUSDT + m.futuresTotalUSDT
	}
	m.mu.Unlock()
}

func (m *Manager) syncFuturesBalances(binance *adapter.BinanceAdapter, acct map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Total margin balance (USDT)
	totalMarginBalance := parseAnyFloat(acct["totalMarginBalance"])
	totalUnrealizedProfit := parseAnyFloat(acct["totalUnrealizedProfit"])
	totalWalletBalance := parseAnyFloat(acct["totalWalletBalance"])

	// Extract assets
	balances := make(map[string]*model.Balance)
	assets, _ := acct["assets"].([]any)
	for _, v := range assets {
		a, ok := v.(map[string]any)
		if !ok {
			continue
		}
		asset, _ := a["asset"].(string)
		wallet := parseAnyFloat(a["walletBalance"])
		upnl := parseAnyFloat(a["unrealizedProfit"])
		if wallet == 0 && upnl == 0 {
			continue
		}
		balances[asset] = &model.Balance{
			Currency: asset,
			Total:    wallet + upnl,
			Free:     wallet,
			Used:     0,
		}
	}

	// Extract positions for exposure
	positions := make(map[string]*model.PositionData)
	posList, _ := acct["positions"].([]any)
	posCount := 0
	for _, v := range posList {
		p, ok := v.(map[string]any)
		if !ok {
			continue
		}
		amt := parseAnyFloat(p["positionAmt"])
		if amt == 0 {
			continue
		}
		symbol, _ := p["symbol"].(string)
		positions[symbol] = &model.PositionData{
			Symbol:         symbol,
			Quantity:       amt,
			AvgEntryPrice:  parseAnyFloat(p["entryPrice"]),
			CurrentPrice:   parseAnyFloat(p["markPrice"]),
			UnrealizedPnL:  parseAnyFloat(p["unrealizedProfit"]),
		}
		posCount++
	}

	m.accounts["binance_futures"] = &model.AccountData{
		ID:          "binance_futures",
		Exchange:    "binance",
		Balances:    balances,
		Positions:   positions,
		CreatedAt:   time.Now().UnixMilli(),
	}

	m.futuresTotalUSDT = totalMarginBalance
	m.futuresUnrealizedPnL = totalUnrealizedProfit
	m.futuresWalletBalance = totalWalletBalance

	if m.spotTotalUSDT+m.futuresTotalUSDT > m.peak {
		m.peak = m.spotTotalUSDT + m.futuresTotalUSDT
	}

	if totalMarginBalance > 0 || posCount > 0 {
		log.Printf("[Portfolio] Futures synced: ~%.2f USDT wallet, %.4f unrealized PnL (%d positions)",
			totalWalletBalance, totalUnrealizedProfit, posCount)
	}
}

func (m *Manager) syncFundingBalances(binance *adapter.BinanceAdapter, funding []adapter.FundingBalance) {
	m.mu.Lock()
	defer m.mu.Unlock()

	balances := make(map[string]*model.Balance)
	fundingUSDT := 0.0

	for _, fb := range funding {
		if fb.Free == 0 && fb.Locked == 0 {
			continue
		}
		total := fb.Free + fb.Locked
		balances[fb.Asset] = &model.Balance{
			Currency: fb.Asset,
			Total:    total,
			Free:     fb.Free,
			Used:     fb.Locked,
		}
		fundingUSDT += estimateUSDT(fb.Asset, total)
	}

	if len(balances) > 0 {
		m.accounts["binance_funding"] = &model.AccountData{
			ID:         "binance_funding",
			Exchange:   "binance",
			Balances:   balances,
			Positions:  make(map[string]*model.PositionData),
			CreatedAt:  time.Now().UnixMilli(),
		}
	}

	m.fundingTotalUSDT = fundingUSDT

	if fundingUSDT > 0 {
		log.Printf("[Portfolio] Funding wallet synced: ~%.4f USDT (%d assets)", fundingUSDT, len(balances))
	}
}

func (m *Manager) syncEarnBalances(binance *adapter.BinanceAdapter, flexible, locked []adapter.EarnPosition) {
	m.mu.Lock()
	defer m.mu.Unlock()

	balances := make(map[string]*model.Balance)
	earnUSDT := 0.0

	for _, ep := range flexible {
		if ep.Amount <= 0 {
			continue
		}
		usdt := estimateUSDT(ep.Asset, ep.Amount)
		earnUSDT += usdt
		key := ep.Asset + "_flex"
		balances[key] = &model.Balance{
			Currency: ep.Asset + " (活期)",
			Total:    ep.Amount,
			Free:     ep.Amount,
			Used:     0,
		}
	}

	for _, ep := range locked {
		if ep.Amount <= 0 {
			continue
		}
		usdt := estimateUSDT(ep.Asset, ep.Amount)
		earnUSDT += usdt
		key := ep.Asset + "_locked"
		balances[key] = &model.Balance{
			Currency: ep.Asset + " (定期)",
			Total:    ep.Amount,
			Free:     0,
			Used:     ep.Amount,
		}
	}

	if len(balances) > 0 {
		m.accounts["binance_earn"] = &model.AccountData{
			ID:         "binance_earn",
			Exchange:   "binance",
			Balances:   balances,
			Positions:  make(map[string]*model.PositionData),
			CreatedAt:  time.Now().UnixMilli(),
		}
	}

	m.earnTotalUSDT = earnUSDT

	if earnUSDT > 0 {
		log.Printf("[Portfolio] Earn synced: ~%.4f USDT (flexible + locked)", earnUSDT)
	}
}

// ── Multi-Exchange Sync ────────────────────────────────────────────────────

// SyncAllExchanges syncs Binance + all other configured exchanges.
func (m *Manager) SyncAllExchanges() {
	m.SyncFromBinance()

	// Sync each configured non-Binance exchange
	exchanges := []string{"okx", "bybit", "gate", "mexc", "bitget", "coinbase"}
	for _, name := range exchanges {
		apiKey, secret, passphrase := adapter.GetCredential(name)
		if apiKey == "" || secret == "" {
			log.Printf("[Portfolio] %s: no credentials, skipping", name)
			continue
		}
		log.Printf("[Portfolio] %s: syncing...", name)
		m.syncGenericExchange(name, apiKey, secret, passphrase)
	}
}

// syncGenericExchange creates the appropriate adapter and syncs spot balance.
func (m *Manager) syncGenericExchange(name, apiKey, secret, passphrase string) {
	var balances []map[string]any
	var err error

	switch name {
	case "okx":
		okx := adapter.NewOKXAdapter(apiKey, secret, passphrase, false)
		balances, err = okx.GetBalance()
	case "bybit":
		bybit := adapter.NewBybitAdapter(apiKey, secret, false)
		balances, err = bybit.GetBalance()
	case "gate":
		gateio := adapter.NewGateIOAdapter(apiKey, secret)
		balances, err = gateio.GetBalance()
	case "mexc":
		mexc := adapter.NewMEXCAdapter(apiKey, secret)
		balances, err = mexc.GetBalance()
	case "bitget":
		bitget := adapter.NewBitgetAdapter(apiKey, secret, passphrase)
		balances, err = bitget.GetBalance()
	case "coinbase":
		coinbase := adapter.NewCoinbaseAdapter(apiKey, secret)
		balances, err = coinbase.GetBalance()
	default:
		return
	}

	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "50102") || strings.Contains(errStr, "Timestamp") {
			log.Printf("[Portfolio] %s: API key valid but rejected — likely IP whitelist. Add 43.165.169.27 to %s API allowlist.", name, name)
		} else {
			log.Printf("[Portfolio] %s sync error: %v", name, err)
		}
		return
	}

	m.syncSpotBalancesGeneric(name, balances)
}

// syncSpotBalancesGeneric processes spot balances from any exchange into the portfolio.
// Each exchange returns different field names, so we normalize them here.
func (m *Manager) syncSpotBalancesGeneric(exchange string, rawBalances []map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	balances := make(map[string]*model.Balance)
	totalUSDT := 0.0

	for _, raw := range rawBalances {
		asset, free, locked := normalizeBalanceFields(exchange, raw)
		if asset == "" {
			continue
		}
		total := free + locked
		if total <= 0 {
			continue
		}
		balances[asset] = &model.Balance{
			Currency: asset,
			Total:    total,
			Free:     free,
			Used:     locked,
		}
		totalUSDT += estimateUSDT(asset, total)
	}

	if len(balances) > 0 {
		accountID := exchange + "_spot"
		m.accounts[accountID] = &model.AccountData{
			ID:         accountID,
			Exchange:   exchange,
			Balances:   balances,
			Positions:  make(map[string]*model.PositionData),
			CreatedAt:  time.Now().UnixMilli(),
		}
	}

	m.otherExcMu.Lock()
	m.otherExcTotals[exchange] = totalUSDT
	m.otherExcMu.Unlock()

	if totalUSDT > 0 {
		log.Printf("[Portfolio] %s synced: ~%.4f USDT (%d assets)", exchange, totalUSDT, len(balances))
	}
}

// normalizeBalanceFields extracts asset/free/locked from exchange-specific response format.
func normalizeBalanceFields(exchange string, raw map[string]any) (asset string, free, locked float64) {
	switch exchange {
	case "okx":
		// OKX: {"ccy":"USDT", "availBal":"100.0", "frozenBal":"0.0"}
		asset = getMapString(raw, "ccy")
		free = getMapFloat(raw, "availBal")
		locked = getMapFloat(raw, "frozenBal")
	case "bybit":
		// Bybit: {"coin":[{"coin":"USDT","walletBalance":"100"}]}
		// Note: Bybit's wallet-balance response wraps coins in "coin" array
		asset = getMapString(raw, "coin")
		free = getMapFloat(raw, "availableToWithdraw")
		locked = getMapFloat(raw, "walletBalance") - free
		if locked < 0 {
			locked = 0
		}
	case "gate":
		// Gate.io: {"currency":"USDT","available":"100","locked":"0"}
		asset = strings.ToUpper(getMapString(raw, "currency"))
		free = getMapFloat(raw, "available")
		locked = getMapFloat(raw, "locked")
	case "mexc":
		// MEXC: {"asset":"USDT","free":"100","locked":"0"} (same as Binance)
		asset = getMapString(raw, "asset")
		free = getMapFloat(raw, "free")
		locked = getMapFloat(raw, "locked")
	case "bitget":
		// Bitget (normalized by adapter): {"asset":"USDT","free":100.0,"locked":0.0}
		asset = getMapString(raw, "asset")
		free = getMapFloat(raw, "free")
		locked = getMapFloat(raw, "locked")
	case "coinbase":
		// Coinbase: {"currency":"USDT", "available_balance":{"value":"100"}}
		asset = getMapString(raw, "currency")
		if ab, ok := raw["available_balance"].(map[string]any); ok {
			free = getMapFloat(ab, "value")
		}
		// Coinbase doesn't have locked; total is available + hold
		if hold, ok := raw["hold"].(map[string]any); ok {
			locked = getMapFloat(hold, "value")
		}
	}
	return
}

func parseAnyFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	}
	return 0
}

func getBinancePrice(symbol string) float64 {
	resp, err := http.Get("https://api.binance.com/api/v3/ticker/price?symbol=" + symbol)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(body, &result)
	if priceStr, ok := result["price"].(string); ok {
		f, _ := strconv.ParseFloat(priceStr, 64)
		return f
	}
	return 0
}

func estimateUSDT(asset string, amount float64) float64 {
	switch asset {
	case "USDT", "BUSD", "USDC", "FDUSD":
		return amount
	case "BTC":
		return amount * getBinancePrice("BTCUSDT")
	case "ETH":
		return amount * getBinancePrice("ETHUSDT")
	case "BNB":
		return amount * getBinancePrice("BNBUSDT")
	case "SOL":
		return amount * getBinancePrice("SOLUSDT")
	case "DOGE":
		return amount * getBinancePrice("DOGEUSDT")
	case "XRP":
		return amount * getBinancePrice("XRPUSDT")
	case "ADA":
		return amount * getBinancePrice("ADAUSDT")
	case "TRX":
		return amount * getBinancePrice("TRXUSDT")
	case "LINK":
		return amount * getBinancePrice("LINKUSDT")
	case "AVAX":
		return amount * getBinancePrice("AVAXUSDT")
	case "DOT":
		return amount * getBinancePrice("DOTUSDT")
	case "MATIC", "POL":
		return amount * getBinancePrice("POLUSDT")
	case "TON":
		return amount * getBinancePrice("TONUSDT")
	default:
		if price := getBinancePrice(asset + "USDT"); price > 0 {
			return amount * price
		}
		return 0
	}
}

// GetAccount returns an account by ID.
func (m *Manager) GetAccount(id string) *model.AccountData {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.accounts[id]
}

// UpdateBalance updates an account's balance.
func (m *Manager) UpdateBalance(accountID, currency string, total, free, used float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	acct := m.accounts[accountID]
	if acct == nil {
		return
	}
	acct.Balances[currency] = &model.Balance{
		Currency: currency,
		Total:    total,
		Free:     free,
		Used:     used,
	}
}

// UpdatePosition updates or creates a position.
func (m *Manager) UpdatePosition(pos model.PositionData) {
	m.mu.Lock()
	m.positions[pos.ID] = &pos
	if acct, ok := m.accounts["default"]; ok {
		acct.Positions[pos.ID] = &pos
	}
	m.mu.Unlock()

	if m.OnPositionUpdate != nil {
		m.OnPositionUpdate(pos)
	}
}

// RemovePosition removes a closed position.
func (m *Manager) RemovePosition(positionID string) {
	m.mu.Lock()
	delete(m.positions, positionID)
	if acct, ok := m.accounts["default"]; ok {
		delete(acct.Positions, positionID)
	}
	m.mu.Unlock()
}

// GetPositions returns all open positions.
func (m *Manager) GetPositions() []*model.PositionData {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*model.PositionData, 0, len(m.positions))
	for _, pos := range m.positions {
		copy_ := *pos
		result = append(result, &copy_)
	}
	return result
}

// ── Equity & KPIs ──

// TotalEquity calculates the total portfolio equity.
func (m *Manager) TotalEquity() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	other := 0.0
	m.otherExcMu.RLock()
	for _, v := range m.otherExcTotals {
		other += v
	}
	m.otherExcMu.RUnlock()
	// Include account balances (e.g., default paper trading account)
	accountTotal := 0.0
	for _, acct := range m.accounts {
		for _, bal := range acct.Balances {
			accountTotal += bal.Total
		}
	}
	return m.spotTotalUSDT + m.futuresTotalUSDT + m.fundingTotalUSDT + m.earnTotalUSDT + other + accountTotal
}

// AvailableBalance returns the sum of free balances.
func (m *Manager) AvailableBalance() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	other := 0.0
	m.otherExcMu.RLock()
	for _, v := range m.otherExcTotals {
		other += v
	}
	m.otherExcMu.RUnlock()
	return m.spotTotalUSDT + m.futuresWalletBalance + m.fundingTotalUSDT + m.earnTotalUSDT + other
}

// SpotBalance returns the spot total (USDT).
func (m *Manager) SpotBalance() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.spotTotalUSDT
}

// FuturesBalance returns the futures total margin balance (USDT).
func (m *Manager) FuturesBalance() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.futuresTotalUSDT
}

// FuturesPnL returns unrealized PnL from futures.
func (m *Manager) FuturesPnL() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.futuresUnrealizedPnL
}

// FuturesWalletBalance returns the futures wallet balance.
func (m *Manager) FuturesWalletBalance() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.futuresWalletBalance
}

// FundingBalance returns the funding wallet total (USDT).
func (m *Manager) FundingBalance() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.fundingTotalUSDT
}

// EarnBalance returns the earn total (USDT).
func (m *Manager) EarnBalance() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.earnTotalUSDT
}

// OtherExchangeBalance returns the total USDT for a specific non-Binance exchange.
func (m *Manager) OtherExchangeBalance(exchange string) float64 {
	m.otherExcMu.RLock()
	defer m.otherExcMu.RUnlock()
	return m.otherExcTotals[exchange]
}

// OtherExchangeTotals returns a copy of all non-Binance exchange totals (non-zero only).
func (m *Manager) OtherExchangeTotals() map[string]float64 {
	m.otherExcMu.RLock()
	defer m.otherExcMu.RUnlock()
	cp := make(map[string]float64)
	for k, v := range m.otherExcTotals {
		if v > 0 {
			cp[k] = v
		}
	}
	return cp
}

// ExchangeBalance returns the total USDT-estimated balance for a specific exchange.
func (m *Manager) ExchangeBalance(exchange string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	switch exchange {
	case "binance":
		return m.spotTotalUSDT + m.futuresTotalUSDT + m.fundingTotalUSDT + m.earnTotalUSDT
	default:
		m.otherExcMu.RLock()
		v := m.otherExcTotals[exchange]
		m.otherExcMu.RUnlock()
		if v > 0 {
			return v
		}
		// Fallback: sum account balances for this exchange
		total := 0.0
		for _, acct := range m.accounts {
			if acct.Exchange != exchange {
				continue
			}
			for _, bal := range acct.Balances {
				total += bal.Total
			}
		}
		return total
	}
}

// MarginUsed returns the sum of used balances.
func (m *Manager) MarginUsed() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := 0.0
	for _, acct := range m.accounts {
		for _, bal := range acct.Balances {
			total += bal.Used
		}
	}
	return total
}

// Drawdown calculates the current drawdown from peak equity.
func (m *Manager) Drawdown() float64 {
	equity := m.TotalEquity()
	m.mu.Lock()
	if equity > m.peak {
		m.peak = equity
	}
	peak := m.peak
	m.mu.Unlock()

	if peak <= 0 {
		return 0
	}
	dd := (peak - equity) / peak * 100
	log.Printf("[Portfolio] Drawdown debug: peak=%.2f equity=%.2f drawdown=%.2f%%", peak, equity, dd)
	return dd
}

// TotalPnL returns the sum of all unrealized PnL across positions.
func (m *Manager) TotalPnL() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := 0.0
	for _, pos := range m.positions {
		total += pos.UnrealizedPnL
	}
	return total
}

// NetExposure calculates total exposure as a percentage of equity.
func (m *Manager) NetExposure() float64 {
	equity := m.TotalEquity()
	if equity <= 0 {
		return 0
	}
	totalExposure := 0.0
	m.mu.RLock()
	for _, pos := range m.positions {
		totalExposure += math.Abs(pos.Quantity * pos.CurrentPrice)
	}
	m.mu.RUnlock()
	return totalExposure / equity * 100
}

// Snapshot records an equity snapshot.
func (m *Manager) Snapshot() {
	snap := model.PortfolioSnapshot{
		TotalEquity:      m.TotalEquity(),
		AvailableBalance: m.AvailableBalance(),
		MarginUsed:       m.MarginUsed(),
		Drawdown:         m.Drawdown(),
		NetExposure:      m.NetExposure(),
		Positions:        m.GetPositions(),
		Timestamp:        time.Now().UnixMilli(),
	}

	m.mu.Lock()
	m.snapshots = append(m.snapshots, snap)
	if len(m.snapshots) > 5000 {
		m.snapshots = m.snapshots[len(m.snapshots)-5000:]
	}
	m.mu.Unlock()
}

// GetSnapshots returns equity history.
func (m *Manager) GetSnapshots() []model.PortfolioSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]model.PortfolioSnapshot, len(m.snapshots))
	copy(result, m.snapshots)
	return result
}

// GetLatestSnapshot returns the most recent snapshot.
func (m *Manager) GetLatestSnapshot() *model.PortfolioSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.snapshots) == 0 {
		return nil
	}
	s := m.snapshots[len(m.snapshots)-1]
	return &s
}

// ── Position Sizing ──

// FixedFraction returns a position size based on a percentage of equity.
func FixedFraction(equity float64, fraction float64) float64 {
	return equity * fraction
}

// KellyCriterion calculates position size using the Kelly formula.
// Uses half-Kelly by default for safety.
func KellyCriterion(winRate, avgWin, avgLoss float64, halfKelly bool) float64 {
	if avgLoss <= 0 {
		return 0
	}
	b := avgWin / avgLoss
	p := winRate
	q := 1 - p
	kelly := (p*b - q) / b
	if kelly <= 0 {
		return 0
	}
	if halfKelly {
		kelly /= 2
	}
	return kelly
}

// RiskBudget sizes a position based on max risk per trade and stop-loss distance.
func RiskBudget(equity, riskPct, stopLossPct float64) float64 {
	riskAmount := equity * riskPct
	if stopLossPct <= 0 {
		return 0
	}
	return riskAmount / stopLossPct
}

// EqualWeight divides equity equally among N positions.
func EqualWeight(equity float64, numPositions int) float64 {
	if numPositions <= 0 {
		return 0
	}
	return equity / float64(numPositions)
}

// VolatilityAdjusted scales position size inversely with volatility.
func VolatilityAdjusted(baseSize, volatility, targetVol float64) float64 {
	if volatility <= 0 {
		return baseSize
	}
	adjustment := targetVol / volatility
	adjusted := baseSize * adjustment
	// Clamp to [0.1x, 2x] of base size
	if adjusted < baseSize*0.1 {
		adjusted = baseSize * 0.1
	}
	if adjusted > baseSize*2 {
		adjusted = baseSize * 2
	}
	return adjusted
}

// ── Monitor ──

// Monitor provides periodic portfolio health checks.
type Monitor struct {
	manager        *Manager
	lastCheck      time.Time
	checkInterval  time.Duration
	dailyLossPct   float64
	maxConsecutive int
	tradeResults   []float64 // positive = win, negative = loss

	OnAlert func(level, message string)
	mu      sync.Mutex
}

func NewMonitor(mgr *Manager) *Monitor {
	return &Monitor{
		manager:        mgr,
		checkInterval:  30 * time.Second,
		dailyLossPct:   5.0,
		maxConsecutive: 5,
	}
}

// Check performs a health check on the portfolio.
func (mon *Monitor) Check() []string {
	mon.mu.Lock()
	defer mon.mu.Unlock()

	var alerts []string
	now := time.Now()

	// Check drawdown
	dd := mon.manager.Drawdown()
	if dd > mon.dailyLossPct {
		alerts = append(alerts, "Daily loss exceeded")
	}

	// Check consecutive losses
	if len(mon.tradeResults) >= mon.maxConsecutive {
		recent := mon.tradeResults[len(mon.tradeResults)-mon.maxConsecutive:]
		allLoss := true
		for _, r := range recent {
			if r >= 0 {
				allLoss = false
				break
			}
		}
		if allLoss {
			alerts = append(alerts, "Consecutive losses detected")
		}
	}

	mon.lastCheck = now
	return alerts
}

// RecordTrade records a trade result for monitoring.
func (mon *Monitor) RecordTrade(pnl float64) {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	mon.tradeResults = append(mon.tradeResults, pnl)
	if len(mon.tradeResults) > 100 {
		mon.tradeResults = mon.tradeResults[len(mon.tradeResults)-100:]
	}
}

// ── KPIs ──

// SharpeRatio calculates the annualized Sharpe ratio from a series of returns.
func SharpeRatio(returns []float64, riskFreeRate float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	avg := 0.0
	for _, r := range returns {
		avg += r
	}
	avg /= float64(len(returns))

	variance := 0.0
	for _, r := range returns {
		variance += (r - avg) * (r - avg)
	}
	std := math.Sqrt(variance / float64(len(returns)-1))
	if std == 0 {
		return 0
	}

	return (avg - riskFreeRate) / std * math.Sqrt(252) // Annualized
}

// SortinoRatio calculates the Sortino ratio (downside deviation only).
func SortinoRatio(returns []float64, riskFreeRate float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	avg := 0.0
	for _, r := range returns {
		avg += r
	}
	avg /= float64(len(returns))

	downVar := 0.0
	count := 0
	for _, r := range returns {
		if r < 0 {
			downVar += r * r
			count++
		}
	}
	if count <= 1 || downVar == 0 {
		return 0
	}
	downStd := math.Sqrt(downVar / float64(count-1))
	return (avg - riskFreeRate) / downStd * math.Sqrt(252)
}

// MaxDrawdownPct calculates maximum drawdown from equity curve.
func MaxDrawdownPct(equity []float64) float64 {
	if len(equity) == 0 {
		return 0
	}
	peak := equity[0]
	maxDD := 0.0
	for _, eq := range equity {
		if eq > peak {
			peak = eq
		}
		dd := (peak - eq) / peak * 100
		if dd > maxDD {
			maxDD = dd
		}
	}
	return maxDD
}

// WinRate calculates the win rate from PnL results.
func WinRate(pnls []float64) float64 {
	if len(pnls) == 0 {
		return 0
	}
	wins := 0
	for _, p := range pnls {
		if p > 0 {
			wins++
		}
	}
	return float64(wins) / float64(len(pnls)) * 100
}

// CalmarRatio calculates return / max drawdown.
func CalmarRatio(totalReturn, maxDD float64) float64 {
	if maxDD == 0 {
		return 0
	}
	return totalReturn / maxDD
}

// ── Sizing Helpers ──

// Sizer provides convenient position sizing calculations.
type Sizer struct {
	Equity      float64
	RiskPct     float64
	StopLossPct float64
}

func NewSizer(equity float64) *Sizer {
	return &Sizer{
		Equity:      equity,
		RiskPct:     0.02,
		StopLossPct: 0.05,
	}
}

func (s *Sizer) FixedFrac(fraction float64) float64 {
	return FixedFraction(s.Equity, fraction)
}

func (s *Sizer) Kelly(winRate, avgWin, avgLoss float64) float64 {
	return KellyCriterion(winRate, avgWin, avgLoss, true)
}

func (s *Sizer) RiskBudget_() float64 {
	return RiskBudget(s.Equity, s.RiskPct, s.StopLossPct)
}

func (s *Sizer) EqualWeight_(n int) float64 {
	return EqualWeight(s.Equity, n)
}

// ── Convenience: Parse returns from snapshots ──

// ReturnsFromSnapshots extracts period returns from equity snapshots.
func ReturnsFromSnapshots(snapshots []model.PortfolioSnapshot) []float64 {
	if len(snapshots) < 2 {
		return nil
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Timestamp < snapshots[j].Timestamp
	})
	returns := make([]float64, len(snapshots)-1)
	for i := 1; i < len(snapshots); i++ {
		prev := snapshots[i-1].TotalEquity
		if prev > 0 {
			returns[i-1] = (snapshots[i].TotalEquity - prev) / prev
		}
	}
	return returns
}

// ── Helpers for exchange balance normalization ──

func getMapString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getMapFloat(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	return 0
}
