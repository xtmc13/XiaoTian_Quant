package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// Stripe 支付通道：仅用标准库实现 Checkout Session 创建与 webhook 验签。
// 未配置 STRIPE_SECRET_KEY 时整个通道禁用（config 返回 enabled:false）。

// stripeAPIBase Stripe API 根地址（包级变量，便于 httptest 注入）。
var stripeAPIBase = envOrDefault("STRIPE_API_BASE", "https://api.stripe.com/v1")

// stripeEnabled 通道开关：以 STRIPE_SECRET_KEY 是否配置为准。
func stripeEnabled() bool { return os.Getenv("STRIPE_SECRET_KEY") != "" }

// stripeHTTPClient 出站超时，避免 webhook/下单挂死。
var stripeHTTPClient = &http.Client{Timeout: 20 * time.Second}

// BillingStripeConfig 查询 Stripe 通道状态（前端据此决定是否展示信用卡按钮）。
func BillingStripeConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"enabled":         stripeEnabled(),
		"publishable_key": os.Getenv("STRIPE_PUBLISHABLE_KEY"),
	})
}

type stripeCheckoutReq struct {
	// OrderID 已有 stripe 订单直付（前端先创建订单的场景）。
	OrderID string `json:"order_id"`
	// PlanID 直接按套餐发起支付：服务端自动复用/创建 pending stripe 订单。
	PlanID     string `json:"plan_id"`
	SuccessURL string `json:"success_url"`
	CancelURL  string `json:"cancel_url"`
}

// BillingStripeCheckout 创建 Stripe Checkout Session 并返回跳转 URL。
// 金额取订单微单位 → 美元分（1e6 micro = 100 分）。支持两种入参：
//   - order_id：为已有 stripe 订单发起收银台；
//   - plan_id（A4.1）：自动复用该用户同套餐的 pending stripe 订单，没有则创建。
func BillingStripeCheckout(c *gin.Context) {
	if !stripeEnabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "stripe 支付未启用"})
		return
	}
	var req stripeCheckoutReq
	if err := c.ShouldBindJSON(&req); err != nil || (req.OrderID == "" && req.PlanID == "") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: order_id or plan_id required"})
		return
	}

	var (
		o   *store.BillingOrder
		err error
	)
	if req.OrderID != "" {
		o, err = billingRepo.GetByID(req.OrderID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
	} else {
		plan := findBillingPlan(req.PlanID)
		if plan == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid plan_id"})
			return
		}
		o, err = stripeFindOrCreateOrder(c, plan)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
	}
	if !billingOrderOwned(c, o) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: not the resource owner"})
		return
	}
	if o.Chain != "stripe" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该订单不是 stripe 订单"})
		return
	}
	if o.Status != store.BillingStatusPending {
		c.JSON(http.StatusConflict, gin.H{"error": "订单当前状态不允许发起支付"})
		return
	}
	plan := findBillingPlan(o.PlanID)
	if plan == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "套餐不存在"})
		return
	}

	successURL := req.SuccessURL
	if successURL == "" {
		successURL = envOrDefault("STRIPE_SUCCESS_URL", "")
	}
	cancelURL := req.CancelURL
	if cancelURL == "" {
		cancelURL = envOrDefault("STRIPE_CANCEL_URL", "")
	}
	if successURL == "" || cancelURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 success_url/cancel_url"})
		return
	}

	form := url.Values{}
	form.Set("mode", "payment")
	form.Set("success_url", successURL)
	form.Set("cancel_url", cancelURL)
	form.Set("client_reference_id", o.ID)
	form.Set("metadata[order_id]", o.ID)
	form.Set("payment_intent_data[metadata][order_id]", o.ID)
	form.Set("line_items[0][quantity]", "1")
	if priceID := stripePriceIDForPlan(plan); priceID != "" {
		// 套餐维护了 stripe_price_id 映射（本地 plans 配置/环境变量）→ 直接引用
		form.Set("line_items[0][price]", priceID)
	} else {
		// 未映射时 price_data 内联套餐价（美分），避免在 Stripe 侧预建 Price 对象
		form.Set("line_items[0][price_data][currency]", "usd")
		form.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(o.AmountMicro/10000, 10))
		form.Set("line_items[0][price_data][product_data][name]", plan.Name+" XiaoTianQuant")
	}

	session, err := stripeCreateCheckoutSession(form)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "创建 Stripe 收银台失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"checkout_url": session.URL})
}

// stripeFindOrCreateOrder plan_id 直达支付（A4.1）：复用该用户同套餐的 pending
// stripe 订单（避免重复下单），否则按套餐价创建新订单。返回订单供收银台使用。
func stripeFindOrCreateOrder(c *gin.Context, plan *billingPlanDef) (*store.BillingOrder, error) {
	uid := billingUID(c)
	orders, err := billingRepo.ListByUser(uid, 100)
	if err == nil {
		for _, existing := range orders {
			if existing.PlanID == plan.ID && existing.Chain == "stripe" && existing.Status == store.BillingStatusPending {
				return existing, nil
			}
		}
	}
	o := &store.BillingOrder{
		ID:          newBillingOrderID(),
		UserID:      uid,
		PlanID:      plan.ID,
		Chain:       "stripe",
		AmountMicro: plan.PriceMicro,
		Status:      store.BillingStatusPending,
		CreatedAt:   time.Now().Unix(),
	}
	if err := billingRepo.Create(o); err != nil {
		return nil, fmt.Errorf("创建订单失败")
	}
	return o, nil
}

// stripePriceIDForPlan 套餐的 Stripe Price 映射：billingPlanDefs 表内字段
// 优先，其次环境变量 STRIPE_PRICE_ID_<PLAN_ID 大写>（如 STRIPE_PRICE_ID_MONTHLY）。
// 返回空 = 未映射，checkout 退回内联 price_data。
func stripePriceIDForPlan(plan *billingPlanDef) string {
	if plan.StripePriceID != "" {
		return plan.StripePriceID
	}
	return os.Getenv("STRIPE_PRICE_ID_" + strings.ToUpper(strings.ReplaceAll(plan.ID, "-", "_")))
}

// stripeCheckoutSession Stripe 返回的 session 子集。
type stripeCheckoutSession struct {
	ID  string `json:"id"`
	URL string `json:"url"`
	Err *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// stripeCreateCheckoutSession 调 Stripe REST 创建 checkout session。
func stripeCreateCheckoutSession(form url.Values) (*stripeCheckoutSession, error) {
	req, err := http.NewRequest(http.MethodPost, stripeAPIBase+"/checkout/sessions", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Basic auth: secret key 作为 username
	req.SetBasicAuth(os.Getenv("STRIPE_SECRET_KEY"), "")
	resp, err := stripeHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var session stripeCheckoutSession
	if err := json.Unmarshal(body, &session); err != nil {
		return nil, fmt.Errorf("stripe 响应解析失败: %w", err)
	}
	if session.Err != nil {
		return nil, fmt.Errorf("%s", session.Err.Message)
	}
	if session.URL == "" {
		return nil, fmt.Errorf("stripe 未返回收银台地址")
	}
	return &session, nil
}

// ── Webhook（公开路由，验签替代 AuthRequired）──

// BillingStripeWebhook 处理 Stripe 事件。仅信任验签通过的请求；
// checkout.session.completed → 按 metadata.order_id 置 paid 并发放（幂等）。
func BillingStripeWebhook(c *gin.Context) {
	secret := os.Getenv("STRIPE_WEBHOOK_SECRET")
	if secret == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "stripe webhook 未配置"})
		return
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}
	if !verifyStripeSignature(c.GetHeader("Stripe-Signature"), raw, secret) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "签名校验失败"})
		return
	}

	var event struct {
		Type   string          `json:"type"`
		Object json.RawMessage `json:"data"` // 结构: data.object
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "事件解析失败"})
		return
	}
	var dataObj struct {
		Object struct {
			ID              string            `json:"id"`
			Metadata        map[string]string `json:"metadata"`
			PaymentStatus   string            `json:"payment_status"`
			Status          string            `json:"status"`
			ClientReference string            `json:"client_reference_id"`
		} `json:"object"`
	}
	if err := json.Unmarshal(event.Object, &dataObj); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "事件载荷解析失败"})
		return
	}

	if event.Type != "checkout.session.completed" &&
		event.Type != "checkout.session.async_payment_failed" &&
		event.Type != "payment_intent.payment_failed" {
		c.JSON(http.StatusOK, gin.H{"received": true, "ignored": event.Type})
		return
	}
	orderID := dataObj.Object.Metadata["order_id"]
	if orderID == "" {
		orderID = dataObj.Object.ClientReference
	}
	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "事件缺少 order_id"})
		return
	}

	o, err := billingRepo.GetByID(orderID)
	if err != nil {
		// 订单不存在也要 200，避免 Stripe 无限重试轰炸
		c.JSON(http.StatusOK, gin.H{"received": true, "ignored": "order not found"})
		return
	}

	// 支付失败（A4.1）：异步支付失败或 PaymentIntent 失败 → 标记 failed，可重新发起。
	if event.Type == "checkout.session.async_payment_failed" || event.Type == "payment_intent.payment_failed" {
		if o.Status == store.BillingStatusPaid {
			c.JSON(http.StatusOK, gin.H{"received": true, "ignored": "already paid"})
			return
		}
		billingMarkFailed(o.ID, "Stripe 支付失败（"+event.Type+"）")
		c.JSON(http.StatusOK, gin.H{"received": true, "failed": o.ID})
		return
	}

	// 只处理支付真正完成的 session（防御 completed 但 unpaid 的异常态）
	if dataObj.Object.PaymentStatus != "" && dataObj.Object.PaymentStatus != "paid" {
		c.JSON(http.StatusOK, gin.H{"received": true, "ignored": "unpaid"})
		return
	}
	if o.Status == store.BillingStatusPaid {
		c.JSON(http.StatusOK, gin.H{"received": true, "ignored": "already paid"})
		return
	}
	billingGrantOrder(o)
	c.JSON(http.StatusOK, gin.H{"received": true})
}

// verifyStripeSignature 校验 Stripe-Signature 头（格式 t=...,v1=hex,...）：
// HMAC-SHA256(secret, "t.payload") 与 v1 做常量时间比较，时间戳容忍 ±300 秒。
// 与 stripe-go 官方实现一致：头部可能携带多个 v1（签名密钥轮换期间 Stripe
// 会同时下发新旧密钥的签名），任一匹配即通过；v0 等其它 scheme 忽略。
func verifyStripeSignature(header string, payload []byte, secret string) bool {
	var ts string
	var v1s []string
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			ts = kv[1]
		case "v1":
			v1s = append(v1s, kv[1])
		}
	}
	if ts == "" || len(v1s) == 0 {
		return false
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	if diff := time.Now().Unix() - t; diff > 300 || diff < -300 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(payload)
	expected := mac.Sum(nil)
	for _, v1 := range v1s {
		actual, err := hex.DecodeString(v1)
		if err == nil && hmac.Equal(expected, actual) {
			return true
		}
	}
	return false
}
