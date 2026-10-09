package portfolio

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── H2（2026-10-09）：SeedPaperMirror 边界口径 ──
// 启动时把 paper 账户恢复快照的余额/持仓补注进 "default" 镜像账户（NewManager
// 只注入 USDT，base/持仓归零会让重启后的 paper 持仓从 Portfolio 页消失）。

func bareManager(withDefault bool) *Manager {
	m := &Manager{
		accounts:       make(map[string]*model.AccountData),
		positions:      make(map[string]*model.PositionData),
		otherExcTotals: make(map[string]float64),
	}
	if withDefault {
		m.accounts["default"] = &model.AccountData{
			ID:       "default",
			Exchange: "paper",
			Balances: map[string]*model.Balance{
				"USDT": {Currency: "USDT", Total: 100000, Free: 100000},
			},
			Positions: make(map[string]*model.PositionData),
		}
	}
	return m
}

// 快照含 base 余额/持仓 → 镜像整体对齐（USDT 也按快照真值替换，config 注入的
// 初始余额在已有历史成交后是过期值）。
func TestSeedPaperMirrorAlignsWithSnapshot(t *testing.T) {
	m := bareManager(true)
	m.SeedPaperMirror(
		map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 96119, Free: 96119},
			"SOL":  {Currency: "SOL", Total: 16.82, Free: 16.82},
		},
		[]model.PositionData{{
			ID: "SOLUSDT-spot", Symbol: "SOLUSDT", Side: "LONG",
			Quantity: 16.82, AvgEntryPrice: 119.37,
		}},
	)

	acct := m.GetAccount("default")
	if b := acct.Balances["USDT"]; b == nil || b.Free != 96119 || b.Total != 96119 {
		t.Fatalf("USDT 必须按快照真值替换: %+v", b)
	}
	if b := acct.Balances["SOL"]; b == nil || b.Free != 16.82 || b.Total != 16.82 {
		t.Fatalf("SOL base 必须补注: %+v", b)
	}
	if pos := acct.Positions["SOLUSDT-spot"]; pos == nil || pos.Quantity != 16.82 {
		t.Fatalf("持仓必须补注进账户: %+v", pos)
	}
	found := false
	for _, p := range m.GetPositions() {
		if p.ID == "SOLUSDT-spot" && p.Quantity == 16.82 {
			found = true
		}
	}
	if !found {
		t.Fatal("持仓必须出现在全局 positions（PortfolioPositions/风险视图读取口径）")
	}

	// 调用方改快照不得串改镜像（拷贝语义）。
	m.SeedPaperMirror(map[string]*model.Balance{
		"USDT": {Currency: "USDT", Total: 1, Free: 1},
	}, nil)
	if b := acct.Balances["USDT"]; b.Free != 1 {
		t.Fatalf("二次补注应替换余额表: %+v", b)
	}
	if acct.Balances["SOL"] != nil {
		t.Fatal("二次补注余额表整体替换，SOL 应消失")
	}
}

// 零行为变化边界：default 账户不存在（paper 被 config 关闭）或快照无数据
// 时不做任何事。
func TestSeedPaperMirrorZeroChangeBoundaries(t *testing.T) {
	// 无 default 账户：不创建、不 panic。
	m := bareManager(false)
	m.SeedPaperMirror(
		map[string]*model.Balance{"SOL": {Currency: "SOL", Total: 1, Free: 1}},
		[]model.PositionData{{ID: "SOLUSDT-spot", Symbol: "SOLUSDT", Quantity: 1}},
	)
	if m.GetAccount("default") != nil {
		t.Fatal("default 账户不存在时不得创建（paper 关闭口径）")
	}
	if len(m.GetPositions()) != 0 {
		t.Fatal("无 default 账户时不得注入持仓")
	}

	// 空快照：原余额分毫不动。
	m2 := bareManager(true)
	m2.SeedPaperMirror(nil, nil)
	if b := m2.GetAccount("default").Balances["USDT"]; b.Free != 100000 {
		t.Fatalf("空快照不得改动镜像: %+v", b)
	}

	// 仅持仓快照（无余额键）：持仓注入但余额保持。
	m2.SeedPaperMirror(nil, []model.PositionData{
		{ID: "BTCUSDT-spot", Symbol: "BTCUSDT", Quantity: 0.5, AvgEntryPrice: 50000},
		{ID: "zero", Symbol: "ZEROUSDT", Quantity: 0}, // 零量持仓不注入
	})
	if b := m2.GetAccount("default").Balances["USDT"]; b.Free != 100000 {
		t.Fatalf("无余额键不得动余额: %+v", b)
	}
	if m2.GetAccount("default").Positions["BTCUSDT-spot"] == nil {
		t.Fatal("仅持仓快照必须注入持仓")
	}
	if m2.GetAccount("default").Positions["zero"] != nil {
		t.Fatal("零量持仓不得注入")
	}
}
