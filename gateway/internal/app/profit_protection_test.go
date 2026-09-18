package app

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/logging"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/risk"
)

// TestMaybeProtectProfit 盈利保护触发链路：开关+实盘+swap+已实现利润≥1U 才
// 触发划转；paper/现货/关闭/小额一律不触发。
func TestMaybeProtectProfit(t *testing.T) {
	orig := profitTransferFn
	t.Cleanup(func() {
		profitTransferFn = orig
		risk.SetProfitProtectionEnabled(false)
	})

	ctx := &Context{Logger: logging.New("test")}

	tests := []struct {
		name     string
		enabled  bool
		ord      *model.OrderData
		wantCall bool
		wantAmt  float64
	}{
		{"live swap 盈利触发", true, &model.OrderData{Symbol: "BTCUSDT", Exchange: "binance", MarketType: model.MarketSwap, RealizedPnL: 5.25}, true, 5.25},
		{"paper 单绝不划转", true, &model.OrderData{Symbol: "BTCUSDT", Exchange: "paper", MarketType: model.MarketSwap, RealizedPnL: 9.99}, false, 0},
		{"空 exchange 视为 paper", true, &model.OrderData{Symbol: "BTCUSDT", Exchange: "", MarketType: model.MarketSwap, RealizedPnL: 9.99}, false, 0},
		{"现货不划转", true, &model.OrderData{Symbol: "BTCUSDT", Exchange: "binance", MarketType: model.MarketType("spot"), RealizedPnL: 9.99}, false, 0},
		{"开关关闭不触发", false, &model.OrderData{Symbol: "BTCUSDT", Exchange: "binance", MarketType: model.MarketSwap, RealizedPnL: 9.99}, false, 0},
		{"小于 1U 最小额保护", true, &model.OrderData{Symbol: "BTCUSDT", Exchange: "binance", MarketType: model.MarketSwap, RealizedPnL: 0.5}, false, 0},
		{"亏损不划转", true, &model.OrderData{Symbol: "BTCUSDT", Exchange: "binance", MarketType: model.MarketSwap, RealizedPnL: -3}, false, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := make(chan float64, 1)
			profitTransferFn = func(amount float64) error {
				called <- amount
				return nil
			}
			risk.SetProfitProtectionEnabled(tc.enabled)

			ctx.maybeProtectProfit(tc.ord)

			if !tc.wantCall {
				select {
				case amt := <-called:
					t.Fatalf("must not transfer, got amount %v", amt)
				case <-time.After(50 * time.Millisecond):
				}
				return
			}
			select {
			case amt := <-called:
				if amt != tc.wantAmt {
					t.Fatalf("transfer amount = %v, want %v", amt, tc.wantAmt)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("expected transfer call")
			}
		})
	}
}
