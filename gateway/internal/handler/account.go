package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/adapter"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
)

// ── Transfer ─────────────────────────────────────────────────────

type TransferRequest struct {
	From     string  `json:"from" binding:"required"`
	To       string  `json:"to" binding:"required"`
	Currency string  `json:"currency" binding:"required"`
	Amount   float64 `json:"amount" binding:"required,min=0.01"`
	// Exchange defaults to binance for backward compatibility with the
	// existing account page. Other venues reject with 501 until their
	// universal-transfer adapter is certified.
	Exchange string `json:"exchange"`
}

// Transfer moves funds between exchange wallets. The exchange is the source
// of truth: local balances are refreshed after a successful transfer and are
// never adjusted in memory on their own.
func Transfer(c *gin.Context) {
	var req TransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid request: " + err.Error()})
		return
	}

	req.From = strings.ToLower(strings.TrimSpace(req.From))
	req.To = strings.ToLower(strings.TrimSpace(req.To))
	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	req.Exchange = strings.ToLower(strings.TrimSpace(req.Exchange))
	if req.Exchange == "" {
		req.Exchange = "binance"
	}

	if req.From == req.To {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Source and destination wallets must be different"})
		return
	}
	switch req.From {
	case "spot", "futures", "funding":
	default:
		c.JSON(http.StatusNotImplemented, gin.H{"success": false, "message": fmt.Sprintf("wallet %q is not supported for transfers", req.From)})
		return
	}
	switch req.To {
	case "spot", "futures", "funding":
	default:
		c.JSON(http.StatusNotImplemented, gin.H{"success": false, "message": fmt.Sprintf("wallet %q is not supported for transfers", req.To)})
		return
	}

	if req.Exchange != "binance" {
		c.JSON(http.StatusNotImplemented, gin.H{
			"success": false,
			"message": fmt.Sprintf("%s 暂不支持应用内划转，请到交易所完成", req.Exchange),
		})
		return
	}

	apiKey, secret, _ := adapter.GetCredential("binance")
	if apiKey == "" || secret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Binance API Key 未配置，无法划转"})
		return
	}

	binance := adapter.NewBinanceAdapter(apiKey, secret, false)
	tranID, err := binance.TransferAsset(req.From, req.To, req.Currency, req.Amount)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": fmt.Sprintf("划转失败: %v（请确认 API Key 已开启万向划转权限）", err),
		})
		return
	}

	// Exchange accepted the movement; re-read balances instead of mutating
	// the in-memory ledger, so a displayed balance always has an exchange
	// counterpart.
	if mgr := portfolio.GetManager(); mgr != nil {
		mgr.SyncFromBinance()
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"tran_id": tranID,
		"message": fmt.Sprintf("已从 %s 划转 %.4f %s 到 %s", req.From, req.Amount, req.Currency, req.To),
	})
}

// ── Buy Crypto ───────────────────────────────────────────────────

type BuyRequest struct {
	Currency      string  `json:"currency" binding:"required"`
	Amount        float64 `json:"amount" binding:"required,min=1"`
	PaymentMethod string  `json:"payment_method"`
}

// BuyCrypto is intentionally not implemented: the platform does not custody
// fiat or execute fiat purchases. Returning a fabricated success used to
// mutate an in-memory balance that had no exchange counterpart.
func BuyCrypto(c *gin.Context) {
	var req BuyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid request: " + err.Error()})
		return
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"success": false,
		"message": "应用内不代办法币买币，请使用交易所的法币入口或先充值 USDT",
	})
}

// ── Swap / Exchange ─────────────────────────────────────────────

type SwapRequest struct {
	FromCurrency string  `json:"from_currency" binding:"required"`
	ToCurrency   string  `json:"to_currency" binding:"required"`
	Amount       float64 `json:"amount" binding:"required,min=0.001"`
}

// SwapCurrency is intentionally not implemented: swapping is a real spot
// trade and must go through the order path (fees, precision, fill state),
// not an in-memory balance rewrite.
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
	c.JSON(http.StatusNotImplemented, gin.H{
		"success": false,
		"message": "应用内不代币币兑换，请使用现货下单（会自动走手续费与成交回报）",
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
