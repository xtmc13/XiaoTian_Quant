package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// billingAuthed 模拟 AuthRequired 注入的登录态中间件。
func billingAuthed(uid int, role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", uid)
		c.Set("role", role)
		c.Next()
	}
}

// billingSeedUser 创建一个普通用户并返回 id（发放测试用）。
func billingSeedUser(t *testing.T, name string) int {
	t.Helper()
	id, err := store.CreateUser(name, "pass-whatever-123", name, name+"@test.local", "user")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

// billingPost 发起 JSON POST 并返回状态码与解析后的 body。
func billingPost(t *testing.T, r *gin.Engine, path string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	var parsed map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &parsed)
	return w.Code, parsed
}

func billingGet(t *testing.T, r *gin.Engine, path string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	var parsed map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &parsed)
	return w.Code, parsed
}

// ── 订单创建 / 状态机 ──

// TestBillingCreateOrderFlow 创建 → 提交哈希 → 占用拒绝 → 发放 → 幂等。
func TestBillingCreateOrderFlow(t *testing.T) {
	t.Setenv("USDT_TRC20_ADDRESS", tronTestB58())
	uid := billingSeedUser(t, "billing_flow")
	r := setupRouter()
	r.POST("/billing/orders", billingAuthed(uid, "user"), BillingCreateOrder)
	r.POST("/billing/orders/:id/tx", billingAuthed(uid, "user"), BillingSubmitTx)

	// 非法套餐
	code, resp := billingPost(t, r, "/billing/orders", map[string]any{"plan_id": "nope", "chain": "TRC20"})
	assertEq(t, code, http.StatusBadRequest, "bad plan must 400")
	if !strings.Contains(fmt.Sprint(resp["error"]), "plan") {
		t.Fatalf("bad plan error should mention plan, got %v", resp["error"])
	}

	// 未配置的链
	code, _ = billingPost(t, r, "/billing/orders", map[string]any{"plan_id": "monthly", "chain": "BEP20"})
	assertEq(t, code, http.StatusBadRequest, "未配置链必须 400")

	// 正常创建
	code, resp = billingPost(t, r, "/billing/orders", map[string]any{"plan_id": "monthly", "chain": "TRC20"})
	assertEq(t, code, http.StatusOK, "create order")
	orderID, _ := resp["order_id"].(string)
	if orderID == "" || resp["status"] != "pending" {
		t.Fatalf("unexpected create resp: %v", resp)
	}
	if resp["amount_usdt"].(float64) != 19900000 {
		t.Fatalf("amount should be 19900000 micro, got %v", resp["amount_usdt"])
	}
	if resp["expires_at"].(float64) <= resp["created_at"].(float64) {
		t.Fatalf("expires_at must be after created_at")
	}

	// 提交 tx_hash（带首尾空白，应被 trim）
	code, resp = billingPost(t, r, "/billing/orders/"+orderID+"/tx", map[string]any{"tx_hash": "  abc123hash  "})
	assertEq(t, code, http.StatusOK, "submit tx")
	if resp["tx_hash"] != "abc123hash" || resp["status"] != "pending" {
		t.Fatalf("unexpected submit resp: %v", resp)
	}

	// 占用：第二个订单使用同一哈希必须 409
	code, _ = billingPost(t, r, "/billing/orders", map[string]any{"plan_id": "monthly", "chain": "TRC20", "tx_hash": "abc123hash"})
	assertEq(t, code, http.StatusConflict, "tx_hash 占用必须 409")

	// failed 状态可重新提交不同哈希：直接置 failed 后重提
	repo := store.NewBillingRepo()
	if err := repo.MarkFailed(orderID, "测试失败"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	code, resp = billingPost(t, r, "/billing/orders/"+orderID+"/tx", map[string]any{"tx_hash": "def456hash"})
	assertEq(t, code, http.StatusOK, "failed 可重新提交")
	if resp["tx_hash"] != "def456hash" || resp["status"] != "pending" || resp["attempts"].(float64) != 0 {
		t.Fatalf("resubmit should reset attempts: %v", resp)
	}

	// 发放（核验通过路径）：事务内置 paid + 积分
	granted, err := repo.GrantPlanTx(orderID, int64(uid), "monthly", 500, 30, time.Now().Unix())
	assertTrue(t, err == nil && granted, "first grant must succeed")
	// 幂等：二次发放必须被拒绝且积分不重复
	granted, err = repo.GrantPlanTx(orderID, int64(uid), "monthly", 500, 30, time.Now().Unix())
	assertTrue(t, err == nil && !granted, "second grant must be idempotent")
	sub, err := repo.GetSubscription(int64(uid))
	assertTrue(t, err == nil, "get subscription")
	if sub.Credits != 500 || sub.Plan != "monthly" {
		t.Fatalf("subscription wrong: %+v", sub)
	}
	if sub.VipExpiresAt <= 0 {
		t.Fatalf("vip_expires_at should be in future: %d", sub.VipExpiresAt)
	}

	// paid 后再提交哈希 → 200 幂等返回（不修改状态）
	code, resp = billingPost(t, r, "/billing/orders/"+orderID+"/tx", map[string]any{"tx_hash": "zzz999"})
	assertEq(t, code, http.StatusOK, "paid 订单提交哈希幂等返回")
	if resp["tx_hash"] != "def456hash" {
		t.Fatalf("paid 订单不得被覆盖: %v", resp)
	}

	// 终身套餐：vip_expires_at = -1
	uid2 := billingSeedUser(t, "billing_life")
	o2 := &store.BillingOrder{ID: "bill_test_lifetime", UserID: int64(uid2), PlanID: "lifetime", Chain: "TRC20",
		Address: "addr", AmountMicro: 499000000, Status: store.BillingStatusPending, CreatedAt: time.Now().Unix()}
	assertTrue(t, repo.Create(o2) == nil, "create lifetime order")
	granted, err = repo.GrantPlanTx(o2.ID, int64(uid2), "lifetime", 800, -1, time.Now().Unix())
	assertTrue(t, err == nil && granted, "lifetime grant")
	sub2, _ := repo.GetSubscription(int64(uid2))
	if sub2.VipExpiresAt != -1 || sub2.Credits != 800 {
		t.Fatalf("lifetime subscription wrong: %+v", sub2)
	}
}

// TestBillingOrderOwnership 越权防护：他人订单 403，admin 可见。
func TestBillingOrderOwnership(t *testing.T) {
	t.Setenv("USDT_TRC20_ADDRESS", tronTestB58())
	owner := billingSeedUser(t, "billing_owner")
	other := billingSeedUser(t, "billing_other")

	rCreate := setupRouter()
	rCreate.POST("/billing/orders", billingAuthed(owner, "user"), BillingCreateOrder)
	code, resp := billingPost(t, rCreate, "/billing/orders", map[string]any{"plan_id": "yearly", "chain": "TRC20"})
	assertEq(t, code, http.StatusOK, "create order")
	orderID, _ := resp["order_id"].(string)

	// 他人（非 admin）查 → 403
	rOther := setupRouter()
	rOther.GET("/billing/orders/:id", billingAuthed(other, "user"), BillingOrderStatus)
	code, _ = billingGet(t, rOther, "/billing/orders/"+orderID)
	assertEq(t, code, http.StatusForbidden, "他人查询必须 403")

	// 本人查 → 200
	rOwner := setupRouter()
	rOwner.GET("/billing/orders/:id", billingAuthed(owner, "user"), BillingOrderStatus)
	code, _ = billingGet(t, rOwner, "/billing/orders/"+orderID)
	assertEq(t, code, http.StatusOK, "本人查询必须 200")

	// admin 查 → 200
	rAdmin := setupRouter()
	rAdmin.GET("/billing/orders/:id", billingAuthed(other, "admin"), BillingOrderStatus)
	code, _ = billingGet(t, rAdmin, "/billing/orders/"+orderID)
	assertEq(t, code, http.StatusOK, "admin 查询必须 200")

	// 列表只含本人订单
	rList := setupRouter()
	rList.GET("/billing/orders", billingAuthed(owner, "user"), BillingListOrders)
	code, listResp := billingGet(t, rList, "/billing/orders")
	assertEq(t, code, http.StatusOK, "list orders")
	orders, _ := listResp["orders"].([]any)
	assertTrue(t, len(orders) == 1, "列表应只含本人 1 个订单")
}

// ── TRC20 核验器（httptest mock TronGrid）──

// tronEventsBody 造一条 TronGrid USDT Transfer 事件（value 为 6 位小数最小单位）。
func tronEventsBody(contract, to, value string, success bool) string {
	return fmt.Sprintf(`{"success":%v,"data":[{"transaction_id":"tx1","event_name":"Transfer","contract_address":"%s","result":{"value":"%s","to_address":"%s"}}]}`,
		success, contract, value, to)
}

// base58Encode 测试用 base58 编码（比特币字母表）。
func base58Encode(b []byte) string {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	n := new(big.Int).SetBytes(b)
	base := big.NewInt(58)
	zero := big.NewInt(0)
	mod := new(big.Int)
	var out []byte
	for n.Cmp(zero) > 0 {
		n.DivMod(n, base, mod)
		out = append(out, alphabet[mod.Int64()])
	}
	// 前导 0x00 → '1'
	for _, x := range b {
		if x != 0 {
			break
		}
		out = append(out, alphabet[0])
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

// tronTestB58 生成合法 TRON base58 地址（21 字节 raw 编码，测试用）。
func tronTestB58() string {
	raw, err := hexDecode("41c2fa4a8e1a5b7e9d3f2a1c4b6d8e0f1a3c5b7d9f")
	if err != nil || len(raw) != 21 {
		panic("bad test tron address")
	}
	return base58Encode(raw)
}

func TestBillingVerifyTRC20(t *testing.T) {
	addr := tronTestB58()
	var mock *httptest.Server
	mock = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/good"):
			fmt.Fprint(w, tronEventsBody(tronUSDTContract, addr, "19900000", true))
		case strings.Contains(r.URL.Path, "/hexto"):
			// to_address 用 hex 形式（41 开头），应与 base58 收款地址归一化匹配
			fmt.Fprint(w, tronEventsBody(tronUSDTContract, "41c2fa4a8e1a5b7e9d3f2a1c4b6d8e0f1a3c5b7d9f", "19900000", true))
		case strings.Contains(r.URL.Path, "/short"):
			fmt.Fprint(w, tronEventsBody(tronUSDTContract, addr, "100", true))
		case strings.Contains(r.URL.Path, "/other"):
			fmt.Fprint(w, tronEventsBody(tronUSDTContract, "TSomeoneElse000000000000000000000", "19900000", true))
		default: // /missing
			fmt.Fprint(w, `{"success":false,"data":[]}`)
		}
	}))
	defer mock.Close()
	old := tronGridBaseURL
	tronGridBaseURL = mock.URL
	defer func() { tronGridBaseURL = old }()

	res := verifyTRC20Tx("good", addr, 19900000)
	assertTrue(t, res.Err == nil && res.Found && res.Valid && res.Confirmed,
		fmt.Sprintf("good tx must be valid+confirmed: %+v", res))

	res = verifyTRC20Tx("short", addr, 19900000)
	assertTrue(t, res.Found && !res.Valid, fmt.Sprintf("金额不足必须 invalid: %+v", res))

	res = verifyTRC20Tx("other", addr, 19900000)
	assertTrue(t, res.Found && !res.Valid, fmt.Sprintf("收款地址不符必须 invalid: %+v", res))

	res = verifyTRC20Tx("missing", addr, 19900000)
	assertTrue(t, res.Err == nil && !res.Found, fmt.Sprintf("未上链交易必须 not found: %+v", res))

	// hex to 地址归一化：同一地址的 base58 形式应与 hex to_address 匹配
	res = verifyTRC20Tx("hexto", addr, 19900000)
	assertTrue(t, res.Found && res.Valid,
		fmt.Sprintf("hex to_address 必须能与 base58 收款地址匹配: %+v", res))
}

// ── BEP20/ERC20 核验器（httptest mock Etherscan 同构 API）──

func TestBillingVerifyEVM(t *testing.T) {
	padTo := func(addr string) string {
		return "0x000000000000000000000000" + strings.ToLower(strings.TrimPrefix(addr, "0x"))
	}
	transferLog := func(to, valueHex string) string {
		return fmt.Sprintf(`{"address":"%s","topics":["%s","0x00","%s"],"data":"%s"}`,
			bep20USDTContract, erc20TransferTopic, padTo(to), valueHex)
	}
	const ourAddr = "0x1111111111111111111111111111111111111111"
	var mock *httptest.Server
	mock = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		action := r.URL.Query().Get("action")
		switch action {
		case "eth_getTransactionReceipt":
			txh := r.URL.Query().Get("txhash")
			switch {
			case strings.Contains(txh, "unconfirmed"):
				fmt.Fprintf(w, `{"result":{"status":"0x1","blockNumber":"0x64","logs":[%s]}}`, transferLog(ourAddr, "0x01304be4"))
			case strings.Contains(txh, "failed"):
				fmt.Fprint(w, `{"result":{"status":"0x0","blockNumber":"0x64","logs":[]}}`)
			case strings.Contains(txh, "missing"):
				fmt.Fprint(w, `{"result":null}`)
			default: // confirmed：块高 0x78(120)，确认数 21 ≥ 15
				fmt.Fprintf(w, `{"result":{"status":"0x1","blockNumber":"0x64","logs":[%s]}}`, transferLog(ourAddr, "0x01304be4"))
			}
		case "eth_blockNumber":
			fmt.Fprint(w, `{"result":"0x78"}`)
		}
	}))
	defer mock.Close()
	old := bscScanBaseURL
	bscScanBaseURL = mock.URL
	defer func() { bscScanBaseURL = old }()

	const amount = int64(19900000) // 0x01304be4

	res := verifyEvmTx(mock.URL, "test-key", bep20USDTContract, "confirmed", ourAddr, amount, bep20MinConfirmations)
	assertTrue(t, res.Err == nil && res.Found && res.Valid && res.Confirmed,
		fmt.Sprintf("确认数足够必须 paid: %+v", res))

	res = verifyEvmTx(mock.URL, "test-key", bep20USDTContract, "unconfirmed", ourAddr, amount, 100)
	assertTrue(t, res.Found && res.Valid && !res.Confirmed,
		fmt.Sprintf("确认数不足必须 confirming: %+v", res))

	res = verifyEvmTx(mock.URL, "test-key", bep20USDTContract, "failed", ourAddr, amount, bep20MinConfirmations)
	assertTrue(t, res.Found && !res.Valid, fmt.Sprintf("reverted 交易必须 invalid: %+v", res))

	res = verifyEvmTx(mock.URL, "test-key", bep20USDTContract, "missing", ourAddr, amount, bep20MinConfirmations)
	assertTrue(t, res.Err == nil && !res.Found, fmt.Sprintf("receipt null 必须 not found: %+v", res))
}

// TestBillingVerifyEVMMissingKey 未配置 API key → failed + 中文原因。
func TestBillingVerifyEVMMissingKey(t *testing.T) {
	t.Setenv("BSCSCAN_API_KEY", "")
	res := verifyChainTx("BEP20", "0xabc", "0xaddr", 100)
	assertTrue(t, res.Found && !res.Valid && strings.Contains(res.Reason, "BSCSCAN_API_KEY"),
		fmt.Sprintf("缺 key 必须 failed: %+v", res))
	t.Setenv("ETHERSCAN_API_KEY", "")
	res = verifyChainTx("ERC20", "0xabc", "0xaddr", 100)
	assertTrue(t, res.Found && !res.Valid && strings.Contains(res.Reason, "ETHERSCAN_API_KEY"),
		fmt.Sprintf("缺 key 必须 failed: %+v", res))
}

// ── SOL 核验器（httptest mock Solana RPC）──

func TestBillingVerifySOL(t *testing.T) {
	solBody := func(pre, post, owner, mint string, txErr bool) string {
		errField := "null"
		if txErr {
			errField = `{"InstructionError":[0,"Custom"]}`
		}
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":{"meta":{"err":%s,
			"preTokenBalances":[{"accountIndex":1,"mint":"%s","owner":"%s","uiTokenAmount":{"amount":"%s","decimals":6}}],
			"postTokenBalances":[{"accountIndex":1,"mint":"%s","owner":"%s","uiTokenAmount":{"amount":"%s","decimals":6}}]}}}`,
			errField, mint, owner, pre, mint, owner, post)
	}
	const our = "SoLAddRessForTests1111111111111111111111"
	var mock *httptest.Server
	mock = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req struct {
			Params []any `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		hash := ""
		if len(req.Params) > 0 {
			hash, _ = req.Params[0].(string)
		}
		switch hash {
		case "good":
			fmt.Fprint(w, solBody("0", "19900000", our, solUSDTMint, false))
		case "partial":
			fmt.Fprint(w, solBody("100", "1000", our, solUSDTMint, false)) // 差额不足
		case "wrongmint":
			fmt.Fprint(w, solBody("0", "19900000", our, "OtherMint1111111111111111111111", false))
		case "txerr":
			fmt.Fprint(w, solBody("0", "19900000", our, solUSDTMint, true))
		default:
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":null}`)
		}
	}))
	defer mock.Close()
	old := solanaRPCBaseURL
	solanaRPCBaseURL = mock.URL
	defer func() { solanaRPCBaseURL = old }()

	res := verifySOLTx("good", our, 19900000)
	assertTrue(t, res.Err == nil && res.Found && res.Valid && res.Confirmed,
		fmt.Sprintf("sol good: %+v", res))
	res = verifySOLTx("partial", our, 19900000)
	assertTrue(t, res.Found && !res.Valid, fmt.Sprintf("sol 差额不足: %+v", res))
	res = verifySOLTx("wrongmint", our, 19900000)
	assertTrue(t, res.Found && !res.Valid, fmt.Sprintf("sol mint 不符: %+v", res))
	res = verifySOLTx("txerr", our, 19900000)
	assertTrue(t, res.Found && !res.Valid, fmt.Sprintf("sol 执行失败: %+v", res))
	res = verifySOLTx("missing", our, 19900000)
	assertTrue(t, res.Err == nil && !res.Found, fmt.Sprintf("sol missing: %+v", res))
}

// ── Stripe webhook 验签 ──

// signStripePayload 生成 Stripe-Signature 头（v1 scheme）。
func signStripePayload(secret string, payload []byte, ts int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.", ts)))
	mac.Write(payload)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

func TestBillingStripeWebhook(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_xxx")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_test_xxx")
	uid := billingSeedUser(t, "billing_stripe")
	repo := store.NewBillingRepo()
	o := &store.BillingOrder{ID: "bill_stripe_1", UserID: int64(uid), PlanID: "monthly", Chain: "stripe",
		AmountMicro: 19900000, Status: store.BillingStatusPending, CreatedAt: time.Now().Unix()}
	assertTrue(t, repo.Create(o) == nil, "create stripe order")

	r := setupRouter()
	r.POST("/billing/stripe/webhook", BillingStripeWebhook) // 注意：无 auth 中间件

	event := map[string]any{
		"type": "checkout.session.completed",
		"data": map[string]any{"object": map[string]any{
			"id":             "cs_test_1",
			"payment_status": "paid",
			"metadata":       map[string]any{"order_id": o.ID},
		}},
	}
	raw, _ := json.Marshal(event)
	ts := time.Now().Unix()

	// 篡改签名 → 400
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/billing/stripe/webhook", strings.NewReader(string(raw)))
	req.Header.Set("Stripe-Signature", "t="+fmt.Sprint(ts)+",v1=deadbeef")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusBadRequest, "坏签名必须 400")

	// 正确签名 → 200 + 订单 paid + 积分发放
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/billing/stripe/webhook", strings.NewReader(string(raw)))
	req.Header.Set("Stripe-Signature", signStripePayload("whsec_test_xxx", raw, ts))
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "好签名必须 200")
	got, err := repo.GetByID(o.ID)
	assertTrue(t, err == nil && got.Status == store.BillingStatusPaid,
		fmt.Sprintf("webhook 必须置 paid: %+v", got))
	sub, _ := repo.GetSubscription(int64(uid))
	if sub.Credits != 500 {
		t.Fatalf("credits should be 500, got %d", sub.Credits)
	}

	// 重复投递（幂等）→ 200 但不重复发放
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/billing/stripe/webhook", strings.NewReader(string(raw)))
	req.Header.Set("Stripe-Signature", signStripePayload("whsec_test_xxx", raw, ts))
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "重复 webhook 仍 200")
	sub, _ = repo.GetSubscription(int64(uid))
	if sub.Credits != 500 {
		t.Fatalf("重复 webhook 不得重复发放, credits=%d", sub.Credits)
	}

	// 过期时间戳 → 400
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/billing/stripe/webhook", strings.NewReader(string(raw)))
	req.Header.Set("Stripe-Signature", signStripePayload("whsec_test_xxx", raw, ts-1000))
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusBadRequest, "过期时间戳必须 400")
}

// TestBillingStripeDisabled 未配置 STRIPE_SECRET_KEY 时 config 返回 enabled:false。
func TestBillingStripeDisabled(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", "")
	r := setupRouter()
	r.GET("/billing/stripe/config", BillingStripeConfig)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/billing/stripe/config", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "config 200")
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["enabled"] != false {
		t.Fatalf("enabled must be false when key missing: %v", resp)
	}
}

// ── 核验器驱动状态机（mock TronGrid + 真实 repo）──

// TestBillingVerifierSweep 端到端跑一轮核验：mock 链上数据 → 订单 paid。
func TestBillingVerifierSweep(t *testing.T) {
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

	uid := billingSeedUser(t, "billing_sweep")
	repo := store.NewBillingRepo()
	o := &store.BillingOrder{ID: "bill_sweep_1", UserID: int64(uid), PlanID: "monthly", Chain: "TRC20",
		Address: tronTestB58(), AmountMicro: 19900000, TxHash: "goodtx", Status: store.BillingStatusPending,
		CreatedAt: time.Now().Unix()}
	assertTrue(t, repo.Create(o) == nil, "create order")

	runBillingVerifySweep()

	got, err := repo.GetByID(o.ID)
	assertTrue(t, err == nil, "reload order")
	if got.Status != store.BillingStatusPaid {
		t.Fatalf("sweep 后订单必须 paid, got %s (%s)", got.Status, got.FailReason)
	}
	sub, _ := repo.GetSubscription(int64(uid))
	if sub.Credits != 500 {
		t.Fatalf("sweep 后必须发放 500 积分, got %d", sub.Credits)
	}

	// 过期清理：创建一个 1 小时前创建的 pending 订单（无哈希）→ expired
	oldOrder := &store.BillingOrder{ID: "bill_sweep_old", UserID: int64(uid), PlanID: "monthly", Chain: "TRC20",
		Address: tronTestB58(), AmountMicro: 19900000, Status: store.BillingStatusPending,
		CreatedAt: time.Now().Unix() - 3600}
	assertTrue(t, repo.Create(oldOrder) == nil, "create stale order")
	runBillingVerifySweep()
	gotOld, _ := repo.GetByID(oldOrder.ID)
	if gotOld.Status != store.BillingStatusExpired {
		t.Fatalf("过期订单必须 expired, got %s", gotOld.Status)
	}
}

// ── C4.1 增强：TRC20 确认数 / 金额容差 / 24h 超时 / verification 端点 ──

// tronEventsBodyWithBlock 带 block_number 的事件体（确认数核验用）。
func tronEventsBodyWithBlock(contract, to, value string, block int64, success bool) string {
	return fmt.Sprintf(`{"success":%v,"data":[{"transaction_id":"tx1","event_name":"Transfer","contract_address":"%s","block_number":%d,"result":{"value":"%s","to_address":"%s"}}]}`,
		success, contract, block, value, to)
}

func TestBillingVerifyTRC20Confirmations(t *testing.T) {
	t.Setenv("TRON_MIN_CONFIRMATIONS", "19")
	addr := tronTestB58()
	var mock *httptest.Server
	mock = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/v1/blocks/latest") {
			fmt.Fprint(w, `{"number":200}`)
			return
		}
		// 块高 100 的足额转账：确认数 = 200-100+1 = 101 ≥ 19
		fmt.Fprint(w, tronEventsBodyWithBlock(tronUSDTContract, addr, "19900000", 100, true))
	}))
	defer mock.Close()
	old := tronGridBaseURL
	tronGridBaseURL = mock.URL
	defer func() { tronGridBaseURL = old }()

	res := verifyTRC20Tx("good", addr, 19900000)
	assertTrue(t, res.Err == nil && res.Found && res.Valid && res.Confirmed,
		fmt.Sprintf("确认数足够必须 confirmed: %+v", res))
	if res.Confirmations != 101 || res.RequiredConfirmations != 19 || res.BlockNumber != 100 {
		t.Fatalf("确认数细节异常: %+v", res)
	}
	if res.ReceivedMicro != 19900000 {
		t.Fatalf("实收金额异常: %+v", res)
	}
}

func TestBillingVerifyTRC20Confirming(t *testing.T) {
	t.Setenv("TRON_MIN_CONFIRMATIONS", "19")
	addr := tronTestB58()
	var mock *httptest.Server
	mock = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/v1/blocks/latest") {
			fmt.Fprint(w, `{"number":105}`) // 确认数 6 < 19 → confirming
			return
		}
		fmt.Fprint(w, tronEventsBodyWithBlock(tronUSDTContract, addr, "19900000", 100, true))
	}))
	defer mock.Close()
	old := tronGridBaseURL
	tronGridBaseURL = mock.URL
	defer func() { tronGridBaseURL = old }()

	res := verifyTRC20Tx("unconfirmed", addr, 19900000)
	assertTrue(t, res.Err == nil && res.Found && res.Valid && !res.Confirmed,
		fmt.Sprintf("确认数不足必须 confirming: %+v", res))
}

func TestBillingVerifyTRC20AmountTolerance(t *testing.T) {
	t.Setenv("BILLING_AMOUNT_TOLERANCE_PCT", "1")
	addr := tronTestB58()
	var mock *httptest.Server
	mock = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 少付 0.5%（19800500 ≥ 19900000*0.99=19701000）→ 应认可
		fmt.Fprint(w, tronEventsBody(tronUSDTContract, addr, "19800500", true))
	}))
	defer mock.Close()
	old := tronGridBaseURL
	tronGridBaseURL = mock.URL
	defer func() { tronGridBaseURL = old }()

	res := verifyTRC20Tx("tolerant", addr, 19900000)
	assertTrue(t, res.Err == nil && res.Found && res.Valid,
		fmt.Sprintf("1%% 内容差必须认可: %+v", res))
}

func TestBillingVerify24hTimeout(t *testing.T) {
	t.Setenv("USDT_TRC20_ADDRESS", tronTestB58())
	var mock *httptest.Server
	mock = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 链上一直查不到（pending）→ 24h 超时必须 failed（而非 expired，可重提哈希）
		fmt.Fprint(w, `{"success":false,"data":[]}`)
	}))
	defer mock.Close()
	old := tronGridBaseURL
	tronGridBaseURL = mock.URL
	defer func() { tronGridBaseURL = old }()

	uid := billingSeedUser(t, "billing_24h")
	repo := store.NewBillingRepo()
	o := &store.BillingOrder{ID: "bill_24h_1", UserID: int64(uid), PlanID: "monthly", Chain: "TRC20",
		Address: tronTestB58(), AmountMicro: 19900000, TxHash: "pendingtx", Status: store.BillingStatusConfirming,
		CreatedAt: time.Now().Unix() - 25*3600}
	assertTrue(t, repo.Create(o) == nil, "create order")

	runBillingVerifySweep()
	got, err := repo.GetByID(o.ID)
	assertTrue(t, err == nil, "reload")
	if got.Status != store.BillingStatusFailed {
		t.Fatalf("24h 超时必须 failed（可重提哈希）, got %s", got.Status)
	}
}

func TestBillingVerificationEndpoint(t *testing.T) {
	t.Setenv("USDT_TRC20_ADDRESS", tronTestB58())
	var mock *httptest.Server
	mock = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/v1/blocks/latest") {
			fmt.Fprint(w, `{"number":100}`) // 确认数 1 < 19 → confirming
			return
		}
		fmt.Fprint(w, tronEventsBodyWithBlock(tronUSDTContract, tronTestB58(), "19900000", 100, true))
	}))
	defer mock.Close()
	old := tronGridBaseURL
	tronGridBaseURL = mock.URL
	defer func() { tronGridBaseURL = old }()
	t.Setenv("TRON_MIN_CONFIRMATIONS", "19")

	uid := billingSeedUser(t, "billing_verif")
	repo := store.NewBillingRepo()
	o := &store.BillingOrder{ID: "bill_verif_1", UserID: int64(uid), PlanID: "monthly", Chain: "TRC20",
		Address: tronTestB58(), AmountMicro: 19900000, TxHash: "vtx", Status: store.BillingStatusPending,
		CreatedAt: time.Now().Unix()}
	assertTrue(t, repo.Create(o) == nil, "create order")

	runBillingVerifySweep()

	r := setupRouter()
	r.GET("/billing/orders/:id/verification", billingAuthed(uid, "user"), BillingOrderVerification)
	code, resp := billingGet(t, r, "/billing/orders/"+o.ID+"/verification")
	assertEq(t, code, http.StatusOK, "verification 200")
	if resp["stage"] != "confirming" {
		t.Fatalf("stage 应为 confirming: %v", resp["stage"])
	}
	ver, ok := resp["verification"].(map[string]any)
	if !ok || ver["block_number"].(float64) != 100 {
		t.Fatalf("verification 快照应含链上细节: %+v", resp["verification"])
	}

	// 他人查询 → 403
	rOther := setupRouter()
	rOther.GET("/billing/orders/:id/verification", billingAuthed(billingSeedUser(t, "billing_verif_other"), "user"), BillingOrderVerification)
	code, _ = billingGet(t, rOther, "/billing/orders/"+o.ID+"/verification")
	assertEq(t, code, http.StatusForbidden, "他人查询必须 403")
}

// ── A4.1 Stripe：plan_id 直达收银台 / price 映射 / 支付失败 ──

// stripeFakeAPI 启动一个假的 Stripe API，捕获创建 checkout session 的表单，
// 返回固定收银台 URL。
func stripeFakeAPI(t *testing.T, captured *map[string][]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/checkout/sessions") {
			t.Errorf("unexpected stripe path: %s", r.URL.Path)
		}
		if user, _, ok := r.BasicAuth(); !ok || !strings.HasPrefix(user, "sk_") {
			t.Errorf("stripe basic auth missing/invalid")
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		*captured = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cs_test_1","url":"https://checkout.stripe.com/pay/cs_test_1"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// stripeFormVal 安全读取 fake Stripe API 捕获的表单值（缺键返回空串）。
func stripeFormVal(form map[string][]string, key string) string {
	if v := form[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// TestBillingStripeCheckoutByPlanID plan_id 直达：自动建单 → 创建 session →
// 再调一次复用同一 pending 订单（不重复下单）。
func TestBillingStripeCheckoutByPlanID(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_planid")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_x")
	var captured map[string][]string
	srv := stripeFakeAPI(t, &captured)
	stripeAPIBase = srv.URL + "/v1"
	defer func() { stripeAPIBase = "https://api.stripe.com/v1" }()

	uid := billingSeedUser(t, "stripe_planid")
	r := setupRouter()
	r.POST("/billing/stripe/checkout", billingAuthed(uid, "user"), BillingStripeCheckout)

	body := map[string]any{
		"plan_id":     "monthly",
		"success_url": "https://app.example.com/billing?ok=1",
		"cancel_url":  "https://app.example.com/billing?ok=0",
	}
	code, resp := billingPost(t, r, "/billing/stripe/checkout", body)
	assertEq(t, code, http.StatusOK, "plan_id checkout 200")
	if !strings.HasPrefix(fmt.Sprint(resp["checkout_url"]), "https://checkout.stripe.com/") {
		t.Fatalf("checkout_url 缺失: %v", resp)
	}
	// 表单必须带 metadata 回链与内联价格（未配置 price 映射时）
	if stripeFormVal(captured, "metadata[order_id]") == "" {
		t.Fatalf("metadata[order_id] 缺失: %v", captured)
	}
	if stripeFormVal(captured, "line_items[0][price_data][unit_amount]") != "1990" {
		t.Fatalf("月付 19.9 USD 应为 1990 美分: %v", stripeFormVal(captured, "line_items[0][price_data][unit_amount]"))
	}
	if stripeFormVal(captured, "success_url") != "https://app.example.com/billing?ok=1" {
		t.Fatalf("success_url 透传失败: %v", captured)
	}

	// 再调一次 → 复用同一订单（同用户同套餐 pending 唯一）
	code, _ = billingPost(t, r, "/billing/stripe/checkout", body)
	assertEq(t, code, http.StatusOK, "重复 checkout 仍 200")
	orders, err := store.NewBillingRepo().ListByUser(int64(uid), 100)
	if err != nil || len(orders) != 1 {
		t.Fatalf("应复用同一 pending 订单（实际 %d 个）: %v", len(orders), err)
	}

	// 非法 plan_id → 400
	code, _ = billingPost(t, r, "/billing/stripe/checkout", map[string]any{"plan_id": "nope"})
	assertEq(t, code, http.StatusBadRequest, "bad plan_id 400")
}

// TestBillingStripeCheckoutPriceMapping 配置了 STRIPE_PRICE_ID_MONTHLY 时
// checkout 直接引用 Price 对象而非内联 price_data。
func TestBillingStripeCheckoutPriceMapping(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_price")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_x")
	t.Setenv("STRIPE_PRICE_ID_MONTHLY", "price_1TestMonthly")
	var captured map[string][]string
	srv := stripeFakeAPI(t, &captured)
	stripeAPIBase = srv.URL + "/v1"
	defer func() { stripeAPIBase = "https://api.stripe.com/v1" }()

	uid := billingSeedUser(t, "stripe_price")
	r := setupRouter()
	r.POST("/billing/stripe/checkout", billingAuthed(uid, "user"), BillingStripeCheckout)

	code, _ := billingPost(t, r, "/billing/stripe/checkout", map[string]any{
		"plan_id": "monthly", "success_url": "https://ok.example.com", "cancel_url": "https://no.example.com",
	})
	assertEq(t, code, http.StatusOK, "checkout 200")
	if stripeFormVal(captured, "line_items[0][price]") != "price_1TestMonthly" {
		t.Fatalf("应引用 stripe price 映射: %v", captured)
	}
	if stripeFormVal(captured, "line_items[0][price_data][unit_amount]") != "" {
		t.Fatalf("配置 price 映射后不应再发 price_data: %v", captured)
	}
}

// TestBillingStripePaymentFailedWebhook 支付失败事件 → 订单标记 failed；
// 已支付订单不受失败事件影响。
func TestBillingStripePaymentFailedWebhook(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_fail")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_fail")
	uid := billingSeedUser(t, "stripe_fail")
	repo := store.NewBillingRepo()
	o := &store.BillingOrder{ID: "bill_stripe_fail1", UserID: int64(uid), PlanID: "monthly", Chain: "stripe",
		AmountMicro: 19900000, Status: store.BillingStatusPending, CreatedAt: time.Now().Unix()}
	assertTrue(t, repo.Create(o) == nil, "create order")

	r := setupRouter()
	r.POST("/billing/stripe/webhook", BillingStripeWebhook) // 公开路由，无 auth

	event := map[string]any{
		"type": "checkout.session.async_payment_failed",
		"data": map[string]any{"object": map[string]any{
			"id":       "cs_fail_1",
			"metadata": map[string]any{"order_id": o.ID},
		}},
	}
	raw, _ := json.Marshal(event)
	ts := time.Now().Unix()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/billing/stripe/webhook", strings.NewReader(string(raw)))
	req.Header.Set("Stripe-Signature", signStripePayload("whsec_fail", raw, ts))
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "失败事件必须 200")
	got, err := repo.GetByID(o.ID)
	assertTrue(t, err == nil && got.Status == store.BillingStatusFailed,
		fmt.Sprintf("async_payment_failed 必须置 failed: %+v", got))
	if !strings.Contains(got.FailReason, "支付失败") {
		t.Fatalf("fail_reason 应说明支付失败: %q", got.FailReason)
	}

	// payment_intent.payment_failed 同样标记失败（新订单）
	o2 := &store.BillingOrder{ID: "bill_stripe_fail2", UserID: int64(uid), PlanID: "yearly", Chain: "stripe",
		AmountMicro: 199000000, Status: store.BillingStatusPending, CreatedAt: time.Now().Unix()}
	assertTrue(t, repo.Create(o2) == nil, "create order 2")
	piEvent := map[string]any{
		"type": "payment_intent.payment_failed",
		"data": map[string]any{"object": map[string]any{
			"id":       "pi_fail_1",
			"metadata": map[string]any{"order_id": o2.ID},
		}},
	}
	raw2, _ := json.Marshal(piEvent)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/billing/stripe/webhook", strings.NewReader(string(raw2)))
	req.Header.Set("Stripe-Signature", signStripePayload("whsec_fail", raw2, ts))
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "pi 失败事件 200")
	got2, _ := repo.GetByID(o2.ID)
	assertTrue(t, got2.Status == store.BillingStatusFailed, "payment_intent.payment_failed 必须置 failed")

	// 已支付订单收到失败事件 → 状态不变（仍 paid）
	o3 := &store.BillingOrder{ID: "bill_stripe_fail3", UserID: int64(uid), PlanID: "monthly", Chain: "stripe",
		AmountMicro: 19900000, Status: store.BillingStatusPaid, CreatedAt: time.Now().Unix(), ConfirmedAt: time.Now().Unix()}
	assertTrue(t, repo.Create(o3) == nil, "create paid order")
	raw3, _ := json.Marshal(map[string]any{
		"type": "checkout.session.async_payment_failed",
		"data": map[string]any{"object": map[string]any{"id": "cs_x", "metadata": map[string]any{"order_id": o3.ID}}},
	})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/billing/stripe/webhook", strings.NewReader(string(raw3)))
	req.Header.Set("Stripe-Signature", signStripePayload("whsec_fail", raw3, ts))
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "paid 订单失败事件仍 200")
	got3, _ := repo.GetByID(o3.ID)
	assertTrue(t, got3.Status == store.BillingStatusPaid, "已支付订单不得被失败事件回退")

	// 未知事件类型 → 200 ignored
	raw4, _ := json.Marshal(map[string]any{
		"type": "charge.succeeded",
		"data": map[string]any{"object": map[string]any{"id": "ch_1"}},
	})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/billing/stripe/webhook", strings.NewReader(string(raw4)))
	req.Header.Set("Stripe-Signature", signStripePayload("whsec_fail", raw4, ts))
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "未知事件 200")
}
