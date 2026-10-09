package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
)

// ── D2/H3：在线单量限制（online_order_limit，币富名词解释 #32）──
//
// 语义口径（引擎模型核查结论，详见 cra_online_limit.go 头注）：CRA 单实例
// 单 symbol 单方向持仓，持仓中不会再开首单（cra_strategy.go 首单分支被
// !st.InPosition 门控），dual 也是每循环单选方向——单实例并发持仓恒为 1，
// 参数在实例内部无调节空间。故实现为币富本来的跨交易对总量控制：同一用户
// 名下 running 状态 CRA 合约实例数上限，在 Start 路径校验。
//
// H3（2026-10-09）补全币富 #32 完整语义：多单数量与空单数量**分别**限制。
// 按实例 direction 分列计数（long→多侧、short→空侧、dual 两侧各占一席），
// 多侧与空侧 running 数分别不超上限才放行，超任一侧 409 注明是哪一侧。

// seedCRAConfig 落库一条策略配置（经 DB 往返，与生产读取路径一致）。
func seedCRAConfig(t *testing.T, id string, userID int64, stype, status string, limit any) {
	t.Helper()
	seedCRAConfigDir(t, id, userID, stype, status, "long", limit)
}

// seedCRAConfigDir 同 seedCRAConfig，显式指定 direction（H3 多空分列用例）。
func seedCRAConfigDir(t *testing.T, id string, userID int64, stype, status, direction string, limit any) {
	t.Helper()
	cfg := map[string]any{
		"first_order_amount": 10, "tp_mode": "static", "take_profit_ratio": 0.013,
		"market_type": "swap", "leverage": 10, "direction": direction,
	}
	if limit != nil {
		cfg["online_order_limit"] = limit
	}
	cj, _ := json.Marshal(cfg)
	item := map[string]any{
		"id": id, "user_id": userID, "name": id, "strategy_type": stype,
		"symbol": "BTCUSDT", "status": status, "config_json": string(cj),
		"market_type": "swap", "leverage": 10,
	}
	store.SetStrategyConfig(id, item)
	store.PersistStrategyConfigs()
	t.Cleanup(func() {
		store.DeleteStrategyConfig(id)
		_ = store.NewStrategyConfigRepo().Delete(id)
	})
}

func TestCRAOnlineOrderLimitParsing(t *testing.T) {
	// 缺失 → 默认 10（与 cra.ParseCRAParams/前端预设一致）。
	if got := craOnlineOrderLimit(map[string]any{}); got != 10 {
		t.Fatalf("missing key: got %d, want 10", got)
	}
	// config_json 取值。
	if got := craOnlineOrderLimit(map[string]any{"config_json": `{"online_order_limit":3}`}); got != 3 {
		t.Fatalf("config_json: got %d, want 3", got)
	}
	// 顶层平铺键兜底；config_json 优先于顶层。
	if got := craOnlineOrderLimit(map[string]any{"online_order_limit": 4}); got != 4 {
		t.Fatalf("top-level: got %d, want 4", got)
	}
	if got := craOnlineOrderLimit(map[string]any{
		"online_order_limit": 4, "config_json": `{"online_order_limit":2}`,
	}); got != 2 {
		t.Fatalf("config_json must win over top-level: got %d, want 2", got)
	}
	// <1 钳制为 1（限额必须有牙齿，0/负数视为配置错误按最严口径）。
	if got := craOnlineOrderLimit(map[string]any{"config_json": `{"online_order_limit":0}`}); got != 1 {
		t.Fatalf("zero clamp: got %d, want 1", got)
	}
}

// H3：有效方向解析——与引擎 ParseCRAParams 同源（config_json direction 优先，
// 顶层兜底，缺失/非法归 long）。
func TestCRAItemDirection(t *testing.T) {
	cases := []struct {
		name string
		item map[string]any
		want string
	}{
		{"缺失默认 long", map[string]any{}, "long"},
		{"config_json short", map[string]any{"config_json": `{"direction":"short"}`}, "short"},
		{"config_json dual", map[string]any{"config_json": `{"direction":"dual"}`}, "dual"},
		{"大小写归一", map[string]any{"config_json": `{"direction":"Short"}`}, "short"},
		{"顶层兜底", map[string]any{"direction": "dual"}, "dual"},
		{"config_json 优先于顶层", map[string]any{"direction": "short", "config_json": `{"direction":"long"}`}, "long"},
		{"非法值归 long（保守多侧）", map[string]any{"config_json": `{"direction":"sideways"}`}, "long"},
	}
	for _, tc := range cases {
		if got := craItemDirection(tc.item); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCRAOnlineOrderLimitCountScope(t *testing.T) {
	seedCRAConfig(t, "ool-scope-run", 7, "cra_contract", "running", nil)
	// 别名类型（trend_long → cra_contract 引擎）也计入。
	seedCRAConfig(t, "ool-scope-alias", 7, "trend_long", "running", nil)
	// 以下一律不计入：stopped / 现货 / 其他用户。
	seedCRAConfig(t, "ool-scope-stopped", 7, "cra_contract", "stopped", nil)
	seedCRAConfig(t, "ool-scope-spot", 7, "cra_spot", "running", nil)
	seedCRAConfig(t, "ool-scope-other-user", 8, "cra_contract", "running", nil)

	longN, shortN := countRunningCRAContractsBySide(7, "")
	if longN != 2 || shortN != 0 {
		t.Fatalf("user 7 running CRA = long %d/short %d, want 2/0 (alias included, stopped/spot/other-user excluded)", longN, shortN)
	}
	// 自排除：重启自身时自己不占名额。
	longN, shortN = countRunningCRAContractsBySide(7, "ool-scope-run")
	if longN != 1 || shortN != 0 {
		t.Fatalf("self-excluded count = %d/%d, want 1/0", longN, shortN)
	}
	longN, shortN = countRunningCRAContractsBySide(8, "")
	if longN != 1 || shortN != 0 {
		t.Fatalf("user 8 count = %d/%d, want 1/0", longN, shortN)
	}
}

// H3：按方向分列计数——long 计多侧、short 计空侧、dual 两侧各占一席。
func TestCRAOnlineOrderLimitCountBySide(t *testing.T) {
	seedCRAConfigDir(t, "ool-side-long", 21, "cra_contract", "running", "long", nil)
	seedCRAConfigDir(t, "ool-side-short", 21, "cra_contract", "running", "short", nil)
	seedCRAConfigDir(t, "ool-side-dual", 21, "cra_contract", "running", "dual", nil)
	// stopped 的 dual 不计入。
	seedCRAConfigDir(t, "ool-side-dual-stopped", 21, "cra_contract", "stopped", "dual", nil)

	longN, shortN := countRunningCRAContractsBySide(21, "")
	if longN != 2 || shortN != 2 {
		t.Fatalf("long/short = %d/%d, want 2/2（long+dual / short+dual）", longN, shortN)
	}
	// 排除 dual 自身：两侧各减一。
	longN, shortN = countRunningCRAContractsBySide(21, "ool-side-dual")
	if longN != 1 || shortN != 1 {
		t.Fatalf("排除 dual 后 long/short = %d/%d, want 1/1", longN, shortN)
	}
}

func TestCRAOnlineOrderLimitEnforcement(t *testing.T) {
	seedCRAConfig(t, "ool-enf-run", 9, "cra_contract", "running", nil)

	overLimit := map[string]any{
		"id": "ool-enf-new", "user_id": int64(9), "strategy_type": "cra_contract",
		"config_json": `{"online_order_limit":1}`,
	}
	if err := enforceOnlineOrderLimit("ool-enf-new", overLimit); err == nil {
		t.Fatal("over-limit start must be rejected")
	} else if !strings.Contains(err.Error(), "在线单量限制") {
		t.Fatalf("error must name the limit, got %v", err)
	}

	// 限额 2、已运行 1 → 放行。
	underLimit := map[string]any{
		"id": "ool-enf-new", "user_id": int64(9), "strategy_type": "cra_contract",
		"config_json": `{"online_order_limit":2}`,
	}
	if err := enforceOnlineOrderLimit("ool-enf-new", underLimit); err != nil {
		t.Fatalf("under-limit start must pass, got %v", err)
	}

	// 现货配置不校验（币富该功能在合约页）。
	spot := map[string]any{"id": "ool-enf-spot", "user_id": int64(9), "strategy_type": "cra_spot"}
	if err := enforceOnlineOrderLimit("ool-enf-spot", spot); err != nil {
		t.Fatalf("spot must be exempt, got %v", err)
	}
}

// H3 多空分列核心语义：多侧满、空侧有余 → 开空放行 / 开多拒绝（409 注明多侧）。
func TestCRAOnlineOrderLimitLongFullShortOpen(t *testing.T) {
	seedCRAConfigDir(t, "ool-split-long-run", 22, "cra_contract", "running", "long", nil)

	newLong := map[string]any{
		"id": "ool-split-new-long", "user_id": int64(22), "strategy_type": "cra_contract",
		"config_json": `{"online_order_limit":1,"direction":"long"}`,
	}
	err := enforceOnlineOrderLimit("ool-split-new-long", newLong)
	if err == nil {
		t.Fatal("多侧已满，开多必须拒绝")
	} else {
		if !strings.Contains(err.Error(), "多侧") {
			t.Fatalf("报错必须注明多侧超限, got %v", err)
		}
		if strings.Contains(err.Error(), "空侧") {
			t.Fatalf("多侧超限不得误报空侧, got %v", err)
		}
	}

	// 空侧无 running 实例 → 开空放行（同一限额口径下两侧互不占名额）。
	newShort := map[string]any{
		"id": "ool-split-new-short", "user_id": int64(22), "strategy_type": "cra_contract",
		"config_json": `{"online_order_limit":1,"direction":"short"}`,
	}
	if err := enforceOnlineOrderLimit("ool-split-new-short", newShort); err != nil {
		t.Fatalf("多侧满不影响开空, got %v", err)
	}
}

// H3 空侧镜像：空侧满、多侧有余 → 开多放行 / 开空拒绝（409 注明空侧）。
func TestCRAOnlineOrderLimitShortFullLongOpen(t *testing.T) {
	seedCRAConfigDir(t, "ool-split-short-run", 23, "cra_contract", "running", "short", nil)

	newShort := map[string]any{
		"id": "ool-split-new-short", "user_id": int64(23), "strategy_type": "cra_contract",
		"config_json": `{"online_order_limit":1,"direction":"short"}`,
	}
	err := enforceOnlineOrderLimit("ool-split-new-short", newShort)
	if err == nil {
		t.Fatal("空侧已满，开空必须拒绝")
	} else if !strings.Contains(err.Error(), "空侧") {
		t.Fatalf("报错必须注明空侧超限, got %v", err)
	}

	newLong := map[string]any{
		"id": "ool-split-new-long", "user_id": int64(23), "strategy_type": "cra_contract",
		"config_json": `{"online_order_limit":1,"direction":"long"}`,
	}
	if err := enforceOnlineOrderLimit("ool-split-new-long", newLong); err != nil {
		t.Fatalf("空侧满不影响开多, got %v", err)
	}
}

// H3 dual 语义：dual 实例两侧各占一席——running dual 把两侧都顶到限额时，
// 开多/开空都拒；新开 dual 要求两侧同时有余（一侧满即拒，报满的那一侧）。
func TestCRAOnlineOrderLimitDualOccupiesBothSides(t *testing.T) {
	seedCRAConfigDir(t, "ool-dual-run", 24, "cra_contract", "running", "dual", nil)

	mk := func(id, dir string) map[string]any {
		return map[string]any{
			"id": id, "user_id": int64(24), "strategy_type": "cra_contract",
			"config_json": `{"online_order_limit":1,"direction":"` + dir + `"}`,
		}
	}
	// dual 占了两侧 → 开多/开空/再开 dual 全拒。
	for _, dir := range []string{"long", "short", "dual"} {
		if err := enforceOnlineOrderLimit("ool-dual-new-"+dir, mk("ool-dual-new-"+dir, dir)); err == nil {
			t.Fatalf("dual 已占满两侧，开 %s 必须拒绝", dir)
		}
	}
	// 开多被拒报多侧、开空被拒报空侧（dual 实例两侧各占一席的直接证据）。
	if err := enforceOnlineOrderLimit("ool-dual-new-long", mk("ool-dual-new-long", "long")); !strings.Contains(err.Error(), "多侧") {
		t.Fatalf("dual 占多侧名额，开多报错须注明多侧, got %v", err)
	}
	if err := enforceOnlineOrderLimit("ool-dual-new-short", mk("ool-dual-new-short", "short")); !strings.Contains(err.Error(), "空侧") {
		t.Fatalf("dual 占空侧名额，开空报错须注明空侧, got %v", err)
	}

	// 新开 dual：多侧满（空侧有余）→ 拒，报多侧。
	seedCRAConfigDir(t, "ool-dual-long2", 25, "cra_contract", "running", "long", nil)
	if err := enforceOnlineOrderLimit("ool-dual-new25", map[string]any{
		"id": "ool-dual-new25", "user_id": int64(25), "strategy_type": "cra_contract",
		"config_json": `{"online_order_limit":1,"direction":"dual"}`,
	}); err == nil || !strings.Contains(err.Error(), "多侧") {
		t.Fatalf("多侧满时新开 dual 必须拒且报多侧, got %v", err)
	}
	// 两侧都有余 → dual 放行。
	if err := enforceOnlineOrderLimit("ool-dual-new26", map[string]any{
		"id": "ool-dual-new26", "user_id": int64(26), "strategy_type": "cra_contract",
		"config_json": `{"online_order_limit":1,"direction":"dual"}`,
	}); err != nil {
		t.Fatalf("两侧有余时 dual 必须放行, got %v", err)
	}
}

// TestCRAOnlineOrderLimitStartHTTP 用户 Start 路径端到端：超限 409 且策略
// 没有进引擎；未超限 200 且引擎启动成功（证明校验接在真实 Start 链路上）。
func TestCRAOnlineOrderLimitStartHTTP(t *testing.T) {
	eng := strategy.GetEngine(event.NewEventBus(4096, 1))
	strategy.RegisterStrategyFactory("cra_contract", func() strategy.Strategy {
		return cra.NewCRAContractStrategy("cra_contract", "BTCUSDT")
	})

	r := setupRouter()
	r.POST("/strategies/configs/:id/start", StartStrategyConfig)
	start := func(id string) (int, map[string]any) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/strategies/configs/"+id+"/start", nil)
		r.ServeHTTP(w, req)
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return w.Code, resp
	}

	// 已运行 1 个（用户 11），新配置限额 1 → 409，detail 说明口径。
	seedCRAConfig(t, "ool-http-run", 11, "cra_contract", "running", nil)
	seedCRAConfig(t, "ool-http-blocked", 11, "cra_contract", "stopped", 1)
	code, resp := start("ool-http-blocked")
	assertEq(t, code, http.StatusConflict, "over-limit start must be 409")
	assertTrue(t, strings.Contains(getString(resp, "detail", ""), "在线单量限制"),
		"409 detail must explain the limit, got "+getString(resp, "detail", ""))
	assertTrue(t, eng.Get("ool-http-blocked") == nil, "blocked strategy must not enter engine")

	// 限额调大为 2 → 同一 Start 链路放行，引擎实例真实在跑。
	seedCRAConfig(t, "ool-http-ok", 11, "cra_contract", "stopped", 2)
	code, resp = start("ool-http-ok")
	assertEq(t, code, http.StatusOK, "under-limit start must be 200, detail="+getString(resp, "detail", ""))
	t.Cleanup(func() { stopStrategyInEngine("ool-http-ok") })
	if s := eng.Get("ool-http-ok"); s == nil || !s.IsRunning() {
		t.Fatal("under-limit strategy must be registered and running in engine")
	}
}

// H3 Start 端到端多空分列：多侧满（限额 1）时，同用户开多 409 注明多侧、
// 开空 200 真实启动——409/200 由 direction 决定而非总数。
func TestCRAOnlineOrderLimitSplitStartHTTP(t *testing.T) {
	eng := strategy.GetEngine(event.NewEventBus(4096, 1))
	strategy.RegisterStrategyFactory("cra_contract", func() strategy.Strategy {
		return cra.NewCRAContractStrategy("cra_contract", "BTCUSDT")
	})

	r := setupRouter()
	r.POST("/strategies/configs/:id/start", StartStrategyConfig)
	start := func(id string) (int, map[string]any) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/strategies/configs/"+id+"/start", nil)
		r.ServeHTTP(w, req)
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return w.Code, resp
	}

	// 用户 12：多侧已跑 1 个（限额 1 顶满多侧，空侧空）。
	seedCRAConfigDir(t, "ool-split-http-run", 12, "cra_contract", "running", "long", nil)

	// 开多 → 409 且注明多侧。
	seedCRAConfigDir(t, "ool-split-http-long", 12, "cra_contract", "stopped", "long", 1)
	code, resp := start("ool-split-http-long")
	assertEq(t, code, http.StatusConflict, "long-side full: new long must be 409")
	assertTrue(t, strings.Contains(getString(resp, "detail", ""), "多侧"),
		"409 detail must name the long side, got "+getString(resp, "detail", ""))
	assertTrue(t, eng.Get("ool-split-http-long") == nil, "blocked long must not enter engine")

	// 开空 → 200 放行（空侧有余），引擎真实启动。
	seedCRAConfigDir(t, "ool-split-http-short", 12, "cra_contract", "stopped", "short", 1)
	code, resp = start("ool-split-http-short")
	assertEq(t, code, http.StatusOK, "short side has room: new short must be 200, detail="+getString(resp, "detail", ""))
	t.Cleanup(func() { stopStrategyInEngine("ool-split-http-short") })
	if s := eng.Get("ool-split-http-short"); s == nil || !s.IsRunning() {
		t.Fatal("short strategy must be registered and running in engine")
	}
}
