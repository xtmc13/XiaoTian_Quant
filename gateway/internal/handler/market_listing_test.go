package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

/* ── 市场上架准入 API 测试 ──────────────────────────────────────
 * 覆盖：作者全生命周期 API、越权（非作者 403 / 非管理员 403 /
 * 未登录 401）、非法状态迁移 409、公开列表与快照序列。
 * 路由不经 AuthRequired，用注入中间件写 user_id/role（同 ai_review_test）。
 * ─────────────────────────────────────────────────────────────── */

func marketTestRouter(userID int, role string) *gin.Engine {
	r := setupRouter()
	withUser := func(h gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if userID > 0 {
				c.Set("user_id", userID)
				c.Set("role", role)
			}
			h(c)
		}
	}
	r.POST("/market/listings", withUser(MarketListingCreate))
	r.GET("/market/my-listings", withUser(MarketMyListings))
	r.POST("/market/listings/:id/submit", withUser(MarketListingSubmit))
	r.POST("/market/listings/:id/cancel", withUser(MarketListingCancel))
	r.GET("/market/listings", withUser(MarketListingList))
	r.GET("/market/listings/:id/stats", withUser(MarketListingStats))
	r.GET("/market/rules", withUser(MarketRulesGet))
	r.GET("/admin/market/listings", withUser(AdminMarketListingList))
	r.POST("/admin/market/listings/:id/approve", withUser(AdminMarketListingApprove))
	r.POST("/admin/market/listings/:id/reject", withUser(AdminMarketListingReject))
	r.POST("/admin/market/listings/:id/delist", withUser(AdminMarketListingDelist))
	r.PUT("/admin/market/rules", withUser(AdminMarketRulesPut))
	return r
}

func marketDo(t *testing.T, r *gin.Engine, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	var req *http.Request
	if body != "" {
		req, _ = http.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, _ = http.NewRequest(method, path, nil)
	}
	r.ServeHTTP(w, req)
	var parsed map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &parsed)
	return w, parsed
}

func seedMarketInstance(t *testing.T, id string, userID int) {
	t.Helper()
	now := time.Now().Unix()
	store.SaveAIBotInstance(map[string]any{
		"id": id, "user_id": userID, "name": "考核实例-" + id,
		"strategy_type": "ai_alpha", "symbol": "BTCUSDT", "market_type": "spot",
		"status": "running", "execution_mode": "paper", "config_json": "{}",
		"initial_balance": 10000, "created_at": now - 40*86400, "updated_at": now,
	})
	if store.GetAIBotInstanceByID(id, userID) == nil {
		t.Fatalf("seed instance %s failed", id)
	}
}

// createProbationListing 作者创建并直接提交考核，返回条目 id。
func createProbationListing(t *testing.T, r *gin.Engine, instanceID string) string {
	t.Helper()
	w, resp := marketDo(t, r, "POST", "/market/listings",
		fmt.Sprintf(`{"bot_instance_id":%q,"kind":"robot","name":"我的策略","fee_model":"profit_share","fee_percent":20,"submit":true}`, instanceID))
	assertEq(t, w.Code, http.StatusOK, "create listing")
	id, _ := resp["id"].(string)
	assertTrue(t, id != "", "listing id")
	assertTrue(t, resp["status"] == "probation", "status should be probation after submit")
	return id
}

func TestMarketListingAuthorLifecycle(t *testing.T) {
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	seedMarketInstance(t, "aibot-mkt-life", 701)
	r := marketTestRouter(701, "user")

	id := createProbationListing(t, r, "aibot-mkt-life")

	// 我的上架：含考核进度（probation 状态）
	w, resp := marketDo(t, r, "GET", "/market/my-listings", "")
	assertEq(t, w.Code, http.StatusOK, "my-listings")
	listings, ok := resp["listings"].([]any)
	assertTrue(t, ok && len(listings) == 1, "my-listings should have 1 entry")
	entry := listings[0].(map[string]any)
	assertTrue(t, entry["id"] == id, "listing id")
	progress, ok := entry["progress"].(map[string]any)
	assertTrue(t, ok, "probation entry should carry progress")
	assertTrue(t, progress["min_days"].(float64) == 30, "default min_days 30")
	assertTrue(t, progress["passed"] == false, "fresh probation should not pass")

	// 重复提交 → 409（非法迁移）
	w, _ = marketDo(t, r, "POST", "/market/listings/"+id+"/submit", "")
	assertEq(t, w.Code, http.StatusConflict, "re-submit should be 409")

	// 撤回考核 → draft
	w, resp = marketDo(t, r, "POST", "/market/listings/"+id+"/cancel", "")
	assertEq(t, w.Code, http.StatusOK, "cancel probation")
	assertTrue(t, resp["status"] == "draft", "status should be draft after cancel")

	// 规则读取
	w, resp = marketDo(t, r, "GET", "/market/rules", "")
	assertEq(t, w.Code, http.StatusOK, "rules get")
	assertTrue(t, resp["min_days"].(float64) == 30, "rules default")
}

func TestMarketListingPermissions(t *testing.T) {
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	seedMarketInstance(t, "aibot-mkt-perm", 702)
	author := marketTestRouter(702, "user")
	id := createProbationListing(t, author, "aibot-mkt-perm")

	// 非作者 submit/cancel → 403
	other := marketTestRouter(703, "user")
	w, _ := marketDo(t, other, "POST", "/market/listings/"+id+"/submit", "")
	assertEq(t, w.Code, http.StatusForbidden, "non-author submit should be 403")
	w, _ = marketDo(t, other, "POST", "/market/listings/"+id+"/cancel", "")
	assertEq(t, w.Code, http.StatusForbidden, "non-author cancel should be 403")

	// 非作者看 probation 快照序列 → 403
	w, _ = marketDo(t, other, "GET", "/market/listings/"+id+"/stats", "")
	assertEq(t, w.Code, http.StatusForbidden, "non-author stats on probation should be 403")

	// 非管理员调管理接口 → 403
	w, _ = marketDo(t, other, "POST", "/admin/market/listings/"+id+"/approve", "")
	assertEq(t, w.Code, http.StatusForbidden, "non-admin approve should be 403")
	w, _ = marketDo(t, other, "POST", "/admin/market/listings/"+id+"/reject", `{"reason":"x"}`)
	assertEq(t, w.Code, http.StatusForbidden, "non-admin reject should be 403")
	w, _ = marketDo(t, other, "POST", "/admin/market/listings/"+id+"/delist", `{"reason":"x"}`)
	assertEq(t, w.Code, http.StatusForbidden, "non-admin delist should be 403")
	w, _ = marketDo(t, other, "PUT", "/admin/market/rules", `{"min_days":7,"min_trades":3,"max_drawdown_pct":30}`)
	assertEq(t, w.Code, http.StatusForbidden, "non-admin rules put should be 403")

	// 未登录 → 401
	anon := marketTestRouter(0, "")
	w, _ = marketDo(t, anon, "GET", "/market/my-listings", "")
	assertEq(t, w.Code, http.StatusUnauthorized, "anonymous my-listings should be 401")
	w, _ = marketDo(t, anon, "POST", "/market/listings", `{"bot_instance_id":"x"}`)
	assertEq(t, w.Code, http.StatusUnauthorized, "anonymous create should be 401")

	// 用别人的实例创建考核 → 400（实例不属于作者）
	w, _ = marketDo(t, other, "POST", "/market/listings", `{"bot_instance_id":"aibot-mkt-perm","submit":true}`)
	assertEq(t, w.Code, http.StatusBadRequest, "create with others' instance should be 400")
}

func TestMarketAdminReviewFlow(t *testing.T) {
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	seedMarketInstance(t, "aibot-mkt-admin", 704)
	author := marketTestRouter(704, "user")
	admin := marketTestRouter(900, "admin")
	id := createProbationListing(t, author, "aibot-mkt-admin")

	// probation 直接 approve → 409（未达标/未进入待审）
	w, _ := marketDo(t, admin, "POST", "/admin/market/listings/"+id+"/approve", "")
	assertEq(t, w.Code, http.StatusConflict, "approve on probation should be 409")

	// 推进到 pending_review（cron 达标路径在 marketplace 包已测，这里直接落库）
	rec := store.NewMarketListingRepo().GetByID(id)
	rec.Status = "pending_review"
	if err := store.NewMarketListingRepo().Update(rec); err != nil {
		t.Fatalf("force pending_review: %v", err)
	}

	// 审核队列可见
	w, resp := marketDo(t, admin, "GET", "/admin/market/listings?status=pending_review", "")
	assertEq(t, w.Code, http.StatusOK, "review queue")
	found := false
	for _, item := range resp["listings"].([]any) {
		if item.(map[string]any)["id"] == id {
			found = true
		}
	}
	assertTrue(t, found, "pending_review listing should be in review queue")

	// 驳回：无原因 → 400；有原因 → rejected，作者可见原因
	w, _ = marketDo(t, admin, "POST", "/admin/market/listings/"+id+"/reject", `{}`)
	assertEq(t, w.Code, http.StatusBadRequest, "reject without reason should be 400")
	w, resp = marketDo(t, admin, "POST", "/admin/market/listings/"+id+"/reject", `{"reason":"回撤数据异常"}`)
	assertEq(t, w.Code, http.StatusOK, "reject with reason")
	assertTrue(t, resp["status"] == "rejected", "status rejected")
	w, resp = marketDo(t, author, "GET", "/market/my-listings", "")
	assertEq(t, w.Code, http.StatusOK, "my-listings")
	assertTrue(t, resp["listings"].([]any)[0].(map[string]any)["reject_reason"] == "回撤数据异常", "author should see reject reason")

	// 驳回后重新提交考核 → probation → 再推进 → approve → listed
	w, resp = marketDo(t, author, "POST", "/market/listings/"+id+"/submit", "")
	assertEq(t, w.Code, http.StatusOK, "resubmit after reject")
	assertTrue(t, resp["status"] == "probation", "resubmit → probation")
	rec = store.NewMarketListingRepo().GetByID(id)
	rec.Status = "pending_review"
	if err := store.NewMarketListingRepo().Update(rec); err != nil {
		t.Fatalf("force pending_review: %v", err)
	}
	w, resp = marketDo(t, admin, "POST", "/admin/market/listings/"+id+"/approve", "")
	assertEq(t, w.Code, http.StatusOK, "approve")
	assertTrue(t, resp["status"] == "listed", "status listed")

	// 公开列表可见且带考核通过标识；统计端点对他人开放
	w, resp = marketDo(t, marketTestRouter(705, "user"), "GET", "/market/listings", "")
	assertEq(t, w.Code, http.StatusOK, "public list")
	items := resp["listings"].([]any)
	assertTrue(t, len(items) == 1, "one listed entry")
	card := items[0].(map[string]any)
	assertTrue(t, card["probation_passed"] == true, "probation_passed badge")
	w, _ = marketDo(t, marketTestRouter(705, "user"), "GET", "/market/listings/"+id+"/stats", "")
	assertEq(t, w.Code, http.StatusOK, "listed stats visible to others")

	// 强制下架：无原因 400；有原因 → delisted，公开列表不再可见
	w, _ = marketDo(t, admin, "POST", "/admin/market/listings/"+id+"/delist", `{}`)
	assertEq(t, w.Code, http.StatusBadRequest, "delist without reason should be 400")
	w, resp = marketDo(t, admin, "POST", "/admin/market/listings/"+id+"/delist", `{"reason":"用户投诉"}`)
	assertEq(t, w.Code, http.StatusOK, "delist")
	assertTrue(t, resp["status"] == "delisted", "status delisted")
	w, resp = marketDo(t, admin, "GET", "/market/listings", "")
	assertEq(t, w.Code, http.StatusOK, "public list after delist")
	assertTrue(t, len(resp["listings"].([]any)) == 0, "delisted entry should disappear from public list")

	// 管理员更新考核规则（缩短考核期做演示）
	w, resp = marketDo(t, admin, "PUT", "/admin/market/rules", `{"min_days":7,"min_trades":3,"max_drawdown_pct":30}`)
	assertEq(t, w.Code, http.StatusOK, "rules put")
	assertTrue(t, resp["min_days"].(float64) == 7, "rules updated")
	w, _ = marketDo(t, admin, "PUT", "/admin/market/rules", `{"min_days":0,"min_trades":3,"max_drawdown_pct":30}`)
	assertEq(t, w.Code, http.StatusBadRequest, "invalid rules should be 400")
}
