package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 注册挂接：带码 / 无效码 / 自己推荐自己 ──

// registerViaHandler 走真实 Register handler 完成一次注册，返回状态码与 body。
func registerViaHandler(t *testing.T, r *gin.Engine, body map[string]any) (int, map[string]any) {
	t.Helper()
	return billingPost(t, r, "/auth/register", body)
}

// seedRegisterCode 造一个可用的注册验证码（走 store 层，绕开 SMTP）。
func seedRegisterCode(t *testing.T, email, code string) {
	t.Helper()
	if err := store.SaveVerificationCode(email, code, "register", "127.0.0.1", 600); err != nil {
		t.Fatalf("seed register code: %v", err)
	}
}

func TestRegisterWithReferralCode(t *testing.T) {
	t.Setenv("TURNSTILE_SECRET_KEY", "")
	referrerID := billingSeedUser(t, "ref_reg_referrer")
	rc, err := store.NewReferralRepo().EnsureCode(int64(referrerID), 10)
	assertTrue(t, err == nil && rc != nil, "referrer code")

	email := "ref_reg_new@test.local"
	seedRegisterCode(t, email, "654321")
	r := setupRouter()
	r.POST("/auth/register", Register)

	// 小写入参也应被归一化匹配
	code, resp := registerViaHandler(t, r, map[string]any{
		"username": "ref_reg_new", "password": "password123", "email": email,
		"code": "654321", "referral_code": strings.ToLower(rc.Code),
	})
	assertEq(t, code, http.StatusOK, "带有效推荐码注册必须 200")
	user, _ := resp["user"].(map[string]any)
	newUserID := int64(user["id"].(float64))
	referredBy, ok := store.GetReferredBy(newUserID)
	assertTrue(t, ok && referredBy == int64(referrerID),
		fmt.Sprintf("referred_by 应写入推荐人: got %d ok=%v want %d", referredBy, ok, referrerID))

	// 回归：不带推荐码注册 → 成功且无推荐关系
	email2 := "ref_reg_plain@test.local"
	seedRegisterCode(t, email2, "654321")
	code, resp = registerViaHandler(t, r, map[string]any{
		"username": "ref_reg_plain", "password": "password123", "email": email2, "code": "654321",
	})
	assertEq(t, code, http.StatusOK, "不带推荐码注册必须 200")
	user2, _ := resp["user"].(map[string]any)
	_, ok = store.GetReferredBy(int64(user2["id"].(float64)))
	assertTrue(t, !ok, "无推荐码注册不应有推荐关系")
}

func TestRegisterInvalidReferralCode(t *testing.T) {
	t.Setenv("TURNSTILE_SECRET_KEY", "")
	r := setupRouter()
	r.POST("/auth/register", Register)

	// 格式非法
	email := "ref_reg_badfmt@test.local"
	seedRegisterCode(t, email, "654321")
	code, resp := registerViaHandler(t, r, map[string]any{
		"username": "ref_reg_badfmt", "password": "password123", "email": email,
		"code": "654321", "referral_code": "nope",
	})
	assertEq(t, code, http.StatusBadRequest, "格式非法推荐码必须 400")
	assertTrue(t, strings.Contains(fmt.Sprint(resp["detail"]), "referral"), "错误应提示推荐码")

	// 格式合法但不存在
	email2 := "ref_reg_badcode@test.local"
	seedRegisterCode(t, email2, "654321")
	code, _ = registerViaHandler(t, r, map[string]any{
		"username": "ref_reg_badcode", "password": "password123", "email": email2,
		"code": "654321", "referral_code": "XTZZZZ999",
	})
	assertEq(t, code, http.StatusBadRequest, "不存在的推荐码必须 400")
	assertTrue(t, store.FindUserByUsername("ref_reg_badcode") == nil, "注册失败不得创建用户")
	assertTrue(t, store.FindUserByUsername("ref_reg_badfmt") == nil, "格式非法不得创建用户")

	// 已停用（active=0）的推荐码必须拒绝
	referrerID := billingSeedUser(t, "ref_reg_inact")
	rc, err := store.NewReferralRepo().EnsureCode(int64(referrerID), 10)
	assertTrue(t, err == nil, "inactive referrer code")
	_, err = store.GetDB().Exec(`UPDATE xt_referral_codes SET active=0 WHERE user_id=?`, referrerID)
	assertTrue(t, err == nil, "deactivate code")
	email3 := "ref_reg_inact_new@test.local"
	seedRegisterCode(t, email3, "654321")
	code, _ = registerViaHandler(t, r, map[string]any{
		"username": "ref_reg_inactnew", "password": "password123", "email": email3,
		"code": "654321", "referral_code": rc.Code,
	})
	assertEq(t, code, http.StatusBadRequest, "停用推荐码必须 400")
}

func TestReferralSelfGuard(t *testing.T) {
	// store 层：自己推荐自己必须被拒
	uid := billingSeedUser(t, "ref_self_guard")
	err := store.SetReferredBy(uid, int64(uid))
	assertTrue(t, errors.Is(err, store.ErrReferralSelf), "SetReferredBy 自推荐必须 ErrReferralSelf")

	// 佣金挂点层：referred_by 指向自己时不得发佣金（防御纵深）
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_refself")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_refself")
	selfUser := billingSeedUser(t, "ref_self_order")
	_, err = store.GetDB().Exec(`UPDATE xt_users SET referred_by=? WHERE id=?`, selfUser, selfUser)
	assertTrue(t, err == nil, "force self referred_by")
	repo := store.NewBillingRepo()
	o := &store.BillingOrder{ID: "bill_ref_self1", UserID: int64(selfUser), PlanID: "monthly", Chain: "stripe",
		AmountMicro: 19900000, Status: store.BillingStatusPending, CreatedAt: time.Now().Unix()}
	assertTrue(t, repo.Create(o) == nil, "create order")

	r := setupRouter()
	r.POST("/billing/stripe/webhook", BillingStripeWebhook)
	event := map[string]any{
		"type": "checkout.session.completed",
		"data": map[string]any{"object": map[string]any{
			"id": "cs_refself", "payment_status": "paid", "metadata": map[string]any{"order_id": o.ID},
		}},
	}
	raw, _ := json.Marshal(event)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/billing/stripe/webhook", strings.NewReader(string(raw)))
	req.Header.Set("Stripe-Signature", signStripePayload("whsec_refself", raw, time.Now().Unix()))
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "webhook 200")
	sub, _ := repo.GetSubscription(int64(selfUser))
	if sub.Credits != 500 {
		t.Fatalf("自推荐不得发佣金（只应有套餐 500 积分）, got %d", sub.Credits)
	}
}

// ── 我的推荐码：取或建 / 比例上限 ──

func TestReferralCodeEndpoints(t *testing.T) {
	uid := billingSeedUser(t, "ref_code_ep")
	r := setupRouter()
	r.GET("/billing/referral/code", billingAuthed(uid, "user"), BillingReferralCode)
	r.POST("/billing/referral/code", billingAuthed(uid, "user"), BillingReferralCodeUpdate)

	// GET 自动生成：XT + 8 位大写字母数字
	code, resp := billingGet(t, r, "/billing/referral/code")
	assertEq(t, code, http.StatusOK, "GET code 200")
	rc, _ := resp["code"].(string)
	assertTrue(t, regexp.MustCompile(`^XT[A-Z0-9]{8}$`).MatchString(rc), fmt.Sprintf("code 格式非法: %q", rc))
	if resp["commission_pct"].(float64) != 10 || resp["active"] != true {
		t.Fatalf("默认比例/状态异常: %v", resp)
	}
	if !strings.Contains(fmt.Sprint(resp["referral_link"]), "/register?ref="+rc) ||
		!strings.Contains(fmt.Sprint(resp["referral_path"]), "/register?ref="+rc) {
		t.Fatalf("推荐链接异常: %v", resp)
	}

	// 再次 GET → 同一码（幂等，不重复生成）
	code, resp2 := billingGet(t, r, "/billing/referral/code")
	assertEq(t, code, http.StatusOK, "GET code again 200")
	if resp2["code"] != rc {
		t.Fatalf("重复 GET 必须返回同一码: %v vs %v", resp2["code"], rc)
	}

	// POST 更新比例 25 → 200
	code, resp = billingPost(t, r, "/billing/referral/code", map[string]any{"commission_pct": 25})
	assertEq(t, code, http.StatusOK, "POST pct 25 200")
	if resp["commission_pct"].(float64) != 25 {
		t.Fatalf("比例未更新: %v", resp)
	}

	// 边界：0 / 50 允许
	code, _ = billingPost(t, r, "/billing/referral/code", map[string]any{"commission_pct": 0})
	assertEq(t, code, http.StatusOK, "pct 0 允许")
	code, _ = billingPost(t, r, "/billing/referral/code", map[string]any{"commission_pct": 50})
	assertEq(t, code, http.StatusOK, "pct 50 允许")

	// 越界：51 / -1 拒绝
	code, _ = billingPost(t, r, "/billing/referral/code", map[string]any{"commission_pct": 51})
	assertEq(t, code, http.StatusBadRequest, "pct 51 必须 400")
	code, _ = billingPost(t, r, "/billing/referral/code", map[string]any{"commission_pct": -1})
	assertEq(t, code, http.StatusBadRequest, "pct -1 必须 400")

	// 缺省字段 → 保持现值（上一次成功设为 50）
	code, resp = billingPost(t, r, "/billing/referral/code", map[string]any{})
	assertEq(t, code, http.StatusOK, "POST 空 body 200")
	if resp["commission_pct"].(float64) != 50 {
		t.Fatalf("缺省不应改动比例: %v", resp)
	}
}

// ── 佣金触发：Stripe webhook 路径 ──

func TestReferralCommissionStripe(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_refcomm")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_refcomm")
	referrerID := billingSeedUser(t, "ref_comm_stripe_ref")
	refRepo := store.NewReferralRepo()
	_, err := refRepo.EnsureCode(int64(referrerID), 10)
	assertTrue(t, err == nil, "referrer code")
	referredID := billingSeedUser(t, "ref_comm_stripe_new")
	assertTrue(t, store.SetReferredBy(referredID, int64(referrerID)) == nil, "referred_by")

	billRepo := store.NewBillingRepo()
	o := &store.BillingOrder{ID: "bill_ref_stripe1", UserID: int64(referredID), PlanID: "monthly", Chain: "stripe",
		AmountMicro: 19900000, Status: store.BillingStatusPending, CreatedAt: time.Now().Unix()}
	assertTrue(t, billRepo.Create(o) == nil, "create stripe order")

	r := setupRouter()
	r.POST("/billing/stripe/webhook", BillingStripeWebhook)
	fire := func(orderID string) {
		t.Helper()
		event := map[string]any{
			"type": "checkout.session.completed",
			"data": map[string]any{"object": map[string]any{
				"id": "cs_refcomm", "payment_status": "paid", "metadata": map[string]any{"order_id": orderID},
			}},
		}
		raw, _ := json.Marshal(event)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/billing/stripe/webhook", strings.NewReader(string(raw)))
		req.Header.Set("Stripe-Signature", signStripePayload("whsec_refcomm", raw, time.Now().Unix()))
		r.ServeHTTP(w, req)
		assertEq(t, w.Code, http.StatusOK, "webhook 200")
	}

	fire(o.ID)
	// 推荐人佣金：19900000 微单位 → 1990 美分 × 10% = 199 积分
	sub, _ := billRepo.GetSubscription(int64(referrerID))
	if sub.Credits != 199 {
		t.Fatalf("推荐人应得 199 积分, got %d", sub.Credits)
	}
	newSub, _ := billRepo.GetSubscription(int64(referredID))
	if newSub.Credits != 500 {
		t.Fatalf("被推荐人应得套餐 500 积分, got %d", newSub.Credits)
	}
	earnings, _ := refRepo.ListEarnings(int64(referrerID), 100)
	assertTrue(t, len(earnings) == 1, fmt.Sprintf("佣金明细应 1 条: %d", len(earnings)))
	if earnings[0].OrderID != o.ID || earnings[0].Credits != 199 || earnings[0].Status != "credited" {
		t.Fatalf("佣金明细异常: %+v", earnings[0])
	}

	// 幂等：同一 order_id 重复投递（webhook 重试）不得重复发放
	fire(o.ID)
	sub, _ = billRepo.GetSubscription(int64(referrerID))
	if sub.Credits != 199 {
		t.Fatalf("重复 webhook 不得重复发佣金, got %d", sub.Credits)
	}
	earnings, _ = refRepo.ListEarnings(int64(referrerID), 100)
	assertTrue(t, len(earnings) == 1, "重复触发不得新增明细")

	// pct=0 不发佣金也不记明细
	assertTrue(t, refRepo.SetCommission(int64(referrerID), 0) == nil, "set pct 0")
	o2 := &store.BillingOrder{ID: "bill_ref_stripe2", UserID: int64(referredID), PlanID: "yearly", Chain: "stripe",
		AmountMicro: 199000000, Status: store.BillingStatusPending, CreatedAt: time.Now().Unix()}
	assertTrue(t, billRepo.Create(o2) == nil, "create stripe order 2")
	fire(o2.ID)
	sub, _ = billRepo.GetSubscription(int64(referrerID))
	if sub.Credits != 199 {
		t.Fatalf("pct=0 不得发佣金, got %d", sub.Credits)
	}
	earnings, _ = refRepo.ListEarnings(int64(referrerID), 100)
	assertTrue(t, len(earnings) == 1, "pct=0 不得新增明细")
}

// ── 佣金触发：USDT 链上核验通过路径 + 挂点二次触发幂等 ──

func TestReferralCommissionChain(t *testing.T) {
	t.Setenv("USDT_TRC20_ADDRESS", tronTestB58())
	var mock *httptest.Server
	mock = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, tronEventsBody(tronUSDTContract, tronTestB58(), "50000000", true))
	}))
	defer mock.Close()
	old := tronGridBaseURL
	tronGridBaseURL = mock.URL
	defer func() { tronGridBaseURL = old }()

	referrerID := billingSeedUser(t, "ref_comm_chain_ref")
	refRepo := store.NewReferralRepo()
	_, err := refRepo.EnsureCode(int64(referrerID), 25)
	assertTrue(t, err == nil, "referrer code pct 25")
	referredID := billingSeedUser(t, "ref_comm_chain_new")
	assertTrue(t, store.SetReferredBy(referredID, int64(referrerID)) == nil, "referred_by")

	billRepo := store.NewBillingRepo()
	o := &store.BillingOrder{ID: "bill_ref_chain1", UserID: int64(referredID), PlanID: "monthly", Chain: "TRC20",
		Address: tronTestB58(), AmountMicro: 19900000, TxHash: "goodtx", Status: store.BillingStatusPending,
		CreatedAt: time.Now().Unix()}
	assertTrue(t, billRepo.Create(o) == nil, "create chain order")

	runBillingVerifySweep()

	got, err := billRepo.GetByID(o.ID)
	assertTrue(t, err == nil && got.Status == store.BillingStatusPaid, fmt.Sprintf("订单必须 paid: %+v", got))
	// 1990 美分 × 25% = 497 积分
	sub, _ := billRepo.GetSubscription(int64(referrerID))
	if sub.Credits != 497 {
		t.Fatalf("链上路径推荐人应得 497 积分, got %d", sub.Credits)
	}
	earnings, _ := refRepo.ListEarnings(int64(referrerID), 100)
	assertTrue(t, len(earnings) == 1 && earnings[0].OrderID == o.ID && earnings[0].Credits == 497,
		fmt.Sprintf("链上路径佣金明细异常: %+v", earnings))

	// 幂等：挂点层对同一订单二次触发（order_id 唯一约束）不得重复发放
	billingGrantReferralCommission(o)
	sub, _ = billRepo.GetSubscription(int64(referrerID))
	if sub.Credits != 497 {
		t.Fatalf("二次触发不得重复发佣金, got %d", sub.Credits)
	}
	earnings, _ = refRepo.ListEarnings(int64(referrerID), 100)
	assertTrue(t, len(earnings) == 1, "二次触发不得新增明细")
}

// ── 汇总 / 明细 / admin 越权 ──

func TestReferralSummaryEarningsAdmin(t *testing.T) {
	referrerID := billingSeedUser(t, "ref_sum_ref")
	refRepo := store.NewReferralRepo()
	rc, err := refRepo.EnsureCode(int64(referrerID), 10)
	assertTrue(t, err == nil, "referrer code")
	n1 := billingSeedUser(t, "ref_sum_n1")
	n2 := billingSeedUser(t, "ref_sum_n2")
	assertTrue(t, store.SetReferredBy(n1, int64(referrerID)) == nil, "n1 referred_by")
	assertTrue(t, store.SetReferredBy(n2, int64(referrerID)) == nil, "n2 referred_by")

	// 直接入账两笔（绕过支付，验证明细/汇总/越权）
	now := time.Now().Unix()
	cases := []struct {
		orderID  string
		referred int
		amt      int64
	}{
		{"bill_ref_sum0", n1, 19900000},
		{"bill_ref_sum1", n2, 499000000},
	}
	for i, tc := range cases {
		inserted, err := refRepo.CreditEarningTx(&store.ReferralEarning{
			ReferrerUserID: int64(referrerID), ReferredUserID: int64(tc.referred),
			OrderID: tc.orderID, OrderAmount: tc.amt,
			Credits: tc.amt / 10000 * 10 / 100, Status: "credited", CreatedAt: now + int64(i),
		})
		assertTrue(t, err == nil && inserted, "credit earning")
	}
	// 同 order_id 再插一次 → 幂等拒绝
	inserted, err := refRepo.CreditEarningTx(&store.ReferralEarning{
		ReferrerUserID: int64(referrerID), ReferredUserID: int64(n1),
		OrderID: "bill_ref_sum0", OrderAmount: 19900000, Credits: 199,
	})
	assertTrue(t, err == nil && !inserted, "重复 order_id 必须幂等拒绝")

	r := setupRouter()
	r.GET("/billing/referral/summary", billingAuthed(referrerID, "user"), BillingReferralSummary)
	r.GET("/billing/referral/earnings", billingAuthed(referrerID, "user"), BillingReferralEarnings)
	r.GET("/admin/referrals", billingAuthed(referrerID, "user"), AdminListReferrals)

	code, resp := billingGet(t, r, "/billing/referral/summary")
	assertEq(t, code, http.StatusOK, "summary 200")
	if resp["code"] != rc.Code || resp["referral_count"].(float64) != 2 ||
		resp["total_credits"].(float64) != 199+4990 || resp["pending_credits"].(float64) != 0 {
		t.Fatalf("summary 异常: %v", resp)
	}

	code, resp = billingGet(t, r, "/billing/referral/earnings")
	assertEq(t, code, http.StatusOK, "earnings 200")
	list, _ := resp["earnings"].([]any)
	assertTrue(t, len(list) == 2, fmt.Sprintf("明细应 2 条: %d", len(list)))
	row, _ := list[0].(map[string]any)
	if row["referred_username"] != "ref_sum_n2" { // 新→旧排序，n2 的后插入排前
		t.Fatalf("明细应带被推荐人用户名且新→旧: %v", row)
	}

	// 他人明细不可见：其他用户登录 → 空列表（服务端按登录态强过滤）
	rOther := setupRouter()
	rOther.GET("/billing/referral/earnings", billingAuthed(billingSeedUser(t, "ref_sum_other"), "user"), BillingReferralEarnings)
	code, resp = billingGet(t, rOther, "/billing/referral/earnings")
	assertEq(t, code, http.StatusOK, "他人 earnings 200")
	otherList, _ := resp["earnings"].([]any)
	assertTrue(t, len(otherList) == 0, "他人不得看到我的明细")

	// 非 admin 访问 /admin/referrals → 403
	code, _ = billingGet(t, r, "/admin/referrals")
	assertEq(t, code, http.StatusForbidden, "非 admin 必须 403")

	// admin → 200 且含汇总行
	rAdmin := setupRouter()
	rAdmin.GET("/admin/referrals", billingAuthed(referrerID, "admin"), AdminListReferrals)
	code, resp = billingGet(t, rAdmin, "/admin/referrals")
	assertEq(t, code, http.StatusOK, "admin 200")
	rows, _ := resp["referrals"].([]any)
	found := false
	for _, item := range rows {
		m, _ := item.(map[string]any)
		if m["user_id"].(float64) == float64(referrerID) {
			found = true
			if m["referral_count"].(float64) != 2 || m["total_credits"].(float64) != 199+4990 ||
				m["code"] != rc.Code || m["commission_pct"].(float64) != 10 {
				t.Fatalf("admin 汇总行异常: %v", m)
			}
		}
	}
	assertTrue(t, found, "admin 列表应含该推荐人")
}
