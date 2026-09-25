package social

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 双轨 SKU（迁移 0028）：轨选择 / 二选一互斥 / 切轨规则 / 结算作用范围 ──
//
// 对标 CryptoRobotics：同一条目同时卖"订阅版"与"利润分成版"，用户二选一。
// 结算路径验证：分成轨进 ListActiveShareSubscriptions（日窗跑批/T+3 锁定），
// 订阅轨走 billing 订单（market_subscription 用途，paid 后激活并顺延）。

// applyDualTrackProvider 直接经数据层创建带 pricing_model 的已上架 provider。
// 与 approveProvider handler 对齐：审核通过后注册进跟单引擎（offset id）。
func applyDualTrackProvider(t *testing.T, env *marketTestEnv, userID int, name, model string, monthlyFee float64, pct *float64) *ProviderApply {
	t.Helper()
	p, err := env.market.ApplyProviderWithPricing(int64(userID), name, "dual-track test", monthlyFee, pct, model)
	require.NoError(t, err)
	_, err = env.market.ApproveProvider(p.ID)
	require.NoError(t, err)
	approved, err := env.market.GetProvider(p.ID)
	require.NoError(t, err)
	env.eng.RegisterProvider(MarketEngineID(approved.ID), approved.MonthlyFee, true)
	return approved
}

func f64(v float64) *float64 { return &v }

func TestDualTrack_ValidatePricingModel(t *testing.T) {
	env := setupMarketEnv(t)
	uid := mustCreateUser(t, "dt_apply", "user")
	tok := mustToken(t, uid, "dt_apply", "user")

	// both 但分成比例低于 10% → 400
	w, _ := doJSON(t, env.router, "POST", "/api/social/providers/apply", tok,
		map[string]any{"name": "bad1", "pricing_model": "both", "monthly_fee": 50, "profit_share_pct": 5})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	// both 但分成比例高于 35% → 400
	w, _ = doJSON(t, env.router, "POST", "/api/social/providers/apply", tok,
		map[string]any{"name": "bad2", "pricing_model": "both", "monthly_fee": 50, "profit_share_pct": 40})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	// profit_share 轨但缺比例 → 400
	w, _ = doJSON(t, env.router, "POST", "/api/social/providers/apply", tok,
		map[string]any{"name": "bad3", "pricing_model": "profit_share", "monthly_fee": 0})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	// subscription 轨但月费为 0 → 400
	w, _ = doJSON(t, env.router, "POST", "/api/social/providers/apply", tok,
		map[string]any{"name": "bad4", "pricing_model": "subscription", "monthly_fee": 0})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	// 非法 model → 400
	w, _ = doJSON(t, env.router, "POST", "/api/social/providers/apply", tok,
		map[string]any{"name": "bad5", "pricing_model": "nope", "monthly_fee": 1})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 合法 both：10-35% 区间 + 月费 → 200，档案带回 pricing_model 与双轨
	w, resp := doJSON(t, env.router, "POST", "/api/social/providers/apply", tok,
		map[string]any{"name": "good", "pricing_model": "both", "monthly_fee": 50, "profit_share_pct": 20})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	p := resp["provider"].(map[string]any)
	assert.Equal(t, PricingBoth, p["pricing_model"])
}

func TestDualTrack_AvailableTracks(t *testing.T) {
	// 双轨新语义
	p := &ProviderApply{PricingModel: PricingBoth}
	assert.ElementsMatch(t, []string{TrackSubscription, TrackProfitShare}, AvailableTracks(p))
	p = &ProviderApply{PricingModel: PricingSubscription}
	assert.Equal(t, []string{TrackSubscription}, AvailableTracks(p))
	p = &ProviderApply{PricingModel: PricingProfitShare}
	assert.Equal(t, []string{TrackProfitShare}, AvailableTracks(p))
	// 旧 fee_mode 推导：hybrid 视作双轨（新订阅二选一）
	p = &ProviderApply{FeeMode: FeeModeHybrid}
	assert.ElementsMatch(t, []string{TrackSubscription, TrackProfitShare}, AvailableTracks(p))
	p = &ProviderApply{FeeMode: FeeModeMonthly}
	assert.Equal(t, []string{TrackSubscription}, AvailableTracks(p))
	p = &ProviderApply{FeeMode: FeeModeProfitShare}
	assert.Equal(t, []string{TrackProfitShare}, AvailableTracks(p))
}

// TestDualTrack_ProfitShareTrackFlow 分成轨：免费开通 → 进结算范围 →
// 互斥（不可直接订另一轨）→ 随时可切订阅轨（创建订单）。
func TestDualTrack_ProfitShareTrackFlow(t *testing.T) {
	env := setupMarketEnv(t)
	ownerID := mustCreateUser(t, "dt_ps_owner", "user")
	follower := mustCreateUser(t, "dt_ps_follower", "user")
	followerTok := mustToken(t, follower, "dt_ps_follower", "user")
	prov := applyDualTrackProvider(t, env, ownerID, "dt-ps-bot", PricingBoth, 50, f64(20))

	// 订阅分成轨：免费开通
	w, resp := doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/subscribe", prov.ID), followerTok,
		map[string]any{"track": "profit_share"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	sub := resp["subscription"].(map[string]any)
	assert.Equal(t, TrackProfitShare, sub["track"])

	// 引擎已开始跟单
	assert.NotEmpty(t, env.eng.GetFollowerConfigs(follower))

	// 结算作用范围：分成轨在列
	subs, err := env.market.ListActiveShareSubscriptions()
	require.NoError(t, err)
	found := false
	for _, s := range subs {
		if s.ProviderID == prov.ID && s.FollowerUserID == int64(follower) {
			found = true
			assert.InDelta(t, 20, s.ProfitSharePct, 1e-9)
		}
	}
	assert.True(t, found, "分成轨订阅必须进入结算作用范围")

	// mySubscription 返回当前轨
	w, resp = doJSON(t, env.router, "GET", fmt.Sprintf("/api/social/providers/%d/subscription", prov.ID), followerTok, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, TrackProfitShare, resp["subscription"].(map[string]any)["track"])

	// 同轨重复订阅 → 幂等 already=true
	w, resp = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/subscribe", prov.ID), followerTok,
		map[string]any{"track": "profit_share"})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, true, resp["already"])

	// 直接订另一轨 → 409（二选一互斥）
	t.Setenv("USDT_TRC20_ADDRESS", "TDTdualTrackTestAddress1111111111111")
	w, _ = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/subscribe", prov.ID), followerTok,
		map[string]any{"track": "subscription"})
	assert.Equal(t, http.StatusConflict, w.Code)

	// 分成轨 → 订阅轨：随时可切（返回计费订单）
	w, resp = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/switch_track", prov.ID), followerTok,
		map[string]any{"track": "subscription", "chain": "TRC20"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	order := resp["order"].(map[string]any)
	assert.Equal(t, store.BillingPurposeMarketSubscription, order["purpose"])
	assert.Equal(t, strconv.FormatInt(prov.ID, 10), order["ref_id"])
	assert.Equal(t, float64(50_000_000), order["amount_usdt"]) // 50 USDT 月费 → 微单位
	assert.Equal(t, "TRC20", order["chain"])
}

// TestDualTrack_SubscriptionTrackFlow 订阅轨：下单 → paid 激活（顺延）→
// 不进结算范围 → 未到期禁切分成轨 → 到期后可切。
func TestDualTrack_SubscriptionTrackFlow(t *testing.T) {
	env := setupMarketEnv(t)
	ownerID := mustCreateUser(t, "dt_sub_owner", "user")
	follower := mustCreateUser(t, "dt_sub_follower", "user")
	followerTok := mustToken(t, follower, "dt_sub_follower", "user")
	prov := applyDualTrackProvider(t, env, ownerID, "dt-sub-bot", PricingBoth, 30, f64(15))

	t.Setenv("USDT_TRC20_ADDRESS", "TDTdualTrackTestAddress2222222222222")

	// 订阅轨：创建订单（此时订阅未激活）
	w, resp := doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/subscribe", prov.ID), followerTok,
		map[string]any{"track": "subscription", "chain": "TRC20"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	order := resp["order"].(map[string]any)
	orderID := order["order_id"].(string)
	require.NotEmpty(t, orderID)
	assert.Equal(t, float64(30_000_000), order["amount_usdt"])

	// 未支付前：无 active 订阅
	_, err := env.market.GetSubscription(prov.ID, int64(follower))
	assert.Error(t, err)

	// 模拟链上核验通过：发放路径激活订阅轨（与 billingGrantMarketSubscription 同一调用）
	granted, err := store.NewBillingRepo().GrantMarketSubscriptionTx(orderID, int64(follower), prov.ID, MarketSubscriptionPeriodDays, time.Now().Unix())
	require.NoError(t, err)
	assert.True(t, granted)
	// 幂等：重复发放不得重复顺延
	granted, err = store.NewBillingRepo().GrantMarketSubscriptionTx(orderID, int64(follower), prov.ID, MarketSubscriptionPeriodDays, time.Now().Unix())
	require.NoError(t, err)
	assert.False(t, granted)

	cur, err := env.market.GetSubscription(prov.ID, int64(follower))
	require.NoError(t, err)
	assert.Equal(t, TrackSubscription, cur.EffectiveTrack())
	assert.Greater(t, cur.TrackExpiresAt, time.Now().UnixMilli()+29*86400_000, "订阅轨应给足 30 天")

	// 订阅轨不在结算范围
	subs, err := env.market.ListActiveShareSubscriptions()
	require.NoError(t, err)
	for _, s := range subs {
		require.False(t, s.ProviderID == prov.ID && s.FollowerUserID == int64(follower),
			"订阅轨不得进入分成结算范围")
	}

	// 未到期切分成轨 → 409（携带到期时间）
	w, resp = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/switch_track", prov.ID), followerTok,
		map[string]any{"track": "profit_share"})
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.NotZero(t, resp["track_expires_at"])

	// 强制到期 → 允许切分成轨
	_, err = store.GetDB().Exec(`UPDATE xt_social_subscriptions SET track_expires_at=? WHERE id=?`,
		time.Now().UnixMilli()-1000, cur.ID)
	require.NoError(t, err)
	w, resp = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/switch_track", prov.ID), followerTok,
		map[string]any{"track": "profit_share"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, TrackProfitShare, resp["subscription"].(map[string]any)["track"])

	// 切回后重新进结算范围
	subs, err = env.market.ListActiveShareSubscriptions()
	require.NoError(t, err)
	found := false
	for _, s := range subs {
		if s.ProviderID == prov.ID && s.FollowerUserID == int64(follower) {
			found = true
		}
	}
	assert.True(t, found, "切回分成轨后必须重新进入结算范围")

	// 订阅轨到期后续费：订阅同轨 → 新订单（renewal）
	// 先切回订阅轨（分成轨随时可切订阅轨）
	w, resp = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/switch_track", prov.ID), followerTok,
		map[string]any{"track": "subscription", "chain": "TRC20"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	order2 := resp["order"].(map[string]any)
	granted, err = store.NewBillingRepo().GrantMarketSubscriptionTx(order2["order_id"].(string), int64(follower), prov.ID, MarketSubscriptionPeriodDays, time.Now().Unix())
	require.NoError(t, err)
	require.True(t, granted)
	// 到期续费场景：把到期时间推回过去，再订同轨 → 新订单而不是 already
	cur2, err := env.market.GetSubscription(prov.ID, int64(follower))
	require.NoError(t, err)
	_, err = store.GetDB().Exec(`UPDATE xt_social_subscriptions SET track_expires_at=? WHERE id=?`,
		time.Now().UnixMilli()-1000, cur2.ID)
	require.NoError(t, err)
	w, resp = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/subscribe", prov.ID), followerTok,
		map[string]any{"track": "subscription", "chain": "TRC20"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotNil(t, resp["order"], "到期后续费必须创建新订单")
}

// TestDualTrack_SingleTrackProvider 单轨条目：另一轨不可选。
func TestDualTrack_SingleTrackProvider(t *testing.T) {
	env := setupMarketEnv(t)
	ownerID := mustCreateUser(t, "dt_single_owner", "user")
	follower := mustCreateUser(t, "dt_single_follower", "user")
	followerTok := mustToken(t, follower, "dt_single_follower", "user")

	// 仅分成轨
	prov := applyDualTrackProvider(t, env, ownerID, "dt-ps-only", PricingProfitShare, 0, f64(25))
	w, _ := doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/subscribe", prov.ID), followerTok,
		map[string]any{"track": "subscription"})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 仅订阅轨
	prov2 := applyDualTrackProvider(t, env, ownerID, "dt-sub-only", PricingSubscription, 20, nil)
	w, _ = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/subscribe", prov2.ID), followerTok,
		map[string]any{"track": "profit_share"})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 未上架不可订
	pending, err := env.market.ApplyProviderWithPricing(int64(ownerID), "dt-pending", "", 10, f64(20), PricingBoth)
	require.NoError(t, err)
	w, _ = doJSON(t, env.router, "POST", fmt.Sprintf("/api/social/providers/%d/subscribe", pending.ID), followerTok,
		map[string]any{"track": "profit_share"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
