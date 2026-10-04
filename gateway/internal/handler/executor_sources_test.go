package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

func seedSignalSource(t *testing.T, feeModel string) *store.SignalSource {
	t.Helper()
	src := &store.SignalSource{
		Name: "测试源-" + feeModel, Type: "webhook", Enabled: true,
		FeeModel: feeModel, FeePercent: 10, MonthlyFee: 20,
		TPSLJSON: `{}`,
	}
	if err := store.NewSignalSourceRepo().Create(src); err != nil {
		t.Fatal(err)
	}
	return src
}

func doJSON(t *testing.T, r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestExecutorUpdateSignalSource(t *testing.T) {
	src := seedSignalSource(t, "free")
	r := setupRouter()
	r.PUT("/executor/signal-sources/:id", ExecutorUpdateSignalSource)

	w := doJSON(t, r, "PUT", "/executor/signal-sources/"+src.ID,
		`{"name":"改名源","enabled":false,"fee_model":"profit_share","fee_percent":15,"tp_sl":{"tp1_pct":50,"sl_pct":5}}`)
	assertEq(t, w.Code, http.StatusOK, "update status code")

	got, err := store.NewSignalSourceRepo().GetByID(src.ID)
	assertTrue(t, err == nil && got != nil, "source should still exist")
	assertTrue(t, got.Name == "改名源", "name should be updated")
	assertTrue(t, !got.Enabled, "enabled should be updated to false")
	assertTrue(t, got.FeeModel == "profit_share" && got.FeePercent == 15, "fee fields should be updated")
	assertTrue(t, strings.Contains(got.TPSLJSON, `"tp1_pct":50`), "tp_sl should be updated")
	assertTrue(t, got.UpdatedAt >= src.UpdatedAt, "updated_at should be bumped")
}

func TestExecutorUpdateSignalSourceValidation(t *testing.T) {
	src := seedSignalSource(t, "free")
	r := setupRouter()
	r.PUT("/executor/signal-sources/:id", ExecutorUpdateSignalSource)

	// 非法 fee_model → 400
	w := doJSON(t, r, "PUT", "/executor/signal-sources/"+src.ID, `{"fee_model":"bogus"}`)
	assertEq(t, w.Code, http.StatusBadRequest, "bad fee_model should 400")

	// 切到 profit_share 但不给正费率 → 400（按合并后最终值校验）
	w = doJSON(t, r, "PUT", "/executor/signal-sources/"+src.ID, `{"fee_model":"profit_share","fee_percent":0}`)
	assertEq(t, w.Code, http.StatusBadRequest, "profit_share without fee should 400")

	// 空 name → 400
	w = doJSON(t, r, "PUT", "/executor/signal-sources/"+src.ID, `{"name":""}`)
	assertEq(t, w.Code, http.StatusBadRequest, "empty name should 400")

	// 不存在 → 404
	w = doJSON(t, r, "PUT", "/executor/signal-sources/src_nonexistent", `{"name":"x"}`)
	assertEq(t, w.Code, http.StatusNotFound, "missing source should 404")

	// 校验失败后原值未被污染
	got, _ := store.NewSignalSourceRepo().GetByID(src.ID)
	assertTrue(t, got.FeeModel == "free", "failed updates must not persist")
}

func TestExecutorDeleteSignalSource(t *testing.T) {
	repo := store.NewSignalSourceRepo()
	src := seedSignalSource(t, "fixed_monthly")
	sub := &store.SignalSourceSubscription{
		SourceID: src.ID, UserID: 77, FeeModel: "fixed_monthly", MonthlyFee: 20,
		Status: "active", CreatedAt: time.Now().UnixMilli(),
	}
	assertTrue(t, repo.CreateSubscription(sub) == nil, "seed subscription")
	bill := &store.SignalSourceBill{SourceID: src.ID, UserID: 77, BillType: "monthly_fee", Amount: 20, Status: "settled"}
	assertTrue(t, repo.CreateBill(bill) == nil, "seed bill")

	r := setupRouter()
	r.DELETE("/executor/signal-sources/:id", ExecutorDeleteSignalSource)
	w := doJSON(t, r, "DELETE", "/executor/signal-sources/"+src.ID, "")
	assertEq(t, w.Code, http.StatusOK, "delete status code")

	var resp map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "response should parse")
	assertTrue(t, resp["success"] == true, "success flag")
	assertTrue(t, resp["cancelled_subscriptions"] == 1.0, "one active subscription should be cancelled")

	// 源已删
	gone, err := repo.GetByID(src.ID)
	assertTrue(t, err != nil || gone == nil, "source row should be deleted")
	// 级联：订阅置 cancelled（不硬删，保留审计）
	subs, err := repo.ListSubscriptionsBySource(src.ID)
	assertTrue(t, err == nil && len(subs) == 1, "subscription row retained")
	assertTrue(t, subs[0].Status == "cancelled", "subscription should be cancelled, got "+subs[0].Status)
	// 账单流水保留
	bills, err := repo.ListBills(src.ID, 10)
	assertTrue(t, err == nil && len(bills) == 1, "bill audit trail must be retained")
}

func TestExecutorDeleteSignalSourceGuards(t *testing.T) {
	r := setupRouter()
	r.DELETE("/executor/signal-sources/:id", ExecutorDeleteSignalSource)

	// 内置 default 源禁删
	store.NewSignalSourceRepo().EnsureDefaultSource()
	w := doJSON(t, r, "DELETE", "/executor/signal-sources/default", "")
	assertEq(t, w.Code, http.StatusBadRequest, "default source must not be deletable")

	// 不存在 → 404
	w = doJSON(t, r, "DELETE", "/executor/signal-sources/src_nonexistent", "")
	assertEq(t, w.Code, http.StatusNotFound, "missing source should 404")
}
