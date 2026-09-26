package handler

import (
	"net/http"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 已平仓持仓列表 API：属主隔离 + 分页 ─────────────────────────────

func TestClosedPositions_OwnerScopeAndPaging(t *testing.T) {
	owner := billingSeedUser(t, "closed_owner")
	other := billingSeedUser(t, "closed_other")

	// 属主 3 条已平仓 + 1 条未平仓（未平仓不得出现）；他人 1 条已平仓
	for i, id := range []string{"pos_closed_a", "pos_closed_b", "pos_closed_c"} {
		p := shareSeedClosedPosition(t, int64(owner), id)
		// 拉开平仓时间，保证 closed_at DESC 分页顺序确定
		p.ClosedAt += int64(i * 1000)
		if err := store.NewPositionRepo().Update(p); err != nil {
			t.Fatalf("bump closed_at: %v", err)
		}
	}
	open := &store.PositionRecord{ID: "pos_closed_open", UserID: int64(owner), Symbol: "ETHUSDT",
		Side: "LONG", Quantity: 1, AvgEntryPrice: 3000, Status: "OPEN"}
	if err := store.NewPositionRepo().Create(open); err != nil {
		t.Fatalf("seed open position: %v", err)
	}
	shareSeedClosedPosition(t, int64(other), "pos_closed_other")

	r := setupRouter()
	r.GET("/positions/closed", billingAuthed(owner, "user"), ClosedPositions)

	// 属主：仅本人已平仓，分页字段齐全
	code, resp := billingGet(t, r, "/positions/closed")
	assertEq(t, http.StatusOK, code, "owner must list closed positions")
	positions, _ := resp["positions"].([]any)
	if len(positions) != 3 {
		t.Fatalf("属主应见 3 条本人已平仓, got %d: %v", len(positions), positions)
	}
	first, _ := positions[0].(map[string]any)
	if first["id"] == nil || first["symbol"] != "BTCUSDT" || first["closed_at"] == nil {
		t.Fatalf("行字段缺失: %v", first)
	}
	if got := first["pnl_pct"].(float64); got < 9.9 || got > 10.1 {
		t.Fatalf("pnl_pct 应为 10%%, got %v", got)
	}
	if resp["has_more"].(bool) != false {
		t.Fatalf("3 条不足一页, has_more 应为 false")
	}

	// 分页：limit=2 → 第一页 2 条 has_more=true，第二页 1 条
	code, resp = billingGet(t, r, "/positions/closed?limit=2")
	assertEq(t, http.StatusOK, code, "page 1")
	page1, _ := resp["positions"].([]any)
	if len(page1) != 2 || resp["has_more"].(bool) != true {
		t.Fatalf("第一页应 2 条且 has_more=true, got %d/%v", len(page1), resp["has_more"])
	}
	code, resp = billingGet(t, r, "/positions/closed?limit=2&offset=2")
	assertEq(t, http.StatusOK, code, "page 2")
	page2, _ := resp["positions"].([]any)
	if len(page2) != 1 || resp["has_more"].(bool) != false {
		t.Fatalf("第二页应 1 条且 has_more=false, got %d/%v", len(page2), resp["has_more"])
	}
	// 两页不重叠
	p1id, _ := page1[0].(map[string]any)["id"].(string)
	p2id, _ := page2[0].(map[string]any)["id"].(string)
	if p1id == "" || p1id == p2id {
		t.Fatalf("分页结果重叠: %q vs %q", p1id, p2id)
	}

	// 他人：看不到属主的持仓（只能见自己的 1 条）
	r2 := setupRouter()
	r2.GET("/positions/closed", billingAuthed(other, "user"), ClosedPositions)
	code, resp = billingGet(t, r2, "/positions/closed")
	assertEq(t, http.StatusOK, code, "other user 200")
	otherPositions, _ := resp["positions"].([]any)
	if len(otherPositions) != 1 {
		t.Fatalf("他人应仅见本人 1 条已平仓, got %d", len(otherPositions))
	}
	if got, _ := otherPositions[0].(map[string]any)["id"].(string); got != "pos_closed_other" {
		t.Fatalf("越权数据泄露: %v", got)
	}

	// admin：见全部已平仓（owner 3 + other 1）
	r3 := setupRouter()
	r3.GET("/positions/closed", billingAuthed(other, "admin"), ClosedPositions)
	code, resp = billingGet(t, r3, "/positions/closed")
	assertEq(t, http.StatusOK, code, "admin 200")
	all, _ := resp["positions"].([]any)
	if len(all) != 4 {
		t.Fatalf("admin 应见全部 4 条已平仓, got %d", len(all))
	}
}
