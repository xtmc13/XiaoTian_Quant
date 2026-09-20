package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

/* ── AI 复盘报告测试 ────────────────────────────────────────────
 * provider mock：aiReviewGetProvider 是包级变量入口，替换为指向
 * httptest server 的 fake provider（OpenAI 兼容响应）；无 provider
 * 场景替换为返回 nil。隔离手法对齐 trading_safety_test.go 的
 * 包级状态保存/恢复（t.Cleanup）。
 * ─────────────────────────────────────────────────────────────── */

// mockAIReviewProvider 注入返回固定文案的 fake provider，并捕获最后一次请求体。
func mockAIReviewProvider(t *testing.T, reply string) *string {
	t.Helper()
	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		lastBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ai.CompletionResponse{
			ID:    "chatcmpl-test",
			Model: "review-model",
			Choices: []ai.Choice{{
				Index:   0,
				Message: ai.ChatMessage{Role: ai.RoleAssistant, Content: reply},
			}},
		})
	}))
	ai.RegisterProvider(ai.Provider{Name: "ai-review-test", BaseURL: srv.URL, APIKey: "sk-test", Model: "review-model"})
	old := aiReviewGetProvider
	aiReviewGetProvider = func() *ai.Provider { return ai.GetProvider("ai-review-test") }
	t.Cleanup(func() {
		aiReviewGetProvider = old
		srv.Close()
	})
	return &lastBody
}

// disableAIReviewProvider 注入"未配置 provider"场景。
func disableAIReviewProvider(t *testing.T) {
	t.Helper()
	old := aiReviewGetProvider
	aiReviewGetProvider = func() *ai.Provider { return nil }
	t.Cleanup(func() { aiReviewGetProvider = old })
}

func aiReviewRouter(userID int) *gin.Engine {
	r := setupRouter()
	withUser := func(h gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Set("user_id", userID)
			c.Set("role", "user")
			h(c)
		}
	}
	r.POST("/ai/review", withUser(AIReviewGenerate))
	r.GET("/ai/review/reports", withUser(AIReviewReportsList))
	r.GET("/ai/review/reports/:id", withUser(AIReviewReportGet))
	return r
}

func postAIReview(t *testing.T, r *gin.Engine, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/ai/review", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	var parsed map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &parsed) == nil, "response should be JSON: "+w.Body.String())
	return w, parsed
}

// 生成成功：strategy scope，真实成交 FIFO 配对统计正确，报告落库 status=done。
func TestAIReviewGenerateSuccess(t *testing.T) {
	lastBody := mockAIReviewProvider(t, "一、总体评价：表现稳健\n二、做得好的点：纪律执行\n三、问题与坏习惯：样本偏少\n四、可执行的改进建议：扩大样本")

	uid := int64(101)
	now := time.Now().UnixMilli()
	assertTrue(t, store.NewStrategyConfigRepo().Create(&store.StrategyConfigRecord{
		ID:     "st-review-ok",
		UserID: uid,
		Name:   "复盘测试策略",
		Symbol: "BTCUSDT",
	}) == nil, "seed strategy")

	tr := store.NewTradeRepo()
	assertTrue(t, tr.Create(&store.TradeRecord{ID: "tr-rev-1", UserID: uid, OrderID: "ord-rev-1", Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, CreatedAt: now - 2*3600*1000}) == nil, "seed buy")
	assertTrue(t, tr.Create(&store.TradeRecord{ID: "tr-rev-2", UserID: uid, OrderID: "ord-rev-2", Symbol: "BTCUSDT", Side: "SELL", Price: 110, Quantity: 1, Fee: 1, CreatedAt: now - 3600*1000}) == nil, "seed sell")

	r := aiReviewRouter(int(uid))
	w, resp := postAIReview(t, r, `{"scope_type":"strategy","scope_id":"st-review-ok","days":30}`)
	assertEq(t, w.Code, http.StatusOK, "status code")
	assertTrue(t, resp["status"] == "done", "report status should be done")

	report, ok := resp["report"].(map[string]any)
	assertTrue(t, ok, "response should carry report")
	assertTrue(t, report["id"] != "", "report id should be set")
	assertEq(t, int(report["trades_count"].(float64)), 2, "trades_count")
	// FIFO 配对：卖 1@110 − 买 1@100 − 卖出手续费 1 = 9
	assertEq(t, int(report["total_pnl"].(float64)), 9, "total_pnl")
	assertEq(t, int(report["win_rate"].(float64)), 100, "win_rate")
	assertEq(t, int(report["max_drawdown"].(float64)), 0, "max_drawdown")
	assertTrue(t, strings.Contains(report["report_text"].(string), "总体评价"), "report_text should be AI content")
	assertTrue(t, strings.HasPrefix(report["model"].(string), "ai-review-test/"), "model recorded")

	// prompt 构造核查：统计与成交流水应进入 prompt
	assertTrue(t, strings.Contains(*lastBody, "成交笔数"), "prompt should contain stats")
	assertTrue(t, strings.Contains(*lastBody, "复盘测试策略"), "prompt should contain scope name")
	assertTrue(t, strings.Contains(*lastBody, "trades_count") || strings.Contains(*lastBody, "110"), "prompt should contain trade flow")

	// 落库可回读
	rec, err := store.NewAIReviewReportRepo().GetByID(report["id"].(string))
	assertTrue(t, err == nil, "persisted report readable")
	assertTrue(t, rec.Status == aiReviewStatusDone, "persisted status should be done, got "+rec.Status)
	assertTrue(t, strings.Contains(rec.ReportText, "总体评价"), "persisted text")
}

// 机器人 scope：OMS 成交经 client_oid 归因到 bot。
func TestAIReviewGenerateBotScope(t *testing.T) {
	mockAIReviewProvider(t, "一、总体评价：OK\n二、做得好的点：-\n三、问题与坏习惯：-\n四、可执行的改进建议：-")

	uid := int64(102)
	now := time.Now().UnixMilli()
	assertTrue(t, store.NewDCARepo().Create(&store.DCABotRecord{ID: "dca-rev-1", UserID: uid, Name: "定投机器人", Symbol: "ETHUSDT"}) == nil, "seed dca bot")
	assertTrue(t, store.NewOrderRepo().Create(&store.OrderRecord{ID: "ord-dca-rev-1", Symbol: "ETHUSDT", Side: "BUY", Status: "FILLED", UserID: uint64(uid), ClientOID: "dca:dca-rev-1", CreatedAt: now - 3600*1000, UpdatedAt: now - 3600*1000}) == nil, "seed order")
	assertTrue(t, store.NewTradeRepo().Create(&store.TradeRecord{ID: "tr-dca-rev-1", UserID: uid, OrderID: "ord-dca-rev-1", Symbol: "ETHUSDT", Side: "BUY", Price: 2000, Quantity: 0.5, CreatedAt: now - 3600*1000}) == nil, "seed trade")

	r := aiReviewRouter(int(uid))
	w, resp := postAIReview(t, r, `{"scope_type":"bot","scope_id":"dca-rev-1"}`)
	assertEq(t, w.Code, http.StatusOK, "status code")
	assertTrue(t, resp["status"] == "done", "status done")
	report := resp["report"].(map[string]any)
	assertEq(t, int(report["trades_count"].(float64)), 1, "bot trades_count via client_oid join")
	assertEq(t, int(report["total_pnl"].(float64)), 0, "只有买入未平仓，已实现盈亏为 0")
}

// AI 机器人实例 scope：走 ai_bot_trades 台账，PnL 直接采用。
func TestAIReviewGenerateAIBotLedger(t *testing.T) {
	mockAIReviewProvider(t, "一、总体评价：台账\n二、做得好的点：-\n三、问题与坏习惯：-\n四、可执行的改进建议：-")

	uid := int64(103)
	now := time.Now().UnixMilli()
	store.SaveAIBotInstance(map[string]any{
		"id": "aibot-rev-1", "user_id": int(uid), "name": "AI趋势实例",
		"symbol": "BTCUSDT", "status": "running", "execution_mode": "paper",
	})
	store.SaveAIBotTrade(map[string]any{
		"bot_instance_id": "aibot-rev-1", "symbol": "BTCUSDT", "side": "LONG",
		"entry_price": 100.0, "exit_price": 112.0, "quantity": 2.0,
		"pnl": 24.0, "close_reason": "tp", "opened_at": now - 7200*1000, "closed_at": now - 3600*1000,
	})
	store.SaveAIBotTrade(map[string]any{
		"bot_instance_id": "aibot-rev-1", "symbol": "BTCUSDT", "side": "LONG",
		"entry_price": 100.0, "exit_price": 95.0, "quantity": 1.0,
		"pnl": -5.0, "close_reason": "sl", "opened_at": now - 7200*1000, "closed_at": now - 1800*1000,
	})

	r := aiReviewRouter(int(uid))
	w, resp := postAIReview(t, r, `{"scope_type":"bot","scope_id":"aibot-rev-1","days":7}`)
	assertEq(t, w.Code, http.StatusOK, "status code")
	assertTrue(t, resp["status"] == "done", "status done")
	report := resp["report"].(map[string]any)
	assertEq(t, int(report["trades_count"].(float64)), 2, "ledger trades_count")
	assertEq(t, int(report["total_pnl"].(float64)), 19, "ledger total pnl")
	assertEq(t, int(report["win_rate"].(float64)), 50, "ledger win rate")
	assertEq(t, int(report["max_drawdown"].(float64)), 5, "ledger max drawdown")
}

// 无 provider：落库 status=failed 并记 error，HTTP 200 + status=failed。
func TestAIReviewGenerateNoProvider(t *testing.T) {
	disableAIReviewProvider(t)

	uid := int64(104)
	assertTrue(t, store.NewStrategyConfigRepo().Create(&store.StrategyConfigRecord{
		ID: "st-review-nop", UserID: uid, Name: "无Provider策略", Symbol: "ETHUSDT",
	}) == nil, "seed strategy")

	r := aiReviewRouter(int(uid))
	w, resp := postAIReview(t, r, `{"scope_type":"strategy","scope_id":"st-review-nop"}`)
	assertEq(t, w.Code, http.StatusOK, "status code")
	assertTrue(t, resp["status"] == "failed", "status should be failed")
	assertTrue(t, resp["msg"] != "", "msg should explain failure")
	report := resp["report"].(map[string]any)
	assertTrue(t, strings.Contains(report["error"].(string), "provider"), "error recorded")

	rec, err := store.NewAIReviewReportRepo().GetByID(report["id"].(string))
	assertTrue(t, err == nil, "failed report persisted")
	assertTrue(t, rec.Status == aiReviewStatusFailed, "persisted status should be failed, got "+rec.Status)
	assertTrue(t, rec.Error != "", "persisted error")
}

// provider 调用失败同样落库 failed。
func TestAIReviewGenerateProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	ai.RegisterProvider(ai.Provider{Name: "ai-review-test", BaseURL: srv.URL, APIKey: "sk-test", Model: "m"})
	old := aiReviewGetProvider
	aiReviewGetProvider = func() *ai.Provider { return ai.GetProvider("ai-review-test") }
	t.Cleanup(func() {
		aiReviewGetProvider = old
		srv.Close()
	})

	uid := int64(105)
	assertTrue(t, store.NewStrategyConfigRepo().Create(&store.StrategyConfigRecord{
		ID: "st-review-err", UserID: uid, Name: "失败策略", Symbol: "SOLUSDT",
	}) == nil, "seed strategy")

	r := aiReviewRouter(int(uid))
	w, resp := postAIReview(t, r, `{"scope_type":"strategy","scope_id":"st-review-err"}`)
	assertEq(t, w.Code, http.StatusOK, "status code")
	assertTrue(t, resp["status"] == "failed", "status failed")
	report := resp["report"].(map[string]any)
	assertTrue(t, report["error"].(string) != "", "error recorded")
}

// 越权：他人策略生成复盘 → 403。
func TestAIReviewGenerateScopeForbidden(t *testing.T) {
	mockAIReviewProvider(t, "不应被调用")

	assertTrue(t, store.NewStrategyConfigRepo().Create(&store.StrategyConfigRecord{
		ID: "st-review-other", UserID: 999, Name: "他人策略", Symbol: "BTCUSDT",
	}) == nil, "seed others strategy")

	r := aiReviewRouter(106)
	w, _ := postAIReview(t, r, `{"scope_type":"strategy","scope_id":"st-review-other"}`)
	assertEq(t, w.Code, http.StatusForbidden, "cross-user scope should be 403")
}

// scope 不存在 → 404。
func TestAIReviewGenerateScopeNotFound(t *testing.T) {
	mockAIReviewProvider(t, "不应被调用")
	r := aiReviewRouter(107)
	w, _ := postAIReview(t, r, `{"scope_type":"bot","scope_id":"bot-missing-xyz"}`)
	assertEq(t, w.Code, http.StatusNotFound, "missing scope should be 404")
}

// 参数校验：非法 scope_type → 400。
func TestAIReviewGenerateBadRequest(t *testing.T) {
	r := aiReviewRouter(108)
	w, _ := postAIReview(t, r, `{"scope_type":"portfolio","scope_id":"x"}`)
	assertEq(t, w.Code, http.StatusBadRequest, "invalid scope_type should be 400")
}

// 历史列表：按用户隔离 + scope 过滤。
func TestAIReviewReportsList(t *testing.T) {
	uid := int64(109)
	repo := store.NewAIReviewReportRepo()
	assertTrue(t, repo.Create(&store.AIReviewReportRecord{ID: "aivr-list-1", UserID: uid, ScopeType: "strategy", ScopeID: "st-a", Status: aiReviewStatusDone, TradesCount: 3, TotalPnL: 12.5}) == nil, "seed report 1")
	assertTrue(t, repo.Create(&store.AIReviewReportRecord{ID: "aivr-list-2", UserID: uid, ScopeType: "bot", ScopeID: "b-1", Status: aiReviewStatusFailed, Error: "boom"}) == nil, "seed report 2")
	assertTrue(t, repo.Create(&store.AIReviewReportRecord{ID: "aivr-list-3", UserID: 999, ScopeType: "strategy", ScopeID: "st-a", Status: aiReviewStatusDone}) == nil, "seed others report")

	r := aiReviewRouter(int(uid))
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/ai/review/reports", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "status code")
	var body map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &body) == nil, "parse list")
	reports := body["reports"].([]any)
	assertEq(t, len(reports), 2, "only own reports")

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/ai/review/reports?scope_type=bot&scope_id=b-1", nil)
	r.ServeHTTP(w2, req2)
	var body2 map[string]any
	assertTrue(t, json.Unmarshal(w2.Body.Bytes(), &body2) == nil, "parse filtered list")
	filtered := body2["reports"].([]any)
	assertEq(t, len(filtered), 1, "scope filter")
	assertTrue(t, filtered[0].(map[string]any)["id"] == "aivr-list-2", "filtered id")
}

// 详情属主校验：越权 403，不存在 404，属主 200。
func TestAIReviewReportGetOwnership(t *testing.T) {
	repo := store.NewAIReviewReportRepo()
	assertTrue(t, repo.Create(&store.AIReviewReportRecord{
		ID: "aivr-get-1", UserID: 201, ScopeType: "strategy", ScopeID: "st-x",
		Status: aiReviewStatusDone, ReportText: "报告正文",
	}) == nil, "seed report")

	// 属主可读
	r := aiReviewRouter(201)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/ai/review/reports/aivr-get-1", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "owner read")
	var body map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &body) == nil, "parse detail")
	assertTrue(t, body["report"].(map[string]any)["report_text"] == "报告正文", "detail text")

	// 他人 → 403
	r2 := aiReviewRouter(202)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/ai/review/reports/aivr-get-1", nil)
	r2.ServeHTTP(w2, req2)
	assertEq(t, w2.Code, http.StatusForbidden, "cross-user report should be 403")

	// 不存在 → 404
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("GET", "/ai/review/reports/aivr-missing", nil)
	r2.ServeHTTP(w3, req3)
	assertEq(t, w3.Code, http.StatusNotFound, "missing report should be 404")
}
