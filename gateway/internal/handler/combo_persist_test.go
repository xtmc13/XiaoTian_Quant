package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// 组合配置持久化（xt_combo_configs）：创建落库 → 模拟"重启"（清内存
// registry → LoadComboConfigsFromStore 重建）仍在；更新/删除落库正确。
// DB 由 handler 包 TestMain 初始化（临时 sqlite）。

func TestComboPersistenceAcrossRestart(t *testing.T) {
	r := setupOwnershipRouter(t)
	token := ownToken(t, 9201, "combouser", "user")

	body := map[string]any{
		"name":   "persist-combo",
		"symbol": "ETHUSDT",
		"members": []any{
			map[string]any{"strategy_name": "martin", "weight": 0.6, "enabled": true},
			map[string]any{"strategy_name": "wallstreet", "weight": 0.4, "enabled": true},
		},
		"aggregation_mode": "weighted",
	}
	w, created := ownDo(t, r, http.MethodPost, "/api/combos", body, token)
	assertEq(t, w.Code, http.StatusOK, "create combo")
	cid, _ := created["id"].(string)
	if cid == "" {
		t.Fatalf("created combo missing id: %v", created)
	}
	t.Cleanup(func() {
		strategy.DeleteComboConfig(cid)
		_ = store.NewComboConfigRepo().Delete(cid)
	})

	// 落库校验：创建后库里即有完整记录。
	rec, err := store.NewComboConfigRepo().GetByID(cid)
	if err != nil || rec == nil {
		t.Fatalf("combo not persisted: rec=%v err=%v", rec, err)
	}
	if rec.Name != "persist-combo" || rec.Symbol != "ETHUSDT" || rec.AggregationMode != "weighted" {
		t.Fatalf("persisted record mismatch: %+v", rec)
	}
	if rec.UserID != 9201 {
		t.Fatalf("persisted user_id = %d, want 9201", rec.UserID)
	}
	if rec.CreatedAt <= 1e12 {
		t.Fatalf("created_at 应为 unix 毫秒, got %d", rec.CreatedAt)
	}

	// 模拟"重启"：清掉内存 registry（进程重启内存即丢），从库重建。
	strategy.DeleteComboConfig(cid)
	if strategy.GetComboConfig(cid) != nil {
		t.Fatal("registry should be empty after simulated restart")
	}
	LoadComboConfigsFromStore()

	reloaded := strategy.GetComboConfig(cid)
	if reloaded == nil {
		t.Fatal("combo should survive restart via DB reload")
	}
	if reloaded.Name != "persist-combo" || reloaded.Symbol != "ETHUSDT" ||
		reloaded.AggregationMode != "weighted" || reloaded.UserID != 9201 {
		t.Fatalf("reloaded config mismatch: %+v", reloaded)
	}
	if len(reloaded.Members) != 2 || reloaded.Members[0].StrategyName != "martin" ||
		reloaded.Members[0].Weight != 0.6 || !reloaded.Members[0].Enabled {
		t.Fatalf("reloaded members mismatch: %+v", reloaded.Members)
	}
	if reloaded.Status != "stopped" {
		t.Fatalf("reloaded status = %q, want stopped", reloaded.Status)
	}

	// 属主过滤语义不变：重启加载后他人列表仍不可见。
	tokenOther := ownToken(t, 9202, "comboother", "user")
	w, _ = ownDo(t, r, http.MethodGet, "/api/combos", nil, tokenOther)
	assertEq(t, w.Code, http.StatusOK, "other list combos")
	var arr []any
	if err := json.Unmarshal(rawBody(t, w), &arr); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if listContainsStringID(t, arr, cid) {
		t.Fatalf("other user list must not contain %s after reload", cid)
	}
}

func TestComboUpdateDeletePersisted(t *testing.T) {
	r := setupOwnershipRouter(t)
	token := ownToken(t, 9203, "comboupd", "user")

	body := map[string]any{
		"name":             "upd-combo",
		"symbol":           "BTCUSDT",
		"members":          []any{map[string]any{"strategy_name": "martin", "weight": 1, "enabled": true}},
		"aggregation_mode": "vote",
	}
	w, created := ownDo(t, r, http.MethodPost, "/api/combos", body, token)
	assertEq(t, w.Code, http.StatusOK, "create combo")
	cid, _ := created["id"].(string)
	if cid == "" {
		t.Fatalf("created combo missing id: %v", created)
	}
	t.Cleanup(func() {
		strategy.DeleteComboConfig(cid)
		_ = store.NewComboConfigRepo().Delete(cid)
	})

	// 更新：改名 + 换 symbol → 落库与内存一致。
	w, _ = ownDo(t, r, http.MethodPut, "/api/combos/"+cid,
		map[string]any{"name": "upd-combo-v2", "symbol": "SOLUSDT"}, token)
	assertEq(t, w.Code, http.StatusOK, "update combo")
	rec, err := store.NewComboConfigRepo().GetByID(cid)
	if err != nil || rec == nil {
		t.Fatalf("combo missing after update: rec=%v err=%v", rec, err)
	}
	if rec.Name != "upd-combo-v2" || rec.Symbol != "SOLUSDT" {
		t.Fatalf("update not persisted: %+v", rec)
	}
	if cfg := strategy.GetComboConfig(cid); cfg == nil || cfg.Name != "upd-combo-v2" {
		t.Fatalf("memory not updated: %+v", cfg)
	}

	// 删除：库与内存同时消失，重启加载不会复活。
	w, _ = ownDo(t, r, http.MethodDelete, "/api/combos/"+cid, nil, token)
	assertEq(t, w.Code, http.StatusOK, "delete combo")
	if rec, _ := store.NewComboConfigRepo().GetByID(cid); rec != nil {
		t.Fatalf("delete not persisted: %+v", rec)
	}
	if strategy.GetComboConfig(cid) != nil {
		t.Fatal("memory should be empty after delete")
	}
	LoadComboConfigsFromStore()
	if strategy.GetComboConfig(cid) != nil {
		t.Fatal("deleted combo must not resurrect on reload")
	}
}
