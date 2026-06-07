package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
)

// ── Transfer ─────────────────────────────────────────────────────

type TransferRequest struct {
	From     string  `json:"from" binding:"required"`
	To       string  `json:"to" binding:"required"`
	Currency string  `json:"currency" binding:"required"`
	Amount   float64 `json:"amount" binding:"required,min=0.01"`
}

// Transfer handles internal fund transfers between wallets (spot/futures/funding).
func Transfer(c *gin.Context) {
	var req TransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid request: " + err.Error()})
		return
	}

	req.From = strings.ToLower(req.From)
	req.To = strings.ToLower(req.To)

	validWallets := map[string]bool{"spot": true, "futures": true, "funding": true, "earn": true}
	if !validWallets[req.From] || !validWallets[req.To] {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid wallet type. Must be: spot, futures, funding, earn"})
		return
	}
	if req.From == req.To {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Source and destination wallets must be different"})
		return
	}

	mgr := portfolio.GetManager()
	if mgr == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Portfolio manager not available"})
		return
	}

	// Simulate transfer by updating balances in the portfolio manager
	// In production, this would call the exchange API
	fromAccount := mgr.GetAccount("binance_" + req.From)
	toAccount := mgr.GetAccount("binance_" + req.To)

	if fromAccount == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": fmt.Sprintf("Source wallet '%s' not found or not connected", req.From)})
		return
	}

	bal, ok := fromAccount.Balances[req.Currency]
	if !ok || bal.Free < req.Amount {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": fmt.Sprintf("Insufficient %s balance in %s wallet (available: %.4f)", req.Currency, req.From, bal.Free)})
		return
	}

	// Deduct from source
	mgr.UpdateBalance("binance_"+req.From, req.Currency, bal.Total-req.Amount, bal.Free-req.Amount, bal.Used)

	// Add to destination (create if not exists)
	if toAccount != nil {
		if toBal, ok := toAccount.Balances[req.Currency]; ok {
			mgr.UpdateBalance("binance_"+req.To, req.Currency, toBal.Total+req.Amount, toBal.Free+req.Amount, toBal.Used)
		} else {
			toAccount.Balances[req.Currency] = &model.Balance{
				Currency: req.Currency,
				Total:    req.Amount,
				Free:     req.Amount,
				Used:     0,
			}
		}
	}

	log.Printf("[Transfer] %.4f %s from %s → %s", req.Amount, req.Currency, req.From, req.To)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Successfully transferred %.4f %s from %s to %s", req.Amount, req.Currency, req.From, req.To),
	})
}

// ── Buy Crypto ───────────────────────────────────────────────────

type BuyRequest struct {
	Currency      string  `json:"currency" binding:"required"`
	Amount        float64 `json:"amount" binding:"required,min=1"`
	PaymentMethod string  `json:"payment_method"`
}

// BuyCrypto simulates buying cryptocurrency with fiat (USDT as placeholder).
func BuyCrypto(c *gin.Context) {
	var req BuyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid request: " + err.Error()})
		return
	}

	req.Currency = strings.ToUpper(req.Currency)
	if req.PaymentMethod == "" {
		req.PaymentMethod = "credit_card"
	}

	// Simulate: buy crypto at estimated market price
	price := 1.0
	if req.Currency != "USDT" {
		if p := fetchBinancePrice(req.Currency + "USDT"); p > 0 {
			price = p
		} else {
			price = 1.0 // fallback
		}
	}

	quantity := req.Amount / price

	mgr := portfolio.GetManager()
	if mgr != nil {
		acct := mgr.GetAccount("binance_spot")
		if acct != nil {
			if bal, ok := acct.Balances["USDT"]; ok {
				mgr.UpdateBalance("binance_spot", "USDT", bal.Total-req.Amount, bal.Free-req.Amount, bal.Used)
			}
			if existing, ok := acct.Balances[req.Currency]; ok {
				mgr.UpdateBalance("binance_spot", req.Currency, existing.Total+quantity, existing.Free+quantity, existing.Used)
			} else {
				acct.Balances[req.Currency] = &model.Balance{
					Currency: req.Currency,
					Total:    quantity,
					Free:     quantity,
					Used:     0,
				}
			}
		}
	}

	orderID := fmt.Sprintf("buy_%s_%d", req.Currency, time.Now().UnixMilli())
	log.Printf("[Buy] %s %.4f @ %.2f USDT each (method: %s)", req.Currency, quantity, price, req.PaymentMethod)

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"order_id": orderID,
		"message":  fmt.Sprintf("Successfully purchased %.6f %s for %.2f USDT", quantity, req.Currency, req.Amount),
	})
}

// ── Swap / Exchange ─────────────────────────────────────────────

type SwapRequest struct {
	FromCurrency string  `json:"from_currency" binding:"required"`
	ToCurrency   string  `json:"to_currency" binding:"required"`
	Amount       float64 `json:"amount" binding:"required,min=0.001"`
}

// SwapCurrency exchanges one cryptocurrency for another at estimated market rates.
func SwapCurrency(c *gin.Context) {
	var req SwapRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid request: " + err.Error()})
		return
	}

	req.FromCurrency = strings.ToUpper(req.FromCurrency)
	req.ToCurrency = strings.ToUpper(req.ToCurrency)

	if req.FromCurrency == req.ToCurrency {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Cannot swap to the same currency"})
		return
	}

	// Get prices from Binance
	var fromPrice, toPrice float64
	if req.FromCurrency == "USDT" {
		fromPrice = 1.0
	} else {
		fromPrice = fetchBinancePrice(req.FromCurrency + "USDT")
	}
	if req.ToCurrency == "USDT" {
		toPrice = 1.0
	} else {
		toPrice = fetchBinancePrice(req.ToCurrency + "USDT")
	}

	if fromPrice <= 0 || toPrice <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Unable to fetch prices for swap"})
		return
	}

	usdtValue := req.Amount * fromPrice
	outputAmount := usdtValue / toPrice
	rate := toPrice / fromPrice

	mgr := portfolio.GetManager()
	if mgr != nil {
		acct := mgr.GetAccount("binance_spot")
		if acct != nil {
			if srcBal, ok := acct.Balances[req.FromCurrency]; ok {
				mgr.UpdateBalance("binance_spot", req.FromCurrency, srcBal.Total-req.Amount, srcBal.Free-req.Amount, srcBal.Used)
			}
			if dstBal, ok := acct.Balances[req.ToCurrency]; ok {
				mgr.UpdateBalance("binance_spot", req.ToCurrency, dstBal.Total+outputAmount, dstBal.Free+outputAmount, dstBal.Used)
			} else {
				acct.Balances[req.ToCurrency] = &model.Balance{
					Currency: req.ToCurrency,
					Total:    outputAmount,
					Free:     outputAmount,
					Used:     0,
				}
			}
		}
	}

	orderID := fmt.Sprintf("swap_%s_%s_%d", req.FromCurrency, req.ToCurrency, time.Now().UnixMilli())
	log.Printf("[Swap] %.6f %s → %.6f %s (rate: %.8f)", req.Amount, req.FromCurrency, outputAmount, req.ToCurrency, rate)

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"order_id": orderID,
		"rate":     rate,
		"message":  fmt.Sprintf("Swapped %.6f %s → %.6f %s at rate %.8f", req.Amount, req.FromCurrency, outputAmount, req.ToCurrency, rate),
	})
}

// fetchBinancePrice fetches the current price from Binance public API.
func fetchBinancePrice(symbol string) float64 {
	resp, err := http.Get("https://api.binance.com/api/v3/ticker/price?symbol=" + symbol)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	var result struct {
		Price string `json:"price"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0
	}
	var price float64
	if _, err := fmt.Sscanf(result.Price, "%f", &price); err != nil {
		return 0
	}
	return price
}
