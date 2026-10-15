package portfolio

import (
	"math"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── 2026-10-11 生产形态：同一 symbol+side 的 paper 持仓与实盘持仓并存 ──
// 修复前 m.positions 以裸 posID（symbol-side）为键，paper 与实盘互相覆盖，
// 两个账户的仓位只剩一份，暴露分口径过滤无从拆分（它们共享同一行）。
// 键空间必须带账户维度：exchange:posID，空 exchange 视同 paper。

func TestPositionKeyAccountScope(t *testing.T) {
	if got := PositionKey("binance", "SOLUSDT-LONG"); got != "binance:SOLUSDT-LONG" {
		t.Fatalf("PositionKey = %q", got)
	}
	if got := PositionKey("", "SOLUSDT-spot"); got != "paper:SOLUSDT-spot" {
		t.Fatalf("空 exchange 必须归一化为 paper, got %q", got)
	}
}

func TestSameSymbolSidePaperAndLiveCoexist(t *testing.T) {
	m := bareManager(true)
	m.UpdatePosition(model.PositionData{ID: "SOLUSDT-BUY", Symbol: "SOLUSDT", Side: "BUY", Quantity: 3.379, AvgEntryPrice: 100, CurrentPrice: 100, Exchange: "paper"})
	m.UpdatePosition(model.PositionData{ID: "SOLUSDT-BUY", Symbol: "SOLUSDT", Side: "BUY", Quantity: 0.5, AvgEntryPrice: 120, CurrentPrice: 120, Exchange: "binance"})

	if got := len(m.GetPositions()); got != 2 {
		t.Fatalf("同键不同账户必须并存为 2 行, got %d（互覆盖即回归）", got)
	}
	acct := m.GetAccount("default")
	paperPos := acct.Positions[PositionKey("paper", "SOLUSDT-BUY")]
	livePos := acct.Positions[PositionKey("binance", "SOLUSDT-BUY")]
	if paperPos == nil || paperPos.Quantity != 3.379 {
		t.Fatalf("paper 持仓丢失或被覆盖: %+v", paperPos)
	}
	if livePos == nil || livePos.Quantity != 0.5 {
		t.Fatalf("实盘持仓丢失或被覆盖: %+v", livePos)
	}

	// 各增各减：paper 加仓不影响实盘行，反之亦然。
	m.UpdatePosition(model.PositionData{ID: "SOLUSDT-BUY", Symbol: "SOLUSDT", Side: "BUY", Quantity: 5, AvgEntryPrice: 101, CurrentPrice: 101, Exchange: "paper"})
	if livePos.Quantity != 0.5 || livePos.AvgEntryPrice != 120 {
		t.Fatalf("paper 加仓串改实盘行: %+v", livePos)
	}
	m.UpdatePosition(model.PositionData{ID: "SOLUSDT-BUY", Symbol: "SOLUSDT", Side: "BUY", Quantity: 0.25, AvgEntryPrice: 130, CurrentPrice: 130, Exchange: "binance"})
	if got := acct.Positions[PositionKey("paper", "SOLUSDT-BUY")]; got == nil || got.Quantity != 5 {
		t.Fatalf("实盘加仓串改 paper 行: %+v", got)
	}

	// 平仓只动对应账户：删掉 paper 行，实盘行原样。
	m.RemovePosition(PositionKey("paper", "SOLUSDT-BUY"))
	if got := acct.Positions[PositionKey("paper", "SOLUSDT-BUY")]; got != nil {
		t.Fatalf("paper 行未被删除: %+v", got)
	}
	if got := acct.Positions[PositionKey("binance", "SOLUSDT-BUY")]; got == nil || got.Quantity != 0.25 {
		t.Fatalf("删除 paper 行误伤实盘行: %+v", got)
	}
	if got := len(m.GetPositions()); got != 1 {
		t.Fatalf("删除后 GetPositions = %d, want 1", got)
	}
}

// 共存场景下暴露分口径：同一 symbol 的 paper/实盘持仓各归各的分子。
func TestExposureSplitWithSameSymbolCoexistence(t *testing.T) {
	m := bareManager(true)
	m.UpdatePosition(model.PositionData{ID: "SOLUSDT-BUY", Symbol: "SOLUSDT", Quantity: 10, CurrentPrice: 100, Exchange: "paper"})
	m.UpdatePosition(model.PositionData{ID: "SOLUSDT-BUY", Symbol: "SOLUSDT", Quantity: 1, CurrentPrice: 100, Exchange: "binance"})

	if got := m.NetExposureAgainst(500); math.Abs(got-20) > 1e-9 {
		t.Fatalf("实盘口径 = %.2f%%, want 20%%（只计实盘 $100）", got)
	}
	if got := m.PaperNetExposureAgainst(10000); math.Abs(got-10) > 1e-9 {
		t.Fatalf("paper 口径 = %.2f%%, want 10%%（只计 paper $1000）", got)
	}
	if got := m.LivePositionCount(); got != 1 {
		t.Fatalf("LivePositionCount = %d, want 1", got)
	}
	if got := m.PaperPositionCount(); got != 1 {
		t.Fatalf("PaperPositionCount = %d, want 1", got)
	}
}

// 空 Exchange 的历史持仓与显式 paper 同键（归一化口径），不得另立一行。
func TestEmptyExchangeSharesPaperKey(t *testing.T) {
	m := bareManager(true)
	m.UpdatePosition(model.PositionData{ID: "BTCUSDT-BUY", Symbol: "BTCUSDT", Quantity: 1, CurrentPrice: 100})
	m.UpdatePosition(model.PositionData{ID: "BTCUSDT-BUY", Symbol: "BTCUSDT", Quantity: 2, CurrentPrice: 100, Exchange: "paper"})
	if got := len(m.GetPositions()); got != 1 {
		t.Fatalf("空 exchange 与 paper 必须同键覆盖, got %d 行", got)
	}
	pos := m.GetAccount("default").Positions[PositionKey("paper", "BTCUSDT-BUY")]
	if pos == nil || pos.Quantity != 2 {
		t.Fatalf("同键覆盖后数量 = %+v, want 2", pos)
	}
	if pos.Exchange != "paper" {
		t.Fatalf("空 exchange 必须归一打标 paper, got %q", pos.Exchange)
	}
}
