package handler

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 收益分享卡 API：属主权限 + 昵称脱敏 ─────────────────────────────

// shareSeedClosedPosition 造一条已平仓持仓（属主 userID）。
func shareSeedClosedPosition(t *testing.T, userID int64, id string) *store.PositionRecord {
	t.Helper()
	now := time.Now().UnixMilli()
	p := &store.PositionRecord{
		ID: id, UserID: userID, Symbol: "BTCUSDT", Side: "LONG",
		Quantity: 0.5, AvgEntryPrice: 60000, CurrentPrice: 66000,
		RealizedPnL: 3000, CostBasis: 30000, Exchange: "BINANCE",
		Status: "CLOSED", OpenedAt: now - 3600_000, ClosedAt: now, UpdatedAt: now,
	}
	if err := store.NewPositionRepo().Create(p); err != nil {
		t.Fatalf("seed position: %v", err)
	}
	// Create 会覆盖 OpenedAt/Status 默认值，按分享卡语义补写
	p.Status = "CLOSED"
	p.OpenedAt = now - 3600_000
	p.ClosedAt = now
	if err := store.NewPositionRepo().Update(p); err != nil {
		t.Fatalf("update position: %v", err)
	}
	return p
}

func TestShareTradeCard_PermissionAndMasking(t *testing.T) {
	owner := billingSeedUser(t, "share_owner")
	other := billingSeedUser(t, "share_other")
	p := shareSeedClosedPosition(t, int64(owner), "pos_share_1")

	r := setupRouter()
	r.GET("/share/trade/:id/card", billingAuthed(owner, "user"), ShareTradeCard)

	// 属主：200，昵称默认脱敏（首字 + ***），金额双口径齐全
	code, resp := billingGet(t, r, "/share/trade/"+p.ID+"/card")
	assertEq(t, http.StatusOK, code, "owner must get card")
	if resp["kind"] != "trade" || resp["symbol"] != "BTCUSDT" {
		t.Fatalf("card fields wrong: %v", resp)
	}
	nick, _ := resp["nickname"].(string)
	if !strings.HasSuffix(nick, "***") || strings.Contains(nick, "share_owner") {
		t.Fatalf("昵称默认必须脱敏, got %q", nick)
	}
	if resp["pnl"].(float64) != 3000 {
		t.Fatalf("pnl 绝对值缺失: %v", resp["pnl"])
	}
	if got := resp["pnl_pct"].(float64); got < 9.9 || got > 10.1 {
		t.Fatalf("pnl_pct 应为 10%%, got %v", got)
	}
	if resp["amount_mode"] != "pct" {
		t.Fatalf("默认 amount_mode 应为 pct, got %v", resp["amount_mode"])
	}

	// reveal_name=1：属主可取回真实昵称；amount=abs 切换展示口径
	code, resp = billingGet(t, r, "/share/trade/"+p.ID+"/card?reveal_name=1&amount=abs")
	assertEq(t, http.StatusOK, code, "reveal 200")
	if nick, _ := resp["nickname"].(string); nick != "share_owner" {
		t.Fatalf("reveal_name=1 必须返回真实昵称, got %q", nick)
	}
	if resp["amount_mode"] != "abs" {
		t.Fatalf("amount=abs 未生效: %v", resp["amount_mode"])
	}

	// 未平仓持仓 → 409
	open := &store.PositionRecord{ID: "pos_share_open", UserID: int64(owner), Symbol: "ETHUSDT",
		Side: "LONG", Quantity: 1, AvgEntryPrice: 3000, Status: "OPEN"}
	if err := store.NewPositionRepo().Create(open); err != nil {
		t.Fatalf("seed open position: %v", err)
	}
	code, _ = billingGet(t, r, "/share/trade/pos_share_open/card")
	assertEq(t, http.StatusConflict, code, "未平仓必须 409")

	// 非本人 → 403（admin 放行另测）
	r2 := setupRouter()
	r2.GET("/share/trade/:id/card", billingAuthed(other, "user"), ShareTradeCard)
	code, _ = billingGet(t, r2, "/share/trade/"+p.ID+"/card")
	assertEq(t, http.StatusForbidden, code, "非本人必须 403")

	// admin → 200（昵称仍默认脱敏）
	r3 := setupRouter()
	r3.GET("/share/trade/:id/card", billingAuthed(other, "admin"), ShareTradeCard)
	code, resp = billingGet(t, r3, "/share/trade/"+p.ID+"/card")
	assertEq(t, http.StatusOK, code, "admin 必须放行")
	if nick, _ := resp["nickname"].(string); !strings.HasSuffix(nick, "***") {
		t.Fatalf("admin 默认也应脱敏, got %q", nick)
	}

	// 不存在 → 404
	code, _ = billingGet(t, r, "/share/trade/pos_nope/card")
	assertEq(t, http.StatusNotFound, code, "missing must 404")
}

func TestShareBacktestCard_PermissionAndMasking(t *testing.T) {
	owner := billingSeedUser(t, "share_bt_owner")
	other := billingSeedUser(t, "share_bt_other")

	rec := &store.PortfolioBacktestRecord{
		ID: "bt_share_1", UserID: int64(owner), Name: "组合Alpha", Timeframe: "1h",
		StartTime: 1700000000000, EndTime: 1700100000000,
		InitialCapital: 10000, FinalEquity: 12500, TotalReturnPct: 25,
		MaxDrawdownPct: 8.5, SharpeRatio: 1.8, WinRate: 62.5, ProfitFactor: 1.6, TotalTrades: 42,
	}
	if err := store.NewPortfolioBacktestRepo().Create(rec); err != nil {
		t.Fatalf("seed portfolio backtest: %v", err)
	}

	r := setupRouter()
	r.GET("/share/backtest/:id/card", billingAuthed(owner, "user"), ShareBacktestCard)

	code, resp := billingGet(t, r, "/share/backtest/bt_share_1/card")
	assertEq(t, http.StatusOK, code, "owner must get card")
	if resp["kind"] != "backtest" || resp["name"] != "组合Alpha" {
		t.Fatalf("card fields wrong: %v", resp)
	}
	if resp["total_return_pct"].(float64) != 25 || resp["total_trades"].(float64) != 42 {
		t.Fatalf("指标缺失: %v", resp)
	}
	if nick, _ := resp["nickname"].(string); !strings.HasSuffix(nick, "***") {
		t.Fatalf("昵称默认必须脱敏, got %q", nick)
	}
	if resp["initial_capital"].(float64) != 10000 || resp["final_equity"].(float64) != 12500 {
		t.Fatalf("绝对值口径字段缺失: %v", resp)
	}

	// 非本人 → 403
	r2 := setupRouter()
	r2.GET("/share/backtest/:id/card", billingAuthed(other, "user"), ShareBacktestCard)
	code, _ = billingGet(t, r2, "/share/backtest/bt_share_1/card")
	assertEq(t, http.StatusForbidden, code, "非本人必须 403")

	// 不存在 → 404
	code, _ = billingGet(t, r, "/share/backtest/bt_nope/card")
	assertEq(t, http.StatusNotFound, code, "missing must 404")
}

// TestShareCardMaskNickname 脱敏函数单测：空昵称/单字/多字节字符。
func TestShareCardMaskNickname(t *testing.T) {
	cases := map[string]string{
		"":        "匿名用户",
		"  ":      "匿名用户",
		"a":       "a***",
		"小明":      "小***",
		"trader1": "t***",
	}
	for in, want := range cases {
		if got := maskShareNickname(in); got != want {
			t.Fatalf("maskShareNickname(%q) = %q, want %q", in, got, want)
		}
	}
}
