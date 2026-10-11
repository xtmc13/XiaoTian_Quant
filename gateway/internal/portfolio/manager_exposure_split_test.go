package portfolio

import (
	"math"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── 2026-10-11 生产实锤：paper 持仓泄漏进实盘风控暴露 ──
// 实盘信号单被拒 "net exposure 224.4% exceeds limit 80.0%"：真实持仓合计
// $67.73（净值 $479.63，真实暴露仅 14.1%），但 NetExposureAgainst 对
// m.positions 全表求和，把 paper 持仓 ~$1177 也计入分子（混合口径 259.6%≈
// 风控读数）。口径必须按账户类型隔离：实盘只合计真实持仓，paper 只合计
// paper 持仓，持仓数同理；空 Exchange 的历史持仓视同 paper（与 app 侧
// isPaperExchangeName 同口径）。

func seedMixedLedger(m *Manager) {
	// 真实持仓合计 $67.73（实盘成交镜像）。
	m.UpdatePosition(model.PositionData{ID: "BTCUSDT-BUY", Symbol: "BTCUSDT", Quantity: 0.0005, CurrentPrice: 100000, Exchange: "binance"})
	m.UpdatePosition(model.PositionData{ID: "ETHUSDT-BUY", Symbol: "ETHUSDT", Quantity: 0.005, CurrentPrice: 3546, Exchange: "binance"})
	// paper 持仓合计 $1177（含未打标的历史持仓：空 Exchange）。
	m.UpdatePosition(model.PositionData{ID: "SOLUSDT-spot", Symbol: "SOLUSDT", Quantity: 11.77, CurrentPrice: 100, Exchange: "paper"})
	m.UpdatePosition(model.PositionData{ID: "LEGACYUSDT-spot", Symbol: "LEGACYUSDT", Quantity: 1, CurrentPrice: 0})
}

func TestNetExposureAgainstMixedLedgerLiveOnly(t *testing.T) {
	m := bareManager(true)
	m.spotTotalUSDT = 479.63
	seedMixedLedger(m)

	want := 67.73 / 479.63 * 100 // ≈14.12%
	if got := m.NetExposureAgainst(479.63); math.Abs(got-want) > 0.01 {
		t.Fatalf("实盘口径暴露 = %.2f%%, want ≈%.2f%%（259.6%% 即生产误拒的混合读数）", got, want)
	}
	if got := m.NetExposure(); math.Abs(got-want) > 0.01 {
		t.Fatalf("NetExposure（实盘默认口径）= %.2f%%, want ≈%.2f%%", got, want)
	}
}

func TestPaperExposureAndCountSplit(t *testing.T) {
	m := bareManager(true)
	m.spotTotalUSDT = 479.63
	seedMixedLedger(m)

	if got := m.PaperNetExposureAgainst(10000); math.Abs(got-11.77) > 0.01 {
		t.Fatalf("paper 口径暴露 = %.2f%%, want ≈11.77%%（只合计 paper 持仓 $1177）", got)
	}
	if got := m.LivePositionCount(); got != 2 {
		t.Fatalf("LivePositionCount = %d, want 2", got)
	}
	if got := m.PaperPositionCount(); got != 2 {
		t.Fatalf("PaperPositionCount = %d, want 2（含空 Exchange 历史持仓）", got)
	}
	if got := len(m.GetPositions()); got != 4 {
		t.Fatalf("GetPositions（展示口径）= %d, want 4（不过滤）", got)
	}
	if got := m.PaperNetExposureAgainst(0); got != 0 {
		t.Fatalf("零权益保护 = %v, want 0", got)
	}
}

func TestSeedPaperMirrorTagsPaperOrigin(t *testing.T) {
	m := bareManager(true)
	m.SeedPaperMirror(nil, []model.PositionData{
		{ID: "SOLUSDT-spot", Symbol: "SOLUSDT", Quantity: 16.82, AvgEntryPrice: 119.37, CurrentPrice: 119.37},
	})
	pos := m.GetAccount("default").Positions["SOLUSDT-spot"]
	if pos == nil || pos.Exchange != "paper" {
		t.Fatalf("paper 恢复持仓必须打标 paper: %+v", pos)
	}
	if got := m.LivePositionCount(); got != 0 {
		t.Fatalf("paper 补注持仓不得计入实盘口径: LivePositionCount = %d", got)
	}
	if got := m.PaperPositionCount(); got != 1 {
		t.Fatalf("PaperPositionCount = %d, want 1", got)
	}
}
