package handler

import (
	"encoding/json"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/paper"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── H2（2026-10-09）：重启恢复时 PortfolioManager 镜像与 paper 账户一致 ──
//
// 修复前缺口：RestorePaperAccount 只恢复 paper 账户（持久化真值），
// PortfolioManager 内存镜像仍只有 NewManager 注入的 USDT 初始余额——base
// 资产归零、持仓为空，Portfolio 页/风险视图（PortfolioSummary/Positions、
// MarginUsed、NetExposure）重启后把已恢复持仓显示为"不存在"。
// 修复：RestorePaperAccount 恢复后把同一账户状态补注进镜像。

// withMirrorState 保存/还原 PortfolioManager "default" 账户（余额表整体 +
// 持仓），防止补注测试污染同包其他用例共享的全局镜像。
func withMirrorState(t *testing.T) {
	t.Helper()
	mgr := portfolio.GetManager()
	acct := mgr.GetAccount("default")
	if acct == nil {
		return
	}
	origBalances := make(map[string]*model.Balance, len(acct.Balances))
	for k, v := range acct.Balances {
		cp := *v
		origBalances[k] = &cp
	}
	origPositions := make([]model.PositionData, 0, len(acct.Positions))
	for _, p := range acct.Positions {
		origPositions = append(origPositions, *p)
	}
	t.Cleanup(func() {
		mgr.SeedPaperMirror(origBalances, nil)
		cur := mgr.GetAccount("default")
		for id := range cur.Positions {
			if _, ok := keepPos(origPositions, id); !ok {
				mgr.RemovePosition(id)
			}
		}
	})
}

func keepPos(list []model.PositionData, id string) (*model.PositionData, bool) {
	for i := range list {
		if list[i].ID == id {
			return &list[i], true
		}
	}
	return nil, false
}

func TestRestorePaperAccountSeedsMirror(t *testing.T) {
	withMirrorState(t)
	pe := paper.GetPaperExchange()
	origSnap := pe.SnapshotAccount()
	origJSON := store.GetPaperAccountJSON()
	t.Cleanup(func() {
		pe.RestoreAccount(origSnap)
		_ = store.SavePaperAccountJSON(origJSON)
	})

	snap := paper.AccountSnapshot{
		Enabled: true, Balance: 96119, InitialBalance: 100000,
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 96119, Free: 96119},
			"SOL":  {Currency: "SOL", Total: 16.82, Free: 16.82},
		},
		Positions: []paper.PositionSnapshot{{Data: model.PositionData{
			ID: "SOLUSDT-spot", Symbol: "SOLUSDT", Side: "LONG",
			Quantity: 16.82, AvgEntryPrice: 119.37,
		}}},
	}
	b, _ := json.Marshal(snap)
	if err := store.SavePaperAccountJSON(string(b)); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	RestorePaperAccount()

	// paper 账户恢复（既有行为）。
	if got := pe.FreeBalance("SOL"); got != 16.82 {
		t.Fatalf("paper SOL free = %v, want 16.82", got)
	}
	// H2 新增：镜像与 paper 账户一致——base 余额、USDT 真值、持仓全部对齐。
	mgr := portfolio.GetManager()
	acct := mgr.GetAccount("default")
	if acct == nil {
		t.Fatal("default 账户必须存在")
	}
	if b := acct.Balances["SOL"]; b == nil || b.Free != 16.82 || b.Total != 16.82 {
		t.Fatalf("镜像 SOL 必须随恢复补注: %+v", b)
	}
	if b := acct.Balances["USDT"]; b == nil || b.Free != 96119 {
		t.Fatalf("镜像 USDT 必须对齐 paper 真值（而非 config 初始 100000）: %+v", b)
	}
	pos := acct.Positions["SOLUSDT-spot"]
	if pos == nil || pos.Quantity != 16.82 || pos.AvgEntryPrice != 119.37 {
		t.Fatalf("镜像持仓必须随恢复补注: %+v", pos)
	}
	if mgr.PaperEquity() != 96119+16.82 {
		t.Fatalf("PaperEquity = %v, want %v", mgr.PaperEquity(), 96119+16.82)
	}
}

// 无持久化快照（全新部署）→ RestorePaperAccount 早退，镜像保持 NewManager
// 原样（零行为变化边界）。
func TestRestorePaperAccountNoSnapshotLeavesMirror(t *testing.T) {
	withMirrorState(t)
	pe := paper.GetPaperExchange()
	origSnap := pe.SnapshotAccount()
	origJSON := store.GetPaperAccountJSON()
	t.Cleanup(func() {
		pe.RestoreAccount(origSnap)
		_ = store.SavePaperAccountJSON(origJSON)
	})

	mgr := portfolio.GetManager()
	acct := mgr.GetAccount("default")
	_, usdtFreeBefore, _ := func() (float64, float64, float64) {
		b := acct.Balances["USDT"]
		return b.Total, b.Free, b.Used
	}()

	// 置空快照 = 无历史数据。
	if err := store.SavePaperAccountJSON(""); err != nil {
		t.Fatalf("clear snapshot: %v", err)
	}
	RestorePaperAccount()

	if b := mgr.GetAccount("default").Balances["USDT"]; b.Free != usdtFreeBefore {
		t.Fatalf("无快照时镜像不得变化: %v → %v", usdtFreeBefore, b.Free)
	}
	if b := mgr.GetAccount("default").Balances["SOL"]; b != nil && b.Total != 0 {
		t.Fatalf("无快照不得凭空补注 base: %+v", b)
	}
}
