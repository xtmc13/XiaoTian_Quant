package social

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 测试环境 ─────────────────────────────────────────────────────
// 临时 SQLite + 全量迁移（含 0017），真实 JWT（AdminRequired 走完整鉴权链）。

var marketTestOnce sync.Once

type marketTestEnv struct {
	router *gin.Engine
	eng    *Engine
	market *MarketService

	adminID      int
	providerID   int // xt_users.id
	followerID   int
	outsiderID   int
	withdrawUser int // 独立 provider 属主：提现类用例专用，避免收益汇总互相污染
	withdrawID   int // withdrawUser 名下的 follower

	adminToken      string
	providerToken   string
	followerToken   string
	outsiderToken   string
	withdrawToken   string
	withdrawFollowT string
}

var sharedEnv *marketTestEnv

func setupMarketEnv(t *testing.T) *marketTestEnv {
	t.Helper()
	marketTestOnce.Do(func() {
		gin.SetMode(gin.TestMode)
		t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
		t.Setenv("SECRET_KEY", "test-secret-key-social-market")
		if err := store.InitDB(); err != nil {
			t.Fatalf("init db: %v", err)
		}
		if err := store.RunSQLMigrations(); err != nil {
			t.Fatalf("run sql migrations: %v", err)
		}
		env := &marketTestEnv{market: NewMarketService(), eng: NewEngine()}
		env.adminID = mustCreateUser(t, "mkt_admin", "admin")
		env.providerID = mustCreateUser(t, "mkt_provider", "user")
		env.followerID = mustCreateUser(t, "mkt_follower", "user")
		env.outsiderID = mustCreateUser(t, "mkt_outsider", "user")
		env.withdrawUser = mustCreateUser(t, "mkt_withdraw", "user")
		env.withdrawID = mustCreateUser(t, "mkt_withdraw_follower", "user")
		env.adminToken = mustToken(t, env.adminID, "mkt_admin", "admin")
		env.providerToken = mustToken(t, env.providerID, "mkt_provider", "user")
		env.followerToken = mustToken(t, env.followerID, "mkt_follower", "user")
		env.outsiderToken = mustToken(t, env.outsiderID, "mkt_outsider", "user")
		env.withdrawToken = mustToken(t, env.withdrawUser, "mkt_withdraw", "user")
		env.withdrawFollowT = mustToken(t, env.withdrawID, "mkt_withdraw_follower", "user")
		env.router = gin.New()
		socialG := env.router.Group("/api/social")
		socialG.Use(middleware.AuthRequired()) // 与 cmd/server/router.go 的生产挂载一致
		RegisterRoutes(socialG, env.eng)
		sharedEnv = env
	})
	return sharedEnv
}

func mustCreateUser(t *testing.T, username, role string) int {
	t.Helper()
	id, err := store.CreateUser(username, "pass-123456", username, username+"@test.local", role)
	require.NoError(t, err)
	return id
}

func mustToken(t *testing.T, userID int, username, role string) string {
	t.Helper()
	tok, err := store.GenerateJWT(userID, username, role, 1)
	require.NoError(t, err)
	return tok
}

func doJSON(t *testing.T, r *gin.Engine, method, path, token string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp map[string]any
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
	}
	return w, resp
}

// applyAndApprove 以指定用户身份申请入驻并完成审核，返回 provider 档案。
func applyAndApproveAs(t *testing.T, env *marketTestEnv, token, name string, pct float64, monthlyFee float64) *ProviderApply {
	t.Helper()
	w, resp := doJSON(t, env.router, "POST", "/api/social/providers/apply", token,
		map[string]any{"name": name, "description": "d", "profit_share_pct": pct, "monthly_fee": monthlyFee})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	p, err := env.market.GetProvider(int64(resp["provider"].(map[string]any)["id"].(float64)))
	require.NoError(t, err)
	w, _ = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/approve", p.ID), env.adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	approved, err := env.market.GetProvider(p.ID)
	require.NoError(t, err)
	require.Equal(t, ApplyApproved, approved.ApplyStatus)
	return approved
}

// applyAndApprove 以默认 provider 用户申请入驻并完成审核。
func applyAndApprove(t *testing.T, env *marketTestEnv, name string, pct float64, monthlyFee float64) *ProviderApply {
	return applyAndApproveAs(t, env, env.providerToken, name, pct, monthlyFee)
}

func todayStr() string { return time.Now().Format("2006-01-02") }

// followMarketProvider 指定 follower 身份订阅某个市场 provider（db id）。
func followMarketProviderAs(t *testing.T, env *marketTestEnv, followerToken string, providerDBID int64) {
	t.Helper()
	engineID := MarketEngineID(providerDBID)
	w, _ := doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/follow", engineID), followerToken, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// followMarketProvider 默认 follower 订阅某个市场 provider（db id）。
func followMarketProvider(t *testing.T, env *marketTestEnv, providerDBID int64) {
	followMarketProviderAs(t, env, env.followerToken, providerDBID)
}

// ── 入驻 / 审核 / 订阅 ───────────────────────────────────────────

func TestMarket_ApplyApproveSubscribeFlow(t *testing.T) {
	env := setupMarketEnv(t)
	w, resp := doJSON(t, env.router, "POST", "/api/social/providers/apply", env.providerToken,
		map[string]any{"name": "Alpha Trader", "description": "btc swing", "profit_share_pct": 20, "monthly_fee": 0})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	p := resp["provider"].(map[string]any)
	assert.Equal(t, ApplyPending, p["apply_status"])
	assert.Equal(t, FeeModeProfitShare, p["fee_mode"])
	providerDBID := int64(p["id"].(float64))

	// 我的申请
	w, resp = doJSON(t, env.router, "GET", "/api/social/providers/my", env.providerToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, ApplyPending, resp["provider"].(map[string]any)["apply_status"])

	// 未审核不可订阅
	engineID := MarketEngineID(providerDBID)
	w, _ = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/follow", engineID), env.followerToken, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 非 admin 审核 → 403
	w, _ = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/approve", providerDBID), env.providerToken, nil)
	assert.Equal(t, http.StatusForbidden, w.Code)
	w, _ = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/reject", providerDBID), env.followerToken, nil)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// admin 通过（对同一条 pending 申请）；通过后注册进引擎、可订阅
	w, resp = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/approve", providerDBID), env.adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	approved, err := env.market.GetProvider(providerDBID)
	require.NoError(t, err)
	require.Equal(t, ApplyApproved, approved.ApplyStatus)
	assert.NotZero(t, approved.ApprovedAt)
	assert.NotNil(t, env.eng.GetProviderStats(engineID))

	w, _ = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/follow", engineID), env.followerToken, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	subs, err := env.market.ListActiveShareSubscriptions()
	require.NoError(t, err)
	found := false
	for _, s := range subs {
		if s.ProviderID == providerDBID && s.FollowerUserID == int64(env.followerID) {
			found = true
			assert.Equal(t, FeeModeProfitShare, s.FeeMode)
		}
	}
	assert.True(t, found, "subscription row should exist")

	// 列表带上市场 provider
	w, resp = doJSON(t, env.router, "GET", "/api/social/providers", env.followerToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	mps, ok := resp["market_providers"].([]any)
	require.True(t, ok)
	assert.NotEmpty(t, mps)

	// 已 approved 的不能再 reject（409）
	w, _ = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/reject", providerDBID), env.adminToken,
		map[string]any{"note": "nope"})
	assert.Equal(t, http.StatusConflict, w.Code)

	// unfollow 取消订阅
	w, _ = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/unfollow", engineID), env.followerToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	subs, err = env.market.ListActiveShareSubscriptions()
	require.NoError(t, err)
	for _, s := range subs {
		assert.False(t, s.ProviderID == providerDBID && s.FollowerUserID == int64(env.followerID),
			"subscription should be cancelled")
	}
}

func TestMarket_ApplyValidationAndFeeMode(t *testing.T) {
	env := setupMarketEnv(t)
	w, _ := doJSON(t, env.router, "POST", "/api/social/providers/apply", env.providerToken,
		map[string]any{"name": "", "profit_share_pct": 20})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w, _ = doJSON(t, env.router, "POST", "/api/social/providers/apply", env.providerToken,
		map[string]any{"name": "x", "profit_share_pct": 31})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w, resp := doJSON(t, env.router, "POST", "/api/social/providers/apply", env.providerToken,
		map[string]any{"name": "MonthlyOnly", "monthly_fee": 9.99})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, FeeModeMonthly, resp["provider"].(map[string]any)["fee_mode"])

	w, resp = doJSON(t, env.router, "POST", "/api/social/providers/apply", env.providerToken,
		map[string]any{"name": "HybridOne", "monthly_fee": 5, "profit_share_pct": 15})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, FeeModeHybrid, resp["provider"].(map[string]any)["fee_mode"])

	// 未登录不能申请
	w, _ = doJSON(t, env.router, "POST", "/api/social/providers/apply", "", map[string]any{"name": "x"})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// ── 分成结算 ─────────────────────────────────────────────────────

func newSettlementEnv(t *testing.T) (*marketTestEnv, *SettlementEngine, int64) {
	t.Helper()
	env := setupMarketEnv(t)
	p := applyAndApprove(t, env, fmt.Sprintf("Settle-%s", t.Name()), 20, 0)
	engine := NewSettlementEngine(env.market)
	engine.SetLogf(func(string, ...any) {})
	engineID := MarketEngineID(p.ID)
	w, _ := doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/follow", engineID), env.followerToken, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return env, engine, p.ID
}

func TestSettlement_PositivePnlSnapshotAndAging(t *testing.T) {
	env, engine, providerDBID := newSettlementEnv(t)

	// 当日 +1000 已实现跟单盈亏 → 分成 200，快照 pending，locked_until = 窗口结束 + 72h
	require.NoError(t, env.market.RecordCopyPnL(providerDBID, int64(env.followerID), 1000, "close"))
	require.NoError(t, engine.SettleWindow(todayStr()))

	snaps, err := env.market.ListSnapshotsByProvider(providerDBID, 10)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	snap := snaps[0]
	assert.Equal(t, todayStr(), snap.WindowDate)
	assert.Equal(t, 1000.0, snap.CopiedPnl)
	assert.Equal(t, 20.0, snap.SharePct)
	assert.InDelta(t, 200.0, snap.ShareAmount, 1e-9)
	assert.Equal(t, SnapStatusPending, snap.Status)
	dayEnd, _ := time.ParseInLocation("2006-01-02", todayStr(), time.Local)
	assert.Equal(t, dayEnd.Add(24*time.Hour).Add(72*time.Hour).UnixMilli(), snap.LockedUntil)

	// 重复结算幂等（唯一键 + ON CONFLICT DO NOTHING）
	require.NoError(t, engine.SettleWindow(todayStr()))
	snaps, err = env.market.ListSnapshotsByProvider(providerDBID, 10)
	require.NoError(t, err)
	assert.Len(t, snaps, 1)

	// T+3 到期 → locked；再过 payableDelay → payable
	future := dayEnd.Add(24 * time.Hour).Add(72 * time.Hour).Add(time.Second)
	toLocked, toPayable, err := engine.Sweep(future)
	require.NoError(t, err)
	assert.Equal(t, int64(1), toLocked)
	assert.Equal(t, int64(0), toPayable)

	payableTime := future.Add(24 * time.Hour)
	toLocked, toPayable, err = engine.Sweep(payableTime)
	require.NoError(t, err)
	assert.Equal(t, int64(0), toLocked)
	assert.Equal(t, int64(1), toPayable)

	// provider 侧收益汇总
	w, resp := doJSON(t, env.router, "GET", "/api/social/earnings", env.providerToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	e := resp["earnings"].(map[string]any)
	assert.InDelta(t, 200.0, e["today"].(float64), 1e-9)
	assert.InDelta(t, 200.0, e["payable"].(float64), 1e-9)
	assert.InDelta(t, 200.0, e["available"].(float64), 1e-9)
}

func TestSettlement_NegativePnlOffsetsOnlyPending(t *testing.T) {
	env, engine, providerDBID := newSettlementEnv(t)
	follower := int64(env.followerID)

	// D1: +100 → 分成 20（pending）
	require.NoError(t, env.market.RecordCopyPnL(providerDBID, follower, 100, ""))
	require.NoError(t, engine.SettleWindow(todayStr()))
	snaps, err := env.market.ListSnapshotsByProvider(providerDBID, 10)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	d1 := snaps[0]
	require.Equal(t, SnapStatusPending, d1.Status)

	// D2: -50 → 冲抵 pending 快照（20 全单 void，剩余 30 不结转）
	tomorrow := time.Now().Add(24 * time.Hour).Format("2006-01-02")
	tomorrowStart, _ := time.ParseInLocation("2006-01-02", tomorrow, time.Local)
	db := store.GetDB()
	_, err = db.Exec(`INSERT INTO xt_social_copy_pnl (provider_id, follower_user_id, pnl, note, created_at)
		VALUES (?, ?, -50, 'drawdown', ?)`, providerDBID, follower, tomorrowStart.Add(time.Hour).UnixMilli())
	require.NoError(t, err)
	require.NoError(t, engine.SettleWindow(tomorrow))

	snaps, err = env.market.ListSnapshotsByProvider(providerDBID, 10)
	require.NoError(t, err)
	byID := map[string]*ProfitSnapshot{}
	for _, s := range snaps {
		byID[s.ID] = s
	}
	require.Len(t, snaps, 1, "亏损窗口不产生新快照")
	assert.Equal(t, SnapStatusVoid, byID[d1.ID].Status)

	// 再次亏损：无 pending 可冲，不报错也不产生快照
	_, err = db.Exec(`INSERT INTO xt_social_copy_pnl (provider_id, follower_user_id, pnl, note, created_at)
		VALUES (?, ?, -10, 'drawdown2', ?)`, providerDBID, follower, tomorrowStart.Add(2*time.Hour).UnixMilli())
	require.NoError(t, err)
	require.NoError(t, engine.SettleWindow(tomorrow))
	snaps, err = env.market.ListSnapshotsByProvider(providerDBID, 10)
	require.NoError(t, err)
	assert.Len(t, snaps, 1)
}

func TestSettlement_LockedSnapshotsNotOffset(t *testing.T) {
	env, engine, providerDBID := newSettlementEnv(t)
	follower := int64(env.followerID)

	require.NoError(t, env.market.RecordCopyPnL(providerDBID, follower, 100, ""))
	require.NoError(t, engine.SettleWindow(todayStr()))
	snaps, err := env.market.ListSnapshotsByProvider(providerDBID, 10)
	require.NoError(t, err)
	require.Len(t, snaps, 1)

	// 直接把快照置为 locked（模拟已过 T+3）
	db := store.GetDB()
	_, err = db.Exec(`UPDATE xt_social_profit_snapshots SET status = ? WHERE id = ?`, SnapStatusLocked, snaps[0].ID)
	require.NoError(t, err)

	// 回撤冲抵不得影响 locked 快照（locked 后不追溯）
	require.NoError(t, env.market.RecordCopyPnL(providerDBID, follower, -1000, ""))
	tomorrow := time.Now().Add(24 * time.Hour).Format("2006-01-02")
	require.NoError(t, engine.SettleWindow(tomorrow))

	got, err := env.market.PendingSnapshots(providerDBID, follower)
	require.NoError(t, err)
	assert.Empty(t, got)
	snaps, err = env.market.ListSnapshotsByProvider(providerDBID, 10)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, SnapStatusLocked, snaps[0].Status)
}

func TestSettlement_MonthlyProviderExcluded(t *testing.T) {
	env := setupMarketEnv(t)
	p := applyAndApprove(t, env, "Monthly-Only", 0, 9.99)
	engine := NewSettlementEngine(env.market)
	engine.SetLogf(func(string, ...any) {})
	engineID := MarketEngineID(p.ID)
	w, _ := doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/follow", engineID), env.followerToken, nil)
	require.Equal(t, http.StatusOK, w.Code)

	require.NoError(t, env.market.RecordCopyPnL(p.ID, int64(env.followerID), 500, ""))
	require.NoError(t, engine.SettleWindow(todayStr()))
	snaps, err := env.market.ListSnapshotsByProvider(p.ID, 10)
	require.NoError(t, err)
	assert.Empty(t, snaps, "monthly 模式不参与分成结算")
}

func TestSettlement_RunDueNoPanicWithoutPnl(t *testing.T) {
	env, engine, providerDBID := newSettlementEnv(t)
	require.NoError(t, engine.RunDue(time.Now()))
	snaps, err := env.market.ListSnapshotsByProvider(providerDBID, 10)
	require.NoError(t, err)
	assert.Empty(t, snaps, "无盈亏记录时不产生快照")
}

// ── 提现 ─────────────────────────────────────────────────────────

// setupPayable 制造 amount 的 payable 余额：结算今日窗口 + 直接把快照置为 payable。
func setupPayable(t *testing.T, env *marketTestEnv, providerDBID int64, followerUserID int, amount float64) {
	t.Helper()
	require.NoError(t, env.market.RecordCopyPnL(providerDBID, int64(followerUserID), amount*5, "")) // pct=20
	settle := NewSettlementEngine(env.market)
	settle.SetLogf(func(string, ...any) {})
	require.NoError(t, settle.SettleWindow(todayStr()))
	db := store.GetDB()
	past := time.Now().Add(-96 * time.Hour).UnixMilli()
	_, err := db.Exec(`UPDATE xt_social_profit_snapshots SET status = ?, locked_until = ? WHERE provider_id = ?`,
		SnapStatusPayable, past, providerDBID)
	require.NoError(t, err)
}

func TestMarket_WithdrawFlowAndIdempotency(t *testing.T) {
	env := setupMarketEnv(t)
	p := applyAndApproveAs(t, env, env.withdrawToken, "Withdraw-Provider", 20, 0)
	followMarketProviderAs(t, env, env.withdrawFollowT, p.ID)
	setupPayable(t, env, p.ID, env.withdrawID, 200) // payable 200

	// 余额不足
	w, _ := doJSON(t, env.router, "POST", "/api/social/earnings/withdraw", env.withdrawToken,
		map[string]any{"amount": 250, "chain": "trc20", "address": "TXxxx"})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 参数校验
	w, _ = doJSON(t, env.router, "POST", "/api/social/earnings/withdraw", env.withdrawToken,
		map[string]any{"amount": 10, "chain": "btc", "address": "x"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = doJSON(t, env.router, "POST", "/api/social/earnings/withdraw", env.withdrawToken,
		map[string]any{"amount": 10, "chain": "trc20", "address": ""})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 正常申请 150
	w, resp := doJSON(t, env.router, "POST", "/api/social/earnings/withdraw", env.withdrawToken,
		map[string]any{"amount": 150, "chain": "TRC20", "address": "TXxxx"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	wd := resp["withdrawal"].(map[string]any)
	assert.Equal(t, WithdrawStatusPending, wd["status"])
	wdID := wd["id"].(string)

	// 在途提现占用余额：再提 100 > 50 可用 → 400（防双花）
	w, _ = doJSON(t, env.router, "POST", "/api/social/earnings/withdraw", env.withdrawToken,
		map[string]any{"amount": 100, "chain": "bep20", "address": "0xabc"})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 非 admin 不能打款/拒绝
	w, _ = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/admin/withdrawals/%s/pay", wdID), env.withdrawToken,
		map[string]any{"tx_hash": "0x1"})
	assert.Equal(t, http.StatusForbidden, w.Code)
	w, _ = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/admin/withdrawals/%s/reject", wdID), env.outsiderToken,
		map[string]any{"note": "n"})
	assert.Equal(t, http.StatusForbidden, w.Code)

	// admin 打款（缺 tx_hash → 400）
	w, _ = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/admin/withdrawals/%s/pay", wdID), env.adminToken,
		map[string]any{"tx_hash": ""})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// admin 打款成功 → 重复打款 409（幂等）
	w, resp = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/admin/withdrawals/%s/pay", wdID), env.adminToken,
		map[string]any{"tx_hash": "0xdeadbeef"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, WithdrawStatusPaid, resp["withdrawal"].(map[string]any)["status"])
	assert.Equal(t, "0xdeadbeef", resp["withdrawal"].(map[string]any)["tx_hash"])

	w, _ = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/admin/withdrawals/%s/pay", wdID), env.adminToken,
		map[string]any{"tx_hash": "0x2"})
	assert.Equal(t, http.StatusConflict, w.Code)

	// 收益汇总：已付 150，可用 50
	w, resp = doJSON(t, env.router, "GET", "/api/social/earnings", env.withdrawToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	e := resp["earnings"].(map[string]any)
	assert.InDelta(t, 150.0, e["withdrawn"].(float64), 1e-9)
	assert.InDelta(t, 50.0, e["available"].(float64), 1e-9)

	// 剩余 50 可提；拒绝后余额恢复
	w, resp = doJSON(t, env.router, "POST", "/api/social/earnings/withdraw", env.withdrawToken,
		map[string]any{"amount": 50, "chain": "sol", "address": "SOLxxx"})
	require.Equal(t, http.StatusOK, w.Code)
	wd2 := resp["withdrawal"].(map[string]any)["id"].(string)

	w, resp = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/admin/withdrawals/%s/reject", wd2), env.adminToken,
		map[string]any{"note": "bad address"})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, WithdrawStatusRejected, resp["withdrawal"].(map[string]any)["status"])
	w, _ = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/admin/withdrawals/%s/reject", wd2), env.adminToken,
		map[string]any{"note": "again"})
	assert.Equal(t, http.StatusConflict, w.Code)

	w, resp = doJSON(t, env.router, "GET", "/api/social/earnings", env.withdrawToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.InDelta(t, 50.0, resp["earnings"].(map[string]any)["available"].(float64), 1e-9)

	// 我的提现列表
	w, resp = doJSON(t, env.router, "GET", "/api/social/earnings/withdrawals", env.withdrawToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	list, ok := resp["withdrawals"].([]any)
	require.True(t, ok)
	assert.Len(t, list, 2)
}

func TestMarket_AdminWithdrawalsAndIsolation(t *testing.T) {
	env := setupMarketEnv(t)
	p := applyAndApproveAs(t, env, env.withdrawToken, "Admin-List-Provider", 20, 0)
	followMarketProviderAs(t, env, env.withdrawFollowT, p.ID)
	setupPayable(t, env, p.ID, env.withdrawID, 100)

	w, _ := doJSON(t, env.router, "POST", "/api/social/earnings/withdraw", env.withdrawToken,
		map[string]any{"amount": 40, "chain": "erc20", "address": "0xE"})
	require.Equal(t, http.StatusOK, w.Code)

	// admin 列表 + status 过滤
	w, resp := doJSON(t, env.router, "GET", "/api/social/admin/withdrawals", env.adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, resp["withdrawals"].([]any))
	w, resp = doJSON(t, env.router, "GET", "/api/social/admin/withdrawals?status=paid", env.adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	for _, item := range resp["withdrawals"].([]any) {
		assert.Equal(t, WithdrawStatusPaid, item.(map[string]any)["status"])
	}
	w, _ = doJSON(t, env.router, "GET", "/api/social/admin/withdrawals?status=bogus", env.adminToken, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = doJSON(t, env.router, "GET", "/api/social/admin/withdrawals", env.withdrawToken, nil)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// 越权/隔离：outsider 看不到别人的收益与提现
	w, resp = doJSON(t, env.router, "GET", "/api/social/earnings", env.outsiderToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	e := resp["earnings"].(map[string]any)
	assert.InDelta(t, 0.0, e["total"].(float64), 1e-9)
	assert.InDelta(t, 0.0, e["available"].(float64), 1e-9)
	w, resp = doJSON(t, env.router, "GET", "/api/social/earnings/withdrawals", env.outsiderToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp["withdrawals"].([]any))
}

// TestSettlement_RunDueBackfill 验证 RunDue 基于快照表恢复进度并补结算。
func TestSettlement_RunDueBackfill(t *testing.T) {
	env := setupMarketEnv(t)
	// 清掉其他用例的快照：RunDue 的全局进度以 MAX(window_date) 恢复，保证基线可控。
	db := store.GetDB()
	_, err := db.Exec(`DELETE FROM xt_social_profit_snapshots`)
	require.NoError(t, err)

	p := applyAndApprove(t, env, "RunDue-Provider", 20, 0)
	engine := NewSettlementEngine(env.market)
	engine.SetLogf(func(string, ...any) {})
	engineID := MarketEngineID(p.ID)
	w, _ := doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/follow", engineID), env.followerToken, nil)
	require.Equal(t, http.StatusOK, w.Code)

	yesterday := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	// 昨日盈亏：直接写库把 created_at 挪到昨日窗口内
	dayStart, _ := time.ParseInLocation("2006-01-02", yesterday, time.Local)
	at := dayStart.Add(time.Hour).UnixMilli()
	_, err = db.Exec(`INSERT INTO xt_social_copy_pnl (provider_id, follower_user_id, pnl, note, created_at)
		VALUES (?, ?, 400, 'hist', ?)`, p.ID, env.followerID, at)
	require.NoError(t, err)

	require.NoError(t, engine.RunDue(time.Now()))
	snaps, err := env.market.ListSnapshotsByProvider(p.ID, 10)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, yesterday, snaps[0].WindowDate)
	assert.InDelta(t, 80.0, snaps[0].ShareAmount, 1e-9) // 400 * 20%
}
