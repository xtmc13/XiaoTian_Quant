package handler

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/adapter"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/store"
)

var (
	exchangeRates map[string]float64
	ratesMu       sync.RWMutex
	ratesTs       time.Time
)

func init() {
	exchangeRates = make(map[string]float64)
	exchangeRates["CNY"] = 7.25 // Default fallback
	go refreshExchangeRates()
}

func refreshExchangeRates() {
	for {
		rates := fetchExchangeRates()
		if len(rates) > 0 {
			ratesMu.Lock()
			exchangeRates = rates
			ratesTs = time.Now()
			ratesMu.Unlock()
			log.Printf("[Rate] Exchange rates updated (%d currencies)", len(rates))
		}
		time.Sleep(1 * time.Hour)
	}
}

func fetchExchangeRates() map[string]float64 {
	apis := []string{
		"https://open.er-api.com/v6/latest/USD",
		"https://api.exchangerate-api.com/v4/latest/USD",
	}
	for _, api := range apis {
		resp, err := http.Get(api)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			continue
		}
		if rates, ok := result["rates"].(map[string]any); ok {
			out := make(map[string]float64)
			for k, v := range rates {
				if f, ok := v.(float64); ok {
					out[k] = f
				}
			}
			return out
		}
	}
	return nil
}

func GetUsdCnyRate() float64 {
	return GetRate("CNY")
}

// GetRate returns the exchange rate for a target currency (1 USD = X target)
func GetRate(currency string) float64 {
	ratesMu.RLock()
	defer ratesMu.RUnlock()
	if r, ok := exchangeRates[currency]; ok {
		return r
	}
	return 0
}

// GetAllRates returns all cached exchange rates
func GetAllRates() map[string]float64 {
	ratesMu.RLock()
	defer ratesMu.RUnlock()
	out := make(map[string]float64)
	for k, v := range exchangeRates {
		out[k] = v
	}
	return out
}

// GetPreferredCurrency returns the user's preferred display currency from config
func GetPreferredCurrency() string {
	cfg := store.GetConfig()
	if settings, ok := cfg["settings"].(map[string]any); ok {
		if cur, ok := settings["currency"].(string); ok && cur != "" {
			return cur
		}
	}
	return "CNY" // default
}

// PortfolioSummary returns real-time portfolio state.
func PortfolioSummary(c *gin.Context) {
	mgr := portfolio.GetManager()
	if mgr == nil {
		c.JSON(http.StatusOK, gin.H{"error": "portfolio not initialized"})
		return
	}

	// Sync all configured exchanges
	mgr.SyncAllExchanges()

	// Load exchange configs
	cfg := store.GetConfig()
	exchanges := []gin.H{}
	if exMap, ok := cfg["exchanges"].(map[string]any); ok {
		for name, v := range exMap {
			ex, _ := v.(map[string]any)
			// 凭证判定走统一解析链（env→保险库→config）：P0-1 后 config.yaml
			// 里的 api_key/secret 恒为空（明文已收进保险库），读 config 原值会
			// 永远得到 configured=false（仪表盘资产分布空白的根因）。
			hasCreds := adapter.HasCredential(name)
			connected := false
			exchangeBalance := 0.0
			if hasCreds {
				// Quick connectivity check + get balance
				if name == "binance" {
					apiKey, secret, _ := adapter.GetCredential("binance")
					if apiKey != "" && secret != "" {
						binance := adapter.NewBinanceAdapter(apiKey, secret, false)
						_, err := binance.GetBalance()
						connected = err == nil
					}
					exchangeBalance = mgr.ExchangeBalance(name)
				} else {
					connected = true
					exchangeBalance = mgr.ExchangeBalance(name)
				}
			}
			exchanges = append(exchanges, gin.H{
				"name":      name,
				"configured": hasCreds,
				"connected": connected,
				"enabled":   ex["enabled"],
				"balance":   exchangeBalance,
			})
		}
	}

	preferredCurrency := GetPreferredCurrency()
	rate := GetRate(preferredCurrency)
	if rate == 0 {
		rate = 1.0 // fallback: show in USD
	}

	c.JSON(http.StatusOK, gin.H{
		"total_equity":             mgr.TotalEquity(),
		"paper_equity":             mgr.PaperEquity(),
		"total_pnl":                mgr.TotalPnL(),
		"available_balance":        mgr.AvailableBalance(),
		"margin_used":              mgr.MarginUsed(),
		"drawdown_pct":             mgr.Drawdown(),
		"net_exposure_pct":         mgr.NetExposure(),
		"position_count":           len(mgr.GetPositions()),
		"exchanges":                exchanges,
		"spot_balance":             mgr.SpotBalance(),
		"futures_balance":          mgr.FuturesBalance(),
		"futures_unrealized_pnl":   mgr.FuturesPnL(),
		"futures_wallet_balance":   mgr.FuturesWalletBalance(),
		"funding_balance":          mgr.FundingBalance(),
		"earn_balance":             mgr.EarnBalance(),
		"other_exchanges":          mgr.OtherExchangeTotals(),
		"usd_cny_rate":             GetUsdCnyRate(),
		"conversion_rate":          rate,
		"preferred_currency":       preferredCurrency,
	})
}

// PortfolioPositions returns all current positions.
func PortfolioPositions(c *gin.Context) {
	mgr := portfolio.GetManager()
	if mgr == nil {
		c.JSON(http.StatusOK, gin.H{"positions": []any{}})
		return
	}

	positions := mgr.GetPositions()
	posList := make([]gin.H, 0, len(positions))
	for _, p := range positions {
		posList = append(posList, gin.H{
			"symbol":            p.Symbol,
			"quantity":          p.Quantity,
			"avg_entry_price":   p.AvgEntryPrice,
			"current_price":     p.CurrentPrice,
			"unrealized_pnl":    p.UnrealizedPnL,
			"realized_pnl":      p.RealizedPnL,
			"side":              p.Side,
			"margin":            p.Margin,
			"liquidation_price": p.LiquidationPrice,
			"leverage":          p.Leverage,
			"market_type":       p.MarketType,
			"margin_mode":       p.MarginMode,
		})
	}
	c.JSON(http.StatusOK, gin.H{"positions": posList})
}

// ClosedPositions GET /api/positions/closed?limit=&offset=
// 已平仓持仓列表（positions.status=CLOSED，平仓时间倒序，分页）。
// 登录用户仅见本人持仓；admin 见全部。每行带 pnl_pct，前端资产页
// 「已平仓持仓」列表与交易分享卡（/api/share/trade/:id/card）共用同一 id。
func ClosedPositions(c *gin.Context) {
	limit := 20
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 {
		limit = v
	}
	if limit > 100 {
		limit = 100
	}
	offset := 0
	if v, err := strconv.Atoi(c.Query("offset")); err == nil && v > 0 {
		offset = v
	}

	filter := map[string]any{"status": "CLOSED"}
	if uid, injected := ctxUserID(c); injected && !ctxIsAdmin(c) {
		filter["user_id"] = uid
	}
	list, err := store.NewPositionRepo().ListPaged(filter, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, p := range list {
		var pnlPct float64
		if p.CostBasis > 0 {
			pnlPct = p.RealizedPnL / p.CostBasis * 100
		}
		items = append(items, gin.H{
			"id":              p.ID,
			"symbol":          p.Symbol,
			"side":            p.Side,
			"quantity":        p.Quantity,
			"avg_entry_price": p.AvgEntryPrice,
			"exit_price":      p.CurrentPrice,
			"realized_pnl":    p.RealizedPnL,
			"cost_basis":      p.CostBasis,
			"pnl_pct":         pnlPct,
			"exchange":        p.Exchange,
			"opened_at":       p.OpenedAt,
			"closed_at":       p.ClosedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"positions": items,
		"limit":     limit,
		"offset":    offset,
		"has_more":  len(items) == limit,
	})
}

// PortfolioSnapshots returns recent equity snapshots.
func PortfolioSnapshots(c *gin.Context) {
	mgr := portfolio.GetManager()
	if mgr == nil {
		c.JSON(http.StatusOK, gin.H{"snapshots": []any{}})
		return
	}

	snapshots := filterEquityOutliers(mgr.GetSnapshots())
	snapList := make([]gin.H, 0, len(snapshots))
	for _, s := range snapshots {
		snapList = append(snapList, gin.H{
			"timestamp":    s.Timestamp,
			"total_equity": s.TotalEquity,
			"drawdown":     s.Drawdown,
		})
	}
	c.JSON(http.StatusOK, gin.H{"snapshots": snapList})
}

// UsdCnyRate returns the current USD/CNY exchange rate.
func UsdCnyRate(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"rate":       GetUsdCnyRate(),
		"rates":      GetAllRates(),
		"updated_at": ratesTs.Format(time.RFC3339),
		"preferred":  GetPreferredCurrency(),
	})
}

// SettingsCurrencyGet returns the current preferred display currency.
func SettingsCurrencyGet(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"currency":   GetPreferredCurrency(),
		"rates":      GetAllRates(),
		"updated_at": ratesTs.Format(time.RFC3339),
	})
}

// SettingsCurrencySet updates the preferred display currency.
func SettingsCurrencySet(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	currency := ""
	if c, ok := data["currency"].(string); ok && c != "" {
		currency = strings.ToUpper(c)
	}
	if currency == "" {
		c.JSON(http.StatusOK, gin.H{"status": "error", "detail": "currency required"})
		return
	}
	cfg := store.GetConfig()
	settings, _ := cfg["settings"].(map[string]any)
	if settings == nil {
		settings = make(map[string]any)
		cfg["settings"] = settings
	}
	settings["currency"] = currency
	store.SaveConfig(cfg)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "currency": currency})
}
