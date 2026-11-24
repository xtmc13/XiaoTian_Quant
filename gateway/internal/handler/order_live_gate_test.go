package handler

import (
	"sync/atomic"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
)

// TestLiveGateExemptsCloseOrders 实盘总闸口径：平仓/减仓等降风险动作不受总闸
// 拦截（总闸关闭时也必须能退出仓位），仅入场单过 canPlaceLiveOrder。
func TestLiveGateExemptsCloseOrders(t *testing.T) {
	// 强制关闭实盘总闸（override=2），用例结束复位为无覆盖。
	atomic.StoreInt32(&liveTradingOverride, 2)
	t.Cleanup(func() { atomic.StoreInt32(&liveTradingOverride, 0) })

	const ex = "binance"

	// 平仓单（合约平多，ClosePosition=true）：豁免总闸，也不查二次确认。
	closeReq := &order.Request{
		Symbol:        "BTCUSDT",
		Side:          model.SideSell,
		OrderType:     model.TypeMarket,
		Exchange:      ex,
		MarketType:    model.MarketSwap,
		PositionSide:  model.PositionLong,
		ClosePosition: true,
		Quantity:      0.1,
	}
	if err := canPlaceLiveOrderForRequest(ex, false, closeReq); err != nil {
		t.Fatalf("平仓单应豁免实盘总闸，err=%v", err)
	}

	// 减仓单（未置 ClosePosition，但与持仓反向）：同样识别为非入场。
	reduceReq := &order.Request{
		Symbol:       "BTCUSDT",
		Side:         model.SideSell,
		OrderType:    model.TypeMarket,
		Exchange:     ex,
		MarketType:   model.MarketSwap,
		PositionSide: model.PositionLong,
		Quantity:     0.1,
	}
	if err := canPlaceLiveOrderForRequest(ex, false, reduceReq); err != nil {
		t.Fatalf("减仓单应豁免实盘总闸，err=%v", err)
	}

	// 入场单（合约开多）：总闸关闭必须被拦。
	entryReq := &order.Request{
		Symbol:       "BTCUSDT",
		Side:         model.SideBuy,
		OrderType:    model.TypeMarket,
		Exchange:     ex,
		MarketType:   model.MarketSwap,
		PositionSide: model.PositionLong,
		Quantity:     0.1,
	}
	if err := canPlaceLiveOrderForRequest(ex, false, entryReq); err == nil {
		t.Fatal("入场单在实盘总闸关闭时必须被拦")
	}

	// paper 单直通（原口径，不依赖豁免分类）。
	paperReq := &order.Request{Symbol: "BTCUSDT", Side: model.SideBuy, Exchange: "paper", Quantity: 0.1}
	if err := canPlaceLiveOrderForRequest("paper", false, paperReq); err != nil {
		t.Fatalf("paper 单应直通，err=%v", err)
	}
}
