package app

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
)

// ── 2026-10-11 生产实锤：buildRiskContext 分口径接线 ──
// 实盘单的风控上下文只读真实持仓暴露/真实权益/真实持仓数，paper 单只读
// paper 口径——paper 持仓不得泄漏进实盘风控（暴露口径的组合层锚定在
// portfolio 包 manager_exposure_split_test.go，此处锚定接线选择）。
func TestBuildRiskContextSplitsPaperAndLive(t *testing.T) {
	markMarketDataFailed() // 离线口径：跳过行情富化的网络调用

	mgr := portfolio.GetManager()
	mgr.UpdatePosition(model.PositionData{ID: "RCSPLIT-BTC-BUY", Symbol: "RCSPLITBTC", Quantity: 1, CurrentPrice: 100, Exchange: "binance"})
	mgr.UpdatePosition(model.PositionData{ID: "RCSPLIT-SOL-spot", Symbol: "RCSPLITSOL", Quantity: 2, CurrentPrice: 100, Exchange: "paper"})
	t.Cleanup(func() {
		mgr.RemovePosition(portfolio.PositionKey("binance", "RCSPLIT-BTC-BUY"))
		mgr.RemovePosition(portfolio.PositionKey("paper", "RCSPLIT-SOL-spot"))
	})

	ctx := &Context{PortfolioManager: mgr}
	req := func(exchange string) *order.Request {
		return &order.Request{
			Symbol: "RCSPLITBTC", Side: model.SideBuy, OrderType: model.TypeLimit,
			Price: 100, Quantity: 0.1, Exchange: exchange,
		}
	}

	liveCtx, _ := ctx.buildRiskContext(req("binance"))
	if liveCtx.PositionCount != mgr.LivePositionCount() {
		t.Fatalf("实盘 PositionCount = %d, want LivePositionCount %d", liveCtx.PositionCount, mgr.LivePositionCount())
	}
	if liveCtx.PositionCount == len(mgr.GetPositions()) {
		t.Fatalf("实盘 PositionCount = %d 仍是混合计数（GetPositions 全量）", liveCtx.PositionCount)
	}
	if liveCtx.NetExposure != mgr.NetExposure() || liveCtx.TotalEquity != mgr.TotalEquity() {
		t.Fatalf("实盘暴露/权益 = %v/%v, want %v/%v",
			liveCtx.NetExposure, liveCtx.TotalEquity, mgr.NetExposure(), mgr.TotalEquity())
	}

	pe := mgr.PaperEquity()
	if pe <= 0 {
		t.Fatalf("前置条件：paper 默认账户权益必须为正, got %v", pe)
	}
	paperCtx, _ := ctx.buildRiskContext(req("paper"))
	if paperCtx.TotalEquity != pe || paperCtx.NetExposure != mgr.PaperNetExposureAgainst(pe) {
		t.Fatalf("paper 暴露/权益 = %v/%v, want %v/%v",
			paperCtx.NetExposure, paperCtx.TotalEquity, mgr.PaperNetExposureAgainst(pe), pe)
	}
	if paperCtx.PositionCount != mgr.PaperPositionCount() {
		t.Fatalf("paper PositionCount = %d, want PaperPositionCount %d", paperCtx.PositionCount, mgr.PaperPositionCount())
	}

	// 空 Exchange 与 LockBalance 的 isPaperExchangeName 同口径：按 paper 处理。
	emptyCtx, _ := ctx.buildRiskContext(req(""))
	if emptyCtx.TotalEquity != pe || emptyCtx.PositionCount != mgr.PaperPositionCount() {
		t.Fatalf("空 Exchange 单必须按 paper 口径: equity=%v count=%d", emptyCtx.TotalEquity, emptyCtx.PositionCount)
	}
}
